package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A renamed property keeps the last few names it had, so a formula written
// from an editor that hadn't yet seen the rename still reads it: a reference
// that names no property of the data source, by name or ID, reads the one
// property that last had that name.

const (
	databasePropertyFormerNamesKey = "formerNames"
	maxDatabasePropertyFormerNames = 3
)

func databasePropertyFormerNames(config map[string]any) []string {
	values, _ := config[databasePropertyFormerNamesKey].([]any)
	names := make([]string, 0, len(values))
	for _, value := range values {
		if name, ok := value.(string); ok && name != "" {
			names = append(names, name)
		}
	}
	return names
}

// recordDatabasePropertyFormerNames adds, to each property the operations
// rename, the name it had, and keeps the former names of a property whose
// upsert doesn't carry them, as a client that doesn't know them sends it.
func recordDatabasePropertyFormerNames(current map[string]databasePropertyAuthority, operations []graphOperation) error {
	for index, operation := range operations {
		property := operation.Property
		if operation.Kind != graphOperationKindUpsertProperty || property == nil {
			continue
		}
		entity, exists := current[property.ID]
		if !exists {
			continue
		}
		config := map[string]any{}
		if err := json.Unmarshal(property.Config, &config); err != nil {
			return err
		}
		_, sent := config[databasePropertyFormerNamesKey]
		renamed := entity.Name != property.Name
		previous := databasePropertyFormerNames(config)
		if !sent {
			previous = databasePropertyFormerNames(entity.Config)
		}
		if !renamed && (sent || len(previous) == 0) {
			continue
		}
		if renamed {
			previous = append([]string{entity.Name}, previous...)
		}
		names := []any{}
		seen := map[string]bool{property.Name: true}
		for _, name := range previous {
			if seen[name] || len(names) == maxDatabasePropertyFormerNames {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
		config[databasePropertyFormerNamesKey] = names
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

// resolveDatabaseFormulaFormerNames rewrites each reference of the formulas
// the operations write that names no property of their data source to the
// property that last had that name, when exactly one did.
func resolveDatabaseFormulaFormerNames(
	app core.App,
	current map[string]databasePropertyAuthority,
	operations []graphOperation,
) error {
	writes := []int{}
	sources := map[string]bool{}
	for index, operation := range operations {
		if property := operation.Property; operation.Kind == graphOperationKindUpsertProperty && property != nil &&
			property.Type == graphValueTypeFormula {
			writes = append(writes, index)
			sources[property.DataSourceID] = true
		}
	}
	if len(writes) == 0 {
		return nil
	}
	// The data sources as the operations leave them.
	after := map[string]databaseFormulaReferenceProperty{}
	for id, entity := range current {
		if sources[entity.DataSourceID] {
			after[id] = databaseFormulaReferenceProperty{
				ID: id, DataSourceID: entity.DataSourceID, Type: entity.Type, Name: entity.Name,
				Position: entity.Position, Config: entity.Config, Stored: true,
			}
		}
	}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil:
			property := operation.Property
			if !sources[property.DataSourceID] {
				delete(after, property.ID)
				continue
			}
			config := map[string]any{}
			if err := json.Unmarshal(property.Config, &config); err != nil {
				return err
			}
			_, stored := current[property.ID]
			after[property.ID] = databaseFormulaReferenceProperty{
				ID: property.ID, DataSourceID: property.DataSourceID, Type: property.Type, Name: property.Name,
				Position: property.Position, Config: config, Stored: stored,
			}
		case operation.Kind == graphOperationKindDeleteProperty && operation.PropertyDeletion != nil:
			delete(after, operation.PropertyDeletion.ID)
		}
	}
	trashed, err := databaseFormulaReferenceTrash(app, after)
	if err != nil {
		return err
	}
	for _, index := range writes {
		property := operations[index].Property
		live := []databaseFormulaReferenceProperty{}
		formerOwners := map[string][]string{}
		for _, candidate := range after {
			if _, deleted := trashed[candidate.ID]; deleted || candidate.DataSourceID != property.DataSourceID {
				continue
			}
			live = append(live, candidate)
			for _, name := range databasePropertyFormerNames(candidate.Config) {
				formerOwners[name] = append(formerOwners[name], candidate.ID)
			}
		}
		names := newDatabaseFormulaReferenceNames(live, nil)
		config := map[string]any{}
		if err := json.Unmarshal(property.Config, &config); err != nil {
			return err
		}
		expression, _ := config["formula"].(string)
		rewritten, changed := rewriteDatabaseFormulaReferences(expression, func(reference string) (string, bool) {
			if _, known := names.resolve(reference); known || len(formerOwners[reference]) != 1 {
				return "", false
			}
			return names.canonical(formerOwners[reference][0])
		})
		if !changed {
			continue
		}
		config["formula"] = rewritten
		encoded, err := json.Marshal(config)
		if err != nil {
			return err
		}
		next := *property
		next.Config = encoded
		operations[index].Property = &next
	}
	return nil
}

// graphOperationsWriteProperties reports a change that writes or deletes a
// property.
func graphOperationsWriteProperties(operations []graphOperation) bool {
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil ||
			operation.Kind == graphOperationKindDeleteProperty && operation.PropertyDeletion != nil {
			return true
		}
	}
	return false
}
