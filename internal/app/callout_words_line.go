package app

import "github.com/pocketbase/pocketbase/core"

// A callout holds its words as its first line, as the screen's does
// (notion_block_callout_line.go), and a page made from their "@" hangs on that
// line. Notion keeps a callout's words as the callout's own: such a page has
// the callout as its parent, and once restored from its trash comes back as
// the last block inside the callout, after its other lines (seen 2026-11-18
// under a Notion test page: four pages made in callouts' words, three restored
// through the API and one through the page's banner). Where Leafen names or
// fills the line such a page belongs to as Notion does, the callout stands
// for that first line.

// calloutOfWordsLine is the callout a line of a page holds the words of — its
// first line, as Notion reads a callout (notionCalloutFirstLine) — or "" for
// any other line.
func calloutOfWordsLine(app core.App, pageID, lineID string) (string, error) {
	line, found, err := loadStoredNotionBlock(app, lineID)
	if err != nil || !found || line.PageID != pageID || line.Type != graphBlockTypeParagraph || line.ParentID == "" {
		return "", err
	}
	callout, found, err := loadStoredNotionBlock(app, line.ParentID)
	if err != nil || !found || callout.Type != graphBlockTypeCallout {
		return "", err
	}
	document, _, err := loadYgoDoc(app, pageID)
	if err != nil {
		return "", err
	}
	first, _, found, err := notionCalloutFirstLine(app, callout, document)
	if err != nil || !found || first.ID != lineID {
		return "", err
	}
	return callout.ID, nil
}

// notionHangingPageParentID is the public ID of the block a page hanging on a
// line, a table row or a captioned block gives as its parent: that block, or
// the callout whose words the line holds.
func notionHangingPageParentID(app core.App, page *core.Record, anchor string) (string, error) {
	callout, err := calloutOfWordsLine(app, page.GetString("parent_id"), anchor)
	if err != nil {
		return "", err
	}
	if callout != "" {
		anchor = callout
	}
	return notionBlockPublicID(app, page.GetString("workspace"), anchor)
}
