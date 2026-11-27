package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Which blocks Notion's API puts blocks under (seen twice 2026-11-21,
// Notion-Version 2026-03-11, synthetic pages under 한지테스트페이지): an
// append under a plain heading (heading_1 to heading_4 without
// is_toggleable), a code, divider, equation, bookmark, embed, image, video,
// audio, file, PDF, table of contents, breadcrumb or link-to-page block, or a
// database, inline or full page, is refused with 400 validation_error — an
// empty one too — once its body and position are checked and before the
// blocks it adds are; every other block takes them, a column list and a
// table only their items (notionBlockAppendPlan). A toggle heading holding
// blocks stays a toggle until they are gone (ones in the trash don't count).
// A request's own new blocks keep its shape's rule instead: a plain heading
// takes children there, as page Markdown gives it some, the others none.
// The words are Leafen's.

// notionLeafBlockNames are the blocks that take no blocks under them, by
// their names in Notion's API.
var notionLeafBlockNames = map[string]string{
	graphBlockTypeCode: "code", graphBlockTypeDivider: "divider", graphBlockTypeEquation: "equation",
	graphBlockTypeBookmark: "bookmark", graphBlockTypeEmbed: "embed", graphBlockTypeImage: "image",
	graphBlockTypeVideo: "video", graphBlockTypeAudio: "audio", graphBlockTypeFile: "file",
	graphBlockTypePDF: "pdf", graphBlockTypeTableOfContents: "table_of_contents",
	graphBlockTypeBreadcrumb: "breadcrumb", graphBlockTypeLinkToPage: "link_to_page",
}

const (
	notionDatabaseTakesNoBlocks = "A database takes no blocks under it: add pages to it as rows instead."
	notionPlainHeadingNoBlocks  = "A heading takes blocks under it only as a toggle heading: set is_toggleable to true first."
)

// notionBlockAppendTargetRefusal is why an append can't put blocks under
// its target, or "".
func notionBlockAppendTargetRefusal(target notionBlockAppendTarget) string {
	if target.ParentID == "" {
		// A full-page database being imported takes its body, as its graph does (keepFullPageDatabaseBody).
		if isFullPageDatabase(target.Page.Page) && !target.Page.Page.GetBool("notion_import_staging") {
			return notionDatabaseTakesNoBlocks
		}
		return ""
	}
	switch target.ParentType {
	case graphBlockTypeHeading1, graphBlockTypeHeading2, graphBlockTypeHeading3, graphBlockTypeHeading4:
		return notionPlainHeadingNoBlocks
	case graphBlockTypeInlineDatabase, graphBlockTypeChildDatabase:
		return notionDatabaseTakesNoBlocks
	}
	if name, leaf := notionLeafBlockNames[target.ParentType]; leaf {
		return "This " + name + " block takes no blocks under it."
	}
	return ""
}

// notionBlockUpdateRefusal is why a block's change is refused for what the
// block holds, or "": a table row's cells (notionTableRowUpdateRefusal), and
// a toggle heading's blocks, which keep it a toggle.
func notionBlockUpdateRefusal(app core.App, block storedNotionBlock, externalType string, raw json.RawMessage) (string, error) {
	if externalType == graphBlockTypeTableRow {
		return notionTableRowUpdateRefusal(app, block, raw)
	}
	change := struct {
		Toggleable *bool `json:"is_toggleable"`
	}{}
	if !strings.HasPrefix(block.Type, "toggle_heading_") || json.Unmarshal(raw, &change) != nil ||
		change.Toggleable == nil || *change.Toggleable {
		return "", nil
	}
	counts, err := loadNotionBlockChildCounts(app, []string{block.ID})
	if err != nil || counts[block.ID] == 0 {
		return "", err
	}
	publicID, err := notionBlockPublicID(app, block.WorkspaceID, block.ID)
	if err != nil {
		return "", err
	}
	return "Block " + publicID + " holds blocks: move or delete them before it stops being a toggle heading.", nil
}
