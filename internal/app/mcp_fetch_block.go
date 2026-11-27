package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// notion-fetch reads a block by its own ID, as Notion's does (seen
// 2026-11-21, a column, a column list, a toggle, a paragraph and a heading):
// its kind "block", its title its words or, with none, an untitled one's in
// the reader's language, its address /p/ and its ID as an ancestor-path names
// it (mcp_fetch_ancestor_blocks.go), and a text like a page's — its words as
// its title property and what it holds as the content, or a blank page —
// without a path. Its ancestor-path is empty but for a column list's
// standing in no other block, which names the pages above it. A block's
// place on its page (#block-) still fetches the page, and a page's or a
// database's block fetches as the page or database it is.

const (
	mcpUntitledBlockKorean = "제목 없음"
	mcpUntitledBlock       = "Untitled"
)

// mcpBlockNamed finds the block an ID names: its own, a UUID without its
// dashes or an alias the public API gave it; not a page's, a database's or
// a suggested one.
func mcpBlockNamed(app core.App, rawID string) (storedNotionBlock, bool) {
	id := strings.TrimSpace(rawID)
	if resolved, ok := resolveNotionBlockID(app, id); ok {
		id = resolved
	}
	block, found, err := loadStoredNotionBlock(app, id)
	if err != nil || !found {
		return storedNotionBlock{}, false
	}
	switch block.Type {
	case graphBlockTypeChildPage, graphBlockTypeChildDatabase, graphBlockTypeInlineDatabase:
		return storedNotionBlock{}, false
	}
	content := map[string]any{}
	if json.Unmarshal([]byte(block.Content), &content) == nil {
		if suggestion, _ := content[graphBlockSuggestionKey].(string); suggestion != "" {
			return storedNotionBlock{}, false
		}
	}
	return block, true
}

// fetchMCPBlock answers notion-fetch of a block the person may read on its
// page; one they may not reads as nothing found, as a page does.
func fetchMCPBlock(ctx context.Context, app core.App, block storedNotionBlock, includeDiscussions bool) (*mcp.CallToolResult, notionMCPFetchOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPFetchOutput{}, err
	}
	authorization, err := authorizeMCPPageRead(app, authority.PrincipalID, block.PageID)
	if err != nil {
		return nil, notionMCPFetchOutput{}, mcpRefusedUnlessStorage(errMCPFetchNothing, err)
	}
	if !mcpAuthorityReachesPage(app, authority, authorization.Page) {
		return nil, notionMCPFetchOutput{}, errMCPFetchNothing
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: authorization.Workspace.WorkspaceID,
	}, permissionWorkspaceRead)
	if err != nil || workspace.WorkspaceID != authorization.Workspace.WorkspaceID {
		return nil, notionMCPFetchOutput{}, mcpRefusedUnlessStorage(errMCPFetchNothing, err)
	}
	// Its words and what it holds mark the discussions left on them
	// (mcp_fetch_block_discussions.go).
	own, held, err := mcpBlockFetchDiscussions(app, authority, block)
	if err != nil {
		return nil, notionMCPFetchOutput{}, err
	}
	discussions := ""
	if includeDiscussions {
		if discussions, err = mcpBlockFetchPageDiscussions(app, authority, block); err != nil {
			return nil, notionMCPFetchOutput{}, err
		}
	}
	words, written := "", ""
	if mcpBlockHoldsWords(block.Type) {
		if document, found, err := loadYgoDoc(app, block.PageID); err != nil {
			return nil, notionMCPFetchOutput{}, failedBecause(errMCPToolFailed, err)
		} else if found {
			var spans []graphTextSpan
			spans, words = rowPageBlockTextAsItStands(document, block.ID, false)
			mentions := mcpPropertyMentions(app, authority, workspace.WorkspaceID)
			mentions.Discussions = own
			written = mcpPropertyMarkdown(words, spans, mentions)
		}
	}
	reader := mcpMarkdownReader(authority)
	reader.Discussions = held
	markdown, err := mcpBlockContent(ctx, app, reader, authorization.Workspace.OwnerID, block)
	if err != nil {
		return nil, notionMCPFetchOutput{}, failedBecause(errMCPToolFailed, err)
	}
	ancestors, err := mcpBlockAncestors(app, authority, block)
	if err != nil {
		return nil, notionMCPFetchOutput{}, failedBecause(errMCPToolFailed, err)
	}
	properties, err := mcpFetchJSON(map[string]any{"title": written})
	if err != nil {
		return nil, notionMCPFetchOutput{}, failedBecause(errMCPToolFailed, err)
	}
	content := "<content>\n" + strings.TrimRight(markdown, "\n") + "\n</content>"
	if strings.TrimSpace(markdown) == "" {
		content = mcpFetchBlankPage
	}
	address := mcpPageURL(authority, block.ID)
	asOf := personClockMillis(time.UnixMilli(block.Updated), authority.clock())
	title := words
	if strings.TrimSpace(title) == "" {
		title = mcpUntitledBlock
		if mcpReaderReadsKorean(app, authority.PrincipalID) {
			title = mcpUntitledBlockKorean
		}
	}
	recordMCPReads(app, authority, workspace.WorkspaceID, []contentRead{{Kind: "page", ID: block.PageID, How: contentReadOpen}})
	return nil, notionMCPFetchOutput{
		Metadata: notionMCPFetchMetadata{Type: "block"}, Title: title, URL: address,
		Text: mcpFetchPageHead(address, asOf) + fmt.Sprintf("<page url=\"%s\">\n%s%s\n<properties>\n%s\n</properties>\n%s\n</page>",
			mcpFetchAttribute(address), discussions, mcpAncestorPathText(ancestors), properties, content),
	}, nil
}

// mcpBlockContent is what a block holds as page Markdown, its blocks
// standing at the top; a column list's columns each as a <column> of its
// own, a blank line between them, as Notion's (seen 2026-11-21).
func mcpBlockContent(ctx context.Context, app core.App, reader notionMarkdownReader, ownerID string, block storedNotionBlock) (string, error) {
	read := func(under string) (string, error) {
		projection := notionPageMarkdownProjection{}
		err := runNotionReadSnapshot(ctx, app, func(txApp core.App) error {
			var err error
			projection, err = projectNotionPageMarkdownUnder(txApp, ownerID, block.PageID, block.PageID, reader, under)
			return err
		})
		return strings.TrimRight(projection.Markdown, "\n"), err
	}
	if block.Type != graphBlockTypeColumnList {
		return read(block.ID)
	}
	columns := []storedNotionBlock{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[content]] FROM {{leafen_blocks}}
		WHERE [[page_id]] = {:page} AND [[parent_id]] = {:list} ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"page": block.PageID, "list": block.ID}).All(&columns); err != nil {
		return "", err
	}
	parts := []string{}
	for _, column := range columns {
		if column.Type != graphBlockTypeColumn {
			continue
		}
		inner, err := read(column.ID)
		if err != nil {
			return "", err
		}
		// Its share of the row, as the page's columns write it.
		opening, content := "<column>", map[string]any{}
		if json.Unmarshal([]byte(column.Content), &content) == nil {
			if width, ok := content["width"].(float64); ok && width > 0 && width < 100 {
				opening = `<column ratio="` + strconv.FormatFloat(width, 'f', -1, 64) + `">`
			}
		}
		lines := []string{opening}
		if inner != "" {
			lines = append(lines, inner)
		}
		parts = append(parts, strings.Join(append(lines, "</column>"), "\n"))
	}
	return strings.Join(parts, "\n\n"), nil
}

// mcpBlockAncestors is the ancestor-path a block's fetch gives: for a
// column list the page holding it and what stands above that, while no
// other block stands above it; none for any other block.
func mcpBlockAncestors(app core.App, authority mcpAuthority, block storedNotionBlock) ([]mcpFetchAncestor, error) {
	if block.Type != graphBlockTypeColumnList {
		return nil, nil
	}
	walk := &mcpAncestorWalk{app: app, authority: authority, seen: map[string]bool{}}
	holder, err := walk.page(block.PageID, block.ID)
	if err == nil && holder != nil {
		err = walk.above(holder)
	}
	return mcpAncestorsShownForPage(walk.ancestors), err
}

// notionMarkdownBlocksUnder keeps the blocks one block holds, however deep,
// those right in it standing at the top. A file, image, PDF, video, audio or
// embed block stands for itself instead: read by its own ID it is its own
// content, written as the page's read writes it — its file by the same
// stable address, its caption with it — where what it holds would be
// nothing, and its file would go unsaid (2026-11-26: such a block fetched as
// a blank page). Notion's reference has a block ID fetched "to retrieve that
// subtree directly" (developers.notion.com, MCP tools-search reference, read
// 2026-11-26); the block fetches it was seen giving (2026-11-21) held words
// or other blocks, which stay as they were.
func notionMarkdownBlocksUnder(blocks []workspaceExportMarkdownBlock, under string) []workspaceExportMarkdownBlock {
	parents := make(map[string]string, len(blocks))
	itself := false
	for _, block := range blocks {
		parents[block.ID] = block.ParentID
		if block.ID == under {
			_, itself = graphManagedFileBlockTypes[block.Type]
		}
	}
	held := func(id string) bool {
		for step := 0; id != "" && step <= len(blocks); step++ {
			if id = parents[id]; id == under {
				return true
			}
		}
		return false
	}
	kept := []workspaceExportMarkdownBlock{}
	for _, block := range blocks {
		switch {
		case itself && block.ID == under:
			block.ParentID = ""
		case !held(block.ID):
			continue
		case block.ParentID == under && !itself:
			block.ParentID = ""
		}
		kept = append(kept, block)
	}
	return kept
}
