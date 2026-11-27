package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database the plan splits comes on its own (the 2026-10-14 recommended
// structure decision, 6), but each of its rows whose property holds one of
// a split's values — an option's name, "true" or "false" for a checkbox, or
// the text — is made in the split's target instead, with the ID it keeps;
// the first split that matches decides. Its columns there — one for each of
// its database's properties, computed or not (notion_migration_split_columns.go)
// — are the target's of the same name and type by the merge's rules
// (notion_migration_merge.go) or new columns of the target under the plan's
// names, each kept in the ledger as "<property>:split:<data source>". A
// relation to a row that went
// elsewhere is kept in a column of its own beside the relation, relating to
// where that row went, kept as "<property>:to:<data source>" and named after
// the column it stands beside (notion_migration_beside_names.go), so no value
// is lost; the names of both follow Notion's on a run again
// (notion_migration_rerun_column_names.go). Rows are made in the second
// pass, after the targets. On a run again a row stays where the first run
// put it, and a column beside a relation holds what the relation names
// there now (siblingValues).

func (run *notionMigrationRunPlan) hasSecondPass() bool {
	return run != nil && (len(run.merges) > 0 || len(run.splits) > 0)
}

func notionSplitColumnSource(propertySource, dataSourceID string) string {
	return propertySource + ":split:" + dataSourceID
}

func notionSplitRelationSource(propertySource, dataSourceID string) string {
	return propertySource + ":to:" + dataSourceID
}

// splitTarget is the database a split database's row goes to, "" for one
// that stays.
func (run *notionMigrationRunPlan) splitTarget(app core.App, jobID string, source storedNotionSource) (string, error) {
	rules := run.splits[notionMigrationSourceKey(source.ParentSourceID)]
	if source.Kind != "row" || len(rules) == 0 {
		return "", nil
	}
	values := map[string]notionDatabaseRowValuePlan{}
	if err := json.Unmarshal([]byte(source.Text), &values); err != nil {
		return "", err
	}
	for _, rule := range rules {
		for propertySource, value := range values {
			_, propertyID, found := strings.Cut(propertySource, ":property:")
			if !found || notionMigrationPropertyKey(propertyID) != notionMigrationPropertyKey(rule.Property) {
				continue
			}
			matches, err := notionSplitMatches(app, jobID, propertySource, value, rule.Values)
			if err != nil || matches {
				return map[bool]string{true: rule.Into}[matches], err
			}
		}
	}
	return "", nil
}

// notionSplitMatches says whether a row's value is one of a split's.
func notionSplitMatches(app core.App, jobID, propertySource string, value notionDatabaseRowValuePlan, values []string) (bool, error) {
	wanted := map[string]bool{}
	for _, item := range values {
		wanted[strings.ToLower(strings.TrimSpace(item))] = true
	}
	switch typed := value.Value.(type) {
	case bool:
		return wanted[fmt.Sprint(typed)], nil
	case string:
		if !notionMigrationOptionTypes[value.Type] {
			return wanted[strings.ToLower(strings.TrimSpace(typed))], nil
		}
	}
	if !notionMigrationOptionTypes[value.Type] {
		return false, nil
	}
	source, found, err := notionImportStagedSource(app, jobID, propertySource)
	if err != nil || !found {
		return false, err
	}
	plan := notionDatabasePropertyPlan{}
	_ = json.Unmarshal([]byte(source.Text), &plan)
	names := map[string]string{}
	options, _ := plan.Config["options"].([]any)
	for _, option := range options {
		fields, _ := option.(map[string]any)
		id, _ := fields["id"].(string)
		name, _ := fields["name"].(string)
		names[id] = strings.ToLower(strings.TrimSpace(name))
	}
	ids := []any{value.Value}
	if list, isList := value.Value.([]any); isList {
		ids = list
	}
	for _, id := range ids {
		if text, ok := id.(string); ok && wanted[names[text]] {
			return true, nil
		}
	}
	return false, nil
}

// notionImportStagedSource reads one staged source of a job.
func notionImportStagedSource(app core.App, jobID, sourceID string) (storedNotionSource, bool, error) {
	source := storedNotionSource{}
	err := app.DB().NewQuery(`
		SELECT [[sequence]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]], [[text]]
		FROM {{leafen_notion_import_sources}} WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId}
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": sourceID}).One(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return source, false, nil
	}
	return source, err == nil, err
}

// applyNotionSplitRow makes a split database's row in the target a split
// sends it to; false for a row that stays, or one made before — whose
// target, where a split sent it, takes the columns its database has since
// (notion_migration_split_columns.go).
func applyNotionSplitRow(
	app core.App, job storedJob, payload notionImportJobPayload, run *notionMigrationRunPlan, source storedNotionSource,
	structuralDocuments map[string]*structuralMutationBatch, mappings *notionApplyMappings,
	dataSourcePages *notionApplyDataSourcePages, propertyReferences *notionApplyPropertyReferences, rowIdentities *notionApplyRowIdentities,
) (bool, error) {
	columns := &notionSplitColumns{
		app: app, job: job, payload: payload, run: run,
		documents: structuralDocuments, mappings: mappings, references: propertyReferences,
	}
	if rowID, made := mappings.nativeID(source.SourceID, "row"); made {
		return false, run.splitRowPlace(columns, source, rowID)
	}
	into, err := run.splitTarget(app, job.ID, source)
	if err != nil || into == "" {
		return false, err
	}
	target, err := notionMergeTargetMade(app, job.ID, run, mappings, into)
	if err != nil {
		return false, err
	}
	if target == "" {
		return false, errors.New(errorCodeNotionImportCreateFailed)
	}
	pageID, err := notionMigrationDataSourcePage(app, target)
	if err != nil {
		return false, err
	}
	if err := dataSourcePages.insert(payload.AuthorityOwnerID, pageID, target); err != nil {
		return false, err
	}
	columns.into, columns.target = into, target
	if err := columns.ensure(source.ParentSourceID); err != nil {
		return false, err
	}
	place := &notionRowPlace{Run: run, DataSourceID: target, Column: columns.column, Skip: run.linkedLater,
		Value: func(propertySource string, value notionDatabaseRowValuePlan) (notionDatabaseRowValuePlan, error) {
			return run.mergedOptionValue(app, job.ID, propertySource, value)
		}}
	_, err = applyNotionDatabaseRow(
		app, job, payload, run, source, structuralDocuments, mappings, dataSourcePages, propertyReferences, rowIdentities, place,
	)
	return true, err
}

// splitRowPlace has the target a split sent a row made before to take the
// columns its database has, once a window.
func (run *notionMigrationRunPlan) splitRowPlace(columns *notionSplitColumns, source storedNotionSource, rowID string) error {
	own, found := columns.mappings.nativeID(source.ParentSourceID, "data_source")
	if !found {
		var err error
		if own, err = notionImportMappedID(columns.app, columns.job.ID, source.ParentSourceID, "data_source"); err != nil || own == "" {
			return err
		}
	}
	var placed string
	err := columns.app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_rows}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": rowID}).Row(&placed)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (placed == "" || placed == own) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, rule := range run.splits[notionMigrationSourceKey(source.ParentSourceID)] {
		made, err := notionMergeTargetMade(columns.app, columns.job.ID, run, columns.mappings, rule.Into)
		if err != nil || made != placed {
			if err != nil {
				return err
			}
			continue
		}
		columns.into, columns.target = rule.Into, placed
		return columns.ensure(source.ParentSourceID)
	}
	return nil
}

// rowPlace is the data source an imported row is in — the one a split sent
// it to, or its own — and whether a split sent it.
func (linker *notionImportLinker) rowPlace(source storedNotionSource, rowID string) (string, bool, error) {
	own, err := linker.lookup(source.ParentSourceID, "data_source")
	if err != nil {
		return "", false, err
	}
	var placed string
	err = linker.app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_rows}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": rowID}).Row(&placed)
	if errors.Is(err, sql.ErrNoRows) || err == nil && placed == "" {
		return own, false, nil
	}
	if err != nil {
		return "", false, err
	}
	return placed, placed != own, nil
}

// splitRelationColumn is the column a split database's relation has in the
// data source a split sent a row to: the target's of its name, type and
// related database, or a new one. A rollup reads through it there
// (splitRollupColumn).
func (linker *notionImportLinker) splitRelationColumn(propertySource, dataSourceID string) (string, error) {
	key := notionSplitColumnSource(propertySource, dataSourceID)
	if nativeID, err := linker.propertyID(key); err != nil || nativeID != "" {
		return nativeID, err
	}
	source, found, err := notionImportStagedSource(linker.app, linker.job.ID, propertySource)
	if err != nil || !found {
		return "", err
	}
	plan := notionDatabasePropertyPlan{}
	if err := json.Unmarshal([]byte(source.Text), &plan); err != nil {
		return "", err
	}
	if plan.Type != graphValueTypeRelation || plan.Link == nil {
		return "", nil
	}
	column, err := linker.run.mergeColumn(source.ParentSourceID, source)
	if err != nil {
		return "", err
	}
	if matched, err := linker.splitTargetColumn(source, &column, dataSourceID, key); err != nil || matched != "" {
		return matched, err
	}
	target, err := linker.relationTarget(*plan.Link)
	if err != nil || target == "" {
		return "", err
	}
	return linker.relationColumn(key, dataSourceID, column.Name, target)
}

// siblingRelationColumn is the column beside a relation that keeps its
// values relating to rows a split sent to another data source, named after
// the column the holder stands for (siblingRelationName). One made before
// took the name a run gives it now as the run's apply ended
// (notion_migration_rerun_column_names.go).
func (linker *notionImportLinker) siblingRelationColumn(holder, column, dataSourceID, target string) (string, error) {
	key := notionSplitRelationSource(holder, target)
	if nativeID, found := linker.linked[key]; found {
		return nativeID, nil
	}
	nativeID, err := linker.resolver.lookup(key, "property")
	if err != nil {
		return "", err
	}
	if nativeID != "" {
		linker.linked[key] = nativeID
		return nativeID, nil
	}
	name, err := linker.siblingRelationName(column, target)
	if err != nil {
		return "", err
	}
	return linker.relationColumn(key, dataSourceID, name, target)
}

// siblingValues puts among a row's values a relation's rows a split sent to
// other data sources, each place's in its column beside the relation, joined
// with those another property put there; column is the one the relation's
// values go into — its own, or the role it is gathered into. On a run again,
// each column beside it the relation names none of now — Notion took them
// out, or the relation holds none — is emptied, unless another property put
// some there, as the relation's own column is (relationValue; 2026-11-24: a
// row whose related rows a split had sent elsewhere kept naming them after
// Notion took them out).
func (linker *notionImportLinker) siblingValues(
	rowValues map[string]graphRowValue, holder, column, dataSourceID string, places []string, elsewhere map[string][]string,
) error {
	for _, place := range places {
		siblingID, err := linker.siblingRelationColumn(holder, column, dataSourceID, place)
		if err != nil {
			return err
		}
		if err := linker.relationValue(rowValues, siblingID, elsewhere[place]); err != nil {
			return err
		}
	}
	if linker.payload.MigrationRerunSince <= 0 {
		return nil
	}
	siblings, err := linker.siblingRelationColumns(holder, dataSourceID)
	if err != nil {
		return err
	}
	for _, siblingID := range siblings {
		if err := linker.relationValue(rowValues, siblingID, nil); err != nil {
			return err
		}
	}
	return nil
}

// siblingRelationColumns are the columns beside a relation that the runs
// before made in a data source, still there and still relations: one a person
// turned into another type keeps its cells, as a cell does
// (notion_migration_rerun_emptied.go).
func (linker *notionImportLinker) siblingRelationColumns(holder, dataSourceID string) ([]string, error) {
	// Each is kept as "<holder>:to:<data source>": those after the prefix and
	// before the same with its last ':' raised to ';'.
	prefix := notionSplitRelationSource(holder, "")
	columns := []string{}
	err := linker.app.DB().NewQuery(`
		SELECT [[mapping]].[[native_id]] FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		JOIN {{leafen_database_properties}} AS [[property]] ON [[property]].[[id]]=[[mapping]].[[native_id]]
		WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]='property'
			AND [[mapping]].[[source_id]] > {:prefix} AND [[mapping]].[[source_id]] < {:after}
			AND [[property]].[[data_source_id]]={:dataSourceId} AND [[property]].[[type]]={:relation}
		ORDER BY [[mapping]].[[source_id]]
	`).Bind(dbx.Params{
		"jobId": linker.job.ID, "prefix": prefix, "after": strings.TrimSuffix(prefix, ":") + ";",
		"dataSourceId": dataSourceID, "relation": graphValueTypeRelation,
	}).Column(&columns)
	return columns, err
}

// relationColumn makes a one-way relation column of the import.
func (linker *notionImportLinker) relationColumn(key, dataSourceID, name, target string) (string, error) {
	config, err := json.Marshal(map[string]any{"relationDatabaseId": target})
	if err != nil {
		return "", err
	}
	propertyID := notionImportLinkedPropertyID(linker.job.ID, key)
	if err := linker.writeRelationColumn(&graphProperty{
		ID: propertyID, DataSourceID: dataSourceID, Type: graphValueTypeRelation, Name: name, Position: "z" + propertyID, Config: config,
	}, target); err != nil {
		return "", err
	}
	linker.linked[key] = propertyID
	return propertyID, linker.mappings.insert(key, propertyID, "property")
}

// writeRelationColumn makes or changes a relation column of the import
// relating to target.
func (linker *notionImportLinker) writeRelationColumn(property *graphProperty, target string) error {
	pageID, err := linker.dataSourcePage(property.DataSourceID)
	if err != nil {
		return err
	}
	references, err := loadGraphReferences(linker.app, "leafen_data_sources", map[string]struct{}{target: {}})
	if err != nil {
		return err
	}
	sources := map[string]graphEntityReference{}
	for id, reference := range references {
		if reference.Owner == linker.payload.AuthorityOwnerID {
			sources[id] = reference
		}
	}
	batch, err := loadStructuralMutationBatch(linker.app, pageID, linker.documents)
	if err != nil {
		return err
	}
	return applyNotionGraphOperationsWith(linker.app, batch, linker.payload.AuthorityOwnerID, linker.job.WorkspaceID, pageID, linker.job.Owner,
		[]graphOperation{{Kind: graphOperationKindUpsertProperty, Property: property}}, notionGraphApplyReferences{sources: sources},
	)
}
