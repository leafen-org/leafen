package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A run makes columns for a Notion property elsewhere than its own column,
// named from Notion's names: in the data source a split sent rows to, kept
// as "<property>:split:<data source>" and named as the property; beside a
// relation for its rows a split sent elsewhere, "<column>:to:<data source>",
// named after the column and that data source (notion_migration_beside_names.go);
// and keeping the names a property turned into objects matched no row of,
// "<column>:kept", named as the property with " (original)". When Notion
// renames the property, or the data source, these follow as the property's
// own column does (syncNotionMigrationPropertyName), by the same rule
// (notionMigrationNameFollows): a column made before takes the name a run
// gives it now while it has one a run gave it — from Notion's names now, or
// as they were at the run before, which the ledger keeps — and one a person
// renamed in Leafen keeps theirs (2026-11-24: Notion renaming a relation or a
// database left the columns split from and beside it with the old name). One
// that stands for a Notion property's own column — the target's column a
// split matched — goes by that property's name instead. They follow once, as
// a run's apply ends, when every page, database and property has its name and
// before the link phase makes what is new.

// notionMigrationKeptSuffixes end the name of a column keeping the names a
// property turned into objects matched no row of, in English and in Korean
// (keptObjectNames).
var notionMigrationKeptSuffixes = [...]string{" (original)", " (원래 값)"}

// notionMigrationColumnNames tells what a run names the columns it makes for
// a Notion property, now and as runs named them before.
type notionMigrationColumnNames struct {
	app                core.App
	jobID, migrationID string
	run                *notionMigrationRunPlan
}

// planned is the name the plan gives a property's column now, and the names
// runs gave it: that one, and the one from the name Notion gave the property
// at the run before. staged is false for a property Notion no longer gives,
// with no name now.
func (names notionMigrationColumnNames) planned(propertySource string) (string, map[string]bool, bool, error) {
	given := map[string]bool{}
	source, staged, err := notionImportStagedSource(names.app, names.jobID, propertySource)
	if err != nil {
		return "", nil, false, err
	}
	now := ""
	if staged {
		column, err := names.run.mergeColumn(source.ParentSourceID, source)
		if err != nil {
			return "", nil, false, err
		}
		now = column.Name
	} else {
		database, _, _ := strings.Cut(propertySource, ":property:")
		source = storedNotionSource{SourceID: propertySource, Kind: "property", ParentSourceID: database, Text: "{}"}
	}
	known, err := notionMigrationLedgerState(names.app, names.migrationID, propertySource, "property")
	if err != nil {
		return "", nil, false, err
	}
	if strings.TrimSpace(known) != "" {
		source.Title = known
		before, err := names.run.mergeColumn(source.ParentSourceID, source)
		if err != nil {
			return "", nil, false, err
		}
		given[before.Name] = true
	}
	given[now] = true
	delete(given, "")
	return now, given, staged, nil
}

// of is the name a run gives a column it made for a property elsewhere, kept
// as key, and the names runs gave it; "" for one that doesn't follow.
func (names notionMigrationColumnNames) of(key string, column graphProperty) (string, map[string]bool, error) {
	switch {
	case strings.HasSuffix(key, ":kept"):
		property := strings.TrimSuffix(key, ":kept")
		if cut := strings.LastIndex(property, ":split:"); cut >= 0 {
			property = property[:cut]
		}
		now, named, staged, err := names.planned(property)
		if err != nil || !staged {
			return "", nil, err
		}
		// The ending is in the language it was made in.
		for _, suffix := range notionMigrationKeptSuffixes {
			if strings.HasSuffix(strings.TrimSpace(column.Name), strings.TrimSpace(suffix)) {
				given := map[string]bool{}
				for name := range named {
					given[name+suffix] = true
				}
				return now + suffix, given, nil
			}
		}
		return "", nil, nil
	case strings.Contains(key, ":to:"):
		cut := strings.LastIndex(key, ":to:")
		holder, target := key[:cut], key[cut+len(":to:"):]
		if cut := strings.LastIndex(holder, ":split:"); cut >= 0 {
			holder = holder[:cut]
		}
		// One a person turned into another type, or to relate elsewhere,
		// stays as it is.
		related := struct {
			DataSourceID string `json:"relationDatabaseId"`
		}{}
		if column.Type != graphValueTypeRelation || json.Unmarshal(column.Config, &related) != nil || related.DataSourceID != target {
			return "", nil, nil
		}
		return names.beside(holder, target)
	case strings.Contains(key, ":split:"):
		now, given, staged, err := names.planned(key[:strings.LastIndex(key, ":split:")])
		if err != nil || !staged {
			return "", nil, err
		}
		return now, given, nil
	}
	return "", nil, nil
}

// followNotionMigrationColumnNames gives each column a run made before for a
// Notion property elsewhere the name a run gives it now, while it has one a
// run gave it, and the formulas of its data source that read it by that name
// its new one.
func followNotionMigrationColumnNames(app core.App, job storedJob, payload notionImportJobPayload, run *notionMigrationRunPlan) error {
	if run == nil || payload.MigrationID == "" {
		return nil
	}
	made := []struct {
		SourceID string `db:"source_id"`
		NativeID string `db:"native_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[made]].[[source_id]],[[made]].[[native_id]] FROM {{leafen_notion_import_mappings}} AS [[made]]
		WHERE [[made]].[[job_id]]={:jobId} AND [[made]].[[kind]]='property' AND ` + notionMigrationMadeElsewhereSQL("made") + `
			AND NOT EXISTS (SELECT 1 FROM {{leafen_notion_import_mappings}} AS [[own]]
				WHERE [[own]].[[job_id]]={:jobId} AND [[own]].[[kind]]='property' AND [[own]].[[native_id]]=[[made]].[[native_id]]
					AND NOT ` + notionMigrationMadeElsewhereSQL("own") + `)
		ORDER BY [[made]].[[source_id]]
	`).Bind(dbx.Params{"jobId": job.ID}).All(&made); err != nil {
		return err
	}
	names := notionMigrationColumnNames{app: app, jobID: job.ID, migrationID: payload.MigrationID, run: run}
	documents := map[string]*structuralMutationBatch{}
	for _, entry := range made {
		stored := struct {
			DataSourceID string `db:"data_source_id"`
			Type         string `db:"type"`
			Name         string `db:"name"`
			Position     string `db:"position"`
			Config       string `db:"config"`
		}{}
		err := app.DB().NewQuery(`
			SELECT [[data_source_id]],[[type]],[[name]],[[position]],[[config]] FROM {{leafen_database_properties}} WHERE [[id]]={:id}
		`).Bind(dbx.Params{"id": entry.NativeID}).One(&stored)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		column := graphProperty{
			ID: entry.NativeID, DataSourceID: stored.DataSourceID, Type: stored.Type, Name: stored.Name,
			Position: stored.Position, Config: json.RawMessage(stored.Config),
		}
		name, given, err := names.of(entry.SourceID, column)
		if err != nil {
			return err
		}
		if name == "" || !notionMigrationGivenNameFollows(stored.Name, name, given) {
			continue
		}
		column.Name = name
		if err := renameNotionMigrationColumn(app, job, payload, column, documents); err != nil {
			return err
		}
	}
	return saveStructuralMutationBatches(app, job.Owner, documents)
}

// notionMigrationMadeElsewhereSQL matches, of the mappings named so, those of
// columns a run made for a property elsewhere.
func notionMigrationMadeElsewhereSQL(mapping string) string {
	sourceID := "[[" + mapping + "]].[[source_id]]"
	return "(instr(" + sourceID + ", ':split:')>0 OR instr(" + sourceID + ", ':to:')>0 OR " + sourceID + " LIKE '%:kept')"
}

// renameNotionMigrationColumn gives a column a run made its new name, and
// the formulas of its data source that read it its new one.
func renameNotionMigrationColumn(
	app core.App, job storedJob, payload notionImportJobPayload, column graphProperty,
	documents map[string]*structuralMutationBatch,
) error {
	pageID, err := notionMigrationDataSourcePage(app, column.DataSourceID)
	if err != nil {
		return err
	}
	operations, err := notionMigrationRenamesWithFormulas(app, pageID, []graphOperation{{Kind: graphOperationKindUpsertProperty, Property: &column}})
	if err != nil {
		return err
	}
	// A relation names the data source it relates to.
	sources := map[string]graphEntityReference{}
	related := struct {
		DataSourceID string `json:"relationDatabaseId"`
	}{}
	if column.Type == graphValueTypeRelation && json.Unmarshal(column.Config, &related) == nil && related.DataSourceID != "" {
		references, err := loadGraphReferences(app, "leafen_data_sources", map[string]struct{}{related.DataSourceID: {}})
		if err != nil {
			return err
		}
		for id, reference := range references {
			if reference.Owner == payload.AuthorityOwnerID {
				sources[id] = reference
			}
		}
	}
	batch, err := loadStructuralMutationBatch(app, pageID, documents)
	if err != nil {
		return err
	}
	return applyNotionGraphOperationsWith(app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner,
		operations, notionGraphApplyReferences{sources: sources})
}
