package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database the plan merges into another comes over into it as the import
// makes it: its data
// source stands for the target's, each of its properties for the target's
// column of the name, type and related database the plan leaves both with —
// a select's options joined by name — or else for a new column of the
// target under the plan's name, and its rows are made in the target with
// the IDs they keep. So every relation to, from or within it goes on
// pointing where it did, and a run again finds the same places through the
// ledger. Its views and its full-page page aren't made. Apply goes in
// Notion's order, and what a merged database holds waits for a second pass,
// after its target and everything else are made.

// mergeTarget is the database a database merges into, as the plan names it.
func (run *notionMigrationRunPlan) mergeTarget(sourceID string) (string, bool) {
	if run == nil {
		return "", false
	}
	into, merged := run.merges[notionMigrationSourceKey(sourceID)]
	return into, merged
}

func (run *notionMigrationRunPlan) hasMerges() bool {
	return run != nil && len(run.merges) > 0
}

// stagedTarget is the staged source of a merge target, or of a database
// bringing columns into one (notionMergeAlongside), "" when the import
// didn't stage it.
func (run *notionMigrationRunPlan) stagedTarget(app core.App, jobID, into string) (string, error) {
	key := notionMigrationSourceKey(into)
	if staged, found := run.targets[key]; found {
		return staged, nil
	}
	staged := ""
	err := app.DB().NewQuery(`
		SELECT [[source_id]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='database' AND lower(replace([[source_id]], '-', ''))={:key}
		ORDER BY [[sequence]] LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID, "key": key}).Row(&staged)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	run.targets[key] = staged
	return staged, nil
}

// notionMergeMember is a source the second pass makes: one a merged
// database holds — with that database's staged source and the database it
// goes into — or a split database's row or what is in one (Split).
type notionMergeMember struct {
	Database, Into string
	Split          bool
}

// notionMergeMembers says which of a window's sources the second pass makes:
// what a merged database holds — the database, its full-page page, its
// properties and rows and what is in them — and a split database's rows
// and what is in them (notion_migration_split.go).
func notionMergeMembers(app core.App, jobID string, run *notionMigrationRunPlan, sources []storedNotionSource) (map[int64]notionMergeMember, error) {
	members := map[int64]notionMergeMember{}
	if !run.hasSecondPass() || len(sources) == 0 {
		return members, nil
	}
	bySequence := map[int64]storedNotionSource{}
	for _, source := range sources {
		bySequence[source.Sequence] = source
	}
	sequences := make([]int64, 0, len(sources))
	for _, source := range sources {
		sequences = append(sequences, source.Sequence)
	}
	encoded, err := json.Marshal(sequences)
	if err != nil {
		return nil, err
	}
	chain := []struct {
		Sequence int64  `db:"sequence"`
		SourceID string `db:"source_id"`
	}{}
	// Each source's sources above it to the top however deep; each is reached
	// once (UNION), so a chain that comes back on itself ends.
	if err := app.DB().NewQuery(`
		WITH RECURSIVE [[up]]([[sequence]], [[source_id]], [[parent_source_id]]) AS (
			SELECT [[sequence]], [[source_id]], [[parent_source_id]] FROM {{leafen_notion_import_sources}}
			WHERE [[job_id]]={:jobId} AND [[sequence]] IN (SELECT [[value]] FROM json_each({:sequences}))
			UNION
			SELECT [[up]].[[sequence]], [[parent]].[[source_id]], [[parent]].[[parent_source_id]]
			FROM [[up]] JOIN {{leafen_notion_import_sources}} [[parent]]
				ON [[parent]].[[job_id]]={:jobId} AND [[parent]].[[source_id]]=[[up]].[[parent_source_id]]
			WHERE [[up]].[[parent_source_id]] != ''
		)
		SELECT DISTINCT [[sequence]], [[source_id]] FROM [[up]]
	`).Bind(dbx.Params{"jobId": jobID, "sequences": string(encoded)}).All(&chain); err != nil {
		return nil, err
	}
	for _, link := range chain {
		database := strings.TrimPrefix(link.SourceID, notionDatabaseRootPageSourceID(""))
		if into, merged := run.mergeTarget(database); merged {
			members[link.Sequence] = notionMergeMember{Database: database, Into: into}
			continue
		}
		source := bySequence[link.Sequence]
		if _, split := run.splits[notionMigrationSourceKey(link.SourceID)]; split && source.SourceID != link.SourceID &&
			(source.ParentSourceID != link.SourceID || source.Kind == "row") {
			if _, merged := members[link.Sequence]; !merged {
				members[link.Sequence] = notionMergeMember{Database: link.SourceID, Split: true}
			}
		}
	}
	return members, nil
}

// notionMergedDatabaseItself says whether a source is a merged database or
// its full-page page: neither is made — the database's data source stands
// for its target's — so what they would hang from isn't needed either
// (2026-10-16: the merge pass failed on the full-page page a merged
// database would have gone in).
func notionMergedDatabaseItself(source storedNotionSource, membership notionMergeMember, member bool) bool {
	if !member || membership.Split {
		return false
	}
	return notionMergeWrapper(source) || source.Kind == "database" && source.ParentKind != notionImportPropertyIssueParentKind
}

// notionMergeWrapper says whether a source is a merged database's full-page
// page, which isn't made.
func notionMergeWrapper(source storedNotionSource) bool {
	return source.Kind == "page" && strings.HasPrefix(source.SourceID, notionDatabaseRootPageSourceID(""))
}

// notionMergeTargetMade is the data source a merge target became, "" while
// the import hasn't made it.
func notionMergeTargetMade(app core.App, jobID string, run *notionMigrationRunPlan, mappings *notionApplyMappings, into string) (string, error) {
	staged, err := run.stagedTarget(app, jobID, into)
	if err != nil || staged == "" {
		return "", err
	}
	if nativeID, found := mappings.nativeID(staged, "data_source"); found {
		return nativeID, nil
	}
	return notionImportMappedID(app, jobID, staged, "data_source")
}

// aliasNotionMergedDatabase lets a merged database's data source stand for
// its target's, before anything it holds is made.
func aliasNotionMergedDatabase(
	app core.App, job storedJob, payload notionImportJobPayload, run *notionMigrationRunPlan, databaseID string,
	mappings *notionApplyMappings, dataSourcePages *notionApplyDataSourcePages,
) error {
	if _, found := mappings.nativeID(databaseID, "data_source"); found {
		return nil
	}
	// An earlier window stored the alias: this one keeps it without storing
	// it again.
	if nativeID, err := notionImportMappedID(app, job.ID, databaseID, "data_source"); err != nil || nativeID != "" {
		if nativeID != "" {
			mappings.remember(databaseID, nativeID, "data_source")
		}
		return err
	}
	into, _ := run.mergeTarget(databaseID)
	target, err := notionMergeTargetMade(app, job.ID, run, mappings, into)
	if err != nil {
		return err
	}
	if target == "" {
		return errors.New(errorCodeNotionImportCreateFailed)
	}
	pageID, err := notionMigrationDataSourcePage(app, target)
	if err != nil {
		return err
	}
	if err := dataSourcePages.insert(payload.AuthorityOwnerID, pageID, target); err != nil {
		return err
	}
	return mappings.insert(databaseID, target, "data_source")
}

// applyNotionMergedProperty makes a merged database's property stand for
// the target's column of its name and type — that column taking the
// options it has none of by name — or a new column of the target under the
// plan's name. A relation or rollup is made in the link phase.
func applyNotionMergedProperty(
	app core.App, job storedJob, payload notionImportJobPayload, run *notionMigrationRunPlan, source storedNotionSource,
	structuralDocuments map[string]*structuralMutationBatch, mappings *notionApplyMappings,
	dataSourcePages *notionApplyDataSourcePages, propertyReferences *notionApplyPropertyReferences,
) error {
	if _, found := mappings.nativeID(source.SourceID, "property"); found {
		return nil
	}
	plan := notionDatabasePropertyPlan{}
	if err := json.Unmarshal([]byte(source.Text), &plan); err != nil {
		return err
	}
	if plan.linked() {
		return nil
	}
	targetID, column, err := notionMergeTargetProperty(app, job.ID, run, source,
		func(_ storedNotionSource, key string, _ bool) (string, error) {
			if nativeID, found := mappings.nativeID(key, "property"); found {
				return nativeID, nil
			}
			return notionImportMappedID(app, job.ID, key, "property")
		})
	if err != nil {
		return err
	}
	if targetID == "" {
		source.Title = column.Name
		_, err := applyNotionDatabasePropertyMapping(
			app, job, payload, source, structuralDocuments, mappings, dataSourcePages, propertyReferences,
		)
		return err
	}
	if err := joinNotionMergedOptions(app, job, payload, plan, targetID, structuralDocuments, propertyReferences); err != nil {
		return err
	}
	return mappings.insert(source.SourceID, targetID, "property")
}

// joinNotionMergedOptions gives the target's column the options of a merged
// property it has none of by name — a status's in its first group — and
// keeps the column's reference for the rows the turn makes.
func joinNotionMergedOptions(
	app core.App, job storedJob, payload notionImportJobPayload, plan notionDatabasePropertyPlan, targetID string,
	structuralDocuments map[string]*structuralMutationBatch, propertyReferences *notionApplyPropertyReferences,
) error {
	stored := struct {
		Owner        string `db:"owner"`
		PageID       string `db:"page_id"`
		DataSourceID string `db:"data_source_id"`
		Type         string `db:"type"`
		Name         string `db:"name"`
		Position     string `db:"position"`
		Config       string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[owner]], [[page_id]], [[data_source_id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_properties}} WHERE [[id]]={:id}
	`).Bind(dbx.Params{"id": targetID}).One(&stored); err != nil {
		return err
	}
	reference := graphEntityReference{
		ID: targetID, Owner: stored.Owner, PageID: stored.PageID, DataSourceID: stored.DataSourceID, Type: stored.Type, Config: stored.Config,
	}
	added, _ := plan.Config["options"].([]any)
	if !notionMigrationOptionTypes[stored.Type] || len(added) == 0 {
		propertyReferences.replace(reference)
		return nil
	}
	config := map[string]any{}
	if err := json.Unmarshal([]byte(stored.Config), &config); err != nil {
		return err
	}
	options, _ := config["options"].([]any)
	names, ids := map[string]bool{}, map[string]bool{}
	for _, option := range options {
		fields, _ := option.(map[string]any)
		name, _ := fields["name"].(string)
		id, _ := fields["id"].(string)
		names[strings.ToLower(strings.TrimSpace(name))], ids[id] = true, true
	}
	joined := []string{}
	for _, option := range added {
		fields, _ := option.(map[string]any)
		name, _ := fields["name"].(string)
		id, _ := fields["id"].(string)
		if folded := strings.ToLower(strings.TrimSpace(name)); folded == "" || names[folded] || id == "" || ids[id] {
			continue
		} else {
			names[folded], ids[id] = true, true
		}
		options = append(options, fields)
		joined = append(joined, id)
	}
	if len(joined) == 0 {
		propertyReferences.replace(reference)
		return nil
	}
	config["options"] = options
	if groups, _ := config["statusGroups"].([]any); len(groups) > 0 {
		if first, ok := groups[0].(map[string]any); ok {
			members, _ := first["optionIds"].([]any)
			for _, id := range joined {
				members = append(members, id)
			}
			first["optionIds"] = members
		}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	batch, err := loadStructuralMutationBatch(app, stored.PageID, structuralDocuments)
	if err != nil {
		return err
	}
	if err := applyNotionGraphOperationsWithReferences(app, batch, payload.AuthorityOwnerID, job.WorkspaceID, stored.PageID, job.Owner,
		[]graphOperation{{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
			ID: targetID, DataSourceID: stored.DataSourceID, Type: stored.Type, Name: stored.Name, Position: stored.Position, Config: encoded,
		}}}, nil, propertyReferences, nil,
	); err != nil {
		return err
	}
	reference.Config = string(encoded)
	propertyReferences.replace(reference)
	return nil
}

// notionMergedOptionValue gives a row's option value in its column's
// options: an option the column doesn't have — one a merged database
// brought — becomes the column's option of its name as the plan leaves it.
func notionMergedOptionValue(
	app core.App, run *notionMigrationRunPlan, jobID, propertySource, columnConfig string, value notionDatabaseRowValuePlan,
) (notionDatabaseRowValuePlan, error) {
	if !notionMigrationOptionTypes[value.Type] || value.Value == nil {
		return value, nil
	}
	config := struct {
		Options []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"options"`
	}{}
	_ = json.Unmarshal([]byte(columnConfig), &config)
	known, byName := map[string]bool{}, map[string]string{}
	for _, option := range config.Options {
		known[option.ID], byName[strings.ToLower(strings.TrimSpace(option.Name))] = true, option.ID
	}
	ids := []string{}
	switch typed := value.Value.(type) {
	case string:
		ids = append(ids, typed)
	case []any:
		for _, item := range typed {
			if id, ok := item.(string); ok {
				ids = append(ids, id)
			}
		}
	default:
		return value, nil
	}
	missing := false
	for _, id := range ids {
		missing = missing || !known[id]
	}
	if !missing {
		return value, nil
	}
	var text string
	if err := app.DB().NewQuery(`
		SELECT [[text]] FROM {{leafen_notion_import_sources}} WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId}
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": propertySource}).Row(&text); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return value, err
	}
	plan := notionDatabasePropertyPlan{}
	_ = json.Unmarshal([]byte(text), &plan)
	own := run.finalOptionNames(propertySource, plan)
	for id, name := range own {
		own[id] = strings.ToLower(strings.TrimSpace(name))
	}
	translated := make([]any, 0, len(ids))
	for _, id := range ids {
		if !known[id] {
			if other, found := byName[own[id]]; found && own[id] != "" {
				id = other
			}
		}
		translated = append(translated, id)
	}
	if _, single := value.Value.(string); single {
		value.Value = translated[0]
	} else {
		value.Value = translated
	}
	return value, nil
}

// notionMergeAliasedEntry says whether a merged database's planned
// property stands for one of its target's own, by the scan, as the import
// matches them (notionMergeStandsFor; notion_migration_merge_computed_plan.go):
// then the target's changes are the column's.
func notionMergeAliasedEntry(
	plan notionMigrationPlan, databases map[string]notionMigrationDatabase, merged notionMigrationPlanDatabase, entry notionMigrationPlanProperty,
) bool {
	columns := newNotionMergePlanColumns(plan, databases)
	database := notionMigrationSourceKey(merged.SourceID)
	property, _, found := columns.property(database, entry.SourceID)
	if !found {
		return false
	}
	matched, _ := columns.match(database, property, entry, notionMigrationSourceKey(merged.Into))
	return matched
}
