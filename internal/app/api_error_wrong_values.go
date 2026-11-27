package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// What a refusal's message names of a value or key a request sent that its
// operation doesn't take (api_error_missing.go): the fields the route named
// itself (apiErrorAbout — an export's option its format doesn't take), and
// a value outside the list a field takes where its handler refuses any other
// (closed, scripts/native-operation-inputs). A field the operation doesn't
// read, or a map's key read by name only, is left to the fields: the route
// may well have let it be, and refused for something else.

// apiErrorWrongParts are those fields, and whether there are any (as a
// start for the caller's check that the route's message names them all).
func apiErrorWrongParts(event *core.RequestEvent, input apiOperationInput, fields []apiErrorField) ([]apiErrorField, bool) {
	wrong := []apiErrorField{}
	named, _ := event.Get(apiErrorNamedKey).(apiErrorNamed)
	for _, field := range named.fields {
		if field.Problem != apiErrorFieldMissing {
			wrong = append(wrong, field)
		}
	}
	for _, field := range fields {
		if apiErrorFieldListed(wrong, field.Name) {
			continue
		}
		switch field.Problem {
		case apiErrorFieldWrongType:
			// A value of another type is refused wherever its body is read
			// into the operation's record (api_error_wrong_types.go).
			wrong = append(wrong, field)
		case apiErrorFieldNotOneOf:
			if taken, known := apiOperationFieldNamed(input, field.Name); known && taken.Closed {
				wrong = append(wrong, field)
			}
		}
	}
	return wrong, len(wrong) > 0
}

func apiErrorFieldListed(fields []apiErrorField, name string) bool {
	for _, field := range fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

// apiErrorWrongSentence names each such field, what is wrong with it and
// what it takes.
func apiErrorWrongSentence(wrong []apiErrorField) string {
	if len(wrong) == 0 {
		return ""
	}
	parts := make([]string, 0, len(wrong))
	for _, field := range wrong {
		part := field.Name + ": " + field.Problem
		if len(field.Allowed) > 0 {
			part += " (" + strings.Join(field.Allowed, ", ") + ")"
		}
		parts = append(parts, part)
	}
	return "Check " + strings.Join(parts, "; ") + "."
}

// apiOperationFieldNamed is the input a refusal's field names by its dotted
// path (scope.kind, items[].type) or as a query name (?kind).
func apiOperationFieldNamed(input apiOperationInput, name string) (nativeOperationInputField, bool) {
	if query, isQuery := strings.CutPrefix(name, "?"); isQuery {
		for _, field := range input.Query {
			if field.Name == query {
				return field, true
			}
		}
		return nativeOperationInputField{}, false
	}
	fields := append([]nativeOperationInputField{}, input.Records[input.Record]...)
	for _, record := range input.Actions {
		fields = append(fields, input.Records[record]...)
	}
	parts := strings.Split(name, ".")
	for at, part := range parts {
		part = strings.TrimSuffix(part, "[]")
		found := -1
		for index, field := range fields {
			if field.Name == part {
				found = index
				break
			}
		}
		if found < 0 {
			return nativeOperationInputField{}, false
		}
		if at == len(parts)-1 {
			return fields[found], true
		}
		fields = input.Records[apiInputRecordOf(fields[found].Type)]
	}
	return nativeOperationInputField{}, false
}
