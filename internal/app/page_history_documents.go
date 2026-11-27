package app

import (
	"database/sql"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// pageHistoryDocuments hands a page's documents between the steps of one
// transaction that reads them more than once. A version restore took a
// checkpoint, settled the history's text at it, found the blocks made since,
// replaced both documents, settled the trash, took a checkpoint again and
// set the history's spans from it: it decoded the page's text six times and
// its structure six, each whole (2026-11-18: a page holding 400 KB of text
// and 580 KB of structure answered a restore in 1.5 s on a NAS-class
// server; read once each and checked once each as written, 1.1 s). A
// document read is kept with the mark its stored one has
// (ygo_document_marks.go) and handed out again only while the stored one
// still has it: a write to it, by any step, gives it a new mark, and it is
// read afresh. A step that changes a document takes it out first (take) and
// keeps it again once it stored it whole (stored), as the stored state
// decodes to it. The checkpoint a step has just taken goes to the step
// right after it, which settles or sets the history's spans at it, without
// reading it back and decoding it again. A nil one keeps nothing: each step
// reads the page as stored.
type pageHistoryDocuments struct {
	kept       map[string]pageHistoryKeptDocument
	checkpoint *pageHistoryTakenCheckpoint
}

type pageHistoryKeptDocument struct {
	mark  int64
	doc   *crdt.Doc
	state []byte
}

// pageHistoryTakenCheckpoint is a checkpoint as a step has just stored it:
// its blocks as written and the text document its state is.
type pageHistoryTakenCheckpoint struct {
	pageID        string
	version       int
	blocksJSON    string
	collaboration *crdt.Doc
}

func newPageHistoryDocuments() *pageHistoryDocuments {
	return &pageHistoryDocuments{kept: map[string]pageHistoryKeptDocument{}}
}

func pageHistoryDocumentKey(stream ygoStreamTables, room string) string {
	return stream.head + "\x00" + room
}

// read is loadYgoStreamDoc: the document kept while the stored one has the
// mark it was kept with, else the stored one, kept.
func (documents *pageHistoryDocuments) read(
	app core.App, stream ygoStreamTables, room string,
) (*crdt.Doc, []byte, bool, error) {
	if documents == nil {
		return loadYgoStreamDoc(app, stream, room)
	}
	key := pageHistoryDocumentKey(stream, room)
	mark, marked, err := loadYgoStreamMark(app, stream, room)
	if err != nil {
		return nil, nil, false, err
	}
	if kept, found := documents.kept[key]; found && marked && kept.mark == mark {
		return kept.doc, kept.state, true, nil
	}
	delete(documents.kept, key)
	doc, state, found, err := loadYgoStreamDoc(app, stream, room)
	if err == nil && found && marked {
		documents.kept[key] = pageHistoryKeptDocument{mark: mark, doc: doc, state: state}
	}
	return doc, state, found, err
}

// checkedState is read with the stored state checked as
// loadMaterializedYgoStream checks it: one that decodes.
func (documents *pageHistoryDocuments) checkedState(
	app core.App, stream ygoStreamTables, room string,
) (*crdt.Doc, []byte, bool, error) {
	doc, state, found, err := documents.read(app, stream, room)
	if err == nil && found && len(state) == 0 {
		err = validateYgoState(state)
	}
	return doc, state, found, err
}

// take is read for a step that changes the document: it is kept no longer.
func (documents *pageHistoryDocuments) take(
	app core.App, stream ygoStreamTables, room string,
) (*crdt.Doc, bool, error) {
	doc, _, found, err := documents.read(app, stream, room)
	if documents != nil {
		delete(documents.kept, pageHistoryDocumentKey(stream, room))
		documents.checkpoint = nil
	}
	return doc, found, err
}

// stored keeps a document a step changed and stored whole as state.
func (documents *pageHistoryDocuments) stored(
	app core.App, stream ygoStreamTables, room string, doc *crdt.Doc, state []byte,
) error {
	if documents == nil {
		return nil
	}
	mark, marked, err := loadYgoStreamMark(app, stream, room)
	if err == nil && marked {
		documents.kept[pageHistoryDocumentKey(stream, room)] = pageHistoryKeptDocument{mark: mark, doc: doc, state: state}
	}
	return err
}

// took notes the checkpoint a step stored, given how its insert went: one
// that was there already at the version stays as it was, and is read back.
func (documents *pageHistoryDocuments) took(
	result sql.Result, err error, pageID string, version int, blocksJSON string, collaboration *crdt.Doc,
) error {
	if err != nil || documents == nil {
		return err
	}
	documents.checkpoint = nil
	if added, err := result.RowsAffected(); err != nil || added != 1 {
		return err
	}
	documents.checkpoint = &pageHistoryTakenCheckpoint{
		pageID: pageID, version: version, blocksJSON: blocksJSON, collaboration: collaboration,
	}
	return nil
}

// checkpointAt is the page's last checkpoint at or before a version, as
// loadPageHistoryCheckpoint reads it — its blocks as written and its text
// document — or none when exact and it isn't at the version: the one a
// step has just taken at the version, else the stored one.
func (documents *pageHistoryDocuments) checkpointAt(
	app core.App, pageID string, version int, exact bool,
) (string, *crdt.Doc, bool, error) {
	if documents != nil && documents.checkpoint != nil {
		taken := documents.checkpoint
		documents.checkpoint = nil
		if taken.pageID == pageID && taken.version == version {
			return taken.blocksJSON, taken.collaboration, true, nil
		}
	}
	checkpoint, err := loadPageHistoryCheckpoint(app, pageID, version)
	if err != nil || exact && checkpoint.VersionCursor != version {
		return "", nil, false, err
	}
	collaboration, err := pageHistoryDocument(checkpoint.CollaborationState)
	return checkpoint.BlocksJSON, collaboration, err == nil, err
}

// capturePageHistoryCheckpointAndSettle takes a page's checkpoint at a
// version and settles the history's text at it, from the checkpoint as
// taken.
func capturePageHistoryCheckpointAndSettle(
	app core.App, workspaceID, pageID string, version int, documents *pageHistoryDocuments,
) error {
	if documents == nil {
		documents = newPageHistoryDocuments()
	}
	if err := capturePageHistoryCheckpoint(app, pageID, version, documents); err != nil {
		return err
	}
	return settlePageHistoryTextSpans(app, workspaceID, pageID, version, documents)
}

// capturePageHistoryCheckpointAndProject takes a page's checkpoint at a
// version and sets the history's spans from it as taken
// (replacePageHistoryProjectionAtVersion).
func capturePageHistoryCheckpointAndProject(
	app core.App, pageID string, version int, documents *pageHistoryDocuments,
) error {
	if documents == nil {
		documents = newPageHistoryDocuments()
	}
	if err := capturePageHistoryCheckpoint(app, pageID, version, documents); err != nil {
		return err
	}
	return replacePageHistoryProjectionAtVersion(app, pageID, version, documents)
}
