package app

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A formula names the properties it reads, as prop("Name"), but means the
// property itself: in Notion a saved formula refers to the property's ID, so
// renaming the property keeps the formula working and shows the new name.
// The mutation that renames, reorders, adds or removes a data source's
// properties rewrites each formula of that source, and each formula value of
// its database buttons, whose reference would otherwise read another
// property or none. A property whose name another property shares is
// referenced by its ID, which the evaluator also reads, until its name is
// unique again. Deleted properties keep their formulas current so they work
// once restored.

type databaseFormulaReferenceProperty struct {
	ID, DataSourceID, Type, Name, Position string
	Config                                 map[string]any
	Stored                                 bool
}

// databaseFormulaReferenceNames resolves a reference the way the evaluator
// does: the first property with that name in property order, else the
// property with that ID.
type databaseFormulaReferenceNames struct {
	first map[string]string
	count map[string]int
	names map[string]string
}

func newDatabaseFormulaReferenceNames(
	properties []databaseFormulaReferenceProperty,
	trashed map[string]bool,
) databaseFormulaReferenceNames {
	live := make([]databaseFormulaReferenceProperty, 0, len(properties))
	for _, property := range properties {
		if _, deleted := trashed[property.ID]; !deleted {
			live = append(live, property)
		}
	}
	sortDatabaseFormulaReferenceProperties(live)
	names := databaseFormulaReferenceNames{
		first: map[string]string{}, count: map[string]int{}, names: map[string]string{},
	}
	for _, property := range live {
		if _, exists := names.first[property.Name]; !exists {
			names.first[property.Name] = property.ID
		}
		names.count[property.Name]++
		names.names[property.ID] = property.Name
	}
	return names
}

func (names databaseFormulaReferenceNames) resolve(reference string) (string, bool) {
	if id, ok := names.first[reference]; ok {
		return id, true
	}
	_, ok := names.names[reference]
	return reference, ok
}

// canonical is how a formula refers to the property: by its name while no
// other property has it, by its ID otherwise.
func (names databaseFormulaReferenceNames) canonical(id string) (string, bool) {
	name, ok := names.names[id]
	if !ok {
		return "", false
	}
	if names.count[name] == 1 {
		return name, true
	}
	return id, true
}

func sortDatabaseFormulaReferenceProperties(properties []databaseFormulaReferenceProperty) {
	sort.Slice(properties, func(left, right int) bool {
		if properties[left].Position == properties[right].Position {
			return properties[left].ID < properties[right].ID
		}
		return properties[left].Position < properties[right].Position
	})
}

// expandDatabaseFormulaReferenceRenames returns the mutation's operations
// with the formula rewrites its property changes need, and how many
// properties it added.
func expandDatabaseFormulaReferenceRenames(
	app core.App,
	pageID string,
	operations []graphOperation,
) ([]graphOperation, int, error) {
	// A change that writes no property leaves every formula as it reads.
	if !graphOperationsWriteProperties(operations) {
		return operations, 0, nil
	}
	current, err := loadDatabasePropertyRecords(app, pageID)
	if err != nil {
		return nil, 0, err
	}
	operations = append([]graphOperation(nil), operations...)
	if err := recordDatabasePropertyFormerNames(current, operations); err != nil {
		return nil, 0, err
	}
	if err := resolveDatabaseFormulaFormerNames(app, current, operations); err != nil {
		return nil, 0, err
	}
	sources := map[string]bool{}
	upserts := map[string]int{}
	deleted := map[string]bool{}
	for index, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil:
			property := operation.Property
			upserts[property.ID] = index
			entity, exists := current[property.ID]
			source := entity.DataSourceID
			if !exists || entity.Name != property.Name || entity.Position != property.Position ||
				source != property.DataSourceID {
				sources[property.DataSourceID] = true
				if source != "" {
					sources[source] = true
				}
			}
		case operation.Kind == graphOperationKindDeleteProperty && operation.PropertyDeletion != nil:
			id := operation.PropertyDeletion.ID
			deleted[id] = true
			if source := current[id].DataSourceID; source != "" {
				sources[source] = true
			}
		}
	}
	if len(sources) == 0 {
		return operations, 0, nil
	}
	stored := databaseFormulaReferenceStoredProperties(current, sources)
	trashed, err := databaseFormulaReferenceTrash(app, stored)
	if err != nil {
		return nil, 0, err
	}
	expanded := append([]graphOperation(nil), operations...)
	added := 0
	sourceIDs := make([]string, 0, len(sources))
	for id := range sources {
		sourceIDs = append(sourceIDs, id)
	}
	sort.Strings(sourceIDs)
	for _, sourceID := range sourceIDs {
		before := []databaseFormulaReferenceProperty{}
		after := []databaseFormulaReferenceProperty{}
		for _, property := range stored {
			if property.DataSourceID != sourceID {
				continue
			}
			before = append(before, property)
			if _, changed := upserts[property.ID]; !changed && !deleted[property.ID] {
				after = append(after, property)
			}
		}
		for _, operation := range operations {
			if operation.Kind != graphOperationKindUpsertProperty || operation.Property == nil ||
				operation.Property.DataSourceID != sourceID {
				continue
			}
			// Validation has already made the configuration a JSON object.
			config := map[string]any{}
			if err := json.Unmarshal(operation.Property.Config, &config); err != nil {
				return nil, 0, err
			}
			property := databaseFormulaReferenceProperty{
				ID: operation.Property.ID, DataSourceID: sourceID, Type: operation.Property.Type,
				Name: operation.Property.Name, Position: operation.Property.Position, Config: config,
			}
			_, property.Stored = stored[property.ID]
			after = append(after, property)
		}
		sortDatabaseFormulaReferenceProperties(after)
		beforeNames := newDatabaseFormulaReferenceNames(before, trashed)
		afterNames := newDatabaseFormulaReferenceNames(after, trashed)
		if err := rewriteDatabaseAutomationDefinitionReferences(app, sourceID, beforeNames, afterNames); err != nil {
			return nil, 0, err
		}
		for _, property := range after {
			if trashed[property.ID] {
				continue
			}
			config, changed := rewriteDatabaseFormulaReferenceConfig(
				property, stored[property.ID].Config, beforeNames, afterNames,
			)
			if !changed {
				continue
			}
			encoded, err := json.Marshal(config)
			if err != nil {
				return nil, 0, err
			}
			if index, inMutation := upserts[property.ID]; inMutation {
				rewritten := *expanded[index].Property
				rewritten.Config = encoded
				expanded[index].Property = &rewritten
				continue
			}
			rewritten := graphProperty{
				ID: property.ID, DataSourceID: property.DataSourceID, Type: property.Type,
				Name: property.Name, Position: property.Position, Config: encoded,
			}
			if err := normalizeGraphProperty(&rewritten); err != nil {
				return nil, 0, err
			}
			expanded = append(expanded, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &rewritten})
			added++
		}
	}
	return expanded, added, nil
}

func databaseFormulaReferenceStoredProperties(
	current map[string]databasePropertyAuthority,
	sources map[string]bool,
) map[string]databaseFormulaReferenceProperty {
	properties := map[string]databaseFormulaReferenceProperty{}
	for id, entity := range current {
		if !sources[entity.DataSourceID] {
			continue
		}
		properties[id] = databaseFormulaReferenceProperty{
			ID: id, DataSourceID: entity.DataSourceID, Type: entity.Type, Name: entity.Name,
			Position: entity.Position, Config: entity.Config, Stored: true,
		}
	}
	return properties
}

// databaseFormulaReferenceTrash maps each property in Deleted properties to
// whether it is being deleted for good, which leaves its formula as it is.
func databaseFormulaReferenceTrash(
	app core.App,
	properties map[string]databaseFormulaReferenceProperty,
) (map[string]bool, error) {
	trashed := map[string]bool{}
	if len(properties) == 0 {
		return trashed, nil
	}
	ids := make(map[string]struct{}, len(properties))
	for id := range properties {
		ids[id] = struct{}{}
	}
	encoded, err := databaseMutationIDJSON(ids)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		PropertyID string `db:"property_id"`
		Deleting   int    `db:"deleting"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[property_id]], [[deleting]] FROM {{leafen_database_property_trash}}
		WHERE [[property_id]] IN (SELECT [[value]] FROM json_each({:propertyIds}))
	`).Bind(dbx.Params{"propertyIds": encoded}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		trashed[row.PropertyID] = row.Deleting != 0
	}
	return trashed, nil
}

// rewriteDatabaseFormulaReferenceConfig rewrites a formula property's
// expression or a database button's formula values. An expression this
// mutation writes was written against the new names, and may still use a
// name the mutation changes; a stored one was written against the old names.
func rewriteDatabaseFormulaReferenceConfig(
	property databaseFormulaReferenceProperty,
	stored map[string]any,
	before, after databaseFormulaReferenceNames,
) (map[string]any, bool) {
	keys := []string{}
	switch property.Type {
	case graphValueTypeFormula:
		keys = []string{"formula", "expression"}
	case graphValueTypeButton:
		keys = []string{"button"}
	default:
		return nil, false
	}
	changed := false
	for _, key := range keys {
		value, exists := property.Config[key]
		if !exists {
			continue
		}
		authored := !property.Stored || !databaseFormulaReferenceSameJSON(value, stored[key])
		replace := databaseFormulaReferenceReplacement(authored, before, after)
		if property.Type == graphValueTypeFormula {
			expression, ok := value.(string)
			if !ok {
				continue
			}
			if next, rewritten := rewriteDatabaseFormulaReferences(expression, replace); rewritten {
				property.Config[key] = next
				changed = true
			}
			continue
		}
		if next, rewritten := rewriteDatabaseAutomationFormulaReferences(value, replace); rewritten {
			property.Config[key] = next
			changed = true
		}
	}
	return property.Config, changed
}

func databaseFormulaReferenceSameJSON(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func databaseFormulaReferenceReplacement(
	authored bool,
	before, after databaseFormulaReferenceNames,
) func(string) (string, bool) {
	return func(reference string) (string, bool) {
		current, readsAfter := after.resolve(reference)
		target, ok := current, readsAfter
		if !authored || !readsAfter {
			target, ok = before.resolve(reference)
		}
		if !ok {
			return "", false
		}
		next, exists := after.canonical(target)
		if !exists || readsAfter && current == target && reference != target {
			return "", false
		}
		return next, next != reference
	}
}

// rewriteDatabaseAutomationFormulaReferences rewrites the formula values of
// a button's action document.
func rewriteDatabaseAutomationFormulaReferences(value any, replace func(string) (string, bool)) (any, bool) {
	changed := false
	switch current := value.(type) {
	case map[string]any:
		if expression, ok := current["expression"].(string); ok && current["kind"] == "formula" {
			if next, rewritten := rewriteDatabaseFormulaReferences(expression, replace); rewritten {
				current["expression"] = next
				changed = true
			}
		}
		for key, child := range current {
			if next, rewritten := rewriteDatabaseAutomationFormulaReferences(child, replace); rewritten {
				current[key] = next
				changed = true
			}
		}
	case []any:
		for index, child := range current {
			if next, rewritten := rewriteDatabaseAutomationFormulaReferences(child, replace); rewritten {
				current[index] = next
				changed = true
			}
		}
	}
	return value, changed
}

// rewriteDatabaseFormulaReferences replaces only complete prop("name")
// arguments and keeps every other character of the expression, including the
// quote style and spacing.
func rewriteDatabaseFormulaReferences(expression string, replace func(string) (string, bool)) (string, bool) {
	characters := []rune(expression)
	var output strings.Builder
	cursor, changed := 0, false
	for _, call := range databaseFormulaPropCalls(characters) {
		next, replaced := replace(call.Name)
		if !replaced {
			continue
		}
		quote := string(characters[call.ArgumentStart])
		output.WriteString(string(characters[cursor:call.ArgumentStart]))
		output.WriteString(quote)
		output.WriteString(strings.ReplaceAll(strings.ReplaceAll(next, `\`, `\\`), quote, `\`+quote))
		output.WriteString(quote)
		cursor = call.ArgumentEnd
		changed = true
	}
	if !changed {
		return expression, false
	}
	output.WriteString(string(characters[cursor:]))
	return output.String(), true
}

// databaseFormulaPropCall is a complete prop("name") call of an expression:
// the name it reads, and where, in runes, the call and its quoted argument
// start and end.
type databaseFormulaPropCall struct {
	Name                                   string
	Start, ArgumentStart, ArgumentEnd, End int
}

// databaseFormulaPropCalls finds the complete prop("name") calls of an
// expression the way the evaluator reads them.
func databaseFormulaPropCalls(characters []rune) []databaseFormulaPropCall {
	calls := []databaseFormulaPropCall{}
	skipSpace := func(position int) int {
		for position < len(characters) && unicode.IsSpace(characters[position]) {
			position++
		}
		return position
	}
	for index := 0; index < len(characters); {
		current := characters[index]
		if current == '"' || current == '\'' {
			_, end, ok := databaseFormulaStringLiteral(characters, index)
			if !ok {
				break
			}
			index = end
			continue
		}
		// A prop() in a comment is not read, nor a quote in one.
		if end := databaseFormulaCommentEnd(characters, index); end >= 0 {
			index = end
			continue
		}
		if !databaseFormulaIdentifierRune(current, false) {
			index++
			continue
		}
		start := index
		index++
		for index < len(characters) && databaseFormulaIdentifierRune(characters[index], true) {
			index++
		}
		if string(characters[start:index]) != "prop" {
			continue
		}
		previous := start - 1
		for previous >= 0 && unicode.IsSpace(characters[previous]) {
			previous--
		}
		if previous >= 0 && characters[previous] == '.' {
			continue
		}
		open := skipSpace(index)
		if open >= len(characters) || characters[open] != '(' {
			continue
		}
		argument := skipSpace(open + 1)
		value, end, ok := databaseFormulaStringLiteral(characters, argument)
		if !ok {
			continue
		}
		closing := skipSpace(end)
		if closing >= len(characters) || characters[closing] != ')' {
			continue
		}
		calls = append(calls, databaseFormulaPropCall{
			Name: value, Start: start, ArgumentStart: argument, ArgumentEnd: end, End: closing + 1,
		})
		index = closing + 1
	}
	return calls
}

// databaseFormulaStringLiteral reads the quoted string at start the way the
// evaluator does and returns its value and the index after it.
func databaseFormulaStringLiteral(characters []rune, start int) (string, int, bool) {
	if start >= len(characters) || characters[start] != '"' && characters[start] != '\'' {
		return "", 0, false
	}
	quote := characters[start]
	var value strings.Builder
	for index := start + 1; index < len(characters); index++ {
		current := characters[index]
		if current == quote {
			return value.String(), index + 1, true
		}
		if current == '\\' && index+1 < len(characters) {
			index++
			current = characters[index]
		}
		value.WriteRune(current)
	}
	return "", 0, false
}

func databaseFormulaIdentifierRune(value rune, allowDigit bool) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' ||
		allowDigit && value >= '0' && value <= '9'
}
