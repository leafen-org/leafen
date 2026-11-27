package app

import (
	"reflect"
	"slices"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A server write to the blocks of a page whose history has begun — an
// import's turns, a Notion migration run again, its restructure, copies and
// duplications into the page — goes into that history as its writer's
// change, as a person's edit does (content_graph_mutation_owner.go): the
// structural update, the blocks it changed and the page's databases it left
// different. Written apart from the history, a migration run again added
// blocks no version showed, and text written into them later had no block
// to read back in (2026-11-15). The batch keeps the page's blocks as read,
// so the blocks a write changed are found from what it left, whichever
// operations made it; the text such writes change goes into the history at
// ygo_server_text_documents.go. Kept out of the workspace's activity, as
// the change is no one's edit there.

type structuralBatchHistory struct {
	metadata map[string]structuralBlockMetadata
	orders   map[string][]string
	// databases reads the page's databases before the write.
	databases *pageHistoryDatabaseCapture
}

// beginStructuralBatchHistory reads what the history needs before a write,
// for a page whose history has begun; nil for any other.
func beginStructuralBatchHistory(app core.App, pageID string, doc *crdt.Doc) (*structuralBatchHistory, error) {
	if started, err := pageHistoryStarted(app, pageID); err != nil || !started {
		return nil, err
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return nil, err
	}
	history := &structuralBatchHistory{}
	if history.databases, err = beginPageHistoryDatabaseCapture(app, page.GetString("owner"), pageID); err != nil {
		return nil, err
	}
	history.metadata, history.orders, err = structuralBatchBlocks(doc)
	return history, err
}

// structuralBatchBlocks are a document's blocks and each parent's order.
func structuralBatchBlocks(doc *crdt.Doc) (map[string]structuralBlockMetadata, map[string][]string, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return nil, nil, err
	}
	orders := map[string][]string{}
	read := func(parentID string) error {
		if _, done := orders[parentID]; done {
			return nil
		}
		order, err := structuralOrder(doc, parentID)
		orders[parentID] = order
		return err
	}
	if err := read(""); err != nil {
		return nil, nil, err
	}
	for _, entry := range metadata {
		if err := read(entry.ParentID); err != nil {
			return nil, nil, err
		}
	}
	return metadata, orders, nil
}

// record keeps a saved write in the page's history, and what the page now
// holds as where the batch's next write starts.
func (history *structuralBatchHistory) record(app core.App, pageID, actorID string, doc *crdt.Doc, update []byte) error {
	if history == nil {
		return nil
	}
	metadata, orders, err := structuralBatchBlocks(doc)
	if err != nil {
		return err
	}
	deltas, err := history.capture(metadata, orders).finish(app, pageID)
	if err != nil {
		return err
	}
	databases, err := history.databases.finish(app)
	if err != nil {
		return err
	}
	if err := recordPageHistoryGraphMutation(
		app, pageID, actorID, update, deltas, databases, nil, workspaceActivityGraphContext{Maintenance: true},
	); err != nil {
		return err
	}
	history.metadata, history.orders = metadata, orders
	history.databases, err = beginPageHistoryDatabaseCapture(app, history.databases.owner, pageID)
	return err
}

// capture is the write as a person's edit's capture would have it: the
// blocks made, changed, moved or deleted, and the parents whose order it
// changed.
func (history *structuralBatchHistory) capture(
	metadata map[string]structuralBlockMetadata, orders map[string][]string,
) *pageHistoryGraphCapture {
	capture := &pageHistoryGraphCapture{
		parents: map[string]struct{}{}, changedIDs: map[string]struct{}{}, deletedIDs: map[string]struct{}{},
		movedIDs: map[string]struct{}{}, placedIn: map[string]struct{}{},
	}
	place := func(parentID string) {
		capture.parents[parentID] = struct{}{}
		capture.placedIn[parentID] = struct{}{}
	}
	for id, entry := range metadata {
		previous, existed := history.metadata[id]
		switch {
		case !existed:
			capture.changedIDs[id] = struct{}{}
			place(entry.ParentID)
		case previous.ParentID != entry.ParentID:
			capture.changedIDs[id], capture.movedIDs[id] = struct{}{}, struct{}{}
			place(previous.ParentID)
			place(entry.ParentID)
		case previous.Type != entry.Type || !reflect.DeepEqual(previous.Content, entry.Content):
			capture.changedIDs[id] = struct{}{}
			capture.parents[entry.ParentID] = struct{}{}
		}
	}
	for id, previous := range history.metadata {
		if _, exists := metadata[id]; !exists {
			capture.deletedIDs[id] = struct{}{}
			capture.parents[previous.ParentID] = struct{}{}
		}
	}
	for parentID, order := range orders {
		if !slices.Equal(history.orders[parentID], order) {
			place(parentID)
		}
	}
	return capture
}
