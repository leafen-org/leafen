package app

import (
	"context"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// What a Notion connection offers as import roots. Without search words it is
// what the connection reaches at the top, as Notion's own list of a
// connection's pages shows it: the pages and databases whose parent is the
// workspace or something the connection can't see. Notion's search returns
// every page it reaches, rows and subpages too (2026-10-16: a company's top
// page was 97th of 928 behind database rows), so the scope reads them all
// (notion_import_scope_walk.go) and keeps the top ones, saying there are more
// when it stopped before the end. With search words it is Notion's search at
// any depth, a page at a time.

const (
	// The scope reads at most this many search pages, from both ends
	// together, of the most Notion gives at once, and stops reading after
	// notionImportScopeWalkBudget, keeping what it has read; its next cursor
	// goes on from there.
	notionImportScopeWalkPages  = 40
	notionImportScopeWalkItems  = 100
	notionImportScopeWalkBudget = 20 * time.Second
	// A page inside a column or toggle names a block as its parent; the scope
	// climbs at most this many blocks to the page that holds it.
	notionImportScopeBlockClimb = 8
	// Search words past this many characters are left off.
	notionImportScopeQueryRunes = 100
	// Marks a cursor as going on with the top ones, not with a search.
	notionImportScopeTopCursor = "top:"
)

type notionProviderParent struct {
	Type       string `json:"type"`
	PageID     string `json:"page_id"`
	DatabaseID string `json:"database_id"`
	BlockID    string `json:"block_id"`
}

func notionScopeID(id string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(id), "-", ""))
}

// searchRoots lists import roots: the top ones without query, else the pages
// and databases whose title matches it, from cursor on.
func (client notionProviderClient) searchRoots(ctx context.Context, cursor, query string) (notionImportScope, error) {
	query = strings.TrimSpace(query)
	if runes := []rune(query); len(runes) > notionImportScopeQueryRunes {
		query = string(runes[:notionImportScopeQueryRunes])
	}
	if query == "" && (cursor == "" || strings.HasPrefix(cursor, notionImportScopeTopCursor)) {
		return client.topLevelRoots(ctx, strings.TrimPrefix(cursor, notionImportScopeTopCursor))
	}
	// A search someone waits for waits out Notion asking to wait, as the top
	// ones' read does, and stops in time for what it says to reach them.
	client.waitOutRateLimit = true
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-notionImportScopeAnswerTime))
		defer cancel()
	}
	if query != "" && client.readsDataSources() {
		return client.searchRootsMatching(ctx, cursor, query)
	}
	payload := map[string]any{"page_size": notionImportProviderPageItems}
	if query != "" {
		payload["query"] = query
	}
	if cursor != "" {
		payload["start_cursor"] = cursor
	}
	var page notionProviderPage
	if err := client.request(ctx, http.MethodPost, "/v1/search", payload, &page); err != nil {
		return notionImportScope{}, err
	}
	results, err := notionProviderPageResults(page)
	if err != nil {
		return notionImportScope{}, err
	}
	items := make([]notionImportRoot, 0, len(results))
	for _, result := range results {
		if result.Object == "page" || result.Object == "database" {
			items = append(items, notionImportRoot{ID: result.ID, Title: notionObjectTitle(result), Kind: result.Object})
		}
	}
	nextCursor := ""
	if page.NextCursor != nil {
		nextCursor = *page.NextCursor
	}
	return notionImportScope{Items: items, NextCursor: nextCursor, HasMore: page.HasMore}, nil
}

// notionScopeRead is what reading the top ones has learned, which a read
// going on from where one stopped starts from: every page and database the
// search gave, the holders of the blocks they sit in, and what Notion said
// of the parents it was asked about (notionScopeParent). pending are pages
// and databases read whose parent Notion didn't say of in time, which a
// later read settles. databases are the databases Notion's data
// sources showed (notion_import_scope_sources.go), read up to sourcesCursor,
// or all of them once sourcesRead.
type notionScopeRead struct {
	seen          map[string]bool
	holders       map[string]string
	parents       map[string]notionScopeParent
	pending       []notionScopeSearchResult
	databases     map[string]notionScopeDatabase
	sourcesCursor string
	sourcesRead   bool
}

func newNotionScopeRead(earlier *notionScopeRead) *notionScopeRead {
	if earlier == nil {
		return &notionScopeRead{
			seen: map[string]bool{}, holders: map[string]string{}, parents: map[string]notionScopeParent{},
			databases: map[string]notionScopeDatabase{},
		}
	}
	return &notionScopeRead{
		seen: maps.Clone(earlier.seen), holders: maps.Clone(earlier.holders),
		parents: maps.Clone(earlier.parents), pending: slices.Clone(earlier.pending),
		databases: maps.Clone(earlier.databases), sourcesCursor: earlier.sourcesCursor, sourcesRead: earlier.sourcesRead,
	}
}

// notionScopeParent is what Notion says of a parent the reads haven't seen:
// one the connection doesn't read, whose children are top ones; one it reads
// that isn't in the trash, which the search gives; or one in the trash, with
// all it holds (2026-11-13: Notion's search and sidebar show nothing of a
// page in the trash, while the API's search still gave eleven databases in
// seven such pages).
type notionScopeParent uint8

const (
	notionScopeParentHidden notionScopeParent = iota
	notionScopeParentShown
	notionScopeParentTrashed
)

// notionScopeAsk is a parent the reads haven't seen, asked about at path; a
// block's holder, with no path, is a page or else a database.
type notionScopeAsk struct {
	id   string
	path string
}

// parentAsk is what holds the entry and where Notion is asked about it; a
// block's holder is known once its block is climbed.
func (read *notionScopeRead) parentAsk(entry notionScopeSearchResult) notionScopeAsk {
	switch entry.Parent.Type {
	case "page_id":
		return notionScopeAsk{id: entry.Parent.PageID, path: "/v1/pages/"}
	case "database_id":
		return notionScopeAsk{id: entry.Parent.DatabaseID, path: "/v1/databases/"}
	case "block_id":
		return notionScopeAsk{id: read.holders[notionScopeID(entry.Parent.BlockID)]}
	}
	return notionScopeAsk{}
}

// readTopLevelRoots walks the search from where from says, on from what an
// earlier read learned, and keeps the top ones; when carry, what it learned
// goes on to the read from its cursor.
func (client notionProviderClient) readTopLevelRoots(ctx context.Context, from string, earlier *notionScopeRead, carry bool) (
	notionImportScope, *notionScopeRead, error,
) {
	read := newNotionScopeRead(earlier)
	entries, next, climbed, err := client.walkTopLevel(ctx, from, read)
	if err != nil {
		return notionImportScope{}, nil, err
	}
	entries, read.pending = append(append(read.pending, entries...), scopeDatabaseEntries(read)...), nil
	// A parent not read may be one the search doesn't give, elsewhere in the
	// list for a read of a stretch, or one the walk passed over while the
	// list moved under it (2026-11-13: every third read of the whole list
	// gave one more top item, as people changed pages while it was read), or
	// in the trash with what it holds. So Notion is asked about each one
	// once, at the path of its kind and a few at a time, while the last
	// blocks are climbed (2026-11-13: asked one by one after the walk, pages
	// first, a stretch ran out of time asking and gave 120-odd rows and
	// subpages as top ones).
	asks := func(blocks bool) map[string]notionScopeAsk {
		asks := map[string]notionScopeAsk{}
		for _, entry := range entries {
			if (entry.Parent.Type == "block_id") != blocks || entry.inTrash() {
				continue
			}
			ask := read.parentAsk(entry)
			if key := notionScopeID(ask.id); key != "" && !read.seen[key] {
				if _, known := read.parents[key]; !known {
					asks[key] = ask
				}
			}
		}
		return asks
	}
	parents := map[string]notionScopeParent{}
	asked := make(chan struct{})
	go func() {
		defer close(asked)
		parents = client.askScopeParents(ctx, asks(false))
	}()
	<-climbed
	for _, entry := range entries {
		// The walk climbs from the blocks the search's results sit in; a
		// database only the data sources showed, and a block Notion didn't
		// answer for, is climbed from here.
		if _, climbed := read.holders[notionScopeID(entry.Parent.BlockID)]; entry.Parent.Type == "block_id" && !climbed {
			client.blockHolder(ctx, entry.Parent.BlockID, read.holders)
		}
	}
	holders := client.askScopeParents(ctx, asks(true))
	<-asked
	maps.Copy(read.parents, parents)
	maps.Copy(read.parents, holders)
	items, named := []notionImportRoot{}, map[string]int{}
	for _, entry := range entries {
		// What is in the trash, or in a page in the trash, isn't offered:
		// Notion shows it only in its trash. It stays seen, so what it holds
		// isn't a top one either.
		if entry.inTrash() {
			continue
		}
		key := notionScopeID(read.parentAsk(entry).id)
		_, climbed := read.holders[notionScopeID(entry.Parent.BlockID)]
		if entry.Parent.Type == "block_id" && !climbed && carry && next != "" {
			read.pending = append(read.pending, entry)
			continue
		}
		top := key == ""
		if key != "" && !read.seen[key] {
			parent, known := read.parents[key]
			if !known && carry && next != "" {
				read.pending = append(read.pending, entry)
				continue
			}
			if parent == notionScopeParentTrashed {
				continue
			}
			top = parent == notionScopeParentHidden
		}
		if top {
			items = append(items, notionImportRoot{ID: entry.ID, Title: entry.title(), Kind: entry.Object})
			named[notionScopeID(entry.ID)] = entry.fromSources
		}
	}
	client.nameScopeDatabases(ctx, items, named)
	scope := notionImportScope{Items: items}
	if next != "" {
		scope.NextCursor, scope.HasMore = notionImportScopeTopCursor+next, true
	}
	return scope, read, nil
}

// askScopeParents asks Notion, as many at once as the connection's pace
// takes, about each parent, and gives the answers it got; one Notion didn't
// answer in time is left out, and the read after asks again (with none
// after, its children are kept as top ones).
func (client notionProviderClient) askScopeParents(ctx context.Context, asks map[string]notionScopeAsk) map[string]notionScopeParent {
	answers := map[string]notionScopeParent{}
	var mu sync.Mutex
	var group sync.WaitGroup
	slots := make(chan struct{}, notionProviderMaximumConcurrentRequests)
	for key, ask := range asks {
		slots <- struct{}{}
		group.Go(func() {
			defer func() { <-slots }()
			if answer, known := client.askScopeParent(ctx, ask); known {
				mu.Lock()
				answers[key] = answer
				mu.Unlock()
			}
		})
	}
	group.Wait()
	return answers
}

// askScopeParent is what Notion says of the parent: whether the connection
// reads it, and whether it is in the trash.
func (client notionProviderClient) askScopeParent(ctx context.Context, ask notionScopeAsk) (answer notionScopeParent, known bool) {
	paths := []string{ask.path}
	if ask.path == "" {
		paths = []string{"/v1/pages/", "/v1/databases/"}
	}
	for _, path := range paths {
		var object struct {
			Archived bool `json:"archived"`
			InTrash  bool `json:"in_trash"`
		}
		err := client.request(ctx, http.MethodGet, path+url.PathEscape(ask.id), nil, &object)
		if err == nil {
			return notionScopeParentOf(object.Archived || object.InTrash), true
		}
		if !notionProviderRefused(err) {
			return notionScopeParentHidden, false
		}
		if path == "/v1/databases/" && client.readsDataSources() {
			return client.readsDatabase(ctx, ask.id)
		}
	}
	return notionScopeParentHidden, true
}

// notionScopeParentOf is a parent the connection reads, in the trash or not.
func notionScopeParentOf(trashed bool) notionScopeParent {
	if trashed {
		return notionScopeParentTrashed
	}
	return notionScopeParentShown
}

// blockHolder is the page or database a block sits in, climbing its parent
// blocks; empty when Notion doesn't show the way up. The blocks climbed
// through are remembered with it: pages in the columns of one row share
// the climb from their row up.
func (client notionProviderClient) blockHolder(ctx context.Context, blockID string, holders map[string]string) string {
	key := notionScopeID(blockID)
	if holder, ok := holders[key]; ok {
		return holder
	}
	holder, current, climbed := "", blockID, []string{key}
	for range notionImportScopeBlockClimb {
		var block struct {
			Parent notionProviderParent `json:"parent"`
		}
		if err := client.request(ctx, http.MethodGet, "/v1/blocks/"+url.PathEscape(current), nil, &block); err != nil {
			if !notionProviderRefused(err) {
				// Notion didn't answer: the climb is tried again.
				return ""
			}
			break
		}
		if block.Parent.Type != "block_id" {
			holder = block.Parent.PageID + block.Parent.DatabaseID
			break
		}
		current = block.Parent.BlockID
		if known, ok := holders[notionScopeID(current)]; ok {
			holder = known
			break
		}
		climbed = append(climbed, notionScopeID(current))
	}
	if holder == "" {
		climbed = climbed[:1]
	}
	for _, block := range climbed {
		holders[block] = holder
	}
	return holder
}
