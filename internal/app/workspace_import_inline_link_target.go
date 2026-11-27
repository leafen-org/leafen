package app

// readLinkTarget is the target a [words](address) link of the parser's
// Markdown links to — "v1"'s as written (version1Link), any other's as
// Notion's Markdown reads it (linkTarget) — "" for none; or, for a link a
// comment's reader refuses, the piece that refuses it (dataSourceLink).
func (parser *workspaceImportInlineParser) readLinkTarget(address string) (string, *graphTextSpan) {
	if refused, ok := parser.sitePages.dataSourceLink(address); ok {
		return "", &refused
	}
	if parser.version1 {
		return parser.version1Link(address), nil
	}
	return parser.linkTarget(address), nil
}
