package app

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/pocketbase/pocketbase/core"
)

// A computed column — a formula or a rollup — of a database merged into
// another, or of the rows a split sends to another, stands for the target's
// column of its name and type only when it computes the same there: a
// formula reading the same columns the same way, by the names it reads them
// in the target, a rollup reading the same property through the same
// relation's column with the same calculation (2026-11-24: a merged
// database's formula matched to its target's of the name took the target's
// expression, and its rows showed other values than Notion's). One that
// computes otherwise than the target's of its name, or than another merged
// database's, keeps a column of its own beside them, named '<name> (<its
// database>)', so its rows compute as they did in Notion, and a formula of
// its database reading it reads that column; the plan's agenda and the
// report say which (notion_migration_merge_computed_plan.go), by the run's
// rule (notionMergeStandsFor). Notion has no merge or split, so this is
// Leafen's own.

// notionMergeFormulaDefinition is a formula as its definition compares: each
// name it reads as named gives it, without the spaces outside its strings.
func notionMergeFormulaDefinition(expression string, named func(string) (string, bool)) string {
	rewritten, _ := rewriteDatabaseFormulaReferences(expression, named)
	var text strings.Builder
	quote, escaped := rune(0), false
	for _, character := range rewritten {
		switch {
		case quote != 0:
			text.WriteRune(character)
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == quote {
				quote = 0
			}
		case character == '"' || character == '\'':
			quote = character
			text.WriteRune(character)
		case !unicode.IsSpace(character):
			text.WriteRune(character)
		}
	}
	return graphValueTypeFormula + "\x00" + text.String()
}

// notionMergeRollupDefinition is a rollup as its definition compares: the
// name of its relation's column, the database it relates to, the name of
// the property it reads there and its calculation.
func notionMergeRollupDefinition(relation, related, property, function string) string {
	return strings.Join([]string{graphValueTypeRollup, strings.ToLower(relation), related, strings.ToLower(property), function}, "\x00")
}

// computedDefinition is a staged formula's or rollup's definition as it
// computes in the database into, its own or the target its rows go to; ""
// for another type.
func (run *notionMigrationRunPlan) computedDefinition(app core.App, jobID string, source storedNotionSource, into string) (string, error) {
	plan := notionDatabasePropertyPlan{}
	if err := json.Unmarshal([]byte(source.Text), &plan); err != nil || !workspaceExportComputedType(plan.Type) {
		return "", err
	}
	key := source.SourceID + "\x00" + notionMigrationSourceKey(into)
	if definition, found := run.definitions[key]; found {
		return definition, nil
	}
	if run.definitions == nil {
		run.definitions = map[string]string{}
	}
	// One reading itself through others, which Notion doesn't let one do,
	// computes as nothing else does.
	run.definitions[key] = source.SourceID
	definition := ""
	if plan.Type == graphValueTypeFormula {
		expression, _ := plan.Config["formula"].(string)
		properties, err := notionSplitProperties(app, jobID, source.ParentSourceID)
		if err != nil {
			return "", err
		}
		byName := map[string]storedNotionSource{}
		for _, property := range properties {
			if _, taken := byName[property.Title]; !taken {
				byName[property.Title] = property
			}
		}
		names := map[string]string{}
		for _, call := range databaseFormulaPropCalls([]rune(expression)) {
			read, found := byName[call.Name]
			if _, done := names[call.Name]; done || !found {
				continue
			}
			if names[call.Name], err = run.nameIn(app, jobID, read, into); err != nil {
				return "", err
			}
		}
		definition = notionMergeFormulaDefinition(expression, func(name string) (string, bool) {
			next, found := names[name]
			return next, found
		})
	} else if plan.Link != nil {
		relationName, related, propertyName := "", "", ""
		if relation, found, err := notionImportStagedSource(app, jobID, plan.Link.RelationSourceID); err != nil {
			return "", err
		} else if found {
			if relationName, err = run.nameIn(app, jobID, relation, into); err != nil {
				return "", err
			}
			column, err := run.mergeColumn(relation.ParentSourceID, relation)
			if err != nil {
				return "", err
			}
			related = column.Target
		}
		if target, found, err := notionImportStagedSource(app, jobID, plan.Link.TargetSourceID); err != nil {
			return "", err
		} else if found {
			if propertyName, err = run.nameIn(app, jobID, target, ""); err != nil {
				return "", err
			}
		}
		function, _ := plan.Config["rollupFunction"].(string)
		definition = notionMergeRollupDefinition(relationName, related, propertyName, function)
	}
	run.definitions[key] = definition
	return definition, nil
}

// nameIn is the name of the column a staged property's values have in the
// database into — its own database's when into is "" or that one: the role's
// for a relation gathered into another, the plan's, and for a computed
// property of a database whose rows go into another, its own column's there
// when it doesn't compute as the target's of its name.
func (run *notionMigrationRunPlan) nameIn(app core.App, jobID string, source storedNotionSource, into string) (string, error) {
	if role, gathered := run.roleColumn(source.SourceID); gathered {
		staged, found, err := notionImportStagedSource(app, jobID, role)
		if err != nil {
			return "", err
		}
		if found {
			source = staged
		}
	}
	column, err := run.mergeColumn(source.ParentSourceID, source)
	if err != nil || !column.Kept {
		return source.Title, err
	}
	if into == "" || notionMigrationSourceKey(into) == notionMigrationSourceKey(source.ParentSourceID) || !workspaceExportComputedType(column.Type) {
		return column.Name, nil
	}
	return run.columnName(app, jobID, column, into, source.ParentSourceID)
}

// sameComputed says whether a formula or rollup computes in the target into
// as the target's candidate does.
func (run *notionMigrationRunPlan) sameComputed(app core.App, jobID string, source, candidate storedNotionSource, into string) (bool, error) {
	mine, err := run.computedDefinition(app, jobID, source, into)
	if err != nil {
		return false, err
	}
	theirs, err := run.computedDefinition(app, jobID, candidate, into)
	return err == nil && mine != "" && mine == theirs, err
}

// columnName is the name a property's column has in the target into: the
// plan's, or '<name> (<its database>)' for a formula or rollup coming apart
// (notionMergeStandsFor) — the name the run makes it under, whichever is
// made first.
func (run *notionMigrationRunPlan) columnName(app core.App, jobID string, column notionMergeColumn, into, from string) (string, error) {
	_, apart, err := run.targetColumnApart(app, jobID, column, into, from,
		func(_ storedNotionSource, key string, _ bool) (string, error) { return key, nil })
	if err != nil || !apart {
		return column.Name, err
	}
	return run.apartName(app, jobID, column.Name, from)
}

// apartName is '<name> (<its database>)', by the database's name as the
// plan leaves it.
func (run *notionMigrationRunPlan) apartName(app core.App, jobID, name, from string) (string, error) {
	title := run.titles[notionMigrationSourceKey(from)]
	if title == "" {
		database, _, err := notionImportStagedSource(app, jobID, from)
		if err != nil {
			return "", err
		}
		title = database.Title
	}
	return notionMergeApartName(name, title), nil
}

// notionMergeApartName names a computed column kept beside its target's of
// the same name after the database it comes from.
func notionMergeApartName(name, database string) string {
	if database = strings.TrimSpace(database); database == "" {
		return name
	}
	return name + " (" + database + ")"
}
