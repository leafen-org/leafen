package app

import (
	"cmp"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's export writes a data source's mention as a link to the source in
// Notion's app, https://app.notion.com/ds/<space>/<source>?db=<database>
// (seen 2026-11-22: its Markdown & CSV export in a page's lines and a text
// cell, written "name (address)", its HTML export there, in a title and in a
// comment too; its Markdown export names a title's "@" and the name). The
// web turns a link to the source of a database the import brings into
// [name](leafen-data-source:<the database page's ID>) and one to any other
// into [name](leafen-missing-data-source:<the source's Notion ID>)
// (importDataSourceMentions.ts). Read here, the first is the mention of the
// data source the import makes for that database: a batch's database takes
// the ID its reserved page names (workspaceImportBatchDataSourceID), so a
// document going in before it mentions it all the same, as a mention of what
// is not there until it is made; the second is the mention of a data source
// not there by the Notion ID (keepUnresolvedDataSourceMention). Each keeps
// the words around it as a page mention does, and its marks.

const (
	workspaceImportDataSourceReference        = "leafen-data-source:"
	workspaceImportMissingDataSourceReference = "leafen-missing-data-source:"
)

// workspaceImportBatchDataSourceID is the ID of the data source a batch's
// database takes, by the ID of the page the batch reserved for it.
func workspaceImportBatchDataSourceID(pageID string) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	digest := sha256.Sum256([]byte("leafen-import-batch-data-source\x00" + pageID))
	id := make([]byte, 15)
	for index := range id {
		id[index] = alphabet[int(digest[index])%len(alphabet)]
	}
	return string(id)
}

// workspaceImportReservedDataSourceID is the ID a CSV's data source takes:
// its batch page's (workspaceImportBatchDataSourceID), unless a data source
// has it already, as one a stopped run of the same document made; "" for a
// new one of its own.
func workspaceImportReservedDataSourceID(app core.App, admission workspaceImportAdmission) (string, error) {
	if admission.ReservedRootPageID == "" || admission.Input.Format != workspaceImportFormatCSV {
		return "", nil
	}
	id, taken := workspaceImportBatchDataSourceID(admission.ReservedRootPageID), ""
	err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": id}).Row(&taken)
	if errors.Is(err, sql.ErrNoRows) {
		return id, nil
	}
	return "", err
}

// workspaceImportReference is the mention an imported document's reference
// names, its words left for its reader; false for an address that is none.
func workspaceImportReference(address string) (graphTextSpan, bool) {
	address = strings.TrimSpace(address)
	if pageID, page := strings.CutPrefix(address, workspaceImportPageReference); page {
		return graphTextSpan{Mention: "page", PageID: pageID}, workspaceImportReferenceIDPattern.MatchString(pageID)
	}
	if pageID, page := strings.CutPrefix(address, workspaceImportSubPageReference); page {
		return graphTextSpan{Mention: "page", PageID: pageID}, workspaceImportReferenceIDPattern.MatchString(pageID)
	}
	if pageID, source := strings.CutPrefix(address, workspaceImportDataSourceReference); source {
		return graphTextSpan{
			Mention: "database", DatabaseID: notionMarkdownDataSourceAddress + workspaceImportBatchDataSourceID(pageID),
		}, workspaceImportReferenceIDPattern.MatchString(pageID)
	}
	if written, missing := strings.CutPrefix(address, workspaceImportMissingDataSourceReference); missing {
		span := graphTextSpan{}
		keepUnresolvedDataSourceMention(&span, written)
		return span, span.Mention != ""
	}
	return graphTextSpan{}, false
}

// referenceWords are a reference's words as a line reads them.
func (parser *workspaceImportInlineParser) referenceWords(text string, depth int) string {
	words := workspaceImportInlineParser{}
	words.parse(text, workspaceImportInlineStyle{}, depth+1)
	written := ""
	for _, span := range words.spans {
		written += span.Text
	}
	return written
}

// workspaceImportReferenceSpan is a reference's mention with its words: a
// page's, or its ID for none; a data source's with its marks too, one not
// there "Untitled".
func workspaceImportReferenceSpan(mention graphTextSpan, words string, style workspaceImportInlineStyle) graphTextSpan {
	if mention.Mention == "page" {
		mention.Text = cmp.Or(words, mention.PageID)
		return mention
	}
	if mention.Text == "" {
		mention.Text = cmp.Or(words, notionUnresolvedMentionTitle)
	}
	mention.Bold, mention.Italic, mention.Strikethrough = style.bold, style.italic, style.strikethrough
	mention.Underline, mention.Color, mention.Background = style.underline, style.color, style.background
	return mention
}
