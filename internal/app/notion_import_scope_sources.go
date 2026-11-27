package app

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Notion's search in the API version the scope reads leaves out a database
// with more than one data source, and GET databases refuses it there
// (2026-11-13: of the 274 databases a connection reached the search left out
// ten; the rows of three of them were read as top ones, and no one could
// choose the databases). The scope reads the data sources too, in the data
// source version beside the search: a database they show that the search
// didn't is one entry, under the database's parent, named as Notion names
// the database. A bundle's staged workspace has no such databases.

// notionScopeDataSource is what the scope reads of a data source the search
// gives: the database it is of, where that database is, and its name.
type notionScopeDataSource struct {
	Object         string                   `json:"object"`
	Parent         notionProviderParent     `json:"parent"`
	DatabaseParent notionProviderParent     `json:"database_parent"`
	Title          []notionProviderRichText `json:"title"`
	InTrash        bool                     `json:"in_trash"`
	Archived       bool                     `json:"archived"`
}

// notionScopeDatabase is a database as its data sources show it: where it
// is, a data source's name, how many of its data sources the search gave
// and how many of those are in the trash.
type notionScopeDatabase struct {
	id      string
	parent  notionProviderParent
	title   []notionProviderRichText
	sources int
	trashed int
}

// readsDataSources is whether the client reads Notion itself, which has
// databases of several data sources, rather than a staged bundle.
func (client notionProviderClient) readsDataSources() bool {
	if client.client == nil {
		return true
	}
	_, bundle := client.client.Transport.(notionBundleTransport)
	return !bundle
}

// readScopeDatabases reads Notion's data sources from where read stopped,
// until all are read, the walk's time or page count runs out, or ctx ends,
// and adds the databases they are of to read.
func (client notionProviderClient) readScopeDatabases(ctx context.Context, read *notionScopeRead, started time.Time) {
	for asked := 0; !read.sourcesRead && asked < notionImportScopeWalkPages &&
		time.Since(started) <= notionImportScopeWalkBudget && ctx.Err() == nil; asked++ {
		payload := map[string]any{
			"page_size": notionImportScopeWalkItems,
			"filter":    map[string]string{"property": "object", "value": "data_source"},
		}
		if read.sourcesCursor != "" {
			payload["start_cursor"] = read.sourcesCursor
		}
		var page struct {
			Results    []notionScopeDataSource `json:"results"`
			NextCursor *string                 `json:"next_cursor"`
			HasMore    bool                    `json:"has_more"`
		}
		if client.requestVersion(ctx, notionProviderDataSourceAPIVersion, http.MethodPost, "/v1/search", payload, &page) != nil ||
			len(page.Results) > notionImportScopeWalkItems {
			return
		}
		next, err := notionProviderContinuationCursor(read.sourcesCursor, notionProviderPage{HasMore: page.HasMore, NextCursor: page.NextCursor})
		if err != nil {
			return
		}
		for _, source := range page.Results {
			key := notionScopeID(source.Parent.DatabaseID)
			if source.Object != "data_source" || source.Parent.Type != "database_id" || key == "" {
				continue
			}
			database, ok := read.databases[key]
			if !ok {
				database = notionScopeDatabase{id: source.Parent.DatabaseID, parent: source.DatabaseParent}
			}
			if len(database.title) == 0 && !source.InTrash && !source.Archived {
				database.title = source.Title
			}
			database.sources++
			if source.InTrash || source.Archived {
				database.trashed++
			}
			read.databases[key] = database
		}
		read.sourcesCursor, read.sourcesRead = next, next == ""
	}
}

// scopeDatabaseEntries are the databases the data sources showed that the
// search hasn't given, as entries of the search, now seen; one whose data
// sources are all in the trash is in the trash with them.
func scopeDatabaseEntries(read *notionScopeRead) []notionScopeSearchResult {
	entries := []notionScopeSearchResult{}
	for key, database := range read.databases {
		if read.seen[key] {
			continue
		}
		read.seen[key] = true
		entries = append(entries, notionScopeSearchResult{
			Object: "database", ID: database.id, Parent: database.parent, Title: database.title,
			fromSources: database.sources, trashed: database.trashed == database.sources,
		})
	}
	return entries
}

// nameScopeDatabases names each top database of several data sources the
// search didn't give as Notion names the database, not its data source; a
// database of one data source has that source's name.
func (client notionProviderClient) nameScopeDatabases(ctx context.Context, items []notionImportRoot, entries map[string]int) {
	var group sync.WaitGroup
	slots := make(chan struct{}, notionProviderMaximumConcurrentRequests)
	for index := range items {
		if entries[notionScopeID(items[index].ID)] < 2 {
			continue
		}
		slots <- struct{}{}
		group.Go(func() {
			defer func() { <-slots }()
			if title, ok := client.scopeDatabaseTitle(ctx, items[index].ID); ok {
				items[index].Title = title
			}
		})
	}
	group.Wait()
}

// scopeDatabaseTitle is a database's own name, read in the data source
// version.
func (client notionProviderClient) scopeDatabaseTitle(ctx context.Context, databaseID string) (string, bool) {
	var database notionProviderObject
	if client.requestVersion(
		ctx, notionProviderDataSourceAPIVersion, http.MethodGet, "/v1/databases/"+url.PathEscape(databaseID), nil, &database,
	) != nil || database.Object != "database" {
		return "", false
	}
	return notionObjectTitle(database), true
}

// readsDatabase is whether the connection reads the database in the data
// source version, which a database of several data sources needs.
func (client notionProviderClient) readsDatabase(ctx context.Context, databaseID string) (notionScopeParent, bool) {
	var database struct {
		Object   string `json:"object"`
		Archived bool   `json:"archived"`
		InTrash  bool   `json:"in_trash"`
	}
	err := client.requestVersion(
		ctx, notionProviderDataSourceAPIVersion, http.MethodGet, "/v1/databases/"+url.PathEscape(databaseID), nil, &database,
	)
	if err == nil && database.Object == "database" {
		return notionScopeParentOf(database.Archived || database.InTrash), true
	}
	if err == nil || notionProviderRefused(err) {
		return notionScopeParentHidden, true
	}
	return notionScopeParentHidden, false
}
