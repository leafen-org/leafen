package app

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// Every table a workspace's deletion reads by its workspace keeps an index
// for that read (a query-plan audit, 2026-11-10). A residue lane clears the
// workspace's rows 32 at a time in rowid order, which an index on the key
// alone walks without sorting; with other columns after the key, every
// window gathers and sorts all of the workspace's rows again. The last step
// counts what each table has left, which without an index led by the key
// reads the server's whole table while the deletion holds the write. This
// migration gives the eight tables that had neither their index; the check
// below keeps a later table from joining the inventory without one.

// validateWorkspaceDeletionAccessPaths reads the schema's indexes and checks
// that every table the deletion reads by its workspace has one serving the
// read: a residue lane one on its key alone, a table the last step counts one
// led by its key. Neither may be partial, and the key compares as written
// (schemaIndexReadPaths). A table the schema hasn't yet is left to the
// inventory check, which refuses it.
func validateWorkspaceDeletionAccessPaths(app core.App) error {
	paths, err := readSchemaIndexReadPaths(app, nil)
	if err != nil {
		return err
	}
	tables := map[string]bool{}
	if err := workspaceDeletionTablesPresent(app, tables); err != nil {
		return err
	}
	for _, lane := range workspaceDeletionResidueLanes {
		if lane.Table == "" || lane.ParentTable != "" || !tables[lane.Table] {
			continue
		}
		if !paths.aloneOn(lane.Table, lane.Key) {
			return fmt.Errorf("residue lane %s.%s has no index on its key alone", lane.Table, lane.Key)
		}
	}
	for _, authority := range workspaceDeletionDirectAuthorities {
		switch authority.Disposition {
		case workspaceDeletionAuthorityAudit, workspaceDeletionAuthorityReceipt, workspaceDeletionAuthorityProjection:
			continue
		}
		if tables[authority.Table] && !paths.ledBy(authority.Table, authority.Key) {
			return fmt.Errorf("%s.%s, counted as a deletion's last step, has no index led by its key", authority.Table, authority.Key)
		}
	}
	return nil
}

func workspaceDeletionTablesPresent(app core.App, tables map[string]bool) error {
	names := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[name]] FROM [[sqlite_master]] WHERE [[type]] = 'table'
	`).Column(&names); err != nil {
		return err
	}
	for _, name := range names {
		tables[name] = true
	}
	return nil
}
