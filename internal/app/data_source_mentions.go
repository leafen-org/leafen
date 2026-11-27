package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A data source's mention is the data source's own, as Notion's is (user
// decision 2026-11-22, in place of the one of 2026-09-29 that made it its
// database's): the app draws it by the source's icon, or the cylinder, and
// the source's name, "휴지통에 있음" after one in the trash, and a press opens
// its database at a view of the source (seen 2026-11-22 in Chrome at 1440: a
// full-page database's only source, an inline database's first of two and one
// sent to the trash, live and in the trash alike; a database's icon is not
// the source's); a title's path and window name say "@" and the source's
// name. Markdown reads it back by its address alone, the public API's rich
// text leaves it out and the MCP's fetch names nothing for it in a title,
// whether a source answers to it or not (notion_markdown_kept_data_sources.go).
//
// Leafen keeps it as the database mention named by the source's address,
// collection://<its own ID>, which a block's text, a copy and every reader
// carry as written; a reader's check of a mention takes it for the page
// holding the source's database (mcpDatabaseHolderPageID). One that names no
// source here keeps the ID written, which no source has. A database mention a
// data source's mention was made before stays the database's: what it was
// written as is not known.

// dataSourceMentionHere makes a span the mention of a data source of the
// workspace by its own ID, its words the source's name now.
func dataSourceMentionHere(app core.App, span *graphTextSpan, dataSourceID string) error {
	name, err := mentionedDataSourceName(app, dataSourceID)
	if err != nil {
		return err
	}
	span.Mention, span.PageID, span.BlockID = "database", "", ""
	span.DatabaseID, span.Text = notionMarkdownDataSourceAddress+dataSourceID, name
	return nil
}

// mentionedDataSourceName is a data source's name as its mention's words keep
// it, one without a name the words Notion's API gives a database without one.
func mentionedDataSourceName(app core.App, dataSourceID string) (string, error) {
	name := ""
	err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": dataSourceID}).Row(&name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		name = notionUntitledDatabaseMentionTitle
	}
	return name, nil
}

// mentionedDataSource is the data source a database mention's ID names by its
// address and its own ID, the database holding it — a live one before one in
// the trash (dataSourceDatabase) — and the page holding that; false for an ID
// that is no address, or the address of no source here, as one kept by the ID
// written is.
func mentionedDataSource(app core.App, databaseID string) (dataSourceID, containerID, pageID string, found bool, err error) {
	id, addressed := strings.CutPrefix(databaseID, notionMarkdownDataSourceAddress)
	if !addressed || id == "" {
		return "", "", "", false, nil
	}
	workspaceID := ""
	err = app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": id}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", false, nil
	}
	if err != nil {
		return "", "", "", false, err
	}
	containerID, pageID, found, err = dataSourceDatabase(app, workspaceID, id)
	return id, containerID, pageID, found, err
}

// mentionedDataSourceHolderPage is the page holding the database of the data
// source a mention's ID names by its address, false for none
// (mentionedDataSource).
func mentionedDataSourceHolderPage(app core.App, databaseID string) (string, bool) {
	_, _, pageID, found, err := mentionedDataSource(app, databaseID)
	return pageID, err == nil && found
}

// notionMarkdownDataSourceID is the ID a data source's mention reads by in
// Markdown: a source here by its public API ID, as Notion's Markdown names
// one, a writer named it and the MCP tools name every source
// (mcpDataSourcePublicID), for a reader of Leafen's own IDs too; an ID kept
// as written stays as it is.
func notionMarkdownDataSourceID(app core.App, id string) string {
	return mcpDataSourcePublicID(app, id)
}

// pageDuplicationDataSourceMentions puts in a copy's database mappings
// (pageDuplicationTextMentionMappings) the data sources a text's mentions
// name by their addresses, each by its copy's address, as a database's
// mention names its copy: one the job did not copy stays as it was.
func pageDuplicationDataSourceMentions(app core.App, jobID string, databaseIDs map[string]struct{}, databases map[string]string) error {
	sources := []string{}
	for databaseID := range databaseIDs {
		if source, addressed := strings.CutPrefix(databaseID, notionMarkdownDataSourceAddress); addressed && source != "" {
			sources = append(sources, source)
		}
	}
	if len(sources) == 0 {
		return nil
	}
	copies, err := pageDuplicationReferenceMappings(app, jobID, "data_source", sources)
	for source, copied := range copies {
		databases[notionMarkdownDataSourceAddress+source] = notionMarkdownDataSourceAddress + copied
	}
	return err
}

// workspaceImportArchiveMentionReference is what a rich text's reference
// (workspaceImportArchiveRichTextKinds) names once an archive is brought
// back: a data source's mention, by its source's address, the source brought
// back by its address, or as it was for one the archive does not carry.
func workspaceImportArchiveMentionReference(db workspaceImportArchiveQueryer, attribute, sourceID string) (string, error) {
	if source, addressed := strings.CutPrefix(sourceID, notionMarkdownDataSourceAddress); addressed && attribute == "databaseId" {
		target, err := workspaceImportArchiveHeldReference(db, []string{workspaceExportItemDataSource}, source)
		return notionMarkdownDataSourceAddress + target, err
	}
	return workspaceImportArchiveHeldReference(db, workspaceImportArchiveRichTextKinds[attribute], sourceID)
}

// dataSourceAddressMention names a database mention whose ID resolved to a
// data source's address — one an MCP value carries to the public API's write
// by an identity of the address (mcpPropertyMention) — as the source's own
// mention, true, when a source here has it; false for any other.
func dataSourceAddressMention(app core.App, span *graphTextSpan, databaseID string) (bool, error) {
	dataSourceID, _, _, found, err := mentionedDataSource(app, databaseID)
	if err != nil || !found {
		return false, err
	}
	return true, dataSourceMentionHere(app, span, dataSourceID)
}
