package app

import (
	"bytes"
	"encoding/json"
	"hash/maphash"
	"slices"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page's collaborative document keeps each mention with its target's
// title as written, and the room sends the document to everyone on the page.
// So a reader's socket carries the document as that reader may read it
// (mcp_reader_mentions.go): a page or database mention they can't open comes
// with as many marks as its title had characters, in place of the title —
// no-access marks for one closed to them, not-found marks for one nothing
// answers to. The mention keeps its place and its length, so the document's
// positions, and the reader's own edits around it, stay the room's; the room
// keeps the title, which everyone who may open the target still reads. The
// reader's app shows such a mention as Notion shows it (seen 2026-11-09): a
// closed one as an eye crossed out and "사용 권한 없음", to a guest; a
// missing one as a page and "찾을 수 없음". What else of a page reaches its
// reader — its graph, its databases' rows — names such a mention with the
// same marks (content_graph_reader_mentions.go).

// closedMentionMark and missingMentionMark stand, one for each UTF-16 unit
// of its title, for a mention its reader may not open and one nothing
// answers to. They are private-use characters: no title is written with
// them.
const (
	closedMentionMark  = '\ue000'
	missingMentionMark = '\ue001'
)

// mentionMarks is a title's length in mark.
func mentionMarks(mark rune, title string) string {
	return strings.Repeat(string(mark), len(utf16.Encode([]rune(title))))
}

// closedMentionWords is a closed mention's words for its reader: its title's
// length in no-access marks.
func closedMentionWords(title string) string {
	return mentionMarks(closedMentionMark, title)
}

// closedMentionsSaid is words with each run of a mention's marks in them
// said as said says a closed (false) or missing (true) one.
func closedMentionsSaid(words string, said func(missing bool) string) string {
	if !strings.ContainsRune(words, closedMentionMark) && !strings.ContainsRune(words, missingMentionMark) {
		return words
	}
	var out strings.Builder
	var run rune
	for _, character := range words {
		if character == closedMentionMark || character == missingMentionMark {
			if character != run {
				out.WriteString(said(character == missingMentionMark))
			}
			run = character
			continue
		}
		run = 0
		out.WriteRune(character)
	}
	return out.String()
}

// closedMentionsInLine is a line of words for what shows it as words — a
// notice's email or push, an export — a mention its reader can't open as
// the app reads it: "사용 권한 없음" by the crossed-out eye where it can, or
// "찾을 수 없음" for one nothing answers to.
func closedMentionsInLine(words, language string) string {
	return closedMentionsSaid(words, func(missing bool) string {
		if missing {
			return unnamedMentionLabel(mentionMissing, language)
		}
		return unnamedMentionLabel(mentionClosed, language)
	})
}

// closedMentionLabel is what a mention or a page link closed to its reader
// says.
func closedMentionLabel(language string) string {
	if language == accountLanguageKo {
		return "사용 권한 없음"
	}
	return "No access"
}

// unnamedMentionLabel is what a mention its reader can't open says.
func unnamedMentionLabel(reach mentionReach, language string) string {
	if reach == mentionMissing {
		return missingMentionLabel(language)
	}
	return closedMentionLabel(language)
}

// missingMentionLabel is what a mention nothing answers to says.
func missingMentionLabel(language string) string {
	if language == accountLanguageKo {
		return "찾을 수 없음"
	}
	return "Not found"
}

// closedMentionsInTitle is a database page's title in words, a mention in it
// its reader can't open as Notion's inbox and path read it: "@찾을 수 없음"
// (seen 2026-11-09, a guest).
func closedMentionsInTitle(title, language string) string {
	return closedMentionsSaid(title, func(bool) string { return "@" + missingMentionLabel(language) })
}

// isClosedMentionText reports a mention's words that are its reader's
// marks, of one kind.
func isClosedMentionText(text string) bool {
	return closedMentionText(text)
}

// The format keys only a page or database mention's text carries, as an
// update writes them (a length, then the key).
var collaborationMentionKeys = []struct {
	mention string
	key     []byte
}{{"page", []byte("\x06pageId")}, {"database", []byte("\x0adatabaseId")}}

// collaborationMentionTargets is what the page and database mentions an
// update writes name, read from its bytes without decoding it: each mention
// format the update holds, its value. It may name more than the update's
// mentions — a key's bytes in other words — never fewer, so an update none
// of whose targets is closed to a reader goes to them as it is (2026-11-09:
// decoding the whole of a page of 3,000 lines with 6,000 mentions took some
// 40 ms of each opening, for a reader with nothing closed too).
func collaborationMentionTargets(update []byte) map[[2]string]bool {
	targets := map[[2]string]bool{}
	for _, mention := range collaborationMentionKeys {
		rest := update
		for {
			at := bytes.Index(rest, mention.key)
			if at < 0 {
				break
			}
			rest = rest[at+len(mention.key):]
			if len(rest) == 0 || rest[0] >= 0x80 || int(rest[0]) >= len(rest) {
				continue
			}
			value := rest[1 : 1+int(rest[0])]
			target := ""
			if json.Unmarshal(value, &target) == nil && target != "" {
				targets[[2]string{mention.mention, target}] = true
			}
		}
	}
	return targets
}

// collaborationReaderUpdates is a reader's socket's rewrite of the document
// updates the room sends them (ygo's ConnectionConfig.OutboundUpdate). It
// runs on the socket's writer, one update at a time, and remembers which
// targets it found closed until the server's access changes.
func collaborationReaderUpdates(app core.App, readerID string) func(*crdt.Doc, []byte) []byte {
	reader := pageReader(readerID)
	var closer learningCloser
	revision := int64(-1)
	return func(doc *crdt.Doc, update []byte) []byte {
		targets := collaborationMentionTargets(update)
		if len(targets) == 0 {
			return update
		}
		if current, err := currentAccessRevision(app); err != nil || current != revision || closer.closes == nil {
			revision, closer = current, reader.learningCloses(app)
		}
		// The pages the update names are worked out together (mention_closer_learning.go).
		asked := make([]mentionTarget, 0, len(targets))
		for target := range targets {
			asked = append(asked, mentionTarget{mention: target[0], id: target[1]})
		}
		closer.learn(asked)
		closes := closer.closes
		closed := []string{}
		for target := range targets {
			if closes(target[0], target[1]) != mentionOpens {
				closed = append(closed, target[0]+":"+target[1])
			}
		}
		if len(closed) == 0 {
			return update
		}
		slices.Sort(closed)
		key, kept := collaborationReadUpdates.find(update, strings.Join(closed, " "))
		if kept != nil {
			return kept
		}
		rewritten, err := crdt.RewriteTextRunsV1(doc, update, readerTextRun(closes))
		if err != nil {
			app.Logger().Warn("collaboration: a document update could not be read for its reader", "error", err)
			return update
		}
		collaborationReadUpdates.keep(key, rewritten)
		return rewritten
	}
}

// readerTextRun reads a run of a page's text as its reader reads it: a page
// or database mention they can't open in marks (unnamedMentionWords).
func readerTextRun(closes searchMentionCloser) func(crdt.TextRun) (string, bool) {
	return func(run crdt.TextRun) (string, bool) {
		if !strings.HasPrefix(run.Root, ygoBlockTextRootPrefix) {
			return "", false
		}
		mention, _ := run.Attributes["mention"].(string)
		pageID, _ := run.Attributes["pageId"].(string)
		databaseID, _ := run.Attributes["databaseId"].(string)
		// A mention whose "mention" format was cleaned away still names its target.
		if mention == "" && pageID != "" {
			mention = "page"
		} else if mention == "" && databaseID != "" {
			mention = "database"
		}
		reach := searchMentionReach(closes, mention, pageID, databaseID)
		if reach == mentionOpens {
			return "", false
		}
		return unnamedMentionWords(reach, run.Text, true), true
	}
}

// A page's whole document, as the readers to whom the same mentions are
// closed get it, is kept for the next of them who opens it before it
// changes: a few of the largest only.
const (
	collaborationReadUpdateMinBytes = 64 << 10
	collaborationReadUpdatesKept    = 16
)

type collaborationReadKey struct {
	hash   uint64
	size   int
	closed string
}

type collaborationReadUpdateCache struct {
	mu      sync.Mutex
	seed    maphash.Seed
	order   []collaborationReadKey
	updates map[collaborationReadKey][]byte
}

var collaborationReadUpdates = &collaborationReadUpdateCache{
	seed: maphash.MakeSeed(), updates: map[collaborationReadKey][]byte{},
}

// find is the key an update read with these mentions closed is kept by,
// and what is kept for it; a small update is neither kept nor looked for.
func (cache *collaborationReadUpdateCache) find(update []byte, closed string) (*collaborationReadKey, []byte) {
	if len(update) < collaborationReadUpdateMinBytes {
		return nil, nil
	}
	key := collaborationReadKey{hash: maphash.Bytes(cache.seed, update), size: len(update), closed: closed}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return &key, cache.updates[key]
}

func (cache *collaborationReadUpdateCache) keep(key *collaborationReadKey, update []byte) {
	if key == nil {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, kept := cache.updates[*key]; kept {
		return
	}
	if len(cache.order) == collaborationReadUpdatesKept {
		delete(cache.updates, cache.order[0])
		cache.order = cache.order[1:]
	}
	cache.order = append(cache.order, *key)
	cache.updates[*key] = update
}
