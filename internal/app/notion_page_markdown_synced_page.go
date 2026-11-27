package app

import (
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A synced block reference showing a page's body (synced_block_page_original.go)
// reads as Notion's API reads one (seen 2026-11-27, "v1" and "v2"): its tag
// names the page after #, on the page read, and holds an <unknown/> line for
// each block at the page's top level, by its address on the page read — "v2"
// with a blank line between them, "v1" each a tab further in. One standing on
// the page it shows holds that page's top level as the page reads it, itself
// left out, as Notion's does (seen 2026-11-27 on a copy pasted into its own
// page); one standing deeper in that page reads as one elsewhere. Read back
// as it reads, it changes nothing (notionMarkdownSyncedOriginals). While the
// page is in the trash, or for a reader who can't open it, the tag holds
// nothing and says why in a notice, as Notion's does in its own words.

// notionMarkdownSyncedPage is what a reference to a page's body reads of the
// page: its name as the reader names pages, its top level's blocks in order,
// and whether it reads as a notice instead.
type notionMarkdownSyncedPage struct {
	Name   string
	Blocks []string
	Notice bool
}

// notionMarkdownSyncedPageNotice is what a reference to a page's body says
// when the page can't be read here.
const notionMarkdownSyncedPageNotice = "This synced block shows a page's body that can't be read here: the page is in the " +
	"trash, or this connection can't open it. Restore the page or share it with this connection to read it here."

// notionMarkdownSyncedPageBody reads a page for a reference to its body;
// internal names it by its own ID, as such readers name pages.
func notionMarkdownSyncedPageBody(app core.App, pageID string, readable func(string) bool, internal bool) notionMarkdownSyncedPage {
	read := notionMarkdownSyncedPage{Name: pageID}
	if !internal {
		if publicID, err := notionPagePublicID(app, pageID); err == nil {
			read.Name = publicID
		}
	}
	if !readable(pageID) || syncedPageOriginalTrashed(app, pageID) {
		read.Notice = true
		return read
	}
	_ = app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_blocks}} INDEXED BY [[idx_leafen_blocks_page_order]]
		WHERE [[owner]] = (SELECT [[owner]] FROM {{leafen_pages}} WHERE [[id]] = {:page})
			AND [[page_id]] = {:page} AND [[parent_id]] = '' AND ` + blockStandsSQL("") + `
		ORDER BY [[position]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"page": pageID, "limit": workspaceExportNotionMarkdownRecordLimit}).Column(&read.Blocks)
	return read
}

// syncedPageReference writes the reference copyID showing a page's body,
// prefix before each of its lines; api writes the public API's "v1" form.
func (w *notionMarkdownWriter) syncedPageReference(pageID, copyID, prefix string, api bool) []string {
	read := notionMarkdownSyncedPage{Name: pageID, Notice: true}
	if w.mentions.SyncedPageBody != nil {
		read = w.mentions.SyncedPageBody(pageID)
	}
	tag, escaped, inside, closing := "synced-block-reference", notionMarkdownAttributeEscaped.Replace, prefix, " />"
	if api {
		tag, escaped, inside, closing = "synced_block_reference", notionMarkdownAPIAttributeEscaped.Replace, prefix+"\t", "/>"
	}
	address := w.mentions.Origin + "/p/" + read.Name + mcpDatabaseLinkFragment + pageID
	if w.mentions.APIForm || w.mentions.Version2 {
		address = w.mentions.Origin + "/p/" + notionMarkdownUndashed(w.publicPageID) + "#" + notionMarkdownUndashed(read.Name)
	}
	opening := prefix + "<" + tag + ` url="` + escaped(address) + `"`
	if read.Notice {
		opening += ` notice="` + escaped(notionMarkdownSyncedPageNotice) + `">`
		if api {
			return []string{opening, prefix + "</" + tag + ">"}
		}
		return []string{opening + "</" + tag + ">"}
	}
	if len(read.Blocks) == 0 {
		return []string{opening + " />"}
	}
	lines := []string{opening + ">"}
	if held, own := w.syncedOwnPage(read.Blocks, copyID, api); own {
		lines = append(lines, notionMarkdownEachIndented(prefix, held)...)
		return append(lines, prefix+"</"+tag+">")
	}
	for index, blockID := range read.Blocks {
		if index > 0 && !api {
			lines = append(lines, "")
		}
		lines = append(lines, inside+`<unknown url="`+escaped(w.syncedAddress(blockID))+`"`+closing)
	}
	return append(lines, prefix+"</"+tag+">")
}

// syncedOwnPage writes the page this writer reads as the reference copyID
// standing on its top level holds it — its top-level blocks but the copy —
// and whether blocks, the page's top level, are this writer's: a writer of
// what it holds names no top level, so a copy it meets of the same page
// reads as one elsewhere.
func (w *notionMarkdownWriter) syncedOwnPage(blocks []string, copyID string, api bool) ([]string, bool) {
	top, own := w.children[""], len(blocks) > 0
	for _, blockID := range blocks {
		own = own && slices.ContainsFunc(top, func(block workspaceExportMarkdownBlock) bool { return block.ID == blockID })
	}
	if !own || !slices.Contains(blocks, copyID) {
		return nil, false
	}
	children := make(map[string][]workspaceExportMarkdownBlock, len(w.children))
	for parent, held := range w.children {
		if parent != "" {
			children[parent] = held
		}
	}
	shown := slices.DeleteFunc(slices.Clone(top), func(block workspaceExportMarkdownBlock) bool { return block.ID == copyID })
	reader := &notionMarkdownWriter{
		children: children, publicPageID: w.publicPageID, publicBlockID: w.publicBlockID, references: w.references,
		mentions: w.mentions, visited: map[string]bool{}, blank: map[string]bool{}, underItem: map[string]bool{},
	}
	defer func() { w.truncated = w.truncated || reader.truncated }()
	if api {
		return notionMarkdownEachIndented("\t", reader.apiFlow(shown, 0)), true
	}
	return reader.flow(shown, ""), true
}

// notionMarkdownUndashed is an ID without its dashes, as Notion's addresses write one.
func notionMarkdownUndashed(id string) string {
	undashed := make([]byte, 0, len(id))
	for index := 0; index < len(id); index++ {
		if id[index] != '-' {
			undashed = append(undashed, id[index])
		}
	}
	return string(undashed)
}

// notionMarkdownEachIndented is lines each after prefix.
func notionMarkdownEachIndented(prefix string, lines []string) []string {
	indented := make([]string, 0, len(lines))
	for _, line := range lines {
		indented = append(indented, prefix+line)
	}
	return indented
}

// notionMarkdownWithoutSyncedHeld is Markdown without the lines its synced
// block references hold, past code: they read the original's blocks, on its
// page, so a write's check of this page's <unknown/> lines leaves them out —
// a reference to a page's body holds one for each block there, and one to an
// original synced block holds the original's. A write changing them is told
// as its original takes it (notionMarkdownSyncedOriginals).
func notionMarkdownWithoutSyncedHeld(markdown string, version2 bool) string {
	lines := strings.Split(markdown, "\n")
	kept := make([]string, 0, len(lines))
	fence := ""
	for index := 0; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		kept = append(kept, lines[index])
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			fence = notionMarkdownFenceOf(trimmed)
			continue
		}
		match := notionMarkdownSyncedReferenceOpen.FindStringSubmatch(trimmed)
		if match == nil || !version2 && match[1] != "synced_block_reference" || strings.Contains(match[3], "</"+match[1]+">") {
			continue
		}
		if end := notionMarkdownSyncedClosing(lines, index, match[1], notionMarkdownSyncedReferenceOpen); end > index {
			kept = append(kept, lines[end])
			index = end
		}
	}
	return strings.Join(kept, "\n")
}
