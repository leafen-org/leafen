package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a direct import — a Notion import job no migration runs, as POST
// /workspaces/{workspaceId}/notion-import/jobs starts one — made is kept past
// its job's records, as a migration's ledger keeps what its runs made
// (notion_address_moved_ledger.go): the links and mentions a write names a
// Notion page, block, data source or view by find what the import made of it
// after the job's records go, 14 days after it ends or once 25 newer jobs
// ended (notion_import_retention_*). Before 2026-11-21 they kept the Notion
// address from then on. The retirement keeps them as it takes the job's
// records: as it takes the job's staged sources, each data source and view
// the import made by the Notion ID of the one it came from (which a
// database's staged source names for its first data source), and as it takes
// each window of the job's map, each page, row and block the import made. It
// keeps only what is still here and shows: an import that failed or was
// canceled had its staging pages taken first. A Notion object imported again
// keeps what each import made of it, as the jobs' maps did: a link names what
// the latest made that is still here (notionMovedMappings).

const notionImportKeptMappingsTable = "leafen_notion_import_kept_mappings"

func init() {
	// They go with their workspace (ON DELETE CASCADE).
	workspaceDeletionDirectAuthorities = append(workspaceDeletionDirectAuthorities,
		workspaceDeletionDirectAuthority{Table: notionImportKeptMappingsTable, Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	)
}

// ensureNativeNotionImportKeptMappingsSchema makes the table, migration 842.
func ensureNativeNotionImportKeptMappingsSchema(app core.App) error {
	if _, err := app.DB().NewQuery(`CREATE TABLE {{leafen_notion_import_kept_mappings}} (
			[[workspace_id]] TEXT NOT NULL,
			[[source_id]] TEXT NOT NULL,
			[[kind]] TEXT NOT NULL CHECK ([[kind]] IN ('page', 'row', 'block', 'data_source_by_id', 'view_by_id')),
			[[native_id]] TEXT NOT NULL,
			[[created]] INTEGER NOT NULL,
			PRIMARY KEY ([[workspace_id]], [[source_id]], [[kind]], [[native_id]]),
			FOREIGN KEY ([[workspace_id]]) REFERENCES {{leafen_workspaces}} ([[id]]) ON DELETE CASCADE
		)`).Execute(); err != nil {
		return err
	}
	return validateNativeNotionImportKeptMappingsSchema(app)
}

func validateNativeNotionImportKeptMappingsSchema(app core.App) error {
	var columns, keys int
	if err := app.DB().NewQuery(`SELECT COUNT(*), COALESCE(SUM([[pk]] > 0), 0) FROM pragma_table_info('leafen_notion_import_kept_mappings')
		WHERE [[name]] IN ('workspace_id', 'source_id', 'kind', 'native_id', 'created') AND [[notnull]] = 1`).Row(&columns, &keys); err != nil {
		return err
	}
	if columns != 5 || keys != 4 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}

// keepNotionDirectImportMade keeps, as the retirement of a direct import's
// records takes the keys of a phase, what the import made: in the staged
// sources' first window each data source and view by its own Notion ID, and
// in each window of the map the pages, rows and blocks of its keys. A job a
// migration ran passes by: its ledger keeps what it made.
func keepNotionDirectImportMade(ctx context.Context, app core.App, run storedNotionImportRetentionRun, keys []string) error {
	ownIDs := run.Phase == notionImportRetentionPhaseSources && run.AfterKey == ""
	if !ownIDs && (run.Phase != notionImportRetentionPhaseMappings || len(keys) == 0) {
		return nil
	}
	workspaceID := ""
	err := app.DB().NewQuery(`
		SELECT [[job]].[[workspace_id]] FROM {{leafen_jobs}} AS [[job]]
		JOIN {{leafen_workspaces}} AS [[workspace]] ON [[workspace]].[[id]] = [[job]].[[workspace_id]]
		WHERE [[job]].[[id]] = {:jobId} AND [[job]].[[kind]] = 'notion-import'
			AND NOT EXISTS (SELECT 1 FROM {{leafen_notion_migrations}} AS [[migration]]
				WHERE [[migration]].[[id]] = CASE WHEN json_valid([[job]].[[payload_json]])
					THEN COALESCE(json_extract([[job]].[[payload_json]], '$.migrationId'), '') ELSE '' END
					AND [[migration]].[[workspace_id]] = [[job]].[[workspace_id]])
	`).WithContext(ctx).Bind(dbx.Params{"jobId": run.JobID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && workspaceID == "" {
		return nil
	}
	if err != nil {
		return err
	}
	if ownIDs {
		return keepNotionDirectImportMadeByOwnID(ctx, app, workspaceID, run.JobID)
	}
	encoded, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_import_kept_mappings}} ([[workspace_id]], [[source_id]], [[kind]], [[native_id]], [[created]])
		SELECT {:workspaceId}, [[mapping]].[[source_id]], [[mapping]].[[kind]], [[mapping]].[[native_id]], [[mapping]].[[created]]
		FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		WHERE [[mapping]].[[job_id]] = {:jobId} AND [[mapping]].[[source_id]] IN (SELECT [[value]] FROM json_each({:keys}))
			AND ([[mapping]].[[kind]] IN ('page', 'row') AND EXISTS (SELECT 1 FROM {{leafen_pages}} AS [[page]]
					WHERE [[page]].[[id]] = [[mapping]].[[native_id]] AND [[page]].[[workspace]] = {:workspaceId}
						AND [[page]].[[notion_import_staging]] = FALSE)
				OR [[mapping]].[[kind]] = 'block' AND EXISTS (SELECT 1 FROM {{leafen_blocks}} AS [[block]]
					WHERE [[block]].[[id]] = [[mapping]].[[native_id]] AND [[block]].[[workspace_id]] = {:workspaceId}))
		` + notionImportKeptOnceSQL,
	).WithContext(ctx).Bind(dbx.Params{"workspaceId": workspaceID, "jobId": run.JobID, "keys": string(encoded)}).Execute()
	return err
}

// keepNotionDirectImportMadeByOwnID keeps each data source and view the job
// made, and that is still here, by the Notion ID of the one it came from.
func keepNotionDirectImportMadeByOwnID(ctx context.Context, app core.App, workspaceID, jobID string) error {
	made, err := notionImportMadeByOwnID(app, jobID)
	if err != nil || len(made) == 0 {
		return err
	}
	encoded, err := json.Marshal(made)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_import_kept_mappings}} ([[workspace_id]], [[source_id]], [[kind]], [[native_id]], [[created]])
		SELECT {:workspaceId}, json_extract([[made]].[[value]], '$.SourceID'), json_extract([[made]].[[value]], '$.Kind'),
			json_extract([[made]].[[value]], '$.NativeID'), json_extract([[made]].[[value]], '$.Created')
		FROM json_each({:made}) AS [[made]]
		WHERE json_extract([[made]].[[value]], '$.Kind') = {:dataSource} AND EXISTS (SELECT 1 FROM {{leafen_data_sources}} AS [[source]]
				WHERE [[source]].[[id]] = json_extract([[made]].[[value]], '$.NativeID') AND [[source]].[[workspace_id]] = {:workspaceId})
			OR json_extract([[made]].[[value]], '$.Kind') = {:view} AND EXISTS (SELECT 1 FROM {{leafen_database_views}} AS [[view]]
				WHERE [[view]].[[id]] = json_extract([[made]].[[value]], '$.NativeID') AND [[view]].[[workspace_id]] = {:workspaceId})
		` + notionImportKeptOnceSQL,
	).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "made": string(encoded),
		"dataSource": notionMigrationLedgerDataSourceByID, "view": notionMigrationLedgerViewByID,
	}).Execute()
	return err
}

// notionImportKeptOnceSQL keeps what an import made once, though a
// retirement taken up again keeps it again.
const notionImportKeptOnceSQL = `
		ON CONFLICT ([[workspace_id]], [[source_id]], [[kind]], [[native_id]]) DO NOTHING`
