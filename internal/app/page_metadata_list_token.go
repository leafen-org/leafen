package app

import "github.com/pocketbase/pocketbase/core"

// A tab's own page change keeps its page list current. The tab sends the
// workspace refresh token its list was read with; when that list was still
// current just before the change, the change answers with the token of the
// list after it, so the change check the write itself sets off
// (useWorkspaceChangeFollow) finds nothing new and the tab doesn't read again
// the list, teamspaces and page it already shows (2026-11-13: one page lock
// sent 12 to 19 requests; Notion's sends one). Read in the change's own
// transaction, the token holds nobody else's change. A change that moves the
// page — its parent, teamspace, owner or the trash — changes more of the list
// than the page's own record, and answers without one.

// applyPageMetadataKeepingList applies the change as applyPageMetadataWithLock
// does, the pages its place change edits noted with it (page_place_parent_edits.go),
// and, when it can, returns the list's refresh token after it.
func applyPageMetadataKeepingList(
	app core.App, user *core.Record, pageID string, input pageMetadataInput, requestHash string, listToken string,
) (pageMetadataResponse, pageMetadataMutationStatus, string, error) {
	if listToken == "" {
		response, status, err := applyPageMetadataInApp(app, user.Id, pageID, input, requestHash)
		return response, status, "", err
	}
	var response pageMetadataResponse
	status := pageMetadataApplied
	refreshed := ""
	err := app.RunInTransaction(func(txApp core.App) error {
		before, _ := txApp.FindRecordById(pagesCollection, pageID)
		workspaceID := ""
		if before != nil {
			workspaceID = before.GetString("workspace")
		}
		current := workspaceID != "" && listCurrent(txApp, user, workspaceID, listToken)
		var err error
		response, status, err = applyPageMetadataInApp(txApp, user.Id, pageID, input, requestHash)
		if err != nil || status != pageMetadataApplied || !current || response.Receipt.Duplicate {
			return err
		}
		after, findErr := txApp.FindRecordById(pagesCollection, pageID)
		if findErr != nil || pageMovedInList(before, after) {
			return nil
		}
		if snapshot, ok := workspaceChangeSnapshotOf(txApp, user.Id, workspaceID); ok {
			refreshed, _ = sealWorkspaceRefreshToken(user, snapshot)
		}
		return nil
	})
	if err != nil {
		refreshed = ""
	}
	return response, status, refreshed, err
}

// listCurrent reports whether a list read with token is the workspace's list
// now, as the change check decides (answerWorkspaceChangeProbe).
func listCurrent(app core.App, user *core.Record, workspaceID, token string) bool {
	payload, err := openWorkspaceRefreshToken(user, token)
	if err != nil || payload.Snapshot.WorkspaceID != workspaceID || payload.Snapshot.PrincipalID != user.Id {
		return false
	}
	snapshot, ok := workspaceChangeSnapshotOf(app, user.Id, workspaceID)
	if !ok {
		return false
	}
	listed := payload.Snapshot
	listed.AccessRevision = snapshot.AccessRevision
	return listed == snapshot
}

// workspaceChangeSnapshotOf is the workspace's change state for the person,
// a member's or a guest's, as the change check reads it.
func workspaceChangeSnapshotOf(app core.App, principalID, workspaceID string) (workspaceChangeSnapshot, bool) {
	snapshots, err := readWorkspaceChangeSnapshots(app, principalID, []string{workspaceID})
	if isPermissionDenied(err) {
		guest, guestErr := readGuestWorkspaceChangeSnapshot(app, principalID, workspaceID, true)
		return guest, guestErr == nil
	}
	if err != nil {
		return workspaceChangeSnapshot{}, false
	}
	snapshot, ok := snapshots[workspaceID]
	return snapshot, ok
}

// pageMovedInList reports whether a change moved the page in the list.
func pageMovedInList(before, after *core.Record) bool {
	if before == nil || after == nil {
		return true
	}
	for _, field := range []string{"workspace", "parent_id", "parent_type", "teamspace_id", "private_owner_id", "in_trash", "archived_at", "kind", "data_source_id"} {
		if before.GetString(field) != after.GetString(field) {
			return true
		}
	}
	return false
}

// applyPageMetadataInApp is the app's own page change: applyPageMetadataWithLock's,
// with the parents its place change edits noted as edited, in one transaction.
func applyPageMetadataInApp(
	app core.App, actorID, pageID string, input pageMetadataInput, requestHash string,
) (pageMetadataResponse, pageMetadataMutationStatus, error) {
	var response pageMetadataResponse
	status := pageMetadataApplied
	err := app.RunInTransaction(func(txApp core.App) error {
		before := pageHeldPlaceOf(txApp, pageID)
		var err error
		response, status, err = applyPageMetadataWithLock(txApp, actorID, pageID, input, requestHash, pageLockEnforced)
		if err != nil || status != pageMetadataApplied || response.Receipt.Duplicate {
			return err
		}
		return notePlaceEditedParents(txApp, actorID, pageID, before, input.KeepLastEdit)
	})
	return response, status, err
}
