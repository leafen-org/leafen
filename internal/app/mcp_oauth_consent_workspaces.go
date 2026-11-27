package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// userKeyStagingWorkspacesSQL selects the workspaces an app's bundle is
// being imported into before it moves to its place: the connection screen
// sets them apart, and a key for all of its person's workspaces is for the
// rest (userKeyPersonWorkspaces).
const userKeyStagingWorkspacesSQL = `SELECT [[staging_workspace_id]] FROM {{leafen_bundle_import_sources}}`

// mcpConsentStagingWorkspaces are those of the workspaces an app's bundle is
// being imported into before it moves to its place.
func mcpConsentStagingWorkspaces(app core.App, rows []storedBootstrapWorkspace) (map[string]struct{}, error) {
	staging := map[string]struct{}{}
	if len(rows) == 0 {
		return staging, nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	found := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[value]] FROM json_each({:ids}) WHERE [[value]] IN (` + userKeyStagingWorkspacesSQL + `)
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&found); err != nil {
		return nil, err
	}
	for _, id := range found {
		staging[id] = struct{}{}
	}
	return staging, nil
}

// userKeyChoosableWorkspaces are the person's workspaces, as their list
// shows them and in its order, that a key for all of them names
// (userKeyPersonWorkspaces): what `leafen connect` and a connection asked
// for by code reach when no workspace is named.
func userKeyChoosableWorkspaces(app core.App, personID string) ([]storedBootstrapWorkspace, error) {
	ids, err := userKeyPersonWorkspaces(app, personID)
	if err != nil {
		return nil, err
	}
	own := []storedBootstrapWorkspace{}
	cursor := bootstrapCursor{}
	for {
		rows, done, err := loadBootstrapWorkspaces(app, personID, cursor, maxMCPOAuthConsentPage)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if slices.Contains(ids, row.ID) {
				own = append(own, row)
			}
		}
		if done || len(rows) == 0 {
			return own, nil
		}
		last := rows[len(rows)-1]
		cursor = bootstrapCursor{WorkspaceCreated: last.Created, WorkspaceID: last.ID}
	}
}
