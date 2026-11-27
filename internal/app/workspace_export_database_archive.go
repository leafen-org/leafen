package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database's archive is the archive of one database, as its HTML and
// Markdown & CSV are: the database the screen shows, opened on its own,
// rather than the page holding it. Its scope's root is that page, whose
// pages the export walks as a page's archive does — the database's row
// pages, with the sub-pages and databases in their bodies — but it takes of
// the page itself only the database: its block, its container, its own data
// sources with their views, properties, rows and templates. The archive
// brings the database back as a full-page database, as Notion's "페이지로
// 전환" makes one of an inline database (notion_database_turn.go): its page
// is named and pictured after its source, and holds only the database.

// workspaceExportDatabaseArchive reports a database's archive.
func workspaceExportDatabaseArchive(payload workspaceExportJobPayload) bool {
	return payload.Format == workspaceExportFormatPageNativeArchive && payload.Scope.Kind == workspaceExportScopeDatabase
}

// workspaceExportDatabaseArchiveContainerSQL is the database a database's
// archive holds: the container its data source is its own source in.
const workspaceExportDatabaseArchiveContainerSQL = `(SELECT [[home]].[[container_id]]
	FROM {{leafen_database_container_sources}} AS [[home]]
	JOIN {{leafen_database_containers}} AS [[home_container]] ON [[home_container]].[[id]] = [[home]].[[container_id]]
	WHERE [[home]].[[data_source_id]] = {:targetId} AND [[home]].[[source_container_id]] = ''
		AND [[home_container]].[[kind]] = 'source'
	ORDER BY [[home]].[[container_id]] LIMIT 1)`

// workspaceExportDatabaseArchiveSourceSQL holds for a data source the
// archived database owns: its own sources, not those it links.
func workspaceExportDatabaseArchiveSourceSQL(sourceExpression string) string {
	return sourceExpression + ` IN (SELECT [[own]].[[data_source_id]] FROM {{leafen_database_container_sources}} AS [[own]]
		WHERE [[own]].[[container_id]] = ` + workspaceExportDatabaseArchiveContainerSQL + ` AND [[own]].[[source_container_id]] = '')`
}

// workspaceExportBeyondRootSQL holds for a page the export takes whole: one
// of its scope's pages other than its root.
func workspaceExportBeyondRootSQL(pageExpression string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} AS [[beyond]]
		WHERE [[beyond]].[[job_id]] = {:jobId} AND [[beyond]].[[page_id]] = ` + pageExpression + `
			AND NOT [[beyond]].[[is_root]])`
}

// workspaceExportDatabaseArchiveItemSQL is what a database's archive takes
// of a database table: the items on the pages it takes whole, and of the
// page holding the database, those of the database's own sources. A view is
// its source's own or the database's (a linked member's).
func workspaceExportDatabaseArchiveItemSQL(table string) string {
	own := workspaceExportDatabaseArchiveSourceSQL("[[items]].[[data_source_id]]")
	if table == "leafen_database_views" {
		own = "(" + own + " AND [[items]].[[container_id]] = '' OR [[items]].[[container_id]] = " +
			workspaceExportDatabaseArchiveContainerSQL + ")"
	}
	return "(" + workspaceExportBeyondRootSQL("[[items]].[[page_id]]") + " OR " + own + ")"
}

// workspaceExportDatabaseArchiveRowsSQL is what a database's archive takes
// of the rows a [[rows]] alias names: those on the pages it takes whole and
// the database's own.
var workspaceExportDatabaseArchiveRowsSQL = "(" + workspaceExportBeyondRootSQL("[[rows]].[[page_id]]") + " OR " +
	workspaceExportDatabaseArchiveSourceSQL("[[rows]].[[data_source_id]]") + ")"

// workspaceExportDatabaseArchiveContainerScopeSQL holds for a container a
// database's archive holds: its database, or one on the pages it takes whole.
func workspaceExportDatabaseArchiveContainerScopeSQL(alias string) string {
	return "(" + alias + ".[[id]] = " + workspaceExportDatabaseArchiveContainerSQL + " OR " +
		workspaceExportBeyondRootSQL(alias+".[[page_id]]") + ")"
}

// workspaceExportDatabaseArchiveRoot turns a database's archive's root, the
// page holding the database, into the full-page database it comes back as.
type workspaceExportDatabaseArchiveRoot struct {
	// id is the page the database comes back on, named by its data source:
	// the archive holds no other page of that ID.
	id            string
	holdingPageID string
	blockID       string
	source        map[string]any
}

func loadWorkspaceExportDatabaseArchiveRoot(
	app core.App, job storedJob, payload workspaceExportJobPayload,
) (*workspaceExportDatabaseArchiveRoot, error) {
	if !workspaceExportDatabaseArchive(payload) {
		return nil, nil
	}
	root := &workspaceExportDatabaseArchiveRoot{id: payload.Scope.TargetID}
	var source struct {
		Title      string `db:"title"`
		TitleSpans string `db:"title_spans"`
		Icon       string `db:"icon"`
		IconType   string `db:"icon_type"`
	}
	err := app.DB().NewQuery(`
		SELECT [[scope]].[[page_id]] FROM {{leafen_workspace_export_scope_pages}} AS [[scope]]
		WHERE [[scope]].[[job_id]] = {:jobId} AND [[scope]].[[is_root]]
	`).Bind(dbx.Params{"jobId": job.ID}).Row(&root.holdingPageID)
	if err == nil {
		err = app.DB().NewQuery(`
			SELECT [[block_id]] FROM {{leafen_database_containers}} WHERE [[id]] = ` + workspaceExportDatabaseArchiveContainerSQL,
		).Bind(dbx.Params{"targetId": payload.Scope.TargetID}).Row(&root.blockID)
	}
	if err == nil {
		err = app.DB().NewQuery(`
			SELECT [[title]], [[title_spans]], [[icon]], [[icon_type]] FROM {{leafen_data_sources}}
			WHERE [[id]] = {:targetId} AND [[workspace_id]] = {:workspaceId}
		`).Bind(dbx.Params{"targetId": payload.Scope.TargetID, "workspaceId": job.WorkspaceID}).One(&source)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, newWorkspaceExportFailure(errorCodeWorkspaceExportSourceChanged)
	}
	if err != nil {
		return nil, err
	}
	root.source = map[string]any{"title": source.Title, "icon": source.Icon, "iconType": source.IconType}
	spans := []any{}
	if source.TitleSpans != "" && json.Unmarshal([]byte(source.TitleSpans), &spans) == nil && len(spans) > 0 {
		root.source["titleSpans"] = spans
	}
	return root, nil
}

// rewrite moves an item from the page holding the database to its own page.
func (root *workspaceExportDatabaseArchiveRoot) rewrite(item workspaceExportInventoryItem) {
	if root == nil {
		return
	}
	value := item.Value
	if item.Kind == workspaceExportItemPage {
		if id, _ := value["id"].(string); id == root.holdingPageID {
			for _, field := range []string{"titleSpans", "reactions", "trashedAt", "archivedAt"} {
				delete(value, field)
			}
			for field, shown := range root.source {
				value[field] = shown
			}
			value["id"], value["content"], value["kind"] = root.id, "", pageKindDatabase
			value["cover"], value["isLocked"], value["inTrash"] = "", false, false
		}
		return
	}
	if pageID, _ := value["pageId"].(string); pageID != root.holdingPageID {
		return
	}
	value["pageId"] = root.id
	switch item.Kind {
	case workspaceExportItemBlock:
		if id, _ := value["id"].(string); id == root.blockID {
			value["parentId"] = nil
		}
	case workspaceExportItemDatabaseContainer:
		value["parentBlockId"] = nil
	}
}
