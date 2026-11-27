package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// The sidebar lists a page's sub-pages in the order of their blocks in its
// body, blocks inside other blocks included, and a page hanging on a line
// (mention_anchored_pages.go), or with a block inside one, where the line's
// words mention it (child_page_line_order.go): a page placed in the
// sidebar moves its block to follow, and a block or line moved in the body
// moves its page in the sidebar.

type childPageSibling struct {
	ID       string  `db:"id"`
	Position float64 `db:"position"`
}

// loadChildPageSiblings reads a page's live sub-pages in sidebar order,
// leaving one out.
func loadChildPageSiblings(app core.App, ownerID, workspaceID, parentPageID, exceptID string) ([]childPageSibling, error) {
	siblings := []childPageSibling{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[position]] FROM {{leafen_pages}}
		WHERE [[owner]] = {:owner} AND [[workspace]] = {:workspaceId} AND [[parent_id]] = {:parentId}
			AND [[id]] != {:exceptId} AND [[in_trash]] = FALSE AND [[notion_import_staging]] = FALSE
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{
		"owner": ownerID, "workspaceId": workspaceID, "parentId": parentPageID, "exceptId": exceptID,
	}).All(&siblings)
	return siblings, err
}

// sidebarNeighbours are the sub-pages with blocks right before and after a
// page at a sidebar position.
func sidebarNeighbours(
	siblings []childPageSibling, held func(string) bool, pageID string, position float64,
) (string, string) {
	previous, next := "", ""
	for _, sibling := range siblings {
		if !held(sibling.ID) {
			continue
		}
		if sibling.Position < position || (sibling.Position == position && sibling.ID < pageID) {
			previous = sibling.ID
		} else if next == "" {
			next = sibling.ID
		}
	}
	return previous, next
}

// childPageSidebarSpot is where a page placed in the sidebar goes in its
// parent's body: right after the block of the sub-page before it there, or
// right before the block of the one after it, or at the body's end.
func childPageSidebarSpot(
	app core.App, ownerID, workspaceID, parentPageID, pageID string, position float64,
) func(*crdt.Doc, map[string]structuralBlockMetadata) (childPageBlockSpot, error) {
	return func(doc *crdt.Doc, metadata map[string]structuralBlockMetadata) (childPageBlockSpot, error) {
		siblings, err := loadChildPageSiblings(app, ownerID, workspaceID, parentPageID, pageID)
		if err != nil {
			return childPageBlockSpot{}, err
		}
		held := func(id string) bool { return isChildPageBlock(id, metadata[id]) }
		previous, next := sidebarNeighbours(siblings, held, pageID, position)
		if previous != "" {
			return childPageBlockSpot{Container: metadata[previous].ParentID, After: &previous}, nil
		}
		if next != "" {
			container := metadata[next].ParentID
			order, err := structuralOrder(doc, container)
			if err != nil {
				return childPageBlockSpot{}, err
			}
			if index := structuralIndex(order, next); index > 0 {
				after := order[index-1]
				return childPageBlockSpot{Container: container, After: &after}, nil
			}
			return childPageBlockSpot{Container: container}, nil
		}
		return endOfChildPageContainer(doc, "")
	}
}

// childPageBodyOrder lists the sub-pages of a body in reading order: those
// hanging on its page's title first, as the title stands above the body
// (Notion's sidebar, seen 2026-11-13), then their blocks, blocks inside
// other blocks included, and the pages a line lists (lines: those hanging on
// it, and those with blocks inside it that its words mention, in the order
// of its words — childPageLines) right where the line stands; a page is
// listed once, where it first stands.
func childPageBodyOrder(
	doc *crdt.Doc, metadata map[string]structuralBlockMetadata, lines map[string][]string,
) ([]string, error) {
	result := []string{}
	listed := map[string]bool{}
	list := func(pages ...string) {
		for _, page := range pages {
			if !listed[page] {
				listed[page] = true
				result = append(result, page)
			}
		}
	}
	list(lines[pageAnchorTitle]...)
	// However deep blocks nest; each block is walked once.
	walked := map[string]bool{}
	var walk func(parentID string) error
	walk = func(parentID string) error {
		if walked[parentID] {
			return nil
		}
		walked[parentID] = true
		order, err := structuralOrder(doc, parentID)
		if err != nil {
			return err
		}
		for _, id := range order {
			if isChildPageBlock(id, metadata[id]) {
				list(id)
				continue
			}
			list(lines[id]...)
			if err := walk(id); err != nil {
				return err
			}
		}
		return nil
	}
	return result, walk("")
}

// childPageBlockFollowsSidebar reports whether a page at a sidebar position
// has the same sub-pages around it there as around its block in the body.
func childPageBlockFollowsSidebar(
	app core.App, ownerID, workspaceID, parentPageID, pageID string, position float64,
) (bool, error) {
	doc, _, err := loadYgoStructuralDoc(app, parentPageID)
	if err != nil {
		return false, err
	}
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return false, err
	}
	if !isChildPageBlock(pageID, metadata[pageID]) {
		return true, nil
	}
	siblings, err := loadChildPageSiblings(app, ownerID, workspaceID, parentPageID, pageID)
	if err != nil {
		return false, err
	}
	live := map[string]bool{}
	for _, sibling := range siblings {
		live[sibling.ID] = true
	}
	held := func(id string) bool { return live[id] && isChildPageBlock(id, metadata[id]) }
	previous, next := sidebarNeighbours(siblings, held, pageID, position)
	// Pages with blocks around it, as the sidebar's neighbours are.
	order, err := childPageBodyOrder(doc, metadata, nil)
	if err != nil {
		return false, err
	}
	bodyPrevious, bodyNext := "", ""
	seen := false
	for _, id := range order {
		switch {
		case id == pageID:
			seen = true
		case !live[id]:
		case !seen:
			bodyPrevious = id
		case bodyNext == "":
			bodyNext = id
		}
	}
	return previous == bodyPrevious && next == bodyNext, nil
}

// alignChildPageSidebar keeps the sidebar order of a page's sub-pages with
// their blocks' order in its body after the blocks moved: a moved page takes
// a position between its neighbours there, and when those are out of order
// the sub-pages are numbered again in body order.
func alignChildPageSidebar(
	app core.App,
	doc *crdt.Doc,
	metadata map[string]structuralBlockMetadata,
	ownerID, workspaceID, parentPageID string,
	moved []string,
) error {
	if len(moved) == 0 {
		return nil
	}
	siblings, err := loadChildPageSiblings(app, ownerID, workspaceID, parentPageID, "")
	if err != nil {
		return err
	}
	positions := map[string]float64{}
	for _, sibling := range siblings {
		positions[sibling.ID] = sibling.Position
	}
	anchors, err := loadMentionAnchors(app, parentPageID)
	if err != nil {
		return err
	}
	holders, callouts := map[string]string{}, map[string]bool{}
	for id, entry := range metadata {
		if isChildPageBlock(id, entry) && entry.ParentID != "" {
			holders[id] = entry.ParentID
			if metadata[entry.ParentID].Type == graphBlockTypeCallout {
				callouts[entry.ParentID] = true
			}
		}
	}
	lines, err := childPageLines(app, parentPageID, anchors, holders, callouts)
	if err != nil {
		return err
	}
	order, err := childPageBodyOrder(doc, metadata, lines)
	if err != nil {
		return err
	}
	sequence := []string{}
	for _, id := range order {
		if _, live := positions[id]; live {
			sequence = append(sequence, id)
		}
	}
	renumber := false
	for _, id := range moved {
		index := structuralIndex(sequence, id)
		if index < 0 {
			continue
		}
		current := positions[id]
		hasPrevious, hasNext := index > 0, index+1 < len(sequence)
		previous, next := 0.0, 0.0
		if hasPrevious {
			previous = positions[sequence[index-1]]
		}
		if hasNext {
			next = positions[sequence[index+1]]
		}
		if (!hasPrevious || previous < current) && (!hasNext || current < next) {
			continue
		}
		wanted := current
		switch {
		case hasPrevious && hasNext && previous < next:
			wanted = previous + (next-previous)/2
		case hasPrevious && !hasNext:
			wanted = previous + 1
		case !hasPrevious && hasNext:
			wanted = next - 1
		}
		if (hasPrevious && wanted <= previous) || (hasNext && wanted >= next) {
			renumber = true
			break
		}
		positions[id] = wanted
		if err := setChildPageSidebarPosition(app, ownerID, id, wanted); err != nil {
			return err
		}
	}
	// Several blocks moved at once can leave one before a neighbour that
	// moved after it (2026-11-07: an imported page's text stood its five
	// sub-pages at once).
	for index := 1; !renumber && index < len(sequence); index++ {
		renumber = positions[sequence[index-1]] >= positions[sequence[index]]
	}
	if !renumber {
		return nil
	}
	for index, id := range sequence {
		if err := setChildPageSidebarPosition(app, ownerID, id, float64(index+1)); err != nil {
			return err
		}
	}
	return nil
}

// setChildPageSidebarPosition moves a sub-page to a sidebar position, as a
// change the page's readers take up.
func setChildPageSidebarPosition(app core.App, ownerID, pageID string, position float64) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	if page.GetFloat("position") == position {
		return nil
	}
	cursor, err := nextWorkspaceUserStateCursor(app, ownerID)
	if err != nil {
		return err
	}
	page.Set("position", position)
	page.Set("cursor", cursor)
	return app.Save(page)
}

// placePageLastAmongSiblings moves a page that changed parents to the end of
// its new siblings in the sidebar.
func placePageLastAmongSiblings(app core.App, ownerID, workspaceID, pageID, parentID string) error {
	var position float64
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[position]]),0)+1 FROM {{leafen_pages}}
		WHERE [[owner]]={:ownerId} AND [[workspace]]={:workspaceId}
			AND [[parent_id]]={:parentPageId} AND [[id]]!={:pageId}
	`).Bind(dbx.Params{
		"ownerId": ownerID, "workspaceId": workspaceID, "parentPageId": parentID, "pageId": pageID,
	}).Row(&position); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`UPDATE {{leafen_pages}} SET [[position]]={:position} WHERE [[id]]={:pageId}`).
		Bind(dbx.Params{"position": position, "pageId": pageID}).Execute()
	return err
}
