package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A Notion wiki's page sits as a page block in the body of the page it is
// under in the wiki — the wiki's home for one at its top — however it was
// made (seen 2026-11-11: from that body's "/페이지", the sidebar's "+" and a
// view's new page, each at the end of the body), and its block goes with it
// when it moves. Here a wiki's page is its database's, so its block is a
// link marked as the wiki's (child_page_block_moves.go): each page of a wiki
// keeps one, in the body of the page above it.

// wikiPageLinkPlace is where a wiki's page belongs: the body of the page
// above it in the wiki, read with the page's owner and workspace.
type wikiPageLinkPlace struct {
	OwnerID     string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	HolderID    string `db:"holder_id"`
}

// keepWikiPageLinks puts each page of a wiki named in the body of the page
// above it, at that body's end, taking its marked link out of another body
// it was in; a page whose link or block that body holds already keeps it,
// and a page of no wiki, or in the trash, is left alone.
func (p *pocketBaseYgoPersistence) keepWikiPageLinks(ctx context.Context, actorID string, pageIDs []string) error {
	// Rows of other databases, the many a paste may add, are passed over at once.
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return err
	}
	wikiPages := []string{}
	if err := p.app.DB().NewQuery(`
		SELECT [[row]].[[id]] FROM {{leafen_database_rows}} AS [[row]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[row]].[[data_source_id]] AND [[source]].[[wiki]] = 1
		WHERE [[row]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&wikiPages); err != nil {
		return err
	}
	// Each is what a change leaves to write, done again as the server starts
	// when its stop cut it, with those after it, the change then answered
	// as made (ygo_persistence_follow_ups.go).
	ends := make([]func(cut bool) bool, len(wikiPages))
	for index, pageID := range wikiPages {
		ends[index] = productAfterworkOf(p.app).begin(wikiPageLinkKept(actorID, pageID))
	}
	for index, pageID := range wikiPages {
		err := ctx.Err()
		cut := false
		if err == nil {
			err = p.runFollowUp(func(txApp core.App) error {
				return keepWikiPageLink(txApp, actorID, pageID)
			})
			cut = followUpCut(err)
		}
		if err != nil {
			left := cut
			for _, end := range ends[index:] {
				left = end(cut) && left
			}
			if left {
				return nil
			}
			return err
		}
		ends[index](false)
	}
	return nil
}

func keepWikiPageLink(app core.App, actorID, pageID string) error {
	place := wikiPageLinkPlace{}
	err := app.DB().NewQuery(`
		SELECT [[row]].[[owner]], [[row]].[[workspace_id]],
			CASE WHEN [[row]].[[subitem_parent_id]] != '' THEN [[row]].[[subitem_parent_id]] ELSE [[source]].[[page_id]] END AS [[holder_id]]
		FROM {{leafen_database_rows}} AS [[row]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[row]].[[data_source_id]] AND [[source]].[[wiki]] = 1
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[row]].[[id]] AND [[page]].[[in_trash]] = 0
		WHERE [[row]].[[id]] = {:id} AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]") + `
	`).Bind(dbx.Params{"id": pageID}).One(&place)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// A page block of it, made before it was the wiki's, becomes its link first.
	if err := swapChildPageBlocksForWikiLinks(app, place.OwnerID, place.WorkspaceID, actorID, []string{pageID}); err != nil {
		return err
	}
	links, err := wikiPageLinkBlocks(app, place.OwnerID, pageID)
	if err != nil {
		return err
	}
	held := false
	for _, link := range links {
		if link.PageID == place.HolderID {
			held = true
			continue
		}
		if err := changeWikiPageLinkBody(app, place, link.PageID, actorID, "wiki-page-link-leave:"+pageID, []graphOperation{{
			Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: link.ID},
		}}); err != nil {
			return err
		}
	}
	if held {
		return nil
	}
	content, err := json.Marshal(map[string]any{"childPageId": pageID, wikiPageLinkMark: true})
	if err != nil {
		return err
	}
	return changeWikiPageLinkBody(app, place, place.HolderID, actorID, "wiki-page-link:"+pageID, []graphOperation{{
		Kind:  graphOperationKindUpsertBlock,
		Block: &graphBlock{ID: uuid.NewString(), Type: graphBlockTypeLinkToPage, Content: content},
	}})
}

// changeWikiPageLinkBody makes a change to a body's top level, a new block
// going at its end, and keeps it for the body's readers and its history.
func changeWikiPageLinkBody(
	app core.App, place wikiPageLinkPlace, bodyID, actorID, reason string, operations []graphOperation,
) error {
	batch, err := loadStructuralMutationBatch(app, bodyID, map[string]*structuralMutationBatch{})
	if err != nil {
		return err
	}
	start := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Block == nil {
			continue
		}
		end, err := endOfChildPageContainer(batch.doc, "")
		if err != nil {
			return err
		}
		operation.Block.AfterID = end.After
		if end.After == nil {
			start[operation.Block.ID] = struct{}{}
		}
	}
	history, err := beginServerPageHistoryCapture(app, bodyID, batch.doc, operations)
	if err != nil {
		return err
	}
	if _, err := applyStructuralGraphMutation(
		app, batch.doc, place.OwnerID, place.WorkspaceID, bodyID, actorID, operations, false, start,
	); err != nil {
		return err
	}
	return saveServerStructuralBatch(app, place.OwnerID, bodyID, actorID, reason, batch, history)
}

// wikiPageLink is a marked link to a wiki's page, in the body holding it.
type wikiPageLink struct {
	ID     string `db:"id"`
	PageID string `db:"page_id"`
}

// wikiPageLinkBlocks reads the marked links to a page, wherever they are.
func wikiPageLinkBlocks(app core.App, ownerID, pageID string) ([]wikiPageLink, error) {
	links := []wikiPageLink{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[page_id]] FROM {{leafen_blocks}}
		WHERE [[owner]] = {:owner} AND [[type]] = 'link_to_page' AND json_valid([[content]])
			AND json_extract([[content]], '$.childPageId') = {:page}
			AND IFNULL(json_extract([[content]], '$.` + wikiPageLinkMark + `'), 0) = 1
		ORDER BY [[page_id]], [[id]]
	`).Bind(dbx.Params{"owner": ownerID, "page": pageID}).All(&links)
	return links, err
}

// dropWikiPageLinks takes a page's marked links out of the bodies holding
// them once the page has left its wiki as a page of its own, whose block its
// new place holds instead (page_row_conversion.go).
func dropWikiPageLinks(app core.App, ownerID, workspaceID, actorID, pageID string) error {
	links, err := wikiPageLinkBlocks(app, ownerID, pageID)
	if err != nil {
		return err
	}
	place := wikiPageLinkPlace{OwnerID: ownerID, WorkspaceID: workspaceID}
	for _, link := range links {
		if err := changeWikiPageLinkBody(app, place, link.PageID, actorID, "wiki-page-link-leave:"+pageID, []graphOperation{{
			Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: link.ID},
		}}); err != nil {
			return err
		}
	}
	return nil
}

// wikiPageLinkFollowsTrash takes a wiki page's link out of the body above it
// while the page is in the trash, as a page's block leaves its parent's body
// (child_page_block_lifecycle.go), and puts it back at that body's end when
// the page comes back.
func (p *pocketBaseYgoPersistence) wikiPageLinkFollowsTrash(
	ctx context.Context, actorID string, row databaseRowPageAuthority, trash bool,
) error {
	if !trash {
		return p.keepWikiPageLinks(ctx, actorID, []string{row.RowID})
	}
	left := databasePageLeft{ownerID: row.OwnerID, workspaceID: row.WorkspaceID, actorID: actorID, pageID: row.RowID}
	return p.runKeptFollowUp(left.kept(), left.write)
}

// placeWikiPageLast puts a page of a wiki last among the pages under the page
// it sits under in the wiki — the wiki's pages under it and its own
// sub-pages — as its link goes last in that page's body.
func placeWikiPageLast(app core.App, ownerID, workspaceID, pageID, sourceID, holderID string) error {
	var position float64
	// Found by their parents' key: a wiki's page has its data source as its
	// parent (database_row_page_record_schema.go). "parent_id = holder OR
	// data_source_id = source" alone left SQLite no key, and it read every
	// page of the workspace (2026-11-18).
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[sibling]].[[position]]), 0) + 1 FROM {{leafen_pages}} AS [[sibling]]
		WHERE [[sibling]].[[parent_id]] IN ({:holderId}, {:sourceId})
			AND +[[sibling]].[[owner]] = {:owner} AND +[[sibling]].[[workspace]] = {:workspaceId} AND [[sibling]].[[id]] != {:pageId}
			AND ([[sibling]].[[parent_id]] = {:holderId} OR [[sibling]].[[data_source_id]] = {:sourceId})
			AND ` + pageTreeParentSQL("[[sibling]]") + ` = {:holderId}
	`).Bind(dbx.Params{
		"owner": ownerID, "workspaceId": workspaceID, "pageId": pageID, "holderId": holderID, "sourceId": sourceID,
	}).Row(&position); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`UPDATE {{leafen_pages}} SET [[position]] = {:position} WHERE [[id]] = {:pageId}`).
		Bind(dbx.Params{"position": position, "pageId": pageID}).Execute()
	return err
}

// placeAddedWikiPagesLast puts each page of a wiki a person added in its
// views last under the page above it, as Notion's sidebar lists a view's new
// page after the others, in the order of that page's body (seen 2026-11-11).
func placeAddedWikiPagesLast(app core.App, rowIDs []string) error {
	if len(rowIDs) == 0 {
		return nil
	}
	encoded, err := json.Marshal(rowIDs)
	if err != nil {
		return err
	}
	added := []struct {
		ID          string `db:"id"`
		OwnerID     string `db:"owner"`
		WorkspaceID string `db:"workspace_id"`
		SourceID    string `db:"data_source_id"`
		HolderID    string `db:"holder_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[row]].[[id]], [[row]].[[owner]], [[row]].[[workspace_id]], [[row]].[[data_source_id]],
			CASE WHEN [[row]].[[subitem_parent_id]] != '' THEN [[row]].[[subitem_parent_id]] ELSE [[source]].[[page_id]] END AS [[holder_id]]
		FROM {{leafen_database_rows}} AS [[row]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[row]].[[data_source_id]] AND [[source]].[[wiki]] = 1
		WHERE [[row]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&added); err != nil {
		return err
	}
	for _, page := range added {
		if err := placeWikiPageLast(app, page.OwnerID, page.WorkspaceID, page.ID, page.SourceID, page.HolderID); err != nil {
			return err
		}
	}
	return nil
}
