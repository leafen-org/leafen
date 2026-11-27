package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A two-way relation and its other side name each other. A copy whose
// relation's other side stays outside it relates one way, as Notion's does
// (pageDuplicationRelationsOneWayOutside). Copies made before that kept the
// other side's ID while that side named the original only: a half pair, which
// the pair work took for its own, pointing the other side at the copy and
// leaving the original's pages out of it. The other side now stays with the
// relation that names it back: the pair work writes a side only for the
// relation it names (databaseRelationPairNamesBack), a change naming a side
// another relation pairs with makes its relation one-way
// (withoutDatabaseRelationHalfPairs), and the half pairs made before were
// made one way once, by migration 780, which the schema baseline holds.
// Every page they relate stays.

// mirrorDatabaseRelationPairs is mirrorSameSourceRelationPairs once the
// change's relations naming a side another pairs with relate one way
// (withoutDatabaseRelationHalfPairs); it returns how many did.
func mirrorDatabaseRelationPairs(app core.App, ownerID string, operations []graphOperation) ([]graphOperation, int, error) {
	operations, oneWay, err := withoutDatabaseRelationHalfPairs(app, ownerID, operations)
	if err != nil {
		return nil, 0, err
	}
	operations, err = mirrorSameSourceRelationPairs(app, ownerID, operations)
	return operations, oneWay, err
}

// databaseRelationHalfPairSide reports whether the relation propertyID,
// naming pairID as its other side, would take that side from another: pairID
// is a relation whose own other side, a third relation, names it back.
func databaseRelationHalfPairSide(
	propertyID, pairID string, lookup func(string) (databaseRelationPairProperty, bool),
) bool {
	pair, exists := lookup(pairID)
	if !exists || pair.Type != graphValueTypeRelation || pairID == propertyID {
		return false
	}
	other, _ := pair.Config["relatedPropertyId"].(string)
	if other == "" || other == propertyID || other == pairID {
		return false
	}
	partner, exists := lookup(other)
	if !exists || partner.Type != graphValueTypeRelation {
		return false
	}
	named, _ := partner.Config["relatedPropertyId"].(string)
	return named == pairID
}

// databaseRelationPairTakenElsewhere reports, from what is stored, whether
// the relation propertyID naming pairID would take that side from another
// (databaseRelationHalfPairSide).
func databaseRelationPairTakenElsewhere(app core.App, ownerID, propertyID, pairID string) (bool, error) {
	stored, err := loadDatabaseRelationPairProperties(app, ownerID, []string{pairID})
	if err != nil {
		return false, err
	}
	other, _ := stored[pairID].Config["relatedPropertyId"].(string)
	if other == "" || other == propertyID || other == pairID {
		return false, nil
	}
	partner, err := loadDatabaseRelationPairProperties(app, ownerID, []string{other})
	if err != nil {
		return false, err
	}
	stored[other] = partner[other]
	return databaseRelationHalfPairSide(propertyID, pairID, func(id string) (databaseRelationPairProperty, bool) {
		property, exists := stored[id]
		return property, exists && property.ID != ""
	}), nil
}

// databaseRelationPairNamesBack reports whether the other side the pair work
// writes still names the relation the work is for, on that relation's data
// source; a side naming another relation is not this one's to write.
func databaseRelationPairNamesBack(app core.App, ownerID string, payload databaseRelationPairPayload) (bool, error) {
	pair, config, exists, err := loadDatabaseRelationPairTargetProperty(app, ownerID, payload.PairPropertyID)
	if err != nil || !exists {
		return false, err
	}
	return pair.Type == graphValueTypeRelation && pair.DataSourceID == payload.TargetDataSourceID &&
		config["relatedPropertyId"] == payload.PropertyID &&
		config["relationDatabaseId"] == payload.SourceDataSourceID, nil
}

// withoutDatabaseRelationHalfPairs makes each relation the change writes that
// names, as its other side, a relation another one pairs with relate one way,
// keeping its pages, as a copy's does; it returns how many it changed, which
// the writer's screen reads again. A sprint's relations keep their pairs.
func withoutDatabaseRelationHalfPairs(
	app core.App, ownerID string, operations []graphOperation,
) ([]graphOperation, int, error) {
	written := map[string]databaseRelationPairProperty{}
	claims := map[int]string{}
	claimed := []int{}
	for index, operation := range operations {
		if operation.Kind != graphOperationKindUpsertProperty || operation.Property == nil {
			continue
		}
		property := operation.Property
		config := map[string]any{}
		if len(property.Config) > 0 && json.Unmarshal(property.Config, &config) != nil {
			continue
		}
		written[property.ID] = databaseRelationPairProperty{
			ID: property.ID, DataSourceID: property.DataSourceID, Type: property.Type, Config: config,
		}
		pair := databaseRelationPairOf(property.ID, property.DataSourceID, property.Type, config)
		if pair != nil && !pair.selfPaired(property.ID) {
			claims[index] = pair.PairPropertyID
			claimed = append(claimed, index)
		}
	}
	if len(claims) == 0 {
		return operations, 0, nil
	}
	stored := map[string]databaseRelationPairProperty{}
	wanted := map[string]struct{}{}
	for _, pairID := range claims {
		wanted[pairID] = struct{}{}
	}
	for round := 0; round < 2 && len(wanted) > 0; round++ {
		loaded, err := loadDatabaseRelationPairProperties(app, ownerID, workspaceExportSortedKeys(wanted))
		if err != nil {
			return nil, 0, err
		}
		wanted = map[string]struct{}{}
		for id, property := range loaded {
			stored[id] = property
			if other, _ := property.Config["relatedPropertyId"].(string); other != "" {
				if _, known := stored[other]; !known {
					wanted[other] = struct{}{}
				}
			}
		}
	}
	lookup := func(id string) (databaseRelationPairProperty, bool) {
		if property, exists := written[id]; exists {
			return property, true
		}
		property, exists := stored[id]
		return property, exists
	}
	changed := 0
	result := operations
	for _, index := range claimed {
		property := operations[index].Property
		if !databaseRelationHalfPairSide(property.ID, claims[index], lookup) {
			continue
		}
		if managed, err := databaseSprintManagedProperty(app, property.DataSourceID, property.ID); err != nil {
			return nil, 0, err
		} else if managed != "" {
			continue
		}
		config := written[property.ID].Config
		delete(config, "relatedPropertyId")
		delete(config, "reciprocalName")
		encoded, err := json.Marshal(config)
		if err != nil {
			return nil, 0, err
		}
		if changed == 0 {
			result = append([]graphOperation(nil), operations...)
		}
		next := *property
		next.Config = encoded
		result[index].Property = &next
		changed++
	}
	return result, changed, nil
}
