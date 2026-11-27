package app

import (
	"strings"

	"github.com/yuin/goldmark/ast"
)

// pageLine reads a paragraph holding only a link to a page of the import
// into its block. An exported page names its sub-pages in its body, each
// alone on a line: the sub-page's block stands there (2026-11-05: a Leafen
// export brought back had its sub-pages first and then again as mentions),
// the first time it is named. A line after a page link's mark, an export's
// link to a page (figure.link-to-page, htmlImportLayout.ts), is a link to
// that page once its sub-page's block is in place or for any other page of
// the import, as Notion's API import keeps a second link to a sub-page
// (2026-11-14: it came as a paragraph naming the page). Any other line is
// read as words, the mark taken out. A sub-page's line marked inline stands
// for a database its page holds inline (htmlImportDatabases.ts): its block
// is put in place, and the import turns it inline there
// (workspace_import_inline_databases.go).
func (reader *workspaceImportMarkdownReader) pageLine(paragraph ast.Node, indent int) bool {
	kind, marked := takeWorkspaceImportMark(paragraph, workspaceImportPageLinkMark)
	var link *ast.Link
	for node := paragraph.FirstChild(); node != nil; node = node.NextSibling() {
		if text, blank := node.(*ast.Text); blank && strings.TrimSpace(string(text.Segment.Value(reader.source))) == "" &&
			!text.SoftLineBreak() && !text.HardLineBreak() {
			continue
		}
		found, ok := node.(*ast.Link)
		if !ok || link != nil {
			return false
		}
		link = found
	}
	if link == nil {
		return false
	}
	destination := strings.TrimSpace(string(link.Destination))
	pageID, sub := strings.CutPrefix(destination, workspaceImportSubPageReference)
	if sub && workspaceImportReferenceIDPattern.MatchString(pageID) && !reader.placed[pageID] {
		if reader.placed == nil {
			reader.placed = map[string]bool{}
		}
		reader.placed[pageID] = true
		if kind == workspaceImportPageLinkInline && reader.inlineDatabases != nil {
			reader.inlineDatabases[pageID] = true
		}
		reader.list.blocks = append(reader.list.blocks, workspaceImportParsedBlock{
			ID: pageID, Type: graphBlockTypeChildPage, Content: childPageBlockContent(pageID), Indent: indent,
		})
		return true
	}
	if !sub {
		pageID, sub = strings.CutPrefix(destination, workspaceImportPageReference)
	}
	if !marked || !sub || !workspaceImportReferenceIDPattern.MatchString(pageID) {
		return false
	}
	reader.list.blocks = append(reader.list.blocks, workspaceImportParsedBlock{
		Type: graphBlockTypeLinkToPage, Content: childPageBlockContent(pageID), Indent: indent,
	})
	return true
}
