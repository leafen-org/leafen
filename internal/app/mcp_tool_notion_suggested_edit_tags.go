package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// new_text's first and last lines may be written as a block's tag, on one
// line or over lines as fetch writes one (mcpSuggestedTagLines):
// "<p color=\"red\">⏎words⏎</p>" is a red paragraph's line, "<p>⏎ words⏎</p>"
// a paragraph's whose words start with a space, "<callout>⏎words⏎</callout>"
// a callout's. Such a line is read as the page's Markdown reads the block.
// Its last line is a block of its own — the block's words after the edit go
// on it, so it takes a suggested block's look (mcpSuggestedBlockRead). Its
// first line goes on where old_text is: where old_text starts the block's
// line it is the block's own line, so its tag is the block's own, look and
// all, as fetch writes it; in the middle of the line a block's tag going on
// over lines can't start, and a tag opened after a line's start can't go on
// over lines.

// mcpSuggestedOwnTag is the words of new_text's first line written as the
// block's own tag where old_text starts its line — a callout's for the
// paragraph a callout's words stand as, which fetch writes inside the
// callout's tag (calloutOfWordsLine). Another kind's tag would turn the
// block into that kind, and a look or a box other than the block's own
// would change it, which a suggestion of its words doesn't do: both are
// refused.
func mcpSuggestedOwnTag(app core.App, pageID string, place mcpSuggestionPlace, line string, site *notionMarkdownSitePages) ([]graphTextSpan, error) {
	item, err := mcpSuggestedTagBlock(line, "first", site, mcpSuggestedBlocksAsWritten)
	if err != nil {
		return nil, err
	}
	block, found, err := loadStoredNotionBlock(app, place.BlockID)
	if err != nil || !found {
		return nil, failedBecause(errMCPToolFailed, err)
	}
	own := map[string]any{}
	_ = json.Unmarshal([]byte(block.Content), &own)
	ownType := place.Type
	if item.Type == graphBlockTypeCallout && place.Type == graphBlockTypeParagraph &&
		mcpSuggestedLook(workspaceImportStagedItem{Type: place.Type}, own) == "" {
		calloutID, err := calloutOfWordsLine(app, pageID, place.BlockID)
		if err != nil {
			return nil, failedBecause(errMCPToolFailed, err)
		}
		if calloutID != "" {
			callout, found, err := loadStoredNotionBlock(app, calloutID)
			if err != nil || !found {
				return nil, failedBecause(errMCPToolFailed, err)
			}
			own, ownType = map[string]any{}, graphBlockTypeCallout
			_ = json.Unmarshal([]byte(callout.Content), &own)
		}
	}
	if item.Type != ownType {
		return nil, mcpSuggestedKindChange("tag", item.Type, place.Type)
	}
	content := map[string]any{}
	_ = json.Unmarshal(item.Content, &content)
	if (content["checked"] == true) != (own["checked"] == true) {
		return nil, mcpInvalid("new_text checks or unchecks the to-do, which a suggested edit of its words doesn't: keep its box as old_text has it")
	}
	look, ownLook := mcpSuggestedLook(item, content), mcpSuggestedLook(workspaceImportStagedItem{Type: ownType}, own)
	if look != ownLook {
		return nil, mcpInvalid("new_text's first line writes the %s's tag with %s where the block has %s: a suggested edit changes a block's words, not its look — write its tag as fetch writes the block, or the words alone",
			mcpBlockKindName(ownType), mcpSuggestedLookName(look), mcpSuggestedLookName(ownLook))
	}
	return mcpSuggestedItemSpans(item), nil
}

// mcpSuggestedTagLastLine is the kind and words of new_text's last line
// written as a block's tag, the block the words after the edit go on.
func mcpSuggestedTagLastLine(line string, site *notionMarkdownSitePages) (string, []graphTextSpan, error) {
	item, err := mcpSuggestedTagBlock(line, "last", site, mcpSuggestedBlockRead)
	if err != nil {
		return "", nil, err
	}
	return item.Type, mcpSuggestedItemSpans(item), nil
}

// mcpSuggestedTagBlock is the one block a line written as a block's tag
// makes, read by read: an empty paragraph for none; refused for a tag opened
// after the line's start going on over lines, or for more than one block.
func mcpSuggestedTagBlock(
	line, which string, site *notionMarkdownSitePages,
	read func(string, *notionMarkdownSitePages) ([]workspaceImportStagedItem, error),
) (workspaceImportStagedItem, error) {
	if mcpSuggestedTagOpening(line) == "" {
		return workspaceImportStagedItem{}, mcpSuggestedTagWithinRefusal(line)
	}
	items, err := read(line, site)
	switch {
	case err != nil:
		return workspaceImportStagedItem{}, err
	case len(items) > 1:
		return workspaceImportStagedItem{}, mcpInvalid("new_text's %s line, a <%s>, holds %d blocks where one goes: write each block on a line of its own",
			which, mcpSuggestedTagOpening(line), len(items))
	case len(items) == 0:
		return workspaceImportStagedItem{Type: graphBlockTypeParagraph}, nil
	}
	return items[0], nil
}

// mcpSuggestedTagOverLines refuses new_text's first line written as a
// block's tag going on over lines where old_text is in the middle of its
// block's line, or with a tag opened after the line's start.
func mcpSuggestedTagOverLines(line string) error {
	if name := mcpSuggestedTagOpening(line); name != "" {
		return mcpInvalid("new_text starts with a <%s> going on over lines, where old_text is in the middle of its block's line and no block's tag starts: its first line goes on there as words — start new_text with a line break to suggest a block after it, or write the words without the tag", name)
	}
	return mcpSuggestedTagWithinRefusal(line)
}

func mcpSuggestedTagWithinRefusal(line string) error {
	return mcpInvalid("new_text opens a <%s> after a line's start and goes on over lines: a block's tag starts its own line, as fetch writes one", mcpSuggestedTagWithin(line))
}

// mcpSuggestedKindChange refuses new_text's first line starting with
// another kind's marker or tag where old_text starts the block's line.
func mcpSuggestedKindChange(written, kind, own string) error {
	advice := "start new_text as old_text starts, or with no marker"
	if written == "tag" {
		advice = "write the block's own tag, as fetch writes it, or the words alone"
	}
	return mcpInvalid("new_text starts with a %s's %s where old_text starts the block's line, which would turn the %s into a %s: a suggested edit changes a block's words, not its kind — %s",
		mcpBlockKindName(kind), written, mcpBlockKindName(own), mcpBlockKindName(kind), advice)
}

// mcpSuggestedLookName is a block's look as a refusal names it.
func mcpSuggestedLookName(look string) string {
	if look == "" {
		return "its kind's own look"
	}
	return look
}

// mcpSuggestedItemSpans is a block's words as read from Markdown.
func mcpSuggestedItemSpans(item workspaceImportStagedItem) []graphTextSpan {
	if item.Spans == nil && item.Text != "" {
		return []graphTextSpan{{Text: item.Text}}
	}
	return item.Spans
}
