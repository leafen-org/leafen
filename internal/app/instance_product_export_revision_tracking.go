package app

import (
	"fmt"
	"slices"

	"github.com/pocketbase/pocketbase/core"
)

// An export in progress learns of each change to a row of a table the
// policy carries through three triggers on that table, which mark the row's
// revision changed (instance_product_export_source_revision_schema.go). The
// schema baseline made them for its tables. A migration after it that adds
// a table the policy carries (instance_product_export_policy.go) puts them
// on it in its up, after creating the table:
//
//	if err := trackInstanceProductExportSourceRevisions(app, "leafen_new_table"); err != nil {
//		return err
//	}
//
// and one that builds a carried table again puts them back the same way.
// Without them the baseline's check, run again after each new migration,
// stops the start: it holds these triggers to exactly the carried tables.

var instanceProductExportRevisionEvents = []string{"insert", "update", "delete"}

// instanceProductExportRevisionTrigger names a carried table's trigger for
// one of its events.
func instanceProductExportRevisionTrigger(table, event string) string {
	return "leafen_instance_product_export_revision_" + table + "_" + event
}

// trackInstanceProductExportSourceRevisions puts the export's tracking
// triggers on tables the policy carries, as the baseline's are written.
func trackInstanceProductExportSourceRevisions(app core.App, tables ...string) error {
	carried := instanceProductExportIncludedTableNames()
	for _, table := range tables {
		if !slices.Contains(carried, table) {
			return fmt.Errorf("export tracking: the export policy doesn't carry %s (instance_product_export_policy.go)", table)
		}
		if !app.HasTable(table) {
			return fmt.Errorf("export tracking: there is no table %s to track", table)
		}
		for _, event := range instanceProductExportRevisionEvents {
			row := "[[row_id]] = OLD.rowid"
			switch event {
			case "insert":
				row = "[[row_id]] = NEW.rowid"
			case "update":
				row = "([[row_id]] = OLD.rowid OR [[row_id]] = NEW.rowid)"
			}
			if _, err := app.DB().NewQuery(`CREATE TRIGGER IF NOT EXISTS ` +
				app.DB().QuoteSimpleTableName(instanceProductExportRevisionTrigger(table, event)) +
				` AFTER ` + event + ` ON ` + app.DB().QuoteSimpleTableName(table) + ` BEGIN
					UPDATE {{leafen_instance_product_export_source_revisions}}
					SET [[changed]] = 1 WHERE [[table_name]] = ` + sqlStringLiteral(table) + ` AND ` + row + `;
				END`).Execute(); err != nil {
				return err
			}
		}
	}
	return nil
}
