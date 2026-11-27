package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// advanceBlockCopyForward gives the copied databases their identities, then
// copies the blocks, which show those identities, then the databases' data.
func advanceBlockCopyForward(app core.App, job storedBlockMoveJob) (bool, error) {
	if err := initializeBlockCopyDatabaseLane(app, job); err != nil {
		return false, err
	}
	if state, found, err := nextBlockCopyDatabaseState(app, job.ID, false); err != nil || found {
		if err != nil {
			return false, err
		}
		return false, mapBlockCopyDatabaseWindow(app, job, state)
	}
	items := []storedBlockMoveItem{}
	if err := app.DB().NewQuery(`
		SELECT [[item]].[[job_id]],[[item]].[[block_id]],[[item]].[[target_block_id]],
			[[item]].[[parent_id]],[[item]].[[position]],[[item]].[[depth]],
			[[item]].[[discovery_cursor]],[[item]].[[discovered]],[[item]].[[moved]]
		FROM {{leafen_block_move_items}} [[item]]
		WHERE [[item]].[[job_id]]={:jobId} AND [[item]].[[moved]]=0 AND (
			[[item]].[[depth]]=0 OR EXISTS (
				SELECT 1 FROM {{leafen_block_move_items}} [[parent]]
				WHERE [[parent]].[[job_id]]=[[item]].[[job_id]]
					AND [[parent]].[[block_id]]=[[item]].[[parent_id]]
					AND [[parent]].[[moved]]=1
			)
		)
		ORDER BY [[item]].[[depth]],[[item]].[[position]],[[item]].[[block_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": job.ID, "limit": maxBlockMoveItemsPerTurn + 1,
	}).All(&items); err != nil {
		return false, err
	}
	if len(items) > 0 {
		if len(items) > maxBlockMoveItemsPerTurn {
			items = items[:maxBlockMoveItemsPerTurn]
		}
		if err := copyBlockCopyWindow(app, job, items); err != nil {
			return false, err
		}
		return false, updateBlockCopyProgress(app, job.ID, len(items), false)
	}
	state, found, err := nextBlockCopyDatabaseState(app, job.ID, true)
	if err != nil {
		return false, err
	}
	if !found {
		// The copied databases take their templates and settings once their
		// data is copied.
		if err := copyBlockCopyTaskFeatures(app, job); err != nil {
			return false, err
		}
		return true, copyDatabaseTemplates(
			app, job.ID, job.sourceOwner(), job.sourceWorkspace(), job.OwnerID, job.WorkspaceID, job.ActorID,
			job.SourcePageID, job.DestinationPageID,
		)
	}
	return false, copyBlockCopyDatabaseWindow(app, job, state)
}

func advanceBlockCopyRollback(app core.App, job storedBlockMoveJob) error {
	items := []storedBlockMoveItem{}
	if err := app.DB().NewQuery(`
		SELECT [[item]].[[job_id]],[[item]].[[block_id]],[[item]].[[target_block_id]],
			[[item]].[[parent_id]],[[item]].[[position]],[[item]].[[depth]],
			[[item]].[[discovery_cursor]],[[item]].[[discovered]],[[item]].[[moved]]
		FROM {{leafen_block_move_items}} [[item]]
		WHERE [[item]].[[job_id]]={:jobId} AND [[item]].[[moved]]=1
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_block_move_items}} [[child]]
				WHERE [[child]].[[job_id]]=[[item]].[[job_id]]
					AND [[child]].[[parent_id]]=[[item]].[[block_id]]
					AND [[child]].[[moved]]=1
			)
		ORDER BY [[item]].[[depth]] DESC,[[item]].[[position]] DESC,[[item]].[[block_id]] DESC
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": job.ID, "limit": maxBlockMoveItemsPerTurn + 1,
	}).All(&items); err != nil {
		return err
	}
	if len(items) == 0 {
		// The copied databases go once no copied block shows them.
		done, err := rollbackBlockCopyDatabaseLane(app, job)
		if err != nil || !done {
			if err != nil {
				return err
			}
			return rescheduleBlockMove(app, job)
		}
		return failBlockMove(app, job)
	}
	if len(items) > maxBlockMoveItemsPerTurn {
		items = items[:maxBlockMoveItemsPerTurn]
	}
	if err := rollbackBlockCopyWindow(app, job, items); err != nil {
		return err
	}
	if err := updateBlockCopyProgress(app, job.ID, len(items), true); err != nil {
		return err
	}
	var remaining int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_block_move_items}}
		WHERE [[job_id]]={:jobId} AND [[moved]]=1
	`).Bind(dbx.Params{"jobId": job.ID}).Row(&remaining); err != nil {
		return err
	}
	if remaining == 0 {
		// Every copied block is gone: the page's history keeps it so.
		if err := recordBlockMoveHistory(app, job); err != nil {
			return err
		}
		job.MovedCount = 0
	}
	return rescheduleBlockMove(app, job)
}

func copyBlockCopyWindow(
	app core.App,
	job storedBlockMoveJob,
	items []storedBlockMoveItem,
) error {
	if len(items) == 0 || len(items) > maxBlockMoveItemsPerTurn {
		return fmt.Errorf("block copy window size %d: %w", len(items), errBlockMoveConflict)
	}
	source, err := loadPageDuplicationStructuralDocument(app, job.SourcePageID)
	if err != nil {
		return err
	}
	// A body written with its page, before anyone's change, has its structure
	// from its stored blocks, as a change to it reads it
	// (loadStructuralMutationBatch): a page made with blocks through the
	// Public API, copied, had none, and its copy failed (2026-11-14).
	if len(source.GetMap(ygoBlockMetadataRoot).Keys()) == 0 {
		if err := seedStructuralGraphDocument(app, job.SourcePageID, source); err != nil {
			return err
		}
	}
	structures := newBlockMoveStructures()
	destination, err := structures.load(app, job.DestinationPageID)
	if err != nil {
		return err
	}
	sourceText, targetText, err := loadBlockMoveTextDocuments(
		app, job.SourcePageID, job.DestinationPageID,
	)
	if err != nil {
		return err
	}
	// The copied blocks' own metadata, not the page's every block's; the
	// destination's orders kept through the window (block_move_orders.go).
	metadata := newStructuralMetadataView(source)
	target := newBlockMoveDocument(destination.doc)
	placement, err := loadBlockMovePlacement(app, job.ID)
	if err != nil {
		return err
	}
	affected := map[string]struct{}{}
	containers := []string{}
	for _, item := range items {
		entry, exists, err := metadata.get(item.BlockID)
		if err != nil {
			return err
		}
		if !exists || item.TargetBlockID == "" {
			return errBlockMoveConflict
		}
		// A sub-page's block keeps naming the sub-page: it is the link its
		// copy takes the place of (child_page_block_copies.go).
		childPage := isChildPageBlock(item.BlockID, entry)
		entry.Content, err = remapBlockCopyContent(app, job.ID, entry.Content, !childPage)
		if err != nil {
			return err
		}
		// A copied toggle starts closed: a button's inserted toggle too (2026-11-13).
		closeCopiedToggle(entry.Type, entry.Content)
		if childPage {
			// A copy of a sub-page's block links to that page until the
			// page's own copy takes its place.
			entry.Type = graphBlockTypeLinkToPage
		}
		targetParentID, err := blockCopyTargetParentID(app, job.ID, item)
		if err != nil {
			return err
		}
		afterID, err := target.last(targetParentID)
		if err != nil {
			return err
		}
		if item.Depth == 0 && placement.CopyChildrenParent != "" {
			targetParentID, afterID, err = nativeBlockCopyRootSpot(app, job, destination.doc, placement)
			if err != nil {
				return err
			}
		}
		if item.Depth == 0 && placement.CopyPlaced {
			targetParentID, afterID, err = pastedBlockCopySpot(destination.doc, placement, targetParentID, afterID)
			if err != nil {
				return err
			}
		}
		duplicate := item.Depth == 0 && job.SourcePageID == job.DestinationPageID &&
			placement.CopyChildrenParent == "" && !placement.CopyPlaced
		if duplicate {
			// A copy on its own page is a duplicate: it sits right after the
			// original, under the same parent.
			sourceID := item.BlockID
			targetParentID, afterID = entry.ParentID, &sourceID
		}
		if err := target.attach(item.TargetBlockID, targetParentID, afterID, entry); err != nil {
			return err
		}
		content, err := json.Marshal(entry.Content)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_blocks}}
				([[id]],[[owner]],[[workspace_id]],[[page_id]],[[parent_id]],[[position]],
				 [[type]],[[content]],[[created_by]],[[last_edited_by]],[[created]],[[updated]])
			VALUES ({:id},{:ownerId},{:workspaceId},{:pageId},{:parentId},'pending',
				{:type},{:content},{:actorId},{:actorId},{:created},{:updated})
		`).Bind(dbx.Params{
			"id": item.TargetBlockID, "ownerId": job.OwnerID, "workspaceId": job.WorkspaceID,
			"pageId": job.DestinationPageID, "parentId": targetParentID,
			"type": entry.Type, "content": string(content), "actorId": job.ActorID,
			"created": now, "updated": now,
		}).Execute(); err != nil {
			return err
		}
		if err := recordWorkspaceCopiedBlock(app, job.ActorID, job.ID, job.DestinationPageID, item.TargetBlockID, entry.Type, now, true); err != nil {
			return err
		}
		block := &graphBlock{ID: item.TargetBlockID, Type: entry.Type, Content: content}
		if err := replaceProductFileBlockReference(
			app, job.WorkspaceID, job.DestinationPageID, block,
		); err != nil {
			return err
		}
		if err := copyBlockCopyText(app, job.ID, sourceText, targetText, item.BlockID, item.TargetBlockID); err != nil {
			return err
		}
		affected[targetParentID] = struct{}{}
		if entry.Type == graphBlockTypeInlineDatabase {
			containers = append(containers, item.TargetBlockID)
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_block_move_items}}
			SET [[moved]]=1,[[updated_at]]={:updatedAt}
			WHERE [[job_id]]={:jobId} AND [[block_id]]={:blockId} AND [[moved]]=0
		`).Bind(dbx.Params{
			"updatedAt": now, "jobId": job.ID, "blockId": item.BlockID,
		}).Execute(); err != nil {
			return err
		}
	}
	if err := saveBlockCopyDestination(app, job, structures, targetText, affected, false, !target.reachesPastPage, len(items)); err != nil {
		return err
	}
	if err := projectBlockCopyDatabaseContainers(app, job, containers); err != nil {
		return err
	}
	return captureNativeBlockCopyUndoWindow(app, job, items)
}

// projectBlockCopyDatabaseContainers shows each copied database block's data
// sources once the block has its place on the destination page.
func projectBlockCopyDatabaseContainers(app core.App, job storedBlockMoveJob, blockIDs []string) error {
	for _, blockID := range blockIDs {
		block := struct {
			ParentID string `db:"parent_id"`
			Position string `db:"position"`
			Content  string `db:"content"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[parent_id]],[[position]],[[content]] FROM {{leafen_blocks}}
			WHERE [[owner]]={:ownerId} AND [[page_id]]={:pageId} AND [[id]]={:blockId}
		`).Bind(dbx.Params{
			"ownerId": job.OwnerID, "pageId": job.DestinationPageID, "blockId": blockID,
		}).One(&block); err != nil {
			return err
		}
		content, err := parseDatabaseContainerBlockContent(json.RawMessage(block.Content))
		if err != nil {
			return errBlockMoveConflict
		}
		if err := projectDatabaseContainerBlock(
			app, job.OwnerID, job.WorkspaceID, job.DestinationPageID,
			blockID, block.ParentID, block.Position, content,
		); err != nil {
			return err
		}
	}
	return nil
}

func rollbackBlockCopyWindow(
	app core.App,
	job storedBlockMoveJob,
	items []storedBlockMoveItem,
) error {
	structures := newBlockMoveStructures()
	destination, err := structures.load(app, job.DestinationPageID)
	if err != nil {
		return err
	}
	_, targetText, err := loadBlockMoveTextDocuments(app, job.SourcePageID, job.DestinationPageID)
	if err != nil {
		return err
	}
	affected := map[string]struct{}{}
	target := newBlockMoveDocument(destination.doc)
	for _, item := range items {
		entry, exists, err := target.metadata.get(item.TargetBlockID)
		if err != nil {
			return err
		}
		if !exists {
			return errBlockMoveConflict
		}
		if err := target.detach(item.TargetBlockID, entry.ParentID); err != nil {
			return err
		}
		if err := deleteProductFileBlockReferences(
			app, job.WorkspaceID, job.DestinationPageID, []string{item.TargetBlockID},
		); err != nil {
			return err
		}
		if entry.Type == graphBlockTypeInlineDatabase {
			if err := deleteDatabaseContainerProjection(app, item.TargetBlockID); err != nil {
				return err
			}
		}
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_blocks}}
			WHERE [[owner]]={:ownerId} AND [[page_id]]={:pageId} AND [[id]]={:blockId}
		`).Bind(dbx.Params{
			"ownerId": job.OwnerID, "pageId": job.DestinationPageID,
			"blockId": item.TargetBlockID,
		}).Execute(); err != nil {
			return err
		}
		text := targetText.GetText(ygoBlockTextRootPrefix + item.TargetBlockID)
		if text.Len() > 0 {
			targetText.Transact(func(transaction *crdt.Transaction) {
				text.Delete(transaction, 0, text.Len())
			})
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_block_move_items}}
			SET [[moved]]=0,[[updated_at]]={:updatedAt}
			WHERE [[job_id]]={:jobId} AND [[block_id]]={:blockId} AND [[moved]]=1
		`).Bind(dbx.Params{
			"updatedAt": now, "jobId": job.ID, "blockId": item.BlockID,
		}).Execute(); err != nil {
			return err
		}
		affected[entry.ParentID] = struct{}{}
	}
	return saveBlockCopyDestination(app, job, structures, targetText, affected, true, !target.reachesPastPage, len(items))
}

// remapBlockCopyContent points a copied block's references to other copied
// blocks, to the copied databases' sources and views and, with pages, to the
// copied pages and rows (block_copy_page_links.go).
func remapBlockCopyContent(
	app core.App,
	jobID string,
	content map[string]any,
	pages bool,
) (map[string]any, error) {
	references := pageDuplicationReferenceSet{
		pageIDs: map[string]struct{}{}, blockIDs: map[string]struct{}{},
		dataSourceIDs: map[string]struct{}{}, viewIDs: map[string]struct{}{},
	}
	collectPageDuplicationReferences(content, false, &references)
	blocks, err := pageDuplicationReferenceMappings(app, jobID, "block", referenceIDs(references.blockIDs))
	if err != nil {
		return nil, err
	}
	dataSources, err := pageDuplicationReferenceMappings(app, jobID, "data_source", referenceIDs(references.dataSourceIDs))
	if err != nil {
		return nil, err
	}
	views, err := pageDuplicationReferenceMappings(app, jobID, "view", referenceIDs(references.viewIDs))
	if err != nil {
		return nil, err
	}
	pageMappings := map[string]string{}
	if pages {
		if pageMappings, err = pageDuplicationMentionTargetMappings(app, jobID, referenceIDs(references.pageIDs)); err != nil {
			return nil, err
		}
	}
	rewritePageDuplicationReferences(content, false, pageMappings, blocks, dataSources, views)
	// A synced copy of an original copied with it shows the original's copy
	// (synced_block_duplication.go).
	originals, err := copiedSyncedOriginals(app, jobID, []map[string]any{content})
	if err != nil {
		return nil, err
	}
	pointSyncedCopyAtCopiedOriginal(content, originals)
	return content, nil
}

// pastedBlockCopySpot is where a paste's copy lands: right after the block
// the paste put it after, under that block's parent, while it stands there;
// at the page's end otherwise, where the paste moves it. Landing at the end
// first, it showed there to everyone on the page before it went to its place
// (2026-11-08).
func pastedBlockCopySpot(doc *crdt.Doc, placement blockMovePlacement, parentID string, end *string) (string, *string, error) {
	// A template body's database first under its parent (database_template_body_databases.go).
	if placement.CopyAfter == blockCopyPlaceFirst {
		return placement.CopyParent, nil, nil
	}
	order, err := structuralOrder(doc, placement.CopyParent)
	if err != nil {
		return "", nil, err
	}
	if structuralIndex(order, placement.CopyAfter) < 0 {
		return parentID, end, nil
	}
	after := placement.CopyAfter
	return placement.CopyParent, &after, nil
}

func blockCopyTargetParentID(
	app core.App,
	jobID string,
	item storedBlockMoveItem,
) (string, error) {
	if item.Depth == 0 {
		return "", nil
	}
	var targetParentID string
	err := app.DB().NewQuery(`
		SELECT [[target_block_id]] FROM {{leafen_block_move_items}}
		WHERE [[job_id]]={:jobId} AND [[block_id]]={:blockId} AND [[moved]]=1
	`).Bind(dbx.Params{"jobId": jobID, "blockId": item.ParentID}).Row(&targetParentID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errBlockMoveConflict
	}
	return targetParentID, err
}

func copyBlockCopyText(app core.App, jobID string, sourceText, targetText *crdt.Doc, sourceID, targetID string) error {
	source := sourceText.GetText(ygoBlockTextRootPrefix + sourceID)
	target := targetText.GetText(ygoBlockTextRootPrefix + targetID)
	if target.Len() != 0 {
		return errBlockMoveConflict
	}
	// Read before the change: a read takes its document's lock, which
	// Transact holds (YText.ToDelta). Its mentions name the copies the copy
	// made (block_copy_page_links.go).
	delta, err := remapBlockCopyTextDelta(app, jobID, source.ToDelta())
	if err != nil {
		return err
	}
	targetText.Transact(func(transaction *crdt.Transaction) {
		target.ApplyDelta(transaction, delta)
	})
	return nil
}

func saveBlockCopyDestination(
	app core.App,
	job storedBlockMoveJob,
	structures *blockMoveStructures,
	targetText *crdt.Doc,
	affected map[string]struct{},
	rollback, blocksOnly bool,
	count int,
) error {
	destination := structures.batches[job.DestinationPageID]
	for parentID := range affected {
		if err := publishStructuralOrderProjection(
			app, destination.doc, job.OwnerID, job.DestinationPageID, parentID, job.ActorID,
		); err != nil {
			return err
		}
	}
	// The databases the new order moved, a duplicated one's original among
	// them, stand where their blocks now do.
	if err := refreshDatabaseContainerPlacements(app, job.OwnerID, job.DestinationPageID); err != nil {
		return err
	}
	update := crdt.EncodeChangesSinceV1(destination.doc, destination.mark)
	if len(update) == 0 || len(update) > maxYgoUpdateBytes {
		return errBlockMoveConflict
	}
	if err := appendYgoStructuralUpdate(app, job.DestinationPageID, update); err != nil {
		return err
	}
	if err := structures.keep(app, job.DestinationPageID, update); err != nil {
		return err
	}
	if err := recordBlockMoveStructuralChange(
		app, job, job.DestinationPageID, update, rollback, count, blocksOnly,
	); err != nil {
		return err
	}
	if err := markSearchDirtyByPageID(app, job.DestinationPageID); err != nil {
		return err
	}
	// Its history keeps the copied blocks once the copy ends
	// (recordBlockMoveHistory).
	return replaceYgoSnapshot(
		app, job.DestinationPageID, crdt.EncodeStateAsUpdateV1(targetText, nil),
	)
}

func updateBlockCopyProgress(app core.App, jobID string, count int, rollback bool) error {
	delta := count
	if rollback {
		delta = -count
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_block_move_jobs}}
		SET [[moved_count]]=[[moved_count]]+{:delta},[[revision]]=[[revision]]+1,
			[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId}
			AND [[phase]] IN ('moving','rolling_back')
	`).Bind(dbx.Params{
		"delta": delta, "updatedAt": time.Now().UTC().UnixMilli(), "jobId": jobID,
	}).Execute()
	return err
}
