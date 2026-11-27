package app

import (
	"bytes"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	notionErrorInvalidJSON        = "invalid_json"
	notionInvalidJSONMessage      = "Error parsing JSON body."
	notionVersionHeaderName       = "Notion-Version header"
	notionBodyTakesOtherKeys      = "notionBodyTakesOtherKeys"
	notionUnsupportedOldVersion   = "Unsupported Notion-Version: this endpoint takes " + notionPreviousAPIVersion + " or " + notionCurrentAPIVersion + "."
	notionInsufficientPermissions = "The integration isn't allowed to use this endpoint."
)

// The versions Notion's API takes (seen 2026-11-10). Leafen takes the last
// two; an older one of these is refused with why, any other as Notion
// refuses a version it doesn't know.
var notionKnownAPIVersions = []string{
	"2021-05-11", "2021-05-13", "2021-08-16", "2022-02-22", "2022-06-28",
	notionPreviousAPIVersion, notionCurrentAPIVersion,
}

// refuseNotionRequest is the one place a public API request is refused
// before its endpoint looks anything up, in Notion's order (seen 2026-11-10):
// a body by its type and size first (refuseNotionBodyByType), then one sent
// as JSON that isn't JSON (refuseNotionUnreadableJSON), a multipart body to
// an endpoint that takes no file (refuseNotionMultipartBody), the token,
// the Notion-Version header, what the token may do (its scope, an internal
// connection's content capabilities and an endpoint no personal access token
// reaches, before any shape: a body or query Notion would refuse is refused
// 403 first), then the query and the body; the
// endpoint then finds its target.
func refuseNotionRequest(event *core.RequestEvent) (bool, error) {
	route := notionRouteShapeFor(event.Request.Method, event.Request.URL.Path)
	contentScope := notionContentWriteScope(event.Request.Method, event.Request.URL.Path)
	if route == nil && contentScope == "" {
		return false, nil
	}
	if route == nil || !route.multipart {
		if refused, err := refuseNotionMultipartBody(event); refused {
			return true, err
		}
	}
	identity, ok, err := notionPublicAPIBearerFromRequest(event)
	if err != nil {
		return true, notionRequestNotCompleted(event, err)
	}
	if !ok {
		return true, notionUnauthorized(event)
	}
	if valid, err := validateNotionUserDirectoryVersion(event); !valid {
		return true, err
	}
	if route != nil && route.scope != "" && !notionOAuthHasScope(identity, route.scope) ||
		route != nil && route.patRefused && identity.ClientID == "personal-access-token" ||
		!notionContentWriteAllowed(identity, contentScope) {
		return true, notionCompatibilityError(event, http.StatusForbidden, notionErrorRestrictedResource, notionInsufficientPermissions)
	}
	if route == nil {
		return false, nil
	}
	query := &notionValue{kind: 'o', fields: map[string]*notionValue{}}
	if route.query != nil {
		read, conflict := notionRequestQuery(event.Request.URL.RawQuery)
		if conflict != "" {
			return true, notionValidationRefused(event, "Conflicting values for \""+conflict+"\" in query parameters.")
		}
		if faults := route.query.check(read, []string{"query"}); len(faults) > 0 {
			return true, notionValidationRefused(event, notionFaultsMessage("query", faults))
		}
		query = read
	}
	if mediaType, _, _ := mime.ParseMediaType(event.Request.Header.Get("Content-Type")); route.multipart && mediaType != "multipart/form-data" {
		return true, notionValidationRefused(event, notionFaultsMessage("body", []notionFault{{
			path: []string{"body", "file"}, expect: []string{"defined"}, shown: "undefined",
		}}))
	}
	body := &notionValue{kind: 'o', fields: map[string]*notionValue{}}
	if route.body != nil {
		read, refusal := notionRequestBody(event)
		if refusal != nil {
			return true, refusal(event)
		}
		if read == nil {
			// Too large to read here: the endpoint refuses it.
			return false, nil
		}
		if faults := route.body.check(read, []string{"body"}); len(faults) > 0 {
			return true, notionValidationRefused(event, notionFaultsMessage("body", faults))
		}
		body = read
		// A body Notion takes other keys in is read the same way after.
		if route.body.takesOtherKeys() {
			event.Set(notionBodyTakesOtherKeys, true)
		}
	}
	if route.after != nil {
		if said := route.after(body, query); said != "" {
			return true, notionValidationRefused(event, said)
		}
	}
	return false, nil
}

// takesOtherKeys reports whether a body of this shape may hold keys it
// doesn't name: a tagged one, an open object, or a choice of open objects (a
// comment's, seen 2026-11-11).
func (shape *notionShape) takesOtherKeys() bool {
	switch shape.kind {
	case notionTagged:
		return true
	case notionObject:
		return !shape.closed
	case notionUnion:
		for _, branch := range shape.branches {
			if branch.kind != notionObject || branch.closed {
				return false
			}
		}
		return len(shape.branches) > 0
	}
	return false
}

// refuseNotionUnreadableJSON refuses a body sent as JSON that doesn't read
// as an object or a list, before anything else of the request but its type
// and size (refuseNotionBodyByType), token and address too, as Notion does
// (seen 2026-11-10: "Error parsing JSON body." without a token, with an
// unknown one and on an unknown path; a body sent as anything else isn't
// read).
func refuseNotionUnreadableJSON(event *core.RequestEvent) (bool, error) {
	if kind, _ := notionRequestBodyKind(event.Request); kind != notionBodyJSON ||
		event.Request.Body == nil || event.Request.Body == http.NoBody {
		return false, nil
	}
	raw, err := io.ReadAll(io.LimitReader(event.Request.Body, notionPublicAPIMaxReadBytes+1))
	if err != nil || len(raw) > notionPublicAPIMaxReadBytes {
		// Not read whole (one past the bound was refused before,
		// refuseNotionOversizedJSON): the endpoint refuses it.
		event.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), event.Request.Body))
		return false, nil
	}
	// Read whole, the body is handed on as read: the request's own reader
	// starts over once it reaches its end.
	event.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if len(bytes.TrimSpace(raw)) == 0 {
		return false, nil
	}
	if value, err := parseNotionValue(raw); err == nil && (value.kind == 'o' || value.kind == 'a') {
		return false, nil
	}
	return true, notionCompatibilityError(event, http.StatusBadRequest, notionErrorInvalidJSON, notionInvalidJSONMessage)
}

func notionValidationRefused(event *core.RequestEvent, message string) error {
	return notionCompatibilityError(event, http.StatusBadRequest, notionErrorValidation, message)
}

// notionStartCursorRefused says a list's start_cursor names nowhere it can
// go on from, as Notion's API refuses every list's, in Leafen's words (seen
// 2026-11-12: people,
// search, children, comments, a data source's query and templates, views,
// a view's query and file uploads): with the cursor as given. A cursor of
// the wrong form, where the list's shape takes a UUID, was refused before
// (notionStartCursor).
func notionStartCursorRefused(event *core.RequestEvent, cursor string) error {
	return notionValidationRefused(event, notionStartCursorInvalid+cursor)
}

const notionStartCursorInvalid = "start_cursor isn't valid: "

// notionStartCursorText is a start_cursor of a UUID's form, as notionStartCursor takes it.
func notionStartCursorText(text string) bool {
	return notionCursorUUID.MatchString(text) || leafenOwnID.MatchString(text)
}

var notionCursorUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// notionQueryArrays are the query names Notion reads as a list, one value
// per repeat.
var notionQueryArrays = map[string]bool{"filter_properties": true}

// notionRequestQuery reads a query as Notion does: each name once (a name
// given twice, or as a list, is a conflict), and each value what JavaScript
// makes of it: true and false, a whole number Number() reads, or else the
// string.
func notionRequestQuery(raw string) (*notionValue, string) {
	query := &notionValue{kind: 'o', fields: map[string]*notionValue{}}
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		rawKey, rawValue, _ := strings.Cut(part, "=")
		key, err := url.QueryUnescape(rawKey)
		if err != nil {
			key = rawKey
		}
		value, err := url.QueryUnescape(rawValue)
		if err != nil {
			value = rawValue
		}
		listed := false
		if base, _, cut := strings.Cut(key, "["); cut && base != "" {
			key, listed = base, true
		}
		existing, seen := query.fields[key]
		if notionQueryArrays[key] {
			if !seen {
				existing = &notionValue{kind: 'a'}
				query.keys = append(query.keys, key)
				query.fields[key] = existing
			}
			existing.items = append(existing.items, &notionValue{kind: 's', text: value})
			continue
		}
		if seen || listed {
			return nil, key
		}
		query.keys = append(query.keys, key)
		query.fields[key] = notionQueryScalar(value)
	}
	return query, ""
}

var notionJSDecimal = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$`)

func notionQueryScalar(text string) *notionValue {
	switch text {
	case "true":
		return &notionValue{kind: 'b', truth: true}
	case "false":
		return &notionValue{kind: 'b'}
	}
	trimmed := strings.TrimSpace(text)
	number, ok := 0.0, false
	switch lower := strings.ToLower(trimmed); {
	case trimmed == "":
		ok = true
	case strings.HasPrefix(lower, "0x"), strings.HasPrefix(lower, "0o"), strings.HasPrefix(lower, "0b"):
		base := map[byte]int{'x': 16, 'o': 8, 'b': 2}[lower[1]]
		if whole, err := strconv.ParseUint(trimmed[2:], base, 64); err == nil {
			number, ok = float64(whole), true
		}
	case notionJSDecimal.MatchString(trimmed):
		parsed, err := strconv.ParseFloat(trimmed, 64)
		number, ok = parsed, err == nil || math.IsInf(parsed, 0)
	}
	if ok && !math.IsInf(number, 0) && number == math.Trunc(number) {
		return &notionValue{kind: 'n', text: notionJSNumber(number)}
	}
	return &notionValue{kind: 's', text: text}
}

// notionQueryDigits is a query number as Notion reads it, in digits: "1e1"
// and "0x10" are 10 and 16 (seen 2026-11-10). Anything else, and an empty
// value, stays as it came.
func notionQueryDigits(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	if value := notionQueryScalar(raw); value.kind == 'n' {
		return value.text
	}
	return raw
}

// notionRequestBody reads a body and puts it back for the endpoint. An
// empty body is an empty object, as is a body of a type Notion doesn't read
// (handed on empty, refuseNotionBodyByType); one that isn't JSON, or is JSON
// but not an object or a list, is refused as Notion refuses it. A body too
// large to read here comes back nil.
func notionRequestBody(event *core.RequestEvent) (*notionValue, func(*core.RequestEvent) error) {
	empty := &notionValue{kind: 'o', fields: map[string]*notionValue{}}
	if event.Request.Body == nil || event.Request.Body == http.NoBody {
		return empty, nil
	}
	raw, err := io.ReadAll(io.LimitReader(event.Request.Body, notionPublicAPIMaxReadBytes+1))
	if err != nil || len(raw) > notionPublicAPIMaxReadBytes {
		event.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), event.Request.Body))
		return nil, nil
	}
	event.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if len(bytes.TrimSpace(raw)) == 0 {
		return empty, nil
	}
	value, err := parseNotionValue(raw)
	if err != nil || value.kind != 'o' && value.kind != 'a' {
		return nil, func(event *core.RequestEvent) error {
			return notionCompatibilityError(event, http.StatusBadRequest, notionErrorInvalidJSON, notionInvalidJSONMessage)
		}
	}
	if value.kind == 'a' {
		return nil, func(event *core.RequestEvent) error {
			return notionValidationRefused(event, notionFaultsMessage("body", []notionFault{{
				path: []string{"body"}, expect: []string{"an object"}, shown: notionShownValue(value),
			}}))
		}
	}
	return value, nil
}

// validateNotionUserDirectoryVersion checks the Notion-Version header, the
// first thing after the token: a header left out or a version Notion doesn't
// know is refused as Notion refuses it, naming the versions Leafen takes; an
// older version Notion still takes is refused with why.
func validateNotionUserDirectoryVersion(event *core.RequestEvent) (bool, error) {
	values := event.Request.Header.Values("Notion-Version")
	if len(values) == 0 {
		return false, notionCompatibilityError(event, http.StatusBadRequest, notionErrorMissingVersion,
			notionFaultsMessage(notionVersionHeaderName, []notionFault{{
				path: []string{notionVersionHeaderName}, expect: []string{"defined"}, shown: "undefined",
			}}))
	}
	// A request at notionPreviousAPIVersion arrives here at the current one
	// (serveNotionAtPreviousVersion).
	version := strings.TrimSpace(values[0])
	if version == notionCurrentAPIVersion {
		return true, nil
	}
	if containsString(notionKnownAPIVersions, version) {
		return false, notionCompatibilityError(event, http.StatusBadRequest, notionErrorValidation, notionUnsupportedOldVersion)
	}
	return false, notionCompatibilityError(event, http.StatusBadRequest, notionErrorMissingVersion,
		notionFaultsMessage(notionVersionHeaderName, []notionFault{{
			path:   []string{notionVersionHeaderName},
			expect: []string{notionQuoted(notionPreviousAPIVersion), notionQuoted(notionCurrentAPIVersion)},
			shown:  notionShownValue(&notionValue{kind: 's', text: version}),
		}}))
}
