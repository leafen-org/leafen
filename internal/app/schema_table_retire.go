package app

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// dropRetiredTable takes away a table a migration retires; SQLite takes its
// indexes and triggers with it. The migration lists them all in
// schemaBaselineRetired.
func dropRetiredTable(app core.App, table string) error {
	_, err := app.DB().NewQuery("DROP TABLE IF EXISTS {{" + table + "}}").Execute()
	return err
}

// validateRetiredTableDropped is that migration's check: nothing named for
// the table, or on it, is left.
func validateRetiredTableDropped(app core.App, table string) error {
	var left int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM [[sqlite_master]] WHERE [[name]] = {:table} OR [[tbl_name]] = {:table}
	`).Bind(dbx.Params{"table": table}).Row(&left); err != nil {
		return err
	}
	if left != 0 {
		return fmt.Errorf("the retired table %s is still there", table)
	}
	return nil
}
