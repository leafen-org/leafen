package app

import (
	"bytes"
	"encoding/json"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	ygoBlockOrderRootPrefix = "leafen:block-order:"
	ygoBlockMetadataRoot    = "leafen:block-meta"
	ygoRootParentToken      = "root"
)

type storedStructuralBlock struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
	Position string `db:"position"`
	Type     string `db:"type"`
	Content  string `db:"content"`
}

type structuralBlockMetadata struct {
	ParentID string
	Type     string
	Content  map[string]any
}

func blockOrderRoot(parentID string) string {
	if parentID == "" {
		parentID = ygoRootParentToken
	}
	return ygoBlockOrderRootPrefix + parentID
}

func seedStructuralGraphDocument(app core.App, pageID string, doc *crdt.Doc) error {
	if len(doc.GetMap(ygoBlockMetadataRoot).Keys()) != 0 {
		return graphStructureAuthorityError()
	}
	cursorParent, cursorPosition, cursorID := "", "", ""
	for {
		blocks := []storedStructuralBlock{}
		err := app.DB().NewQuery(`
			SELECT [[id]], [[parent_id]], [[position]], [[type]], [[content]]
			FROM {{leafen_blocks}}
			WHERE [[page_id]] = {:pageId} AND (
				[[parent_id]] > {:cursorParent}
				OR ([[parent_id]] = {:cursorParent} AND [[position]] > {:cursorPosition})
				OR ([[parent_id]] = {:cursorParent} AND [[position]] = {:cursorPosition} AND [[id]] > {:cursorId})
			)
			ORDER BY [[parent_id]], [[position]], [[id]]
			LIMIT {:limit}
		`).Bind(dbx.Params{
			"pageId": pageID, "cursorParent": cursorParent,
			"cursorPosition": cursorPosition, "cursorId": cursorID,
			"limit": structuralGraphUpgradeWindow,
		}).All(&blocks)
		if err != nil {
			return err
		}
		if len(blocks) == 0 {
			return nil
		}
		var seedErr error
		doc.Transact(func(txn *crdt.Transaction) {
			metadata := txn.GetMap(ygoBlockMetadataRoot)
			for _, block := range blocks {
				content := map[string]any{}
				if err := json.Unmarshal([]byte(block.Content), &content); err != nil {
					seedErr = err
					return
				}
				metadata.Set(txn, block.ID, map[string]any{
					"parentId": block.ParentID, "type": block.Type, "content": content,
				})
				txn.GetArray(blockOrderRoot(block.ParentID)).Push(txn, []any{block.ID})
			}
		})
		if seedErr != nil {
			return seedErr
		}
		last := blocks[len(blocks)-1]
		cursorParent, cursorPosition, cursorID = last.ParentID, last.Position, last.ID
		if len(blocks) < structuralGraphUpgradeWindow {
			return nil
		}
	}
}

func structuralMetadata(doc *crdt.Doc) (map[string]structuralBlockMetadata, error) {
	result := map[string]structuralBlockMetadata{}
	for id, value := range doc.GetMap(ygoBlockMetadataRoot).Entries() {
		entry, err := structuralMetadataEntry(value)
		if err != nil {
			return nil, err
		}
		result[id] = entry
	}
	return result, nil
}

func structuralSnapshot(doc *crdt.Doc) ([]byte, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return nil, err
	}
	parents := map[string]struct{}{"": {}}
	for _, entry := range metadata {
		parents[entry.ParentID] = struct{}{}
	}
	orders := map[string][]string{}
	for parentID := range parents {
		order, err := structuralOrder(doc, parentID)
		if err != nil {
			return nil, err
		}
		orders[parentID] = order
	}
	return json.Marshal(struct {
		Metadata map[string]structuralBlockMetadata `json:"metadata"`
		Orders   map[string][]string                `json:"orders"`
	}{Metadata: metadata, Orders: orders})
}

func rejectReservedStructuralUpdate(current, update []byte) error {
	beforeDoc := crdt.New()
	if err := crdt.ApplyUpdateV1(beforeDoc, current, nil); err != nil {
		return err
	}
	before, err := structuralSnapshot(beforeDoc)
	if err != nil {
		return err
	}
	afterDoc := crdt.New()
	if err := crdt.ApplyUpdateV1(afterDoc, current, nil); err != nil {
		return err
	}
	if err := crdt.ApplyUpdateV1(afterDoc, update, nil); err != nil {
		return err
	}
	after, err := structuralSnapshot(afterDoc)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, after) {
		return graphStructureAuthorityError()
	}
	return nil
}

func structuralOrder(doc *crdt.Doc, parentID string) ([]string, error) {
	values := doc.GetArray(blockOrderRoot(parentID)).ToSlice()
	result := make([]string, len(values))
	seen := map[string]struct{}{}
	for index, value := range values {
		id, ok := value.(string)
		if !ok || id == "" {
			return nil, graphStructureAuthorityError()
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, graphStructureAuthorityError()
		}
		seen[id] = struct{}{}
		result[index] = id
	}
	return result, nil
}

func structuralIndex(values []string, id string) int {
	for index, value := range values {
		if value == id {
			return index
		}
	}
	return -1
}

func moveStructuralID(doc *crdt.Doc, blockID, oldParent, newParent string, afterID *string) error {
	oldOrder, err := structuralOrder(doc, oldParent)
	if err != nil {
		return err
	}
	from := structuralIndex(oldOrder, blockID)
	if from < 0 {
		return graphStructureAuthorityError()
	}
	if oldParent == newParent {
		remaining := append([]string(nil), oldOrder[:from]...)
		remaining = append(remaining, oldOrder[from+1:]...)
		to := 0
		if afterID != nil {
			after := structuralIndex(remaining, *afterID)
			if after < 0 || *afterID == blockID {
				return graphInputError(errorCodeGraphReferenceInvalid, "Block order anchor not found.")
			}
			to = after + 1
		}
		next := append(remaining, "")
		copy(next[to+1:], next[to:])
		next[to] = blockID
		doc.Transact(func(txn *crdt.Transaction) {
			order := txn.GetArray(blockOrderRoot(oldParent))
			order.Delete(txn, 0, len(oldOrder))
			order.Insert(txn, 0, stringsToAny(next))
		})
		return nil
	}
	newOrder, err := structuralOrder(doc, newParent)
	if err != nil {
		return err
	}
	insertAt := 0
	if afterID != nil {
		after := structuralIndex(newOrder, *afterID)
		if after < 0 {
			return graphInputError(errorCodeGraphReferenceInvalid, "Block order anchor not found.")
		}
		insertAt = after + 1
	}
	doc.Transact(func(txn *crdt.Transaction) {
		txn.GetArray(blockOrderRoot(oldParent)).Delete(txn, from, 1)
		txn.GetArray(blockOrderRoot(newParent)).Insert(txn, insertAt, []any{blockID})
	})
	return nil
}

func stringsToAny(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func insertStructuralID(doc *crdt.Doc, blockID, parentID string, afterID *string) error {
	order, err := structuralOrder(doc, parentID)
	if err != nil {
		return err
	}
	insertAt := 0
	if afterID != nil {
		after := structuralIndex(order, *afterID)
		if after < 0 {
			return graphInputError(errorCodeGraphReferenceInvalid, "Block order anchor not found.")
		}
		insertAt = after + 1
	}
	doc.Transact(func(txn *crdt.Transaction) {
		txn.GetArray(blockOrderRoot(parentID)).Insert(txn, insertAt, []any{blockID})
	})
	return nil
}

// structuralOrderView keeps the block order of each parent a change works
// in: read from the document and checked once, then kept as the change
// inserts and deletes there (2026-10-15: read and checked again for every
// block an agent's 500-block batch added, each batch slower as the page
// grew, holding the one write connection). A parent whose order changes
// any other way is read again.
type structuralOrderView struct {
	doc    *crdt.Doc
	orders map[string]*structuralOrderEntry
}

type structuralOrderEntry struct {
	ids  []string
	seen map[string]struct{}
}

func newStructuralOrderView(doc *crdt.Doc) *structuralOrderView {
	return &structuralOrderView{doc: doc, orders: map[string]*structuralOrderEntry{}}
}

func (view *structuralOrderView) entry(parentID string) (*structuralOrderEntry, error) {
	if entry := view.orders[parentID]; entry != nil {
		return entry, nil
	}
	ids, err := structuralOrder(view.doc, parentID)
	if err != nil {
		return nil, err
	}
	entry := &structuralOrderEntry{ids: ids, seen: make(map[string]struct{}, len(ids))}
	for _, id := range ids {
		entry.seen[id] = struct{}{}
	}
	view.orders[parentID] = entry
	return entry, nil
}

// indexOf is where a block stands in its parent's order; -1 when it isn't
// there. An anchor is most often near the end (blocks appended), so the
// order is searched from there.
func (view *structuralOrderView) indexOf(parentID, blockID string) (int, error) {
	entry, err := view.entry(parentID)
	if err != nil {
		return -1, err
	}
	if _, found := entry.seen[blockID]; !found {
		return -1, nil
	}
	for index := len(entry.ids) - 1; index >= 0; index-- {
		if entry.ids[index] == blockID {
			return index, nil
		}
	}
	return -1, nil
}

// insert puts a new block into its parent's order after another, or first.
func (view *structuralOrderView) insert(blockID, parentID string, afterID *string) error {
	entry, err := view.entry(parentID)
	if err != nil {
		return err
	}
	if _, duplicate := entry.seen[blockID]; duplicate {
		return graphStructureAuthorityError()
	}
	insertAt := 0
	if afterID != nil {
		after, err := view.indexOf(parentID, *afterID)
		if err != nil {
			return err
		}
		if after < 0 {
			return graphInputError(errorCodeGraphReferenceInvalid, "Block order anchor not found.")
		}
		insertAt = after + 1
	}
	view.doc.Transact(func(txn *crdt.Transaction) {
		txn.GetArray(blockOrderRoot(parentID)).Insert(txn, insertAt, []any{blockID})
	})
	entry.ids = append(entry.ids, "")
	copy(entry.ids[insertAt+1:], entry.ids[insertAt:])
	entry.ids[insertAt] = blockID
	entry.seen[blockID] = struct{}{}
	return nil
}

// removed says the block at an index left its parent's order.
func (view *structuralOrderView) removed(parentID string, index int) {
	entry := view.orders[parentID]
	if entry == nil || index < 0 || index >= len(entry.ids) {
		delete(view.orders, parentID)
		return
	}
	delete(entry.seen, entry.ids[index])
	entry.ids = append(entry.ids[:index], entry.ids[index+1:]...)
}

// forget has the parents' orders read again when next asked.
func (view *structuralOrderView) forget(parentIDs ...string) {
	for _, parentID := range parentIDs {
		delete(view.orders, parentID)
	}
}

func collectStructuralSubtree(metadata map[string]structuralBlockMetadata, root string) ([]string, error) {
	children := map[string][]string{}
	for id, entry := range metadata {
		children[entry.ParentID] = append(children[entry.ParentID], id)
	}
	for parent := range children {
		sort.Strings(children[parent])
	}
	result := []string{}
	queue := []string{root}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		result = append(result, id)
		queue = append(queue, children[id]...)
	}
	return result, nil
}

func applyStructuralGraphMutation(
	app core.App,
	doc *crdt.Doc,
	owner, workspaceID, pageID string,
	actorID string,
	operations []graphOperation,
	appendOnly bool,
	explicitStartBlockIDs map[string]struct{},
) (int, error) {
	affectedParents := map[string]struct{}{}
	containerAppended := false
	appendProjection := newStructuralAppendProjection(appendOnly)
	deletedCount := 0
	deletedInBatch := map[string]struct{}{}
	byPositionPlaces := newPositionPlaces()
	// The blocks' metadata is read once and kept as each operation changes
	// it (2026-10-14: read again for every operation, a page's import slowed
	// as the page grew).
	metadata := newStructuralMetadataView(doc)
	orders := newStructuralOrderView(doc)
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			block := operation.Block
			parentID := ""
			if block.ParentID != nil {
				parentID = *block.ParentID
			}
			existing, exists, err := metadata.get(block.ID)
			if err != nil {
				return 0, err
			}
			afterID := block.AfterID
			_, explicitStart := explicitStartBlockIDs[block.ID]
			byPosition := !exists && afterID == nil && !explicitStart
			if byPosition {
				// Where a position alone puts it (content_graph_position_place.go).
				afterID, err = byPositionPlaces.after(app, orders, owner, pageID, parentID, block.Position, deletedInBatch)
				if err != nil {
					return 0, err
				}
			}
			projectionPosition := ""
			appended := false
			if !exists {
				projectionPosition, appended, err = appendProjection.insert(
					app, doc, owner, pageID, block.ID, parentID, afterID,
				)
				if err != nil {
					return 0, err
				}
				if appended {
					orders.forget(parentID)
				}
			}
			// A later block of this change placed by position finds this one by
			// the position it was given, whatever place it took.
			if byPosition {
				byPositionPlaces.note(parentID, block.Position, block.ID)
			}
			if !exists && !appended {
				if err := orders.insert(block.ID, parentID, afterID); err != nil {
					return 0, err
				}
				// Until the order is published below, the new row keeps its given
				// position, so a later block of this change placed only by position
				// follows it instead of whichever unplaced row has the largest id.
				projectionPosition = block.Position
			} else if exists && (existing.ParentID != parentID || afterID != nil || explicitStart) {
				// A block just appended already stands after the block it
				// follows (2026-10-14: moved there again, every appended block
				// rewrote its parent's whole order).
				if err := moveStructuralID(doc, block.ID, existing.ParentID, parentID, afterID); err != nil {
					return 0, err
				}
				orders.forget(existing.ParentID, parentID)
				affectedParents[existing.ParentID] = struct{}{}
			}
			var content map[string]any
			if err := json.Unmarshal(block.Content, &content); err != nil {
				return 0, err
			}
			// A suggested block stays suggested through other changes to it,
			// and a block's suggested break stays at its end.
			if suggestionID, _ := existing.Content[graphBlockSuggestionKey].(string); exists && suggestionID != "" {
				if block.Content, err = keepGraphBlockSuggestion(content, existing.Content, suggestionID); err != nil {
					return 0, err
				}
			}
			if splitID, _ := existing.Content[graphBlockSplitSuggestionKey].(string); exists && splitID != "" {
				content[graphBlockSplitSuggestionKey] = splitID
				content[graphBlockSplitBlockKey] = existing.Content[graphBlockSplitBlockKey]
				if block.Content, err = json.Marshal(content); err != nil {
					return 0, err
				}
			}
			doc.Transact(func(txn *crdt.Transaction) {
				txn.GetMap(ygoBlockMetadataRoot).Set(txn, block.ID, map[string]any{
					"parentId": parentID, "type": block.Type, "content": content,
				})
			})
			metadata.set(block.ID, structuralBlockMetadata{ParentID: parentID, Type: block.Type, Content: content})
			if err := upsertStructuralBlockProjectionAt(
				app, owner, workspaceID, pageID, actorID, block, parentID, projectionPosition,
			); err != nil {
				return 0, err
			}
			// Its row among the databases, as every write of a block writes
			// it (database_block_return.go).
			if err := projectDatabaseBlock(
				app, owner, workspaceID, pageID, block.ID, block.Type, parentID, block.Position, block.Content,
			); err != nil {
				return 0, err
			}
			// A database appended takes its place from its block below, as one
			// whose parent's order is published does.
			containerAppended = containerAppended || (appended && block.Type == graphBlockTypeInlineDatabase)
			if err := replaceProductFileBlockReference(app, workspaceID, pageID, block); err != nil {
				return 0, err
			}
			if !appended {
				affectedParents[parentID] = struct{}{}
			}
		case graphOperationKindMoveBlock:
			move := operation.Move
			existing, _, err := metadata.get(move.BlockID)
			if err != nil {
				return 0, err
			}
			oldParentID := existing.ParentID
			parentID := ""
			if move.ParentID != nil {
				parentID = *move.ParentID
			}
			if err := moveStructuralID(doc, move.BlockID, existing.ParentID, parentID, move.AfterID); err != nil {
				return 0, err
			}
			orders.forget(existing.ParentID, parentID)
			existing.ParentID = parentID
			doc.Transact(func(txn *crdt.Transaction) {
				txn.GetMap(ygoBlockMetadataRoot).Set(txn, move.BlockID, map[string]any{
					"parentId": parentID, "type": existing.Type, "content": existing.Content,
				})
			})
			metadata.set(move.BlockID, existing)
			if _, err := app.DB().NewQuery(`
				UPDATE {{leafen_blocks}}
				SET [[parent_id]] = {:parentId}, [[last_edited_by]] = {:actorId}, [[updated]] = {:updated}
				WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] = {:id}
			`).Bind(dbx.Params{
				"parentId": parentID, "actorId": actorID, "updated": time.Now().UTC().UnixMilli(),
				"owner": owner, "pageId": pageID, "id": move.BlockID,
			}).Execute(); err != nil {
				return 0, err
			}
			affectedParents[oldParentID] = struct{}{}
			affectedParents[parentID] = struct{}{}
		case graphOperationKindDeleteBlock:
			// A parent deletion already removes its descendants. A later
			// deletion of one of those IDs in this same batch has no effect.
			if _, deleted := deletedInBatch[operation.Deletion.BlockID]; deleted {
				if _, exists, err := metadata.get(operation.Deletion.BlockID); err != nil {
					return 0, err
				} else if !exists {
					continue
				}
			}
			whole, err := metadata.all()
			if err != nil {
				return 0, err
			}
			ids, err := collectStructuralSubtree(whole, operation.Deletion.BlockID)
			if err != nil {
				return 0, err
			}
			// Read before the view lets the blocks go (structuralMetadataView.remove).
			if err := rememberUnavailableDatabaseBlocks(app, workspaceID, pageID, whole, ids); err != nil {
				return 0, err
			}
			for _, id := range ids {
				entry := whole[id]
				index, err := orders.indexOf(entry.ParentID, id)
				if err != nil {
					return 0, err
				}
				if index < 0 {
					return 0, graphStructureAuthorityError()
				}
				textLength := doc.GetText(ygoBlockTextRootPrefix + id).Len()
				doc.Transact(func(txn *crdt.Transaction) {
					txn.GetArray(blockOrderRoot(entry.ParentID)).Delete(txn, index, 1)
					txn.GetMap(ygoBlockMetadataRoot).Delete(txn, id)
					if textLength > 0 {
						txn.GetText(ygoBlockTextRootPrefix+id).Delete(txn, 0, textLength)
					}
				})
				orders.removed(entry.ParentID, index)
				deletedInBatch[id] = struct{}{}
			}
			for _, id := range ids {
				metadata.remove(id)
			}
			if err := deleteProductFileBlockReferences(app, workspaceID, pageID, ids); err != nil {
				return 0, err
			}
			if err := deleteDatabaseContainerBlocks(app, ids, actorID); err != nil {
				return 0, err
			}
			if err := deleteStructuralBlockProjections(app, owner, pageID, ids); err != nil {
				return 0, err
			}
			deletedCount += len(ids)
		}
	}
	for parentID := range affectedParents {
		if err := publishStructuralOrderProjection(app, doc, owner, pageID, parentID, actorID); err != nil {
			return 0, err
		}
	}
	if len(affectedParents) > 0 || containerAppended {
		if err := refreshDatabaseContainerPlacements(app, owner, pageID); err != nil {
			return 0, err
		}
	}
	if err := keepFullPageDatabaseBody(app, doc, pageID); err != nil {
		return 0, err
	}
	return deletedCount, nil
}
