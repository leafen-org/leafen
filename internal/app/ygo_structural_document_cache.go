package app

import (
	"sync"
	"time"

	"github.com/reearth/ygo/crdt"
)

// A page's structure document a change has just written stays in memory for
// the next change of the same page (2026-10-14: read again from its stored
// parts for every turn of a long page's import, each turn took longer as the
// page grew). It is used again only while the page's stored document has the
// mark the change left it with (ygo_document_marks.go), so a change made any
// other way or a replaced snapshot has it read afresh; a compaction, which
// stores the same document in fewer parts, gives it the new mark. A
// document in use is out of the cache; one goes back only once its change is
// committed, and goes after a minute unused. A few pages' at most are kept,
// and only those that take a while to read again: a small page's document
// is read again at once, and kept, the edits of many people on small pages
// put a long page's out before its next change (2026-10-15: 300 people
// writing while a 20,000-paragraph page was imported, every turn of the
// import read the whole page again while it held the one write connection).
//
// An open page's typing goes into its kept text document as it is saved
// (ygo_attributed_persistence.go). Up to 32 pages' are kept, as long as their
// stored size stays within a share of the server's memory (memory_plan.go),
// and always the last one: with two, a
// server whose people typed in 25 long pages while others wrote blocks to
// them read a page's whole document for nearly every block (2026-10-20: 49
// ms a block on a 750 KB page being typed, 13 ms on a quiet one). A document
// typed a keystroke at a time takes about 18 times its stored size in memory.

const (
	ygoStructuralCacheEntries = 32
	// ygoDocumentCacheShare is the part of the server's memory whose stored
	// size each cache's documents may come to: a 750 KB page eight times over
	// on the default 1.5 GiB, at most about 7% of the memory once in memory.
	ygoDocumentCacheShare  = 256
	ygoStructuralCacheIdle = time.Minute
	// ygoDocumentCacheMinBytes is the stored size from which a document is kept.
	ygoDocumentCacheMinBytes = 64 << 10
)

type ygoCachedDocument struct {
	mark  int64
	bytes int
	doc   *crdt.Doc
	timer *time.Timer
}

type ygoDocumentCache struct {
	mu      sync.Mutex
	entries map[string]*ygoCachedDocument
	order   []string
	bytes   int
}

var ygoStructuralDocuments = &ygoDocumentCache{entries: map[string]*ygoCachedDocument{}}

// ygoTextDocuments keeps a page's text document an import's turn has just
// written, the same way, for its next turn (2026-10-15: read from its stored
// parts on each turn, at 15,000 blocks a seventh of the import's time).
var ygoTextDocuments = &ygoDocumentCache{entries: map[string]*ygoCachedDocument{}}

// ygoDocumentCacheFor is the cache that keeps a stream's documents.
func ygoDocumentCacheFor(stream ygoStreamTables) *ygoDocumentCache {
	if stream.head == ygoStructuralStream.head {
		return ygoStructuralDocuments
	}
	return ygoTextDocuments
}

// ygoStreamPartsBytes is the stored size of a stream's parts.
func ygoStreamPartsBytes(parts [][]byte, more ...[]byte) int {
	total := 0
	for _, part := range append(append([][]byte{}, parts...), more...) {
		total += len(part)
	}
	return total
}

// take hands out the room's document, and its stored size, when the stored
// document has the mark, leaving the cache without it.
func (cache *ygoDocumentCache) take(room string, mark int64) (*crdt.Doc, int) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry := cache.entries[room]
	if entry == nil {
		return nil, 0
	}
	cache.remove(room)
	if entry.mark != mark {
		return nil, 0
	}
	return entry.doc, entry.bytes
}

// put keeps the room's document as the stored one with the mark, of the
// given stored size, holds it; a small one isn't kept.
func (cache *ygoDocumentCache) put(room string, mark int64, doc *crdt.Doc, bytes int) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.remove(room)
	if bytes < ygoDocumentCacheMinBytes {
		return
	}
	entry := &ygoCachedDocument{mark: mark, bytes: bytes, doc: doc}
	entry.timer = time.AfterFunc(ygoStructuralCacheIdle, func() {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		if cache.entries[room] == entry {
			cache.remove(room)
		}
	})
	cache.entries[room] = entry
	cache.order = append(cache.order, room)
	cache.bytes += bytes
	budget := ygoDocumentCacheBudget()
	for len(cache.order) > 1 && (len(cache.order) > ygoStructuralCacheEntries || int64(cache.bytes) > budget) {
		cache.remove(cache.order[0])
	}
}

// ygoDocumentCacheBudget is the stored size a cache's documents may come to.
func ygoDocumentCacheBudget() int64 {
	limit, _ := serverMemoryLimit()
	return limit / ygoDocumentCacheShare
}

// holds reports whether the room's document is kept.
func (cache *ygoDocumentCache) holds(room string) bool {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.entries[room] != nil
}

// rekey has the room's document, kept for the stored one with one mark,
// stand for it with another, its stored size changed by delta, as a
// compaction leaves it holding the same.
func (cache *ygoDocumentCache) rekey(room string, before, after int64, delta int) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if entry := cache.entries[room]; entry != nil && entry.mark == before {
		entry.mark, entry.bytes = after, entry.bytes+delta
		cache.bytes += delta
	}
}

func (cache *ygoDocumentCache) remove(room string) {
	if entry := cache.entries[room]; entry != nil {
		entry.timer.Stop()
		delete(cache.entries, room)
		cache.bytes -= entry.bytes
	}
	for index, name := range cache.order {
		if name == room {
			cache.order = append(cache.order[:index], cache.order[index+1:]...)
			break
		}
	}
}
