package app

import (
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Moving blocks to another page moves the sub-pages whose blocks go with
// them, and those hanging on lines that go, as in Notion: each page's
// parent becomes the page its block or line now sits in.

// blockSubtreeChildPages lists the sub-pages whose blocks are in a block's
// subtree, the block included.
func blockSubtreeChildPages(app core.App, ownerID, pageID, rootID string) ([]string, error) {
	ids := []string{}
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[tree]]([[id]]) AS (
			SELECT [[id]] FROM {{leafen_blocks}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:page} AND [[id]] = {:root}
			-- However deep the subtree goes; UNION keeps each block once.
			-- CROSS JOIN climbs from the subtree by key: SQLite's own order
			-- read the page's or the server's every block (2026-10-18).
			UNION
			SELECT [[child]].[[id]] FROM [[tree]]
			CROSS JOIN {{leafen_blocks}} AS [[child]] ON [[child]].[[parent_id]] = [[tree]].[[id]]
			WHERE [[child]].[[owner]] = {:owner} AND [[child]].[[page_id]] = {:page}
		)
		SELECT [[block]].[[id]] FROM [[tree]]
		CROSS JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]] = [[tree]].[[id]]
		WHERE [[block]].[[type]] = 'child_page' AND json_valid([[block]].[[content]])
			AND json_extract([[block]].[[content]], '$.childPageId') = [[block]].[[id]]
		UNION
		-- The pages hanging on its lines go with them (mention_anchored_pages.go).
		SELECT [[page]].[[id]] FROM [[tree]]
		CROSS JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[parent_id]] = {:page}
			AND [[page]].[[anchor_block_id]] = [[tree]].[[id]]
		WHERE [[page]].[[anchor_block_id]] != ''
		ORDER BY 1
	`).Bind(dbx.Params{"owner": ownerID, "page": pageID, "root": rootID}).Column(&ids)
	return ids, err
}

// childPagesMayMoveTo reports whether sub-pages may follow their blocks to
// another page: one whose body holds page blocks, in the same teamspace and
// private pages as the page they leave, and not below any of them; each
// sub-page takes the destination's sharing, so the mover needs its full
// access, as a page's own move does. No pages named is the source's whole
// body, with every sub-page in it.
func childPagesMayMoveTo(app core.App, actorID string, pageIDs []string, sourcePageID, destinationPageID string) (bool, error) {
	source, err := app.FindRecordById(pagesCollection, sourcePageID)
	if err != nil {
		return false, err
	}
	destination, err := app.FindRecordById(pagesCollection, destinationPageID)
	if err != nil {
		return false, err
	}
	if isDatabaseRowPageRecord(destination) ||
		destination.GetString("teamspace_id") != source.GetString("teamspace_id") ||
		destination.GetString(privatePageOwnerField) != source.GetString(privatePageOwnerField) {
		return false, nil
	}
	if _, inWiki, err := loadWikiPlace(app, destinationPageID); err != nil || inWiki {
		return false, err
	}
	if pageIDs == nil {
		if err := app.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_blocks}}
			WHERE [[page_id]] = {:page} AND [[type]] = 'child_page' AND json_valid([[content]])
				AND json_extract([[content]], '$.childPageId') = [[id]]
			UNION
			SELECT [[id]] FROM {{leafen_pages}} WHERE [[parent_id]] = {:page} AND [[anchor_block_id]] != ''
		`).Bind(dbx.Params{"page": sourcePageID}).Column(&pageIDs); err != nil {
			return false, err
		}
	}
	for _, pageID := range pageIDs {
		if cycle, err := pageParentCreatesCycle(app, pageID, destinationPageID); err != nil || cycle {
			return false, err
		}
		if _, err := authorizePageAccessRole(app, actorID, pageID, pageAccessRoleFullAccess); err != nil {
			if isPermissionDenied(err) {
				return false, nil
			}
			return false, err
		}
	}
	return true, nil
}

// reparentChildPage puts a sub-page under the page its block now sits in,
// as a change the page's readers take up.
func reparentChildPage(app core.App, ownerID, workspaceID, actorID, pageID, parentPageID string) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	if page.GetString("parent_id") == parentPageID {
		return nil
	}
	// The page's history starts from what it was before its first change.
	if err := ensurePageHistoryBaseline(app, pageID); err != nil {
		return err
	}
	cursor, err := nextWorkspaceUserStateCursor(app, ownerID)
	if err != nil {
		return err
	}
	// It goes last among its new siblings in the sidebar, as its block goes
	// with the blocks moved to the end of the body.
	position, err := nextPageSiblingPosition(app, workspaceID, parentPageID)
	if err != nil {
		return err
	}
	page.Set("parent_id", parentPageID)
	page.Set("position", position)
	page.Set("cursor", cursor)
	page.Set("revision", page.GetInt("revision")+1)
	if err := app.Save(page); err != nil {
		return err
	}
	if err := markSearchDirty(app, page); err != nil {
		return err
	}
	if err := recordPageHistoryMetadataMutation(app, pageID, actorID); err != nil {
		return err
	}
	// Sockets open on the page, or under it, turn read-only or editable.
	recheckCollaborationAccessAfterCommit(app)
	return nil
}

// A wiki's pages are its database's, so their page blocks can't stay page
// blocks; each becomes a link to its page where it stood, marked as the
// wiki's, as a Notion wiki's home keeps showing its pages (seen 2026-10-29:
// a page with two sub-pages turned into a wiki keeps both in its body).
// Undoing the wiki turns the marked links back into its pages' blocks.

// wikiPageLinkMark marks a link that stands for a wiki's page in a body.
const wikiPageLinkMark = "wikiPage"

// notWikiPageLinkSQL holds a block that isn't such a link: a page's block
// in Notion's wiki is no backlink of the page, so its link here is none.
// A link without the mark is an ordinary one and stays: the mark it lacks
// reads as 0, never as NULL that would drop every page link block too.
func notWikiPageLinkSQL(block string) string {
	return `NOT (` + wikiPageLinkSQL(block) + `)`
}

// wikiPageLinkSQL holds a block that is such a link, standing for a wiki's
// page where its page block stood.
func wikiPageLinkSQL(block string) string {
	return block + `.[[type]]='link_to_page' AND json_valid(` + block + `.[[content]])
		AND IFNULL(json_extract(` + block + `.[[content]], '$.` + wikiPageLinkMark + `'), 0)=1`
}

// replaceBlockInPlace is the change that puts a block where another stood:
// that one gone, the new one after the block before it. A block before it
// replaced in the same change is its replacement by then (replaced).
func replaceBlockInPlace(doc *crdt.Doc, oldID string, entry structuralBlockMetadata, block *graphBlock, replaced map[string]string) ([]graphOperation, error) {
	siblings, err := structuralOrder(doc, entry.ParentID)
	if err != nil {
		return nil, err
	}
	// The block it follows places it; its position is only a placeholder.
	block.Position = "a0"
	if entry.ParentID != "" {
		container := entry.ParentID
		block.ParentID = &container
	}
	if index := structuralIndex(siblings, oldID); index > 0 {
		previous := siblings[index-1]
		if replacement, found := replaced[previous]; found {
			previous = replacement
		}
		block.AfterID = &previous
	}
	replaced[oldID] = block.ID
	return []graphOperation{
		{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: oldID}},
		{Kind: graphOperationKindUpsertBlock, Block: block},
	}, nil
}

// bodyOrder lists blocks of a body in its order, container by container;
// ones it doesn't hold follow as given.
func bodyOrder(doc *crdt.Doc, metadata map[string]structuralBlockMetadata, blockIDs []string) []string {
	wanted, containers := map[string]bool{}, map[string]bool{}
	for _, id := range blockIDs {
		wanted[id] = true
		if entry, found := metadata[id]; found {
			containers[entry.ParentID] = true
		}
	}
	names := make([]string, 0, len(containers))
	for container := range containers {
		names = append(names, container)
	}
	sort.Strings(names)
	ordered, seen := []string{}, map[string]bool{}
	for _, container := range names {
		order, err := structuralOrder(doc, container)
		if err != nil {
			continue
		}
		for _, id := range order {
			if wanted[id] && !seen[id] {
				seen[id] = true
				ordered = append(ordered, id)
			}
		}
	}
	for _, id := range blockIDs {
		if !seen[id] {
			ordered = append(ordered, id)
		}
	}
	return ordered
}

// childPageBlockWikiLinks is the change that turns the blocks of pages
// becoming a wiki's, in a body being changed, into links to them.
func childPageBlockWikiLinks(doc *crdt.Doc, pageIDs []string) ([]graphOperation, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return nil, err
	}
	operations, replaced := []graphOperation{}, map[string]string{}
	// In body order, so a block's predecessor is replaced before it.
	for _, pageID := range bodyOrder(doc, metadata, pageIDs) {
		if !isChildPageBlock(pageID, metadata[pageID]) {
			continue
		}
		content, _ := json.Marshal(map[string]any{"childPageId": pageID, wikiPageLinkMark: true})
		link := &graphBlock{ID: uuid.NewString(), Type: graphBlockTypeLinkToPage, Content: content}
		swap, err := replaceBlockInPlace(doc, pageID, metadata[pageID], link, replaced)
		if err != nil {
			return nil, err
		}
		operations = append(operations, swap...)
	}
	return operations, nil
}

// swapChildPageBlocksForWikiLinks turns the blocks of pages becoming a
// wiki's, in the bodies holding them, into links to them; a block already
// turned is left alone.
func swapChildPageBlocksForWikiLinks(app core.App, ownerID, workspaceID, actorID string, pageIDs []string) error {
	for _, pageID := range pageIDs {
		holder, held, err := childPageBlockHolder(app, ownerID, pageID)
		if err != nil || !held {
			if err != nil {
				return err
			}
			continue
		}
		batch, err := loadStructuralMutationBatch(app, holder, map[string]*structuralMutationBatch{})
		if err != nil {
			return err
		}
		swap, err := childPageBlockWikiLinks(batch.doc, []string{pageID})
		if err != nil || len(swap) == 0 {
			if err != nil {
				return err
			}
			continue
		}
		history, err := beginServerPageHistoryCapture(app, holder, batch.doc, swap)
		if err != nil {
			return err
		}
		if _, err := applyStructuralGraphMutation(app, batch.doc, ownerID, workspaceID, holder, actorID, swap, false, nil); err != nil {
			return err
		}
		if err := saveServerStructuralBatch(app, ownerID, holder, actorID, "wiki-page-link:"+pageID, batch, history); err != nil {
			return err
		}
	}
	return nil
}

// wikiPageLinkReturns is the change that turns a body's links marked as a
// wiki's, for pages leaving the wiki, back into those pages' blocks where
// they stand; a page whose block is already somewhere keeps its link.
func wikiPageLinkReturns(app core.App, ownerID string, doc *crdt.Doc, pageIDs []string) ([]graphOperation, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return nil, err
	}
	leaving := map[string]bool{}
	for _, pageID := range pageIDs {
		leaving[pageID] = true
	}
	links := []string{}
	for id, entry := range metadata {
		target, _ := entry.Content["childPageId"].(string)
		marked, _ := entry.Content[wikiPageLinkMark].(bool)
		if entry.Type == graphBlockTypeLinkToPage && marked && leaving[target] {
			links = append(links, id)
		}
	}
	sort.Strings(links)
	operations, replaced, returned := []graphOperation{}, map[string]string{}, map[string]bool{}
	for _, id := range bodyOrder(doc, metadata, links) {
		target, _ := metadata[id].Content["childPageId"].(string)
		if _, held, err := childPageBlockHolder(app, ownerID, target); err != nil || held || returned[target] {
			if err != nil {
				return nil, err
			}
			continue
		}
		returned[target] = true
		block := &graphBlock{ID: target, Type: graphBlockTypeChildPage, Content: childPageBlockContent(target)}
		swap, err := replaceBlockInPlace(doc, id, metadata[id], block, replaced)
		if err != nil {
			return nil, err
		}
		operations = append(operations, swap...)
	}
	return operations, nil
}

// returnWikiPageLinks turns the marked links for pages leaving a wiki, in
// the body of the page each now sits under, back into their blocks.
func returnWikiPageLinks(app core.App, ownerID, workspaceID, actorID, skipPageID string, pageIDs []string) error {
	holders := map[string][]string{}
	for _, pageID := range pageIDs {
		page, err := app.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		if parent := page.GetString("parent_id"); parent != "" && parent != skipPageID {
			holders[parent] = append(holders[parent], pageID)
		}
	}
	for holder, leaving := range holders {
		batch, err := loadStructuralMutationBatch(app, holder, map[string]*structuralMutationBatch{})
		if err != nil {
			return err
		}
		returns, err := wikiPageLinkReturns(app, ownerID, batch.doc, leaving)
		if err != nil || len(returns) == 0 {
			if err != nil {
				return err
			}
			continue
		}
		if _, err := applyStructuralGraphMutation(app, batch.doc, ownerID, workspaceID, holder, actorID, returns, false, nil); err != nil {
			return err
		}
		if err := saveServerStructuralBatch(app, ownerID, holder, actorID, "wiki-page-link-return", batch, nil); err != nil {
			return err
		}
	}
	return nil
}
