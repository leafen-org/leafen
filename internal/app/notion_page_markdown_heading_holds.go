package app

// A heading's tag holding blocks ("<h2>⏎x⏎⏎y⏎</h2>", "v2") is the heading
// holding them where the heading keeps a block already there — the same
// line, or its line changed, the block in its place
// (notion_page_markdown_place_pairs.go) — and the heading with them after it
// where it is a new block: on a new page, inserted, or inside a block that is
// new. So Notion's page Markdown reads it (seen 2026-11-21 through its API, h1–h4,
// paragraphs, items, quotes, headings, toggles, callouts, code and dividers
// held alike), its block API calling such a heading has_children false while
// listing what it holds, and adding no block under a plain heading itself.

// notionMarkdownPlainHeadings are the headings that are no toggle.
var notionMarkdownPlainHeadings = map[string]bool{
	graphBlockTypeHeading1: true, graphBlockTypeHeading2: true, graphBlockTypeHeading3: true, graphBlockTypeHeading4: true,
}

// notionMarkdownHeadingHolds stands the blocks each new plain heading holds
// after it, in their order, before the block that came after it; a heading
// that keeps a block (matched or edited, by item) keeps them.
func notionMarkdownHeadingHolds(items []workspaceImportStagedItem, matched, edited []int) {
	for index := range items {
		heading := items[index]
		if !notionMarkdownPlainHeadings[heading.Type] || matched[index] >= 0 || edited[index] >= 0 {
			continue
		}
		last := ""
		for child := index + 1; child < len(items); child++ {
			if items[child].ParentID != heading.ID {
				continue
			}
			items[child].ParentID = heading.ParentID
			if items[child].AfterID == "" {
				items[child].AfterID = heading.ID
			}
			last = items[child].ID
		}
		if last == "" {
			continue
		}
		for next := index + 1; next < len(items); next++ {
			if items[next].ParentID == heading.ParentID && items[next].AfterID == heading.ID && items[next].ID != items[index+1].ID {
				items[next].AfterID = last
			}
		}
	}
}
