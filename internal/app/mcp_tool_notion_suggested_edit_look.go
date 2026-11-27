package app

import (
	"fmt"
	"strings"
)

// A block new_text suggests takes its kind's own look, as a block suggesting
// mode makes does: Leafen's suggestions are its screen's
// (page_suggestions.go), whose block menu while suggesting has no colors or
// icon, as Notion's (seen 2026-11-18, blockActionMenuItems), and a block
// suggested is made with no content of its own — a callout's icon 💡, as
// fetch writes it. So a block's look written in its line — a color
// ("<p color=\"red\">", "<callout color=\"red_bg\">"), a callout's icon
// other than 💡, a numbered list's letters or numerals — is refused, not
// left out unsaid: the page's editor gives it once the block is accepted.
// What only restates that look reads as written: no color or "default", a
// callout with no icon or 💡. Notion's own suggested edits run on Business
// only, so their answer to one wasn't seen; this follows fetch's write form
// and suggesting mode.

// mcpSuggestedLook is the look a block read from new_text holds that a
// suggested block doesn't carry, as fetch writes it, "" for none.
func mcpSuggestedLook(item workspaceImportStagedItem, content map[string]any) string {
	if color, _ := content["color"].(string); color != "" && color != "default" {
		return fmt.Sprintf(`the color "%s"`, strings.Replace(color, "_background", "_bg", 1))
	}
	switch item.Type {
	case graphBlockTypeCallout:
		if name, _ := content["customEmojiName"].(string); name != "" {
			return fmt.Sprintf(`the icon ":%s:"`, name)
		}
		if _, uploaded := content[notionMarkdownIconFileKey]; uploaded {
			return "an uploaded picture as its icon"
		}
		icon, _ := content["icon"].(string)
		if icon = strings.TrimSpace(icon); content["iconType"] != calloutIconNone && icon != "" && icon != calloutDefaultIcon {
			return fmt.Sprintf(`the icon "%s"`, icon)
		}
	case graphBlockTypeNumberedListItem:
		if format, _ := content["listFormat"].(string); format != "" && format != numberedListFormatNumbers {
			return fmt.Sprintf(`the list format "%s"`, format)
		}
	}
	return ""
}

// mcpSuggestedLookRefusal refuses a block's look a suggested block doesn't
// carry.
func mcpSuggestedLookRefusal(kind, look string) error {
	return mcpInvalid("new_text gives a %s %s, which a suggested block doesn't carry: a block suggested takes its kind's own look, as suggesting mode makes one (a callout's icon 💡) — write it without, and give it its look once it is accepted",
		mcpBlockKindName(kind), look)
}
