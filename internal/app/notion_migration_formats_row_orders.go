package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A run reports that Notion's API doesn't give the order people put a
// database's rows in (view_row_order_unavailable) for each database with a
// view that shows them so (notion_import_view_discovery.go). The reading of
// the person's Notion brings that order, or finds there is none to keep: a
// database whose every view the reading read and matched — its rows in
// Notion's order, or no order set by hand — is no longer so. A run again
// matches no reading it matched before, which still holds in the views it
// keeps, so it settles its own report by that matching too (2026-11-16: a run
// again reported 5 databases' row orders the reading had matched, databases
// with no order set by hand among them, where the first run reported none).

// settleNotionMigrationReadRowOrders takes the run's report of rows' order
// Notion didn't give off the databases the migration's reading matched.
func settleNotionMigrationReadRowOrders(app core.App, jobID, migrationID string) error {
	values := []string{}
	if err := app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_notion_migration_formats}}
		WHERE [[migration_id]]={:id} AND [[kind]]='view' ORDER BY [[notion_id]]`).
		Bind(dbx.Params{"id": migrationID}).Column(&values); err != nil {
		return err
	}
	// The databases whose views' order people set the reading brought, each
	// as long as no view of it is left unordered.
	ordered := map[string]bool{}
	for _, value := range values {
		view := notionMigrationViewFormat{}
		if json.Unmarshal([]byte(value), &view) != nil || view.Database == "" {
			continue
		}
		if done, seen := ordered[view.Database]; !seen || done {
			ordered[view.Database] = !slices.Contains(view.Left, "rows")
		}
	}
	for databaseID, done := range ordered {
		if !done {
			continue
		}
		issueID := databaseID + ":views#" + notionImportIssueViewRowOrderUnavailable
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_notion_import_sources}}
			WHERE [[job_id]]={:jobId} AND [[kind]]='issue' AND [[source_id]]={:issueId}`).
			Bind(dbx.Params{"jobId": jobID, "issueId": issueID}).Execute(); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_notion_migration_issues}}
			WHERE [[migration_id]]={:migrationId} AND [[issue_id]]={:issueId}`).
			Bind(dbx.Params{"migrationId": migrationID, "issueId": issueID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// viewRows puts a view's rows in the order people put them in Notion, a view
// that keeps such an order: the reading's, kept once for the views that have
// it (RowsOf, takeNotionMigrationFormats), or the view's own from a reading
// kept before. One whose order Leafen didn't take, or with more rows than a
// view places, is left.
func (matcher *notionMigrationFormatsMatcher) viewRows(viewType string, format *notionMigrationViewFormat, config map[string]any) error {
	if _, manual := notionImportManualOrderViews[viewType]; !manual {
		return nil
	}
	if format.RowsNotTaken {
		format.Left = append(format.Left, "rows")
		return nil
	}
	rows := format.Rows
	if rows == nil && format.RowsOf != "" {
		if matcher.orders == nil {
			matcher.orders = map[string][]string{}
		}
		kept, found := matcher.orders[format.RowsOf]
		if !found {
			var value string
			err := matcher.app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_notion_migration_formats}}
				WHERE [[migration_id]]={:id} AND [[kind]]='order' AND [[notion_id]]={:order}`).
				Bind(dbx.Params{"id": matcher.payload.MigrationID, "order": format.RowsOf}).Row(&value)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil && json.Unmarshal([]byte(value), &kept) != nil {
				kept = nil
			}
			matcher.orders[format.RowsOf] = kept
		}
		rows = kept
	}
	if rows == nil {
		return nil
	}
	order := []string{}
	for _, notionRow := range rows {
		rowID, err := matcher.mapped(canonicalNotionID(notionRow), "row")
		if err != nil {
			return err
		}
		if rowID != "" && !slices.Contains(order, rowID) {
			order = append(order, rowID)
		}
	}
	if len(order) > importViewPlacedRowsMax {
		format.Left = append(format.Left, "rows")
	} else {
		config["rowOrder"] = order
	}
	return nil
}
