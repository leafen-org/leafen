package app

import "github.com/pocketbase/pocketbase/core"

// A Leafen archive, of a workspace or of a page, carries where each sub-page
// hangs as the page keeps it (mention_anchored_pages.go): the line, table
// row, caption or field it hangs on, whether it is in the trash for that
// anchor's sake, and whether it hangs there unmentioned, which the sidebar
// leaves out and later edits of the line leave so
// (mention_anchored_unmentioned.go). The import hangs it so on the imported
// line or field; on one the archive lacks it hangs nowhere and gets a block
// of its own.

// workspaceExportPageAnchorPatch is what a page's archived value takes of
// where it hangs: nothing for a page hanging nowhere.
func workspaceExportPageAnchorPatch(page string) string {
	return `CASE WHEN ` + page + `.[[anchor_block_id]] = '' THEN '{}' ELSE json_object(
		'anchorBlockId', ` + page + `.[[anchor_block_id]],
		'anchorLost', json(CASE WHEN ` + page + `.[[anchor_lost]] THEN 'true' ELSE 'false' END),
		'anchorUnmentioned', json(CASE WHEN ` + page + `.[[anchor_unmentioned]] THEN 'true' ELSE 'false' END)) END`
}

// workspaceImportPageAnchor is where an imported page hangs: the imported
// line or field, and whether it is in the trash for its sake or hangs there
// unmentioned; empty for one hanging nowhere.
type workspaceImportPageAnchor struct {
	AnchorBlockID     string `json:"anchorBlockId,omitempty"`
	AnchorLost        bool   `json:"anchorLost,omitzero"`
	AnchorUnmentioned bool   `json:"anchorUnmentioned,omitzero"`
}

// workspaceImportArchivePageAnchor is where an archived sub-page hangs once
// imported: on the line's import when the archive holds the line, on the
// imported parent's title or cell (workspaceImportArchiveAnchor), as it hung
// there; else nowhere, as a page's archive's own page hangs nowhere under the
// page it is imported into.
func workspaceImportArchivePageAnchor(db workspaceImportArchiveQueryer, value archivePageValue) workspaceImportPageAnchor {
	if value.ParentID == "" || value.AnchorBlockID == nil || *value.AnchorBlockID == "" {
		return workspaceImportPageAnchor{}
	}
	anchor := workspaceImportPageAnchor{AnchorBlockID: workspaceImportArchiveAnchor(db, *value.AnchorBlockID)}
	if anchor.AnchorBlockID != "" {
		anchor.AnchorLost = value.AnchorLost != nil && *value.AnchorLost
		anchor.AnchorUnmentioned = value.AnchorUnmentioned != nil && *value.AnchorUnmentioned
	}
	return anchor
}

// set hangs an imported page where its archive said.
func (anchor workspaceImportPageAnchor) set(page *core.Record) {
	if anchor.AnchorBlockID == "" {
		return
	}
	page.Set(pageAnchorBlockField, anchor.AnchorBlockID)
	page.Set(pageAnchorLostField, anchor.AnchorLost)
	page.Set(pageAnchorUnmentionedField, anchor.AnchorUnmentioned)
}
