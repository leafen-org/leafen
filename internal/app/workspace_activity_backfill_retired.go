package app

import "github.com/pocketbase/pocketbase/core"

// The feed's groups once came from the organization audit log's earlier
// rows, a row for every block of every saved version, which the job runner
// walked newest first from the place this table kept. The schema baseline
// begins that walk at its end: it seeds the place at the first row, so the
// walk found nothing and let the place go, and nothing writes it again. So
// migration 833 takes the table away (schemaBaselineRetired).

const workspaceActivityBackfillTable = "leafen_workspace_activity_backfill"

func dropWorkspaceActivityBackfill(app core.App) error {
	return dropRetiredTable(app, workspaceActivityBackfillTable)
}

func validateWorkspaceActivityBackfillDropped(app core.App) error {
	return validateRetiredTableDropped(app, workspaceActivityBackfillTable)
}
