package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The job runner fills the waiting bodies with their sub-pages' blocks, a
// window of pages at a time. A body waits when what made its pages left
// their blocks for later: a Notion import's publishing and its templates, a
// workspace import, a workspace archive's import (whose earlier archives
// hold child_page blocks of their own) and a wiki turned back into pages.
// Each live sub-page with no block gets one after the body's blocks, the
// sub-pages in their sidebar order. An earlier child_page block standing
// for a sub-page becomes its block in place; one naming another page
// becomes a link to that page, and one repeating a sub-page that has its
// block goes. A child_page block that names no page stays as it is.

const childPageBlockBackfillWindow = 32

// childPageBlockBackfillScan paces the filling: a look when the runner
// starts and whenever a body is asked for, going on at once while bodies
// wait and each minute after a failure.
type childPageBlockBackfillScan struct {
	pending bool
	next    time.Time
}

func (r *durableJobRunner) scanChildPageBlockBackfill(ctx context.Context, now time.Time, scan *childPageBlockBackfillScan) {
	if r.childPageBlocksRequested.Swap(false) {
		scan.pending, scan.next = true, time.Time{}
	}
	if !scan.pending || now.Before(scan.next) {
		return
	}
	more, err := fillChildPageBlockWindow(r.app, now)
	scan.pending, scan.next = more || err != nil, now
	if err != nil {
		scan.next = now.Add(time.Minute)
		if ctx.Err() == nil {
			logDurableJobDiagnostic(
				r.app, errorCodeJobRunnerFailed, newDiagnosticReference(), "", "child-page.blocks", "fill", err,
			)
		}
		return
	}
	if more {
		r.notify()
	}
}

// requestChildPageBlockBackfill asks for pages' bodies to be filled once
// the change is committed.
func requestChildPageBlockBackfill(app core.App, workspaceID string, pageIDs ...string) error {
	now := time.Now().UTC().UnixMilli()
	for _, pageID := range pageIDs {
		if pageID == "" {
			continue
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_child_page_block_backfill}} ([[parent_page_id]], [[workspace_id]], [[requested_at]])
			VALUES ({:pageId}, {:workspaceId}, {:now})
			ON CONFLICT ([[parent_page_id]]) DO UPDATE SET [[requested_at]] = excluded.[[requested_at]]
		`).Bind(dbx.Params{"pageId": pageID, "workspaceId": workspaceID, "now": now}).Execute(); err != nil {
			return err
		}
	}
	notifyChildPageBlockBackfillAfterCommit(app)
	return nil
}

func notifyChildPageBlockBackfillAfterCommit(app core.App) {
	request := func() {
		if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
			runner.childPageBlocksRequested.Store(true)
			runner.notify()
		}
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				request()
			}
			return nil
		})
		return
	}
	request()
}

// fillChildPageBlockWindow fills a window of waiting bodies and reports
// whether more wait. A body that can't be filled waits at the back.
func fillChildPageBlockWindow(app core.App, now time.Time) (bool, error) {
	parents := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[parent_page_id]] FROM {{leafen_child_page_block_backfill}}
		ORDER BY [[requested_at]], [[parent_page_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"limit": childPageBlockBackfillWindow + 1}).Column(&parents); err != nil {
		return false, err
	}
	more := len(parents) > childPageBlockBackfillWindow
	if more {
		parents = parents[:childPageBlockBackfillWindow]
	}
	var failed error
	for _, parentID := range parents {
		err := app.RunInTransaction(func(txApp core.App) error {
			if err := fillChildPageBlocks(txApp, parentID); err != nil {
				return err
			}
			_, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_child_page_block_backfill}} WHERE [[parent_page_id]] = {:id}`).
				Bind(dbx.Params{"id": parentID}).Execute()
			return err
		})
		if err != nil {
			failed = errors.Join(failed, err)
			if _, retryErr := app.DB().NewQuery(`
				UPDATE {{leafen_child_page_block_backfill}} SET [[requested_at]] = {:later} WHERE [[parent_page_id]] = {:id}
			`).Bind(dbx.Params{"id": parentID, "later": now.Add(time.Hour).UnixMilli()}).Execute(); retryErr != nil {
				return more, errors.Join(failed, retryErr)
			}
		}
	}
	return more, failed
}

// fillChildPageBlocks gives a page's live sub-pages their blocks in its body,
// after its blocks in their sidebar order.
func fillChildPageBlocks(app core.App, parentPageID string) error {
	parent, err := app.FindRecordById(pagesCollection, parentPageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, inWiki, err := loadWikiPlace(app, parentPageID); err != nil || inWiki {
		return err
	}
	ownerID, workspaceID := parent.GetString("owner"), parent.GetString("workspace")
	children, err := loadChildPageSiblings(app, ownerID, workspaceID, parentPageID, "")
	if err != nil {
		return err
	}
	batches := map[string]*structuralMutationBatch{}
	batch, err := loadStructuralMutationBatch(app, parentPageID, batches)
	if err != nil {
		return err
	}
	metadata, err := structuralMetadata(batch.doc)
	if err != nil {
		return err
	}
	// A database page shown by an inline database block stays with that
	// block; earlier child_page blocks are found by the page they name.
	inline, earlier := map[string]bool{}, map[string][]string{}
	for id, entry := range metadata {
		target, _ := entry.Content["childPageId"].(string)
		switch {
		case entry.Type == graphBlockTypeInlineDatabase && target != "":
			inline[target] = true
		case entry.Type == graphBlockTypeChildPage && target != "" && target != id:
			earlier[target] = append(earlier[target], id)
		}
	}
	live := map[string]bool{}
	order := []string{}
	// A page hanging on a line has no block of its own
	// (mention_anchored_pages.go).
	anchors, err := loadMentionAnchors(app, parentPageID)
	if err != nil {
		return err
	}
	for _, pages := range anchors {
		for _, id := range pages {
			inline[id] = true
		}
	}
	for _, child := range children {
		if !inline[child.ID] {
			live[child.ID] = true
			order = append(order, child.ID)
		}
	}
	apply := func(operation graphOperation, start bool) error {
		starts := map[string]struct{}{}
		if start && operation.Block != nil {
			starts[operation.Block.ID] = struct{}{}
		}
		if _, err := applyStructuralGraphMutation(
			app, batch.doc, ownerID, workspaceID, parentPageID, ownerID, []graphOperation{operation}, false, starts,
		); err != nil {
			return err
		}
		metadata, err = structuralMetadata(batch.doc)
		return err
	}
	// Each page goes after the body's last block, or the page placed before
	// it; one with its block already is such a mark too, and a page put in
	// an earlier child_page block's place stands where that block stood.
	after, err := blockMoveLastStructuralID(batch.doc, "")
	if err != nil {
		return err
	}
	for _, pageID := range order {
		if entry, found := metadata[pageID]; found {
			if isChildPageBlock(pageID, entry) && entry.ParentID == "" {
				id := pageID
				after = &id
			}
			continue
		}
		if _, held, err := childPageBlockHolder(app, ownerID, pageID); err != nil || held {
			if err != nil {
				return err
			}
			continue
		}
		block := &graphBlock{ID: pageID, Type: graphBlockTypeChildPage, Content: childPageBlockContent(pageID)}
		if stand := earlier[pageID]; len(stand) > 0 {
			// The earlier block's place becomes the page's block's.
			sort.Strings(stand)
			replaced := stand[0]
			earlier[pageID] = stand[1:]
			entry := metadata[replaced]
			siblings, err := structuralOrder(batch.doc, entry.ParentID)
			if err != nil {
				return err
			}
			if entry.ParentID != "" {
				container := entry.ParentID
				block.ParentID = &container
			}
			if index := structuralIndex(siblings, replaced); index > 0 {
				previous := siblings[index-1]
				block.AfterID = &previous
			}
			if err := apply(graphOperation{Kind: graphOperationKindUpsertBlock, Block: block}, block.AfterID == nil); err != nil {
				return err
			}
			if err := apply(graphOperation{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: replaced}}, false); err != nil {
				return err
			}
			continue
		}
		block.AfterID = after
		if err := apply(graphOperation{Kind: graphOperationKindUpsertBlock, Block: block}, after == nil); err != nil {
			return err
		}
		id := pageID
		after = &id
	}
	// The earlier blocks left repeat a sub-page that has its block, and go,
	// or name another page, and link to it.
	targets := make([]string, 0, len(earlier))
	for target := range earlier {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		for _, blockID := range earlier[target] {
			entry, found := metadata[blockID]
			if !found {
				continue
			}
			if live[target] {
				if err := apply(graphOperation{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: blockID}}, false); err != nil {
					return err
				}
				continue
			}
			content, err := json.Marshal(entry.Content)
			if err != nil {
				return err
			}
			link := &graphBlock{ID: blockID, Type: graphBlockTypeLinkToPage, Content: content}
			if entry.ParentID != "" {
				container := entry.ParentID
				link.ParentID = &container
			}
			if err := apply(graphOperation{Kind: graphOperationKindUpsertBlock, Block: link}, false); err != nil {
				return err
			}
		}
	}
	if _, changed := structuralBatchUpdate(batch); !changed {
		return nil
	}
	if err := saveServerStructuralBatch(app, ownerID, parentPageID, ownerID, "child-page-backfill", batch, nil); err != nil {
		return err
	}
	// Its history keeps the page blocks it gained as they now stand.
	return recordPageHistoryRewrite(app, parentPageID, ownerID, true)
}

// childPageBlocksFollowPages reports whether a page's body holds a block for
// each of its live sub-pages but those hanging on a line, and only for them.
func childPageBlocksFollowPages(app core.App, parentPageID string) (bool, error) {
	parent, err := app.FindRecordById(pagesCollection, parentPageID)
	if err != nil {
		return false, err
	}
	children, err := loadChildPageSiblings(app, parent.GetString("owner"), parent.GetString("workspace"), parentPageID, "")
	if err != nil {
		return false, err
	}
	doc, _, err := loadYgoStructuralDoc(app, parentPageID)
	if err != nil {
		return false, err
	}
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return false, err
	}
	inline := map[string]bool{}
	for _, entry := range metadata {
		if target, _ := entry.Content["childPageId"].(string); entry.Type == graphBlockTypeInlineDatabase && target != "" {
			inline[target] = true
		}
	}
	anchors, err := loadMentionAnchors(app, parentPageID)
	if err != nil {
		return false, err
	}
	for _, pages := range anchors {
		for _, id := range pages {
			inline[id] = true
		}
	}
	live := map[string]bool{}
	for _, child := range children {
		if inline[child.ID] {
			continue
		}
		live[child.ID] = true
		if !isChildPageBlock(child.ID, metadata[child.ID]) {
			return false, nil
		}
	}
	for id, entry := range metadata {
		if isChildPageBlock(id, entry) && !live[id] {
			return false, nil
		}
	}
	return true, nil
}
