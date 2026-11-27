package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A link to a view's address (view://ID, which reads as the app's address of
// the ID, /p/<ID>, as Notion's does; notionMarkdownAppAddress) naming by the
// ID Notion gives it a view a Notion move into the workspace brought points at
// the view the move made, as a data source's does
// (notionMovedDataSourcePath): at the address a link to it by its public API
// ID reads at. The move's map keeps a view under its database's key
// (<database>:view:<ID>) while the job's records are kept, and the
// migration's ledger by its own ID after (notion_address_moved_ledger.go).

// notionMovedView is the view a Notion move into the workspace made from the
// Notion view an ID names, the latest move's first, or "" for none.
func notionMovedView(app core.App, workspaceID, id string) (string, error) {
	nativeID := ""
	err := app.DB().NewQuery(`
		SELECT [[made]].[[native_id]] FROM (
			SELECT [[mapping]].[[native_id]], [[mapping]].[[created]] AS [[made_at]]
			FROM {{leafen_jobs}} AS [[job]]
			JOIN {{leafen_notion_import_mappings}} AS [[mapping]] ON [[mapping]].[[job_id]] = [[job]].[[id]]
				AND [[mapping]].[[kind]] = 'view'
			WHERE [[job]].[[workspace_id]] = {:workspaceId}
				AND substr([[mapping]].[[source_id]], -length({:keyed})) = {:keyed}
			UNION ALL
			SELECT [[ledger]].[[native_id]], MAX([[migration]].[[run_started_at]], [[migration]].[[created_at]])
			FROM {{leafen_notion_migrations}} AS [[migration]]
			JOIN {{leafen_notion_migration_ledger}} AS [[ledger]] ON [[ledger]].[[migration_id]] = [[migration]].[[id]]
				AND [[ledger]].[[source_id]] = {:id} AND [[ledger]].[[kind]] = {:byId}
			WHERE [[migration]].[[workspace_id]] = {:workspaceId}
` + notionMovedKeptByIDSQL + `
		) AS [[made]]
		JOIN {{leafen_database_views}} AS [[view]] ON [[view]].[[id]] = [[made]].[[native_id]]
			AND [[view]].[[workspace_id]] = {:workspaceId}
		ORDER BY [[made]].[[made_at]] DESC
		LIMIT 1
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "id": id, "byId": notionMigrationLedgerViewByID,
		"keyed": notionImportViewKeyMarker + strings.ReplaceAll(id, "-", ""),
	}).Row(&nativeID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return nativeID, err
}

// notionMovedViewPath is the link a write keeps for a path in the app naming
// by an ID nothing here answers to the view a Notion move made from the Notion
// view of that ID, at its public API ID's address; else link as it is.
func notionMovedViewPath(app core.App, workspaceID, id, link string) (string, error) {
	moved, err := notionMovedView(app, workspaceID, id)
	if err != nil || moved == "" {
		return link, err
	}
	public, err := ensureNotionObjectIdentity(app, notionObjectIdentityView, workspaceID, moved)
	if err != nil {
		return link, err
	}
	return "/p/" + strings.ReplaceAll(public, "-", ""), nil
}
