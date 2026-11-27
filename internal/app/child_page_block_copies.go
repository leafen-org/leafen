package app

import (
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A copied block that holds a sub-page's block copies the sub-page too, as
// in Notion: the copied block first links to the page it copies, and the
// page's own copy, made by a page duplication, takes the link's place once
// it is ready. A page that can't be copied keeps the link.

type childPageBlockCopy struct {
	placeholderID, pageID string
}

// admitChildPageBlockCopy starts copying a sub-page into the page holding a
// placeholder link to it, and reports whether the copy started. A page the
// person can't copy now keeps the link. A planned ID is the copy's ID the
// copied blocks already name (planBlockCopyPages).
func admitChildPageBlockCopy(
	app core.App, actorID, parentPageID, placeholderID, sourcePageID, requestID, plannedID string,
) (bool, error) {
	if copies, err := blockCopyCopiesSubPages(app, parentPageID); err != nil || !copies {
		return false, err
	}
	_, err := admitPageDuplicationInPlace(app, actorID, sourcePageID, pageDuplicationInput{
		RequestID: requestID, DestinationParentID: parentPageID,
	}, placeholderID, plannedID)
	var failure *pageDuplicationFailure
	if isPermissionDenied(err) || errors.As(err, &failure) {
		return false, nil
	}
	return err == nil, err
}

// copyBlockCopySubPages starts copying the sub-pages whose blocks a block
// copy copied, into the page the blocks went to; their copied blocks are
// the links the copy made of them.
func copyBlockCopySubPages(app core.App, job storedBlockMoveJob) error {
	placeholders := []struct {
		Source string `db:"block_id"`
		Target string `db:"target_block_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[item]].[[block_id]], [[item]].[[target_block_id]]
		FROM {{leafen_block_move_items}} AS [[item]]
		JOIN {{leafen_blocks}} AS [[copy]] ON [[copy]].[[id]] = [[item]].[[target_block_id]]
			AND [[copy]].[[page_id]] = {:destinationPageId}
		WHERE [[item]].[[job_id]] = {:jobId} AND [[copy]].[[type]] = 'link_to_page'
			AND json_valid([[copy]].[[content]])
			AND json_extract([[copy]].[[content]], '$.childPageId') = [[item]].[[block_id]]
		ORDER BY [[item]].[[depth]], [[item]].[[position]], [[item]].[[block_id]]
	`).Bind(dbx.Params{"jobId": job.ID, "destinationPageId": job.DestinationPageID}).All(&placeholders); err != nil {
		return err
	}
	// Every sub-page's copy starts before any is tied to the block copy: one
	// tied keeps the copy's page closed to writes until it is done
	// (blockMoveActiveFenceSQL), and the next sub-page's copy, refused its
	// place there, stayed a link (2026-11-13: of a button's eight sub-pages,
	// only the first came as a page each press).
	admitted := make([]string, 0, len(placeholders))
	for _, placeholder := range placeholders {
		planned, err := pageDuplicationReferenceMappings(app, job.ID, "page", []string{placeholder.Source})
		if err != nil {
			return err
		}
		started, err := admitChildPageBlockCopy(
			app, job.ActorID, job.DestinationPageID, placeholder.Target, placeholder.Source,
			"block-copy-page:"+childPageBlockDigest(job.ID, placeholder.Source), planned[placeholder.Source],
		)
		if err != nil {
			return err
		}
		if started {
			admitted = append(admitted, placeholder.Source)
		}
	}
	for _, source := range admitted {
		if err := linkNativePageCopyUndo(app, job, source); err != nil {
			return err
		}
	}
	return nil
}

// replaceChildPageCopyPlaceholder puts a copied page's block where the
// placeholder link to the page it copies stands, and takes the link away.
// It reports false when the link is gone, and the copy goes where copies go.
// activity is the copy's own where its placing is no line
// (pageDuplicationQuietActivity).
func replaceChildPageCopyPlaceholder(
	app core.App, ownerID, workspaceID, parentPageID, placeholderID, sourcePageID, copyPageID, actorID string,
	activity ...workspaceActivityGraphContext,
) (bool, error) {
	if _, inWiki, err := loadWikiPlace(app, parentPageID); err != nil || inWiki {
		return false, err
	}
	batches := map[string]*structuralMutationBatch{}
	batch, err := loadStructuralMutationBatch(app, parentPageID, batches)
	if err != nil {
		return false, err
	}
	metadata, err := structuralMetadata(batch.doc)
	if err != nil {
		return false, err
	}
	placeholder, found := metadata[placeholderID]
	linked, _ := placeholder.Content["childPageId"].(string)
	if _, copied := metadata[copyPageID]; !found || copied ||
		placeholder.Type != graphBlockTypeLinkToPage || linked != sourcePageID {
		return false, nil
	}
	var parentID *string
	if placeholder.ParentID != "" {
		container := placeholder.ParentID
		parentID = &container
	}
	after := placeholderID
	placement := []graphOperation{
		{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: copyPageID, ParentID: parentID, AfterID: &after,
			Type: graphBlockTypeChildPage, Content: childPageBlockContent(copyPageID),
		}},
		{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: placeholderID}},
	}
	history, err := beginServerPageHistoryCapture(app, parentPageID, batch.doc, placement)
	if err != nil {
		return false, err
	}
	if _, err := applyStructuralGraphMutation(
		app, batch.doc, ownerID, workspaceID, parentPageID, actorID, placement, false, nil,
	); err != nil {
		return false, err
	}
	if err := saveServerStructuralBatch(
		app, ownerID, parentPageID, actorID, "child-page-block:"+copyPageID, batch, history, activity...,
	); err != nil {
		return false, err
	}
	metadata, err = structuralMetadata(batch.doc)
	if err != nil {
		return false, err
	}
	return true, alignChildPageSidebar(app, batch.doc, metadata, ownerID, workspaceID, parentPageID, []string{copyPageID})
}
