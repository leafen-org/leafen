package app

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page's collaborative text as a server write changes it apart from the
// realtime editor: an import's turns and the mentions it resolves, a Notion
// migration, imported people mapped to members. The blocks such writes
// change go into the history at content_graph_structural_batch_history.go. Each page's document is read once for the write and
// stored at its end, and a page whose history has begun gets the text the
// write changed in its history, as the writer's change, as the editor's
// typing does (ygo_persistence.go). Stored apart from the history, an
// imported page's text was missing from every version, and the edits made
// to it later could not be read back: their changes need the text they
// change (2026-11-13: a Markdown page's later edits showed nothing in its
// updates).

type notionYgoDocument struct {
	doc *crdt.Doc
	// stored is where the document stood as stored when read, which a save
	// adds what changed since to; nil when nothing was stored yet.
	stored *crdt.ChangeMark
	// found says a document was stored when read; bytes is its stored size.
	found bool
	bytes int
	// writer is the number the import writes the page's text under.
	writer ygoServerWriter
	// history holds the text changes made since the last save, where the
	// page's history had begun when the document was read.
	history *ygoServerTextHistory
}

type ygoServerTextHistory struct {
	changes []crdt.TextChange
	stop    func()
}

func loadNotionYgoDocument(app core.App, pageID string, documents map[string]*notionYgoDocument) (*notionYgoDocument, error) {
	if document := documents[pageID]; document != nil {
		return document, nil
	}
	document := &notionYgoDocument{doc: crdt.New()}
	document.writer = ygoServerWriters.lease(app, ygoCollaborationStream, pageID)
	mark, marked, err := loadYgoStreamMark(app, ygoCollaborationStream, pageID)
	if err != nil {
		return nil, err
	}
	var cached *crdt.Doc
	if marked {
		// The document an import's turn just left, when nothing changed it
		// since (ygo_structural_document_cache.go).
		cached, document.bytes = document.writer.take(ygoTextDocuments, pageID, mark)
	}
	if cached != nil {
		document.doc, document.found = cached, true
	} else {
		// The stored text goes into the document once, which checks it.
		_, parts, found, err := loadYgoStreamParts(app, ygoCollaborationStream, pageID)
		if err != nil {
			return nil, err
		}
		if found {
			if document.doc, err = document.writer.document(parts); err != nil {
				return nil, err
			}
			document.found, document.bytes = true, ygoStreamPartsBytes(parts)
		}
	}
	if document.found {
		mark := crdt.MarkChanges(document.doc)
		document.stored = &mark
	}
	if err := watchYgoServerTextHistory(app, pageID, document); err != nil {
		return nil, err
	}
	documents[pageID] = document
	return document, nil
}

// watchYgoServerTextHistory notes which blocks' text the write changes, for
// a page whose history has begun; a page an import is still making has its
// history begin once its first turn is kept. The note ends with the write's
// transaction: the document may be kept for the page's next write.
func watchYgoServerTextHistory(app core.App, pageID string, document *notionYgoDocument) error {
	started, err := pageHistoryStarted(app, pageID)
	if err != nil || !started {
		return err
	}
	history := &ygoServerTextHistory{}
	history.stop = document.doc.OnUpdateDetails(func(details crdt.UpdateDetails) {
		history.changes = append(history.changes, details.TextChanges...)
	})
	document.history = history
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(error) error {
			history.stop()
			return nil
		})
	}
	return nil
}

// saveNotionYgoDocuments stores each document: what changed in it since it
// was read, after what was stored, or the whole document when nothing was
// (2026-10-14: the whole document stored again on each turn of a long
// page's import grew each turn's work and the server's memory with the
// page). The text it changed goes into the page's history as actorID's,
// and is searched for anew where shown (server_save_search_marks.go).
func saveNotionYgoDocuments(app core.App, actorID string, documents map[string]*notionYgoDocument) error {
	written := map[string]string{}
	for pageID, document := range documents {
		whole := document.stored == nil
		change := []byte(nil)
		if !whole {
			update := crdt.EncodeChangesSinceV1(document.doc, *document.stored)
			change = update
			changed, err := appendYgoUpdateIfChanged(app, pageID, update)
			// A change too large for one update, or a page whose updates wait
			// to be compacted, stores the whole document as before.
			if err != nil && (err.Error() == errorCodeYgoUpdateTooLarge || err.Error() == errorCodeYgoPendingBacklogFull) {
				whole, err = true, nil
			}
			if err != nil {
				return err
			}
			if changed {
				publishStoredYgoUpdateAfterCommit(app, pageID, update)
				document.bytes += len(update)
				written[pageID] = ""
			}
			// The document stays for the page's next turn, once this one is
			// kept, under the mark the stored one has with it.
			if transaction := app.TxInfo(); !whole && transaction != nil && document.found {
				mark, marked, err := loadYgoStreamMark(app, ygoCollaborationStream, pageID)
				if err != nil {
					return err
				}
				doc, bytes := document.doc, document.bytes
				transaction.OnComplete(func(txErr error) error {
					if txErr == nil && marked {
						ygoTextDocuments.put(pageID, mark, doc, bytes)
					}
					return nil
				})
			}
		}
		if whole {
			state := crdt.EncodeStateAsUpdateV1(document.doc, nil)
			if change == nil {
				change = state
			}
			if err := replaceYgoSnapshot(app, pageID, state); err != nil {
				return err
			}
			written[pageID] = ""
		}
		if err := recordYgoServerTextHistory(app, pageID, actorID, document, change); err != nil {
			return err
		}
		document.writer.keep(app, document.doc)
		mark := crdt.MarkChanges(document.doc)
		document.stored = &mark
	}
	return markShownSearchDirty(app, written)
}

// recordYgoServerTextHistory records the text a save stored in the page's
// history, where it changed any.
func recordYgoServerTextHistory(app core.App, pageID, actorID string, document *notionYgoDocument, change []byte) error {
	history := document.history
	if history == nil {
		return nil
	}
	changes := history.changes
	history.changes = nil
	if app.TxInfo() == nil {
		history.stop()
		document.history = nil
	}
	if len(changes) == 0 || len(change) == 0 {
		return nil
	}
	return recordPageHistoryCollaborationUpdate(app, pageID, actorID, change, changes)
}
