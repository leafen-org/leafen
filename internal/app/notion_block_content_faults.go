package app

import (
	"encoding/json"
	"strings"
)

// What Notion's API refuses a request's new blocks for once its request
// check has passed, in its order (seen 2026-11-10, Notion-Version
// 2026-03-11, block append and a new page's children alike):
//
//  1. level by level, each level in order: an AI block, a table row outside
//     a table, a column outside a column list, a paragraph's icon outside a
//     tab — the first it meets (notionBlockRequestRefusal);
//  2. a custom block anywhere;
//  3. a link's or a synced block's target it can't reach
//     (notion_block_page_reference.go);
//  4. every fault in content it would make, in the request's order, said
//     together after notionContentCreationPrefix (notionContentFault): a template or meeting notes block, an uploaded
//     icon on a callout or a paragraph, a new media block's address.
//
// Leafen reaches a link's target as the blocks are written, and before it
// says the faults in their content (notionRequestTargetRefused).

const (
	notionAIBlockRefused        = "Cannot create ai_block"
	notionTableRowPlaceRefused  = "A table takes only table_row blocks as its children"
	notionColumnPlaceRefused    = "A column block goes only inside a column_list block"
	notionCustomBlockRefused    = "The API makes no custom_block blocks."
	notionBlockFileIconRefused  = "This block type can't take an uploaded file as its icon: give it an emoji, external, custom_emoji or icon one."
	notionContentCreationPrefix = "The content wasn't made; fix these first: \n"
)

// notionContentFault is a fault Notion finds in content it would make. A
// new block's is said with the request's others (notionContentCreationFailed);
// a block changed says it alone.
type notionContentFault string

func (fault notionContentFault) Error() string { return string(fault) }

// notionContentCreationFailed is faults as Notion's API says them together.
func notionContentCreationFailed(faults ...string) string {
	return notionContentCreationPrefix + strings.Join(faults, "\n")
}

// notionUncreatableBlockFault is Notion's fault for a block its API can't
// make, or "" for one it can.
func notionUncreatableBlockFault(blockType string) notionContentFault {
	switch blockType {
	case graphBlockTypeTemplate, "meeting_notes":
		return notionContentFault("The public API makes no " + blockType + " blocks.")
	}
	return ""
}

// notionMediaLinkRequestFault is Notion's fault for a new picture's, video's,
// PDF's, sound's or file's external address it doesn't take
// (notionLinkTaken), or "": one it takes is kept as given
// (notion_block_media_address.go), "example.com/x" and "mailto:…" too.
func notionMediaLinkRequestFault(blockType string, incoming map[string]any, create bool) notionContentFault {
	external, _ := incoming["external"].(map[string]any)
	address, isText := external["url"].(string)
	if !create || !isText || notionLinkTaken(address) {
		return ""
	}
	return notionContentFault("Invalid " + blockType + " url.")
}

// notionBlockRequestRefusal is what Notion refuses new blocks under a parent
// of parentType for before it looks at their content: an AI block, a table
// row, a column or a paragraph's icon out of place, level by level, then a
// custom block.
func notionBlockRequestRefusal(children []json.RawMessage, parentType string) string {
	type placed struct {
		raw        json.RawMessage
		parentType string
	}
	level := make([]placed, 0, len(children))
	for _, child := range children {
		level = append(level, placed{child, parentType})
	}
	custom := false
	for len(level) > 0 {
		next := []placed{}
		for _, block := range level {
			blockType, payload, err := notionBlockRequestValue(block.raw)
			if err != nil {
				continue
			}
			switch {
			case blockType == "ai_block":
				return notionAIBlockRefused
			case blockType == graphBlockTypeTableRow && block.parentType != graphBlockTypeTable:
				return notionTableRowPlaceRefused
			case blockType == graphBlockTypeColumn && block.parentType != graphBlockTypeColumnList:
				return notionColumnPlaceRefused
			case blockType == "custom_block":
				custom = true
			}
			content := struct {
				Children []json.RawMessage `json:"children"`
				Icon     json.RawMessage   `json:"icon"`
			}{}
			err = json.Unmarshal(payload, &content)
			if err == nil && blockType == graphBlockTypeParagraph && content.Icon != nil && block.parentType != graphBlockTypeTab {
				return notionParagraphIconRefused
			}
			if err == nil {
				for _, child := range content.Children {
					next = append(next, placed{child, blockType})
				}
			}
		}
		level = next
	}
	if custom {
		return notionCustomBlockRefused
	}
	return ""
}
