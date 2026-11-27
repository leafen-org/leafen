package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's export count (leafen_workspace_export_page_counts) is what a
// workspace export takes of it: the page, its document (one for any of its
// streams) and every row the per-table triggers count on it — its blocks,
// data sources, views, properties, database rows, comments, templates,
// database containers and trashed blocks' tombstones; its workspace's count
// (leafen_workspace_content_revisions) holds the sum over its pages. Three
// triggers count a page whole instead of one row at a time: a page made, a
// page whose workspace or owner changes (a move to another workspace, an
// owner's change) and a database row that becomes a page. The baseline's
// left out the last three tables, so a page moved to another workspace came
// out short by its inline databases, linked views, templates and
// tombstones, while their rows still took one each from it as they moved
// on: moving it back, or deleting enough of them, took its count below one
// and the change rolled back (2026-11-24: a page holding its own database,
// a linked view of it and a linked view of another couldn't move back).
// Migration 859 makes the three count every table (pageExportCountedTables
// is the one list) and recounts every stored page once, carrying the
// difference into its workspace's count.

const (
	nativePageExportCountWholeVersion = 859
	nativePageExportCountWholeName    = "native_page_export_count_whole"
)

// pageExportCountedTables are the tables whose rows count on a page, each
// by its page, workspace and (but comments) storage owner columns, as their
// own triggers count them one at a time.
var pageExportCountedTables = []struct{ table, owner string }{
	{"leafen_blocks", "owner"},
	{"leafen_data_sources", "owner"},
	{"leafen_database_views", "owner"},
	{"leafen_database_properties", "owner"},
	{"leafen_database_rows", "owner"},
	{"leafen_comments", ""},
	{"leafen_database_templates", "owner"},
	{"leafen_database_containers", "owner"},
	{"leafen_block_trash_tombstones", "owner_id"},
}

// pageExportCountWholeTriggers are the triggers that count a page whole.
var pageExportCountWholeTriggers = []string{
	"leafen_workspace_export_count_pages_INSERT",
	"leafen_workspace_export_count_pages_UPDATE",
	"leafen_page_kind_export_arrive",
}

// pageExportCountedRowsSQL sums a page's counted rows, the page, its
// workspace and its owner given as SQL expressions.
func pageExportCountedRowsSQL(page, workspace, owner string) string {
	parts := make([]string, 0, len(pageExportCountedTables))
	for _, counted := range pageExportCountedTables {
		where := `[[workspace_id]] = ` + workspace + ` AND [[page_id]] = ` + page
		if counted.owner != "" {
			where += ` AND [[` + counted.owner + `]] = ` + owner
		}
		parts = append(parts, `(SELECT COUNT(*) FROM {{`+counted.table+`}} WHERE `+where+`)`)
	}
	return strings.Join(parts, "\n\t\t\t\t\t+ ")
}

// pageExportStreamsSQL counts a page's document streams.
const pageExportStreamsSQL = `(SELECT
				(SELECT COUNT(*) FROM {{leafen_ygo_documents}} WHERE [[room]] = NEW.[[id]])
				+ (SELECT COUNT(*) FROM {{leafen_ygo_structural_documents}} WHERE [[room]] = NEW.[[id]]) AS [[count]]
			) AS [[streams]]`

// pageExportWorkspaceAddSQL adds the page's count to its workspace's, for
// a page of the workspace's own owner outside a Notion move's staging; the
// sign and row (NEW or OLD) given.
func pageExportWorkspaceAddSQL(sign, row string) string {
	return `UPDATE {{leafen_workspace_content_revisions}}
			SET [[item_count]] = [[item_count]] ` + sign + ` COALESCE((
				SELECT [[item_count]] FROM {{leafen_workspace_export_page_counts}} WHERE [[page_id]] = ` + row + `.[[id]]
			), 0)
			WHERE [[workspace_id]] = ` + row + `.[[workspace]] AND ` + row + `.[[notion_import_staging]] = FALSE
				AND EXISTS (SELECT 1 FROM {{leafen_workspaces}}
					WHERE [[id]] = ` + row + `.[[workspace]] AND [[owner]] = ` + row + `.[[owner]]);`
}

func pageExportCountWholeTriggerStatements() []string {
	counted := pageExportCountedRowsSQL("NEW.[[id]]", "NEW.[[workspace]]", "NEW.[[owner]]")
	return []string{
		`CREATE TRIGGER [[leafen_workspace_export_count_pages_INSERT]] AFTER INSERT ON {{leafen_pages}}
		WHEN NEW.[[data_source_id]] = ''
		BEGIN
			INSERT INTO {{leafen_workspace_export_page_counts}}
				([[page_id]], [[workspace_id]], [[owner]], [[item_count]], [[document_streams]])
			SELECT NEW.[[id]], NEW.[[workspace]], NEW.[[owner]],
				1 + CASE WHEN [[streams]].[[count]] > 0 THEN 1 ELSE 0 END
					+ ` + counted + `,
				MIN([[streams]].[[count]], 2)
			FROM ` + pageExportStreamsSQL + `;
			` + pageExportWorkspaceAddSQL("+", "NEW") + `
		END`,
		`CREATE TRIGGER [[leafen_workspace_export_count_pages_UPDATE]]
		AFTER UPDATE OF [[workspace]], [[owner]], [[notion_import_staging]] ON {{leafen_pages}}
		WHEN NEW.[[data_source_id]] = '' AND (OLD.[[workspace]] IS NOT NEW.[[workspace]]
			OR OLD.[[owner]] IS NOT NEW.[[owner]]
			OR OLD.[[notion_import_staging]] IS NOT NEW.[[notion_import_staging]])
		BEGIN
			` + pageExportWorkspaceAddSQL("-", "OLD") + `
			UPDATE {{leafen_workspace_export_page_counts}}
			SET [[workspace_id]] = NEW.[[workspace]], [[owner]] = NEW.[[owner]],
				[[item_count]] = CASE
					WHEN OLD.[[workspace]] = NEW.[[workspace]] AND OLD.[[owner]] = NEW.[[owner]] THEN [[item_count]]
					ELSE 1 + CASE WHEN [[document_streams]] > 0 THEN 1 ELSE 0 END
						+ ` + counted + `
				END
			WHERE [[page_id]] = NEW.[[id]];
			` + pageExportWorkspaceAddSQL("+", "NEW") + `
		END`,
		`CREATE TRIGGER [[leafen_page_kind_export_arrive]]
		AFTER UPDATE OF [[data_source_id]] ON {{leafen_pages}}
		WHEN OLD.[[data_source_id]] != '' AND NEW.[[data_source_id]] = '' BEGIN
			INSERT OR REPLACE INTO {{leafen_workspace_export_page_counts}}
				([[page_id]], [[workspace_id]], [[owner]], [[item_count]], [[document_streams]])
			SELECT NEW.[[id]], NEW.[[workspace]], NEW.[[owner]],
				1 + CASE WHEN [[streams]].[[count]] > 0 THEN 1 ELSE 0 END
					+ ` + counted + `,
				[[streams]].[[count]]
			FROM ` + pageExportStreamsSQL + `;
			` + pageExportWorkspaceAddSQL("+", "NEW") + `
		END`,
	}
}

// ensureNativePageExportCountWhole makes the three triggers count a page
// whole and recounts the pages stored before.
func ensureNativePageExportCountWhole(app core.App) error {
	for _, trigger := range pageExportCountWholeTriggers {
		if _, err := app.DB().NewQuery(`DROP TRIGGER IF EXISTS [[` + trigger + `]]`).Execute(); err != nil {
			return err
		}
	}
	for _, statement := range pageExportCountWholeTriggerStatements() {
		if _, err := app.DB().NewQuery(statement).Execute(); err != nil {
			return err
		}
	}
	return recountPageExportCounts(app)
}

// pageExportRecount is a stored page count that differs from its recount.
type pageExportRecount struct {
	PageID      string `db:"page_id"`
	WorkspaceID string `db:"workspace_id"`
	Counted     bool   `db:"counted"`
	Difference  int    `db:"difference"`
}

// recountPageExportCounts sets every stored page count to its recount and
// carries the difference into its workspace's count, where the workspace
// counts the page (its triggers' rule: a page of the workspace's own owner,
// outside a Notion move's staging).
func recountPageExportCounts(app core.App) error {
	recounts := []pageExportRecount{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], [[workspace_id]], [[counted]], [[whole]] - [[item_count]] AS [[difference]]
		FROM (
			SELECT [[c]].[[page_id]], [[c]].[[workspace_id]], [[c]].[[item_count]],
				1 + CASE WHEN [[c]].[[document_streams]] > 0 THEN 1 ELSE 0 END
					+ ` + pageExportCountedRowsSQL("[[c]].[[page_id]]", "[[c]].[[workspace_id]]", "[[c]].[[owner]]") + ` AS [[whole]],
				([[p]].[[notion_import_staging]] = FALSE AND EXISTS (SELECT 1 FROM {{leafen_workspaces}} AS [[w]]
					WHERE [[w]].[[id]] = [[c]].[[workspace_id]] AND [[w]].[[owner]] = [[c]].[[owner]])) AS [[counted]]
			FROM {{leafen_workspace_export_page_counts}} AS [[c]]
			JOIN {{leafen_pages}} AS [[p]] ON [[p]].[[id]] = [[c]].[[page_id]]
		)
		WHERE [[whole]] <> [[item_count]]
		ORDER BY [[page_id]]
	`).All(&recounts); err != nil {
		return err
	}
	workspaces, order := map[string]int{}, []string{}
	for _, recount := range recounts {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_workspace_export_page_counts}} SET [[item_count]] = [[item_count]] + {:difference}
			WHERE [[page_id]] = {:pageId}
		`).Bind(dbx.Params{"difference": recount.Difference, "pageId": recount.PageID}).Execute(); err != nil {
			return fmt.Errorf("page %s's count: %w", recount.PageID, err)
		}
		if !recount.Counted {
			continue
		}
		if _, seen := workspaces[recount.WorkspaceID]; !seen {
			order = append(order, recount.WorkspaceID)
		}
		workspaces[recount.WorkspaceID] += recount.Difference
	}
	for _, workspaceID := range order {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_workspace_content_revisions}} SET [[item_count]] = MAX([[item_count]] + {:difference}, 0)
			WHERE [[workspace_id]] = {:workspaceId}
		`).Bind(dbx.Params{"difference": workspaces[workspaceID], "workspaceId": workspaceID}).Execute(); err != nil {
			return fmt.Errorf("workspace %s's count: %w", workspaceID, err)
		}
	}
	if len(recounts) > 0 {
		app.Logger().Info("page export counts recounted whole", "pages", len(recounts), "workspaces", len(order))
	}
	return nil
}

// validateNativePageExportCountWhole checks that each of the three triggers
// counts every counted table.
func validateNativePageExportCountWhole(app core.App) error {
	for _, trigger := range pageExportCountWholeTriggers {
		var body string
		if err := app.DB().NewQuery(`SELECT [[sql]] FROM [[sqlite_master]] WHERE [[type]] = 'trigger' AND [[name]] = {:name}`).
			Bind(dbx.Params{"name": trigger}).Row(&body); err != nil {
			return fmt.Errorf("%s: trigger %s: %w", errorCodeSchemaMigrationHistoryInvalid, trigger, err)
		}
		for _, counted := range pageExportCountedTables {
			if !strings.Contains(body, "`"+counted.table+"`") {
				return errors.New(errorCodeSchemaMigrationHistoryInvalid + ": trigger " + trigger + " doesn't count " + counted.table)
			}
		}
	}
	return nil
}
