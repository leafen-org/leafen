package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
	"strconv"
)

// Turning a sub-page back into a block is asked for explicitly: a native
// history restore, or "전환" on the page's block (Notion's, seen
// 2026-10-14). A change that only retypes a page's block is refused. The
// person must be able to write both bodies; the page and its body stay
// recoverable.
func prepareChildPageConversionRestore(
	app core.App, actorID string, parent pageAuthorization, doc *crdt.Doc, pageID string,
) error {
	page, err := authorizePageBody(app, actorID, pageID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	if page.Page.GetString("parent_id") != parent.Page.Id ||
		page.Workspace.WorkspaceID != parent.Workspace.WorkspaceID {
		return errChildPageBlockInvalid
	}
	if locked, err := pageLockedFor(app, page.Page, actorID); err != nil {
		return err
	} else if locked {
		return pageLockedError()
	}
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return err
	}
	entry := metadata[pageID]
	order, err := structuralOrder(doc, entry.ParentID)
	if err != nil {
		return err
	}
	after := ""
	if index := structuralIndex(order, pageID); index > 0 {
		after = order[index-1]
	}
	return rememberChildPageBlockPlace(app, pageID, parent.Workspace.WorkspaceID, parent.Page.Id, entry.ParentID, after)
}

func restoreChildPageConversion(app core.App, actorID, parentID, pageID, mutationID string) (string, error) {
	var count int
	if err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId} LIMIT 1)`).
		Bind(dbx.Params{"pageId": pageID}).Row(&count); err != nil {
		return "", err
	}
	jobID := ""
	// Apply the metadata lifecycle before the move's fences deny ordinary
	// page writes. Both actions are still inside the graph transaction.
	if err := setChildPageTrash(app, actorID, pageID, true, mutationID); err != nil {
		return "", err
	}
	if count == 0 {
		// No body to move: its discussions go to the block at once.
		if err := moveConvertedPageComments(app, pageID, parentID, true); err != nil {
			return "", err
		}
	}
	if count > 0 {
		admission, err := admitPageBodyConversionReturn(app, actorID, pageID, blockMoveInput{
			RequestID: "page-block-return:" + childPageBlockDigest(mutationID, pageID), DestinationPageID: parentID,
		})
		if err != nil {
			return "", err
		}
		jobID = admission.Job.ID
	}
	// The restored block is already in the parent projection, so the page's
	// usual trash lifecycle leaves it alone. A durable move holds both bodies
	// and brings every current child back without a second text snapshot.
	return jobID, nil
}

// A failed return keeps the current body on its original page. If the
// writer lost permission, keep it recoverable in the trash instead of
// bypassing the new authority or holding both pages' fences indefinitely.
func recoverChildPageConversion(app core.App, job storedBlockMoveJob) error {
	if err := setChildPageTrash(app, job.ActorID, job.SourcePageID, false, "page-block-return-failed:"+job.ID); err != nil {
		if isPermissionDenied(err) {
			return nil
		}
		return err
	}
	return nil
}

// Restoring the page from the trash also reverses its conversion Undo.
// Native Redo has already restored the page block and admits its own move;
// an ordinary trash restore finds the original block here instead. Redo
// brings the page back inside its change, before its parent's stored body
// shows the page block, which the parent's blocks already name a page block
// (2026-11-16: read from the stored body, a redo moved the block's body
// twice and was refused with a 500).
func restoreConvertedChildPageBlock(app core.App, ownerID, workspaceID, parentID, pageID, actorID string) error {
	if parentID == "" {
		return nil
	}
	batch, err := loadStructuralMutationBatch(app, parentID, map[string]*structuralMutationBatch{})
	if err != nil {
		return err
	}
	metadata, err := structuralMetadata(batch.doc)
	if err != nil {
		return err
	}
	holder, held, err := childPageBlockHolder(app, ownerID, pageID)
	if err != nil {
		return err
	}
	current, exists := metadata[pageID]
	converted := exists && !isChildPageBlock(pageID, current) && !(held && holder == parentID)
	if err := restoreChildPageBlock(app, ownerID, workspaceID, parentID, pageID, actorID, converted); err != nil || !converted {
		return err
	}
	if err := moveConvertedPageComments(app, pageID, parentID, false); err != nil {
		return err
	}
	batch, err = loadStructuralMutationBatch(app, parentID, map[string]*structuralMutationBatch{})
	if err != nil {
		return err
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	_, err = moveChildPageBlockChildren(app, actorID, parentID, batch.doc,
		"page-trash-restore:"+strconv.Itoa(page.GetInt("cursor")), []string{pageID})
	return err
}
