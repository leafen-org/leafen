package app

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Imported Markdown keeps its inline formatting, as Notion's Markdown import
// does: **bold** and __bold__, *italic* and _italic_, ~~strikethrough~~,
// `code`, [links](https://…) and <https://…> autolinks become text spans, and
// a backslash escapes the next punctuation character. Links keep only web
// and mail addresses. A GitHub table becomes a table block with a header
// row, and a line holding only ![alt](https://…) becomes an image block.
//
// Documents imported together refer to one another through two references
// the web writes after creating their pages and uploading their files:
// [text](leafen-page:<page ID>) is a page mention, a line holding only
// ![alt](leafen-file:<file ID>) or [name](leafen-file:<file ID>) is an image
// or file block of that uploaded file, and a line holding only
// [title](leafen-subpage:<page ID>) is where the page's own sub-page stands,
// its block moved there from where the page's making put it. The graph mutation checks the file
// belongs to the workspace and fills in its stored details.

const (
	workspaceImportPageReference    = "leafen-page:"
	workspaceImportSubPageReference = "leafen-subpage:"
	workspaceImportFileReference    = "leafen-file:"
)

var (
	workspaceImportTableSeparatorPattern = regexp.MustCompile(`^\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)*\|?$`)
	workspaceImportImagePattern          = regexp.MustCompile(`^!\[([^\]]*)\]\(\s*(\S+?)\s*\)$`)
	workspaceImportFileLinePattern       = regexp.MustCompile(`^\[([^\]]*)\]\(\s*leafen-file:(\S+?)\s*\)$`)
	workspaceImportReferenceIDPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

type workspaceImportInlineStyle struct {
	bold, italic, strikethrough, code, underline bool
	link, color, background, discussions         string
}

func workspaceImportLinkTarget(raw string) string {
	target := strings.TrimSpace(raw)
	parsed, err := parseGraphLinkURL(target)
	if err != nil || len(target) > 2048 {
		return ""
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		if parsed.Host == "" {
			return ""
		}
		return target
	case "mailto", "tel":
		return target
	}
	return ""
}

// linkTarget is a [text](address) link's address: a web, mail or phone address
// and, in Notion's Markdown, a page of the workspace, or a block on one, by
// Notion's address (/ID#block) or the app's (notionMarkdownPageLinkTarget),
// kept as the app's, which the app opens and the page's backlinks read.
func (parser *workspaceImportInlineParser) linkTarget(raw string) string {
	if !parser.notion {
		return workspaceImportLinkTarget(raw)
	}
	// The MCP's Markdown keeps a user:// link out, as one that opens nothing.
	if notionMarkdownUserLink(raw) && (parser.sitePages == nil || !parser.sitePages.InternalIDs) {
		return "/"
	}
	// A data source's, for a writer that makes links mentions, is as
	// written, the source it mentions, and one by Leafen's own ID as written
	// too, for the write to point here (notion_address_own_sources_here.go).
	if target, source := parser.sitePages.sourceLinkTarget(raw); source {
		return target
	}
	return notionMarkdownReadLinkTarget(raw, parser.sitePages != nil && parser.sitePages.InternalIDs)
}

// notionMarkdownReadLinkTarget is an address as Notion's Markdown links it, a
// [text](address) link's, a link preview's source or an address standing
// bare: one of Notion's naming a page by its ID as a write keeps it
// (notionMarkdownWrittenAddress); for the MCP (mcp), one of the forms it
// names a page by too (notionMarkdownPageLinkTarget).
func notionMarkdownReadLinkTarget(raw string, mcp bool) string {
	if kept, named := notionMarkdownWrittenAddress(raw); named {
		return kept
	}
	if target := workspaceImportLinkTarget(raw); target != "" {
		return target
	}
	if target := notionMarkdownPageLinkTarget(raw, mcp); target != "" {
		return target
	}
	return notionMarkdownSchemelessLinkTarget(raw)
}

// notionMarkdownUserLink is whether a [text](address) link's address is a
// person's (user://…), which Notion's page Markdown links to its app's
// address, in "v1" and "v2" (seen 2026-11-11: "[w](user://abc)" reads
// https://app.notion.com/): the app's own, "/" here. Notion's MCP links
// one as written, which opens nothing, and so stays words in the MCP's
// (an approved difference).
func notionMarkdownUserLink(raw string) bool {
	address := strings.TrimSpace(raw)
	return len(address) >= len("user://") && strings.EqualFold(address[:len("user://")], "user://")
}

// notionMarkdownSchemelessLinkTarget is an address without a scheme as
// Notion's Markdown links it (seen 2026-11-10, "v1" and "v2" alike): a host
// (example.com, www.example.com/x, localhost; notionMarkdownLinkHost) or one
// after // as an https address; a path in the app and an anchor as written,
// as a page's own links are kept (Notion writes its app's address before the
// path). Anything else, which Notion links as a relative address that opens
// nothing ("notaurl", "not%20a%20url", "relative.html", "a.txt", seen
// 2026-11-11), makes no link, as Leafen keeps a link that opens nothing
// out.
func notionMarkdownSchemelessLinkTarget(raw string) string {
	target := strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(target, "//"):
		return workspaceImportLinkTarget("https:" + target)
	case strings.HasPrefix(target, "#"), strings.HasPrefix(target, "/"):
		if validGraphTextLink(target) {
			return target
		}
		return ""
	}
	if notionMarkdownHostPortAddress(target) || !notionMarkdownLinkHost(notionMarkdownSchemelessHost(target)) {
		return ""
	}
	return workspaceImportLinkTarget("https://" + target)
}

// notionMarkdownHostPortAddress is whether an address without a scheme has a
// host and a colon after it before its path ("example.com:8080/x",
// "localhost:3000", "127.0.0.1:8080/x", "example.com:/x"), which Notion's
// Markdown reads as a scheme of the host's name: a [text](address) link of
// it is its text (seen 2026-11-11, "v1" and "v2"), and a "v1" mention of it
// reads as nothing.
func notionMarkdownHostPortAddress(address string) bool {
	host, _, _ := strings.Cut(strings.TrimSpace(address), "/")
	host, _, _ = strings.Cut(host, "?")
	host, _, _ = strings.Cut(host, "#")
	name, _, ported := strings.Cut(host, ":")
	return ported && (strings.EqualFold(name, "localhost") || notionMarkdownBareHostPattern.MatchString(name) ||
		notionMarkdownIPv4Pattern.MatchString(name))
}

var notionMarkdownIPv4Pattern = regexp.MustCompile(`^\d{1,3}(?:\.\d{1,3}){3}$`)

// notionMarkdownSchemelessHost is what an address without a scheme names
// before its path, query or fragment, as a host would stand there.
func notionMarkdownSchemelessHost(target string) string {
	host, _, _ := strings.Cut(strings.TrimSpace(target), "/")
	host, _, _ = strings.Cut(host, "?")
	host, _, _ = strings.Cut(host, "#")
	return host
}

// notionMarkdownLinkHost is whether a host written without a scheme is one
// Notion's Markdown links over https (seen 2026-11-11, "v1" and "v2"):
// localhost, in any case, or names joined by dots whose last is a top-level
// domain as a JSON write's link takes one (graphSchemelessWebLink: "notes.md",
// "a.mov", "x.co.kr", "a.рф", not "relative.html", "host.example",
// "foo.local", "x.onion" or "a.xn--p1ai"), save .zip, which a Markdown link
// keeps as a relative address ("a.zip", "a.ZIP/x") where a JSON write and
// bare text link it.
func notionMarkdownLinkHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if !notionMarkdownBareHostPattern.MatchString(host) || !graphSchemelessWebLink(host) {
		return false
	}
	name, _, _ := strings.Cut(host, ":")
	return !strings.EqualFold(name[strings.LastIndexByte(name, '.')+1:], "zip")
}

// notionMarkdownBareHostPattern is a host written without a scheme: names
// of letters, digits and hyphens joined by dots, a port after it.
var notionMarkdownBareHostPattern = regexp.MustCompile(`^(?:[\p{L}\p{N}](?:[\p{L}\p{N}-]*[\p{L}\p{N}])?\.)+\p{L}[\p{L}\p{N}-]*(?::\d{1,5})?$`)

// notionMarkdownLinkAnchor is the block a relative page address goes to, with
// its #, or "".
func notionMarkdownLinkAnchor(address string) string {
	if _, anchor, found := strings.Cut(strings.TrimSpace(address), "#"); found && anchor != "" {
		return "#" + anchor
	}
	return ""
}

// notionMarkdownPageLinkID is the page a relative page address names.
func notionMarkdownPageLinkID(address string) string {
	address, _, _ = strings.Cut(strings.TrimSpace(address), "#")
	for _, prefix := range []string{"/pages/", "/p/"} {
		if id, named := strings.CutPrefix(address, prefix); named && workspaceImportReferenceIDPattern.MatchString(id) {
			return id
		}
	}
	return ""
}

type workspaceImportInlineParser struct {
	spans []graphTextSpan
	// pending is the text the last span goes on with, put into it when a
	// span after it starts or the line ends (2026-10-14: added a piece at a
	// time, a long import's text was copied over and over).
	pending strings.Builder
	// notion reads Notion's enhanced Markdown: $equations$, <span> underline
	// and color, and addresses standing bare as links.
	notion bool
	// version1 reads code and emphasis as the public API's "v1" does, its own
	// way (notionMarkdownVersion1Emphasis); Notion's others read emphasis as
	// CommonMark (importEmphasis).
	version1 bool
	// cell reads a "v1" table cell's text, whose links take an address a
	// space parts (graphWrittenCellTextLink), and which keeps a
	// piece for each thing read in it (seen 2026-11-11, where a paragraph's
	// pieces of one style join): each emphasis, link, <span> and code its
	// own piece, the text between them one piece, and code of nothing ("``")
	// an empty piece of no style. node is what the text being put was read
	// as, and joins the node the last span's text came from, 0 for one that
	// nothing joins; nodes counts them.
	cell               bool
	node, joins, nodes int
	// cuts are where, in the spans' text, a piece Notion's reading keeps
	// apart begins inside a run the spans join: the text of each emphasis,
	// <span> and code its own, as Notion leaves them when Markdown rewrites a
	// block whose words and marks it doesn't change (seen 2026-11-11: "a"
	// and "b" of "**a****b**" two bold pieces; a link's text one piece).
	cuts []int
	// tagPieces are where, in the spans' text, the words a "v1" mention tag
	// reads as begin and end, kept apart as Notion's reading keeps them
	// however the text is marked (version1MentionWords), the two the same
	// for one that reads as an empty piece; notionMarkdownParsedPieces puts
	// them among the cuts.
	tagPieces []int
	// plain is how long the spans' text is so far, and barriers where an
	// escaped colon or full stop parts it (notionMarkdownBareLinks).
	plain    int
	barriers map[int]bool
	// sitePages reads an address of one of this server's pages standing bare
	// in Notion's Markdown as a mention of it (notionMarkdownSitePages); nil
	// reads it as a link.
	sitePages *notionMarkdownSitePages
}

// add puts a span of its own into the line's spans.
func (parser *workspaceImportInlineParser) add(span graphTextSpan) {
	parser.flush()
	parser.spans = append(parser.spans, span)
	parser.plain += len(span.Text)
	parser.joins = 0
}

// read starts a node of the line's text and makes it the one text is put
// for, returning it.
func (parser *workspaceImportInlineParser) read() int {
	parser.nodes++
	parser.node = parser.nodes
	return parser.node
}

func (parser *workspaceImportInlineParser) emit(text string, style workspaceImportInlineStyle) {
	if text == "" {
		return
	}
	parser.plain += len(text)
	span := graphTextSpan{
		Text: text, Bold: style.bold, Italic: style.italic, Strikethrough: style.strikethrough,
		Code: style.code, Link: style.link, Underline: style.underline, Color: style.color,
		Background: style.background, CommentID: parser.sitePages.discussionThreads(style.discussions),
	}
	if count := len(parser.spans); count > 0 && (!parser.cell || parser.joins == parser.node) {
		last := &parser.spans[count-1]
		if last.Mention == "" && last.Equation == "" && last.EmptyPiece == "" && last.Bold == span.Bold && last.Italic == span.Italic &&
			last.Strikethrough == span.Strikethrough && last.Code == span.Code && last.Link == span.Link &&
			last.Underline == span.Underline && last.Color == span.Color && last.Background == span.Background && last.CommentID == span.CommentID {
			if parser.pending.Len() == 0 {
				parser.pending.WriteString(last.Text)
			}
			parser.pending.WriteString(text)
			// Text of another node joins the run, where Notion's reading
			// starts a piece of its own; one link's text goes on as one.
			if parser.joins != parser.node && last.Link == "" {
				parser.cuts = append(parser.cuts, parser.plain-len(text))
			}
			parser.joins = parser.node
			return
		}
	}
	parser.flush()
	parser.spans = append(parser.spans, span)
	parser.joins = parser.node
}

// flush puts the text the last span goes on with into it.
func (parser *workspaceImportInlineParser) flush() {
	if parser.pending.Len() > 0 && len(parser.spans) > 0 {
		parser.spans[len(parser.spans)-1].Text = parser.pending.String()
	}
	parser.pending.Reset()
}

func workspaceImportWordByte(value string, index int, before bool) bool {
	var r rune
	if before {
		if index <= 0 {
			return false
		}
		r, _ = utf8.DecodeLastRuneInString(value[:index])
	} else {
		if index >= len(value) {
			return false
		}
		r, _ = utf8.DecodeRuneInString(value[index:])
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func workspaceImportSpaceAt(value string, index int) bool {
	if index < 0 || index >= len(value) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(value[index:])
	return unicode.IsSpace(r)
}

// workspaceImportLinkClose is where a link's address in rest ends: its first
// ), or in Notion's Markdown the ) past the ones it opens, as CommonMark's,
// a \) escaped but in "v1", which reads a backslash there as itself (seen
// 2026-11-10: "[x](https://example.com/a\)b)" links https://example.com/a/).
func workspaceImportLinkClose(rest string, balanced, escapes bool) int {
	if !balanced {
		return strings.IndexByte(rest, ')')
	}
	depth := 0
	for index := 0; index < len(rest); index++ {
		switch rest[index] {
		case '\\':
			if escapes {
				index++
			}
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return index
			}
			depth--
		}
	}
	return strings.IndexByte(rest, ')')
}

// workspaceImportUnderscoreOpens reports whether the run of underscores at
// value[index:] may open emphasis: no letter or digit before it and no space
// after it. Emphasis is read before addresses standing bare, as Notion
// reads it (seen 2026-11-10): https://example.com/_a_ is a link and an
// italic a.
func workspaceImportUnderscoreOpens(value string, index int) bool {
	run := index + len(value[index:]) - len(strings.TrimLeft(value[index:], "_"))
	return !workspaceImportWordByte(value, index, true) && !workspaceImportSpaceAt(value, run)
}

// closing finds where a delimiter run that opens at start closes, as
// CommonMark's: at the next run of its mark at least as long that may close
// it (no space before it; for _, no letter or digit after it), past the runs
// that open inside it and the ones that close those. A closing run doesn't
// stand in code or a link's address, nor right after the opening one.
func workspaceImportClosing(value string, start int, delimiter string) int {
	mark := delimiter[0]
	inner := 0
	for index := start + len(delimiter); index < len(value); index++ {
		switch {
		case value[index] == '\\':
			index++
			continue
		case value[index] == '`':
			if end := strings.IndexByte(value[index+1:], '`'); end >= 0 {
				index += end + 1
				continue
			}
		case strings.HasPrefix(value[index:], "]("):
			if end := strings.IndexByte(value[index+2:], ')'); end >= 0 {
				index += end + 2
				continue
			}
		}
		if value[index] != mark {
			continue
		}
		run := index + len(value[index:]) - len(strings.TrimLeft(value[index:], string(mark)))
		closes := index > start+len(delimiter) && !workspaceImportSpaceAt(value, index-1) &&
			!(mark == '_' && workspaceImportWordByte(value, run, false))
		opens := !workspaceImportSpaceAt(value, run) && !(mark == '_' && workspaceImportWordByte(value, index, true))
		switch {
		case closes && inner > 0:
			inner--
		case closes && run-index >= len(delimiter):
			return index
		case opens && !closes:
			inner++
		}
		index = run - 1
	}
	return -1
}

// workspaceImportInlineSpans reads a line's inline Markdown into its plain
// text and spans; spans are nil when the text carries no formatting.
func workspaceImportInlineSpans(value string) (string, []graphTextSpan) {
	parser := workspaceImportInlineParser{}
	parser.parse(value, workspaceImportInlineStyle{}, 0)
	plain := strings.Builder{}
	styled := false
	for _, span := range parser.spans {
		plain.WriteString(span.Text)
		styled = styled || span.Bold || span.Italic || span.Strikethrough || span.Code || span.Link != "" || span.Mention != ""
	}
	if !styled {
		return plain.String(), nil
	}
	return plain.String(), parser.spans
}

// splitWorkspaceImportSpans cuts spans at the same byte boundaries as the
// plain text fragments.
func splitWorkspaceImportSpans(fragments []string, spans []graphTextSpan) [][]graphTextSpan {
	result := make([][]graphTextSpan, len(fragments))
	spanIndex, offset := 0, 0
	for fragmentIndex, fragment := range fragments {
		left := len(fragment)
		for left > 0 && spanIndex < len(spans) {
			span := spans[spanIndex]
			remaining := span.Text[offset:]
			if len(remaining) <= left {
				part := span
				part.Text = remaining
				result[fragmentIndex] = append(result[fragmentIndex], part)
				left -= len(remaining)
				spanIndex, offset = spanIndex+1, 0
				continue
			}
			part := span
			part.Text = remaining[:left]
			result[fragmentIndex] = append(result[fragmentIndex], part)
			offset += left
			left = 0
		}
	}
	return result
}

func workspaceImportTableCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	if strings.HasSuffix(trimmed, "|") && !strings.HasSuffix(trimmed, `\|`) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	cells := []string{}
	current := strings.Builder{}
	for index := 0; index < len(trimmed); index++ {
		if trimmed[index] == '\\' && index+1 < len(trimmed) && trimmed[index+1] == '|' {
			current.WriteByte('|')
			index++
			continue
		}
		if trimmed[index] == '|' {
			cells = append(cells, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}
		current.WriteByte(trimmed[index])
	}
	return append(cells, strings.TrimSpace(current.String()))
}

// workspaceImportTable reads a GitHub table starting at lines[start]: its
// header row, separator and body rows, every column and every cell's text.
// It reports how many lines it used, and false for a table past what a row
// can hold.
func workspaceImportTable(lines []string, start int) ([]workspaceImportParsedBlock, int, bool) {
	if start+1 >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[start]), "|") ||
		!workspaceImportTableSeparatorPattern.MatchString(strings.TrimSpace(lines[start+1])) {
		return nil, 0, true
	}
	rows := [][]string{workspaceImportTableCells(lines[start])}
	used := 2
	for start+used < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[start+used]), "|") {
		rows = append(rows, workspaceImportTableCells(lines[start+used]))
		used++
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row))
	}
	width = max(width, minGraphTableWidth)
	if width > maxGraphTableWidth {
		return nil, used, false
	}
	indent := workspaceImportIndent(lines[start])
	tableContent, _ := json.Marshal(graphTableBlockContent{TableWidth: width, HasColumnHeader: true})
	blocks := []workspaceImportParsedBlock{{Type: graphBlockTypeTable, Content: tableContent, Indent: indent}}
	for _, row := range rows {
		cells := make([][]graphTextSpan, width)
		for column := range cells {
			cells[column] = []graphTextSpan{}
			if column < len(row) && row[column] != "" {
				plain, spans := workspaceImportInlineSpans(row[column])
				if spans == nil {
					spans = []graphTextSpan{{Text: plain}}
				}
				cells[column] = spans
			}
		}
		rowContent, _ := json.Marshal(graphTableRowBlockContent{Cells: cells})
		blocks = append(blocks, workspaceImportParsedBlock{Type: graphBlockTypeTableRow, Content: rowContent, Indent: indent + 1})
	}
	return blocks, used, true
}

// workspaceImportManagedFileContent is an uploaded file's block content;
// the graph mutation replaces its details with the stored file's. Its row
// shows no size, as the file blocks Notion's ZIP import brings (seen
// 2026-11-07 and 2026-11-12).
func workspaceImportManagedFileContent(fileID, name string) json.RawMessage {
	content, _ := json.Marshal(graphFileBlockContent{
		FileID: fileID, Name: name, ContentType: "application/octet-stream", SHA256: strings.Repeat("0", 64),
		SizeHidden: true,
	})
	return content
}

// workspaceImportManagedImageContent is an uploaded image's block shown at
// its own size at the column's start, as Notion's import draws the pictures
// it brings, unlike a picture pasted in (seen 2026-11-07); words given show
// under it as its caption and are its alt text, as Notion's Markdown import
// keeps an image's alt, which the web writes as the image's title.
func workspaceImportManagedImageContent(fileID, name, words string) json.RawMessage {
	if utf8.RuneCountInString(words) > maxGraphTextCharacters {
		words = string([]rune(words)[:maxGraphTextCharacters])
	}
	block := graphFileBlockContent{
		FileID: fileID, Name: name, ContentType: "application/octet-stream", SHA256: strings.Repeat("0", 64), NaturalSize: true,
	}
	if words != "" {
		block.Caption, block.AltText = []graphTextSpan{{Text: words}}, words
	}
	content, _ := json.Marshal(block)
	return content
}

// workspaceImportMediaLine reads a line holding only an image (a web
// address or an uploaded file) or an uploaded file link into its block.
func workspaceImportMediaLine(line string) (string, json.RawMessage, bool) {
	trimmed := strings.TrimSpace(line)
	if match := workspaceImportFileLinePattern.FindStringSubmatch(trimmed); match != nil &&
		workspaceImportReferenceIDPattern.MatchString(match[2]) {
		name := strings.TrimSpace(match[1])
		if name == "" {
			name = "file"
		}
		return graphBlockTypeFile, workspaceImportManagedFileContent(match[2], name), true
	}
	match := workspaceImportImagePattern.FindStringSubmatch(trimmed)
	if match == nil {
		return "", nil, false
	}
	if fileID, uploaded := strings.CutPrefix(match[2], workspaceImportFileReference); uploaded {
		if !workspaceImportReferenceIDPattern.MatchString(fileID) {
			return "", nil, false
		}
		name := strings.TrimSpace(match[1])
		if name == "" {
			name = "image"
		}
		return graphBlockTypeImage, workspaceImportManagedImageContent(fileID, name, ""), true
	}
	content, image := workspaceImportImageLine(match[2])
	return graphBlockTypeImage, content, image
}

// workspaceImportImageLine reads an image's web address into an image
// block's content.
func workspaceImportImageLine(address string) (json.RawMessage, bool) {
	target := workspaceImportLinkTarget(address)
	if target == "" || strings.HasPrefix(strings.ToLower(target), "mailto:") {
		return nil, false
	}
	content, _ := json.Marshal(map[string]string{"url": target})
	return content, true
}
