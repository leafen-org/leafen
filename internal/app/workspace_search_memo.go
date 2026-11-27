package app

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A long page's search entry is made again from the blocks whose text
// changed, not from the whole page (2026-10-20: each change to a long page
// read its whole document, cut its whole body into chunks again and counted
// all its words — on a 2.6 MB page being typed, half the server's work).
//
// So the indexer keeps, for long pages it made lately, what their entries
// were made from: each block's text in reading order, the body's lines and
// pieces (searchChunkUnits), where each chunk ends, and the body's words and
// the chunks' pairs counted. A page waiting for some blocks' text alone
// (workspace_search_dirty_blocks.go) has those blocks read from its text
// document, which stays in memory between saves and readings
// (ygo_structural_document_cache.go), and its chunks cut again from the one
// a changed block starts in until they end where the old ones did: a
// block always ends a line, so they end where cutting the whole body would.
// What it keeps is the page as indexed: taken while the page is made and
// kept again only once the entry is written, so a change missed or a write
// that failed has the whole page made next. Every so often the page is made
// whole too and compared, and a difference is logged.
//
// A kept page holds its text once and the chunks by their ends, not their
// pairs: a chunk cut again has its pairs made again from the units it had
// (2026-10-21: a Korean page's pairs came to twice its text, so many long
// pages changing at once didn't fit and each was made whole every turn).

const (
	// A page's body from this size is kept; a shorter one is made whole.
	searchMemoMinBytes = 64 << 10
	searchMemoEntries  = 32
	// searchMemoShare is the part of the server's memory the kept pages may
	// come to.
	searchMemoShare = 32
	searchMemoIdle  = time.Minute
	// After this many changes made from a kept page, the page is made whole
	// and compared.
	searchMemoCheckEvery = 64
)

// errSearchMemoMiss is a kept page that can't make the change: the whole
// page is made.
var errSearchMemoMiss = errors.New("search: the kept page can't make this change")

// errSearchMemoStale is a chunk the index doesn't hold that the kept page
// has no text for: the page is made whole on the next turn.
var errSearchMemoStale = errors.New("search: the index doesn't hold a kept page's chunk")

// searchBodyPart is a text block's (or a sub-page's title) in reading order.
type searchBodyPart struct {
	id   string
	text string
}

type searchMemoPart struct {
	id    string
	text  string
	units int
}

type searchMemoChunk struct {
	hash    string
	midLine bool
	tail    string
	runes   int
	end     int
	forced  bool
	// made is the chunk with its text, made since the page was last kept.
	made *searchChunk
}

type searchPageMemo struct {
	// parts[0] is the page's own text; the others its blocks'.
	parts  []searchMemoPart
	at     map[string]int
	units  []searchChunkUnit
	chunks []searchMemoChunk
	// terms counts the body's words and the chunks' pairs; vocab is the size
	// of the distinct ones, a space after each.
	terms  map[string]int
	vocab  int
	runes  int
	filled int
	bytes  int
	steps  int
}

func searchPartUnits(text string) []searchChunkUnit {
	if text == "" {
		return nil
	}
	return searchChunkUnits(text)
}

// newSearchPageMemo keeps a page made whole from its own text and parts.
func newSearchPageMemo(head string, order []searchBodyPart) *searchPageMemo {
	memo := &searchPageMemo{at: make(map[string]int, len(order)), terms: map[string]int{}}
	memo.parts = append(make([]searchMemoPart, 0, len(order)+1), searchMemoPart{text: head})
	for _, part := range order {
		memo.at[part.id] = len(memo.parts)
		memo.parts = append(memo.parts, searchMemoPart{id: part.id, text: part.text})
	}
	for index := range memo.parts {
		units := searchPartUnits(memo.parts[index].text)
		memo.parts[index].units = len(units)
		memo.units = append(memo.units, units...)
		memo.countText(memo.parts[index].text, 1)
	}
	for _, cut := range cutSearchChunks(memo.units, 0, "", false, nil) {
		memo.chunks = append(memo.chunks, memo.keepCut(cut))
	}
	memo.measure()
	return memo
}

func (memo *searchPageMemo) keepCut(cut searchChunkCut) searchMemoChunk {
	made := cut.chunk
	memo.countTerms(strings.Fields(made.Grams), 1)
	return searchMemoChunk{hash: made.Hash, midLine: made.MidLine, tail: cut.tail,
		runes: utf8.RuneCountInString(made.Body), end: cut.end, forced: cut.forced, made: &made}
}

// dropChunk counts an old chunk's pairs out, made again from the units it
// had (unit gives them as they were) as cutSearchChunks made them.
func (memo *searchPageMemo) dropChunk(old []searchMemoChunk, index int, unit func(int) searchChunkUnit) {
	from, before, midLine := 0, "", false
	if index > 0 {
		from, before, midLine = old[index-1].end, old[index-1].tail, old[index-1].midLine
	}
	end := old[index].end
	text := strings.Builder{}
	for at := from; at < end; at++ {
		piece := unit(at)
		text.WriteString(piece.text)
		if piece.lineEnd && at < end-1 {
			text.WriteByte('\n')
		}
	}
	indexed := foldSearchText(text.String())
	if before != "" {
		joiner := "\n"
		if midLine {
			joiner = ""
		}
		indexed = before + joiner + indexed
	}
	memo.countTerms(strings.Fields(searchChunkGrams(old[index].hash, indexed)), -1)
}

// countText counts a part's words in or out, and its characters.
func (memo *searchPageMemo) countText(text string, sign int) {
	if text == "" {
		return
	}
	memo.countTerms(strings.FieldsFunc(foldSearchText(text), searchWordsSplitAt), sign)
	memo.runes += sign * utf8.RuneCountInString(text)
	memo.filled += sign
}

func (memo *searchPageMemo) countTerms(terms []string, sign int) {
	for _, term := range terms {
		count := memo.terms[term] + sign
		switch {
		case count <= 0:
			if _, held := memo.terms[term]; held {
				delete(memo.terms, term)
				memo.vocab -= len(term) + 1
			}
		default:
			if memo.terms[term] == 0 {
				memo.vocab += len(term) + 1
			}
			memo.terms[term] = count
		}
	}
}

// bodyRunes is the body's length, a line break between parts with text.
func (memo *searchPageMemo) bodyRunes() int {
	if memo.filled == 0 {
		return 0
	}
	return memo.runes + memo.filled - 1
}

func (memo *searchPageMemo) measure() {
	bytes := 0
	for _, part := range memo.parts {
		bytes += len(part.text) + len(part.id) + 48
	}
	for _, chunk := range memo.chunks {
		bytes += len(chunk.tail) + 96
	}
	memo.bytes = bytes + 32*len(memo.units) + 48*len(memo.terms) + memo.vocab
}

// replace makes a part's text new, cutting the chunks it touches again.
func (memo *searchPageMemo) replace(index int, text string) {
	part := &memo.parts[index]
	if part.text == text {
		return
	}
	start := 0
	for _, before := range memo.parts[:index] {
		start += before.units
	}
	units := searchPartUnits(text)
	gone := slices.Clone(memo.units[start : start+part.units])
	memo.countText(part.text, -1)
	memo.countText(text, 1)
	part.text, part.units = text, len(units)
	memo.units = slices.Replace(memo.units, start, start+len(gone), units...)
	memo.recut(start, gone, len(units))
}

// recut cuts the chunks again from the one the changed units start in until
// one ends where an old one did after them; the old chunk after that is made
// again only when the end it is indexed with changed. gone are the units the
// change took out at start.
func (memo *searchPageMemo) recut(start int, gone []searchChunkUnit, newCount int) {
	oldCount := len(gone)
	delta := newCount - oldCount
	old := memo.chunks
	// The units as they were, for the old chunks' pairs.
	unit := func(at int) searchChunkUnit {
		switch {
		case at < start:
			return memo.units[at]
		case at < start+oldCount:
			return gone[at-start]
		}
		return memo.units[at+delta]
	}
	first := sort.Search(len(old), func(index int) bool { return old[index].end > start })
	if first == len(old) && first > 0 && old[first-1].forced {
		first--
	}
	from, before, midLine := 0, "", false
	if first > 0 {
		from, before, midLine = old[first-1].end, old[first-1].tail, old[first-1].midLine
	}
	match := -1
	cuts := cutSearchChunks(memo.units, from, before, midLine, func(end int) bool {
		if end < start+newCount {
			return false
		}
		at := sort.Search(len(old), func(index int) bool { return old[index].end >= end-delta })
		if at >= first && at < len(old) && old[at].end == end-delta && !old[at].forced {
			match = at
			return true
		}
		return false
	})
	through := len(old) - 1
	if match >= 0 {
		through = match
	}
	for index := first; index <= through; index++ {
		memo.dropChunk(old, index, unit)
	}
	made := make([]searchMemoChunk, 0, len(cuts))
	for _, cut := range cuts {
		made = append(made, memo.keepCut(cut))
	}
	after := []searchMemoChunk{}
	if match >= 0 {
		after = append(after, old[match+1:]...)
		for index := range after {
			after[index].end += delta
		}
	}
	// The old chunk after is indexed with the end of the one before it.
	if len(after) > 0 && len(made) > 0 {
		last := made[len(made)-1]
		if last.tail != old[match].tail || last.midLine != old[match].midLine {
			again := cutSearchChunks(memo.units, last.end, last.tail, last.midLine, func(int) bool { return true })
			if len(again) == 1 && again[0].end == after[0].end {
				memo.dropChunk(old, match+1, unit)
				after[0] = memo.keepCut(again[0])
			} else {
				// Not where it was: cut on to the end.
				for index := match + 1; index < len(old); index++ {
					memo.dropChunk(old, index, unit)
				}
				after = after[:0]
				for _, cut := range cutSearchChunks(memo.units, last.end, last.tail, last.midLine, nil) {
					after = append(after, memo.keepCut(cut))
				}
			}
		}
	}
	memo.chunks = append(append(append(make([]searchMemoChunk, 0, first+len(made)+len(after)), old[:first]...), made...), after...)
}

// kept forgets the chunks' text once their entry is written.
func (memo *searchPageMemo) kept() {
	for index := range memo.chunks {
		memo.chunks[index].made = nil
	}
	memo.measure()
}

// words is what the page's row holds of its body, the distinct words and
// pairs in order, or nothing when they are too many (workspace_search_apart.go).
func (memo *searchPageMemo) words() (string, bool) {
	if memo.vocab-1 > searchPageWordsApartBytes {
		return "", true
	}
	terms := make([]string, 0, len(memo.terms))
	for term := range memo.terms {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	return strings.Join(terms, " "), false
}

// project fills a page's entry from what is kept: a chunk made since it was
// kept with its text, the others by what they hold alone.
func (memo *searchPageMemo) project(projection *searchPageProjection) {
	chunks := make([]searchChunk, len(memo.chunks))
	runes := 0
	for index, chunk := range memo.chunks {
		if chunk.made != nil {
			chunks[index] = *chunk.made
		} else {
			chunks[index] = searchChunk{Hash: chunk.hash, MidLine: chunk.midLine, Kept: true}
		}
		runes += chunk.runes
	}
	projection.Chunks = chunks
	projection.BodyRunes = -1
	if len(chunks) > 0 {
		projection.BodyRunes = len(chunks) - 1 + runes
	}
	projection.Words, projection.WordsApart = memo.words()
	projection.Grams = searchTextGrams(projection.TitleFolded)
	projection.Prepared = true
	projection.memo = memo
}

// sameAs says whether two kept pages hold the same chunks and words.
func (memo *searchPageMemo) sameAs(other *searchPageMemo) bool {
	if len(memo.chunks) != len(other.chunks) || memo.vocab != other.vocab || len(memo.terms) != len(other.terms) {
		return false
	}
	for index := range memo.chunks {
		if memo.chunks[index].hash != other.chunks[index].hash || memo.chunks[index].end != other.chunks[index].end {
			return false
		}
	}
	for term, count := range memo.terms {
		if other.terms[term] != count {
			return false
		}
	}
	return true
}

// buildSearchPageIncrement makes a kept page's entry again from the blocks
// that changed, read from its text document — in the page's order again
// when it may have changed (workspace_search_memo_order.go).
func buildSearchPageIncrement(app core.App, page *core.Record, dirty storedSearchDirty, memo *searchPageMemo, blocks []string, reordered bool) (searchPageProjection, error) {
	// A block it doesn't keep may be new to the page.
	for _, id := range blocks {
		if _, held := memo.at[id]; !held {
			reordered = true
		}
	}
	doc, found, done, err := borrowYgoStreamDocument(app, ygoCollaborationStream, page.Id)
	if err != nil {
		return searchPageProjection{}, err
	}
	defer done()
	if !found {
		return searchPageProjection{}, errSearchMemoMiss
	}
	memo.replace(0, page.GetString("content"))
	// Mentions are read as the index keeps them (workspace_search_date_words.go).
	read := searchIndexTextReader(app, dirty.WorkspaceID)
	if reordered {
		if err := memo.reorder(app, page.Id, doc, blocks, read); err != nil {
			return searchPageProjection{}, err
		}
	} else {
		changed := []int{}
		for _, id := range blocks {
			changed = append(changed, memo.at[id])
		}
		slices.Sort(changed)
		for _, index := range slices.Compact(changed) {
			memo.replace(index, searchBlockText(read(doc.GetText(ygoBlockTextRootPrefix+memo.parts[index].id))))
		}
	}
	if memo.bodyRunes() > maxWorkspaceSearchBodyRunes {
		return searchPageProjection{}, errors.New(errorCodeWorkspaceSearchDocumentTooLarge)
	}
	memo.steps++
	memo.measure()
	projection := searchPageProjection{
		PageID: page.Id, WorkspaceID: dirty.WorkspaceID, OwnerID: page.GetString("owner"),
		Title: page.GetString("title"), TitleFolded: foldSearchText(searchPageTitleWords(app, page)), SourceVersion: dirty.Version,
	}
	memo.project(&projection)
	return projection, nil
}

// borrowYgoStreamDocument lends a page's stored document for reading: the
// one kept in memory, else one read now, kept afterwards for the next
// reading and the next save when no change came while it was read; done
// gives it back.
func borrowYgoStreamDocument(app core.App, stream ygoStreamTables, room string) (*crdt.Doc, bool, func(), error) {
	cache := ygoDocumentCacheFor(stream)
	mark, marked, err := loadYgoStreamMark(app, stream, room)
	if err != nil {
		return nil, false, nil, err
	}
	if marked {
		if doc, bytes := cache.take(room, mark); doc != nil {
			return doc, true, func() { cache.put(room, mark, doc, bytes) }, nil
		}
	}
	doc, state, found, err := loadYgoStreamDoc(app, stream, room)
	if err != nil || !found {
		return doc, found, func() {}, err
	}
	after, markedAfter, err := loadYgoStreamMark(app, stream, room)
	if err != nil || !marked || !markedAfter || after != mark {
		return doc, true, func() {}, nil
	}
	return doc, true, func() { cache.put(room, mark, doc, len(state)) }, nil
}

// searchPageMemos keeps the long pages made lately. When they don't all fit,
// those kept stay and a page not kept before is made whole each time
// (2026-10-21: pushing out the oldest for each newcomer, many long pages
// changing in turn were all made whole, each pushed out just before it was
// needed again).
var searchPageMemos = &searchMemoCache{entries: map[string]*searchMemoEntry{}, out: map[string]struct{}{}}

type searchMemoEntry struct {
	memo  *searchPageMemo
	timer *time.Timer
}

type searchMemoCache struct {
	mu      sync.Mutex
	entries map[string]*searchMemoEntry
	order   []string
	bytes   int
	// out are the pages handed out, which come back kept before.
	out map[string]struct{}
}

// take hands out a page's kept memo, which is out of the cache until kept again.
func (cache *searchMemoCache) take(pageID string) *searchPageMemo {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry := cache.entries[pageID]
	if entry == nil {
		return nil
	}
	cache.remove(pageID)
	if len(cache.out) >= searchMemoEntries*4 {
		clear(cache.out)
	}
	cache.out[pageID] = struct{}{}
	return entry.memo
}

func (cache *searchMemoCache) put(pageID string, memo *searchPageMemo) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.remove(pageID)
	_, back := cache.out[pageID]
	delete(cache.out, pageID)
	limit, _ := serverMemoryLimit()
	full := func(bytes int) bool {
		return len(cache.order) >= searchMemoEntries || int64(cache.bytes+bytes) > limit/searchMemoShare
	}
	// A page not kept before waits for room rather than pushing out one kept.
	if !back && len(cache.order) > 0 && full(memo.bytes) {
		return
	}
	entry := &searchMemoEntry{memo: memo}
	entry.timer = time.AfterFunc(searchMemoIdle, func() {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		if cache.entries[pageID] == entry {
			cache.remove(pageID)
		}
	})
	// One coming back that grew makes room from the oldest.
	for len(cache.order) > 0 && full(memo.bytes) {
		cache.remove(cache.order[0])
	}
	cache.entries[pageID] = entry
	cache.order = append(cache.order, pageID)
	cache.bytes += memo.bytes
}

func (cache *searchMemoCache) remove(pageID string) {
	if entry := cache.entries[pageID]; entry != nil {
		entry.timer.Stop()
		delete(cache.entries, pageID)
		cache.bytes -= entry.memo.bytes
	}
	for index, name := range cache.order {
		if name == pageID {
			cache.order = append(cache.order[:index], cache.order[index+1:]...)
			break
		}
	}
}
