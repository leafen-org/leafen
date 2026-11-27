package app

import (
	"fmt"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func moveBlockMoveWindow(
	app core.App,
	job storedBlockMoveJob,
	items []storedBlockMoveItem,
) error {
	return transferBlockMoveItems(app, job, items, false)
}

func rollbackBlockMoveWindow(
	app core.App,
	job storedBlockMoveJob,
	items []storedBlockMoveItem,
) error {
	return transferBlockMoveItems(app, job, items, true)
}

func transferBlockMoveItems(
	app core.App,
	job storedBlockMoveJob,
	items []storedBlockMoveItem,
	rollback bool,
) error {
	if len(items) == 0 || len(items) > maxBlockMoveItemsPerTurn {
		return fmt.Errorf("block move window size %d: %w", len(items), errBlockMoveConflict)
	}
	// Both pages' structures as loadStructuralMutationBatch reads them, the
	// last window's documents taken up again (block_move_structure.go).
	structures := newBlockMoveStructures()
	source, err := structures.load(app, job.SourcePageID)
	if err != nil {
		return err
	}
	destination, err := structures.load(app, job.DestinationPageID)
	if err != nil {
		return err
	}
	sourceText, destinationText, err := loadBlockMoveTextDocuments(
		app, job.SourcePageID, job.DestinationPageID,
	)
	if err != nil {
		return err
	}
	affected := map[string]map[string]struct{}{
		job.SourcePageID: {}, job.DestinationPageID: {},
	}
	sourceDocument, destinationDocument := newBlockMoveDocument(source.doc), newBlockMoveDocument(destination.doc)
	places := map[string]string{}
	placement := blockMovePlacement{}
	if !rollback {
		placement, err = loadBlockMovePlacement(app, job.ID)
		if err != nil {
			return err
		}
	}
	for _, item := range items {
		if rollback {
			err = rollbackBlockMoveItem(
				app, job, item, sourceDocument, destinationDocument, sourceText, destinationText, affected, places,
			)
		} else {
			err = forwardBlockMoveItem(
				app, job, item, sourceDocument, destinationDocument, sourceText, destinationText, affected, placement,
			)
		}
		if err != nil {
			return err
		}
	}
	for pageID, parents := range affected {
		batch := structures.batches[pageID]
		for parentID := range parents {
			// Blocks moving back from inside a block that already went back
			// keep their places until they go: no one reads them there, and
			// placed again each window, every one left was rewritten.
			if rollback && pageID == job.DestinationPageID && parentID != "" {
				if _, present, err := destinationDocument.metadata.get(parentID); err != nil {
					return err
				} else if !present {
					continue
				}
			}
			if err := publishStructuralOrderProjection(
				app, batch.doc, job.OwnerID, pageID, parentID, job.ActorID,
			); err != nil {
				return err
			}
		}
		if err := refreshDatabaseContainerPlacements(app, job.OwnerID, pageID); err != nil {
			return err
		}
	}
	// Both pages' changes say their blocks alone unless the window moved a
	// block reaching past them (block_move_graph_change.go).
	blocksOnly := !sourceDocument.reachesPastPage && !destinationDocument.reachesPastPage
	if err := saveBlockMoveStructuralBatches(app, job, structures, rollback, len(items), blocksOnly); err != nil {
		return err
	}
	if err := replaceYgoSnapshot(
		app, job.SourcePageID, crdt.EncodeStateAsUpdateV1(sourceText, nil),
	); err != nil {
		return err
	}
	if err := replaceYgoSnapshot(
		app, job.DestinationPageID, crdt.EncodeStateAsUpdateV1(destinationText, nil),
	); err != nil {
		return err
	}
	// Both pages' histories keep the blocks once the move ends
	// (recordBlockMoveHistory).
	return nil
}

func forwardBlockMoveItem(
	app core.App,
	job storedBlockMoveJob,
	item storedBlockMoveItem,
	source, destination *blockMoveDocument,
	sourceText, destinationText *crdt.Doc,
	affected map[string]map[string]struct{},
	placement blockMovePlacement,
) error {
	// The block's own metadata, not the page's every block's (2026-10-15:
	// read whole for every moved block, a move out of a long page took the
	// square of its length).
	entry, exists, err := source.metadata.get(item.BlockID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("forward block %s missing source metadata: %w", item.BlockID, errBlockMoveConflict)
	}
	if err := source.detach(item.BlockID, entry.ParentID); err != nil {
		return err
	}
	parentID, afterID, err := blockMoveDestinationSpot(app, job, item, destination, placement)
	if err != nil {
		return err
	}
	if err := destination.attach(item.BlockID, parentID, afterID, entry); err != nil {
		return err
	}
	if err := moveBlockMoveText(sourceText, destinationText, item.BlockID); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_blocks}}
		SET [[page_id]]={:destinationPageId},[[parent_id]]={:parentId},
			[[last_edited_by]]={:actorId},[[updated]]={:updated}
		WHERE [[owner]]={:ownerId} AND [[page_id]]={:sourcePageId} AND [[id]]={:blockId}
	`).Bind(dbx.Params{
		"destinationPageId": job.DestinationPageID, "parentId": parentID, "actorId": job.ActorID,
		"updated": now, "ownerId": job.OwnerID, "sourcePageId": job.SourcePageID,
		"blockId": item.BlockID,
	}).Execute(); err != nil {
		return err
	}
	if err := moveDatabaseContainerProjection(
		app, job.OwnerID, job.SourcePageID, job.DestinationPageID, item.BlockID, parentID,
	); err != nil {
		return err
	}
	if err := moveLinkedDatabaseViews(app, job.OwnerID, job.WorkspaceID,
		job.SourcePageID, job.DestinationPageID, item.BlockID); err != nil {
		return err
	}
	if err := moveBlockMoveComments(app, job, item.BlockID, false); err != nil {
		return err
	}
	if err := moveBlockMoveFileReferences(app, job, item.BlockID, false); err != nil {
		return err
	}
	if isChildPageBlock(item.BlockID, entry) {
		if err := reparentChildPage(
			app, job.OwnerID, job.WorkspaceID, job.ActorID, item.BlockID, job.DestinationPageID,
		); err != nil {
			return err
		}
	}
	if err := moveMentionAnchoredPages(app, job.OwnerID, job.WorkspaceID, job.ActorID, item.BlockID, job.SourcePageID, job.DestinationPageID); err != nil {
		return err
	}
	if err := adoptBlockMoveChildren(app, job, item, destination, affected); err != nil {
		return err
	}
	if err := recordWorkspaceMovedBlock(app, job, item.BlockID, entry.Type, now); err != nil {
		return err
	}
	affected[job.SourcePageID][entry.ParentID] = struct{}{}
	affected[job.DestinationPageID][parentID] = struct{}{}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_block_move_items}}
		SET [[moved]]=1,[[updated_at]]={:updated}
		WHERE [[job_id]]={:jobId} AND [[block_id]]={:blockId} AND [[moved]]=0
	`).Bind(dbx.Params{"jobId": job.ID, "blockId": item.BlockID, "updated": now}).Execute()
	return err
}

func rollbackBlockMoveItem(
	app core.App,
	job storedBlockMoveJob,
	item storedBlockMoveItem,
	source, destination *blockMoveDocument,
	sourceText, destinationText *crdt.Doc,
	affected map[string]map[string]struct{},
	places map[string]string,
) error {
	entry, exists, err := destination.metadata.get(item.BlockID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("rollback block %s missing destination metadata: %w", item.BlockID, errBlockMoveConflict)
	}
	if err := destination.detach(item.BlockID, entry.ParentID); err != nil {
		return err
	}
	afterID, err := blockMoveRollbackAnchor(app, job, item, source, places)
	if err != nil {
		return err
	}
	if err := source.attach(item.BlockID, item.ParentID, afterID, entry); err != nil {
		return err
	}
	if err := moveBlockMoveText(destinationText, sourceText, item.BlockID); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_blocks}}
		SET [[page_id]]={:sourcePageId},[[parent_id]]={:parentId},
			[[last_edited_by]]={:actorId},[[updated]]={:updated}
		WHERE [[owner]]={:ownerId} AND [[page_id]]={:destinationPageId} AND [[id]]={:blockId}
	`).Bind(dbx.Params{
		"sourcePageId": job.SourcePageID, "parentId": item.ParentID,
		"actorId": job.ActorID, "updated": now, "ownerId": job.OwnerID,
		"destinationPageId": job.DestinationPageID, "blockId": item.BlockID,
	}).Execute(); err != nil {
		return err
	}
	if err := moveDatabaseContainerProjection(
		app, job.OwnerID, job.DestinationPageID, job.SourcePageID, item.BlockID, item.ParentID,
	); err != nil {
		return err
	}
	if err := moveLinkedDatabaseViews(app, job.OwnerID, job.WorkspaceID,
		job.DestinationPageID, job.SourcePageID, item.BlockID); err != nil {
		return err
	}
	if err := moveBlockMoveComments(app, job, item.BlockID, true); err != nil {
		return err
	}
	if err := moveBlockMoveFileReferences(app, job, item.BlockID, true); err != nil {
		return err
	}
	if isChildPageBlock(item.BlockID, entry) {
		if err := reparentChildPage(
			app, job.OwnerID, job.WorkspaceID, job.ActorID, item.BlockID, job.SourcePageID,
		); err != nil {
			return err
		}
	}
	if err := moveMentionAnchoredPages(app, job.OwnerID, job.WorkspaceID, job.ActorID, item.BlockID, job.DestinationPageID, job.SourcePageID); err != nil {
		return err
	}
	affected[job.DestinationPageID][entry.ParentID] = struct{}{}
	affected[job.SourcePageID][item.ParentID] = struct{}{}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_block_move_items}}
		SET [[moved]]=0,[[updated_at]]={:updated}
		WHERE [[job_id]]={:jobId} AND [[block_id]]={:blockId} AND [[moved]]=1
	`).Bind(dbx.Params{"jobId": job.ID, "blockId": item.BlockID, "updated": now}).Execute()
	return err
}

func blockMoveLastStructuralID(doc *crdt.Doc, parentID string) (*string, error) {
	order, err := structuralOrder(doc, parentID)
	if err != nil || len(order) == 0 {
		return nil, err
	}
	last := order[len(order)-1]
	return &last, nil
}

func loadBlockMoveTextDocuments(
	app core.App,
	sourcePageID, destinationPageID string,
) (*crdt.Doc, *crdt.Doc, error) {
	load := func(pageID string) (*crdt.Doc, error) {
		document, _, err := loadYgoDoc(app, pageID)
		return document, err
	}
	source, err := load(sourcePageID)
	if err != nil {
		return nil, nil, err
	}
	destination, err := load(destinationPageID)
	return source, destination, err
}

func moveBlockMoveText(source, destination *crdt.Doc, blockID string) error {
	sourceText := source.GetText(ygoBlockTextRootPrefix + blockID)
	destinationText := destination.GetText(ygoBlockTextRootPrefix + blockID)
	if destinationText.Len() != 0 {
		return fmt.Errorf("block %s target text is occupied: %w", blockID, errBlockMoveConflict)
	}
	deltas := sourceText.ToDelta()
	destination.Transact(func(transaction *crdt.Transaction) {
		destinationText.ApplyDelta(transaction, deltas)
	})
	if sourceText.Len() > 0 {
		source.Transact(func(transaction *crdt.Transaction) {
			sourceText.Delete(transaction, 0, sourceText.Len())
		})
	}
	return nil
}

func moveBlockMoveComments(
	app core.App,
	job storedBlockMoveJob,
	blockID string,
	rollback bool,
) error {
	from, to := job.SourcePageID, job.DestinationPageID
	if rollback {
		from, to = to, from
	}
	if tableKnown(app, "leafen_file_references") {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_file_references}} SET [[page_id]]={:toPageId}
			WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:fromPageId}
				AND [[owner_kind]]='comment' AND [[owner_id]] IN (
					SELECT [[id]] FROM {{leafen_comments}}
					WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:fromPageId}
						AND [[target_kind]]='page' AND [[target_id]]={:fromPageId}
						AND [[block_id]]={:blockId})
		`).Bind(dbx.Params{
			"toPageId": to, "workspaceId": job.WorkspaceID,
			"fromPageId": from, "blockId": blockID,
		}).Execute(); err != nil {
			return err
		}
	}
	if tableKnown(app, "leafen_comments") {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_comments}} SET [[page_id]]={:toPageId},[[target_id]]={:toPageId}
			WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:fromPageId}
				AND [[target_kind]]='page' AND [[target_id]]={:fromPageId}
				AND [[block_id]]={:blockId}
		`).Bind(dbx.Params{
			"toPageId": to, "workspaceId": job.WorkspaceID,
			"fromPageId": from, "blockId": blockID,
		}).Execute(); err != nil {
			return err
		}
	}
	for _, table := range []string{"leafen_notification_events", "leafen_notifications"} {
		if !tableKnown(app, table) {
			continue
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{` + table + `}} SET [[page_id]]={:toPageId}
			WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:fromPageId}
				AND [[block_id]]={:blockId}
		`).Bind(dbx.Params{
			"toPageId": to, "workspaceId": job.WorkspaceID,
			"fromPageId": from, "blockId": blockID,
		}).Execute(); err != nil {
			return err
		}
	}
	return movePagePersonMentions(app, job.WorkspaceID, job.ActorID, blockID, from, to, !rollback)
}

func moveBlockMoveFileReferences(
	app core.App,
	job storedBlockMoveJob,
	blockID string,
	rollback bool,
) error {
	from, to := job.SourcePageID, job.DestinationPageID
	if rollback {
		from, to = to, from
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_file_references}} SET [[page_id]]={:toPageId}
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:fromPageId}
			AND [[owner_kind]]='block' AND [[owner_id]]={:blockId}
	`).Bind(dbx.Params{
		"toPageId": to, "workspaceId": job.WorkspaceID,
		"fromPageId": from, "blockId": blockID,
	}).Execute()
	return err
}

func saveBlockMoveStructuralBatches(
	app core.App,
	job storedBlockMoveJob,
	structures *blockMoveStructures,
	rollback bool,
	count int,
	blocksOnly bool,
) error {
	pageIDs := []string{job.SourcePageID, job.DestinationPageID}
	sort.Strings(pageIDs)
	for _, pageID := range pageIDs {
		batch := structures.batches[pageID]
		update := crdt.EncodeChangesSinceV1(batch.doc, batch.mark)
		if len(update) == 0 || len(update) > maxYgoUpdateBytes {
			return fmt.Errorf("page %s structural update size %d: %w", pageID, len(update), errBlockMoveConflict)
		}
		if err := appendYgoStructuralUpdate(app, pageID, update); err != nil {
			return err
		}
		if err := structures.keep(app, pageID, update); err != nil {
			return err
		}
		if err := recordBlockMoveStructuralChange(app, job, pageID, update, rollback, count, blocksOnly); err != nil {
			return err
		}
		if err := markSearchDirtyByPageID(app, pageID); err != nil {
			return err
		}
	}
	return nil
}
