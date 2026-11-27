package app

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
)

// Beside Notion's tags, a page's Markdown writes the blocks people make on the
// screen that Notion's writes as unknown (seen 2026-11-04: breadcrumb, alias):
// <breadcrumb />, and in every form <link-to-page url="…">Title</link-to-page>
// (notion_page_markdown_link_to_page.go) and <button url="…">Label</button>
// (notion_page_markdown_button.go), so an agent makes what a person does. A
// synced block is Notion's: <synced-block> around its blocks, and
// <synced-block-reference url="…#block-…" /> showing one from elsewhere,
// which Leafen's reading puts as these lines first
// (notion_page_markdown_containers.go).

var notionMarkdownOwnBlockPattern = regexp.MustCompile(`^<(breadcrumb|synced-block-reference|synced-block)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*/>$`)

// notionMarkdownOwnBlockLine reads a line holding one of these blocks.
func notionMarkdownOwnBlockLine(line string) (string, json.RawMessage, bool) {
	trimmed := strings.TrimSpace(line)
	if kind, content, button := notionMarkdownButtonLine(trimmed); button {
		return kind, content, true
	}
	if kind, content, linked := notionMarkdownLinkToPageLine(trimmed); linked {
		return kind, content, true
	}
	match := notionMarkdownOwnBlockPattern.FindStringSubmatch(trimmed)
	if match == nil {
		return "", nil, false
	}
	address := html.UnescapeString(notionMarkdownAttributes(match[2])["url"])
	switch match[1] {
	case "breadcrumb":
		return graphBlockTypeBreadcrumb, json.RawMessage(`{}`), true
	case "synced-block":
		return graphBlockTypeSyncedBlock, json.RawMessage(`{}`), true
	}
	// A reference keeps its address until the write resolves it
	// (notionMarkdownSyncedReferences); Leafen's own form names its original's
	// ID after #block-.
	if strings.TrimSpace(address) == "" {
		return "", nil, false
	}
	content := map[string]string{notionMarkdownSyncedAddressKey: address}
	if _, blockID, found := strings.Cut(address, mcpDatabaseLinkFragment); found && blockID != "" && !strings.ContainsAny(blockID, "/?#&") {
		content["syncedBlockId"] = blockID
	}
	encoded, err := json.Marshal(content)
	return graphBlockTypeSyncedBlock, encoded, err == nil
}

// ownBlock writes a block of these kinds, or says it isn't one Markdown holds.
func (w *notionMarkdownWriter) ownBlock(block workspaceExportMarkdownBlock, prefix string) ([]string, bool) {
	address := notionMarkdownAttributeEscaped.Replace
	switch block.Type {
	case graphBlockTypeBreadcrumb:
		w.rendered = append(w.rendered, block.ID)
		return []string{prefix + "<breadcrumb />"}, true
	case graphBlockTypeButton:
		w.rendered = append(w.rendered, block.ID)
		return []string{prefix + w.button(block)}, true
	case graphBlockTypeLinkToPage:
		line, linked := w.linkToPage(block)
		if !linked {
			return nil, false
		}
		w.rendered = append(w.rendered, block.ID)
		return []string{prefix + line}, true
	case graphBlockTypeSyncedBlock:
		w.rendered = append(w.rendered, block.ID)
		// A reference reads with what its original holds, as Notion's does;
		// written back as read that changes nothing, and with other blocks it
		// writes them into its original (notion_page_markdown_synced_content.go).
		// One showing a page's body reads the page's top level (notion_page_markdown_synced_page.go).
		if page := syncedCopyPage(block.Content); page != "" {
			return w.syncedPageReference(page, block.ID, prefix, false), true
		}
		if original := strings.TrimSpace(mapString(block.Content, "syncedBlockId")); original != "" {
			opening := prefix + `<synced-block-reference url="` + address(w.syncedAddress(syncedBlockNamed(original, block.ID))) + `"`
			// One syncing from no block reads as Notion's, its tags with
			// nothing between (synced_block_sourceless.go).
			if original == syncedBlockSourceless {
				return []string{opening + "></synced-block-reference>"}, true
			}
			held := w.syncedOriginal(original)
			if len(held) == 0 {
				return []string{opening + " />"}, true
			}
			lines := []string{opening + ">"}
			for _, line := range held {
				lines = append(lines, prefix+line)
			}
			return append(lines, prefix+"</synced-block-reference>"), true
		}
		opening := prefix + `<synced-block url="` + address(w.syncedAddress(block.ID)) + `">`
		lines := []string{opening}
		lines = append(lines, w.flow(w.children[block.ID], prefix)...)
		return append(lines, prefix+"</synced-block>"), true
	}
	return nil, false
}

// syncedAddress is a synced block's address, on the page it stands on; the
// public API's names it on the page read, by its ID without dashes after #,
// as Notion's does (seen 2026-11-10).
func (w *notionMarkdownWriter) syncedAddress(blockID string) string {
	if w.mentions.APIForm || w.mentions.Version2 {
		return w.mentions.Origin + "/p/" + strings.ReplaceAll(w.publicPageID, "-", "") + "#" +
			strings.ReplaceAll(w.blockName(blockID), "-", "")
	}
	pageID := w.publicPageID
	if w.mentions.SyncedPage != nil {
		if holder := w.mentions.SyncedPage(blockID); holder != "" {
			pageID = holder
		}
	}
	return w.mentions.Origin + "/p/" + pageID + mcpDatabaseLinkFragment + w.blockName(blockID)
}

// syncedOriginal writes what an original synced block holds, for a reference
// to it, by a writer of its own: its blocks aren't this page's to account for.
func (w *notionMarkdownWriter) syncedOriginal(blockID string) []string {
	children := w.children
	if _, here := w.children[blockID]; !here {
		if w.mentions.SyncedBlocks == nil {
			return nil
		}
		children = map[string][]workspaceExportMarkdownBlock{}
		held, more := w.mentions.SyncedBlocks(blockID)
		for _, block := range held {
			children[block.ParentID] = append(children[block.ParentID], block)
		}
		// What it holds past the records the Markdown takes is left out, and
		// the Markdown says it is cut, as for the page's own blocks.
		w.truncated = w.truncated || more
	}
	reader := &notionMarkdownWriter{
		children: children, publicPageID: w.publicPageID, publicBlockID: w.publicBlockID, references: w.references,
		mentions: w.mentions, visited: map[string]bool{}, blank: map[string]bool{},
		underItem: map[string]bool{},
	}
	defer func() { w.truncated = w.truncated || reader.truncated }()
	if w.mentions.APIForm {
		return reader.apiFlow(children[blockID], 0)
	}
	return reader.flow(children[blockID], "")
}

func (w *notionMarkdownWriter) blockName(blockID string) string {
	if name := w.publicBlockID(blockID); name != "" {
		return name
	}
	return blockID
}
