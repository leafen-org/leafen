package app

import (
	"container/list"
	"sync"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page's server changes go into its documents under one writer number per
// page and stream, so a page the server edits many times keeps one writer
// rather than gaining one per change (2026-10-16: a ten-hour run left a page
// written 1,713 times with 1,706 writers, and the conflict scan of every
// change grew with them).
//
// A writer continues from the clock its items reach in the stored document,
// which is safe only while nothing outside that document holds a later item
// of it. The server keeps the clock each page's writer reached when its last
// change was committed, and the writer goes on only while the stored document
// reaches exactly that clock: a snapshot replaced whole, a dropped trailing
// update, a restore, a restart or a writer no longer kept takes a new random
// number, as every change did before. Within one transaction a page's writer
// goes to one document only — a second document of the page opened beside it
// writes under a random number, since two documents from the same stored
// state would give two items one ID. Outside a transaction nothing is kept.

// ygoServerWritersKept bounds the pages whose writer the server keeps; a page
// past it takes a new writer at its next change.
const ygoServerWritersKept = 8192

type ygoServerWriterEntry struct {
	key    string
	client crdt.ClientID
	clock  uint64
}

type ygoServerWriterBook struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List
	leased  map[string]struct{}
}

var ygoServerWriters = &ygoServerWriterBook{
	entries: map[string]*list.Element{}, order: list.New(), leased: map[string]struct{}{},
}

// ygoServerWriter is the number a transaction writes a page's server change
// under.
type ygoServerWriter struct {
	key    string
	leased bool
	known  bool
	client crdt.ClientID
	clock  uint64
}

func ygoServerWriterKey(stream ygoStreamTables, room string) string {
	return stream.head + "\x00" + room
}

// lease is the page's writer for the one document the transaction writes the
// page's stream in.
func (book *ygoServerWriterBook) lease(app core.App, stream ygoStreamTables, room string) ygoServerWriter {
	writer := ygoServerWriter{key: ygoServerWriterKey(stream, room)}
	transaction := app.TxInfo()
	if transaction == nil {
		return writer
	}
	book.mu.Lock()
	if _, held := book.leased[writer.key]; held {
		book.mu.Unlock()
		return writer
	}
	book.leased[writer.key] = struct{}{}
	if element := book.entries[writer.key]; element != nil {
		entry := element.Value.(*ygoServerWriterEntry)
		writer.known, writer.client, writer.clock = true, entry.client, entry.clock
	}
	book.mu.Unlock()
	writer.leased = true
	transaction.OnComplete(func(error) error {
		book.mu.Lock()
		delete(book.leased, writer.key)
		book.mu.Unlock()
		return nil
	})
	return writer
}

func (book *ygoServerWriterBook) remember(key string, client crdt.ClientID, clock uint64) {
	book.mu.Lock()
	defer book.mu.Unlock()
	if element := book.entries[key]; element != nil {
		entry := element.Value.(*ygoServerWriterEntry)
		entry.client, entry.clock = client, clock
		book.order.MoveToFront(element)
		return
	}
	book.entries[key] = book.order.PushFront(&ygoServerWriterEntry{key: key, client: client, clock: clock})
	for book.order.Len() > ygoServerWritersKept {
		oldest := book.order.Back()
		book.order.Remove(oldest)
		delete(book.entries, oldest.Value.(*ygoServerWriterEntry).key)
	}
}

// goesOn reports whether the document is the page's stored one under its
// kept writer, at the clock the writer's last change reached.
func (writer ygoServerWriter) goesOn(doc *crdt.Doc) bool {
	return writer.leased && writer.known && doc.ClientID() == writer.client &&
		doc.StateVector()[writer.client] == writer.clock
}

// take is the page's document a change just left in the cache, and its
// stored size, when the stored document still has the mark and it writes
// under the page's writer.
func (writer ygoServerWriter) take(cache *ygoDocumentCache, room string, mark int64) (*crdt.Doc, int) {
	doc, bytes := cache.take(room, mark)
	if doc == nil || !writer.goesOn(doc) {
		return nil, 0
	}
	return doc, bytes
}

// document is the page's document read from its stored parts, under the
// page's writer when it may go on and else under a new random number.
func (writer ygoServerWriter) document(parts [][]byte) (*crdt.Doc, error) {
	state, err := ygoStreamState(parts)
	if err != nil {
		return nil, err
	}
	if writer.leased && writer.known {
		doc := crdt.New(crdt.WithClientID(writer.client))
		if err := applyYgoState(doc, state); err != nil {
			return nil, err
		}
		if writer.goesOn(doc) {
			return doc, nil
		}
	}
	doc := crdt.New()
	if err := applyYgoState(doc, state); err != nil {
		return nil, err
	}
	return doc, nil
}

// keep has the page's writer go on from where the document now is once the
// transaction is committed; a document under a new number makes that number
// the page's writer.
func (writer ygoServerWriter) keep(app core.App, doc *crdt.Doc) {
	transaction := app.TxInfo()
	if !writer.leased || transaction == nil {
		return
	}
	client := doc.ClientID()
	clock := doc.StateVector()[client]
	transaction.OnComplete(func(txErr error) error {
		if txErr == nil {
			ygoServerWriters.remember(writer.key, client, clock)
		}
		return nil
	})
}

// keepAtCommit has the page's writer go on from where the document is when
// the transaction is committed, for a document no one else takes up before
// then: what its holders wrote in it and stored, the stored document has.
func (writer ygoServerWriter) keepAtCommit(app core.App, doc *crdt.Doc) {
	transaction := app.TxInfo()
	if !writer.leased || transaction == nil {
		return
	}
	transaction.OnComplete(func(txErr error) error {
		if txErr == nil {
			client := doc.ClientID()
			ygoServerWriters.remember(writer.key, client, doc.StateVector()[client])
		}
		return nil
	})
}

// ygoStreamState is a stream's stored parts as one update.
func ygoStreamState(parts [][]byte) ([]byte, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return crdt.MergeUpdatesV1(parts...)
}

func applyYgoState(doc *crdt.Doc, state []byte) error {
	if len(state) == 0 {
		return nil
	}
	return crdt.ApplyUpdateV1(doc, state, nil)
}
