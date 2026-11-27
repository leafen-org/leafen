package app

import (
	"bytes"
	"database/sql"
	"errors"
	"sort"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type structuralMutationBatch struct {
	doc *crdt.Doc
	// mark is where the document stood when read; the batch stores what
	// changed since.
	mark crdt.ChangeMark
	// Blocks new to the page that go first in their parent.
	starts map[string]struct{}
	// The page's blocks as read, for a page whose history has begun
	// (content_graph_structural_batch_history.go).
	history *structuralBatchHistory
	// The database rows its writes made, changed or deleted, by workspace,
	// which its save marks for search (server_save_search_marks.go).
	rows map[string]string
}

func loadStructuralMutationBatch(
	app core.App,
	pageID string,
	batches map[string]*structuralMutationBatch,
) (*structuralMutationBatch, error) {
	if batch := batches[pageID]; batch != nil {
		return batch, nil
	}
	if _, err := recoverTrailingUnownedStructuralUpdate(app, pageID); err != nil {
		return nil, err
	}
	if err := ensureYgoStreamHead(app, ygoStructuralStream, pageID); err != nil {
		return nil, err
	}
	_, parts, _, err := loadYgoStreamParts(app, ygoStructuralStream, pageID)
	if err != nil {
		return nil, err
	}
	writer := ygoServerWriters.lease(app, ygoStructuralStream, pageID)
	doc, err := writer.document(parts)
	if err != nil {
		return nil, err
	}
	writer.keepAtCommit(app, doc)
	batch := &structuralMutationBatch{doc: doc, mark: crdt.MarkChanges(doc)}
	// A body written with the page, before anyone's change, has its structure
	// from its stored blocks, as with a person's first change: read empty, a
	// block of it could not move, and what came next stood alone.
	if len(doc.GetMap(ygoBlockMetadataRoot).Keys()) == 0 {
		if err := seedStructuralGraphDocument(app, pageID, doc); err != nil {
			return nil, err
		}
	}
	if batch.history, err = beginStructuralBatchHistory(app, pageID, doc); err != nil {
		return nil, err
	}
	batches[pageID] = batch
	return batch, nil
}

// saveStructuralMutationBatches keeps what imports, migrations, duplicates
// and copies wrote to each page, in the page's history as actorID's once
// that has begun. Each page also records a server change —
// one that wrote only its databases' rows too, which its structural document
// doesn't hold — so its readers take it up and its database views read the
// change, as after anyone's edit (2026-10-14: a migration run again left a
// view's job reading the rows of the run before). The rows its writes made,
// changed or deleted are marked for search where shown, all of a save's in
// one write (server_save_search_marks.go).
func saveStructuralMutationBatches(
	app core.App,
	actorID string,
	batches map[string]*structuralMutationBatch,
) error {
	pageIDs := make([]string, 0, len(batches))
	rows := map[string]string{}
	for pageID, batch := range batches {
		pageIDs = append(pageIDs, pageID)
		for rowID, workspaceID := range batch.rows {
			rows[rowID] = workspaceID
		}
	}
	sort.Strings(pageIDs)
	for _, pageID := range pageIDs {
		batch := batches[pageID]
		update, changed := structuralBatchUpdate(batch)
		if changed {
			if len(update) > maxYgoUpdateBytes {
				return graphInputError(errorCodeGraphChangeTooLarge, "This change is too large for one change (64 MB). Make it in parts.")
			}
			if err := appendYgoStructuralUpdate(app, pageID, update); err != nil {
				return err
			}
			if err := markSearchDirtyByPageID(app, pageID); err != nil {
				return err
			}
			if err := batch.history.record(app, pageID, actorID, batch.doc, update); err != nil {
				return err
			}
		} else {
			update = []byte{0, 0}
		}
		page, err := app.FindRecordById(pagesCollection, pageID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if err := recordServerGraphChange(
			app, page.GetString("owner"), pageID, "structural-batch:"+core.GenerateDefaultRandomId(),
			update, graphMutationCounts{},
		); err != nil {
			return err
		}
	}
	return markShownSearchDirty(app, rows)
}

// structuralBatchUpdate is what changed in a batch's document. A deletion
// leaves the state vector as it was, so the update itself tells whether
// anything changed: an empty one holds no items and no deletions.
func structuralBatchUpdate(batch *structuralMutationBatch) ([]byte, bool) {
	update := crdt.EncodeChangesSinceV1(batch.doc, batch.mark)
	return update, len(update) > 0 && !bytes.Equal(update, []byte{0, 0})
}
