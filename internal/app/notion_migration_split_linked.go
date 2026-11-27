package app

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/pocketbase/dbx"
)

// The link phase's part of a split database's columns in the targets of its
// rows (notion_migration_split_columns.go): as it makes the database's own
// relation or rollup (property), it makes it in each target a split sent
// rows of the database to — a relation as its rows' relations are made there
// (splitRelationColumn), a rollup reading through that relation's column
// there (splitRollupColumn) — and then the database's formulas that waited
// there for them. One the plan preserves as text was made with the rest.

// splitLinkedColumns makes a split database's relation or rollup in the
// targets of its rows.
func (linker *notionImportLinker) splitLinkedColumns(source storedNotionSource) error {
	run := linker.run
	if run == nil || len(run.splits[notionMigrationSourceKey(source.ParentSourceID)]) == 0 ||
		!run.splitAhead(source.SourceID) || run.preservesShown(source.SourceID) {
		return nil
	}
	plan, err := linker.stagedProperty(source.SourceID)
	if err != nil || plan == nil || !plan.linked() {
		return err
	}
	targets, err := linker.splitTargets(source.ParentSourceID)
	if err != nil || len(targets) == 0 {
		return err
	}
	properties, err := notionSplitProperties(linker.app, linker.job.ID, source.ParentSourceID)
	if err != nil {
		return err
	}
	for _, columns := range targets {
		if plan.Type == graphValueTypeRollup {
			_, err = linker.splitRollupColumn(source, *plan, columns.target)
		} else {
			_, err = linker.splitRelationColumn(source.SourceID, columns.target)
		}
		if err != nil {
			return err
		}
		for _, property := range properties {
			if !strings.Contains(property.Text, `"type":"formula"`) || !run.splitAhead(property.SourceID) {
				continue
			}
			if _, err := columns.column(property.SourceID); err != nil {
				return err
			}
		}
	}
	return nil
}

// splitTargets are the targets a split sent rows of a database to, each
// with what makes the database's columns there: one the second pass made
// any of them in (ensure).
func (linker *notionImportLinker) splitTargets(database string) ([]*notionSplitColumns, error) {
	references := &notionApplyPropertyReferences{propertyReferences: map[string]graphEntityReference{}}
	targets, seen := []*notionSplitColumns{}, map[string]bool{}
	for _, rule := range linker.run.splits[notionMigrationSourceKey(database)] {
		target, err := notionMergeTargetMade(linker.app, linker.job.ID, linker.run, linker.mappings, rule.Into)
		if err != nil {
			return nil, err
		}
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		held := 0
		if err := linker.app.DB().NewQuery(`
			SELECT EXISTS (SELECT 1 FROM {{leafen_notion_import_mappings}}
				WHERE [[job_id]]={:jobId} AND [[kind]]='property' AND [[source_id]] > {:from} AND [[source_id]] < {:to}
					AND substr([[source_id]], -length({:suffix}))={:suffix})
		`).Bind(dbx.Params{
			"jobId": linker.job.ID, "from": database + ":property:", "to": database + ":property;",
			"suffix": notionSplitColumnSource("", target),
		}).Row(&held); err != nil {
			return nil, err
		}
		if held == 0 {
			continue
		}
		targets = append(targets, &notionSplitColumns{
			app: linker.app, job: linker.job, payload: linker.payload, run: linker.run, into: rule.Into, target: target,
			documents: linker.documents, mappings: linker.mappings, references: references,
			gone: func(propertySource string) (bool, error) {
				nativeID, err := linker.linkedPropertyID(propertySource, 0)
				return nativeID == "", err
			},
		})
	}
	return targets, nil
}

// splitTargetColumn is the target's own relation or rollup a split
// database's property stands for there — of its name, type and related
// database, made first when it isn't yet — "" for none; a rollup computing
// otherwise than the target's of its name takes the name it has beside it
// (notion_migration_merge_computed.go).
func (linker *notionImportLinker) splitTargetColumn(source storedNotionSource, column *notionMergeColumn, dataSourceID, key string) (string, error) {
	if !column.Kept {
		return "", nil
	}
	into, err := linker.splitInto(source.ParentSourceID, dataSourceID)
	if err != nil || into == "" {
		return "", err
	}
	matched, apart, err := linker.run.targetColumnApart(linker.app, linker.job.ID, *column, into, source.ParentSourceID,
		func(candidate storedNotionSource, key string, own bool) (string, error) {
			nativeID, err := linker.propertyID(key)
			if err != nil || nativeID != "" || !own {
				return nativeID, err
			}
			if err := linker.property(candidate); err != nil {
				return "", err
			}
			return linker.propertyID(candidate.SourceID)
		})
	if err == nil && matched == "" && apart {
		column.Name, err = linker.run.apartName(linker.app, linker.job.ID, column.Name, source.ParentSourceID)
	}
	if err != nil || matched == "" {
		return "", err
	}
	linker.linked[key] = matched
	return matched, linker.mappings.insert(key, matched, "property")
}

// splitInto is the database a split of a database sends rows into that
// became the data source dataSourceID, "" for none.
func (linker *notionImportLinker) splitInto(database, dataSourceID string) (string, error) {
	for _, rule := range linker.run.splits[notionMigrationSourceKey(database)] {
		made, err := notionMergeTargetMade(linker.app, linker.job.ID, linker.run, linker.mappings, rule.Into)
		if err != nil {
			return "", err
		}
		if made == dataSourceID {
			return rule.Into, nil
		}
	}
	return "", nil
}

// splitRollupColumn is the column a split database's rollup has in a
// target: the target's own of its name, or a new rollup reading, through
// its relation's column there, the property it reads; "" while either isn't
// made.
func (linker *notionImportLinker) splitRollupColumn(source storedNotionSource, plan notionDatabasePropertyPlan, dataSourceID string) (string, error) {
	key := notionSplitColumnSource(source.SourceID, dataSourceID)
	if nativeID, err := linker.propertyID(key); err != nil || nativeID != "" || plan.Link == nil {
		return nativeID, err
	}
	column, err := linker.run.mergeColumn(source.ParentSourceID, source)
	if err != nil {
		return "", err
	}
	if matched, err := linker.splitTargetColumn(source, &column, dataSourceID, key); err != nil || matched != "" {
		return matched, err
	}
	relation := plan.Link.RelationSourceID
	if role, gathered := linker.run.roleColumn(relation); gathered {
		relation = role
	}
	relationID, err := linker.splitRelationColumn(relation, dataSourceID)
	if err != nil || relationID == "" {
		return "", err
	}
	_, targetID, err := linker.rollupReferences(*plan.Link)
	if err != nil || targetID == "" {
		return "", err
	}
	config := maps.Clone(plan.Config)
	if config == nil {
		config = map[string]any{}
	}
	config["rollupRelationPropertyId"], config["rollupTargetPropertyId"] = relationID, targetID
	encoded, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	pageID, err := linker.dataSourcePage(dataSourceID)
	if err != nil {
		return "", err
	}
	batch, err := loadStructuralMutationBatch(linker.app, pageID, linker.documents)
	if err != nil {
		return "", err
	}
	propertyID := notionImportLinkedPropertyID(linker.job.ID, key)
	if err := applyNotionGraphOperationsWith(linker.app, batch, linker.payload.AuthorityOwnerID, linker.job.WorkspaceID, pageID, linker.job.Owner,
		[]graphOperation{{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
			ID: propertyID, DataSourceID: dataSourceID, Type: graphValueTypeRollup, Name: column.Name,
			Position: fmt.Sprintf("z%010d", source.Position), Config: encoded,
		}}}, notionGraphApplyReferences{},
	); err != nil {
		return "", err
	}
	linker.linked[key] = propertyID
	return propertyID, linker.mappings.insert(key, propertyID, "property")
}
