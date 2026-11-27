package app

import (
	"encoding/json"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	extensionast "github.com/yuin/goldmark/extension/ast"
)

// What Markdown has no form for, a document the web converts (an HTML or
// Word file, importDocumentConverters.ts) marks with a link with no words to
// an address of its own, which the reader takes out:
//
//   - a bulleted item opening with [](leafen-toggle:) is a toggle, and with
//     [](leafen-toggle:<1–4>) a toggle heading of that level; what follows
//     in the item is inside it, as in the toggle;
//   - a bulleted item opening with [](leafen-callout:<color>,<icon>) is a
//     callout of that color and icon (workspaceImportCalloutContent), its
//     words its first line and what follows in the item its other lines, as
//     Notion's API import makes one (notionImportCalloutFirstLine) — one
//     empty line when it holds nothing;
//   - a table whose first cell opens with [](leafen-table:<headers>) has the
//     headers it names, "header-row" and "header-column" between commas, and
//     none when it names none, and stays a table of one column; a table
//     without the mark has a header row, as Notion's Markdown import reads
//     one, and is its cells' blocks when it has one column (columnTable);
//     with "database" and "title=<column>" among them it is an inline
//     database (workspace_import_markdown_database.go);
//   - a paragraph opening with [](leafen-block:<kind>) and holding a link
//     alone is a block of that kind at the link's web address: a bookmark,
//     an embed, a link preview, or an outside image, video, audio, PDF or file, a file named
//     by the link's words, else by its address's last part, as Notion's API
//     import names an outside file without a name (notion_import_file_fetch.go),
//     and [](leafen-block:table_of_contents), with a color mark after it or
//     none and nothing else, is a table of contents of that color, as an
//     export draws one and Notion's API import keeps it;
//   - a paragraph opening with [](leafen-caption:) right after a code block,
//     an image, a file or such a block at its depth is that block's caption;
//     elsewhere it is the paragraph of its words;
//   - a heading's, a paragraph's, a list item's, a toggle's or a quote's
//     words opening with [](leafen-color:<color>), after a toggle's mark,
//     are a block of that color, a block color's name, as an export colors
//     the block (htmlImportColors.ts) and Notion's API import keeps it;
//   - a block's words opening with [](leafen-spaces:), after any other mark,
//     keep the spaces and line breaks at their ends, as a document whose
//     style keeps its spaces lays them out (Notion's HTML export, which
//     Notion's API import keeps), where Markdown's lose them; a paragraph
//     of that mark alone is an empty paragraph, an export's empty line;
//   - a picture with no words at ![](leafen-text-color:<color>,<background>)
//     gives the words after it, up to the next one, that text color and
//     background, each a span's own or none, and an underline too with
//     ",underline" after them — a picture, which Markdown keeps inside a
//     link's words, where a link it does not;
//   - a document's first line [](leafen-page-head:<icon>,<cover>,<place>) gives
//     its page the icon and cover an export's header names
//     (workspace_import_page_head.go);
//   - a bulleted item opening with [](leafen-columns:) is a column list, its
//     items opening with [](leafen-column:<width>) its columns, each its
//     share of the row in percent or none, what follows in each inside it,
//     and one opening with [](leafen-paragraph:) a paragraph, what follows
//     in it inside it, as an export lays them out (htmlImportLayout.ts,
//     workspace_import_markdown_columns.go);
//   - a paragraph opening with [](leafen-page-link:) and holding a link to a
//     page of the import alone is a link to that page, the first link to a
//     sub-page its sub-page's block (workspace_import_markdown_page_lines.go),
//     and with [](leafen-page-link:inline) that sub-page is a database the
//     page holds inline there (workspace_import_inline_databases.go).
const (
	workspaceImportToggleMark    = "leafen-toggle:"
	workspaceImportCalloutMark   = "leafen-callout:"
	workspaceImportTableMark     = "leafen-table:"
	workspaceImportBlockMark     = "leafen-block:"
	workspaceImportCaptionMark   = "leafen-caption:"
	workspaceImportColorMark     = "leafen-color:"
	workspaceImportSpacesMark    = "leafen-spaces:"
	workspaceImportTextColorMark = "leafen-text-color:"
	workspaceImportPageMark      = "leafen-page-head:"
	workspaceImportColumnsMark   = "leafen-columns:"
	workspaceImportColumnMark    = "leafen-column:"
	workspaceImportParagraphMark = "leafen-paragraph:"
	workspaceImportPageLinkMark  = "leafen-page-link:"
	// workspaceImportPageLinkInline is a page link mark's for an inline database.
	workspaceImportPageLinkInline = "inline"
)

// workspaceImportWebBlockKinds are the blocks a block mark can make.
var workspaceImportWebBlockKinds = map[string]bool{
	graphBlockTypeBookmark: true, graphBlockTypeEmbed: true, graphBlockTypeImage: true, graphBlockTypeVideo: true,
	graphBlockTypeAudio: true, graphBlockTypePDF: true, graphBlockTypeFile: true, graphBlockTypeLinkPreview: true,
}

var workspaceImportToggleKinds = map[string]string{
	"": graphBlockTypeToggle, "1": graphBlockTypeToggleHeading1, "2": graphBlockTypeToggleHeading2,
	"3": graphBlockTypeToggleHeading3, "4": graphBlockTypeToggleHeading4,
}

// workspaceImportCaptionedKinds are the blocks a caption line can belong to.
var workspaceImportCaptionedKinds = map[string]bool{
	graphBlockTypeCode: true, graphBlockTypeImage: true, graphBlockTypeFile: true, graphBlockTypePDF: true,
	graphBlockTypeVideo: true, graphBlockTypeAudio: true, graphBlockTypeBookmark: true, graphBlockTypeEmbed: true,
}

// takeWorkspaceImportMark takes a mark of the kind out of the start of a
// paragraph or a cell, and gives what its address names after the kind.
func takeWorkspaceImportMark(parent ast.Node, kind string) (string, bool) {
	if parent == nil {
		return "", false
	}
	link, ok := parent.FirstChild().(*ast.Link)
	if !ok || link.HasChildren() {
		return "", false
	}
	value, marked := strings.CutPrefix(string(link.Destination), kind)
	if !marked {
		return "", false
	}
	parent.RemoveChild(parent, link)
	return value, true
}

// takeWordsMark takes a mark of the kind out of the start of a block's
// words, past a task box and the spaces another mark leaves before it, and
// gives what its address names after the kind.
func (reader *workspaceImportMarkdownReader) takeWordsMark(parent ast.Node, kind string) (string, bool) {
	node := parent.FirstChild()
	for node != nil {
		if text, ok := node.(*ast.Text); ok && strings.TrimSpace(string(text.Segment.Value(reader.source))) == "" &&
			!text.SoftLineBreak() && !text.HardLineBreak() {
			node = node.NextSibling()
		} else if _, box := node.(*extensionast.TaskCheckBox); box {
			node = node.NextSibling()
		} else {
			break
		}
	}
	link, ok := node.(*ast.Link)
	if !ok || link.HasChildren() {
		return "", false
	}
	value, marked := strings.CutPrefix(string(link.Destination), kind)
	if marked {
		parent.RemoveChild(parent, link)
	}
	return value, marked
}

// workspaceImportOnlyMark reports a block whose words are only a mark of
// kind, a link with no words to its address.
func workspaceImportOnlyMark(parent ast.Node, kind string) bool {
	link, ok := parent.FirstChild().(*ast.Link)
	return ok && parent.ChildCount() == 1 && !link.HasChildren() && strings.HasPrefix(string(link.Destination), kind)
}

// takeSpacesMark takes the spaces mark out of the start of a block's words.
func (reader *workspaceImportMarkdownReader) takeSpacesMark(parent ast.Node) bool {
	_, marked := reader.takeWordsMark(parent, workspaceImportSpacesMark)
	return marked
}

// takeColorMark takes the color mark out of the start of a block's words and
// gives the color as the block keeps it, "" for none or one Notion hasn't.
func (reader *workspaceImportMarkdownReader) takeColorMark(parent ast.Node) string {
	color, _ := reader.takeWordsMark(parent, workspaceImportColorMark)
	if !validNotionBlockColor(color) || color == "default" {
		return ""
	}
	return color
}

// workspaceImportTextStyle is the style a text color mark gives the words
// after it: its text color and background, each "" for none or one a span
// can't have, and its underline.
func workspaceImportTextStyle(image *ast.Image, style workspaceImportInlineStyle) (workspaceImportInlineStyle, bool) {
	value, marked := strings.CutPrefix(string(image.Destination), workspaceImportTextColorMark)
	if !marked || image.HasChildren() {
		return style, false
	}
	color, rest, _ := strings.Cut(value, ",")
	background, look, _ := strings.Cut(rest, ",")
	if !validNotionBlockColor(color) || color == "default" || strings.HasSuffix(color, "_background") {
		color = ""
	}
	if !validNotionSpanBackground(background) || background == "default_background" {
		background = ""
	}
	style.color, style.background, style.underline = color, background, look == "underline"
	return style, true
}

// workspaceImportToggleKind is the toggle a bulleted item's mark makes it.
func workspaceImportToggleKind(paragraph ast.Node) (string, bool) {
	level, marked := takeWorkspaceImportMark(paragraph, workspaceImportToggleMark)
	if !marked {
		return "", false
	}
	kind, known := workspaceImportToggleKinds[level]
	if !known {
		kind = graphBlockTypeToggle
	}
	return kind, true
}

// workspaceImportCalloutContent is the callout a mark's address names, each
// part written as a URL's: its color, a block color's name ("default" for
// none or one Leafen hasn't), and its icon, as an export names one — an
// emoji, a built-in icon's /icons/name_color.svg, or a picture's web address,
// Notion's icon set's the built-in icon — the content Notion's API import
// gives a callout with that icon; none for any other or none named.
func workspaceImportCalloutContent(address string) json.RawMessage {
	colorPart, iconPart, _ := strings.Cut(address, ",")
	color, _ := url.PathUnescape(colorPart)
	icon, err := url.PathUnescape(iconPart)
	if err != nil {
		icon = ""
	}
	fields := map[string]any{"color": "default", "icon": "", "iconType": calloutIconNone}
	if color = strings.TrimSpace(color); validNotionBlockColor(color) {
		fields["color"] = color
	}
	switch value, kind := workspaceImportIconOf(icon); kind {
	case workspaceImportIconUploaded:
		// A picture the import uploaded is the callout's icon, as its
		// export kept it (the graph checks the workspace holds it).
		fields["icon"], fields["iconType"] = value, graphCalloutIconTypeImage
	case "":
	default:
		fields["icon"], fields["iconType"] = value, kind
	}
	content, _ := json.Marshal(fields)
	return content
}

// workspaceImportIconUploaded is the kind of an icon that is a picture the
// import uploaded.
const workspaceImportIconUploaded = "uploaded"

// workspaceImportIconOf reads an icon as an export names one — an emoji, a
// built-in icon's /icons/name_color.svg or Notion's address of it, a
// picture's web address, or a picture the import uploaded (leafen-file:) —
// as its value and kind: "emoji", nativeIconType, notionIconTypeExternal or
// workspaceImportIconUploaded; "" for none it reads.
func workspaceImportIconOf(icon string) (string, string) {
	icon = strings.TrimSpace(icon)
	stored, notion, err := nativeIconFromNotionURL(icon)
	file, uploaded := strings.CutPrefix(icon, workspaceImportFileReference)
	switch {
	case uploaded:
		if workspaceImportReferenceIDPattern.MatchString(file) {
			return file, workspaceImportIconUploaded
		}
	case notion && err == nil:
		return stored, nativeIconType
	case notion || icon == "":
	case notionPageExternalFile(icon) != nil:
		return icon, notionIconTypeExternal
	default:
		named := notionMarkdownCalloutIconContent(icon)
		if kind, _ := named["iconType"].(string); kind == "emoji" || kind == nativeIconType {
			value, _ := named["icon"].(string)
			return value, kind
		}
	}
	return "", ""
}

// callout reads a bulleted item a callout mark opens into the callout, its
// words its first line and the item's other blocks its other lines.
func (reader *workspaceImportMarkdownReader) callout(paragraph ast.Node, address string, indent int) bool {
	plain, spans := workspaceImportTrimMarkSpace(reader.inline(paragraph))
	if !reader.list.add(graphBlockTypeCallout, "", nil, workspaceImportCalloutContent(address), indent) {
		return false
	}
	rest := paragraph.NextSibling()
	if strings.TrimSpace(plain) != "" || rest == nil {
		if !reader.list.add(graphBlockTypeParagraph, plain, spans, json.RawMessage(`{}`), indent+1) {
			return false
		}
	}
	for ; rest != nil; rest = rest.NextSibling() {
		if !reader.block(rest, indent+1) {
			return false
		}
	}
	return true
}

// workspaceImportTableMarks are what a table's mark names: its headers, a
// header row when it has no mark, and, with "database", that it is an
// inline database whose title is the column "title=" names.
type workspaceImportTableMarks struct {
	row, column, marked, database bool
	title                         int
}

// workspaceImportTableHeaders takes a table's mark out of its first cell and
// gives what it names.
func workspaceImportTableHeaders(table ast.Node) workspaceImportTableMarks {
	header := table.FirstChild()
	if header == nil {
		return workspaceImportTableMarks{row: true}
	}
	value, marked := takeWorkspaceImportMark(header.FirstChild(), workspaceImportTableMark)
	if !marked {
		return workspaceImportTableMarks{row: true}
	}
	marks := workspaceImportTableMarks{marked: true}
	for _, name := range strings.Split(value, ",") {
		switch name = strings.TrimSpace(name); {
		case name == "header-row":
			marks.row = true
		case name == "header-column":
			marks.column = true
		case name == "database":
			marks.database = true
		case strings.HasPrefix(name, "title="):
			marks.title, _ = strconv.Atoi(strings.TrimPrefix(name, "title="))
		}
	}
	return marks
}

// caption reads a caption line into the caption of the block before it, or
// as the paragraph of its words when that block takes none.
func (reader *workspaceImportMarkdownReader) caption(paragraph ast.Node, indent int) (bool, bool) {
	if _, marked := takeWorkspaceImportMark(paragraph, workspaceImportCaptionMark); !marked {
		return false, true
	}
	plain, spans := workspaceImportTrimMarkSpace(reader.inline(paragraph))
	if strings.TrimSpace(plain) == "" {
		return true, true
	}
	if spans == nil {
		spans = []graphTextSpan{{Text: plain}}
	}
	if count := len(reader.list.blocks); count > 0 {
		block := &reader.list.blocks[count-1]
		fields := map[string]json.RawMessage{}
		if block.Indent == indent && workspaceImportCaptionedKinds[block.Type] && json.Unmarshal(block.Content, &fields) == nil {
			fields["caption"], _ = json.Marshal(spans)
			content, err := json.Marshal(fields)
			if err == nil {
				block.Content = content
				return true, true
			}
		}
	}
	return true, reader.list.add(graphBlockTypeParagraph, plain, spans, json.RawMessage(`{}`), indent)
}

// webBlock reads a paragraph a block mark opens into the block of its kind
// at the address of the link it holds; one that holds anything else is read
// as its words.
func (reader *workspaceImportMarkdownReader) webBlock(paragraph ast.Node) (string, json.RawMessage, bool) {
	kind, marked := takeWorkspaceImportMark(paragraph, workspaceImportBlockMark)
	if marked && kind == graphBlockTypeTableOfContents {
		return reader.tableOfContents(paragraph)
	}
	if !marked || !workspaceImportWebBlockKinds[kind] {
		return "", nil, false
	}
	var link *ast.Link
	for node := paragraph.FirstChild(); node != nil; node = node.NextSibling() {
		if text, ok := node.(*ast.Text); ok && strings.TrimSpace(string(text.Segment.Value(reader.source))) == "" {
			continue
		}
		found, ok := node.(*ast.Link)
		if !ok || link != nil {
			return "", nil, false
		}
		link = found
	}
	if link == nil || !validGraphExternalFileURL(string(link.Destination)) {
		return "", nil, false
	}
	address := strings.TrimSpace(string(link.Destination))
	fields := map[string]string{"url": address}
	if kind == graphBlockTypeFile {
		name := strings.TrimSpace(reader.plain(link))
		if parsed, err := url.Parse(address); name == "" && err == nil {
			name = path.Base(parsed.Path)
		}
		if name == "" || name == "." || name == "/" {
			name = "file"
		}
		fields["source"], fields["name"] = graphExternalFileSource, name
	}
	content, _ := json.Marshal(fields)
	return kind, content, true
}

// tableOfContents reads a paragraph a table of contents mark opens into the
// block, in the color its color mark names; one that holds words is read as
// its words.
func (reader *workspaceImportMarkdownReader) tableOfContents(paragraph ast.Node) (string, json.RawMessage, bool) {
	color := reader.takeColorMark(paragraph)
	for node := paragraph.FirstChild(); node != nil; node = node.NextSibling() {
		if text, ok := node.(*ast.Text); !ok || strings.TrimSpace(string(text.Segment.Value(reader.source))) != "" {
			return "", nil, false
		}
	}
	return graphBlockTypeTableOfContents, withNotionMarkdownBlockColor(json.RawMessage(`{}`), color), true
}
