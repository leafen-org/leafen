package app

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A sub-page made from a line's "@" or "+" menu ("새 “…” 하위 페이지") has no
// block of its own in its parent's body, as Notion's (seen 2026-11-12, user
// decision 2026-11-11): it hangs on the line it was made from, whose mention
// of it is its anchor, and the page names that line's block. The sidebar
// lists it among its parent's sub-pages where the line stands
// (child_page_sidebar.go), and the line moved to another page takes it
// along (block_move_transfer.go). When the line goes, or a person takes the
// line's mention of the page out, the page goes to the trash, marked as gone
// with its anchor; when the line or the mention comes back (Undo, a cut line
// pasted again, two lines joined) the page comes back with it, hanging where
// its mention now stands. A mention an integration takes out leaves the page
// hanging unmentioned (mention_anchored_unmentioned.go, anchoredPageStepOf).
// A page sent to the trash by itself stays there
// whatever its line does; restored, it becomes an ordinary sub-page with a
// block of its own, as Notion's restore makes it (restoreMentionAnchoredPage),
// as one moved under another page does. A page
// made from a table cell, a caption, a page's title or a database page's
// cell hangs on that field so (mention_anchored_fields.go).
//
// A second page made from the same line's or title's "@" hangs there beside
// the first, as every one made from a table row's cell or a caption does:
// Notion's sends the page already hanging on that line or title to the
// trash, its mention left saying so, though its own sidebar keeps listing
// it (seen 2026-11-18 in two tabs of a Notion test page); Leafen keeps every
// page a person made there (an approved difference).

type mentionAnchoredPage struct {
	ID          string `db:"id"`
	Anchor      string `db:"anchor_block_id"`
	InTrash     bool   `db:"in_trash"`
	Lost        bool   `db:"anchor_lost"`
	Unmentioned bool   `db:"anchor_unmentioned"`
}

// textMentionsPage reports whether a block's text, as it stands, mentions a
// page (a mention of one of its blocks is not one of the page).
func textMentionsPage(text *crdt.YText, pageID string) bool {
	return slices.Contains(textPageMentions(text), pageID)
}

// setPageAnchor names the line a page hangs on, and whether it is in the
// trash for its line's sake; one hanging nowhere hangs unmentioned nowhere
// either (mention_anchored_unmentioned.go).
func setPageAnchor(app core.App, pageID, blockID string, lost bool) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[anchor_block_id]] = {:block}, [[anchor_lost]] = {:lost}
		WHERE [[id]] = {:id} AND ([[anchor_block_id]] != {:block} OR [[anchor_lost]] != {:lost})
	`).Bind(dbx.Params{"id": pageID, "block": blockID, "lost": lost}).Execute()
	if err != nil || blockID != "" {
		return err
	}
	return setPageAnchorUnmentioned(app, pageID, false)
}

// pageAnchorsSeenStoreKey marks an app whose pages can hang on lines.
const pageAnchorsSeenStoreKey = "leafenPageAnchorsSeen"

// pageAnchorsKnown reports whether pages can hang on lines yet: the sidebar
// and the body fill read them, which migrations before 768 run too.
func pageAnchorsKnown(app core.App) (bool, error) {
	if app.Store().Has(pageAnchorsSeenStoreKey) {
		return true, nil
	}
	var columns int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_pages') WHERE [[name]] = {:name}`).
		Bind(dbx.Params{"name": pageAnchorBlockField}).Row(&columns); err != nil || columns == 0 {
		return false, err
	}
	app.Store().Set(pageAnchorsSeenStoreKey, true)
	return true, nil
}

// loadMentionAnchors reads the live sub-pages of a page that hang on its
// lines, by line, in their sidebar order.
func loadMentionAnchors(app core.App, parentPageID string) (map[string][]string, error) {
	anchors := map[string][]string{}
	if known, err := pageAnchorsKnown(app); err != nil || !known {
		return anchors, err
	}
	pages := []mentionAnchoredPage{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[anchor_block_id]] FROM {{leafen_pages}}
		WHERE [[parent_id]] = {:parent} AND [[anchor_block_id]] != '' AND [[in_trash]] = FALSE
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"parent": parentPageID}).All(&pages); err != nil {
		return nil, err
	}
	for _, page := range pages {
		anchors[page.Anchor] = append(anchors[page.Anchor], page.ID)
	}
	return anchors, nil
}

// pagesAnchoredOn lists the sub-pages of a page, in the trash or not, that
// hang on any of the named lines.
func pagesAnchoredOn(app core.App, parentPageID string, blockIDs []string) ([]string, error) {
	ids := []string{}
	if len(blockIDs) == 0 {
		return ids, nil
	}
	encoded, _ := json.Marshal(blockIDs)
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_pages}}
		WHERE [[parent_id]] = {:parent} AND [[anchor_block_id]] != ''
			AND [[anchor_block_id]] IN (SELECT [[value]] FROM json_each({:blocks}))
		ORDER BY [[id]]
	`).Bind(dbx.Params{"parent": parentPageID, "blocks": string(encoded)}).Column(&ids)
	return ids, err
}

// settleMentionAnchoredPages keeps the sub-pages hanging on a page's lines
// with their lines after some of its blocks changed (blocks; every block
// when whole, made as Undo makes a line again), as a person's change does
// (anchoredPageStepOf). A line the server doesn't have yet takes no page
// away: a page made from a new line may come before the line does. The
// pages it brought back, or back to the sidebar, are returned for the
// sidebar to place.
func settleMentionAnchoredPages(
	app core.App, actorID, parentPageID, mutationID string, blocks, made []string, whole bool,
) ([]string, error) {
	return settleAnchoredPages(app, actorID, parentPageID, mutationID, anchorChange{blocks: blocks, made: made, whole: whole})
}

// anchorChange is what a change did to a page's anchors: the blocks it wrote
// or took away (blocks; every block when whole) and those it made again
// (made); kept, an integration made it through the Public API.
type anchorChange struct {
	blocks, made []string
	whole, kept  bool
}

type anchoredPageStep int

const (
	anchoredPageStays anchoredPageStep = iota
	anchoredPageBack
	anchoredPageMentioned
	anchoredPageUnmentioned
	anchoredPageGone
)

// anchoredPageStepOf is what a change does to a page hanging on its parent's
// line, block or field, by whether that still mentions it (holds) and
// stands (live), as Notion's does (seen 2026-11-12 and, for the pages left
// unmentioned, 2026-11-18 in two tabs of a Notion test page,
// mention_anchored_unmentioned.go):
//   - mentioned again, one in the trash for its anchor's sake comes back
//     and one hanging unmentioned is listed again;
//   - its mention taken out by a person, it goes to the trash, though the
//     mention went on to another line, as two lines joined (the joined line
//     keeps the mention, of a page in the trash); taken out by an
//     integration, of a line, a block, a title or a cell alike, it stays
//     there unmentioned, and later writes of it leave it so;
//   - its line or block gone, it goes to the trash, unmentioned or not, and
//     comes back with it: one that went unmentioned comes back so, with no
//     mention; one sent to the trash when its mention went stays there.
func anchoredPageStepOf(page mentionAnchoredPage, holds bool, live map[string]bool, change anchorChange) anchoredPageStep {
	standing := pageAnchorOnField(page.Anchor) || live[page.Anchor]
	switch {
	case holds && page.InTrash && change.kept && !slices.Contains(change.made, page.Anchor):
		// An integration's mention of a page in the trash (Notion's not seen)
		// leaves it there; the block it restores brings it back.
		return anchoredPageStays
	case holds && page.InTrash:
		return anchoredPageBack
	case holds && page.Unmentioned:
		return anchoredPageMentioned
	case holds:
		return anchoredPageStays
	case page.InTrash:
		if page.Unmentioned && standing && slices.Contains(change.made, page.Anchor) {
			return anchoredPageBack
		}
		return anchoredPageStays
	case !slices.Contains(change.blocks, page.Anchor) && !(change.whole && live[page.Anchor]):
		return anchoredPageStays
	case !standing:
		return anchoredPageGone
	case page.Unmentioned:
		return anchoredPageStays
	case change.kept:
		return anchoredPageUnmentioned
	}
	return anchoredPageGone
}

// settleAnchoredPages takes each page hanging on a change's blocks its
// anchoredPageStepOf.
func settleAnchoredPages(app core.App, actorID, parentPageID, mutationID string, change anchorChange) ([]string, error) {
	named := slices.Concat(change.blocks, change.made)
	if actorID == "" || parentPageID == "" || (!change.whole && len(named) == 0) {
		return nil, nil
	}
	unmentioned, err := pageAnchorUnmentionedColumn(app)
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(named)
	pages := []mentionAnchoredPage{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[anchor_block_id]], [[in_trash]], [[anchor_lost]], ` + unmentioned + ` AS [[anchor_unmentioned]]
		FROM {{leafen_pages}}
		WHERE [[parent_id]] = {:parent} AND [[anchor_block_id]] != ''
			AND ({:whole} OR [[anchor_block_id]] IN (SELECT [[value]] FROM json_each({:blocks})))
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"parent": parentPageID, "whole": change.whole, "blocks": string(encoded)}).All(&pages); err != nil {
		return nil, err
	}
	// A page in the trash by itself is left there; one on a field (a title,
	// a cell) follows only that field's writes.
	pages = slices.DeleteFunc(pages, func(page mentionAnchoredPage) bool {
		return page.InTrash && !page.Lost || pageAnchorOnField(page.Anchor) && !slices.Contains(named, page.Anchor)
	})
	if len(pages) == 0 {
		return nil, nil
	}
	holding, live, err := pageAnchorsHold(app, parentPageID, pages)
	if err != nil {
		return nil, err
	}
	placed := []string{}
	for _, page := range pages {
		switch anchoredPageStepOf(page, holding[page.ID], live, change) {
		case anchoredPageBack:
			if err := setChildPageTrash(app, actorID, page.ID, false, mutationID+":anchor"); err != nil {
				return nil, err
			}
			placed = append(placed, page.ID)
		case anchoredPageMentioned:
			if err := setPageAnchorUnmentioned(app, page.ID, false); err != nil {
				return nil, err
			}
			placed = append(placed, page.ID)
		case anchoredPageUnmentioned:
			if err := setPageAnchorUnmentioned(app, page.ID, true); err != nil {
				return nil, err
			}
		case anchoredPageGone:
			if err := setPageAnchor(app, page.ID, page.Anchor, true); err != nil {
				return nil, err
			}
			if err := setChildPageTrash(app, actorID, page.ID, true, mutationID+":anchor"); err != nil {
				return nil, err
			}
		}
	}
	return placed, nil
}

// settleMentionAnchoredPagesAfterText keeps a page's anchored sub-pages with
// its text once a person's typing is saved, in a change of its own: the
// text stays saved when the pages can't follow it.
func settleMentionAnchoredPagesAfterText(app core.App, actorID, pageID string, blocks []string, whole bool) {
	if actorID == "" {
		return
	}
	var anchored int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[parent_id]] = {:parent} AND [[anchor_block_id]] != ''
	`).Bind(dbx.Params{"parent": pageID}).Row(&anchored); err != nil || anchored == 0 {
		return
	}
	err := app.RunInTransaction(func(txApp core.App) error {
		placed, err := settleMentionAnchoredPages(txApp, actorID, pageID, "text-anchor:"+newDiagnosticReference(), blocks, nil, whole)
		if err != nil || len(placed) == 0 {
			return err
		}
		return alignMentionAnchoredPages(txApp, pageID, placed)
	})
	if err != nil && !isPermissionDenied(err) {
		logDiagnostic(app, http.StatusInternalServerError, errorCodeYgoPersistence, newDiagnosticReference(),
			"page.anchors", "settle", "", err, "")
	}
}

// alignMentionAnchoredPages puts anchored pages in their parent's sidebar
// where their lines stand.
func alignMentionAnchoredPages(app core.App, parentPageID string, pageIDs []string) error {
	parent, err := app.FindRecordById(pagesCollection, parentPageID)
	if err != nil {
		return err
	}
	doc, _, err := loadYgoStructuralDoc(app, parentPageID)
	if err != nil {
		return err
	}
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return err
	}
	return alignChildPageSidebar(app, doc, metadata, parent.GetString("owner"), parent.GetString("workspace"), parentPageID, pageIDs)
}

// restoreMentionAnchoredPage places an anchored page back from the trash and
// reports whether it did; false leaves the page, no longer hanging, to get
// a block at its parent's body end. One in the trash for its line's sake
// (Undo bringing its line or mention back, settleMentionAnchoredPages)
// hangs on its line, or its field, again while that mentions it, or while
// it stands for one that went with it unmentioned there. One sent
// to the trash by itself comes back as Notion's restore brings it (its trash
// window, the trashed page's banner and its API alike, seen 2026-11-18 in
// two tabs of a Notion test page): no longer hanging, with a block of its own
// though its field still mentions it — the last inside its line, for a line
// that takes blocks inside (restoredLineHoldsPage), and at the body's end
// for one on the title or any other line. One on a table row's cells or a
// caption, whose block shows none inside, hangs there again, as Notion's
// shows nothing new in the body; one on a database page's cell too (Notion's
// not seen).
func restoreMentionAnchoredPage(app core.App, ownerID, workspaceID, actorID string, page *core.Record) (bool, error) {
	anchor, parentID := page.GetString(pageAnchorBlockField), page.GetString("parent_id")
	if anchor == "" {
		return false, nil
	}
	holding, live, err := pageAnchorsHold(app, parentID, []mentionAnchoredPage{{ID: page.Id, Anchor: anchor}})
	if err != nil {
		return false, err
	}
	unmentioned := page.GetBool(pageAnchorUnmentionedField)
	switch {
	case !holding[page.Id] && unmentioned && page.GetBool(pageAnchorLostField) && live[anchor]:
		// Gone with its block while unmentioned there, it hangs there again
		// unmentioned with the block back (mention_anchored_unmentioned.go).
		return true, setPageAnchor(app, page.Id, anchor, false)
	case !holding[page.Id]:
		return false, setPageAnchor(app, page.Id, "", false)
	case unmentioned:
		if err := setPageAnchorUnmentioned(app, page.Id, false); err != nil {
			return false, err
		}
	}
	if !page.GetBool(pageAnchorLostField) {
		if line, hanging, err := restoredPageAnchorKind(app, parentID, anchor); err != nil || !hanging {
			if err != nil {
				return false, err
			}
			if err := setPageAnchor(app, page.Id, "", false); err != nil || !line {
				return false, err
			}
			return true, putRestoredPageInLine(app, ownerID, workspaceID, actorID, parentID, page.Id, anchor)
		}
	}
	if err := setPageAnchor(app, page.Id, anchor, false); err != nil || strings.HasPrefix(anchor, pageAnchorCellPrefix) {
		return true, err
	}
	return true, alignMentionAnchoredPages(app, parentID, []string{page.Id})
}

// moveMentionAnchoredPages puts the pages hanging on a line under the page
// the line moved to, still hanging on it.
func moveMentionAnchoredPages(app core.App, ownerID, workspaceID, actorID, blockID, fromPageID, toPageID string) error {
	pages, err := pagesAnchoredOn(app, fromPageID, []string{blockID})
	if err != nil {
		return err
	}
	for _, pageID := range pages {
		if err := reparentChildPage(app, ownerID, workspaceID, actorID, pageID, toPageID); err != nil {
			return err
		}
	}
	return nil
}

// markAnchoredPages marks the exported sub-pages hanging on a line or a
// table row: its mention there links to each, and the body lists none of
// them again (exportLinkedAnchors).
func (document *workspaceExportDocument) markAnchoredPages(app core.App) error {
	if len(document.Pages) == 0 {
		return nil
	}
	ids := make([]string, 0, len(document.Pages))
	for id := range document.Pages {
		ids = append(ids, id)
	}
	anchored, err := exportLinkedAnchors(app, ids)
	if err != nil {
		return err
	}
	for _, id := range anchored {
		document.Pages[id].Anchored = true
	}
	return nil
}
