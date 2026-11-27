package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A formula a run makes reads, in place of each property of its database,
// the column the run makes for that property, by the name it makes it under
// (2026-11-24: a merged database's formula, made in its target, read its
// properties by their Notion names where the plan had named their columns
// otherwise, and a formula reading a relation the plan gathers into another
// read a name no column had — 0 and blank where Notion showed values). A
// property of the database's own the plan renames gets its new name later,
// with the formulas reading it (restructureNotionDatabase); a split's target
// reads its own columns there (notion_migration_split_columns.go), as here a
// relation gathered into another reads the column it is gathered into.

// planned gives a staged property the import makes in its own database or a
// merge's target its options as the plan leaves them (plannedOptions) and, a
// formula, the expression reading the run's columns (plannedFormula).
func (run *notionMigrationRunPlan) planned(app core.App, jobID string, source storedNotionSource) (storedNotionSource, error) {
	source, err := run.plannedOptions(app, jobID, source)
	if err != nil {
		return source, err
	}
	return run.plannedFormula(app, jobID, source)
}

// plannedFormula gives a staged formula property the import makes in its
// own database or a merge's target the expression that reads the columns
// the run makes: a merged database's properties by the names the plan
// leaves them in the target — a formula or rollup kept beside the target's
// of its name by the name it has there (notion_migration_merge_computed.go)
// — a gathered relation by its role's column. A name no property of the
// database has is left as it is.
func (run *notionMigrationRunPlan) plannedFormula(app core.App, jobID string, source storedNotionSource) (storedNotionSource, error) {
	if run == nil {
		return source, nil
	}
	plan := notionDatabasePropertyPlan{}
	if err := json.Unmarshal([]byte(source.Text), &plan); err != nil {
		return source, err
	}
	expression, _ := plan.Config["formula"].(string)
	if plan.Type != graphValueTypeFormula || strings.TrimSpace(expression) == "" {
		return source, nil
	}
	into, merged := run.mergeTarget(source.ParentSourceID)
	properties, err := notionSplitProperties(app, jobID, source.ParentSourceID)
	if err != nil {
		return source, err
	}
	byName, byKey := map[string]storedNotionSource{}, map[string]storedNotionSource{}
	for _, property := range properties {
		if _, taken := byName[property.Title]; !taken {
			byName[property.Title] = property
		}
		_, propertyID, _ := strings.Cut(property.SourceID, ":property:")
		byKey[notionMigrationPropertyKey(propertyID)] = property
	}
	names := map[string]string{}
	for _, call := range databaseFormulaPropCalls([]rune(expression)) {
		read, found := byName[call.Name]
		if _, done := names[call.Name]; done || !found {
			continue
		}
		if role, gathered := run.roleColumn(read.SourceID); gathered {
			_, roleID, _ := strings.Cut(role, ":property:")
			if read, found = byKey[notionMigrationPropertyKey(roleID)]; !found {
				continue
			}
		}
		name := read.Title
		if merged {
			if name, err = run.nameIn(app, jobID, read, into); err != nil {
				return source, err
			}
		}
		if name != call.Name {
			names[call.Name] = name
		}
	}
	if len(names) == 0 {
		return source, nil
	}
	plan.Config["formula"], _ = rewriteDatabaseFormulaReferences(expression, func(name string) (string, bool) {
		next, found := names[name]
		return next, found
	})
	encoded, err := json.Marshal(plan)
	if err != nil {
		return source, err
	}
	source.Text = string(encoded)
	return source, nil
}
