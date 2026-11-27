package app

import "github.com/pocketbase/pocketbase/core"

// A synced copy showing a page's body (synced_block_page_original.go) is
// exported as a link to that page where it stands, as Notion's export writes
// one (seen 2026-11-27, Markdown & CSV twice: "[title](path)", the page's
// file when the export holds it and its address otherwise; HTML once: the
// page link's figure, named by the page), not the page's blocks.

// syncedPageLinkBlock is the page link a copy of a page's body exports as.
func syncedPageLinkBlock(block *workspaceExportDocBlock, pageID string) *workspaceExportDocBlock {
	return &workspaceExportDocBlock{
		ID: block.ID, PageID: block.PageID, ParentID: block.ParentID, Position: block.Position,
		Type: graphBlockTypeLinkToPage, Content: map[string]any{"childPageId": pageID},
	}
}

// syncedPageLinkMarkdown writes a copy of a page's body as its page link's line.
func (renderer *workspaceExportRenderer) syncedPageLinkMarkdown(block *workspaceExportDocBlock, pageID string) string {
	link := syncedPageLinkBlock(block, pageID)
	if renderer.linkedBefore(pageID) {
		return ""
	}
	title := renderer.linkTitle(link, pageID)
	if href := renderer.pageLinkHref(link, pageID); href != "" {
		return "[" + title + "](" + href + ")"
	}
	return title
}

// copiedSyncedPageOriginals adds to found the copies job made of the pages
// the copies of a page's body among contents show, each the page's copy
// twice over, in place of a sub-page block's copy that names it.
func copiedSyncedPageOriginals(app core.App, job string, contents []map[string]any, found map[string]copiedSyncedBlock) error {
	pages := []string{}
	for _, content := range contents {
		if page := syncedCopyPage(content); page != "" {
			pages = append(pages, page)
		}
	}
	if len(pages) == 0 {
		return nil
	}
	for _, kind := range []string{"page", "row"} {
		copies, err := pageDuplicationReferenceMappings(app, job, kind, pages)
		if err != nil {
			return err
		}
		for source, target := range copies {
			if target != "" {
				found[source] = copiedSyncedBlock{SourceID: source, TargetID: target, TargetPageID: target}
			}
		}
	}
	return nil
}

// notionImportSyncedPageOriginal is the page a Notion move made of the
// Notion page, or database page, a synced copy shows; "" for none.
func notionImportSyncedPageOriginal(resolver *notionImportReferenceResolver, notionID string) (string, error) {
	for _, kind := range []string{"page", "row"} {
		if page, err := resolver.lookup(notionID, kind); err != nil || page != "" {
			return page, err
		}
	}
	return "", nil
}

// keepSyncedCopyOfPage keeps a copy of a page's body, whose fields a restore
// named apart, naming one page twice: its restored page.
func keepSyncedCopyOfPage(content map[string]any, before string) {
	if before != "" {
		if page, _ := content["syncedPageId"].(string); page != "" {
			content["syncedBlockId"] = page
		}
	}
}
