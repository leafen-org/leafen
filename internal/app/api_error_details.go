package app

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// An API error names its cause where the request shows it, so whoever sent
// it can change what they sent rather than guess (2026-11-16, an agent
// calling every operation: a missing group, member or invitation answered
// "workspace not found", and "Check the views." named no field). The one
// writer of errors (writeAPIErrorWithReference) adds them to every route's
// answer alike:
//
//   - target, on a 404: what wasn't found — its kind, from the error's
//     message or code (LEAFEN_GROUP_NOT_FOUND is a group), and its ID, from
//     the path value of that kind (groupId, or the value after /groups/;
//     api_error_target.go);
//   - fields, on a 400, 404 or 422: each field of the body the operation
//     doesn't read, takes only some values of (a list's entries too, where
//     its handler refuses any other: closed), or needs and didn't get, each
//     key of a map it doesn't take or needs and didn't get, and each query
//     name it doesn't read, with what it takes — read from what the
//     operation's handler binds and compares (scripts/native-operation-inputs,
//     the same description leafen_operations gives), so it can't say other
//     than the validation does.
//
// A route that knows more says it in its message; these come beside it.

const (
	apiErrorFieldUnknown  = "not read by this operation"
	apiErrorFieldNotOneOf = "not one of the values it takes"
	apiErrorFieldNotAKey  = "not one of the keys it takes"
	apiErrorFieldMissing  = "required"
	// apiErrorFieldsMax caps the fields named, and the names an unknown
	// one lists beside it.
	apiErrorFieldsMax = 20
	// apiErrorBodyBytes is how much of a body is read back to name its fields.
	apiErrorBodyBytes = 64 << 10
)

// apiErrorNamedKey keeps what a route said of its next error itself
// (apiErrorAbout), which the writer gives in place of what it would read.
const apiErrorNamedKey = "leafenAPIErrorNamed"

type apiErrorNamed struct {
	target *apiErrorTarget
	fields []apiErrorField
}

// apiErrorAbout says, for the error a route answers next, what wasn't found
// or which fields are wrong, where the route knows better than its path and
// body show (a key's place named in the body, say).
func apiErrorAbout(event *core.RequestEvent, target *apiErrorTarget, fields ...apiErrorField) {
	event.Set(apiErrorNamedKey, apiErrorNamed{target: target, fields: fields})
}

// apiErrorDetails are an error's target and fields.
func apiErrorDetails(event *core.RequestEvent, status int, code, message string) (*apiErrorTarget, []apiErrorField) {
	if event == nil || event.Request == nil || !strings.HasPrefix(event.Request.URL.Path, apiRoot+"/") {
		return nil, nil
	}
	named, _ := event.Get(apiErrorNamedKey).(apiErrorNamed)
	event.Set(apiErrorNamedKey, nil)
	target, fields := named.target, named.fields
	if target == nil && status == http.StatusNotFound {
		target = apiNotFoundTarget(event, code, message)
	}
	// What the body shows beside what the route named.
	if status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusUnprocessableEntity {
		for _, field := range apiErrorFields(event) {
			if !slices.ContainsFunc(fields, func(given apiErrorField) bool { return given.Name == field.Name }) {
				fields = append(fields, field)
			}
		}
	}
	return target, fields
}

// apiOperationInput is what one operation reads: the body's record and the
// records its fields name, the body's other records by its action, and the
// query names it reads.
type apiOperationInput struct {
	Record  string
	Records map[string][]nativeOperationInputField
	Actions map[string]string
	Query   []nativeOperationInputField
}

var apiOperationInputs = struct {
	sync.Mutex
	router  any
	byShape map[string]string
	inputs  map[string]nativeOperationInput
}{}

// apiOperationInputOf is what the operation a request matched reads, by its
// route's shape and its handler's generated input.
func apiOperationInputOf(request *http.Request) (apiOperationInput, bool) {
	pattern := request.Pattern
	if _, path, method := strings.Cut(pattern, " "); method {
		pattern = path
	}
	if !strings.HasPrefix(pattern, apiRoot+"/") {
		return apiOperationInput{}, false
	}
	nativeOperationsState.Lock()
	group := nativeOperationsState.router
	nativeOperationsState.Unlock()
	if group == nil {
		return apiOperationInput{}, false
	}
	apiOperationInputs.Lock()
	defer apiOperationInputs.Unlock()
	if apiOperationInputs.router != any(group) {
		// As leafen_operations describes them: a body's own mutationId isn't
		// needed, so it's never named missing (native_api_body_mutation_id.go).
		described, err := nativeOperationInputsParsed()
		if err != nil {
			return apiOperationInput{}, false
		}
		byShape := map[string]string{}
		for _, route := range nativeRoutes(group) {
			byShape[nativeOperationShape(route.Method+" "+route.Path)] = nativeOperationHandler(route)
		}
		apiOperationInputs.router, apiOperationInputs.byShape, apiOperationInputs.inputs = group, byShape, described
	}
	handler, known := apiOperationInputs.byShape[nativeOperationShape(request.Method+" "+strings.TrimPrefix(pattern, apiRoot))]
	if !known {
		return apiOperationInput{}, false
	}
	found, described := apiOperationInputs.inputs[handler]
	if !described {
		return apiOperationInput{}, false
	}
	return apiOperationInput{Record: found.Record, Records: found.Records, Actions: found.Actions, Query: found.Query}, true
}

// apiErrorFields are what the request sent that its operation doesn't take
// as sent.
func apiErrorFields(event *core.RequestEvent) []apiErrorField {
	request := event.Request
	input, known := apiOperationInputOf(request)
	if !known {
		return nil
	}
	fields := []apiErrorField{}
	add := func(field apiErrorField) bool {
		if len(fields) >= apiErrorFieldsMax {
			return false
		}
		if len(field.Allowed) > apiErrorFieldsMax {
			field.Allowed = append(field.Allowed[:apiErrorFieldsMax:apiErrorFieldsMax], "…")
		}
		fields = append(fields, field)
		return true
	}
	// The query names it reads.
	// Only where its handler's reading of the query is known.
	if len(input.Query) > 0 {
		names := make([]string, 0, len(input.Query))
		for _, field := range input.Query {
			names = append(names, field.Name)
		}
		sent := request.URL.Query()
		for _, name := range slices.Sorted(func(yield func(string) bool) {
			for name := range sent {
				if !yield(name) {
					return
				}
			}
		}) {
			if !slices.Contains(names, name) && !apiErrorQueryKept(name) {
				add(apiErrorField{Name: "?" + name, Problem: apiErrorFieldUnknown, Allowed: names})
			}
		}
		for _, field := range input.Query {
			value := sent.Get(field.Name)
			if field.Required && !sent.Has(field.Name) {
				add(apiErrorField{Name: "?" + field.Name, Problem: apiErrorFieldMissing, Allowed: field.Values})
			} else if value != "" && field.Closed && !slices.Contains(field.Values, value) {
				add(apiErrorField{Name: "?" + field.Name, Problem: apiErrorFieldNotOneOf, Allowed: field.Values})
			}
		}
	}
	if input.Record == "" || request.Method == http.MethodGet || request.Method == http.MethodHead {
		return fields
	}
	body, read := apiErrorRequestBody(event)
	if !read {
		return fields
	}
	apiErrorRecordFields(input, input.Record, "", body, add)
	return fields
}

// apiErrorQueryKept are query names every route takes beside its own.
func apiErrorQueryKept(name string) bool {
	return name == "fields" || name == "detail"
}

// apiErrorRecordFields checks one object against a record: names it
// doesn't read (beside any its action records read), values outside those
// it takes, what it needs and lacks, and the objects its fields hold.
func apiErrorRecordFields(input apiOperationInput, record, prefix string, body map[string]any, add func(apiErrorField) bool) {
	own := input.Records[record]
	if len(own) == 0 {
		return
	}
	names := []string{}
	byName := map[string]nativeOperationInputField{}
	for _, field := range own {
		names = append(names, field.Name)
		byName[field.Name] = field
	}
	if prefix == "" {
		for _, other := range input.Actions {
			for _, field := range input.Records[other] {
				if _, seen := byName[field.Name]; !seen {
					names = append(names, field.Name)
					byName[field.Name] = field
				}
			}
		}
	}
	slices.Sort(names)
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for name := range body {
			if !yield(name) {
				return
			}
		}
	}) {
		if _, read := byName[name]; !read {
			add(apiErrorField{Name: prefix + name, Problem: apiErrorFieldUnknown, Allowed: names})
		}
	}
	for _, name := range names {
		field := byName[name]
		value, sent := body[name]
		// A value of another type than its own field's (api_error_wrong_types.go);
		// an action's field of the same name may take another.
		if sent && slices.ContainsFunc(own, func(mine nativeOperationInputField) bool { return mine.Name == name }) &&
			!apiErrorValueFits(input, field.Type, value) {
			add(apiErrorField{Name: prefix + name, Problem: apiErrorFieldWrongType, Allowed: []string{field.Type}})
			continue
		}
		switch {
		case !sent || value == nil:
			if field.Required && slices.ContainsFunc(own, func(mine nativeOperationInputField) bool { return mine.Name == name }) {
				add(apiErrorField{Name: prefix + name, Problem: apiErrorFieldMissing, Allowed: field.Values})
			}
		case strings.HasPrefix(field.Type, "map of "):
			// Its keys, and what each holds (api_error_map_entries.go).
			if held, isObject := value.(map[string]any); isObject {
				if len(field.Keys) > 0 {
					apiErrorMapKeys(prefix+name, field, held, add)
				}
				apiErrorMapEntries(input, prefix+name, strings.TrimPrefix(field.Type, "map of "), held, add)
			}
		case len(field.Keys) > 0:
			if held, isObject := value.(map[string]any); isObject {
				apiErrorMapKeys(prefix+name, field, held, add)
			}
		case len(field.Values) > 0:
			// A text, or a list whose every entry takes one of them, where its
			// handler refuses any other (closed): values another check only
			// compares it with may be a few it treats apart.
			if !field.Closed {
				continue
			}
			entries, isList := value.([]any)
			if !isList {
				entries = []any{value}
			}
			for _, entry := range entries {
				if text, isText := entry.(string); isText && !slices.Contains(field.Values, text) {
					add(apiErrorField{Name: prefix + name, Problem: apiErrorFieldNotOneOf, Allowed: field.Values})
					break
				}
			}
		default:
			nested := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(field.Type, "*"), "[]"), "[]")
			if _, isRecord := input.Records[nested]; !isRecord || nested == record {
				continue
			}
			switch held := value.(type) {
			case map[string]any:
				apiErrorRecordFields(input, nested, prefix+name+".", held, add)
			case []any:
				for _, item := range held {
					if object, isObject := item.(map[string]any); isObject {
						apiErrorRecordFields(input, nested, prefix+name+"[].", object, add)
						break
					}
				}
			}
		}
	}
}

// apiErrorMapKeys checks a map's keys against those its field takes: each
// it doesn't take, and each it needs and lacks.
func apiErrorMapKeys(name string, field nativeOperationInputField, held map[string]any, add func(apiErrorField) bool) {
	for _, key := range slices.Sorted(maps.Keys(held)) {
		if !slices.Contains(field.Keys, key) {
			add(apiErrorField{Name: name + "." + key, Problem: apiErrorFieldNotAKey, Allowed: field.Keys})
		}
	}
	for _, key := range field.RequiredKeys {
		if _, sent := held[key]; !sent {
			add(apiErrorField{Name: name + "." + key, Problem: apiErrorFieldMissing})
		}
	}
}

// apiErrorRequestBody reads the request's JSON object back: as it was read
// (api_error_form.go keeps what was), or, unread, as the router keeps it to
// be read again; one that isn't an object names no fields.
func apiErrorRequestBody(event *core.RequestEvent) (map[string]any, bool) {
	request := event.Request
	var raw []byte
	if kept, keeps := event.Get(apiBodyKeptKey).(*apiBodyKept); keeps && (kept.ended || json.Valid(kept.read)) {
		raw = kept.read
	} else if request.Body == nil {
		return map[string]any{}, true
	} else if rereader, rereads := request.Body.(router.Rereader); rereads {
		rereader.Reread()
		read, err := io.ReadAll(io.LimitReader(request.Body, apiErrorBodyBytes+1))
		rereader.Reread()
		if err != nil {
			return nil, false
		}
		raw = read
	} else {
		return nil, false
	}
	if len(raw) > apiErrorBodyBytes {
		return nil, false
	}
	if strings.TrimSpace(string(raw)) == "" {
		return map[string]any{}, true
	}
	body := map[string]any{}
	if json.Unmarshal(raw, &body) != nil {
		return nil, false
	}
	return body, true
}
