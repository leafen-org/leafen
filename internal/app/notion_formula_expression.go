package app

import (
	"encoding/json"
	"net/url"
	"strings"
)

// A formula's references go out through the Public API as Notion writes them:
// prop("Name") while that name reads the property, and otherwise
// {{notion:block_property:<property ID>:<data source ID>:<workspace ID>}}, the
// property named by its public ID URL-encoded, as Notion's (seen 2026-11-09:
// "{{notion:block_property:y%7C%3B%5C:…}}") —
// for a property Leafen refers to by ID because another property shares its
// name, or one whose name holds a quote or a backslash. The workspace is named
// by its UUID, the workspace ID a connection is given with its token; a
// request may also name it by Leafen's own ID. A request may use either form
// of reference; its block_property references become the references Leafen
// stores. A formula a request writes with a block_property reference reads
// back with every reference in that form, its prop("…") ones too, and keeps
// that form through renames and later changes until a request writes it
// again, as Notion's does (seen 2026-11-09 at 2022-06-28, 2025-09-03 and
// 2026-03-11 alike: `{{…}} + prop("Pts") * 2` reads back as `{{…}} + {{…}} * 2`,
// the rest of its text as written; a formula of prop("…") references alone
// reads back as written, under a property's new name once it is renamed).

const notionFormulaBlockPropertyPrefix = "{{notion:block_property:"

// notionFormulaByIDKey marks a formula's settings when a request wrote it
// naming a property by block_property.
const notionFormulaByIDKey = "formulaReferencesByID"

// notionFormulaScope is the data source a formula reads, named as the Public
// API names it.
type notionFormulaScope struct {
	DataSourceID, WorkspaceID, WorkspaceUUID string
	Names                                    databaseFormulaReferenceNames
	IDs                                      notionPropertyIDs
}

func (scope notionFormulaScope) blockProperty(propertyID string) string {
	return notionFormulaBlockPropertyPrefix + scope.IDs.publicID(propertyID) + ":" +
		scope.DataSourceID + ":" + scope.WorkspaceUUID + "}}"
}

// notionFormulaExpression writes a stored formula as the Public API shows it;
// byID names every property it reads by block_property.
func notionFormulaExpression(expression string, scope notionFormulaScope, byID bool) string {
	characters := []rune(expression)
	var output strings.Builder
	cursor := 0
	for _, call := range databaseFormulaPropCalls(characters) {
		id, ok := scope.Names.resolve(call.Name)
		if !ok {
			continue
		}
		name := scope.Names.names[id]
		if !byID && call.Name == name && scope.Names.count[name] == 1 && !strings.ContainsAny(name, `"\`) {
			continue
		}
		output.WriteString(string(characters[cursor:call.Start]))
		output.WriteString(scope.blockProperty(id))
		cursor = call.End
	}
	if cursor == 0 {
		return expression
	}
	output.WriteString(string(characters[cursor:]))
	return output.String()
}

// notionFormulaStoredExpression turns the block_property references of a
// formula a request writes into the prop("…") references Leafen stores; one
// to a property the data source doesn't have stays as written. It reports
// false for a reference to another data source or workspace.
func notionFormulaStoredExpression(expression string, scope notionFormulaScope) (string, bool) {
	if !strings.Contains(expression, notionFormulaBlockPropertyPrefix) {
		return expression, true
	}
	characters := []rune(expression)
	prefix := []rune(notionFormulaBlockPropertyPrefix)
	var output strings.Builder
	cursor := 0
	for index := 0; index < len(characters); {
		if current := characters[index]; current == '"' || current == '\'' {
			_, end, ok := databaseFormulaStringLiteral(characters, index)
			if !ok {
				break
			}
			index = end
			continue
		}
		if index+len(prefix) > len(characters) || string(characters[index:index+len(prefix)]) != notionFormulaBlockPropertyPrefix {
			index++
			continue
		}
		closing := strings.Index(string(characters[index+len(prefix):]), "}}")
		if closing < 0 {
			return "", false
		}
		inner := []rune(string(characters[index+len(prefix):])[:closing])
		parts := strings.Split(string(inner), ":")
		if len(parts) != 3 || !notionFormulaSameID(parts[1], scope.DataSourceID) ||
			parts[2] != scope.WorkspaceID && !notionFormulaSameID(parts[2], scope.WorkspaceUUID) {
			return "", false
		}
		propertyID, named := scope.IDs.internalID(parts[0])
		if !named {
			unescaped, err := url.QueryUnescape(parts[0])
			if err != nil {
				return "", false
			}
			propertyID = unescaped
		}
		reference, ok := scope.Names.canonical(propertyID)
		if !ok {
			// One naming a property the source doesn't have stays as
			// written, as Notion's API keeps it (seen 2026-11-12).
			index += len(prefix) + len(inner) + len("}}")
			continue
		}
		output.WriteString(string(characters[cursor:index]))
		output.WriteString(`prop("` + strings.ReplaceAll(strings.ReplaceAll(reference, `\`, `\\`), `"`, `\"`) + `")`)
		index += len(prefix) + len(inner) + len("}}")
		cursor = index
	}
	output.WriteString(string(characters[cursor:]))
	return output.String(), true
}

func notionFormulaSameID(given, want string) bool {
	normalized, ok := normalizeNotionUUIDRequest(given)
	wanted, wantedOK := normalizeNotionUUIDRequest(want)
	return ok && wantedOK && normalized == wanted
}

// storeNotionFormulaReferences turns the block_property references of the
// formulas the operations write into the references Leafen stores, and marks
// those formulas to read back by block_property, reading the data source as
// scope gives it once the request applies.
func storeNotionFormulaReferences(operations []graphOperation, scope func() (notionFormulaScope, error)) error {
	var loaded *notionFormulaScope
	for index := range operations {
		property := operations[index].Property
		if operations[index].Kind != graphOperationKindUpsertProperty || property == nil ||
			normalizeNotionDatabaseRowPagePropertyType(property.Type) != graphValueTypeFormula {
			continue
		}
		config := map[string]any{}
		if json.Unmarshal(property.Config, &config) != nil {
			continue
		}
		expression, _ := config["formula"].(string)
		if !strings.Contains(expression, notionFormulaBlockPropertyPrefix) {
			continue
		}
		if loaded == nil {
			given, err := scope()
			if err != nil {
				return err
			}
			loaded = &given
		}
		stored, ok := notionFormulaStoredExpression(expression, *loaded)
		if !ok {
			return notionDatabaseRowPageRequestInvalid(
				"database property %q formula names a property this data source doesn't have", property.Name,
			)
		}
		config["formula"] = stored
		config[notionFormulaByIDKey] = true
		encoded, err := json.Marshal(config)
		if err != nil {
			return err
		}
		rewritten := *property
		rewritten.Config = encoded
		operations[index].Property = &rewritten
	}
	return nil
}
