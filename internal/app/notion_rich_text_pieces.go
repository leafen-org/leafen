package app

import (
	"maps"
	"unicode/utf16"
)

// A text object Notion's API gives holds at most 2,000 characters — UTF-16
// units, as the request check counts them (notionShape.longAtMost) — the
// limit its write sets on a text object's content (developers.notion.com,
// request limits, read 2026-10-13), so a value read is written back as read.
// One read out whole past it was refused written back: a 6,009-character memo
// an agent wrote through MCP as one value came as one text object, and
// PATCH /pages answered "…content.length takes at most 2000" (2026-11-26).
// A longer run is given in pieces of that size, each with the run's marks and
// link, cut between characters: every rich text the API gives — a block's, a
// title's, a cell's, a caption's, a comment's, a description's — goes
// through notionRichTextPieces. How Notion's own reads cut one isn't
// measured.
const notionRichTextPieceUnits = 2000

// notionRichTextPieces is a rich text item as the API gives it: a text
// object past notionRichTextPieceUnits as pieces of it, any other as it is.
func notionRichTextPieces(item map[string]any) []map[string]any {
	text, _ := item["text"].(map[string]any)
	content, _ := text["content"].(string)
	if item["type"] != "text" || notionTextUnits(content) <= notionRichTextPieceUnits {
		return []map[string]any{item}
	}
	pieces := []map[string]any{}
	start, units := 0, 0
	cut := func(end int) {
		piece := maps.Clone(item)
		piece["text"] = map[string]any{"content": content[start:end], "link": text["link"]}
		piece["plain_text"] = content[start:end]
		pieces = append(pieces, piece)
	}
	for index, character := range content {
		width := max(utf16.RuneLen(character), 1)
		if units+width > notionRichTextPieceUnits {
			cut(index)
			start, units = index, 0
		}
		units += width
	}
	cut(len(content))
	return pieces
}

// notionRichTextItemPieces are rich text items as the API gives them
// (notionRichTextPieces).
func notionRichTextItemPieces(items []map[string]any) []map[string]any {
	pieced := make([]map[string]any, 0, len(items))
	for _, item := range items {
		pieced = append(pieced, notionRichTextPieces(item)...)
	}
	return pieced
}

func notionTextUnits(text string) int {
	units := 0
	for _, character := range text {
		units += max(utf16.RuneLen(character), 1)
	}
	return units
}
