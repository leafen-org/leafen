package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// What a refused content change says to fix (request_refusal.go): the
// operation and field as the request names them — operations[0].block.position
// — and why, so an agent calling POST /pages/{id}/content-graph/mutations
// knows its next call from the answer.

// graphOperationPayloadFields are each operation kind's one payload, as the
// request names it.
var graphOperationPayloadFields = map[string]string{
	graphOperationKindUpsertBlock: "block", graphOperationKindMoveBlock: "move",
	graphOperationKindDeleteBlock: "deletion", graphOperationKindUpsertDataSource: "dataSource",
	graphOperationKindDeleteDataSource: "dataSourceDeletion", graphOperationKindUpsertView: "view",
	graphOperationKindUpdateView: "viewUpdate", graphOperationKindDeleteView: "viewDeletion",
	graphOperationKindUpsertProperty: "property", graphOperationKindDeleteProperty: "propertyDeletion",
	graphOperationKindUpsertRow: "row", graphOperationKindRestoreDateCommand: "dateRestore",
	graphOperationKindDeleteRow: "rowDeletion", graphOperationKindConfigureDependency: "dependencyBinding",
	graphOperationKindUpsertDependencyEdge: "dependencyEdge",
	graphOperationKindDeleteDependencyEdge: "dependencyEdgeDeletion",
}

// graphOperationShapeRefusal is why operations[index] isn't one change: a
// kind this route doesn't take, or a payload other than its kind's one.
func graphOperationShapeRefusal(index int, operation graphOperation) error {
	field := graphOperationPayloadFields[operation.Kind]
	if field == "" {
		return graphRefused(errorCodeGraphInvalid, "Unsupported content change.",
			"operations[%d].kind %q isn't one of %s", index, operation.Kind, graphNamesList(graphOperationPayloadFields))
	}
	return graphRefused(errorCodeGraphInvalid, "Check the content change details.",
		"operations[%d] (kind %s) holds %q and no other change", index, operation.Kind, field)
}

// graphNamesList is a set's names, sorted and joined for a refusal.
func graphNamesList[V any](names map[string]V) string {
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	slices.Sort(sorted)
	return strings.Join(sorted, ", ")
}

// graphIDField is a content id the request names field, trimmed, or why it
// can't be one.
func graphIDField(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		return "", graphRefused(errorCodeGraphInvalid, "Check the content ID.", "%s is empty", field)
	case len(value) > maxGraphIdentifierLength:
		return "", graphRefused(errorCodeGraphInvalid, "Check the content ID.",
			"%s is %d bytes, over %d", field, len(value), maxGraphIdentifierLength)
	}
	return value, nil
}

// graphOptionalIDField is graphIDField for an id the request may leave out.
func graphOptionalIDField(field string, value **string) error {
	if *value == nil {
		return nil
	}
	normalized, err := graphIDField(field, **value)
	if err != nil {
		return err
	}
	*value = &normalized
	return nil
}

// graphPositionField is an order place the request names field, or why it
// can't be one. A block's may be left out: it goes after afterId, or last
// under its parent (content_graph_structure.go).
func graphPositionField(field, value string, optional bool) (string, error) {
	value = strings.TrimSpace(value)
	switch {
	case value == "" && optional:
		return "", nil
	case value == "":
		return "", graphRefused(errorCodeGraphInvalid, "Check the content order.",
			"%s is empty: give a place that sorts (as text) among its siblings' — GET /pages/{id}/content-graph reads theirs; one sorting after the last puts it last", field)
	case len(value) > maxGraphPositionLength:
		return "", graphRefused(errorCodeGraphInvalid, "Check the content order.",
			"%s is %d bytes, over %d", field, len(value), maxGraphPositionLength)
	}
	return value, nil
}

// graphNameField is a name of up to maxGraphIdentifierLength characters.
func graphNameField(message, field, value string, required bool) error {
	switch count := utf8.RuneCountInString(value); {
	case value == "" && required:
		return graphRefused(errorCodeGraphInvalid, message, "%s is empty", field)
	case count > maxGraphIdentifierLength:
		return graphRefused(errorCodeGraphInvalid, message, "%s is %d characters, over %d", field, count, maxGraphIdentifierLength)
	}
	return nil
}

// graphValueAt names the value a refusal calls "the value": field, as
// content or config.
func graphValueAt(err error, field string) error {
	var refusal *graphRequestError
	if !errors.As(err, &refusal) || !strings.HasPrefix(refusal.reason, "the value ") {
		return err
	}
	named := *refusal
	named.reason = field + strings.TrimPrefix(refusal.reason, "the value")
	return &named
}

// graphOperationRefusalAt places a normalizer's refusal at its operation.
func graphOperationRefusalAt(err error, index int, field string) error {
	return graphRefusalAt(err, fmt.Sprintf("operations[%d].%s.", index, field))
}

// graphRowValueShape is what a row value of a type holds, as a content
// change writes it (graphrowvalue.value in leafen_operations).
func graphRowValueShape(valueType string) string {
	switch valueType {
	case graphValueTypeTitle, graphValueTypeRichText, graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		return "a string"
	case graphValueTypeSelect, graphValueTypeStatus:
		return "an option's id from the property's config, a string"
	case graphValueTypeNumber:
		return "a number"
	case graphValueTypeUniqueID:
		return "a whole number from 1, written only by the server"
	case graphValueTypeCheckbox:
		return "true or false"
	case graphValueTypeMultiSelect:
		return "a list of option ids"
	case graphValueTypePerson:
		return "a list of user ids"
	case graphValueTypeRelation:
		return "a list of row ids"
	case graphValueTypeDate:
		return `{"start": "YYYY-MM-DD" or "YYYY-MM-DDTHH:MM", "end", "timeZone"}`
	case graphValueTypeVerification:
		return `{"state": "verified" or "unverified", "date"}`
	case graphValueTypePlace:
		return `{"name", "address", "lat", "lon"}`
	case graphValueTypeFiles:
		return graphFilesValueShape
	}
	return "null to clear it"
}

// graphFilesValueShape is what a files value holds, as a write gives it.
const graphFilesValueShape = `a list of {"fileId": an uploaded file's ID in this workspace, its size, type and hash taken from the file; "name" to show another} or {"source": "external", "url", "name"}`
