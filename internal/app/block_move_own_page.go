package app

import (
	"context"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// moveBlockToOwnPageEnd moves a block to the end of the page it is on, as
// Notion's block move does when its own page is chosen (seen 2026-11-10):
// with what it holds, after the page's last top block, in one change made
// at once. It is kept as a finished move, so the same request again finds
// it done rather than moving the block once more. A block already last on
// the page stays.
func moveBlockToOwnPageEnd(
	app core.App,
	actorID, ownerID, workspaceID, pageID, blockID string,
	input blockMoveInput, requestHash string,
) (storedBlockMoveJob, error) {
	active, err := blockMovePageFenceActive(app, pageID)
	if err != nil {
		return storedBlockMoveJob{}, err
	}
	if active {
		return storedBlockMoveJob{}, errBlockMoveConflict
	}
	batch, err := loadStructuralMutationBatch(app, pageID, map[string]*structuralMutationBatch{})
	if err != nil {
		return storedBlockMoveJob{}, err
	}
	entry, exists, err := newStructuralMetadataView(batch.doc).get(blockID)
	if err != nil {
		return storedBlockMoveJob{}, err
	}
	if !exists {
		return storedBlockMoveJob{}, errBlockMoveNotFound
	}
	last, err := blockMoveLastStructuralID(batch.doc, "")
	if err != nil {
		return storedBlockMoveJob{}, err
	}
	if last != nil && (entry.ParentID != "" || *last != blockID) {
		operations := []graphOperation{{Kind: graphOperationKindMoveBlock, Move: &graphBlockMove{BlockID: blockID, AfterID: last}}}
		history, err := beginServerPageHistoryCapture(app, pageID, batch.doc, operations)
		if err != nil {
			return storedBlockMoveJob{}, err
		}
		if _, err := applyStructuralGraphMutation(
			app, batch.doc, ownerID, workspaceID, pageID, actorID, operations, false, nil,
		); err != nil {
			return storedBlockMoveJob{}, err
		}
		if err := saveServerStructuralBatch(app, ownerID, pageID, actorID, "block-move-own-page:"+blockID, batch, history); err != nil {
			return storedBlockMoveJob{}, err
		}
		// A change to its blocks, which edits the page (block_move_page_edits.go).
		if err := notePageContentEdited(context.Background(), app, pageID, actorID); err != nil {
			return storedBlockMoveJob{}, err
		}
	}
	now := time.Now().UTC().UnixMilli()
	job := storedBlockMoveJob{
		ID: core.GenerateDefaultRandomId(), ActorID: actorID, OwnerID: ownerID, WorkspaceID: workspaceID,
		SourcePageID: pageID, DestinationPageID: pageID, RootBlockID: blockID, TargetRootBlockID: blockID,
		Operation: blockMoveOperationMove, RequestHash: requestHash, Status: durableJobStatusCompleted,
		Phase: blockMovePhaseCompleted, Revision: 1, DiscoveredCount: 1, MovedCount: 1,
		CreatedAt: now, UpdatedAt: now, CompletedAt: now,
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_jobs}}
			([[id]],[[owner]],[[workspace_id]],[[request_id]],[[request_hash]],[[kind]],[[page_id]],
			 [[status]],[[error_code]],[[diagnostic_reference]],[[not_before]],[[created]],[[updated]],
			 [[payload_json]],[[cursor]],[[processed_items]],[[total_items]],[[attempt]],[[max_attempts]],
			 [[chunk_count]],[[captured_bytes]])
		VALUES
			({:id},{:owner},{:workspaceId},{:requestId},{:requestHash},{:kind},{:pageId},
			 'completed','','',{:now},{:now},{:now},'{}','',1,1,0,{:maxAttempts},1,0)
	`).Bind(dbx.Params{
		"id": job.ID, "owner": actorID, "workspaceId": workspaceID,
		"requestId": input.RequestID, "requestHash": requestHash, "kind": blockMoveJobKind,
		"pageId": pageID, "now": now, "maxAttempts": blockMoveDefaultRetries,
	}).Execute(); err != nil {
		return storedBlockMoveJob{}, err
	}
	if _, err := app.DB().Insert(blockMoveJobsTable, dbx.Params{
		"job_id": job.ID, "actor_id": actorID, "owner_id": ownerID, "workspace_id": workspaceID,
		"source_page_id": pageID, "destination_page_id": pageID, "root_block_id": blockID,
		"target_root_block_id": blockID, "operation": job.Operation, "request_hash": requestHash,
		"phase": job.Phase, "revision": job.Revision, "discovered_count": job.DiscoveredCount,
		"moved_count": job.MovedCount, "error_code": "", "created_at": now, "updated_at": now,
		"completed_at": now,
	}).Execute(); err != nil {
		return storedBlockMoveJob{}, err
	}
	return job, nil
}
