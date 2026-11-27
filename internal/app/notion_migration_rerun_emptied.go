package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// A cell Notion emptied since the run that last took its row — text, an
// option, a number, a date, a link, a file, a person, a relation — is emptied
// by a run again, as Notion holds it (decision "설계 우선 Notion 이사" 3절
// 원칙 4: what changed during the move comes; 2026-11-24: an emptied number,
// option, date, link or file kept the value Notion no longer had, and a
// relation cut on both sides stayed on both). Notion gives no value of an
// empty cell (notionImportRowValue), so the run tells an emptied one by the
// row as the ledger keeps it (notionMigrationRowChanged). It is a change like
// any other of the source's, so it follows the rule of a changed cell: the
// cell takes the source's value, whatever was changed in it in Leafen since,
// as a cell Notion emptied of its text always did. A cell the source left as
// it was keeps what was changed in it in Leafen; so does a cell whose
// property Notion no longer gives (deleted there), and one whose property a
// person or the plan turned into a type it can't stand for. Relations are
// linked last, and an emptied one there (notion_migration_relation_pairs.go).

// notionMigrationRowTaken is a row's staged values with, beside them, the
// cells the source emptied since as the run that last took them had them.
func notionMigrationRowTaken(values, emptied map[string]notionDatabaseRowValuePlan) map[string]notionDatabaseRowValuePlan {
	if len(emptied) == 0 {
		return values
	}
	taken := make(map[string]notionDatabaseRowValuePlan, len(values)+len(emptied))
	for propertySource, value := range values {
		taken[propertySource] = value
	}
	for propertySource, value := range emptied {
		if _, given := taken[propertySource]; !given {
			taken[propertySource] = value
		}
	}
	return taken
}

// emptyNotionMigrationCell empties among a row's values the cell of a
// property the source emptied, while Notion still gives the property.
func emptyNotionMigrationCell(app core.App, jobID, propertySource, propertyID, propertyType string, values map[string]graphRowValue) error {
	if _, clears := graphClearableValueTypes[propertyType]; !clears {
		return nil
	}
	staged, found, err := notionImportStagedSource(app, jobID, propertySource)
	if err != nil || !found || staged.Kind != "property" {
		return err
	}
	values[propertyID] = graphRowValue{Type: propertyType, Value: graphRowValueCleared}
	return nil
}
