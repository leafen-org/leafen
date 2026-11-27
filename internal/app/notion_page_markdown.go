package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/pocketbase/pocketbase/core"
)

const notionPageMarkdownGraphWindow = 100

var errNotionPageMarkdownChanged = errors.New("Notion-compatible page Markdown source changed")

// The public API's page Markdown comes in the two versions Notion's takes
// at both Notion-Versions Leafen takes (seen 2026-11-10): "v1", the default,
// the API's own form (notion_page_markdown_api_form.go), and "v2",
// Notion-flavored Markdown, the form Notion's MCP and Leafen's MCP tools read
// and write. A read names it in its query, a write in its body, and it holds
// for the whole request: what it reads and what it answers.
const notionMarkdownVersion2 = "v2"

type notionPageMarkdownProjection struct {
	Markdown string
	// Lines is the Markdown its blocks write before "v1" trims the space
	// ending it, which Markdown written over the page is matched against, so
	// that a last line ending in a space reads as the same words.
	Lines           string
	Truncated       bool
	UnknownBlockIDs []string
	BlockIDs        []string
	RootBlockIDs    []string
	// SuggestedRootBlockIDs are the top blocks only suggested so far, which
	// the Markdown doesn't read and erasing the page removes too.
	SuggestedRootBlockIDs []string
	GraphRevision         int
	// References are the sub-pages and databases the Markdown names with
	// tags, in body order.
	References []notionMarkdownReference
	// UnknownBlocks are the blocks the Markdown writes as <unknown/> lines,
	// by the ID each line names (notionMarkdownUnknownName), and Buttons its
	// buttons so (notion_page_markdown_button.go).
	UnknownBlocks, Buttons map[string]string
	// RenderedBlockIDs are the blocks the Markdown writes, one for each block
	// it reads back as, in order, save BlankBlockIDs: paragraphs of spaces,
	// whose lines read back as no block, as in Notion. BlockParents and
	// BlockTypes hold every block's parent and type.
	RenderedBlockIDs []string
	BlankBlockIDs    map[string]bool
	BlockParents     map[string]string
	BlockTypes       map[string]string
	// LinkContents are the web bookmarks' and embeds' contents, which a
	// line that changes one keeps but for what it says (notionMarkdownLinkWrite).
	LinkContents map[string]map[string]any
	// Expressions are the equations' expressions, which a line that reads
	// back as another one writes over (notionMarkdownRereadEquations).
	Expressions map[string]string
	// RowCells are the table rows' cells, which a line that reads back as
	// other cells makes a new row of (notionMarkdownRereadRows).
	RowCells map[string]string
	// KeepLineEnds reads its Markdown, and Markdown written over it, as the
	// public API's "v1" does: the whitespace ending a paragraph's, a list
	// item's, a to-do's or a callout's text line is its text's (seen
	// 2026-11-10); a heading's, a quote's and a toggle's go.
	KeepLineEnds bool
	// SitePages reads the addresses of this server's pages standing bare in
	// Markdown written over it as mentions of them, named as its writer
	// names pages (notionMarkdownSitePages); nil reads them as links.
	SitePages *notionMarkdownSitePages
	// Version1 is whether Markdown written over it is the public API's
	// "v1", whose refusals name "v1"'s tags (notionMarkdownElsewhereError).
	Version1 bool
}

func serveNotionPageMarkdownRetrieve(event *core.RequestEvent, publicPageID string) error {
	authorization, authorized, err := authorizeNotionPageOrItemBody(
		event, publicPageID, "pages:read", permissionWorkspaceRead,
	)
	if err != nil || !authorized {
		return err
	}
	// The query's version is checked before (notionRouteShapes).
	reader, err := notionIdentityPageMarkdownReader(
		event, authorization.Identity, event.Request.URL.Query().Get("markdown_version"), authorization.Page.Page.Id, publicPageID,
	)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	projection := notionPageMarkdownProjection{}
	err = runNotionReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		current, err := authorizePage(
			txApp, authorization.Identity.PrincipalID,
			authorization.Page.Page.Id, permissionWorkspaceRead,
		)
		reached := err == nil && current.Workspace.WorkspaceID == authorization.Identity.WorkspaceID &&
			notionOAuthIdentityAllowsPage(txApp, authorization.Identity, current)
		if isPermissionDenied(err) {
			// A database page with its database in the trash reads through
			// the page holding it (notion_trash_reads.go).
			current, reached, err = authorizeNotionRowInTrashedSource(
				txApp, authorization.Identity, authorization.Page.Page.Id, permissionWorkspaceRead,
			)
		}
		if err != nil || !reached {
			if err == nil || isPermissionDenied(err) {
				return permissionDenied()
			}
			return err
		}
		projection, err = projectNotionPageMarkdown(
			txApp, current.Workspace.OwnerID, current.Page.Id, publicPageID, reader,
		)
		return err
	})
	if isPermissionDenied(err) {
		return notionPageNotFound(event)
	}
	if errors.Is(err, errNotionPageMarkdownChanged) {
		return notionCompatibilityError(
			event, http.StatusConflict, "conflict_error", "The page changed while it was being read. Retry the request.",
		)
	}
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	recordAPIReads(event.App, authorization.Identity, []contentRead{contentReadOfPage(authorization.Page.Page)})
	return writeNotionPageMarkdown(event, authorization.Page.Page.Id, publicPageID, projection, nil)
}

func projectNotionPageMarkdown(
	app core.App,
	owner string,
	internalPageID string,
	publicPageID string,
	reader notionMarkdownReader,
) (notionPageMarkdownProjection, error) {
	return projectNotionPageMarkdownUnder(app, owner, internalPageID, publicPageID, reader, "")
}

// projectNotionPageMarkdownUnder reads a page's Markdown, or with under the
// blocks one of its blocks holds alone, as a block's fetch gives them
// (mcp_fetch_block.go).
func projectNotionPageMarkdownUnder(
	app core.App,
	owner, internalPageID, publicPageID string,
	reader notionMarkdownReader,
	under string,
) (notionPageMarkdownProjection, error) {
	return projectNotionPageMarkdownMarked(app, owner, internalPageID, publicPageID, reader, under, nil)
}

// projectNotionPageMarkdownMarked reads it with each block's place marked
// when marks is given (notion_page_markdown_block_marks.go).
func projectNotionPageMarkdownMarked(
	app core.App,
	owner, internalPageID, publicPageID string,
	reader notionMarkdownReader,
	under string,
	marks *notionMarkdownBlockMarks,
) (notionPageMarkdownProjection, error) {
	blocks := make([]workspaceExportMarkdownBlock, 0)
	cursor := ""
	snapshotRevision := -1
	truncated := false
	suggestedRoots := []string{}
	for {
		window, err := loadContentGraphWindow(app, owner, internalPageID, cursor, notionPageMarkdownGraphWindow)
		if err != nil {
			return notionPageMarkdownProjection{}, err
		}
		if snapshotRevision < 0 {
			snapshotRevision = window.SnapshotRevision
		} else if window.SnapshotRevision != snapshotRevision {
			return notionPageMarkdownProjection{}, errNotionPageMarkdownChanged
		}
		reachedNonBlock := false
		// As in Notion's Markdown, a page as it stands: suggested blocks and
		// text aren't in it yet, text suggested away is, and formatting is as
		// it was (seen 2026-11-04).
		standing := graphItemsAsTheyStand(window.Items)
		if len(standing) != len(window.Items) {
			suggestedRoots = append(suggestedRoots, notionMarkdownSuggestedRoots(window.Items)...)
		}
		for _, item := range standing {
			if item.Kind != graphReadKindBlock {
				reachedNonBlock = true
				break
			}
			block := workspaceExportMarkdownBlock{Content: map[string]any{}}
			block.ID, _ = item.Value["id"].(string)
			block.ParentID, _ = item.Value["parentId"].(string)
			block.Position, _ = item.Value["position"].(string)
			block.Type, _ = item.Value["type"].(string)
			if content, ok := item.Value["content"].(map[string]any); ok {
				block.Content = content
			}
			blocks = append(blocks, block)
			if len(blocks) > workspaceExportNotionMarkdownRecordLimit+workspaceExportNotionMarkdownUnknownIDLimit {
				truncated = true
				break
			}
		}
		if truncated || reachedNonBlock || !window.HasMore || window.NextCursor == nil {
			break
		}
		// A window's cursor is as the route gives it; the next window reads it decoded.
		if cursor, err = decodeGraphCursor(*window.NextCursor); err != nil {
			return notionPageMarkdownProjection{}, err
		}
	}
	if under != "" {
		blocks = notionMarkdownBlocksUnder(blocks, under)
	}
	document, found, err := loadYgoDoc(app, internalPageID)
	if err != nil {
		return notionPageMarkdownProjection{}, err
	}
	if found {
		for index := range blocks {
			spans, text := rowPageBlockTextAsItStands(document, blocks[index].ID, false)
			// A text of empty pieces alone has spans and no words.
			if text != "" || len(spans) > 0 {
				blocks[index].Content["text"], blocks[index].Spans = text, spans
			}
		}
	}
	// A block written through the content graph keeps its formatted text
	// with itself until the page's text document holds it.
	for index := range blocks {
		if len(blocks[index].Spans) == 0 {
			blocks[index].Spans = workspaceExportMarkdownRichSpans(blocks[index].Content)
		}
	}
	workspaceID := ""
	if page, err := app.FindRecordById(pagesCollection, internalPageID); err == nil {
		workspaceID = page.GetString("workspace")
	}
	// A sub-page's block is its page, and has its page's ID, as the block
	// reads show it.
	subPages := map[string]bool{}
	for _, block := range blocks {
		if block.Type == graphBlockTypeChildPage {
			subPages[block.ID] = true
		}
	}
	publicBlockID := func(blockID string) string {
		if subPages[blockID] {
			publicID, err := notionPagePublicID(app, blockID)
			if err != nil {
				return ""
			}
			return publicID
		}
		if validNotionBlockID(blockID) {
			return blockID
		}
		if workspaceID == "" {
			return ""
		}
		publicID, err := notionBlockPublicID(app, workspaceID, blockID)
		if err != nil {
			return ""
		}
		return publicID
	}
	references, err := loadNotionMarkdownReferences(app, owner, workspaceID, internalPageID, blocks, reader)
	if err != nil {
		return notionPageMarkdownProjection{}, err
	}
	var mentionErr error
	mentions := reader.mentions(app, workspaceID, &mentionErr)
	mentions.PageID = internalPageID
	projection := renderNotionPageMarkdownMarked(blocks, publicPageID, truncated, publicBlockID, references, mentions, reader.Sources, marks)
	if mentionErr != nil {
		return notionPageMarkdownProjection{}, mentionErr
	}
	projection.GraphRevision = max(snapshotRevision, 0)
	projection.BlockParents = make(map[string]string, len(blocks))
	projection.BlockTypes = make(map[string]string, len(blocks))
	projection.LinkContents, projection.Expressions, projection.RowCells = map[string]map[string]any{}, map[string]string{}, map[string]string{}
	for _, block := range blocks {
		projection.BlockParents[block.ID], projection.BlockTypes[block.ID] = block.ParentID, block.Type
		if notionMarkdownLinkKinds[block.Type] {
			projection.LinkContents[block.ID] = block.Content
		}
		if expression, ok := block.Content["expression"].(string); ok && block.Type == graphBlockTypeEquation {
			projection.Expressions[block.ID] = expression
		}
		if block.Type == graphBlockTypeTableRow {
			var row graphTableRowBlockContent
			if stored, err := json.Marshal(block.Content); err == nil && json.Unmarshal(stored, &row) == nil {
				projection.RowCells[block.ID] = notionMarkdownRowCellsKey(row.Cells)
			}
		}
		projection.BlockIDs = append(projection.BlockIDs, block.ID)
		if block.ParentID == "" {
			projection.RootBlockIDs = append(projection.RootBlockIDs, block.ID)
		}
	}
	notionMarkdownCalloutTextPlace(&projection)
	projection.SuggestedRootBlockIDs = suggestedRoots
	return projection, nil
}

// writeNotionPageMarkdown answers with a page's Markdown, and the warnings of
// the write that made it, when it has any, naming the page by the ID it
// answers with (notionPageAnswerID).
func writeNotionPageMarkdown(
	event *core.RequestEvent,
	pageID, publicPageID string,
	projection notionPageMarkdownProjection,
	warnings []notionMarkdownWarning,
) error {
	event.Response.Header().Set("Cache-Control", "no-store")
	answer := map[string]any{
		"object":            "page_markdown",
		"id":                notionPageAnswerID(event.App, pageID, publicPageID),
		"markdown":          projection.Markdown,
		"truncated":         projection.Truncated,
		"unknown_block_ids": projection.UnknownBlockIDs,
	}
	if len(warnings) > 0 {
		answer["warnings"] = warnings
	}
	return event.JSON(http.StatusOK, answer)
}

func renderNotionPageMarkdown(
	blocks []workspaceExportMarkdownBlock,
	publicPageID string,
	truncated bool,
	publicBlockID func(string) string,
	references map[string]notionMarkdownReference,
	mentions notionMarkdownMentions,
	sources bool,
) notionPageMarkdownProjection {
	return renderNotionPageMarkdownMarked(blocks, publicPageID, truncated, publicBlockID, references, mentions, sources, nil)
}

// renderNotionPageMarkdownMarked writes it with each block's place marked
// when marks is given.
func renderNotionPageMarkdownMarked(
	blocks []workspaceExportMarkdownBlock,
	publicPageID string,
	truncated bool,
	publicBlockID func(string) string,
	references map[string]notionMarkdownReference,
	mentions notionMarkdownMentions,
	sources bool,
	marks *notionMarkdownBlockMarks,
) notionPageMarkdownProjection {
	mentions.learnAhead(blocks, references)
	children := map[string][]workspaceExportMarkdownBlock{}
	for _, block := range blocks {
		children[block.ParentID] = append(children[block.ParentID], block)
	}
	for parentID := range children {
		sort.Slice(children[parentID], func(left, right int) bool {
			if children[parentID][left].Position != children[parentID][right].Position {
				return children[parentID][left].Position < children[parentID][right].Position
			}
			return children[parentID][left].ID < children[parentID][right].ID
		})
	}
	writer := &notionMarkdownWriter{
		children: children, publicPageID: publicPageID, publicBlockID: publicBlockID, references: references,
		mentions: mentions, sources: sources, truncated: truncated, visited: map[string]bool{},
		blank: map[string]bool{}, underItem: map[string]bool{}, unknownBlocks: map[string]string{}, marks: marks,
	}
	lines := []string{}
	if mentions.APIForm {
		lines = writer.apiFlow(children[""], 0)
	} else {
		lines = writer.flow(children[""], "")
	}
	markdown := notionMarkdownCodeBreaksWritten(strings.Join(lines, "\n"))
	written := markdown
	if mentions.APIForm {
		// "v1" ends at its last block's last word and starts at its first
		// block's first, as Notion's (seen 2026-11-10 and 2026-11-14: a page
		// first holding a paragraph of spaces that holds "b" is "b"); the
		// block keeps the space after it.
		markdown = strings.TrimFunc(markdown, unicode.IsSpace)
	}
	if mentions.APIForm {
		// Its last text's ending whitespace stays, escaped (notion_page_markdown_edge_spaces.go).
		markdown = notionMarkdownVersion1EndSpace(markdown, written, notionMarkdownLastBlockType(blocks, writer.rendered))
	}
	return notionPageMarkdownProjection{
		Markdown: markdown, Lines: written, Truncated: writer.truncated, UnknownBlockIDs: writer.unknownIDs, References: writer.named,
		UnknownBlocks: writer.unknownBlocks, RenderedBlockIDs: writer.rendered, BlankBlockIDs: writer.blank,
		Buttons: notionMarkdownButtonNames(blocks, writer.rendered, publicBlockID), KeepLineEnds: mentions.APIForm,
	}
}

func validNotionBlockID(value string) bool {
	decoded, err := parseNotionUserUUID(notionPublicUserID(value))
	return err == nil && formatNotionUserUUID(decoded) == value
}

// notionMarkdownSuggestedRoots are a window's suggested blocks at the top of
// the page.
func notionMarkdownSuggestedRoots(items []graphReadItem) []string {
	roots := []string{}
	for _, item := range items {
		content, _ := item.Value["content"].(map[string]any)
		suggested, _ := content[graphBlockSuggestionKey].(string)
		parent, _ := item.Value["parentId"].(string)
		id, _ := item.Value["id"].(string)
		if item.Kind == graphReadKindBlock && suggested != "" && parent == "" && id != "" {
			roots = append(roots, id)
		}
	}
	return roots
}
