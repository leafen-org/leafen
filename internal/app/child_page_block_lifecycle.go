package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A sub-page's block follows its page: it leaves its parent's body while
// the page is in the trash and comes back last in the block it was in — at
// the body's end from the top level — as Notion's restore puts it (the
// blocks around its old place may be gone or moved by then; user decision
// 2026-11-07, and inside a block seen 2026-11-18), it moves to the new
// parent's body when the page moves, and the sidebar lists a page's
// sub-pages in the order of their blocks in its body.

// childPageBlockSpot is where a sub-page's block goes in a body: after a
// block in a container, or at the container's start when After is nil.
type childPageBlockSpot struct {
	Container string
	After     *string
}

// endOfChildPageContainer is the spot at the end of a container.
func endOfChildPageContainer(doc *crdt.Doc, container string) (childPageBlockSpot, error) {
	last, err := blockMoveLastStructuralID(doc, container)
	return childPageBlockSpot{Container: container, After: last}, err
}

// restoredChildPageSpotIn is where a sub-page's block comes back from the
// trash: last inside the block it was in (container), as Notion's restore
// puts it, after the blocks added behind its old place since — through its
// API and the trashed page's banner alike (seen 2026-11-18 in two tabs of
// a Notion test page, 'W1905 place': a callout's, a toggle's and a line's) —
// and at the body's end from the top level, or once that block is gone or
// takes no blocks inside. A callout's words line stands for the callout
// (callout_words_line.go).
func restoredChildPageSpotIn(
	app core.App, parentPageID, container string,
) (func(*crdt.Doc, map[string]structuralBlockMetadata) (childPageBlockSpot, error), error) {
	if container != "" {
		callout, err := calloutOfWordsLine(app, parentPageID, container)
		if err != nil {
			return nil, err
		}
		if callout != "" {
			container = callout
		}
	}
	return func(doc *crdt.Doc, metadata map[string]structuralBlockMetadata) (childPageBlockSpot, error) {
		entry, standing := metadata[container]
		if _, words := graphTextBlockTypes[entry.Type]; container == "" || !standing || words && !restoredLineHoldsPage[entry.Type] {
			return endOfChildPageContainer(doc, "")
		}
		return endOfChildPageContainer(doc, container)
	}, nil
}

// lastTopLevelBlock reads the last top-level block of a page's body, which a
// block added at the end follows.
func lastTopLevelBlock(app core.App, ownerID, pageID string) (*string, error) {
	var id string
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_blocks}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:page} AND [[parent_id]] = ''
		ORDER BY [[position]] DESC, [[id]] DESC LIMIT 1
	`).Bind(dbx.Params{"owner": ownerID, "page": pageID}).Row(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// childPageBlockHolder reads the page whose body holds a sub-page's block.
func childPageBlockHolder(app core.App, ownerID, pageID string) (string, bool, error) {
	var holder string
	err := app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_blocks}}
		WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[type]] = 'child_page'
	`).Bind(dbx.Params{"id": pageID, "owner": ownerID}).Row(&holder)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return holder, err == nil, err
}

// putChildPageBlock puts a sub-page's block at a spot in its parent's body,
// or with move there when it is already in that body. A block already in
// another body is left alone. One under a database template gets its block
// as one under a page does, as Notion's template holds sub-pages (seen
// 2026-11-16); one under a wiki's page gets its block too, which becomes its
// link once the page is the wiki's (wiki_page_adoption.go).
func putChildPageBlock(
	app core.App,
	ownerID, workspaceID, parentPageID, pageID, actorID string,
	move bool,
	spot func(*crdt.Doc, map[string]structuralBlockMetadata) (childPageBlockSpot, error),
	activity ...workspaceActivityGraphContext,
) error {
	if parentPageID == "" {
		return nil
	}
	holder, held, err := childPageBlockHolder(app, ownerID, pageID)
	if err != nil || (held && (holder != parentPageID || !move)) {
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
	current, exists := metadata[pageID]
	where, err := spot(batch.doc, metadata)
	if err != nil {
		return err
	}
	// A spot after the page's own block — the last in its container, as the
	// block a page turned back into stands when the page comes back from the
	// trash (restoreConvertedChildPageBlock) — is where that block stands.
	// 2026-11-16: a return that stopped and rolled back put the page's block
	// after itself and failed with LEAFEN_GRAPH_REFERENCE_INVALID.
	if exists && where.After != nil && *where.After == pageID {
		order, err := structuralOrder(batch.doc, current.ParentID)
		if err != nil {
			return err
		}
		where = childPageBlockSpot{Container: current.ParentID}
		if index := structuralIndex(order, pageID); index > 0 {
			previous := order[index-1]
			where.After = &previous
		}
	}
	var parentID *string
	if where.Container != "" {
		value := where.Container
		parentID = &value
	}
	operation := graphOperation{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
		ID: pageID, ParentID: parentID, AfterID: where.After,
		Type: graphBlockTypeChildPage, Content: childPageBlockContent(pageID),
	}}
	if exists && isChildPageBlock(pageID, current) {
		order, err := structuralOrder(batch.doc, current.ParentID)
		if err != nil {
			return err
		}
		previous := ""
		if index := structuralIndex(order, pageID); index > 0 {
			previous = order[index-1]
		}
		if current.ParentID == where.Container &&
			((where.After == nil && previous == "") || (where.After != nil && *where.After == previous)) {
			return nil
		}
		operation = graphOperation{Kind: graphOperationKindMoveBlock, Move: &graphBlockMove{
			BlockID: pageID, ParentID: parentID, AfterID: where.After,
		}}
	}
	start := map[string]struct{}{}
	if where.After == nil {
		start[pageID] = struct{}{}
	}
	history, err := beginServerPageHistoryCapture(app, parentPageID, batch.doc, []graphOperation{operation})
	if err != nil {
		return err
	}
	if _, err := applyStructuralGraphMutation(
		app, batch.doc, ownerID, workspaceID, parentPageID, actorID,
		[]graphOperation{operation}, false, start,
	); err != nil {
		return err
	}
	return saveServerStructuralBatch(app, ownerID, parentPageID, actorID, "child-page-block:"+pageID, batch, history, activity...)
}

// removeChildPageBlock takes a sub-page's block out of the body holding it;
// a page going to the trash keeps the place its block held.
func removeChildPageBlock(app core.App, ownerID, workspaceID, pageID, actorID string, remember bool) error {
	holder, held, err := childPageBlockHolder(app, ownerID, pageID)
	if err != nil || !held {
		return err
	}
	batches := map[string]*structuralMutationBatch{}
	batch, err := loadStructuralMutationBatch(app, holder, batches)
	if err != nil {
		return err
	}
	metadata, err := structuralMetadata(batch.doc)
	if err != nil {
		return err
	}
	entry, found := metadata[pageID]
	if !found || !isChildPageBlock(pageID, entry) {
		return nil
	}
	if remember {
		order, err := structuralOrder(batch.doc, entry.ParentID)
		if err != nil {
			return err
		}
		after := ""
		if index := structuralIndex(order, pageID); index > 0 {
			after = order[index-1]
		}
		if err := rememberChildPageBlockPlace(app, pageID, workspaceID, holder, entry.ParentID, after); err != nil {
			return err
		}
	}
	removal := []graphOperation{{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: pageID}}}
	history, err := beginServerPageHistoryCapture(app, holder, batch.doc, removal)
	if err != nil {
		return err
	}
	if _, err := applyStructuralGraphMutation(
		app, batch.doc, ownerID, workspaceID, holder, actorID, removal, false, nil,
	); err != nil {
		return err
	}
	return saveServerStructuralBatch(app, ownerID, holder, actorID, "child-page-block:"+pageID, batch, history)
}

// rememberChildPageBlockPlace keeps where a trashed sub-page's block was.
func rememberChildPageBlockPlace(app core.App, pageID, workspaceID, parentPageID, containerID, afterID string) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_child_page_block_places}}
			([[page_id]], [[workspace_id]], [[parent_page_id]], [[parent_block_id]], [[after_id]])
		VALUES ({:pageId}, {:workspaceId}, {:parentPageId}, {:containerId}, {:afterId})
		ON CONFLICT ([[page_id]]) DO UPDATE SET [[workspace_id]] = excluded.[[workspace_id]],
			[[parent_page_id]] = excluded.[[parent_page_id]], [[parent_block_id]] = excluded.[[parent_block_id]],
			[[after_id]] = excluded.[[after_id]]
	`).Bind(dbx.Params{
		"pageId": pageID, "workspaceId": workspaceID, "parentPageId": parentPageID,
		"containerId": containerID, "afterId": afterID,
	}).Execute()
	return err
}

// restoreChildPageBlock puts a restored sub-page's block back in its
// parent's body: last in the block it was in for a page back from the trash
// (restoredChildPageSpotIn), and where it was for a page turned back from a
// block (keepPlace) — after the block before it, at its container's start
// when it was first, and at the body's end when that place is gone.
func restoreChildPageBlock(app core.App, ownerID, workspaceID, parentPageID, pageID, actorID string, keepPlace bool) error {
	place := struct {
		ParentPageID string `db:"parent_page_id"`
		ContainerID  string `db:"parent_block_id"`
		AfterID      string `db:"after_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[parent_page_id]], [[parent_block_id]], [[after_id]] FROM {{leafen_child_page_block_places}}
		WHERE [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).One(&place)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	known := err == nil && place.ParentPageID == parentPageID
	remembered := keepPlace && known
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_child_page_block_places}} WHERE [[page_id]] = {:pageId}`).
		Bind(dbx.Params{"pageId": pageID}).Execute(); err != nil {
		return err
	}
	container := ""
	if known && !keepPlace {
		container = place.ContainerID
	}
	back, err := restoredChildPageSpotIn(app, parentPageID, container)
	if err != nil {
		return err
	}
	if err := putChildPageBlock(app, ownerID, workspaceID, parentPageID, pageID, actorID, false,
		func(doc *crdt.Doc, metadata map[string]structuralBlockMetadata) (childPageBlockSpot, error) {
			if _, kept := metadata[place.ContainerID]; remembered && (place.ContainerID == "" || kept) {
				if place.AfterID == "" {
					return childPageBlockSpot{Container: place.ContainerID}, nil
				}
				if entry, found := metadata[place.AfterID]; found && entry.ParentID == place.ContainerID {
					after := place.AfterID
					return childPageBlockSpot{Container: place.ContainerID, After: &after}, nil
				}
				return endOfChildPageContainer(doc, place.ContainerID)
			}
			return back(doc, metadata)
		}); err != nil || parentPageID == "" {
		return err
	}
	// The sidebar lists it where its block came back, as Notion's restore
	// puts both at the end (seen 2026-11-12); its sidebar place from before
	// the trash stayed behind otherwise.
	doc, _, err := loadYgoStructuralDoc(app, parentPageID)
	if err != nil {
		return err
	}
	metadata, err := structuralMetadata(doc)
	if err != nil || !isChildPageBlock(pageID, metadata[pageID]) {
		return err
	}
	return alignChildPageSidebar(app, doc, metadata, ownerID, workspaceID, parentPageID, []string{pageID})
}

// keepChildPageBlockWithPage keeps a sub-page's block with its page once
// the page record changed: out of the body while the page is in the trash,
// back where it was when the page comes back, and in its new parent's body
// when it moves, at the body's end unless a sidebar place was asked. A page
// placed in the sidebar so that its order among its siblings there differs
// from their blocks' moves its block to follow the sidebar.
func keepChildPageBlockWithPage(
	app core.App,
	ownerID, workspaceID, actorID string,
	page *core.Record,
	oldParentID string,
	wasTrashed bool,
	requested *float64,
) error {
	trashed, parentID := page.GetBool("in_trash"), page.GetString("parent_id")
	switch {
	case trashed && !wasTrashed:
		return removeChildPageBlock(app, ownerID, workspaceID, page.Id, actorID, true)
	case trashed:
		return nil
	case wasTrashed:
		// One that hung on a line or a field is placed by its anchor
		// (mention_anchored_pages.go).
		if anchored, err := restoreMentionAnchoredPage(app, ownerID, workspaceID, actorID, page); err != nil || anchored {
			return err
		}
		// One at the top level comes back last among its section's pages
		// placed by hand, as a sub-page comes back at its parent's end; a
		// section sorted by last edit shows it first, as Notion's, whose
		// restore is an edit (its API, 2026-11-14).
		if parentID == "" {
			return placePageLastAmongSiblings(app, ownerID, workspaceID, page.Id, "")
		}
		return restoreConvertedChildPageBlock(app, ownerID, workspaceID, parentID, page.Id, actorID)
	case parentID != oldParentID:
		// One that hung on a line is an ordinary sub-page under its new parent.
		if page.GetString(pageAnchorBlockField) != "" {
			if err := setPageAnchor(app, page.Id, "", false); err != nil {
				return err
			}
		}
		if err := removeChildPageBlock(app, ownerID, workspaceID, page.Id, actorID, false); err != nil {
			return err
		}
		spot := func(doc *crdt.Doc, _ map[string]structuralBlockMetadata) (childPageBlockSpot, error) {
			return endOfChildPageContainer(doc, "")
		}
		if requested != nil {
			spot = childPageSidebarSpot(app, ownerID, workspaceID, parentID, page.Id, *requested)
		}
		return putChildPageBlock(app, ownerID, workspaceID, parentID, page.Id, actorID, false, spot)
	case requested != nil:
		agrees, err := childPageBlockFollowsSidebar(app, ownerID, workspaceID, parentID, page.Id, *requested)
		if err != nil || agrees {
			return err
		}
		return putChildPageBlock(app, ownerID, workspaceID, parentID, page.Id, actorID, true,
			childPageSidebarSpot(app, ownerID, workspaceID, parentID, page.Id, *requested))
	}
	return nil
}

// placeMadeChildPageBlock puts a page someone made under a page in its
// parent's body (placeChildPageBlock), which edits the parent, as Notion's: a
// sub-page made with the sidebar's '+' brings its parent's last edit to now
// and the parent first in a section sorted by last edit (seen 2026-11-17
// twice, in two tabs of the free test workspace's private section), as a
// line made a page ("/페이지") does through the body's change
// (noteGraphPageEdited). A page an import brings keeps its parent's edit
// time.
func placeMadeChildPageBlock(
	app core.App, authorization workspaceAuthorization, input pageCreationAdmissionInput,
	pageID, actorID string, placement pageBodyPlacement,
) error {
	if err := placeChildPageBlock(
		app, authorization.OwnerID, authorization.WorkspaceID, input.ParentID, pageID, actorID, placement,
	); err != nil || input.ParentID == "" || input.Imported {
		return err
	}
	return notePageContentEdited(context.Background(), app, input.ParentID, actorID)
}

// placeChildPageCopy puts a copied page's block right after the block of the
// page it copies, when both sit in the same body, and moves the copy next to
// it in the sidebar; otherwise the copy's block goes at the body's end. The
// copy edits its parent as a sub-page made there does (placeMadeChildPageBlock):
// Notion's '⋯ › 복제' of a sub-page brought its parent's last edit to the
// copy's minute (seen 2026-11-17, its API's last_edited_time). activity is
// the copy's own where its placing is no line (pageDuplicationQuietActivity).
func placeChildPageCopy(app core.App, ownerID, workspaceID, parentPageID, sourcePageID, copyPageID, actorID string, activity ...workspaceActivityGraphContext) error {
	holder, held, err := childPageBlockHolder(app, ownerID, sourcePageID)
	if err != nil {
		return err
	}
	placement := pageBodyPlacement{Type: "end"}
	if held && holder == parentPageID {
		placement = pageBodyPlacement{Type: "after", AfterKind: pageBodyItemPage, AfterID: sourcePageID}
	}
	if err := placeChildPageBlock(app, ownerID, workspaceID, parentPageID, copyPageID, actorID, placement, activity...); err != nil || parentPageID == "" {
		return err
	}
	return notePageContentEdited(context.Background(), app, parentPageID, actorID)
}
