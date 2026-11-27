package app

import (
	"cmp"
	"regexp"
	"strings"
)

// An export's CSV writes a page a cell mentions as its name and its file's
// path in parentheses (Notion's, seen 2026-11-13: "행 둘 행제목쪽 (…/행제목쪽
// <id>.html) "), and names the page of a row whose title mentions one with
// "@" and the name ("행 둘 @행제목쪽 <id>.html"). The web turns a cell's
// mention of a page the import brings into [name](leafen-page:<page ID>):
// a title or text cell takes it as the page's mention, and the row its page
// is is found by its title so named. A cell's data source mention comes so
// too (workspace_import_data_source_references.go).

var workspaceImportCSVMentionPattern = regexp.MustCompile(
	`\[((?:\\.|[^\]\\])*)\]\(((?:` + workspaceImportPageReference + `|` + workspaceImportDataSourceReference + `)[A-Za-z0-9_-]{1,64}|` +
		workspaceImportMissingDataSourceReference + `[0-9a-fA-F-]{32,36})\)`)

var workspaceImportCSVEscapePattern = regexp.MustCompile(`\\(.)`)

// workspaceImportCSVMentions reads a title or text cell holding page
// mentions into its words and spans; found is false for a cell with none.
func workspaceImportCSVMentions(value string) (string, []graphTextSpan, bool) {
	matches := workspaceImportCSVMentionPattern.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return value, nil, false
	}
	spans := []graphTextSpan{}
	plain := strings.Builder{}
	at := 0
	for _, match := range matches {
		if before := value[at:match[0]]; before != "" {
			spans = append(spans, graphTextSpan{Text: before})
			plain.WriteString(before)
		}
		// A page's mention, or a data source's (workspace_import_data_source_references.go).
		mention, _ := workspaceImportReference(value[match[4]:match[5]])
		label := workspaceImportCSVEscapePattern.ReplaceAllString(value[match[2]:match[3]], "$1")
		if mention.Text == "" {
			mention.Text = cmp.Or(label, mention.PageID, notionUnresolvedMentionTitle)
		}
		spans = append(spans, mention)
		plain.WriteString(mention.Text)
		at = match[1]
	}
	if rest := value[at:]; rest != "" {
		spans = append(spans, graphTextSpan{Text: rest})
		plain.WriteString(rest)
	}
	return plain.String(), spans, true
}

// workspaceImportCSVTitleName is a title cell as an export names its row's
// page: each mention "@" and the name, its ends trimmed.
func workspaceImportCSVTitleName(value string) string {
	plain, spans, found := workspaceImportCSVMentions(value)
	if !found {
		return strings.TrimSpace(plain)
	}
	return strings.TrimSpace(workspaceImportExportedName(spans))
}

// workspaceImportCSVTextValue is a title or text cell with page mentions as
// its cell holds it: its words, a title's ends trimmed, and the spans that
// spell them, none once the words are cut.
func workspaceImportCSVTextValue(valueType, value string) (string, []graphTextSpan, bool) {
	plain, spans, found := workspaceImportCSVMentions(value)
	if !found {
		return value, nil, false
	}
	if valueType == graphValueTypeTitle {
		plain, spans = strings.TrimSpace(plain), trimGraphTextSpans(spans)
	}
	bound := maxGraphTextCharacters
	if valueType == graphValueTypeTitle {
		bound = maxGraphTitleRunes
	}
	if within := graphTextWithin(plain, bound); within != plain {
		return within, nil, true
	}
	return plain, spans, true
}
