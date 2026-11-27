package app

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// blockMoveStructures are the structure documents one window of a move or
// copy writes, by page, with the stored size each was read at.
type blockMoveStructures struct {
	batches map[string]*structuralMutationBatch
	bytes   map[string]int
}

func newBlockMoveStructures() *blockMoveStructures {
	return &blockMoveStructures{batches: map[string]*structuralMutationBatch{}, bytes: map[string]int{}}
}

// load reads a page's structure document for the window: the one the move's
// last window left in memory, when nothing changed the page since
// (ygo_structural_document_cache.go), as a person's change takes it up; else
// from its stored parts. Read from them for every window, a long page's whole
// structure was most of each window, which held the one write connection
// (2026-11-23: two pages of 30,000 blocks, some 60 ms a window). The page's
// blocks aren't read for its history here: the move keeps that once it ends
// (recordBlockMoveHistory).
func (structures *blockMoveStructures) load(app core.App, pageID string) (*structuralMutationBatch, error) {
	if batch := structures.batches[pageID]; batch != nil {
		return batch, nil
	}
	if _, err := recoverTrailingUnownedStructuralUpdate(app, pageID); err != nil {
		return nil, err
	}
	if err := ensureYgoStreamHead(app, ygoStructuralStream, pageID); err != nil {
		return nil, err
	}
	mark, marked, err := loadYgoStreamMark(app, ygoStructuralStream, pageID)
	if err != nil {
		return nil, err
	}
	writer := ygoServerWriters.lease(app, ygoStructuralStream, pageID)
	var doc *crdt.Doc
	bytes := 0
	if marked {
		doc, bytes = writer.take(ygoStructuralDocuments, pageID, mark)
	}
	if doc == nil {
		_, parts, _, err := loadYgoStreamParts(app, ygoStructuralStream, pageID)
		if err != nil {
			return nil, err
		}
		if doc, err = writer.document(parts); err != nil {
			return nil, err
		}
		bytes = ygoStreamPartsBytes(parts)
	}
	writer.keepAtCommit(app, doc)
	batch := &structuralMutationBatch{doc: doc, mark: crdt.MarkChanges(doc)}
	// A body written with the page, before anyone's change, has its structure
	// from its stored blocks (loadStructuralMutationBatch).
	if len(doc.GetMap(ygoBlockMetadataRoot).Keys()) == 0 {
		if err := seedStructuralGraphDocument(app, pageID, doc); err != nil {
			return nil, err
		}
	}
	structures.batches[pageID] = batch
	structures.bytes[pageID] = bytes
	return batch, nil
}

// keep leaves a page's document, once the window that stored its update is
// committed, for the move's next window, under the mark the stored document
// has with it.
func (structures *blockMoveStructures) keep(app core.App, pageID string, update []byte) error {
	batch := structures.batches[pageID]
	transaction := app.TxInfo()
	if batch == nil || transaction == nil {
		return nil
	}
	after, marked, err := loadYgoStreamMark(app, ygoStructuralStream, pageID)
	if err != nil || !marked {
		return err
	}
	doc, bytes := batch.doc, structures.bytes[pageID]+len(update)
	transaction.OnComplete(func(txErr error) error {
		if txErr == nil {
			ygoStructuralDocuments.put(pageID, after, doc, bytes)
		}
		return nil
	})
	return nil
}
