package app

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"
)

// Imported Markdown is read by goldmark as CommonMark with GitHub's tables,
// strikethrough, task lists, the bare addresses Notion's Markdown import
// links (workspaceImportLinkify) and $…$ and $$…$$ equations
// (workspace_import_markdown_math.go) and <aside> blocks
// (workspace_import_markdown_aside.go), and its syntax tree becomes blocks and
// text spans directly. What the tree means in Leafen stays Leafen's: which
// links are kept (workspaceImportLinkTarget), page and file references, a
// lone image or file line as its own block, and the import's size bounds.
var workspaceImportMarkdown = goldmark.New(goldmark.WithExtensions(workspaceImportTables{}, extension.Strikethrough, extension.TaskList, workspaceImportLinkify,
	workspaceImportMathExtension{}, workspaceImportAsideExtension{}))

// workspaceImportLinkify links the bare addresses in imported Markdown's
// words that Notion's Markdown and text import links (seen 2026-11-13): a
// web address with http:// or https:// in any case, a host without a dot
// (localhost:3000) too, a mailto: address and a bare mail address, ending
// before the punctuation that ends a sentence; a www. address without a
// scheme and a bare domain stay words. An ftp:// one stays words too, as
// Leafen's links take web, mail and phone addresses only.
var workspaceImportLinkify = extension.NewLinkify(
	extension.WithLinkifyAllowedProtocols([]string{"http:", "https:", "mailto:", "HTTP:", "HTTPS:", "MAILTO:", "Http:", "Https:", "Mailto:"}),
	extension.WithLinkifyURLRegexp(regexp.MustCompile(`(?i)^(?:https?://[-a-z0-9@:%._+~#=]{1,256}(?:[/#?][-a-z0-9@:%_+.~#$!?&/=();,'">^{}\[\]`+"`"+`]*)?|mailto:[a-z0-9.!#$%&'*+/=?^_`+"`"+`{|}~-]+@[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+)`)),
	extension.WithLinkifyWWWRegexp(regexp.MustCompile(`^www\.[^\s\S]`)),
)

// workspaceImportAutoLinkAddress is a bare web or mail address as Notion's
// Markdown import links it: its scheme and host in lower case.
func workspaceImportAutoLinkAddress(address string) string {
	scheme, rest, found := strings.Cut(address, ":")
	if !found {
		return address
	}
	scheme = strings.ToLower(scheme)
	host, hasHost := strings.CutPrefix(rest, "//")
	if !hasHost {
		return scheme + ":" + rest
	}
	end := strings.IndexAny(host, "/?#")
	if end < 0 {
		end = len(host)
	}
	return scheme + "://" + strings.ToLower(host[:end]) + host[end:]
}

type workspaceImportMarkdownReader struct {
	source []byte
	list   workspaceImportBlockList
	// locale names what the import names itself, a database's columns
	// (workspace_import_markdown_database.go).
	locale string
	// placed holds the sub-pages a document has put in place, through all
	// its parts: a sub-page named again is a mention.
	placed map[string]bool
	// named holds the sub-pages its words name inline
	// (workspace_import_field_pages.go).
	named map[string]bool
	// inlineDatabases holds the sub-pages put in place that are databases
	// to turn inline there (workspace_import_inline_databases.go).
	inlineDatabases map[string]bool
	// liftFrom is the item's or quote's first block whose words are being
	// read, and lifted the math blocks taken out of them (openingWithMath).
	liftFrom ast.Node
	lifted   *[]string
	// liftAll lifts every math block out of those words, words before it
	// or none: a list item's later paragraph's (itemLaterParts).
	liftAll bool
}

func (reader *workspaceImportMarkdownReader) children(parent ast.Node, indent int) bool {
	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		if !reader.block(node, indent) {
			return false
		}
	}
	return true
}

func (reader *workspaceImportMarkdownReader) block(node ast.Node, indent int) bool {
	empty := json.RawMessage(`{}`)
	switch node := node.(type) {
	case *ast.Heading:
		kinds := []string{graphBlockTypeHeading1, graphBlockTypeHeading2, graphBlockTypeHeading3, graphBlockTypeHeading4}
		color := reader.takeColorMark(node)
		plain, spans := reader.inline(node)
		return reader.list.add(kinds[min(max(node.Level, 1), len(kinds))-1], plain, spans, withNotionMarkdownBlockColor(empty, color), indent)
	case *ast.Paragraph, *ast.TextBlock:
		if held, fits := reader.displayMathParagraph(node, indent); held {
			return fits
		}
		if captioned, fits := reader.caption(node, indent); captioned {
			return fits
		}
		if kind, content, linked := reader.webBlock(node); linked {
			return reader.list.add(kind, "", nil, content, indent)
		}
		if reader.pageLine(node, indent) {
			return true
		}
		if kind, content, media := reader.media(node); media {
			return reader.list.add(kind, "", nil, content, indent)
		}
		color := reader.takeColorMark(node)
		// A paragraph holding only the spaces mark is an empty one, as a
		// converted export marks its empty paragraph's line.
		blank := workspaceImportOnlyMark(node, workspaceImportSpacesMark)
		plain, spans := reader.inline(node)
		if strings.TrimSpace(plain) == "" && !blank {
			return true
		}
		if blank {
			plain, spans = "", nil
		}
		return reader.list.add(graphBlockTypeParagraph, plain, spans, withNotionMarkdownBlockColor(empty, color), indent)
	case *ast.List:
		for item := node.FirstChild(); item != nil; item = item.NextSibling() {
			if !reader.listItem(item, node.IsOrdered(), indent) {
				return false
			}
		}
		return true
	case *ast.Blockquote:
		// A quote's first block's words are its words, and its color mark its
		// color; what follows is inside it.
		color := ""
		if first, ok := node.FirstChild().(*ast.Paragraph); ok {
			color = reader.takeColorMark(first)
		}
		var plain string
		var spans []graphTextSpan
		var rest ast.Node
		lifted := reader.openingWithMath(node.FirstChild(), func() { plain, spans, rest = reader.openingWords(node.FirstChild(), true) })
		var column ast.Node
		if table, ok := node.FirstChild().(*extensionast.Table); ok {
			if plain, spans, rest = reader.tableOpening(table); workspaceImportColumnTable(table) {
				column = table
			}
		}
		if !reader.list.add(graphBlockTypeQuote, plain, spans, withNotionMarkdownBlockColor(empty, color), indent) ||
			!reader.liftedEquations(lifted, indent+1) {
			return false
		}
		if rest, column = reader.columnAfterWords(rest, column); column != nil && !reader.joinColumnTable(column, indent) {
			return false
		}
		for ; rest != nil; rest = rest.NextSibling() {
			if !reader.block(rest, indent+1) {
				return false
			}
		}
		return true
	case *ast.ThematicBreak:
		return reader.list.add(graphBlockTypeDivider, "", nil, empty, indent)
	case *ast.FencedCodeBlock:
		info := ""
		if node.Info != nil {
			info = string(node.Info.Segment.Value(reader.source))
		}
		content, _ := json.Marshal(map[string]string{"language": importedCodeLanguage(info)})
		return reader.list.add(graphBlockTypeCode, reader.lines(node), nil, content, indent)
	case *ast.CodeBlock:
		content, _ := json.Marshal(map[string]string{"language": ""})
		return reader.list.add(graphBlockTypeCode, reader.lines(node), nil, content, indent)
	case *ast.HTMLBlock:
		// HTML is kept as the text it was written as.
		raw := reader.lines(node)
		if node.HasClosure() {
			raw += string(node.ClosureLine.Value(reader.source))
		}
		if raw = strings.TrimSpace(raw); raw == "" {
			return true
		}
		return reader.list.add(graphBlockTypeParagraph, raw, nil, empty, indent)
	case *extensionast.Table:
		return reader.table(node, indent)
	case *workspaceImportAside:
		return reader.aside(node, indent)
	}
	return reader.children(node, indent)
}

func (reader *workspaceImportMarkdownReader) lines(node ast.Node) string {
	return strings.TrimSuffix(string(node.Lines().Value(reader.source)), "\n")
}

// media reads a paragraph holding only an image (a web address or an
// uploaded file) or an uploaded file's link into its block.
func (reader *workspaceImportMarkdownReader) media(paragraph ast.Node) (string, json.RawMessage, bool) {
	only := paragraph.FirstChild()
	if only == nil || only.NextSibling() != nil {
		return "", nil, false
	}
	label := strings.TrimSpace(reader.plain(only))
	switch node := only.(type) {
	case *ast.Image:
		destination := string(node.Destination)
		if fileID, uploaded := strings.CutPrefix(destination, workspaceImportFileReference); uploaded {
			if !workspaceImportReferenceIDPattern.MatchString(fileID) {
				return "", nil, false
			}
			if label == "" {
				label = "image"
			}
			return graphBlockTypeImage, workspaceImportManagedImageContent(fileID, label, strings.TrimSpace(string(node.Title))), true
		}
		content, image := workspaceImportImageLine(destination)
		return graphBlockTypeImage, content, image
	case *ast.Link:
		fileID, uploaded := strings.CutPrefix(string(node.Destination), workspaceImportFileReference)
		if !uploaded || !workspaceImportReferenceIDPattern.MatchString(fileID) {
			return "", nil, false
		}
		if label == "" {
			label = "file"
		}
		return graphBlockTypeFile, workspaceImportManagedFileContent(fileID, label), true
	}
	return "", nil, false
}

// table is a GitHub table's block, with a header row unless its mark names
// its headers, and its rows; one of a single column without a mark is its
// cells' blocks (columnTable), and one its mark makes a database that
// database (workspace_import_markdown_database.go).
func (reader *workspaceImportMarkdownReader) table(table *extensionast.Table, indent int) bool {
	marks := workspaceImportTableHeaders(table)
	rows := [][]ast.Node{}
	width := 0
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		cells := []ast.Node{}
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, cell)
		}
		rows = append(rows, cells)
		width = max(width, len(cells))
	}
	if width <= 1 && !marks.marked {
		return reader.columnTable(rows, indent)
	}
	if marks.database && width > 1 {
		return reader.database(rows, marks, width, indent)
	}
	// Every column and every cell's text, as Notion reads a Markdown table;
	// a table past what a row can hold refuses the import.
	width = max(width, minGraphTableWidth)
	if width > maxGraphTableWidth {
		return false
	}
	tableContent, _ := json.Marshal(graphTableBlockContent{TableWidth: width, HasColumnHeader: marks.row, HasRowHeader: marks.column})
	reader.list.blocks = append(reader.list.blocks, workspaceImportParsedBlock{Type: graphBlockTypeTable, Content: tableContent, Indent: indent})
	for _, row := range rows {
		cells := make([][]graphTextSpan, width)
		for column := range cells {
			cells[column] = []graphTextSpan{}
			if column >= len(row) {
				continue
			}
			plain, spans := reader.inline(row[column])
			if plain == "" {
				continue
			}
			if spans == nil {
				spans = []graphTextSpan{{Text: plain}}
			}
			cells[column] = spans
		}
		rowContent, _ := json.Marshal(graphTableRowBlockContent{Cells: cells})
		reader.list.blocks = append(reader.list.blocks, workspaceImportParsedBlock{Type: graphBlockTypeTableRow, Content: rowContent, Indent: indent + 1})
	}
	return true
}

// inline reads a block's inline nodes into its plain text and spans; spans
// are nil when the text carries no formatting. The spaces and line breaks
// at the ends of a block's words are not its words, as Notion's Markdown
// import reads a block (seen 2026-11-13: "[](address) words", "&#32;words",
// a line under an empty link and "words&#32;" all lose them, a line's own
// spaces inside the words stay), unless its words open with the spaces mark
// of a converted document (takeSpacesMark).
func (reader *workspaceImportMarkdownReader) inline(parent ast.Node) (string, []graphTextSpan) {
	kept := reader.takeSpacesMark(parent)
	spans := workspaceImportInlineParser{}
	reader.inlineChildren(&spans, parent, workspaceImportInlineStyle{})
	spans.flush()
	if !kept {
		spans.spans = workspaceImportTrimSpans(spans.spans)
	}
	return workspaceImportSpansText(spans.spans)
}

func (reader *workspaceImportMarkdownReader) inlineChildren(spans *workspaceImportInlineParser, parent ast.Node, style workspaceImportInlineStyle) {
	for node := parent.FirstChild(); node != nil; node = node.NextSibling() {
		switch node := node.(type) {
		case *ast.Text:
			spans.emit(reader.text(node.Segment.Value(reader.source), node.IsRaw()), style)
			if node.SoftLineBreak() || node.HardLineBreak() {
				spans.emit("\n", style)
			}
		case *ast.String:
			spans.emit(reader.text(node.Value, node.IsRaw() || node.IsCode()), style)
		case *ast.CodeSpan:
			code := style
			code.code = true
			spans.emit(reader.plain(node), code)
		case *ast.Emphasis:
			inner := style
			if node.Level >= 2 {
				inner.bold = true
			} else {
				inner.italic = true
			}
			reader.inlineChildren(spans, node, inner)
		case *extensionast.Strikethrough:
			inner := style
			inner.strikethrough = true
			reader.inlineChildren(spans, node, inner)
		case *ast.Link:
			reader.link(spans, node, style)
		case *ast.AutoLink:
			// A mail address mails; a web address links as Notion's does.
			label := string(node.Label(reader.source))
			address := workspaceImportAutoLinkAddress(string(node.URL(reader.source)))
			if node.AutoLinkType == ast.AutoLinkEmail {
				address = "mailto:" + label
			}
			if target := workspaceImportLinkTarget(address); target != "" {
				linked := style
				linked.link = target
				spans.emit(label, linked)
			} else {
				spans.emit(label, style)
			}
		case *ast.Image:
			// An image inside text is kept as the text it was written as; a
			// text color mark styles the words after it.
			if marked, ok := workspaceImportTextStyle(node, style); ok {
				style = marked
				continue
			}
			spans.emit("!["+reader.plain(node)+"]("+string(node.Destination)+")", style)
		case *ast.RawHTML:
			for index := range node.Segments.Len() {
				segment := node.Segments.At(index)
				spans.emit(string(segment.Value(reader.source)), style)
			}
		case *extensionast.TaskCheckBox:
		case *workspaceImportMath:
			reader.inlineMath(spans, parent, node, style)
		default:
			reader.inlineChildren(spans, node, style)
		}
	}
}

// link is a page or data source mention for a reference, a link for a web, mail or
// phone address or one of the app's own (workspaceImportMarkdownLinkTarget),
// and otherwise the text it was written as.
func (reader *workspaceImportMarkdownReader) link(spans *workspaceImportInlineParser, link *ast.Link, style workspaceImportInlineStyle) {
	destination := string(link.Destination)
	pageID, page := strings.CutPrefix(strings.TrimSpace(destination), workspaceImportPageReference)
	if !page {
		pageID, page = strings.CutPrefix(strings.TrimSpace(destination), workspaceImportSubPageReference)
		if page && reader.named != nil && workspaceImportReferenceIDPattern.MatchString(pageID) {
			reader.named[pageID] = true
		}
	}
	if page && workspaceImportReferenceIDPattern.MatchString(pageID) {
		label := reader.plain(link)
		if label == "" {
			label = pageID
		}
		spans.flush()
		spans.spans = append(spans.spans, graphTextSpan{Text: label, Mention: "page", PageID: pageID})
		return
	}
	// A data source's (workspace_import_data_source_references.go).
	if mention, referenced := workspaceImportReference(destination); referenced && mention.Mention == "database" {
		spans.flush()
		spans.spans = append(spans.spans, workspaceImportReferenceSpan(mention, reader.plain(link), style))
		return
	}
	if target := workspaceImportMarkdownLinkTarget(destination); target != "" {
		linked := style
		linked.link = target
		reader.inlineChildren(spans, link, linked)
		return
	}
	spans.emit("[", style)
	reader.inlineChildren(spans, link, style)
	written := "](" + destination
	if len(link.Title) > 0 {
		written += ` "` + string(link.Title) + `"`
	}
	spans.emit(written+")", style)
}

// workspaceImportMarkdownLinkTarget is a [text](address) link's address: a
// web, mail or phone address, or one of the app's own as the editor writes
// it and opens it — a page (/p/ID), a block on one (/p/ID#block-ID) or a
// block on the page it stands on (#block-ID) — kept as written, as Notion's
// Markdown keeps a path in the app and an anchor
// (notionMarkdownSchemelessLinkTarget), and a web address without its
// scheme (//host/path) as written, as Notion's Markdown and HTML import
// link one (seen 2026-11-13).
func workspaceImportMarkdownLinkTarget(raw string) string {
	if target := workspaceImportLinkTarget(raw); target != "" {
		return target
	}
	target := strings.TrimSpace(raw)
	if (strings.HasPrefix(target, "#") || strings.HasPrefix(target, "/")) && validGraphTextLink(target) {
		return target
	}
	return ""
}

// plain is the text of an inline node's children, without formatting.
func (reader *workspaceImportMarkdownReader) plain(node ast.Node) string {
	collected := workspaceImportInlineParser{}
	reader.inlineChildren(&collected, node, workspaceImportInlineStyle{})
	collected.flush()
	plain := strings.Builder{}
	for _, span := range collected.spans {
		plain.WriteString(span.Text)
	}
	return plain.String()
}

// text resolves backslash escapes and character references in one pass, as
// CommonMark reads them, except in code: an escaped character is itself and
// starts nothing, so "\&amp;" is the text "&amp;" (2026-11-13: resolving the
// escapes first made it "&", where the converters escape the "&" of text
// that reads "&amp;").
func (reader *workspaceImportMarkdownReader) text(value []byte, raw bool) string {
	if raw {
		return string(value)
	}
	references := func(part []byte) []byte { return util.ResolveEntityNames(util.ResolveNumericReferences(part)) }
	resolved := make([]byte, 0, len(value))
	start := 0
	for index := 0; index+1 < len(value); index++ {
		if value[index] == '\\' && util.IsPunct(value[index+1]) {
			resolved = append(append(resolved, references(value[start:index])...), value[index+1])
			index++
			start = index + 1
		}
	}
	return string(append(resolved, references(value[start:])...))
}

// workspaceImportTrimSpans drops the spaces and line breaks at the ends of a
// block's words — a no-break and an ideographic space too, as Notion's
// Markdown import drops them; a page mention or an equation ends the dropping.
func workspaceImportTrimSpans(spans []graphTextSpan) []graphTextSpan {
	for len(spans) > 0 && spans[0].Mention == "" && spans[0].Equation == "" {
		if spans[0].Text = strings.TrimLeftFunc(spans[0].Text, unicode.IsSpace); spans[0].Text != "" {
			break
		}
		spans = spans[1:]
	}
	for len(spans) > 0 && spans[len(spans)-1].Mention == "" && spans[len(spans)-1].Equation == "" {
		last := &spans[len(spans)-1]
		if last.Text = strings.TrimRightFunc(last.Text, unicode.IsSpace); last.Text != "" {
			break
		}
		spans = spans[:len(spans)-1]
	}
	return spans
}

// workspaceImportTrimMarkSpace drops the one space a mark leaves before the
// words it marks; the words' own leading spaces stay where the spaces mark
// keeps them, as Notion's API import keeps them (a task box's space goldmark
// takes with the box).
func workspaceImportTrimMarkSpace(plain string, spans []graphTextSpan) (string, []graphTextSpan) {
	plain = strings.TrimPrefix(plain, " ")
	if len(spans) > 0 {
		spans[0].Text = strings.TrimPrefix(spans[0].Text, " ")
		if spans[0].Text == "" {
			spans = spans[1:]
		}
	}
	if len(spans) == 0 {
		return plain, nil
	}
	return plain, spans
}
