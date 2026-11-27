package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A synced copy holds no blocks of its own: what it shows is its original's,
// and blocks appended under it go under the original
// (notionAppendUnderOriginal). Before 2026-11-21 the block API put them under
// the copy, where neither a read of it nor the app showed them. Migration
// 841 puts each copy's own blocks where they are seen, in their order and
// with their last edits: at the end of its original's blocks when the
// original is on the copy's page, as the append now puts them, and else —
// the original on another page, in the trash or gone — right after the copy
// on its page, as ordinary blocks. What brings back blocks as they were
// stored, from before that day, puts them there too
// (applyStoredContentGraphMutation).

type syncedCopyLeftoverHolder struct {
	ID          string `db:"id"`
	Owner       string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
	OriginalID  string `db:"original_id"`
}

type syncedCopyLeftoverEdit struct {
	ID           string `db:"id"`
	LastEditedBy string `db:"last_edited_by"`
	Updated      int64  `db:"updated"`
}

// moveSyncedCopyLeftoverBlocks is migration 841's content.
func moveSyncedCopyLeftoverBlocks(app core.App) error {
	holders, err := loadSyncedCopyLeftoverHolders(app, "")
	if err != nil {
		return err
	}
	moved := 0
	for start := 0; start < len(holders); {
		end := start + 1
		for end < len(holders) && holders[end].PageID == holders[start].PageID {
			end++
		}
		count, err := moveSyncedCopyLeftoversOnPage(app, holders[start:end])
		if err != nil {
			return fmt.Errorf("blocks under the synced copies of page %s: %w", holders[start].PageID, err)
		}
		moved += count
		start = end
	}
	if moved > 0 {
		app.Logger().Info("blocks left under synced copies put where they are seen", "copies", len(holders), "blocks", moved)
	}
	return nil
}

// loadSyncedCopyLeftoverHolders are the synced copies holding blocks of
// their own, on the page or, with none named, on every page.
func loadSyncedCopyLeftoverHolders(app core.App, pageID string) ([]syncedCopyLeftoverHolder, error) {
	onPage := ""
	if pageID != "" {
		onPage = "AND [[copy]].[[page_id]] = {:pageId}"
	}
	holders := []syncedCopyLeftoverHolder{}
	err := app.DB().NewQuery(`
		SELECT [[copy]].[[id]], [[copy]].[[owner]], [[copy]].[[workspace_id]], [[copy]].[[page_id]],
			TRIM(` + notionSyncedCopyOriginalSQL + `) AS [[original_id]]
		FROM {{leafen_blocks}} AS [[copy]]
		WHERE [[copy]].[[type]] = 'synced_block' AND TRIM(COALESCE(` + notionSyncedCopyOriginalSQL + `, '')) <> ''
			` + onPage + ` AND EXISTS (
				SELECT 1 FROM {{leafen_blocks}} AS [[child]] INDEXED BY [[idx_leafen_blocks_page_order]]
				WHERE [[child]].[[owner]] = [[copy]].[[owner]] AND [[child]].[[page_id]] = [[copy]].[[page_id]]
					AND [[child]].[[parent_id]] = [[copy]].[[id]]
			)
		ORDER BY [[copy]].[[page_id]], [[copy]].[[id]]
	`).Bind(dbx.Params{"pageId": pageID}).All(&holders)
	return holders, err
}

// applyStoredContentGraphMutation applies a change bringing back blocks as
// they were stored (contentGraphMutationHooks.StoredBlocks): an import of an
// export, a restore of a trash entry, a revert to a version, which may hold
// blocks a write put under a synced copy before 2026-11-21, where nothing
// shows them. They come back with the rest, and then go where they are
// seen, as migration 841 put those left before it.
func (p *pocketBaseYgoPersistence) applyStoredContentGraphMutation(
	principalID, pageID string, input graphMutationInput, hooks contentGraphMutationHooks,
) (graphMutationReceipt, error) {
	return p.applyStoredContentGraphMutationContext(context.Background(), principalID, pageID, input, hooks)
}

func (p *pocketBaseYgoPersistence) applyStoredContentGraphMutationContext(
	ctx context.Context, principalID, pageID string, input graphMutationInput, hooks contentGraphMutationHooks,
) (graphMutationReceipt, error) {
	hooks.StoredBlocks = true
	receipt, err := p.applyContentGraphMutationContext(ctx, principalID, pageID, input, hooks)
	if err == nil {
		p.bringSyncedCopyLeftoversIntoSight(pageID)
	}
	return receipt, err
}

// bringSyncedCopyLeftoversIntoSight puts the blocks under the page's synced
// copies where they are seen, after what brought them back has been kept: a
// page it can't settle keeps them, told in the log, as nothing it brought
// back is lost.
func (p *pocketBaseYgoPersistence) bringSyncedCopyLeftoversIntoSight(pageID string) {
	err := p.app.RunInTransaction(func(txApp core.App) error {
		holders, err := loadSyncedCopyLeftoverHolders(txApp, pageID)
		if err != nil || len(holders) == 0 {
			return err
		}
		_, err = moveSyncedCopyLeftoversOnPage(txApp, holders)
		return err
	})
	if err != nil {
		p.app.Logger().Warn("leafen blocks under synced copies not put where they are seen", "page", pageID, "error", err)
	}
}

// moveSyncedCopyLeftoversOnPage moves the blocks of a page's copies and says
// how many moved.
func moveSyncedCopyLeftoversOnPage(app core.App, holders []syncedCopyLeftoverHolder) (int, error) {
	first := holders[0]
	batches := map[string]*structuralMutationBatch{}
	batch, err := loadStructuralMutationBatch(app, first.PageID, batches)
	if err != nil {
		return 0, err
	}
	operations := []graphOperation{}
	movedIDs := []string{}
	for _, holder := range holders {
		metadata := newStructuralMetadataView(batch.doc)
		copied, exists, err := metadata.get(holder.ID)
		if err != nil || !exists {
			continue
		}
		children, err := structuralOrder(batch.doc, holder.ID)
		if err != nil || len(children) == 0 {
			continue
		}
		parentID, afterID := copied.ParentID, &holder.ID
		if original, onPage, err := syncedCopyLeftoverOriginal(app, metadata, holder); err != nil {
			return 0, err
		} else if onPage {
			parentID = original
			if afterID, err = blockMoveLastStructuralID(batch.doc, original); err != nil {
				return 0, err
			}
		}
		moves := make([]graphOperation, 0, len(children))
		for _, child := range children {
			parent, after := parentID, afterID
			moves = append(moves, graphOperation{
				Kind: graphOperationKindMoveBlock,
				Move: &graphBlockMove{BlockID: child, ParentID: &parent, AfterID: after},
			})
			placed := child
			afterID = &placed
		}
		edits, err := loadSyncedCopyLeftoverEdits(app, holder, children)
		if err != nil {
			return 0, err
		}
		// Each copy's moves are applied before the next copy reads the page.
		if _, err := applyStructuralGraphMutation(
			app, batch.doc, holder.Owner, holder.WorkspaceID, holder.PageID, holder.Owner, moves, false, nil,
		); err != nil {
			return 0, err
		}
		if err := keepSyncedCopyLeftoverEdits(app, holder, edits); err != nil {
			return 0, err
		}
		operations = append(operations, moves...)
		movedIDs = append(movedIDs, children...)
	}
	if len(operations) == 0 {
		return 0, nil
	}
	if err := saveServerStructuralBatch(app, first.Owner, first.PageID, first.Owner, "synced-copy-leftovers", batch, nil); err != nil {
		return 0, err
	}
	// Its history keeps the blocks as they now stand.
	return len(movedIDs), recordPageHistoryRewrite(app, first.PageID, first.Owner, true)
}

// syncedCopyLeftoverOriginal is the copy's original when it stands on the
// copy's page in its workspace, outside the copy, and is no copy itself: its
// blocks then go there.
func syncedCopyLeftoverOriginal(
	app core.App, metadata *structuralMetadataView, holder syncedCopyLeftoverHolder,
) (string, bool, error) {
	original, found, err := loadStoredNotionBlock(app, holder.OriginalID)
	if err != nil || !found || original.Owner != holder.Owner || original.WorkspaceID != holder.WorkspaceID ||
		original.PageID != holder.PageID || original.ID == holder.ID || original.Type != graphBlockTypeSyncedBlock ||
		notionSyncedCopySourceID(original) != "" {
		return "", false, err
	}
	seen := map[string]bool{}
	for at := original.ID; at != "" && !seen[at]; {
		seen[at] = true
		entry, exists, err := metadata.get(at)
		if err != nil || !exists {
			return "", false, err
		}
		if entry.ParentID == holder.ID {
			return "", false, nil
		}
		at = entry.ParentID
	}
	return original.ID, true, nil
}

// loadSyncedCopyLeftoverEdits are the last edits of a copy's blocks.
func loadSyncedCopyLeftoverEdits(app core.App, holder syncedCopyLeftoverHolder, ids []string) ([]syncedCopyLeftoverEdit, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	edits := []syncedCopyLeftoverEdit{}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[last_edited_by]], [[updated]] FROM {{leafen_blocks}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"owner": holder.Owner, "pageId": holder.PageID, "ids": string(encoded)}).All(&edits)
	return edits, err
}

// keepSyncedCopyLeftoverEdits gives the moved blocks back their last edits,
// which the move gives the page's owner and now: they stand where they were
// meant to, unedited.
func keepSyncedCopyLeftoverEdits(app core.App, holder syncedCopyLeftoverHolder, edits []syncedCopyLeftoverEdit) error {
	for _, edit := range edits {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_blocks}} SET [[last_edited_by]] = {:by}, [[updated]] = {:updated}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] = {:id}
		`).Bind(dbx.Params{
			"by": edit.LastEditedBy, "updated": edit.Updated,
			"owner": holder.Owner, "pageId": holder.PageID, "id": edit.ID,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// validateSyncedCopyLeftoverBlocks is its check: the tables it reads and
// writes are there.
func validateSyncedCopyLeftoverBlocks(app core.App) error {
	for _, table := range []string{"leafen_blocks", "leafen_ygo_structural_documents"} {
		if !app.HasTable(table) {
			return fmt.Errorf("blocks under synced copies: the table %s is missing", table)
		}
	}
	return nil
}
