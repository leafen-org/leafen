package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Matching a page's new Markdown with its current blocks, for the plan that
// writes it (notion_page_markdown_plan.go): level by level, the blocks whose
// own lines stay as they are and the changed ones that keep the blocks in
// their places (notion_page_markdown_place_pairs.go).

// notionMarkdownOwnKinds keep no block they read back as another kind of.
var notionMarkdownOwnKinds = map[string]bool{graphBlockTypeTable: true, graphBlockTypeTableRow: true}

// matchNotionMarkdownBlocks matches the blocks of new Markdown with the
// current blocks: matched are those whose own line the new Markdown holds
// unchanged where they keep their block, or moved (moved), and edited the
// changed blocks that keep the block in their place
// (notion_page_markdown_place_pairs.go) — the file blocks whose caption
// changed and the bookmarks and embeds whose line did among them — each by
// the index of its current block or -1.
func matchNotionMarkdownBlocks(
	old []workspaceImportStagedItem,
	oldIDs []string,
	items []workspaceImportStagedItem,
	tags notionMarkdownTagPlan,
	current *notionPageMarkdownProjection,
) ([]int, []int, []bool) {
	matched, edited, moved := make([]int, len(items)), make([]int, len(items)), make([]bool, len(items))
	for index := range items {
		matched[index], edited[index] = -1, -1
	}
	if len(old) == 0 {
		return matched, edited, moved
	}
	oldKeys := notionMarkdownBlockKeys(old, func(index int) string { return oldIDs[index] }, current.SitePages)
	// A table or a row whose Markdown reads back as other blocks keeps none of
	// them, as its own kind (seen 2026-11-14: a header row without cells,
	// "|  |⏎|  |", written over by itself is a paragraph with a new ID).
	for index := range old {
		if stored := current.BlockTypes[oldIDs[index]]; notionMarkdownOwnKinds[stored] && old[index].Type != stored {
			oldKeys[index] = "\x00other\x00" + oldIDs[index]
		}
	}
	newKeys := notionMarkdownBlockKeys(items, func(index int) string {
		if reference, ok := tags.Kept[items[index].ID]; ok {
			return reference.BlockID
		}
		return "new:" + items[index].ID
	}, current.SitePages)
	// Changed blocks keep the blocks in their place, as the kinds those turn
	// into.
	turnsInto := func(oldIndex, newIndex int) bool {
		return old[oldIndex].Type != notionMarkdownReferenceBlock && items[newIndex].Type != notionMarkdownReferenceBlock &&
			notionMarkdownTurnsInto(current.BlockTypes[oldIDs[oldIndex]], items[newIndex].Type)
	}
	// A file line keeps the block of its kind in its place holding the same
	// file, its caption changed (seen 2026-11-21: another file there, or one
	// in another place, is a new block).
	sameFile := func(oldIndex, newIndex int) bool {
		before, file := notionMarkdownFileKey(old[oldIndex])
		after, _ := notionMarkdownFileKey(items[newIndex])
		return file && before == after && current.BlockTypes[oldIDs[oldIndex]] == items[newIndex].Type
	}
	// A web bookmark's or embed's line keeps the block of its kind in its
	// place, as Notion's keeps the block whatever the line changed (seen
	// 2026-11-11), in "v1" a bookmark's <unknown> line too; not one a tag keeps.
	tagged := map[string]bool{}
	for _, reference := range tags.Kept {
		tagged[reference.BlockID] = true
	}
	oldLink := func(oldIndex int) bool {
		blockID := oldIDs[oldIndex]
		return !tagged[blockID] && notionMarkdownWebLink(current.BlockTypes[blockID], current.LinkContents[blockID])
	}
	sameLink := func(oldIndex, newIndex int) bool {
		line := map[string]any{}
		return oldLink(oldIndex) && current.BlockTypes[oldIDs[oldIndex]] == items[newIndex].Type &&
			json.Unmarshal(items[newIndex].Content, &line) == nil && notionMarkdownWebLink(items[newIndex].Type, line)
	}
	places := newNotionMarkdownPlaces(old, items, oldKeys, newKeys, matched, edited, func(oldIndex, newIndex int) bool {
		return turnsInto(oldIndex, newIndex) || sameFile(oldIndex, newIndex) || sameLink(oldIndex, newIndex)
	})
	places.match(places.oldTree.roots, places.newTree.roots)
	// A block keeping one in its place whose own line stays as it was keeps
	// it as it is.
	for index, oldIndex := range edited {
		if oldIndex >= 0 && oldKeys[oldIndex] == newKeys[index] {
			matched[index], edited[index] = oldIndex, -1
		}
	}
	// A line the new Markdown holds unchanged out of its order is its block
	// moved, when one current line left over reads the same (2026-11-04:
	// lines an agent swapped made one a new block, its comments lost).
	taken := make([]bool, len(old))
	for index := range items {
		if matched[index] >= 0 {
			taken[matched[index]] = true
		}
		if edited[index] >= 0 {
			taken[edited[index]] = true
		}
	}
	left := map[string][]int{}
	for oldIndex, key := range oldKeys {
		if !taken[oldIndex] {
			left[key] = append(left[key], oldIndex)
		}
	}
	wanted := map[string]int{}
	for index, key := range newKeys {
		if matched[index] < 0 && edited[index] < 0 {
			wanted[key]++
		}
	}
	for index, key := range newKeys {
		if matched[index] >= 0 || edited[index] >= 0 || len(left[key]) != 1 || wanted[key] != 1 {
			continue
		}
		matched[index], moved[index] = left[key][0], true
	}
	return matched, edited, moved
}

// notionMarkdownRereadEquations makes each equation the new Markdown holds
// as it was an edited one where its line reads back as another expression
// than the block holds, so that it takes the one its line reads, keeping its
// ID, as Notion's page Markdown does (seen 2026-11-14: an equation "a⏎",
// whose "v2" Markdown is "$$⏎a⏎$$", written back over its page is "a", and
// "⏎a⏎" is "a", "⏎ ⏎" " " and "⏎⏎" nothing; other blocks stay as they
// are, but a table's row, notionMarkdownRereadRows).
func notionMarkdownRereadEquations(
	items []workspaceImportStagedItem, oldIDs []string, matched, edited []int, current *notionPageMarkdownProjection,
) {
	if current == nil {
		return
	}
	for index, item := range items {
		if matched[index] < 0 || item.Type != graphBlockTypeEquation {
			continue
		}
		stored, held := current.Expressions[oldIDs[matched[index]]]
		var read struct {
			Expression string `json:"expression"`
		}
		if !held || json.Unmarshal(item.Content, &read) != nil || read.Expression == stored {
			continue
		}
		matched[index], edited[index] = -1, matched[index]
	}
}

// notionMarkdownRereadRows makes each table's row the new Markdown holds a
// new row where its line reads as other cells than the row it stands for
// holds, kept or changed in place, as Notion's page Markdown does (seen
// 2026-11-14: "| c | d |" written over as "| c | e |" keeps the table and
// its other rows, the row new; a cell "a⏎" and the code "⏎b⏎", whose "v2"
// Markdown is "a<br /><code><br />b<br /></code>", written back over its
// page is "a⏎" and the code "<br />b<br />" in a new row; a cell " " written
// back as "<td>⏎ ⏎</td>" keeps its row). The cells compare by their words and
// their code (notionMarkdownRowCellsKey).
func notionMarkdownRereadRows(
	items []workspaceImportStagedItem, oldIDs []string, matched, edited []int, current *notionPageMarkdownProjection,
) {
	if current == nil {
		return
	}
	for index, item := range items {
		if item.Type != graphBlockTypeTableRow {
			continue
		}
		at := &matched[index]
		if *at < 0 {
			at = &edited[index]
		}
		if *at < 0 {
			continue
		}
		stored, held := current.RowCells[oldIDs[*at]]
		var read graphTableRowBlockContent
		if !held || json.Unmarshal(item.Content, &read) != nil || notionMarkdownRowCellsKey(read.Cells) == stored {
			continue
		}
		*at = -1
	}
}

// notionMarkdownRowCellsKey names a row's cells by their words and which of
// them are code, its empty pieces left out.
func notionMarkdownRowCellsKey(cells [][]graphTextSpan) string {
	key := make([][]graphTextSpan, 0, len(cells))
	for _, cell := range cells {
		pieces := []graphTextSpan{}
		for _, span := range cell {
			if span.Text == "" {
				continue
			}
			if last := len(pieces) - 1; last >= 0 && pieces[last].Code == span.Code {
				pieces[last].Text += span.Text
				continue
			}
			pieces = append(pieces, graphTextSpan{Text: span.Text, Code: span.Code})
		}
		key = append(key, pieces)
	}
	named, _ := json.Marshal(key)
	return string(named)
}

// notionMarkdownBlockKeys names each block as a match sees it: a tag by the
// block it names, and any other block by its depth, type, text, formatting
// (its links to pages by the pages they name, notionMarkdownKeyLink) and
// content.
func notionMarkdownBlockKeys(items []workspaceImportStagedItem, reference func(int) string, site *notionMarkdownSitePages) []string {
	depth := make(map[string]int, len(items))
	keys := make([]string, len(items))
	for index, item := range items {
		if item.ParentID != "" {
			depth[item.ID] = depth[item.ParentID] + 1
		}
		if item.Type == notionMarkdownReferenceBlock {
			keys[index] = fmt.Sprintf("%d\x00ref\x00%s", depth[item.ID], reference(index))
			continue
		}
		keyed := slices.Clone(item.Spans)
		for spanIndex := range keyed {
			keyed[spanIndex].Link = notionMarkdownKeyLink(keyed[spanIndex].Link, site)
		}
		spans, _ := json.Marshal(keyed)
		keys[index] = fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s", depth[item.ID], item.Type, item.Text, spans, item.Content)
	}
	return keys
}

// notionMarkdownKeyLink is a link as a match sees it: one to one of the
// workspace's pages by the page alone — at this server's address or not, to
// a block on it or not, after its title or not — as Notion's page Markdown
// keeps a line whose words and marks are the same and whose links name the
// same pages as it was, its links and their anchors too (seen 2026-11-11,
// "v1" and "v2": "[a](/ID#block)" written over by "[a](/Title-ID)" or
// "[a](/ID)" keeps its #block, "[a](/ID)" by "[a](/ID#block)" keeps none;
// a line with another page in a link takes its new links). Any other link
// is as it is ("https://a.com/x#y" is not "https://a.com/x").
func notionMarkdownKeyLink(link string, site *notionMarkdownSitePages) string {
	address := strings.TrimSpace(link)
	if parsed, err := url.Parse(address); err == nil && parsed.Host != "" && site.names(address) {
		address = parsed.Path
		if parsed.Fragment != "" {
			address += "#" + parsed.Fragment
		}
	}
	// One of Notion's web addresses names the page its ID names, the one a
	// write points it at when that page is here (notion_address_links_here.go).
	if kept, named := notionMarkdownAppAddress(notionMarkdownNotionWebAddress(address)); named {
		address = kept
	}
	page, _, _ := strings.Cut(address, "#")
	if id, named := strings.CutPrefix(page, "/p/"); named && (notionAppAddressLink(address) || notionMarkdownOwnPageRef(id)) {
		return page
	}
	return link
}

// sameGraphTextSpans tells whether two texts are formatted alike.
func sameGraphTextSpans(left, right []graphTextSpan) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}
