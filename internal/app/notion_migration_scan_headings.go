package app

import (
	"context"
	"encoding/json"
	"slices"
)

// notionImportReadsChildren is the one rule of whose children a move reads,
// the import and the scan alike: a block Notion says holds blocks, of a kind
// the import takes (a toggle heading_4 among them), but not a table row's
// (they stay in Notion, notion_import_table_row_blocks.go), a synced copy's
// (its original's, read where the original is), a page link's, or a sub-page's
// or database's, which are read as pages and databases. A table's rows are
// read like any block's children: the run reads them, a request each table,
// and makes them. Plain headings Notion calls childless are read as their
// page's Markdown says (notion_import_heading_children.go).
func notionImportReadsChildren(block notionProviderObject) bool {
	switch block.Type {
	case graphBlockTypeTableRow, graphBlockTypeLinkToPage, "child_page", "child_database":
		return false
	case graphBlockTypeSyncedBlock:
		copied := struct {
			SyncedFrom json.RawMessage `json:"synced_from"`
		}{}
		if json.Unmarshal(block.Payload, &copied) == nil && len(copied.SyncedFrom) > 0 && string(copied.SyncedFrom) != "null" {
			return false
		}
	}
	return block.HasChildren && notionImportBlockTypeTaken(block.Type)
}

// notionMigrationScanReads tells whose children the scan lists: those the run
// reads, but a template button's, whose blocks are its blueprint, made when
// it is pressed, and whose sub-pages the run doesn't make pages of.
func notionMigrationScanReads(block notionProviderObject) bool {
	return block.Type != "template" && notionImportReadsChildren(block)
}

// notionMigrationScanHeadings queues what a listing's plain headings Notion
// calls childless hold when their page's Markdown shows a heading holding
// blocks, as the import reads them (notion_import_heading_children.go); the
// Markdown read, once a page, counts with the listing's request, read too
// where the listing's lines may have lost a data source's mention, as the
// run reads it then (notion_import_data_source_mentions.go).
func notionMigrationScanHeadings(
	ctx context.Context, client notionProviderClient, migrationID string, change *notionMigrationScanChange,
	item notionMigrationScanItem, blocks []notionProviderObject,
) error {
	headings := []string{}
	for _, block := range blocks {
		if notionProviderPlainHeadingNamedEmpty(block) {
			headings = append(headings, block.ID)
		}
	}
	if len(headings) == 0 && !notionProviderBlocksMayLoseSource(blocks) || item.PageID == "" {
		return nil
	}
	holds, read, err := notionPageHoldsUnderHeadings(ctx, client, "scan "+migrationID, item.PageID)
	if read {
		change.extra++
	}
	if err != nil || !holds || len(headings) == 0 {
		return err
	}
	for _, id := range headings {
		change.queue = append(change.queue, notionMigrationScanItem{Kind: "blocks", ID: id, PageID: item.PageID, Depth: item.Depth + 1})
	}
	return nil
}

// notionMigrationSampleHeadings tells whether a sampled row's plain headings
// Notion calls childless hold blocks, from the row's Markdown as the run reads
// it, once a row: a row whose top has such a heading, or lines that may have
// lost a data source's mention, counts that read for the run, a read made now
// counts for the scan.
func notionMigrationSampleHeadings(
	ctx context.Context, client notionProviderClient, migrationID string, change *notionMigrationScanChange,
	rowID string, blocks []notionProviderObject, top bool,
) (bool, error) {
	if rowID == "" || !slices.ContainsFunc(blocks, notionProviderPlainHeadingNamedEmpty) && !notionProviderBlocksMayLoseSource(blocks) {
		return false, nil
	}
	holds, read, err := notionPageHoldsUnderHeadings(ctx, client, "scan "+migrationID, rowID)
	if read {
		change.extra++
	}
	if top || read {
		change.sample.markdown++
	}
	return holds, err
}
