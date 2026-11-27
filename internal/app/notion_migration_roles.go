package app

import (
	"strings"
)

// Several relations a plan gathers into one (the 2026-10-14 recommended
// structure decision, 6): a relation's "into" names another relation of the
// same database, relating to the same database once merged — the role
// column, under its plan's name — and the import makes no column for it:
// it stands for the role column, and each row's values of both go into the
// role column together; a two-way role column's other side names them back
// (notion_migration_shared_pairs.go). A two-way relation whose other side
// stands for another column, gathered or merged, keeps its values one way:
// one column can't be the other side of two.

// roleColumn is the property a gathered relation stands for, by its staged
// source.
func (run *notionMigrationRunPlan) roleColumn(propertySource string) (string, bool) {
	if run == nil {
		return "", false
	}
	databaseID, propertyID, found := strings.Cut(propertySource, ":property:")
	if !found {
		return "", false
	}
	entry, planned := run.columns[notionMigrationSourceKey(databaseID)][notionMigrationPropertyKey(propertyID)]
	if !planned || strings.TrimSpace(entry.Into) == "" {
		return "", false
	}
	return notionPropertySourceID(databaseID, strings.TrimSpace(entry.Into)), true
}

// aliased says whether a staged property stands for another column, so no
// column of its own is made: a gathered relation, or a merged database's
// property the target has a column for.
func (linker *notionImportLinker) aliased(propertySource string) (bool, error) {
	if _, gathered := linker.run.roleColumn(propertySource); gathered {
		return true, nil
	}
	source, found, err := notionImportStagedSource(linker.app, linker.job.ID, propertySource)
	if err != nil || !found {
		return false, err
	}
	if _, merged := linker.run.mergeTarget(source.ParentSourceID); !merged {
		return false, nil
	}
	match, _, err := notionMergeTargetProperty(linker.app, linker.job.ID, linker.run, source,
		func(_ storedNotionSource, key string, _ bool) (string, error) { return key, nil })
	return match != "", err
}

// gatherRelation makes a gathered relation stand for its role column, made
// first when it isn't yet.
func (linker *notionImportLinker) gatherRelation(source storedNotionSource, role string) error {
	roleID, err := linker.propertyID(role)
	if err != nil {
		return err
	}
	if roleID == "" {
		staged, found, err := notionImportStagedSource(linker.app, linker.job.ID, role)
		if err != nil || !found {
			return err
		}
		if err := linker.property(staged); err != nil {
			return err
		}
		if roleID, err = linker.propertyID(role); err != nil || roleID == "" {
			return err
		}
	}
	linker.linked[source.SourceID] = roleID
	return linker.mappings.insert(source.SourceID, roleID, "property")
}

// gatheredValue joins a row's values into what the role column has from
// another relation of the row.
func gatheredValue(rowValues map[string]graphRowValue, propertyID string, ids []string) ([]string, bool) {
	current, found := rowValues[propertyID]
	if !found {
		return ids, false
	}
	joined := []string{}
	seen := map[string]bool{}
	for _, list := range [][]string{graphRelationRowIDs(current.Value), ids} {
		for _, id := range list {
			if !seen[id] {
				seen[id] = true
				joined = append(joined, id)
			}
		}
	}
	return joined, true
}
