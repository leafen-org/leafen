package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// restoredLineHoldsPage lists the lines that take a restored page's block
// inside, as Notion's restore nests it in its line (seen 2026-11-18 on a
// paragraph): the lines that take blocks inside, a heading's and a code
// block's not.
var restoredLineHoldsPage = map[string]bool{
	graphBlockTypeParagraph: true, graphBlockTypeBulletedListItem: true, graphBlockTypeNumberedListItem: true,
	graphBlockTypeToDo: true, graphBlockTypeToggle: true, graphBlockTypeQuote: true, graphBlockTypeCallout: true,
	graphBlockTypeToggleHeading1: true, graphBlockTypeToggleHeading2: true, graphBlockTypeToggleHeading3: true,
	graphBlockTypeToggleHeading4: true,
}

// restoredPageAnchorKind tells where a page sent to the trash by itself
// goes once restored while its anchor still mentions it: inside its line
// (line), at its parent's body end (neither), or hanging on its anchor
// again (hanging) — a table row's cells, a caption or a database page's
// cell, which show no block inside.
func restoredPageAnchorKind(app core.App, parentPageID, anchor string) (line, hanging bool, err error) {
	switch {
	case anchor == pageAnchorTitle:
		return false, false, nil
	case pageAnchorOnField(anchor):
		return false, true, nil
	}
	var blockType string
	err = app.DB().NewQuery(`SELECT [[type]] FROM {{leafen_blocks}} WHERE [[id]] = {:id} AND [[page_id]] = {:page}`).
		Bind(dbx.Params{"id": anchor, "page": parentPageID}).Row(&blockType)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if _, words := graphTextBlockTypes[blockType]; !words || blockType == graphBlockTypeCode {
		return false, true, nil
	}
	return restoredLineHoldsPage[blockType], false, nil
}

// putRestoredPageInLine gives a restored page its block as the last inside
// the line it hung on — inside the callout, after its other lines, for one
// hanging on a callout's words — as a sub-page's block comes back last in
// the block it was in (restoredChildPageSpotIn), and its sidebar place there
// (by its line's words, child_page_line_order.go).
func putRestoredPageInLine(app core.App, ownerID, workspaceID, actorID, parentPageID, pageID, lineID string) error {
	spot, err := restoredChildPageSpotIn(app, parentPageID, lineID)
	if err != nil {
		return err
	}
	if err := putChildPageBlock(app, ownerID, workspaceID, parentPageID, pageID, actorID, false, spot); err != nil {
		return err
	}
	return alignMentionAnchoredPages(app, parentPageID, []string{pageID})
}
