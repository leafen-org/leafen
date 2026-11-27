package app

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"

	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	// A block with more formatting changes than this keeps its text plain,
	// so one staged item stays within a staged chunk.
	workspaceImportMaxBlockSpans = 4096
)

var (
	workspaceImportHeadingPattern = regexp.MustCompile(`^(#{1,6})[ \t]+(.+)$`)
	// A list item, a to-do or a quote may be empty, as Notion writes one: "-",
	// "1.", "- [ ]", ">".
	workspaceImportBulletPattern = regexp.MustCompile(`^[ \t]*[-*+](?:[ \t]+(.*))?$`)
	workspaceImportNumberPattern = regexp.MustCompile(`^[ \t]*[0-9]+\.(?:[ \t]+(.*))?$`)
	workspaceImportTodoPattern   = regexp.MustCompile(`^[ \t]*[-*+][ \t]+\[([ xX])\](?:[ \t]+(.*))?$`)
	// A divider is three or more of one of -, * and _, spaces between them
	// or not (notionMarkdownFlowThematic); "v1" reads dashes alone from two,
	// none between them (seen 2026-11-11: "--" and "\t-- " are dividers,
	// "-- -", "*-*" and "-_-" text).
	workspaceImportVersion1DividerPattern = regexp.MustCompile(`^(?:-{2,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$`)
)

type workspaceImportStagedItem struct {
	ID           string          `json:"id"`
	PageID       string          `json:"pageId,omitempty"`
	ParentID     string          `json:"parentId"`
	AfterID      string          `json:"afterId"`
	Position     string          `json:"position"`
	Type         string          `json:"type"`
	Text         string          `json:"text"`
	TextDelta    json.RawMessage `json:"textDelta,omitempty"`
	Spans        []graphTextSpan `json:"spans,omitempty"`
	Content      json.RawMessage `json:"content"`
	Start        bool            `json:"start,omitempty"`
	BodyPosition *int            `json:"bodyPosition,omitempty"`
	Operation    *graphOperation `json:"operation,omitempty"`
	// PropertyDeletedAt keeps an imported property in Deleted properties.
	PropertyDeletedAt *string `json:"propertyDeletedAt,omitempty"`
	// WikiSource names the Owner and Verification properties of an imported
	// data source that was a wiki's.
	WikiSource *workspaceImportStagedWikiSource `json:"wikiSource,omitempty"`
	// TaskSettings are an imported data source's sub-item, dependency and
	// task settings and its dependency binding, written when it publishes.
	TaskSettings *workspaceImportStagedTaskSettings `json:"taskSettings,omitempty"`
	// StarterIdentity is what an imported data source was made as.
	StarterIdentity *archiveDataSourceStarterIdentity     `json:"starterIdentity,omitempty"`
	Page            *workspaceImportStagedPage            `json:"page,omitempty"`
	Comment         *workspaceImportStagedComment         `json:"comment,omitempty"`
	Template        *workspaceImportStagedTemplate        `json:"template,omitempty"`
	BlockTrash      *workspaceImportStagedBlockTrash      `json:"blockTrash,omitempty"`
	CustomEmoji     *workspaceImportStagedCustomEmoji     `json:"customEmoji,omitempty"`
	RowPageBlock    *workspaceImportStagedRowPageBlock    `json:"rowPageBlock,omitempty"`
	RowBlockTrash   *workspaceImportStagedRowBlockTrash   `json:"rowBlockTrash,omitempty"`
	Automation      *workspaceImportStagedAutomation      `json:"automation,omitempty"`
	RowPresentation *workspaceImportStagedRowPresentation `json:"rowPresentation,omitempty"`
	// Title is a Markdown document's first heading taken as its page's
	// title, and Anchors the page's own sub-pages hanging on this block, or
	// on that title (workspace_import_field_pages.go).
	Title   *workspaceImportStagedTitle `json:"title,omitempty"`
	Anchors []string                    `json:"anchors,omitempty"`
	// Head is the icon and cover a Markdown document's first line names for
	// its page (workspace_import_page_head.go).
	Head *workspaceImportStagedPageHead `json:"head,omitempty"`

	// Cuts are where, in Text, a piece of Notion's reading begins inside a
	// run of Spans (workspaceImportInlineParser.cuts); page Markdown only.
	Cuts []int `json:"-"`
	// InlineDatabase marks a database its page holds inline, which the
	// import turns once its text is in (workspace_import_inline_databases.go).
	InlineDatabase string `json:"-"`
}

type workspaceImportParsedBlock struct {
	// ID is a block's own when it is one already there (a sub-page's block
	// put where its document named it), otherwise a new one.
	ID      string
	Type    string
	Text    string
	Spans   []graphTextSpan
	Content json.RawMessage
	Indent  int
	// Anchors are the page's own sub-pages hanging on the block
	// (workspace_import_field_pages.go).
	Anchors []string
	// Database is an inline database block's data source, properties, view
	// and rows (workspace_import_markdown_database.go).
	Database *workspaceImportMarkdownDatabase

	// Cuts are where, in Text, a piece of Notion's reading begins inside a
	// run of Spans (workspaceImportInlineParser.cuts).
	Cuts []int
}

// workspaceImportBlockList collects parsed blocks within an import's
// bounds: long text is cut into fragments and over-formatted text kept plain.
type workspaceImportBlockList struct {
	blocks []workspaceImportParsedBlock
}

func (list *workspaceImportBlockList) add(kind, text string, spans []graphTextSpan, content json.RawMessage, indent int) bool {
	fragments := splitWorkspaceImportText(text)
	if len(fragments) == 0 {
		fragments = []string{""}
	}
	fragmentSpans := make([][]graphTextSpan, len(fragments))
	// A block the text isn't cut in keeps its spans as they are.
	if spans != nil && len(fragments) == 1 && fragments[0] == text {
		fragmentSpans[0] = spans
	} else if spans != nil {
		fragmentSpans = splitWorkspaceImportSpans(fragments, spans)
	}
	for index, fragment := range fragments {
		if len(fragmentSpans[index]) > workspaceImportMaxBlockSpans {
			fragmentSpans[index] = nil
		}
		list.blocks = append(list.blocks, workspaceImportParsedBlock{
			Type: kind, Text: fragment, Spans: fragmentSpans[index], Content: content, Indent: indent,
		})
	}
	return true
}

// items stages the blocks in order, each under the nearest block before it
// one level out. Markdown of spaces and line breaks alone makes none, as
// Notion's leaves a page written with it empty (seen 2026-11-10).
func (list *workspaceImportBlockList) items() []workspaceImportStagedItem {
	items := make([]workspaceImportStagedItem, 0, len(list.blocks))
	builder := newWorkspaceImportItemBuilder()
	for _, block := range list.blocks {
		items = append(items, builder.item(block))
	}
	return items
}

// workspaceImportItemBuilder stages parsed blocks one after another, each
// under the nearest block before it one level out, so a long file's blocks
// can be staged a part at a time.
type workspaceImportItemBuilder struct {
	count       int
	parentStack map[int]string
	lastSibling map[string]string
}

func newWorkspaceImportItemBuilder() *workspaceImportItemBuilder {
	return &workspaceImportItemBuilder{parentStack: map[int]string{}, lastSibling: map[string]string{}}
}

func (builder *workspaceImportItemBuilder) item(block workspaceImportParsedBlock) workspaceImportStagedItem {
	id := block.ID
	if id == "" {
		id = security.RandomString(15)
	}
	parentID := ""
	for depth := block.Indent - 1; depth >= 0; depth-- {
		if candidate := builder.parentStack[depth]; candidate != "" {
			parentID = candidate
			break
		}
	}
	item := workspaceImportStagedItem{
		ID: id, ParentID: parentID, AfterID: builder.lastSibling[parentID], Position: workspaceImportPosition(builder.count),
		Type: block.Type, Text: block.Text, Spans: block.Spans, Cuts: block.Cuts, Content: block.Content,
		Anchors: block.Anchors,
	}
	builder.count++
	builder.lastSibling[parentID] = id
	builder.parentStack[block.Indent] = id
	for depth := range builder.parentStack {
		if depth > block.Indent {
			delete(builder.parentStack, depth)
		}
	}
	return item
}

// markdownParseOptions reads Notion's enhanced Markdown.
type markdownParseOptions struct {
	// Reference takes a line, outside code, that is a block of its own with
	// the type and content it gives.
	Reference func(line string) (string, json.RawMessage, bool)
	// KeepLineEnds keeps the whitespace ending a paragraph's, a list item's,
	// a to-do's or a callout's text line (notionPageMarkdownProjection), and
	// reads emphasis as the public API's "v1" does: both are its reading.
	KeepLineEnds bool
	// ReferenceImages leaves pictures to Reference: a ![…](…) line it doesn't
	// take is text (notionMarkdownImageTaken).
	ReferenceImages bool
	// SitePages reads the addresses of its pages standing bare in the text
	// as their mentions (notionMarkdownSitePages); nil as links.
	SitePages *notionMarkdownSitePages
}

var markdownLineBreakPattern = regexp.MustCompile(`(?i)<br\s*/?>`)

// parseMarkdownItems reads Notion's enhanced Markdown into blocks, a line at
// a time, as Notion writes it: each line is a block of its own, <br> a line
// break inside a block, <empty-block/> an empty paragraph and a tab one
// level of indentation. Its tags and references are Notion's, not
// CommonMark, so plain Markdown is read by goldmark instead.
func parseMarkdownItems(markdown string, options markdownParseOptions) ([]workspaceImportStagedItem, error) {
	markdown = strings.ReplaceAll(strings.ReplaceAll(markdown, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(markdown, "\n")
	list := workspaceImportBlockList{blocks: make([]workspaceImportParsedBlock, 0, len(lines))}
	inFence := false
	fence := ""
	fenceIndent := 0
	fenceLead := ""
	fenceLanguage := ""
	fenceLines := []string{}
	// The numbered items whose start their <ol> named, by block.
	namedStarts, taggedItems := map[int]bool{}, map[int]int{}

	indentOf := notionMarkdownIndent
	appendBlock := func(kind, text string, content json.RawMessage, indent int) bool {
		var spans []graphTextSpan
		var cuts []int
		switch {
		case kind != graphBlockTypeCode:
			text, spans, cuts = notionMarkdownSitePieces(
				notionMarkdownLineBreaks(text, options.KeepLineEnds), options.KeepLineEnds, options.SitePages,
			)
		case text == "":
			// Code of nothing is one empty piece of text, as Notion reads
			// one its page Markdown made (seen 2026-11-11: rich text [""],
			// and [] for one made as JSON; graph_text_empty_pieces.go).
			spans = []graphTextSpan{{EmptyPiece: graphTextEmptyText}}
		}
		added := len(list.blocks)
		if !list.add(kind, text, spans, content, indent) {
			return false
		}
		// A line kept whole keeps where its reading's pieces begin.
		if len(cuts) > 0 && len(list.blocks) == added+1 && list.blocks[added].Text == text {
			list.blocks[added].Cuts = cuts
		}
		return true
	}
	appendFence := func() bool {
		content, _ := json.Marshal(map[string]string{"language": fenceLanguage})
		return appendBlock(graphBlockTypeCode, strings.Join(fenceLines, "\n"), content, fenceIndent)
	}

	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		line := lines[lineIndex]
		trimmed := strings.TrimSpace(line)
		if inFence {
			// A fence closes at one as long as its opening, of backticks alone.
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				if !appendFence() {
					return nil, workspaceImportInputError()
				}
				inFence, fenceLanguage, fenceLines = false, "", nil
			} else {
				// Code stands as far in as its fence and keeps what is further.
				fenceLines = append(fenceLines, strings.TrimPrefix(line, fenceLead))
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			fenceLead = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			inFence = true
			fence = trimmed[:len(trimmed)-len(strings.TrimLeft(trimmed, "`"))]
			fenceIndent = indentOf(line)
			// A language's own value reads back as written, so a page's Markdown
			// keeps its blocks ("vb.net", which a file import reads as plain
			// text); other names read as a Markdown import does — py, as Notion
			// writes Python's fence, is Python.
			fenceLanguage = strings.TrimSpace(strings.TrimPrefix(trimmed, fence))
			if validNotionCodeLanguage(fenceLanguage) {
				fenceLanguage = canonicalNotionCodeLanguage(fenceLanguage)
			} else {
				fenceLanguage = importedCodeLanguage(fenceLanguage)
			}
			continue
		}
		if trimmed == "" {
			continue
		}
		if options.Reference != nil {
			if kind, content, ok := options.Reference(line); ok {
				if !appendBlock(kind, "", content, indentOf(line)) {
					return nil, workspaceImportInputError()
				}
				continue
			}
		}
		// A callout's text is the line after its tag, one level in, when that
		// is a line of text; a block there (a heading, a to-do, an empty line
		// as <empty-block/>…) is its first line as that block, as Notion's
		// reads one (seen 2026-11-12).
		if content, text, closed, callout := notionMarkdownCalloutLine(trimmed); callout {
			if next := lineIndex + 1; !closed && text == "" && next < len(lines) &&
				notionMarkdownCalloutText(lines[next], indentOf(line), options) {
				if !notionMarkdownCalloutBlockLine(lines, next, indentOf, options) {
					text = options.lineText(lines[next])
					lineIndex++
				} else if notionMarkdownCalloutColoredLine(lines[next], options) {
					// A paragraph in a color first is its text all the same
					// (notion_block_callout_line.go).
					content = withCalloutText(content, calloutTextLine)
				}
			}
			// A callout holds its lines, its first too, as the screen's does.
			if !appendBlock(graphBlockTypeCallout, "", content, indentOf(line)) ||
				text != "" && !appendBlock(graphBlockTypeParagraph, text, json.RawMessage(`{}`), indentOf(line)+1) {
				return nil, workspaceImportInputError()
			}
			continue
		}
		if trimmed == notionMarkdownCalloutClose {
			continue
		}
		// Toggles, equations, tables and columns, written with tags of their own.
		if blocks, used, fits := notionMarkdownStructure(lines, lineIndex, indentOf, options.KeepLineEnds, options.SitePages); used > 0 {
			if !fits {
				return nil, workspaceImportInputError()
			}
			for _, block := range blocks {
				if !list.add(block.Type, block.Text, block.Spans, block.Content, block.Indent) {
					return nil, workspaceImportInputError()
				}
			}
			lineIndex += used - 1
			continue
		}
		// An empty block's color ends its line, as a text line's does.
		if empty, color := takeNotionMarkdownBlockColor(trimmed); strings.EqualFold(empty, "<empty-block/>") {
			if !appendBlock(graphBlockTypeParagraph, "", withNotionMarkdownBlockColor(json.RawMessage(`{}`), color), indentOf(line)) {
				return nil, workspaceImportInputError()
			}
			continue
		}
		if table, used, fits := workspaceImportTable(lines, lineIndex); used > 0 {
			if !fits {
				return nil, workspaceImportInputError()
			}
			list.blocks = append(list.blocks, table...)
			lineIndex += used - 1
			continue
		}
		if kind, content, media := workspaceImportMediaLine(line); media && !(options.ReferenceImages && kind == graphBlockTypeImage) {
			if !appendBlock(kind, "", content, indentOf(line)) {
				return nil, workspaceImportInputError()
			}
			continue
		}
		// A text block's color ends its line (notion_page_markdown_block_color.go).
		colored, color := takeNotionMarkdownBlockColor(strings.TrimRightFunc(line, unicode.IsSpace))
		if color == "" {
			colored = line
		}
		// A heading's marks and a color are an empty heading of that color
		// (notion_page_markdown_empty_headings.go).
		if kind, empty := notionMarkdownEmptyHeading(strings.TrimSpace(colored)); empty && color != "" {
			if !appendBlock(kind, "", withNotionMarkdownBlockColor(json.RawMessage(`{}`), color), indentOf(line)) {
				return nil, workspaceImportInputError()
			}
			continue
		}
		if kind, text, content, _, matched := classifyWorkspaceImportMarkdownLine(colored, options.KeepLineEnds); matched {
			if options.KeepLineEnds && text != "" && kind != graphBlockTypeQuote && !strings.HasPrefix(kind, "heading") {
				text += colored[len(strings.TrimRightFunc(colored, unicode.IsSpace)):]
			}
			if kind == graphBlockTypeNumberedListItem {
				var named, tagged bool
				text, content, named, tagged = notionMarkdownNumberedContent(strings.TrimSpace(colored), text, content, options.KeepLineEnds)
				// A plain line right after an <ol>'s item at its depth begins a
				// Markdown list of its own, its number kept (notion_page_markdown_list_numbers.go).
				if at := len(list.blocks); named || !tagged && taggedItems[at-1] == indentOf(line)+1 {
					namedStarts[at] = true
				}
				if tagged {
					taggedItems[len(list.blocks)] = indentOf(line) + 1
				}
			}
			if !appendBlock(kind, text, withNotionMarkdownBlockColor(content, color), indentOf(line)) {
				return nil, workspaceImportInputError()
			}
			continue
		}
		if !appendBlock(graphBlockTypeParagraph, options.lineText(colored), withNotionMarkdownBlockColor(json.RawMessage(`{}`), color), indentOf(line)) {
			return nil, workspaceImportInputError()
		}
	}
	if inFence && !appendFence() {
		return nil, workspaceImportInputError()
	}
	items := list.items()
	notionMarkdownListStarts(items, namedStarts)
	return items, nil
}

// lineText is a text line's text: without its indentation, and without the
// whitespace ending it unless the Markdown keeps it.
func (options markdownParseOptions) lineText(line string) string {
	if options.KeepLineEnds {
		return strings.TrimLeftFunc(line, unicode.IsSpace)
	}
	return strings.TrimSpace(line)
}

// notionMarkdownCalloutBlockLine reports whether a callout's first line is
// a block of its own rather than text: a heading, a list item, a to-do, a
// quote, a divider, a colored line, media, a table or a tag's block.
func notionMarkdownCalloutBlockLine(lines []string, index int, indentOf func(string) int, options markdownParseOptions) bool {
	line := strings.TrimRightFunc(lines[index], unicode.IsSpace)
	if _, color := takeNotionMarkdownBlockColor(line); color != "" {
		return true
	}
	if _, _, _, _, matched := classifyWorkspaceImportMarkdownLine(line, options.KeepLineEnds); matched {
		return true
	}
	if _, _, media := workspaceImportMediaLine(lines[index]); media {
		return true
	}
	if _, used, _ := workspaceImportTable(lines, index); used > 0 {
		return true
	}
	_, used, _ := notionMarkdownStructure(lines, index, indentOf, options.KeepLineEnds, options.SitePages)
	return used > 0
}

// notionMarkdownCalloutColoredLine reports whether a callout's first line is
// a paragraph of text, or an empty one, in a color.
func notionMarkdownCalloutColoredLine(line string, options markdownParseOptions) bool {
	rest, color := takeNotionMarkdownBlockColor(strings.TrimRightFunc(line, unicode.IsSpace))
	if color == "" {
		return false
	}
	if _, _, _, _, matched := classifyWorkspaceImportMarkdownLine(rest, options.KeepLineEnds); matched {
		return false
	}
	_, _, media := workspaceImportMediaLine(rest)
	return !media
}

// notionMarkdownCalloutText reports whether a line after a callout's tag is
// its text: one level in, and not a block's own tag.
func notionMarkdownCalloutText(line string, indent int, options markdownParseOptions) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || notionMarkdownIndent(line) != indent+1 || trimmed == notionMarkdownCalloutClose ||
		strings.EqualFold(trimmed, "<empty-block/>") || strings.HasPrefix(trimmed, "```") {
		return false
	}
	if _, _, _, callout := notionMarkdownCalloutLine(trimmed); callout {
		return false
	}
	if options.Reference != nil {
		if _, _, reference := options.Reference(line); reference {
			return false
		}
	}
	return true
}

// classifyWorkspaceImportMarkdownLine is the block a line of Notion's
// enhanced Markdown starts, read as the public API's "v1" reads it when
// version1 is set.
func classifyWorkspaceImportMarkdownLine(line string, version1 bool) (string, string, json.RawMessage, int, bool) {
	indent := workspaceImportIndent(line)
	trimmed := strings.TrimSpace(line)
	if match := workspaceImportHeadingPattern.FindStringSubmatch(trimmed); match != nil {
		return notionMarkdownHeadingKind(len(match[1])), match[2], json.RawMessage(`{}`), indent, true
	}
	// Its marks alone are an empty heading, but text in "v1"
	// (notion_page_markdown_empty_headings.go).
	if kind, empty := notionMarkdownEmptyHeading(trimmed); empty && !version1 {
		return kind, "", json.RawMessage(`{}`), indent, true
	}
	if match := workspaceImportTodoPattern.FindStringSubmatch(trimmed); match != nil {
		content, _ := json.Marshal(map[string]bool{"checked": strings.EqualFold(match[1], "x")})
		return graphBlockTypeToDo, match[2], content, indent, true
	}
	if match := workspaceImportBulletPattern.FindStringSubmatch(trimmed); match != nil {
		return graphBlockTypeBulletedListItem, match[1], json.RawMessage(`{}`), indent, true
	}
	if match := workspaceImportNumberPattern.FindStringSubmatch(trimmed); match != nil {
		return graphBlockTypeNumberedListItem, match[1], json.RawMessage(`{}`), indent, true
	}
	if strings.HasPrefix(trimmed, ">") {
		return graphBlockTypeQuote, strings.TrimSpace(strings.TrimPrefix(trimmed, ">")), json.RawMessage(`{}`), indent, true
	}
	if version1 && workspaceImportVersion1DividerPattern.MatchString(trimmed) ||
		!version1 && notionMarkdownFlowThematic.MatchString(trimmed) {
		return graphBlockTypeDivider, "", json.RawMessage(`{}`), indent, true
	}
	return "", "", nil, 0, false
}

func workspaceImportIndent(line string) int {
	spaces := len(line) - len(strings.TrimLeft(line, " \t"))
	return spaces / 2
}

// notionMarkdownIndent is a line's depth in Notion's enhanced Markdown: a tab
// or two spaces for each level.
func notionMarkdownIndent(line string) int {
	depth, spaces := 0, 0
	for _, character := range line {
		switch character {
		case '\t':
			depth++
		case ' ':
			spaces++
		default:
			return depth + spaces/2
		}
	}
	return depth + spaces/2
}

func workspaceImportPosition(index int) string {
	const digits = "0123456789"
	buffer := []byte("import:00000000")
	for cursor := len(buffer) - 1; cursor >= len("import:"); cursor-- {
		buffer[cursor] = digits[index%10]
		index /= 10
	}
	return string(buffer)
}
