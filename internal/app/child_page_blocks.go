package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A sub-page is a block in its parent page's body, as in Notion: a
// child_page block whose ID is the page's ID. The page keeps its parent
// page for its ancestry, shares and sidebar; the block holds its place in
// the parent's body, so the page shows there and moves with the body.
// Deleting the block sends the page to the trash and putting the block back
// restores it; turning a block into a page makes a page with the block's ID
// and text, which takes the blocks inside it. A child_page block naming
// another page is an earlier link to a page and stays as it was.

var (
	errChildPageBlockInvalid    = graphInputError(errorCodeGraphReferenceInvalid, "A page block can only hold a page inside this page.")
	errChildPageBlockTypeChange = graphInputError(errorCodeGraphInvalid, "A page can't be turned into another block.")
	errChildPageBlockPageChange = graphConflictError(errorCodeGraphMutationFailed, "The page couldn't be changed.")
)

// childPageBlockContent is a sub-page block's content: the page it is.
func childPageBlockContent(pageID string) json.RawMessage {
	encoded, _ := json.Marshal(map[string]string{"childPageId": pageID})
	return encoded
}

// isChildPageBlock reports whether a block is a sub-page's block: a
// child_page block naming itself.
func isChildPageBlock(id string, entry structuralBlockMetadata) bool {
	if entry.Type != graphBlockTypeChildPage {
		return false
	}
	target, _ := entry.Content["childPageId"].(string)
	return target == id
}

// isStoredChildPageBlock reports whether a stored block is a sub-page's
// block.
func isStoredChildPageBlock(id, blockType, content string) bool {
	if blockType != graphBlockTypeChildPage {
		return false
	}
	stored := struct {
		ChildPageID string `json:"childPageId"`
	}{}
	return json.Unmarshal([]byte(content), &stored) == nil && stored.ChildPageID == id
}

// placeChildPageBlock puts a sub-page's block in its parent's body where the
// page was placed, at the end when no place was given, and the page in the
// sidebar where its block stands: there, or where its parent's body already
// held it (2026-11-07: an imported page whose parent's text named it before
// it was made kept the sidebar place of its making).
func placeChildPageBlock(
	app core.App,
	ownerID, workspaceID, parentPageID, pageID, actorID string,
	placement pageBodyPlacement, activity ...workspaceActivityGraphContext,
) error {
	holder, held, err := childPageBlockHolder(app, ownerID, pageID)
	if err != nil {
		return err
	}
	if err := putChildPageBlock(app, ownerID, workspaceID, parentPageID, pageID, actorID, false,
		func(doc *crdt.Doc, metadata map[string]structuralBlockMetadata) (childPageBlockSpot, error) {
			if placement.Type == "start" {
				return childPageBlockSpot{}, nil
			}
			if anchor, anchored := metadata[placement.AfterID]; placement.Type == "after" && anchored {
				after := placement.AfterID
				return childPageBlockSpot{Container: anchor.ParentID, After: &after}, nil
			}
			return endOfChildPageContainer(doc, "")
		}, activity...); err != nil {
		return err
	}
	// A new block at the body's end is where a new page's sidebar place is.
	if parentPageID == "" || placement.Type == "end" && !(held && holder == parentPageID) {
		return nil
	}
	doc, _, err := loadYgoStructuralDoc(app, parentPageID)
	if err != nil {
		return err
	}
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return err
	}
	return alignChildPageSidebar(app, doc, metadata, ownerID, workspaceID, parentPageID, []string{pageID})
}

// beginServerPageHistoryCapture starts noting the blocks a change the server
// makes to a page touches, for a page whose history has begun.
func beginServerPageHistoryCapture(
	app core.App, pageID string, doc *crdt.Doc, operations []graphOperation,
) (*pageHistoryGraphCapture, error) {
	if started, err := pageHistoryStarted(app, pageID); err != nil || !started {
		return nil, err
	}
	return beginPageHistoryGraphCapture(doc, operations)
}

// saveServerStructuralBatch keeps a change the server made to a page's
// blocks and records it for the page's readers, and in its history when a
// capture noted the blocks it touched, with the activity it is part of.
func saveServerStructuralBatch(
	app core.App, ownerID, pageID, actorID, reason string,
	batch *structuralMutationBatch, history *pageHistoryGraphCapture, activity ...workspaceActivityGraphContext,
) error {
	update, changed := structuralBatchUpdate(batch)
	if !changed {
		return nil
	}
	if len(update) > maxYgoUpdateBytes {
		return graphInputError(errorCodeGraphChangeTooLarge, "This change is too large for one change (64 MB). Make it in parts.")
	}
	if err := appendYgoStructuralUpdate(app, pageID, update); err != nil {
		return err
	}
	if err := markSearchDirtyByPageID(app, pageID); err != nil {
		return err
	}
	if err := recordServerGraphBlocksChange(
		app, ownerID, pageID, reason+":"+newDiagnosticReference(), update, history,
	); err != nil || history == nil {
		return err
	}
	deltas, err := history.finish(app, pageID)
	if err != nil {
		return err
	}
	return recordPageHistoryGraphMutation(app, pageID, actorID, update, deltas, nil, nil, activity...)
}

// childPageBlockPlan is what a content change does to the pages whose
// blocks it touches, done in the change's transaction.
type childPageBlockPlan struct {
	// trash holds the pages whose blocks the change deletes, with the
	// places their blocks held.
	trash []childPageBlockTrash
	// restore holds the trashed pages whose blocks the change puts back.
	restore []string
	// placed holds the sub-page blocks the change adds or moves.
	placed []string
	// made holds the blocks the change made pages of.
	made []string
	// unmade restores a page conversion to its original block and takes
	// its page body back inside that block before the page is retired.
	unmade []string
	// copies holds the new blocks naming a sub-page of the page, a copied
	// block's copy of its sub-page's block, which link to the page until the
	// page's own copy takes their place.
	copies []childPageBlockCopy
	// lines holds the blocks the change deletes or writes again and
	// newLines those it makes, which pages made from a line's mention may
	// hang on, or from a table row's cell or a caption
	// (mention_anchored_pages.go); linesMoved tells that it moves blocks,
	// those lines among them. cells holds the database pages' cells it
	// writes, which pages may hang on too (mention_anchored_fields.go).
	lines, newLines []string
	linesMoved      bool
	cells           []pageFieldAnchor
	// wikiLinks are the links standing for a wiki's pages it takes away or
	// puts in, whose pages follow them (page_block_trash.go).
	wikiLinks []wikiPageLinkChange
}

// childPageBlockJobs are the jobs a change's page blocks started.
type childPageBlockJobs struct {
	moves  []string
	copies bool
	// wiki holds the pages made in a wiki's page, which become the wiki's
	// once the change is made, and wikiLinks the plan's.
	wiki      []string
	wikiLinks []wikiPageLinkChange
}

type childPageBlockTrash struct {
	pageID, containerID, afterID string
}

// prepareChildPageBlocks checks a content change against the sub-page
// blocks of the page it changes, before the change is made: a block turned
// into a page, or a new page block, makes its page here.
func prepareChildPageBlocks(
	app core.App,
	actorID string,
	authorization pageAuthorization,
	doc *crdt.Doc,
	mutationID string,
	operations []graphOperation,
) (childPageBlockPlan, error) {
	plan := childPageBlockPlan{}
	// The blocks a change names are looked up one by one, the whole page's
	// only for a deletion's subtree (2026-10-21: read whole for each block
	// written, a long page's metadata held the one write connection).
	view, parents := newStructuralMetadataView(doc), childPageChangeParents{}
	pageID := authorization.Page.Id
	pageOf := func(id string) *core.Record {
		page, err := app.FindRecordById(pagesCollection, id)
		if err != nil || page.GetString("owner") != authorization.Workspace.OwnerID ||
			page.GetString("workspace") != authorization.Workspace.WorkspaceID {
			return nil
		}
		return page
	}
	for _, operation := range operations {
		parents.note(operation)
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			block := operation.Block
			existing, exists, err := view.get(block.ID)
			if err != nil {
				return plan, err
			}
			if block.Type != graphBlockTypeChildPage {
				if !exists {
					plan.newLines, plan.wikiLinks = append(plan.newLines, block.ID), noteWikiPageLink(plan.wikiLinks, block.Type, block.Content, false)
				} else {
					plan.lines = append(plan.lines, block.ID)
				}
				if exists && isChildPageBlock(block.ID, existing) && pageOf(block.ID) != nil {
					if !block.RestorePageConversion {
						return plan, errChildPageBlockTypeChange
					}
					if err := prepareChildPageConversionRestore(app, actorID, authorization, doc, block.ID); err != nil {
						return plan, err
					}
					plan.unmade = append(plan.unmade, block.ID)
				}
				continue
			}
			content := map[string]any{}
			_ = json.Unmarshal(block.Content, &content)
			target, _ := content["childPageId"].(string)
			if target != "" && target != block.ID && !exists {
				// A new block naming another page, as a copy of a page
				// block, links to that page; a copy of one of this page's
				// sub-pages takes the link's place once it is made.
				block.Type = graphBlockTypeLinkToPage
				if copied := pageOf(target); copied != nil && copied.GetString("parent_id") == pageID {
					plan.copies = append(plan.copies, childPageBlockCopy{placeholderID: block.ID, pageID: target})
				}
				continue
			}
			if (target != "" && target != block.ID) ||
				(exists && existing.Type == graphBlockTypeChildPage && !isChildPageBlock(block.ID, existing) && target == "") {
				// An earlier block naming another page, or an earlier page
				// block with no page, stays as it was.
				continue
			}
			page := pageOf(block.ID)
			switch {
			case page != nil && page.GetString("parent_id") == pageID:
				if page.GetBool("in_trash") && (!exists || !isChildPageBlock(block.ID, existing)) {
					plan.restore = append(plan.restore, block.ID)
					if exists {
						plan.made = append(plan.made, block.ID)
					}
				}
			case page != nil:
				return plan, errChildPageBlockInvalid
			default:
				if err := createChildPageForBlock(
					app, actorID, authorization, mutationID, block, exists, existing,
				); err != nil {
					return plan, err
				}
				plan.made = append(plan.made, block.ID)
			}
			block.Content = childPageBlockContent(block.ID)
			plan.placed = append(plan.placed, block.ID)
		case graphOperationKindMoveBlock:
			moved, _, err := view.get(operation.Move.BlockID)
			if err != nil {
				return plan, err
			}
			if isChildPageBlock(operation.Move.BlockID, moved) {
				plan.placed = append(plan.placed, operation.Move.BlockID)
			} else {
				plan.linesMoved = true
			}
		case graphOperationKindUpsertRow:
			plan.cells = append(plan.cells, rowCellFieldAnchors([]graphOperation{operation})...)
		case graphOperationKindDeleteBlock:
			// What the deletion takes is what still stands inside the block
			// once the change's earlier blocks are placed.
			metadata, ids, err := parents.deleted(view, operation.Deletion.BlockID)
			if err != nil {
				return plan, err
			}
			for _, id := range ids {
				if !isChildPageBlock(id, metadata[id]) {
					plan.lines, plan.wikiLinks = append(plan.lines, id), noteWikiPageLink(plan.wikiLinks, metadata[id].Type, metadata[id].Content, true)
					continue
				}
				if page := pageOf(id); page != nil && page.GetString("parent_id") == pageID && !page.GetBool("in_trash") {
					container := metadata[id].ParentID
					order, err := structuralOrder(doc, container)
					if err != nil {
						return plan, err
					}
					after := ""
					if index := structuralIndex(order, id); index > 0 {
						after = order[index-1]
					}
					plan.trash = append(plan.trash, childPageBlockTrash{pageID: id, containerID: container, afterID: after})
				}
			}
		}
	}
	return plan, nil
}

// createChildPageForBlock makes the page a block becomes: the block's ID
// and text, under the page holding it, with a callout's icon
// (child_page_block_icon.go). A block of no words, as a divider, makes an
// untitled page, whatever text it keeps unshown (Notion's, seen 2026-11-10:
// a divider moved into a database is an untitled row).
func createChildPageForBlock(
	app core.App,
	actorID string,
	authorization pageAuthorization,
	mutationID string,
	block *graphBlock,
	exists bool,
	previous structuralBlockMetadata,
) error {
	pageID := authorization.Page.Id
	previousType := previous.Type
	title := ""
	// The block's words become the title with their formatting, links and
	// mentions, as a block Notion makes a page keeps them (seen 2026-11-10:
	// one moved into a database is a row titled so); the spans go where
	// they don't spell the title, as a title trimmed or cut short.
	var spans []graphTextSpan
	var blockText *crdt.YText
	content := map[string]any{}
	_ = json.Unmarshal(block.Content, &content)
	_, words := graphTextBlockTypes[previousType]
	if exists && words {
		text, found, err := loadYgoDoc(app, pageID)
		if err != nil {
			return err
		}
		if found {
			blockText = text.GetText(ygoBlockTextRootPrefix + block.ID)
			title = blockText.ToString()
			spans = graphTextSpansFromYText(blockText)
		}
	}
	if chosen, supplied := content["childPageTitle"]; supplied && (words || !exists) {
		var valid bool
		title, valid = chosen.(string)
		if !valid {
			return graphInputError(errorCodeGraphInvalid, "Provide a text title for the page.")
		}
	}
	// Its title is kept as written, the spaces at its ends too (page_creation.go).
	if runes := []rune(title); len(runes) > maxGraphTitleRunes {
		title = string(runes[:maxGraphTitleRunes])
	}
	if spelledName(spans) != title {
		spans = nil
	}
	_, status, err := admitPageCreationTransactionWithLock(app, actorID, normalizePageCreationAdmissionInput(
		pageCreationAdmissionInput{
			RequestID:   "page-block:" + childPageBlockDigest(mutationID, block.ID),
			WorkspaceID: authorization.Workspace.WorkspaceID, ParentID: pageID,
			Title: title, TitleSpans: spans, PreparedPageID: block.ID, BodyBlockPlaced: true,
		},
	), pageLockIgnored)
	if err != nil {
		return err
	}
	if status != pageCreationApplied {
		return errChildPageBlockPageChange
	}
	if exists && previousType == graphBlockTypeCallout {
		if err := giveChildPageCalloutIcon(app, actorID, authorization.Workspace.WorkspaceID, block.ID, previous.Content); err != nil {
			return err
		}
	}
	if blockText != nil {
		return tellChildPagePersonMentions(app, authorization.Workspace.WorkspaceID, title, block.ID, actorID, blockText)
	}
	return nil
}

func childPageBlockDigest(mutationID, pageID string) string {
	digest := sha256.Sum256([]byte(mutationID + "\x00" + pageID))
	return hex.EncodeToString(digest[:16])
}

// finish sends the pages whose blocks went to the trash, brings back the
// pages whose blocks came back, keeps the body's page order with the blocks,
// starts moving the blocks inside the blocks made pages into those pages
// and starts the copies of the copied sub-pages, once the change is made. It
// returns the jobs it started.
func (plan childPageBlockPlan) finish(
	app core.App, actorID string, authorization pageAuthorization, doc *crdt.Doc, mutationID string,
) (childPageBlockJobs, error) {
	jobs := childPageBlockJobs{wikiLinks: plan.wikiLinks}
	for _, id := range plan.unmade {
		move, err := restoreChildPageConversion(app, actorID, authorization.Page.Id, id, mutationID)
		if err != nil {
			return jobs, err
		}
		if move != "" {
			jobs.moves = append(jobs.moves, move)
		}
	}
	for _, id := range plan.restore {
		if err := setChildPageTrash(app, actorID, id, false, mutationID); err != nil {
			return jobs, err
		}
	}
	ownerID, workspaceID := authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID
	for _, trashed := range plan.trash {
		if err := setChildPageTrash(app, actorID, trashed.pageID, true, mutationID); err != nil {
			return jobs, err
		}
		if err := rememberChildPageBlockPlace(
			app, trashed.pageID, workspaceID, authorization.Page.Id, trashed.containerID, trashed.afterID,
		); err != nil {
			return jobs, err
		}
	}
	for _, copied := range plan.copies {
		admitted, err := admitChildPageBlockCopy(app, actorID, authorization.Page.Id, copied.placeholderID, copied.pageID,
			"page-block-copy:"+childPageBlockDigest(mutationID, copied.placeholderID), "")
		if err != nil {
			return jobs, err
		}
		jobs.copies = jobs.copies || admitted
	}
	if plan.linesMoved {
		// The pages hanging on lines follow them in the sidebar.
		anchors, err := loadMentionAnchors(app, authorization.Page.Id)
		if err != nil {
			return jobs, err
		}
		for _, pages := range anchors {
			plan.placed = append(plan.placed, pages...)
		}
	}
	if len(plan.placed) == 0 {
		return jobs, nil
	}
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return jobs, err
	}
	if err := alignChildPageSidebar(app, doc, metadata, ownerID, workspaceID, authorization.Page.Id, plan.placed); err != nil {
		return jobs, err
	}
	moves, err := moveChildPageBlockChildren(app, actorID, authorization.Page.Id, doc, mutationID, plan.made)
	jobs.moves = append(jobs.moves, moves...)
	if err != nil || len(plan.made) == 0 {
		return jobs, err
	}
	if _, inWiki, err := loadWikiPlace(app, authorization.Page.Id); err != nil || !inWiki {
		return jobs, err
	}
	jobs.wiki = plan.made
	return jobs, nil
}

// settleAnchors keeps the pages hanging on the page's lines with them once
// the change's blocks and text are in: a page whose line or mention the
// change took goes to the trash, and one whose line or mention it brought
// back returns (mention_anchored_pages.go); those hanging on the cells it
// wrote follow them so (mention_anchored_fields.go).
func (plan childPageBlockPlan) settleAnchors(
	app core.App, actorID string, authorization pageAuthorization, mutationID string, texts ...[]graphTextUpdate,
) error {
	lines := slices.Clone(plan.lines)
	for _, list := range texts {
		for _, text := range list {
			lines = append(lines, text.BlockID)
		}
	}
	if err := settleFieldAnchoredPages(app, actorID, mutationID+":cells", plan.cells, false); err != nil {
		return err
	}
	placed, err := settleMentionAnchoredPages(app, actorID, authorization.Page.Id, mutationID, lines, plan.newLines, false)
	if err != nil || len(placed) == 0 {
		return err
	}
	return alignMentionAnchoredPages(app, authorization.Page.Id, placed)
}

// setChildPageParent moves a sub-page under another page, as a move of the
// page does: its block leaves the body holding it for the new parent's. It
// keeps page locks, so one in or into a locked page stays where it is.
func setChildPageParent(app core.App, actorID, pageID, parentID, mutationID string) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	if page.GetString("parent_id") == parentID {
		return nil
	}
	input := normalizePageMetadataInput(pageMetadataInput{
		Title: page.GetString("title"), ParentID: parentID, InTrash: page.GetBool("in_trash"),
		MutationID: "page-block:" + childPageBlockDigest(mutationID+":parent", pageID), BaseCursor: page.GetInt("cursor"),
	})
	_, status, err := applyPageMetadataTransactionWithLock(
		app, personAccessSubject(actorID), pageID, input, pageMetadataRequestHash(pageID, input), pageLockEnforced,
	)
	if err != nil {
		return err
	}
	if status != pageMetadataApplied {
		return errChildPageBlockPageChange
	}
	return nil
}
