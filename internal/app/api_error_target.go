package app

import (
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/pocketbase/pocketbase/core"
)

// A 404's target is the thing that wasn't found: its kind and the path value
// that is its ID (api_error_details.go adds it to every route's answer). The
// error says which thing in its words — its message names it ("Row not
// found.", "No such hook."), or its code does (LEAFEN_TEAMSPACE_NOT_FOUND a
// teamspace) — and the path value whose own kind those words name is its ID:
// /admin/apps/installs/{installId}/secrets/{name} with "App not found." names
// the install, never the secret's name beside it (2026-11-24: an unknown
// install answered {kind: app, id: <the secret's name>}, so the agent looked
// for the wrong thing). The message comes first because a code may name a
// whole family (LEAFEN_DATABASE_MY_TASKS_NOT_FOUND with "Workspace not
// found."). A path value nothing names gives no ID rather than one of another
// kind: the target's kind and ID always name the same thing.

// apiRouteValue is one value a route's path holds: its name, the segment
// before it and what the request gave.
type apiRouteValue struct {
	Name, Before, Value string
}

// apiSentPathValuesKey keeps path values as the request sent them where a
// step before the route put others in their place — a key's session handle
// made the session's ID (user_key_response_secrets.go) — so a 404 names
// what was sent, never what stood in for it (2026-11-26: an unknown
// session's target read "lsh_", the value standing for no session; the ID a
// handle stands for is what the handle keeps from the key).
const apiSentPathValuesKey = "leafenAPISentPathValues"

// apiKeepSentPathValue keeps a path value as sent before it is replaced.
func apiKeepSentPathValue(event *core.RequestEvent, name string) {
	sent, _ := event.Get(apiSentPathValuesKey).(map[string]string)
	if sent == nil {
		sent = map[string]string{}
	}
	if _, kept := sent[name]; !kept {
		sent[name] = event.Request.PathValue(name)
	}
	event.Set(apiSentPathValuesKey, sent)
}

// apiRouteValues are the values of the route a request matched, outermost
// first.
func apiRouteValues(request *http.Request) []apiRouteValue {
	pattern := request.Pattern
	if _, path, method := strings.Cut(pattern, " "); method {
		pattern = path
	}
	values := []apiRouteValue{}
	before := ""
	for _, segment := range strings.Split(strings.Trim(pattern, "/"), "/") {
		name, wildcard := strings.CutPrefix(segment, "{")
		if !wildcard {
			before = segment
			continue
		}
		name = strings.TrimSuffix(strings.TrimSuffix(name, "}"), "...")
		values = append(values, apiRouteValue{Name: name, Before: before, Value: request.PathValue(name)})
	}
	return values
}

// apiErrorKind is the kind of thing a not-found code names, in words:
// LEAFEN_DATABASE_ROW_PAGE_NOT_FOUND is a "database row page".
func apiErrorKind(code string) string {
	words := strings.TrimPrefix(code, "LEAFEN_")
	for _, suffix := range []string{"_NOT_FOUND", "_MISSING"} {
		if trimmed, cut := strings.CutSuffix(words, suffix); cut {
			return strings.ToLower(strings.ReplaceAll(trimmed, "_", " "))
		}
	}
	return ""
}

// apiKindWords are a kind's words, lower case, the last one singular:
// "Database rows" is database row.
func apiKindWords(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if last := len(words) - 1; last >= 0 {
		word := words[last]
		switch {
		case strings.HasSuffix(word, "ies"):
			word = strings.TrimSuffix(word, "ies") + "y"
		case slices.ContainsFunc([]string{"sses", "uses", "xes", "ches", "shes"}, func(end string) bool { return strings.HasSuffix(word, end) }):
			word = strings.TrimSuffix(word, "es")
		case strings.HasSuffix(word, "s") && !slices.ContainsFunc([]string{"ss", "us", "is"}, func(end string) bool { return strings.HasSuffix(word, end) }):
			// databases a database, status itself.
			word = strings.TrimSuffix(word, "s")
		}
		words[last] = word
	}
	return words
}

// apiPathNameWords are the kind a path value's name says: installId an
// install, dataSourceId a data source; a name that says only what sort of
// value it is (id, key, name, token, hash) says none.
func apiPathNameWords(name string) []string {
	spaced := strings.Builder{}
	for index, r := range name {
		if index > 0 && unicode.IsUpper(r) {
			spaced.WriteByte(' ')
		}
		spaced.WriteRune(r)
	}
	words := apiKindWords(spaced.String())
	for len(words) > 0 && slices.Contains([]string{"id", "key", "name", "token", "hash", "number", "slug"}, words[len(words)-1]) {
		words = words[:len(words)-1]
	}
	return words
}

// apiKindsAlike are the words one kind of path value goes by elsewhere: an
// install is the app installed, a slug an app's address, a data source what
// the messages call a database, and /collaboration-status/{room} a page's room.
var apiKindsAlike = map[string][]string{
	"install":              {"app"},
	"app home screen":      {"app"},
	"org":                  {"organization"},
	"data source":          {"database"},
	"collaboration status": {"page"},
}

// apiRouteValueForms are the kinds a path value is named by: its name's, its
// segment's (/database-rows/{rowId} a database row) and their other words.
func apiRouteValueForms(value apiRouteValue) [][]string {
	forms := [][]string{}
	for _, form := range [][]string{apiPathNameWords(value.Name), apiKindWords(strings.ReplaceAll(value.Before, "-", " "))} {
		if len(form) == 0 {
			continue
		}
		forms = append(forms, form)
		for _, alike := range apiKindsAlike[strings.Join(form, " ")] {
			forms = append(forms, strings.Fields(alike))
		}
	}
	return forms
}

// apiRouteValueKind is the one kind a path value is: its segment's where that
// says the same thing more fully (/database-rows/{rowId} a database row),
// else its name's (/databases/{dataSourceId} a data source).
func apiRouteValueKind(value apiRouteValue) string {
	name := apiPathNameWords(value.Name)
	segment := apiKindWords(strings.ReplaceAll(value.Before, "-", " "))
	if len(name) == 0 || len(segment) == 0 || apiWordsEndWith(segment, name) {
		if len(segment) == 0 {
			return strings.Join(name, " ")
		}
		return strings.Join(segment, " ")
	}
	return strings.Join(name, " ")
}

func apiWordsEndWith(words, end []string) bool {
	return len(end) > 0 && len(end) <= len(words) && slices.Equal(words[len(words)-len(end):], end)
}

func apiWordsStartWith(words, start []string) bool {
	return len(start) > 0 && len(start) <= len(words) && slices.Equal(words[:len(start)], start)
}

// apiKindNames says whether words naming a kind name a path value's form: the
// same kind, a sort of it ("database data source" names a data source) or it a
// sort of them ("page" names a source page). leading also takes a form the
// words begin ("app" names an app password): only where nothing names more
// closely.
func apiKindNames(kind, form []string, leading bool) bool {
	if leading {
		return apiWordsStartWith(form, kind)
	}
	return apiWordsEndWith(kind, form) || apiWordsEndWith(form, kind)
}

var (
	apiMessageNotFound = regexp.MustCompile(`^(?:the |this )?([a-z][a-z ]{0,48}?) (?:was |is )?not found\b`)
	apiMessageNoSuch   = regexp.MustCompile(`^no such ([a-z]+)\b`)
)

// apiMessageKind is the kind a message says wasn't found: "Row not found." a
// row, "No such hook." a hook. One naming two ("Wiki or page not found.") or
// none names nothing.
func apiMessageKind(message string) []string {
	text := strings.ToLower(strings.TrimSpace(message))
	for _, pattern := range []*regexp.Regexp{apiMessageNotFound, apiMessageNoSuch} {
		if found := pattern.FindStringSubmatch(text); found != nil {
			kind := apiKindWords(found[1])
			if slices.Contains(kind, "or") || len(kind) > 4 {
				return nil
			}
			return kind
		}
	}
	return nil
}

// apiNotFoundTarget is what a 404 didn't find: the innermost path value the
// message's kind names, else the code's; else a body field naming it (a page
// a body names by pageId); a sort the words begin only where none is named
// more closely. Its kind is the code's where the code names it (the words
// every route of that code answers with), else the value's own. Words that
// name nothing there leave what the route looked up last, the innermost path
// value, by its own kind — a family's code (LEAFEN_ACCESS_REQUEST_NOT_FOUND)
// on /workspaces/{workspaceId}/access-requests didn't find the workspace —
// unless they name a workspace or organization the path doesn't hold.
func apiNotFoundTarget(event *core.RequestEvent, code, message string) *apiErrorTarget {
	request := event.Request
	if code == errorCodeAPIRouteNotFound {
		return &apiErrorTarget{Kind: "operation", ID: request.Method + " " + strings.TrimPrefix(request.URL.Path, apiRoot)}
	}
	values := apiRouteValues(request)
	if sent, kept := event.Get(apiSentPathValuesKey).(map[string]string); kept {
		for index := range values {
			if value, given := sent[values[index].Name]; given {
				values[index].Value = value
			}
		}
	}
	codeKind := apiErrorKind(code)
	if code == errorCodeNotFound {
		codeKind = ""
	}
	kinds := [][]string{}
	if kind := apiMessageKind(message); len(kind) > 0 {
		kinds = append(kinds, kind)
	}
	if codeKind != "" {
		kinds = append(kinds, apiKindWords(codeKind))
	}
	names := func(kind []string, value apiRouteValue, leading bool) bool {
		return slices.ContainsFunc(apiRouteValueForms(value), func(form []string) bool { return apiKindNames(kind, form, leading) })
	}
	said := func(value apiRouteValue) string {
		if codeKind != "" && names(apiKindWords(codeKind), value, false) {
			return codeKind
		}
		return apiRouteValueKind(value)
	}
	for _, leading := range []bool{false, true} {
		for _, kind := range kinds {
			for index := len(values) - 1; index >= 0; index-- {
				if names(kind, values[index], leading) {
					return &apiErrorTarget{Kind: said(values[index]), ID: values[index].Value}
				}
			}
		}
		if leading {
			break
		}
		if target := apiNotFoundBodyTarget(event, kinds, said); target != nil {
			return target
		}
	}
	if slices.ContainsFunc(kinds, func(kind []string) bool {
		return slices.Equal(kind, []string{"workspace"}) || slices.Equal(kind, []string{"organization"})
	}) || len(values) == 0 {
		if codeKind == "" {
			return nil
		}
		return &apiErrorTarget{Kind: codeKind}
	}
	innermost := values[len(values)-1]
	return &apiErrorTarget{Kind: apiRouteValueKind(innermost), ID: innermost.Value}
}

// apiNotFoundBodyTarget is the body's field the words name, where the path
// holds none: {"pageId": …} for a page. A requestId is the call's own key for
// sending it again, never a thing looked up.
func apiNotFoundBodyTarget(event *core.RequestEvent, kinds [][]string, said func(apiRouteValue) string) *apiErrorTarget {
	if len(kinds) == 0 || event.Request.Method == http.MethodGet || event.Request.Method == http.MethodHead {
		return nil
	}
	body, read := apiErrorRequestBody(event)
	if !read {
		return nil
	}
	for _, kind := range kinds {
		for _, name := range slices.Sorted(maps.Keys(body)) {
			value, text := body[name].(string)
			field := apiRouteValue{Name: name}
			if !text || strings.TrimSpace(value) == "" || name == "requestId" || len(apiPathNameWords(name)) == 0 ||
				!slices.ContainsFunc(apiRouteValueForms(field), func(form []string) bool { return apiKindNames(kind, form, false) }) {
				continue
			}
			return &apiErrorTarget{Kind: said(field), ID: value}
		}
	}
	return nil
}
