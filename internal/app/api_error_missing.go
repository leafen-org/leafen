package app

import (
	"net/http"
	"slices"
	"strings"
	"unicode"

	"github.com/pocketbase/pocketbase/core"
)

// An agent's request refused while it lacks an input its operation needs is
// told that input and its form, and one refused for a value or key it
// doesn't take is told that field and what it takes, as the refusal's
// fields name them, whatever else the route said (2026-11-26,
// real agents: a member added without a role was told "Check the member
// email.", and a page's version restored without baseRevision was told the
// page had changed, a 409 to load it again; neither named what was missing;
// a member added with role "admin" was told "Check the member email." while
// its fields named role).
// What an operation needs is what its handler refuses a request without
// (scripts/native-operation-inputs, the same description leafen_operations
// gives), so the refusal comes as before — only its words name the cause:
// a 400 with the route's code (LEAFEN_INVALID_INPUT for a conflict, which it
// wasn't), the inputs in its message and, as for every 400, in its fields
// (api_error_details.go). A 400 whose message names them all is the route's
// own, and what one says of a field in particular ("Check the request:
// principalType is user or email") follows them. The screen's own requests
// and an app's keep the route's answer: they send what the screen sends.

// apiErrorSaysMissing is a refusal's status, code and message for an
// agent's request that lacks an input its operation needs, or sends a value
// or key it doesn't take (api_error_wrong_values.go).
func apiErrorSaysMissing(event *core.RequestEvent, status int, code, message string) (int, string, string) {
	if status != http.StatusBadRequest && status != http.StatusConflict && status != http.StatusUnprocessableEntity {
		return status, code, message
	}
	if event == nil || event.Request == nil || !strings.HasPrefix(event.Request.URL.Path, apiRoot+"/") {
		return status, code, message
	}
	if !apiErrorAgentRequest(event) {
		return status, code, message
	}
	input, known := apiOperationInputOf(event.Request)
	if !known {
		return status, code, message
	}
	words := strings.FieldsFunc(message, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	names := func(name string) bool { return slices.Contains(words, name[strings.LastIndexAny(name, ".?")+1:]) }
	fields := apiErrorFields(event)
	missing, named := []string{}, true
	for _, field := range fields {
		if field.Problem == apiErrorFieldMissing {
			missing = append(missing, field.Name+" ("+apiOperationInputForm(input, field.Name)+")")
			named = named && names(field.Name)
		}
	}
	wrong, wrongNamed := apiErrorWrongParts(event, input, fields)
	for _, field := range wrong {
		wrongNamed = wrongNamed && names(field.Name)
	}
	// A route's refusal that names them already says it in its own words. A
	// conflict for a request lacking what it needs, or sending a value its
	// handler turns away, is the request's to fix, not a change made
	// meanwhile (2026-11-26: an account suspended with status "suspended",
	// not one of active and disabled, was told the account had changed).
	if len(missing) == 0 && (len(wrong) == 0 || wrongNamed) ||
		len(missing) > 0 && named && (len(wrong) == 0 || wrongNamed) && status == http.StatusBadRequest {
		return status, code, message
	}
	said := strings.TrimSpace(apiErrorMissingSentence(missing) + " " + apiErrorWrongSentence(wrong))
	if status == http.StatusConflict {
		return http.StatusBadRequest, errorCodeInvalidInput, said
	}
	// What a route's refusal says of a field in particular goes after.
	if strings.Contains(message, ": ") {
		said += " " + message
	}
	if len(missing) == 0 {
		return status, code, said
	}
	return http.StatusBadRequest, code, said
}

// apiErrorAgentRequest tells a request an agent's key makes — not an app's
// — known by its key, or, refused while its key is checked (a place the key
// must name, user_key_place_refusal.go), by the MCP call it comes from.
func apiErrorAgentRequest(event *core.RequestEvent) bool {
	key, keyed := userKeyOf(event)
	if !keyed {
		key, keyed = event.Request.Context().Value(userKeyInternalContextKey{}).(userKeyAuthority)
	}
	return keyed && key.AppInstallID == "" && !key.AppAccount
}

// apiErrorMissingSentence names the inputs a request lacks, with their forms.
func apiErrorMissingSentence(missing []string) string {
	if len(missing) == 0 {
		return ""
	}
	return "Missing what this operation needs: " + strings.Join(missing, "; ") + "."
}

// apiOperationInputForm is the form of an input a refusal names: a query
// name (?kind), a body's field by its dotted path (scope.targetId,
// items[].id), or a map's key (options.includeFiles).
func apiOperationInputForm(input apiOperationInput, name string) string {
	if query, isQuery := strings.CutPrefix(name, "?"); isQuery {
		for _, field := range input.Query {
			if field.Name == query {
				return apiInputFieldForm(input, field)
			}
		}
		return "text"
	}
	fields := slices.Clone(input.Records[input.Record])
	for _, record := range input.Actions {
		fields = append(fields, input.Records[record]...)
	}
	parts := strings.Split(name, ".")
	for at, part := range parts {
		part = strings.TrimSuffix(part, "[]")
		index := slices.IndexFunc(fields, func(field nativeOperationInputField) bool { return field.Name == part })
		if index < 0 {
			return "see leafen_operations"
		}
		field := fields[index]
		if at == len(parts)-1 {
			return apiInputFieldForm(input, field)
		}
		// A map's key holds its value's form.
		if value, isMap := strings.CutPrefix(field.Type, "map of "); isMap && at == len(parts)-2 {
			return value
		}
		fields = input.Records[apiInputRecordOf(field.Type)]
	}
	return "see leafen_operations"
}

// apiInputFieldForm says an input's type with what it takes: its values, a
// map's keys and those it needs, or a record's fields, those it needs marked.
func apiInputFieldForm(input apiOperationInput, field nativeOperationInputField) string {
	form := field.Type
	cut := func(values []string) string {
		if len(values) > apiErrorFieldsMax {
			values = append(values[:apiErrorFieldsMax:apiErrorFieldsMax], "…")
		}
		return strings.Join(values, ", ")
	}
	// A record's first fields and those it needs, as a description folds it.
	fold := func(names []string, needed []string) string {
		if shown := nativeOperationHeldFieldsShown / 2; len(names) > shown {
			first := names[:shown:shown]
			for _, name := range needed {
				if !slices.Contains(first, name) {
					first = append(first, name)
				}
			}
			names = append(first, "…")
		}
		return strings.Join(names, ", ")
	}
	switch record := input.Records[apiInputRecordOf(field.Type)]; {
	case len(field.Values) > 0:
		form += ": one of " + cut(field.Values)
	case len(field.Keys) > 0:
		form += ": keys " + cut(field.Keys)
		if len(field.RequiredKeys) > 0 {
			form += ", needing " + cut(field.RequiredKeys)
		}
	case len(record) > 0:
		names, needed := []string{}, []string{}
		for _, held := range record {
			if held.Required {
				names, needed = append(names, held.Name+" (needed)"), append(needed, held.Name+" (needed)")
			} else {
				names = append(names, held.Name)
			}
		}
		form += " with " + fold(names, needed)
	}
	return form
}

// apiInputRecordOf is the record a field's type names, a list's or a
// pointer's too.
func apiInputRecordOf(fieldType string) string {
	return strings.TrimSuffix(strings.TrimPrefix(fieldType, "*"), "[]")
}
