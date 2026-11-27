package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// How the scope reads all a connection reaches to find the top ones. One
// walk of Notion's search waits on Notion for each page in turn, under the
// connection's pace (2026-11-13: 27 pages one after another, about 15 of a
// read's 26 seconds, the blocks climbed after them the rest). The scope reads
// it from both ends at once, newest first and oldest first by when Notion
// last changed each, until the two have passed each other, and climbs from
// the blocks pages sit in while it reads.

// notionScopeSearchResult is what the scope reads of a search result: a
// row's properties stay unread unless it is one of the top ones.
type notionScopeSearchResult struct {
	Object         string                   `json:"object"`
	ID             string                   `json:"id"`
	Parent         notionProviderParent     `json:"parent"`
	LastEditedTime string                   `json:"last_edited_time"`
	Title          []notionProviderRichText `json:"title"`
	Properties     json.RawMessage          `json:"properties"`
	InTrash        bool                     `json:"in_trash"`
	Archived       bool                     `json:"archived"`
	// A database the data sources showed: how many of them, and whether
	// they are all in the trash.
	fromSources int
	trashed     bool
}

// inTrash is whether Notion has it in the trash.
func (result notionScopeSearchResult) inTrash() bool {
	return result.InTrash || result.Archived || result.trashed
}

// title is what notionObjectTitle makes of the result.
func (result notionScopeSearchResult) title() string {
	object := notionProviderObject{Object: result.Object, Title: result.Title}
	if len(object.Title) == 0 && len(result.Properties) > 0 {
		_ = json.Unmarshal(result.Properties, &object.Properties)
	}
	return notionObjectTitle(object)
}

// notionScopeChain is one way through Notion's search: newest first,
// oldest first, or Notion's own order (a cursor from before both ends were
// read). edge is when Notion last changed the last result of its latest
// page: what it hasn't read yet comes after that.
type notionScopeChain struct {
	sort      string
	cursor    string
	edge      time.Time
	ended     bool
	sinceMeet int
}

type notionScopeChainPage struct {
	chain   int
	results []notionScopeSearchResult
	next    string
	err     error
}

// notionScopeChains are where a walk starts: both ends of the list, or where
// a cursor says the last read stopped.
func notionScopeChains(from string) []notionScopeChain {
	if from == "" {
		return []notionScopeChain{{sort: "descending"}, {sort: "ascending"}}
	}
	newest, oldest, both := strings.Cut(from, ",")
	if !both {
		return []notionScopeChain{{cursor: from}}
	}
	newest, _ = url.QueryUnescape(newest)
	oldest, _ = url.QueryUnescape(oldest)
	return []notionScopeChain{{sort: "descending", cursor: newest}, {sort: "ascending", cursor: oldest}}
}

// notionScopeChainsCursor is where a walk stopped before the chains met.
func notionScopeChainsCursor(chains []notionScopeChain) string {
	if len(chains) == 1 {
		return chains[0].cursor
	}
	return url.QueryEscape(chains[0].cursor) + "," + url.QueryEscape(chains[1].cursor)
}

func (client notionProviderClient) scopeSearchPage(ctx context.Context, chain notionScopeChain) notionScopeChainPage {
	payload := map[string]any{"page_size": notionImportScopeWalkItems}
	if chain.sort != "" {
		payload["sort"] = map[string]string{"direction": chain.sort, "timestamp": "last_edited_time"}
	}
	if chain.cursor != "" {
		payload["start_cursor"] = chain.cursor
	}
	var page struct {
		Results    []notionScopeSearchResult `json:"results"`
		NextCursor *string                   `json:"next_cursor"`
		HasMore    bool                      `json:"has_more"`
	}
	if err := client.request(ctx, http.MethodPost, "/v1/search", payload, &page); err != nil {
		return notionScopeChainPage{err: err}
	}
	if len(page.Results) > notionImportScopeWalkItems {
		return notionScopeChainPage{err: errors.New(errorCodeNotionImportProviderFailed)}
	}
	next, err := notionProviderContinuationCursor(chain.cursor, notionProviderPage{HasMore: page.HasMore, NextCursor: page.NextCursor})
	return notionScopeChainPage{results: page.Results, next: next, err: err}
}

// walkTopLevel reads the search from where from says, at most
// notionImportScopeWalkPages pages for notionImportScopeWalkBudget, adding
// to read what it learns, and gives the pages and databases it read that
// the reads before hadn't, where to go on when it stopped before the end,
// and when the blocks they sit in are all climbed. Once the chains have
// passed each other each reads one page more, so a result Notion put a
// little out of its order where they met is still read.
func (client notionProviderClient) walkTopLevel(ctx context.Context, from string, read *notionScopeRead) (
	entries []notionScopeSearchResult, next string, climbed <-chan struct{}, err error,
) {
	started := time.Now()
	chains := notionScopeChains(from)
	seen := read.seen
	sourcesRead := make(chan struct{})
	go func() {
		defer close(sourcesRead)
		if client.readsDataSources() {
			client.readScopeDatabases(ctx, read, started)
		}
	}()
	climbs := make(chan string, notionImportScopeWalkPages*notionImportScopeWalkItems)
	allClimbed := make(chan struct{})
	go func() {
		defer close(allClimbed)
		for blockID := range climbs {
			client.blockHolder(ctx, blockID, read.holders)
		}
	}()
	queued := map[string]bool{}
	pages := make(chan notionScopeChainPage, len(chains))
	readPage := func(index int) {
		chain := chains[index]
		go func() {
			page := client.scopeSearchPage(ctx, chain)
			page.chain = index
			pages <- page
		}()
	}
	for index := range chains {
		readPage(index)
	}
	asked, waiting, stopped, met := len(chains), len(chains), false, false
	for waiting > 0 {
		page := <-pages
		waiting--
		chain := &chains[page.chain]
		if page.err != nil {
			if errors.Is(page.err, context.DeadlineExceeded) && len(entries) > 0 {
				stopped = true
				continue
			}
			if err == nil {
				err = page.err
			}
			stopped = true
			continue
		}
		for _, result := range page.results {
			if result.Object != "page" && result.Object != "database" {
				continue
			}
			key := notionScopeID(result.ID)
			if seen[key] {
				continue
			}
			seen[key] = true
			entries = append(entries, result)
			if blockID := notionScopeID(result.Parent.BlockID); result.Parent.Type == "block_id" && !queued[blockID] {
				queued[blockID] = true
				climbs <- result.Parent.BlockID
			}
		}
		if len(page.results) > 0 {
			if edited, parseErr := time.Parse(time.RFC3339, page.results[len(page.results)-1].LastEditedTime); parseErr == nil {
				chain.edge = edited
			}
		}
		chain.cursor, chain.ended = page.next, page.next == ""
		if met {
			chain.sinceMeet++
		}
		if len(chains) == 2 && !met && !chains[0].edge.IsZero() && chains[1].edge.After(chains[0].edge) {
			met = true
		}
		if chain.ended || stopped || met && chain.sinceMeet > 0 ||
			asked >= notionImportScopeWalkPages || time.Since(started) > notionImportScopeWalkBudget {
			continue
		}
		asked++
		waiting++
		readPage(page.chain)
	}
	close(climbs)
	<-sourcesRead
	if err != nil {
		<-allClimbed
		return nil, "", nil, err
	}
	if !met && !slices.ContainsFunc(chains, func(chain notionScopeChain) bool { return chain.ended }) {
		next = notionScopeChainsCursor(chains)
	}
	return entries, next, allClimbed, nil
}

// notionScopeWalks are the top-level reads going on, by connection and where
// they start. A read of the same while one goes on waits for its answer
// rather than reading it all again at the same pace beside it (2026-11-13:
// the import window's check of a saved connection and "옮길 항목 선택" each
// read all of it, the second at half the pace). What a read that stopped
// before the end learned is kept a while by connection and the cursor
// it gave, for the read that goes on from there (2026-11-13: the stretch
// after a first one asked Notion about 28 parents the first had read, and
// ran out of time).
type notionScopeWalks struct {
	mu    sync.Mutex
	walks map[string]*notionScopeWalk
	kept  map[string]notionScopeKept
}

type notionScopeKept struct {
	read  *notionScopeRead
	until time.Time
}

// learned is what the read that gave the cursor key ends with learned, with
// walks.mu held.
func (walks *notionScopeWalks) learned(key string) *notionScopeRead {
	if kept, ok := walks.kept[key]; ok && time.Now().Before(kept.until) {
		return kept.read
	}
	return nil
}

// keep keeps what a read learned for the read going on from its cursor key,
// with walks.mu held.
func (walks *notionScopeWalks) keep(key string, read *notionScopeRead) {
	now := time.Now()
	for other, kept := range walks.kept {
		if !now.Before(kept.until) {
			delete(walks.kept, other)
		}
	}
	if walks.kept == nil {
		walks.kept = map[string]notionScopeKept{}
	}
	walks.kept[key] = notionScopeKept{read: read, until: now.Add(notionImportScopeKeptFor)}
}

type notionScopeWalk struct {
	done    chan struct{}
	scope   notionImportScope
	err     error
	waiting int
	cancel  context.CancelFunc
}

const (
	// A request of the scope's that Notion doesn't answer in this long is
	// asked again (2026-11-13: one block that never came back held a read to
	// its end, and the blocks after it weren't climbed).
	notionImportScopeRequestTime = 8 * time.Second
	// The read stops this long before its caller's time runs out, so what it
	// has read still reaches the caller.
	notionImportScopeAnswerTime = 2 * time.Second
	// What a read learned waits this long for the read going on from it:
	// someone may look over a stretch a while before asking for more.
	notionImportScopeKeptFor = 30 * time.Minute
)

// topLevelRoots are what the connection reaches at the top, from where from
// says: one read of them at a time per connection, which goes on while
// anyone still waits for it.
func (client notionProviderClient) topLevelRoots(ctx context.Context, from string) (notionImportScope, error) {
	client.client = &http.Client{Transport: client.client.Transport, Timeout: notionImportScopeRequestTime}
	client.waitOutRateLimit = true
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	deadline = deadline.Add(-notionImportScopeAnswerTime)
	if client.admission == nil || client.connectionID == "" {
		readContext, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		scope, _, err := client.readTopLevelRoots(readContext, from, nil, false)
		return scope, err
	}
	walks := &client.admission.scopeWalks
	key := client.connectionID + "\x00" + from
	walks.mu.Lock()
	walk := walks.walks[key]
	if walk == nil {
		walkContext, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
		walk = &notionScopeWalk{done: make(chan struct{}), cancel: cancel}
		if walks.walks == nil {
			walks.walks = map[string]*notionScopeWalk{}
		}
		walks.walks[key] = walk
		earlier := walks.learned(key)
		go func() {
			defer cancel()
			scope, read, err := client.readTopLevelRoots(walkContext, from, earlier, true)
			walks.mu.Lock()
			walk.scope, walk.err = scope, err
			if walks.walks[key] == walk {
				delete(walks.walks, key)
			}
			if err == nil && scope.HasMore {
				walks.keep(client.connectionID+"\x00"+strings.TrimPrefix(scope.NextCursor, notionImportScopeTopCursor), read)
			}
			walks.mu.Unlock()
			close(walk.done)
		}()
	}
	walk.waiting++
	walks.mu.Unlock()
	select {
	case <-walk.done:
		scope := walk.scope
		scope.Items = slices.Clone(scope.Items)
		return scope, walk.err
	case <-ctx.Done():
		walks.mu.Lock()
		walk.waiting--
		if walk.waiting == 0 {
			walk.cancel()
			if walks.walks[key] == walk {
				delete(walks.walks, key)
			}
		}
		walks.mu.Unlock()
		return notionImportScope{}, ctx.Err()
	}
}
