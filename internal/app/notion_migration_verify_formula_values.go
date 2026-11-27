package app

import (
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// notionMigrationVerifyComputing computes a data source's formulas and
// rollups for the check (notion_migration_verify_formulas.go), as the migration's person
// reads its rows.
type notionMigrationVerifyComputing struct {
	principal  string
	contexts   *databaseQueryContextMemo
	properties []storedNotionDatabaseRowPageProperty
	byID       map[string]storedNotionDatabaseRowPageProperty
	// clock are the formulas reading the clock or the page's own ID, by
	// themselves or through another formula of the data source.
	clock map[string]bool
}

func newNotionMigrationVerifyComputing(app core.App, principal string, row notionMigrationVerifyFormulaRow) (*notionMigrationVerifyComputing, error) {
	computing := &notionMigrationVerifyComputing{
		principal: principal, contexts: &databaseQueryContextMemo{},
		byID: map[string]storedNotionDatabaseRowPageProperty{}, clock: map[string]bool{},
	}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[name]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]]={:source} AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"source": row.DataSourceID}).All(&computing.properties); err != nil {
		return nil, err
	}
	withNotionFormulaResults(computing.properties)
	byName, reads := map[string]string{}, map[string][]string{}
	for _, property := range computing.properties {
		computing.byID[property.ID] = property
		if _, taken := byName[property.Name]; !taken {
			byName[property.Name] = property.ID
		}
	}
	for _, property := range computing.properties {
		if property.Type != graphValueTypeFormula {
			continue
		}
		config, err := decodeDatabaseComputedConfig(property.Config)
		if err != nil {
			continue
		}
		expression := cmpOr(config.Formula, config.Expression)
		root, err := parseDatabaseFormula(expression)
		if err != nil {
			continue
		}
		var visit func(*databaseFormulaNode)
		visit = func(node *databaseFormulaNode) {
			if node == nil {
				return
			}
			if (node.kind == databaseFormulaNodeCall || node.kind == databaseFormulaNodeMethod) && notionMigrationVerifyClockFunctions[node.text] {
				computing.clock[property.ID] = true
			}
			for _, child := range node.children {
				visit(child)
			}
		}
		visit(root)
		for _, call := range databaseFormulaPropCalls([]rune(expression)) {
			if id, found := byName[call.Name]; found {
				reads[property.ID] = append(reads[property.ID], id)
			} else if _, found := computing.byID[call.Name]; found {
				reads[property.ID] = append(reads[property.ID], call.Name)
			}
		}
	}
	// One reading such a formula reads the clock too.
	for changed := true; changed; {
		changed = false
		for id, read := range reads {
			for _, other := range read {
				if computing.clock[other] && !computing.clock[id] {
					computing.clock[id], changed = true, true
				}
			}
		}
	}
	return computing, nil
}

// compared says whether the check compares a formula's or a rollup's
// values: not a formula reading the clock or the page's own ID, nor a
// rollup showing its related values as they are.
func (computing *notionMigrationVerifyComputing) compared(propertyID string) bool {
	property := computing.byID[propertyID]
	if property.Type == graphValueTypeRollup {
		config, err := decodeDatabaseComputedConfig(property.Config)
		function := notionRollupReadFunction(config)
		return err == nil && function != "show_original" && function != "show_unique"
	}
	return !computing.clock[propertyID]
}

// values are a row's formulas and rollups as a row page computes them; true
// when its related rows are more than a row page reads at once.
func (computing *notionMigrationVerifyComputing) values(app core.App, row notionMigrationVerifyFormulaRow) (map[string]databaseComputedValue, bool, error) {
	return loadDatabaseRowPageComputedValuesFor(app, computing.principal, databaseQueryMentions{}, databaseRowPageAuthority{
		RowID: row.RowID, OwnerID: row.Owner, WorkspaceID: row.WorkspaceID, ContainerPageID: row.PageID, DataSourceID: row.DataSourceID,
	}, computing.properties, computing.contexts, nil)
}

// shown is a formula's or a rollup's value as the check says Notion's: as
// Notion's API gives it (notionDatabaseRowPageFormulaValue,
// notionDatabaseRowPageRollupValue), in the words a run keeps of what Notion
// showed (notionImportShownText); false for one the API gives no value of.
func (computing *notionMigrationVerifyComputing) shown(propertyID string, value databaseComputedValue) (string, bool, error) {
	property := computing.byID[propertyID]
	project := notionDatabaseRowPageFormulaValue
	if property.Type == graphValueTypeRollup {
		project = notionDatabaseRowPageRollupValue
	}
	projected, err := project(property, value, false)
	if errors.Is(err, errNotionDatabaseRowPageProjectionInvalid) || err == nil && projected["type"] == "unsupported" {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		return "", false, err
	}
	text, _, _ := notionImportFittedCell(notionImportShownText(notionProviderProperty{Type: property.Type, Settings: encoded}), nil, false)
	return text, true, nil
}
