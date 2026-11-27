package app

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a server save keeps — an import's or a migration run's, a copy's, a
// workspace import's — is searched for anew where it's shown already, as a
// person's change is (content_graph_mutation_owner.go): the pages whose text
// it changed (saveNotionYgoDocuments), the pages whose blocks it changed, and
// the database rows its graph writes made, changed or deleted
// (saveStructuralMutationBatches). A run again that brings a row's new values
// alone is found by them (2026-11-24: a row whose name changed in Notion kept
// its old one in search after the run, while a row whose body changed too
// took both). What isn't shown yet — an import's staged pages and rows —
// waits for its publishing (notion_import_publish.go), so a first import
// indexes each row once.

// wroteRows keeps the database rows a graph write on the batch's page made,
// changed or deleted, in the workspace it wrote them in, for the batch's save
// to mark for search.
func (batch *structuralMutationBatch) wroteRows(workspaceID string, operations []graphOperation) {
	for _, rowID := range graphMutationRowIDs(operations) {
		if rowID == "" {
			continue
		}
		if batch.rows == nil {
			batch.rows = map[string]string{}
		}
		batch.rows[rowID] = workspaceID
	}
}

// markShownSearchDirty marks for search, in one write, the pages and rows
// named that aren't staged, each in its own workspace. One whose record is
// gone, as a row deleted, is marked under the workspace named with it, so its
// entry goes.
func markShownSearchDirty(app core.App, named map[string]string) error {
	if len(named) == 0 {
		return nil
	}
	ids := make([]string, 0, len(named))
	for id := range named {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	shown := []struct {
		ID          string `db:"id"`
		WorkspaceID string `db:"workspace_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[named]].[[value]] AS [[id]], COALESCE([[page]].[[workspace]], '') AS [[workspace_id]]
		FROM json_each({:ids}) AS [[named]]
		LEFT JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[named]].[[value]]
		WHERE COALESCE([[page]].[[notion_import_staging]], 0) = 0
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&shown); err != nil {
		return err
	}
	targets := make([]searchDirtyTarget, 0, len(shown))
	for _, page := range shown {
		workspaceID := page.WorkspaceID
		if workspaceID == "" {
			workspaceID = named[page.ID]
		}
		if workspaceID == "" {
			continue
		}
		targets = append(targets, searchDirtyTarget{PageID: page.ID, WorkspaceID: workspaceID})
	}
	return markSearchDirtyTargets(context.Background(), app, targets)
}
