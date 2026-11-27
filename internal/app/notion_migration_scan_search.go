package app

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// The scan's last step finds, through Notion's search, what the tree does
// not show: pages hanging on what it read without a block of their own, and
// pages and databases in a row's body, which the scan does not read, in
// passes bounded by when what they look under was made
// (notion_migration_scan_made.go). It searches in the data source version,
// as the scope does (notion_import_scope_sources.go): the older version
// leaves out a database with more than one data source (2026-11-13), which
// the run still brings with the row's blocks, so the plan missed what the
// run moved. There a database comes as its data sources, each naming the
// database and where it is; a database of several is queued once. A staged
// bundle's workspace, read in the older version, has no such databases.

// notionMigrationSearchResult is what the scan reads of a search result: a
// data source's database and where that database is.
type notionMigrationSearchResult struct {
	Object         string               `json:"object"`
	ID             string               `json:"id"`
	Parent         notionProviderParent `json:"parent"`
	DatabaseParent notionProviderParent `json:"database_parent"`
	LastEditedTime string               `json:"last_edited_time"`
}

// placed is the page or database a result stands for, where it is and what
// the scan reads it as; ok is false for anything else.
func (result notionMigrationSearchResult) placed() (string, notionProviderParent, string, bool) {
	switch result.Object {
	case "page":
		return result.ID, result.Parent, "page", true
	case "database":
		return result.ID, result.Parent, "database", true
	case "data_source":
		if result.Parent.Type != "database_id" || result.Parent.DatabaseID == "" {
			return "", notionProviderParent{}, "", false
		}
		return result.Parent.DatabaseID, result.DatabaseParent, "database", true
	}
	return "", notionProviderParent{}, "", false
}

// scanNotionMigrationSearch queues each page and database the search finds
// under something the scan has seen and not seen itself, the most recently
// edited first, down to its pass's bound; a pass that queued one queues the
// next. A search going on keeps the version its cursor is of, so one begun
// before the data source version stays in the older.
func scanNotionMigrationSearch(
	ctx context.Context, client notionProviderClient, store notionMigrationScanStore, change *notionMigrationScanChange,
	item notionMigrationScanItem,
) error {
	if item.Cursor == "" {
		// A pass begins, looking under what was read since the last began;
		// with nothing read no page can newly hang on it.
		made, err := store.searchMade()
		if err != nil {
			return err
		}
		change.made.begin = true
		item.Bound, item.Dated = made.Since, item.Dated && !made.Undated
		if item.Dated && item.Bound == 0 {
			return nil
		}
	}
	version := item.Source
	if item.Cursor == "" && client.readsDataSources() {
		version = notionProviderDataSourceAPIVersion
	}
	payload := map[string]any{
		"page_size": notionImportProviderPageItems,
		"sort":      map[string]any{"direction": "descending", "timestamp": "last_edited_time"},
	}
	if item.Cursor != "" {
		payload["start_cursor"] = item.Cursor
	}
	found := struct {
		Results    []notionMigrationSearchResult `json:"results"`
		HasMore    bool                          `json:"has_more"`
		NextCursor *string                       `json:"next_cursor"`
	}{}
	var err error
	if version != "" {
		err = client.requestVersion(ctx, version, http.MethodPost, "/v1/search", payload, &found)
	} else {
		err = client.request(ctx, http.MethodPost, "/v1/search", payload, &found)
	}
	if err != nil {
		// Search is help, not the tree: without it the import still brings them.
		if notionProviderRefused(err) {
			return nil
		}
		return err
	}
	holders, queued := map[string]string{}, map[string]bool{}
	for _, result := range found.Results {
		id, parent, kind, ok := result.placed()
		// A row is its database's, and a database's rows are read with it.
		if !ok || parent.Type == "database_id" || parent.Type == "data_source_id" || queued[notionScopeID(id)] {
			continue
		}
		if seen, err := store.seen(id); err != nil {
			return err
		} else if seen {
			continue
		}
		parentID := parent.PageID
		if parent.Type == "block_id" {
			parentID = client.blockHolder(ctx, parent.BlockID, holders)
		}
		if parentID == "" {
			continue
		}
		if seen, err := store.seen(parentID); err != nil {
			return err
		} else if seen {
			queued[notionScopeID(id)] = true
			change.queue = append(change.queue, notionMigrationScanItem{Kind: kind, ID: id, ParentID: parentID})
		}
	}
	// The next pass looks under what this one found, once it is read.
	next := len(queued) > 0 && !item.Next
	item.Next = item.Next || next
	if found.HasMore && found.NextCursor != nil && !notionMigrationSearchPastBound(item, found.Results) {
		more := item
		more.Cursor, more.Source = *found.NextCursor, version
		change.queue = append(change.queue, more)
	}
	if next {
		change.queue = append(change.queue, notionMigrationScanItem{Kind: "search", notionMigrationScanPass: notionMigrationScanPass{Dated: item.Dated}})
	}
	return nil
}

// notionMigrationSearchPastBound tells whether a search page, newest first,
// reached what was last edited before its pass's bound less the margin:
// nothing after it can hang on what the pass looks under.
func notionMigrationSearchPastBound(item notionMigrationScanItem, results []notionMigrationSearchResult) bool {
	if !item.Dated || item.Bound == 0 || len(results) == 0 {
		return false
	}
	edited, err := time.Parse(time.RFC3339, strings.TrimSpace(results[len(results)-1].LastEditedTime))
	return err == nil && edited.Before(time.Unix(item.Bound, 0).Add(-notionImportUnmentionedMargin))
}
