package app

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// A page a person puts in another place in the app — dragged in the
// sidebar into another page or among its sub-pages, moved with the move
// window, sent to the trash from its menu — edits the pages whose bodies its
// block leaves and goes into, as Notion's does (seen 2026-11-27 in Chrome,
// two tabs at two times, synthetic pages in a section sorted by last edit:
// a sub-page dragged into another page and one moved with the move window
// brought both the page it left and the page it went into to the top; a
// sub-page dragged before its sibling brought its parent there; one sent to
// the trash from its menu its parent). Its title changed leaves its parent
// as it was, and so does its return from the trash. The page itself is
// edited by the move (page_metadata_kept_order.go).
//
// Notion's public API edits no parent this way (the same day, at two times:
// a page moved through it left its own last_edited_time, the page it left's
// and the page it went into's as they were — notion_page_move_kept.go — and
// one sent to the trash or back left its parent's), so a change through the
// API, MCP or the server's own work notes no parent; only the app's own page
// change does.

// pageHeldPlace is where a page stood in its parent's body: its parent, its
// trash and its place among its siblings.
type pageHeldPlace struct {
	found    bool
	parentID string
	inTrash  bool
	position float64
}

func pageHeldPlaceOf(app core.App, pageID string) pageHeldPlace {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return pageHeldPlace{}
	}
	return pageHeldPlace{found: true, parentID: page.GetString("parent_id"),
		inTrash: page.GetBool("in_trash"), position: page.GetFloat("position")}
}

// placeEditedParents are the pages a change of a page's place from `before`
// to `after` edits: the page it left and the page it went into; its parent
// as it goes to the trash; its parent as it takes another place among its
// siblings, unless it only keeps an order shown (keepLastEdit). A page in
// the trash before, coming back or not, edits none.
func placeEditedParents(before, after pageHeldPlace, keepLastEdit bool) []string {
	if !before.found || !after.found || before.inTrash {
		return nil
	}
	edited := []string{}
	switch {
	case after.inTrash:
		edited = append(edited, before.parentID)
	case after.parentID != before.parentID:
		edited = append(edited, before.parentID, after.parentID)
	case after.position != before.position && !keepLastEdit:
		edited = append(edited, after.parentID)
	}
	parents := edited[:0]
	for _, parentID := range edited {
		if parentID != "" {
			parents = append(parents, parentID)
		}
	}
	return parents
}

// notePlaceEditedParents notes the pages a person's change of a page's place
// in the app edits (placeEditedParents) as edited now, by them, in the
// change's transaction; `before` is the page's place read before it.
func notePlaceEditedParents(app core.App, actorID, pageID string, before pageHeldPlace, keepLastEdit bool) error {
	for _, parentID := range placeEditedParents(before, pageHeldPlaceOf(app, pageID), keepLastEdit) {
		if err := notePageContentEdited(context.Background(), app, parentID, actorID); err != nil {
			return err
		}
	}
	return nil
}
