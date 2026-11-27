package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// storePageHistoryRevertReceipt keeps a revert's receipt at the history's
// frontier after it, so the same request again answers as it did, and the
// app's "한 일" says the person took that version's changes back
// (pageHistoryVersionRevertedBy). A revert of a page's only version, which
// leaves the page as it is, keeps one too.
func storePageHistoryRevertReceipt(
	app core.App, actorID, workspaceID, pageID, versionID, requestID, requestHash string, revision int,
) error {
	_, err := app.DB().Insert("leafen_page_history_restore_receipts", dbx.Params{
		"actor_id": actorID, "workspace_id": workspaceID,
		"page_id": pageID, "request_id": requestID,
		"request_hash": requestHash, "restored_version_id": versionID,
		"revision": revision, "created_at": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

// settlePageHistoryRevertReceipt moves a revert's receipt to the frontier
// its content change reached once it committed, after the receipt, so the
// same request again answers as the first did.
func settlePageHistoryRevertReceipt(app core.App, actorID, pageID, requestID string) (int, error) {
	revision, err := pageHistorySnapshotFrontier(app, pageID)
	if err != nil {
		return 0, err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_page_history_restore_receipts}} SET [[revision]]={:revision}
		WHERE [[actor_id]]={:actor} AND [[request_id]]={:request} AND [[page_id]]={:page}`).
		Bind(dbx.Params{"revision": revision, "actor": actorID, "request": requestID, "page": pageID}).Execute()
	return revision, err
}
