package app

import "github.com/pocketbase/pocketbase/core"

// Every workspace has its People database from the start, as each of
// Notion's has its People, which its search finds in a workspace no one
// opened People in (seen 2026-11-15). So reading people — People's list, a
// person's profile — is never what makes it (2026-11-16: an agent's read
// key read a profile and a People database it had never seen came up in
// search): a new workspace has it made as it is made (createWorkspaceRecords),
// and one made before the server kept one for each, as the server starts.
// A workspace being deleted and an app's own workspace get none.

// makePeopleDatabasesSoon makes, as the server starts, the People database
// of each workspace that has none.
func makePeopleDatabasesSoon(app core.App) {
	workspaces := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[workspace]].[[id]] FROM {{leafen_workspaces}} AS [[workspace]]
		WHERE COALESCE([[workspace]].[[deletion_job_id]], '') = '' AND COALESCE([[workspace]].[[app_install_id]], '') = ''
			AND NOT EXISTS (SELECT 1 FROM {{leafen_workspace_people_databases}} WHERE [[workspace_id]] = [[workspace]].[[id]])
		ORDER BY [[workspace]].[[id]]
	`).Column(&workspaces); err != nil {
		app.Logger().Warn("leafen people databases wait", "error", err)
		return
	}
	for _, workspaceID := range workspaces {
		syncPeopleDatabaseSoon(app, workspaceID)
	}
}
