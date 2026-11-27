package app

import (
	"crypto/sha256"
	"sync"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A duplication copies a page's documents 32 blocks at a time. Decoded again
// for every window — the source's text, the copy's structure and text — and
// encoded whole after each, a page of 3,000 paragraphs spent almost all its
// minute decoding and kept the server busy (2026-10-16). Between windows the
// documents stay decoded here; a window uses them only while the copy's
// stored documents are still the ones the last window wrote, and otherwise
// (another server, a window rolled back, a restart) reads them again. The
// source is read once for a page's copy, as it was when the copy began.

type pageDuplicationDocuments struct {
	source, structural, text *crdt.Doc
	hasText                  bool
	structuralDigest         [32]byte
	textDigest               [32]byte
}

const maxPageDuplicationDocumentsKept = 8

var pageDuplicationDocumentsKept = struct {
	sync.Mutex
	entries map[string]*pageDuplicationDocuments
}{entries: map[string]*pageDuplicationDocuments{}}

func pageDuplicationDocumentsKey(jobID, sourcePageID string) string {
	return jobID + "\x00" + sourcePageID
}

// loadPageDuplicationDocuments gives the window its documents: those kept
// from the last window while the copy's stored documents are still theirs,
// else read again.
func loadPageDuplicationDocuments(app core.App, jobID string, item storedPageDuplicationItem) (*pageDuplicationDocuments, error) {
	key := pageDuplicationDocumentsKey(jobID, item.SourcePageID)
	pageDuplicationDocumentsKept.Lock()
	kept := pageDuplicationDocumentsKept.entries[key]
	delete(pageDuplicationDocumentsKept.entries, key)
	pageDuplicationDocumentsKept.Unlock()
	if kept != nil {
		structural, _, err := loadMaterializedYgoStructuralDocument(app, item.TargetPageID)
		if err != nil {
			return nil, err
		}
		text, _, err := loadMaterializedYgoDocument(app, item.TargetPageID)
		if err != nil {
			return nil, err
		}
		if sha256.Sum256(structural.State) == kept.structuralDigest && (!kept.hasText || sha256.Sum256(text.State) == kept.textDigest) {
			return kept, nil
		}
	}
	structural, err := loadPageDuplicationStructuralDocument(app, item.TargetPageID)
	if err != nil {
		return nil, err
	}
	source, text, hasText, err := loadPageDuplicationTextDocuments(app, item.SourcePageID, item.TargetPageID)
	if err != nil {
		return nil, err
	}
	return &pageDuplicationDocuments{source: source, structural: structural, text: text, hasText: hasText}, nil
}

// keepPageDuplicationDocuments keeps the documents for the next window with
// the states this window stored.
func keepPageDuplicationDocuments(jobID string, item storedPageDuplicationItem, documents *pageDuplicationDocuments, structural, text []byte) {
	documents.structuralDigest = sha256.Sum256(structural)
	if documents.hasText {
		documents.textDigest = sha256.Sum256(text)
	}
	pageDuplicationDocumentsKept.Lock()
	defer pageDuplicationDocumentsKept.Unlock()
	if len(pageDuplicationDocumentsKept.entries) >= maxPageDuplicationDocumentsKept {
		for other := range pageDuplicationDocumentsKept.entries {
			delete(pageDuplicationDocumentsKept.entries, other)
			break
		}
	}
	pageDuplicationDocumentsKept.entries[pageDuplicationDocumentsKey(jobID, item.SourcePageID)] = documents
}
