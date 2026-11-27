package app

import "github.com/pocketbase/pocketbase/core"

// deletionInventories are the lists that say what a deletion does to each
// table it reaches, read against the schema: every table kept by workspace
// for deleting a workspace (workspace_deletion_inventory.go), and every
// table that keeps rows by person for deleting an account
// (account_deletion_inventory.go); and, kept by the same rule, every table
// kept by workspace for moving a page to another workspace
// (page_workspace_move_tables.go).
var deletionInventories = []struct {
	check   func(core.App) error
	message string
}{
	{validateWorkspaceDeletionDirectAuthorityInventory,
		"leafen workspace deletion inventory doesn't fit this schema; deleting a workspace is refused until an update makes it fit"},
	{validateAccountDeletionInventory,
		"leafen account deletion inventory doesn't fit this schema; deleting an account is refused until an update makes it fit"},
	{validatePageWorkspaceMoveInventory,
		"leafen page workspace move inventory doesn't fit this schema; moving a page to another workspace is refused until an update makes it fit"},
}

// checkDeletionInventoriesAtStart holds the start to the rule every "a new
// table needs a class" check follows (schema_migration_export_classes.go).
// A development build stops at its first start over a schema with a table
// an inventory doesn't name, so the change that added the table names it
// before it ships. A release build writes the reason to its log and starts
// as usual: a server that won't start after an update costs its people far
// more than a deletion that waits. The deletion itself refuses meanwhile,
// before it changes anything (preflightWorkspaceDeletion, the account
// deletion routes) and at each step of one already under way
// (advanceWorkspaceDeletion, the account cleanup's account step), so none
// leaves that table's rows behind; it goes on once an update names the
// table (2026-11-17).
func checkDeletionInventoriesAtStart(app core.App) error {
	for _, inventory := range deletionInventories {
		err := inventory.check(app)
		if err == nil {
			continue
		}
		if unclassifiedTablesStopStart {
			return err
		}
		app.Logger().Error(inventory.message, "error", err.Error())
		if !app.IsDev() {
			productionDiagnosticLogger.Error(inventory.message, "error", err.Error())
		}
	}
	return nil
}
