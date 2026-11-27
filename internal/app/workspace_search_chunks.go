package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"unicode"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's text is in the index two ways (2026-10-18: a 2.6 MB page's one
// row was taken apart and written again for every change, 140 ms of the one
// write connection, and every result read the whole body). Its row holds the
// title, the title's pairs and the body's words and pairs without their
// order: every search's words and word starts match it as they matched the
// whole text, each FTS token being made of what the words hold. The body
// itself is in chunks of its lines — a line longer than a chunk in pieces of
// its words (2026-10-19: a page's one long line was one chunk however long,
// 407 KB written again for every letter typed into it) — each indexed with
// the end of the one before so a phrase across two chunks is still found:
// phrases and a word's pairs are checked in order there
// (workspace_search_order.go), and a result's line is read from the chunk
// that holds it. A change writes the row and the chunks it touched.

const (
	// A chunk ends after a line or piece once it holds this much and the
	// line or piece says so, and always once it holds the most: where chunks
	// end follows from what comes before alone, so neither an edit elsewhere
	// nor letters added at a line's end moves the chunks before.
	searchChunkLeastBytes = 2 << 10
	searchChunkMostBytes  = 16 << 10
	// A word longer than this goes in runs of this many characters.
	searchChunkWordRunes = 256
	// How much of the chunk before a chunk is indexed with: more than a
	// search's keywords hold (maxWorkspaceSearchQueryRunes), from a word's start.
	searchChunkOverlapRunes = 320
)

type searchChunk struct {
	Body       string
	BodyFolded string
	// Indexed is what the index holds: the end of the chunk before, then the chunk's.
	Indexed string
	Grams   string
	Hash    string
	// MidLine is set when the chunk ends inside a line, the next going on with it.
	MidLine bool
	// Kept is a chunk of a kept page known by its hash alone, which the index
	// holds already (workspace_search_memo.go).
	Kept bool
}

// searchChunkUnit is what a chunk ends after: a line, or a piece of a long one.
type searchChunkUnit struct {
	text    string
	lineEnd bool
}

// searchChunkUnits splits a body into lines, a line longer than a chunk into
// its words with what follows each, and a word longer than a run into runs.
func searchChunkUnits(body string) []searchChunkUnit {
	units := []searchChunkUnit{}
	for _, line := range strings.Split(body, "\n") {
		if len(line) < searchChunkMostBytes {
			units = append(units, searchChunkUnit{text: line, lineEnd: true})
			continue
		}
		pieces := []string{}
		start, runes, inSeparator := 0, 0, false
		for at, r := range line {
			separator := searchWordsSplitAt(r)
			if !separator && inSeparator || runes == searchChunkWordRunes {
				pieces = append(pieces, line[start:at])
				start, runes = at, 0
			}
			inSeparator = separator
			runes++
		}
		pieces = append(pieces, line[start:])
		for index, piece := range pieces {
			units = append(units, searchChunkUnit{text: piece, lineEnd: index == len(pieces)-1})
		}
	}
	return units
}

// searchChunksOf cuts a page's body into chunks of whole lines, and of
// pieces of a line longer than a chunk.
func searchChunksOf(body string) []searchChunk {
	if body == "" {
		return nil
	}
	cuts := cutSearchChunks(searchChunkUnits(body), 0, "", false, nil)
	chunks := make([]searchChunk, len(cuts))
	for index, cut := range cuts {
		chunks[index] = cut.chunk
	}
	return chunks
}

// searchChunkCut is a chunk cut from units: the unit after its last, the end
// the next is indexed with, and whether it ended only because the units did.
type searchChunkCut struct {
	chunk  searchChunk
	end    int
	tail   string
	forced bool
}

// cutSearchChunks cuts units into chunks from start, the chunk before having
// ended with before (as a line's middle when midLine), as from the body's
// start: where a chunk ends follows from its own units alone. After each
// chunk that ends by itself, stop may end the cutting (a page changed in
// one place is cut again only until its old chunks go on as they were,
// workspace_search_memo.go).
func cutSearchChunks(units []searchChunkUnit, start int, before string, midLine bool, stop func(end int) bool) []searchChunkCut {
	cuts := []searchChunkCut{}
	joiner := "\n"
	if midLine {
		joiner = ""
	}
	size := 0
	cut := func(end int, forced bool) {
		text := strings.Builder{}
		for index := start; index < end; index++ {
			text.WriteString(units[index].text)
			if units[index].lineEnd && index < end-1 {
				text.WriteByte('\n')
			}
		}
		folded := foldSearchText(text.String())
		indexed := folded
		if before != "" {
			indexed = before + joiner + folded
		}
		digest := sha256.Sum256([]byte(text.String() + "\x00" + indexed))
		hash := hex.EncodeToString(digest[:12])
		midLine := !units[end-1].lineEnd
		before, joiner = searchChunkTail(folded), "\n"
		if midLine {
			joiner = ""
		}
		cuts = append(cuts, searchChunkCut{chunk: searchChunk{Body: text.String(), BodyFolded: folded, Indexed: indexed,
			Grams: searchChunkGrams(hash, indexed), Hash: hash, MidLine: midLine}, end: end, tail: before, forced: forced})
		start, size = end, 0
	}
	for index := start; index < len(units); index++ {
		unit := units[index]
		size += len(unit.text)
		if unit.lineEnd {
			size++
		}
		if size >= searchChunkMostBytes || size >= searchChunkLeastBytes && searchChunkEndsAfter(unit.text) {
			cut(index+1, false)
			if stop != nil && stop(index+1) {
				return cuts
			}
		}
	}
	if start < len(units) {
		cut(len(units), true)
	}
	return cuts
}

// searchChunkTail is the end of a chunk the next is indexed with: its last
// searchChunkOverlapRunes characters, from the start of a word.
func searchChunkTail(folded string) string {
	runes := []rune(folded)
	if len(runes) <= searchChunkOverlapRunes {
		return folded
	}
	tail := runes[len(runes)-searchChunkOverlapRunes:]
	for at, r := range tail {
		if searchWordsSplitAt(r) {
			if at+1 < len(tail) {
				return string(tail[at+1:])
			}
			break
		}
	}
	return string(tail)
}

// searchChunkGramsKept keeps the pairs of chunks made lately, by their
// hash: a long page changed again makes those of its changed chunks alone.
var searchChunkGramsKept = struct {
	sync.Mutex
	grams map[string]string
}{grams: map[string]string{}}

const searchChunkGramsKeptMost = 8192

func searchChunkGrams(hash, indexed string) string {
	searchChunkGramsKept.Lock()
	grams, known := searchChunkGramsKept.grams[hash]
	searchChunkGramsKept.Unlock()
	if known {
		return grams
	}
	grams = searchTextGrams(indexed)
	searchChunkGramsKept.Lock()
	if len(searchChunkGramsKept.grams) >= searchChunkGramsKeptMost {
		clear(searchChunkGramsKept.grams)
	}
	searchChunkGramsKept.grams[hash] = grams
	searchChunkGramsKept.Unlock()
	return grams
}

func searchChunkEndsAfter(line string) bool {
	digest := fnv.New32a()
	_, _ = digest.Write([]byte(line))
	return digest.Sum32()%4 == 0
}

// searchPageWords is what a page's row holds of its body: each word and each
// pair once, in the order they first come. Words split only where the index
// splits too (spaces and ASCII marks), so the index takes the same tokens
// from them as from the whole text.
func searchPageWords(bodyFolded string, chunks []searchChunk) string {
	seen := map[string]struct{}{}
	words := strings.Builder{}
	add := func(word string) {
		if _, repeated := seen[word]; repeated {
			return
		}
		seen[word] = struct{}{}
		words.WriteString(word)
		words.WriteByte(' ')
	}
	for _, word := range strings.FieldsFunc(bodyFolded, searchWordsSplitAt) {
		add(word)
	}
	for _, chunk := range chunks {
		for _, gram := range strings.Fields(chunk.Grams) {
			add(gram)
		}
	}
	return strings.TrimSpace(words.String())
}

func searchWordsSplitAt(r rune) bool {
	return unicode.IsSpace(r) || r < 0x80 && !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// A chunk's index row is kept apart from every page row: its own page and
// workspace tokens, which no page search names.
func workspaceSearchChunkPageToken(pageID string) string {
	return "c" + hex.EncodeToString([]byte(pageID))
}

func workspaceSearchChunkScopeToken(workspaceID string) string {
	return "x" + hex.EncodeToString([]byte(workspaceID))
}

type storedSearchChunk struct {
	Key string `db:"chunk_key"`
	No  int    `db:"chunk_no"`
	Row int64  `db:"fts_row"`
}

// writeSearchPageChunks makes a page's chunks in the index what its body is
// now. A chunk is known by what it holds (its hash, and which of the same it
// is), so one that only moved — an edit before it took or gave a line — is
// numbered again rather than written again.
func writeSearchPageChunks(app core.App, projection searchPageProjection) error {
	kept := []storedSearchChunk{}
	if err := app.DB().NewQuery(`SELECT [[chunk_key]], [[chunk_no]], [[fts_row]] FROM {{leafen_search_chunks}} WHERE [[page_id]] = {:pageId}`).
		Bind(dbx.Params{"pageId": projection.PageID}).All(&kept); err != nil {
		return err
	}
	byKey := make(map[string]storedSearchChunk, len(kept))
	for _, chunk := range kept {
		byKey[chunk.Key] = chunk
	}
	pageToken := workspaceSearchChunkPageToken(projection.PageID)
	scopeToken := workspaceSearchChunkScopeToken(projection.WorkspaceID)
	seen := map[string]int{}
	for no, chunk := range projection.Chunks {
		key := fmt.Sprintf("%s:%d", chunk.Hash, seen[chunk.Hash])
		seen[chunk.Hash]++
		if old, found := byKey[key]; found {
			delete(byKey, key)
			if old.No != no {
				if _, err := app.DB().NewQuery(`UPDATE {{leafen_search_chunks}} SET [[chunk_no]] = {:no} WHERE [[page_id]] = {:pageId} AND [[chunk_key]] = {:key}`).
					Bind(dbx.Params{"no": no, "pageId": projection.PageID, "key": key}).Execute(); err != nil {
					return err
				}
			}
			continue
		}
		if chunk.Kept {
			return errSearchMemoStale
		}
		// A chunk's row names its page by tokens alone: a page has one row in
		// the index of its own, as everything reading it by page expects.
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_search_pages_fts}} ([[page_id]], [[workspace_id]], [[page_token]], [[workspace_token]], [[title]], [[body]], [[grams]])
			VALUES ('', '', {:pageToken}, {:scopeToken}, '', {:indexed}, {:grams})
		`).Bind(dbx.Params{"pageToken": pageToken, "scopeToken": scopeToken, "indexed": chunk.Indexed, "grams": chunk.Grams}).Execute(); err != nil {
			return err
		}
		noteSearchIndexWrite(len(chunk.Indexed) + len(chunk.Grams))
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_search_chunks}} ([[page_id]], [[chunk_key]], [[chunk_no]], [[workspace_id]], [[body]], [[body_folded]], [[fts_row]], [[mid_line]])
			VALUES ({:pageId}, {:key}, {:no}, {:workspaceId}, {:body}, {:folded}, last_insert_rowid(), {:midLine})
		`).Bind(dbx.Params{"pageId": projection.PageID, "key": key, "no": no, "workspaceId": projection.WorkspaceID,
			"body": chunk.Body, "folded": chunk.BodyFolded, "midLine": chunk.MidLine}).Execute(); err != nil {
			return err
		}
	}
	// Chunks the body no longer holds go.
	for key, old := range byKey {
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_search_pages_fts}} WHERE [[rowid]] = {:row}`).
			Bind(dbx.Params{"row": old.Row}).Execute(); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_search_chunks}} WHERE [[page_id]] = {:pageId} AND [[chunk_key]] = {:key}`).
			Bind(dbx.Params{"pageId": projection.PageID, "key": key}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// deleteSearchPageChunks lets go of a page's chunks, in the index and kept.
func deleteSearchPageChunks(app core.App, pageID string) error {
	return writeSearchPageChunks(app, searchPageProjection{PageID: pageID})
}

// readSearchPageBody is a page's body as search keeps it: its chunks in
// order, a line break between them where a line ended, or the body a page
// indexed before chunks keeps.
func readSearchPageBody(app core.App, pageID string, kept string) (string, error) {
	chunks := []struct {
		Body    string `db:"body"`
		MidLine bool   `db:"mid_line"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[body]], [[mid_line]] FROM {{leafen_search_chunks}} WHERE [[page_id]] = {:pageId} ORDER BY [[chunk_no]]`).
		Bind(dbx.Params{"pageId": pageID}).All(&chunks); err != nil {
		return "", err
	}
	if len(chunks) == 0 {
		return kept, nil
	}
	body := strings.Builder{}
	for index, chunk := range chunks {
		body.WriteString(chunk.Body)
		if index < len(chunks)-1 && !chunk.MidLine {
			body.WriteByte('\n')
		}
	}
	return body.String(), nil
}
