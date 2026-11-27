package app

import (
	"context"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// An agent reads a page at its write version (page_write_version.go) and
// writes against it: leafen_fetch and a write's answer give it as cursor,
// leafen_update_page takes it back as base_cursor, and an anchored insert
// holds to the one it read. Its body is read as the agent's write reads it,
// with the reader mcpWriteReader makes, so the fetch, the write and the
// answer read the same Markdown when nothing changed, and read it once.

// mcpWriteReader is the reader an agent's page write reads the page with:
// the connection's, the page's discussions named in it.
func mcpWriteReader(app core.App, authority mcpAuthority, pageID string) (notionMarkdownReader, error) {
	reader := mcpMarkdownReader(authority)
	var err error
	reader.Discussions, reader.Threads, err = mcpWriteDiscussions(app, authority, pageID)
	return reader, err
}

// mcpPageVersion is a page's write version, its body read as the agent's
// write reads it; page is its record as read before.
func mcpPageVersion(ctx context.Context, app core.App, authority mcpAuthority, page *core.Record) (int, error) {
	reader, err := mcpWriteReader(app, authority, page.Id)
	if err != nil {
		return 0, err
	}
	read := notionPageMarkdownProjection{}
	if err := runNotionReadSnapshot(ctx, app, func(txApp core.App) error {
		read, err = projectNotionPageMarkdown(txApp, page.GetString("owner"), page.Id, page.Id, reader)
		return err
	}); err != nil {
		return 0, err
	}
	return pageWriteVersionOf(app, page, read)
}

// mcpFetchedPageBody is a page's body as an agent's read gives it, and the
// version it gives with it. The words its discussions were left on are
// marked in <span discussion-urls="…"> whether or not they are asked for, by
// the names its write reads back (mcpWriteDiscussions), as notion-fetch and
// Notion's own reads mark them (seen 2026-11-22 and 2026-11-27: the public
// API's page Markdown marks them unasked, and replace_content leaving a span
// out takes the mark away, the discussion staying on the page unplaced): so
// what it gives, written back, keeps them, and is the Markdown the version
// digests, read once. A read naming them its own way (notion-fetch with
// include_discussions) gives no version. Before (2026-11-27) leafen_fetch
// and a trash's answer read without the marks, and their content written
// back took every discussion's mark away.
func mcpFetchedPageBody(
	ctx context.Context, app core.App, authority mcpAuthority, page *core.Record, ownerID string,
	names func(threads, blockID string) string,
) (content string, truncated bool, version int, err error) {
	reader := mcpMarkdownReader(authority)
	reader.Discussions = names
	own := names == nil
	if own {
		if reader.Discussions, _, err = mcpWriteDiscussions(app, authority, page.Id); err != nil {
			return "", false, 0, err
		}
	}
	if content, truncated, err = mcpPageBodyMarkdownRead(app, reader, ownerID, page.Id); err != nil || !own {
		return content, truncated, 0, err
	}
	if truncated {
		// Its version counts the body's revision, which a whole read gives.
		version, err = mcpPageVersion(ctx, app, authority, page)
		return content, truncated, version, err
	}
	version, err = pageWriteVersionOf(app, page, notionPageMarkdownProjection{Markdown: content})
	return content, truncated, version, err
}

// mcpHoldPageVersion refuses an agent's write sent against a version the
// page has moved on from as its cursor conflict, which says to read it
// again. It is checked just before a write whose own fence (the page's
// record cursor, a row's revision) then holds it there.
func mcpHoldPageVersion(ctx context.Context, app core.App, authority mcpAuthority, pageID string, base *int) error {
	if base == nil {
		return nil
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return mcpRefusedUnlessStorage(errMCPPageMissing, err)
	}
	version, err := mcpPageVersion(ctx, app, authority, page)
	if errors.Is(err, errNotionPageMarkdownChanged) {
		return errMCPCursorConflict // it changed while it was read
	}
	if err != nil {
		return failedBecause(errMCPToolFailed, err)
	}
	if version != *base {
		return errMCPCursorConflict
	}
	return nil
}

// mcpWrittenPageCursor is the cursor an agent's page write answers with: the
// page's write version with its body as the answer's content read it, in the
// write's transaction — after it, or as read for one that repeats a write
// made — so that a change made after it reads as one.
func mcpWrittenPageCursor(app core.App, pageID string, written pageMarkdownReplaced) (int, error) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return 0, err
	}
	read := written.After
	if written.Receipt.Duplicate {
		read = written.Before
	}
	return pageWriteVersionOf(app, page, read)
}
