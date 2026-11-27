package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a Notion move into a workspace made from a Notion page, database page,
// database, block or data source is found, for the links and mentions a write
// names it by (notion_address_links_here.go), in the import job's ID map while
// the job's records are kept, and in its migration's ledger, which keeps what
// each Notion object it moved became past them (notion_migration_rerun.go):
// the job's records go 14 days after it ends, or once 25 newer jobs ended
// (notion_import_retention_*), and before 2026-11-21 a link or a mention of a
// page the move brought kept its Notion address from then on, unnoticed. The
// ledger also keeps each data source the move made by its own Notion ID
// (notionMigrationLedgerDataSourceByID) — the map keeps a database's first
// one under the database's ID alone, and the staged database that names its
// own ID goes with the job's records — and each view by its own
// (notionMigrationLedgerViewByID), which the map keeps only inside its key. A
// direct import, which no migration runs (the jobs POST
// /workspaces/{workspaceId}/notion-import/jobs starts), keeps no ledger: what
// it made is kept past its records by their retirement instead, in
// leafen_notion_import_kept_mappings (notion_import_kept_mappings.go).

// notionMigrationLedgerDataSourceByID and notionMigrationLedgerViewByID mark,
// in a migration's ledger, a data source or a view the move made by the
// Notion ID of the one it came from, the one here as its native ID.
const (
	notionMigrationLedgerDataSourceByID = "data_source_by_id"
	notionMigrationLedgerViewByID       = "view_by_id"
)

// notionImportViewKeyMarker comes before a view's own Notion ID, its 32
// hexadecimal digits, in its key in the move's map (<database>:view:<ID>).
const notionImportViewKeyMarker = ":view:"

// notionMovedMadeSQL is what the Notion moves into the workspace
// {:workspaceId} made from the Notion objects whose keys the list sources
// gives (bound parameters, comma-separated), each row the key, its kind in
// the map, what it made and when that move ran: the jobs' maps, the
// migrations' ledgers and what direct imports' retired jobs made, the same
// thing from a job and its ledger alike.
func notionMovedMadeSQL(sources string) string {
	return `(
		SELECT [[mapping]].[[source_id]], [[mapping]].[[kind]], [[mapping]].[[native_id]], [[mapping]].[[created]] AS [[made_at]]
		FROM {{leafen_jobs}} AS [[job]]
		JOIN {{leafen_notion_import_mappings}} AS [[mapping]] ON [[mapping]].[[job_id]] = [[job]].[[id]]
			AND [[mapping]].[[source_id]] IN (` + sources + `)
		WHERE [[job]].[[workspace_id]] = {:workspaceId}
		UNION ALL
		SELECT [[ledger]].[[source_id]], [[ledger]].[[kind]], [[ledger]].[[native_id]],
			MAX([[migration]].[[run_started_at]], [[migration]].[[created_at]]) AS [[made_at]]
		FROM {{leafen_notion_migrations}} AS [[migration]]
		JOIN {{leafen_notion_migration_ledger}} AS [[ledger]] ON [[ledger]].[[migration_id]] = [[migration]].[[id]]
			AND [[ledger]].[[source_id]] IN (` + sources + `)
		WHERE [[migration]].[[workspace_id]] = {:workspaceId}
		UNION ALL
		SELECT [[kept]].[[source_id]], [[kept]].[[kind]], [[kept]].[[native_id]], [[kept]].[[created]] AS [[made_at]]
		FROM {{leafen_notion_import_kept_mappings}} AS [[kept]]
		WHERE [[kept]].[[workspace_id]] = {:workspaceId} AND [[kept]].[[source_id]] IN (` + sources + `)
	)`
}

// notionMovedKeptByIDSQL goes, as one more branch, after the migrations'
// ledgers in a lookup of a data source or a view a Notion move made by the
// Notion ID {:id} of the one it came from, its kind in the ledger {:byId}
// (notionMovedDataSource, notionMovedView): what direct imports whose jobs'
// records were retired made of it, each row what it made and when.
const notionMovedKeptByIDSQL = `
			UNION ALL
			SELECT [[kept]].[[native_id]], [[kept]].[[created]]
			FROM {{leafen_notion_import_kept_mappings}} AS [[kept]]
			WHERE [[kept]].[[workspace_id]] = {:workspaceId} AND [[kept]].[[source_id]] = {:id} AND [[kept]].[[kind]] = {:byId}
`

// notionImportMadeByOwn is a data source or a view a job made, by the Notion
// ID of the one it came from (its kind notionMigrationLedgerDataSourceByID or
// notionMigrationLedgerViewByID), and when its mapping was made.
type notionImportMadeByOwn struct {
	SourceID string
	Kind     string
	NativeID string
	Created  int64
}

// notionImportMadeByOwnID is each data source and view a job made by the
// Notion ID of the one it came from: a database's first data source by the ID
// its staged database names (recordNotionImportDatabaseDataSource), each other
// by its key's, and a view by its key's. It reads the job's staged databases,
// so only before its records are retired.
func notionImportMadeByOwnID(app core.App, jobID string) ([]notionImportMadeByOwn, error) {
	made := []struct {
		SourceID string `db:"source_id"`
		Kind     string `db:"kind"`
		NativeID string `db:"native_id"`
		Created  int64  `db:"created"`
		First    string `db:"first"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[mapping]].[[source_id]], [[mapping]].[[kind]], [[mapping]].[[native_id]], [[mapping]].[[created]], COALESCE((
			SELECT json_extract(substr([[staged]].[[text]], length({:prefix}) + 1), {:path})
			FROM {{leafen_notion_import_sources}} AS [[staged]]
			WHERE [[staged]].[[job_id]] = [[mapping]].[[job_id]] AND [[staged]].[[source_id]] = [[mapping]].[[source_id]]
				AND [[staged]].[[kind]] = 'database' AND substr([[staged]].[[text]], 1, length({:prefix})) = {:prefix}
				AND json_valid(substr([[staged]].[[text]], length({:prefix}) + 1))), '') AS [[first]]
		FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		WHERE [[mapping]].[[job_id]] = {:jobId} AND ([[mapping]].[[kind]] = 'data_source'
			OR [[mapping]].[[kind]] = 'view' AND instr([[mapping]].[[source_id]], {:viewMarker}) > 0)
	`).Bind(dbx.Params{
		"jobId": jobID, "prefix": notionPresentationSourcePrefix, "path": "$." + notionImportDatabaseDataSourceKey,
		"viewMarker": notionImportViewKeyMarker,
	}).All(&made); err != nil {
		return nil, err
	}
	byOwn := []notionImportMadeByOwn{}
	for _, item := range made {
		own, kind := item.First, notionMigrationLedgerDataSourceByID
		if _, keyed := notionImportKeyedIDs(item.SourceID); keyed != "" {
			own = keyed
		}
		if item.Kind == "view" {
			_, own, _ = strings.Cut(item.SourceID, notionImportViewKeyMarker)
			kind = notionMigrationLedgerViewByID
		}
		if id, isUUID := normalizeNotionUUIDRequest(strings.ToLower(strings.TrimSpace(own))); isUUID && item.NativeID != "" {
			byOwn = append(byOwn, notionImportMadeByOwn{SourceID: id, Kind: kind, NativeID: item.NativeID, Created: item.Created})
		}
	}
	return byOwn, nil
}

// recordNotionMigrationLedgerDataSources keeps in the ledger each data source
// and view a run's job made by the Notion ID of the one it came from
// (notionImportMadeByOwnID).
func recordNotionMigrationLedgerDataSources(app core.App, migrationID, jobID string) error {
	made, err := notionImportMadeByOwnID(app, jobID)
	if err != nil || len(made) == 0 {
		return err
	}
	entries := make([][3]string, len(made))
	for index, item := range made {
		entries[index] = [3]string{item.SourceID, item.Kind, item.NativeID}
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_migration_ledger}}
			([[migration_id]],[[workspace_id]],[[source_id]],[[kind]],[[native_id]],[[page_id]],[[state]])
		SELECT [[migration]].[[id]], [[migration]].[[workspace_id]], json_extract([[made]].[[value]], '$[0]'),
			json_extract([[made]].[[value]], '$[1]'), json_extract([[made]].[[value]], '$[2]'), '', ''
		FROM {{leafen_notion_migrations}} AS [[migration]], json_each({:made}) AS [[made]]
		WHERE [[migration]].[[id]] = {:migrationId}
		ON CONFLICT ([[migration_id]],[[source_id]],[[kind]]) DO UPDATE SET [[native_id]] = excluded.[[native_id]]
	`).Bind(dbx.Params{"migrationId": migrationID, "made": string(encoded)}).Execute()
	return err
}
