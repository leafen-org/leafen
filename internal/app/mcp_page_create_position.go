package app

import (
	"context"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// notion-create-pages puts new pages in a parent page's content where its
// position says, as Notion's MCP does (seen 2026-11-22): at the top with
// start, at the bottom with end or none, and by an anchor beside a block, at
// its level — inside a toggle too — in the order they were given; an anchor
// that doesn't place them is refused before any page is made, in the same
// words as insert_content's (notion_page_markdown_anchor.go). The pages are
// made at the bottom, then their blocks move to the place in one write of the
// parent's Markdown (mcp_page_anchor.go); a place that can't be written any
// more leaves them at the bottom and says so in warnings.

// definitionProperties are the properties the page at index was given.
func definitionProperties(pages []notionMCPCreatePageDefinition, index int) map[string]any {
	if index < 0 || index >= len(pages) {
		return nil
	}
	return pages[index].Properties
}

// createMCPPagesPlaced makes pages as createMCPPages does, a page's content
// read from the file it names in its place (mcp_page_body_upload.go), then
// places them where position says.
func createMCPPagesPlaced(
	ctx context.Context, request *mcp.CallToolRequest, app core.App, input notionMCPCreatePagesInput,
) (*mcp.CallToolResult, notionMCPCreatePagesOutput, error) {
	input.Pages = slices.Clone(input.Pages)
	if err := mcpCreatePagesBodies(ctx, app, input.Pages); err != nil {
		return nil, notionMCPCreatePagesOutput{}, err
	}
	position := input.Position
	if position == nil || position.Type == "end" {
		return createMCPPages(ctx, request, app, input)
	}
	anchored, before := notionMarkdownAnchored(position.Type), position.Type == "before_content"
	parentID := mcpLocalID(app, strings.TrimSpace(input.Parent.PageID))
	switch {
	case position.Type != "start" && !anchored:
		return nil, notionMCPCreatePagesOutput{}, mcpInvalid("position.type is start, end, after_content or before_content")
	case parentID == "":
		return nil, notionMCPCreatePagesOutput{}, mcpInvalid("position places the pages in a parent page's content: give parent.page_id")
	case anchored && strings.TrimSpace(position.Anchor) == "":
		return nil, notionMCPCreatePagesOutput{}, mcpInvalid("The %q position needs an anchor.", position.Type)
	}
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPCreatePagesOutput{}, err
	}
	if anchored {
		page, err := readMCPPageMarkdownBlocks(ctx, app, authority, parentID)
		if err != nil {
			return nil, notionMCPCreatePagesOutput{}, err
		}
		if _, err := notionMarkdownAnchorTarget(page.markdown, page.spans, before, position.Anchor); err != nil {
			return nil, notionMCPCreatePagesOutput{}, mcpPageMarkdownError(err)
		}
	}
	result, output, err := createMCPPages(ctx, request, app, input)
	if err != nil || result != nil && result.IsError || len(output.Pages) == 0 {
		return result, output, err
	}
	made := make([]string, 0, len(output.Pages))
	for _, page := range output.Pages {
		made = append(made, mcpLocalID(app, page.ID))
	}
	_, err = writeMCPPageMarkdownPlaced(ctx, app, parentID, mcpUpdatePageInput{}, func(page mcpPageMarkdownBlocks) (string, error) {
		rest, spans, pages, cut := notionMarkdownBlocksCut(page.markdown, page.spans, made)
		if !cut {
			return "", pageMarkdownMatchError{message: "The new pages' blocks aren't in the parent page's content."}
		}
		if !anchored {
			return strings.TrimRight(pages+"\n\n"+rest, "\n"), nil
		}
		target, err := notionMarkdownAnchorTarget(rest, spans, before, position.Anchor)
		if err != nil {
			return "", err
		}
		return notionMarkdownPlacedBeside(rest, target, before, pages), nil
	})
	if err != nil {
		output.Warnings = append(output.Warnings, mcpMarkdownWarning{
			Code: "position_not_applied", Message: "The pages stay at the bottom of the parent page: " + err.Error(),
		})
	}
	return result, output, nil
}

// notionMarkdownBlocksCut is a page's Markdown without the top blocks named,
// where its other blocks stand in it then, and the named blocks' lines in
// the order named, a blank line apart; false when one isn't a block at the
// page's top.
func notionMarkdownBlocksCut(markdown string, spans []notionMarkdownBlockSpan, ids []string) (string, []notionMarkdownBlockSpan, string, bool) {
	named := make(map[string]int, len(ids))
	for index, id := range ids {
		named[id] = index
	}
	texts := make([]string, len(ids))
	var rest strings.Builder
	kept := make([]notionMarkdownBlockSpan, 0, len(spans))
	at, removed, found := 0, 0, 0
	for _, span := range spans {
		index, cutting := named[span.ID]
		if !cutting {
			if span.start < at {
				// Inside a block cut out.
				return "", nil, "", false
			}
			span.line, span.start, span.end = span.line-removed, span.start-removed, span.end-removed
			kept = append(kept, span)
			continue
		}
		if span.line != span.start || span.start < at {
			return "", nil, "", false
		}
		// The block and the blank lines before it go.
		from := span.line
		for from > at && markdown[from-1] == '\n' {
			from--
		}
		rest.WriteString(markdown[at:from])
		removed += span.end - from
		at, texts[index] = span.end, markdown[span.start:span.end]
		found++
	}
	rest.WriteString(markdown[at:])
	if found != len(ids) {
		return "", nil, "", false
	}
	trimmed := strings.TrimLeft(rest.String(), "\n")
	if lead := rest.Len() - len(trimmed); lead > 0 {
		for index := range kept {
			kept[index].line, kept[index].start, kept[index].end = kept[index].line-lead, kept[index].start-lead, kept[index].end-lead
		}
	}
	return trimmed, kept, strings.Join(texts, "\n\n"), true
}
