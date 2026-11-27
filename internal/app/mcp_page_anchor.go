package app

import (
	"context"
	"errors"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Content an agent places by an anchor (notion_page_markdown_anchor.go) is
// written as the page's whole Markdown with it in place, under the cursor
// the page had when it was read: a page changed in between is read again
// and the anchor found anew, so no one else's change is written over. The
// page is read with the reader its writes use, its discussions' marks too,
// so all else reads back as it was.

// mcpAnchorAttempts is how often a placement reads the page again when it
// changed in between.
const mcpAnchorAttempts = 3

// mcpPageMarkdownBlocks is a page's Markdown as an agent's write reads it,
// where each of its blocks stands in it, and the cursor it was read at.
type mcpPageMarkdownBlocks struct {
	markdown string
	spans    []notionMarkdownBlockSpan
	cursor   int
}

// readMCPPageMarkdownBlocks reads a page's Markdown with where its blocks
// stand, for a connection that may read it.
func readMCPPageMarkdownBlocks(ctx context.Context, app core.App, authority mcpAuthority, pageID string) (mcpPageMarkdownBlocks, error) {
	authorization, err := authorizeMCPPageRead(app, authority.PrincipalID, pageID)
	if err != nil {
		return mcpPageMarkdownBlocks{}, mcpPageReadRefusal(app, authority, pageID, err)
	}
	if !mcpAuthorityReachesPage(app, authority, authorization.Page) {
		return mcpPageMarkdownBlocks{}, errMCPOutsideTokenPages
	}
	reader, err := mcpWriteReader(app, authority, pageID)
	if err != nil {
		return mcpPageMarkdownBlocks{}, err
	}
	marks := &notionMarkdownBlockMarks{}
	var plain, marked notionPageMarkdownProjection
	err = runNotionReadSnapshot(ctx, app, func(txApp core.App) error {
		owner := authorization.Workspace.OwnerID
		if plain, err = projectNotionPageMarkdown(txApp, owner, pageID, pageID, reader); err != nil {
			return err
		}
		marked, err = projectNotionPageMarkdownMarked(txApp, owner, pageID, pageID, reader, "", marks)
		return err
	})
	if err != nil {
		return mcpPageMarkdownBlocks{}, failedBecause(errMCPToolFailed, err)
	}
	if plain.Truncated {
		return mcpPageMarkdownBlocks{}, mcpInvalid("this page holds more blocks than one read gives, so content can't be placed by an anchor in it: use a `start` or `end` position")
	}
	markdown, spans, read := notionMarkdownBlockSpansOf(marked.Markdown, marks)
	if !read || markdown != plain.Markdown {
		return mcpPageMarkdownBlocks{}, failedBecause(errMCPToolFailed, errors.New("the page's blocks couldn't be placed in its Markdown"))
	}
	// The version it was read at, its body as the write reads it: a change
	// in between is another version than the one the write holds to.
	cursor, err := pageWriteVersionOf(app, authorization.Page, plain)
	if err != nil {
		return mcpPageMarkdownBlocks{}, failedBecause(errMCPToolFailed, err)
	}
	return mcpPageMarkdownBlocks{markdown: markdown, spans: spans, cursor: cursor}, nil
}

// writeMCPPageMarkdownPlaced writes a page's Markdown as place makes it from
// the page as read, holding to the cursor it was read at; a page changed in
// between is read again.
func writeMCPPageMarkdownPlaced(
	ctx context.Context, app core.App, pageID string, presentation mcpUpdatePageInput,
	place func(mcpPageMarkdownBlocks) (string, error),
) (mcpPageMutationOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return mcpPageMutationOutput{}, err
	}
	for attempt := 1; ; attempt++ {
		page, err := readMCPPageMarkdownBlocks(ctx, app, authority, pageID)
		if err != nil {
			return mcpPageMutationOutput{}, err
		}
		markdown, err := place(page)
		if err != nil {
			return mcpPageMutationOutput{}, mcpPageMarkdownError(err)
		}
		write := presentation
		write.PageID, write.Command, write.NewString, write.BaseCursor = pageID, "replace_content", markdown, &page.cursor
		_, output, err := updateMCPPage(ctx, app, write)
		if errors.Is(err, errMCPCursorConflict) && attempt < mcpAnchorAttempts {
			continue
		}
		return output, err
	}
}

// insertMCPPageAnchored applies notion-update-page's insert_content placed by
// an anchor.
func insertMCPPageAnchored(ctx context.Context, app core.App, input notionMCPUpdatePageInput) (notionMCPPageOutput, error) {
	position := *input.Position
	if strings.TrimSpace(position.Anchor) == "" {
		return notionMCPPageOutput{}, mcpInvalid("The %q position needs an anchor.", position.Type)
	}
	// It may add a synced block's reference, not change its original.
	if refused := notionMarkdownSyncedRefusal(input.Content, true, true); refused != "" {
		return notionMCPPageOutput{}, mcpInvalid("%s", refused)
	}
	before := position.Type == "before_content"
	output, err := writeMCPPageMarkdownPlaced(ctx, app, mcpLocalID(app, input.PageID),
		mcpUpdatePageInput{Icon: input.Icon, Cover: input.Cover},
		func(page mcpPageMarkdownBlocks) (string, error) {
			target, err := notionMarkdownAnchorTarget(page.markdown, page.spans, before, position.Anchor)
			if err != nil {
				return "", err
			}
			return notionMarkdownPlacedBeside(page.markdown, target, before, input.Content), nil
		})
	if err != nil {
		return notionMCPPageOutput{}, err
	}
	return notionPageOutputFromMutation(output), nil
}
