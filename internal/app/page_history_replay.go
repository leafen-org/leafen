package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type pageHistoryBlockTimes struct {
	CreatedAt int64
	UpdatedAt int64
}

type pageHistoryMaterializedState struct {
	Page          pageHistoryPageState
	Structural    *crdt.Doc
	Collaboration *crdt.Doc
	BlockTimes    map[string]pageHistoryBlockTimes
	// Databases are the page's databases, by data source.
	Databases map[string]pageHistoryDatabaseState
	// CheckpointTexts is each block's text at the checkpoint the replay
	// began from, when that checkpoint kept text the page couldn't place yet
	// (page_history_text_replay.go); nil otherwise.
	CheckpointTexts map[string]string
	// blocks keeps Structural's block metadata between replayed events
	// (page_history_replay_blocks.go).
	blocks *pageHistoryReplayBlocks
}

// pageHistoryReplayParts are the parts of a version a replay rebuilds. The
// structure with the databases and the text document change apart (no
// structure update or database change touches the text, and no text update
// the structure), so a reader of one part replays that part alone: a version
// preview of a 3,900-block page replayed the page whole twice, for its
// typing and for its databases (2026-11-18).
type pageHistoryReplayParts int

const (
	// pageHistoryReplayWhole is the page, its structure and databases, its
	// text and its blocks' times.
	pageHistoryReplayWhole pageHistoryReplayParts = iota
	// pageHistoryReplayStructure is the page, its structure and databases.
	pageHistoryReplayStructure
	// pageHistoryReplayText is the page and its text document, while the
	// checkpoint's holds no words it couldn't place (CheckpointTexts, which
	// reads the structure): a version whose checkpoint's does is replayed
	// whole.
	pageHistoryReplayText
)

func materializePageHistoryVersion(
	app core.App, pageID string, versionCursor int,
) (pageHistoryMaterializedState, error) {
	return materializePageHistoryParts(app, pageID, versionCursor, pageHistoryReplayWhole)
}

func materializePageHistoryParts(
	app core.App, pageID string, versionCursor int, parts pageHistoryReplayParts,
) (pageHistoryMaterializedState, error) {
	checkpoint, err := loadPageHistoryCheckpointParts(app, pageID, versionCursor, parts)
	if err != nil {
		return pageHistoryMaterializedState{}, err
	}
	state := pageHistoryMaterializedState{
		Structural: crdt.New(), Collaboration: crdt.New(),
		BlockTimes: map[string]pageHistoryBlockTimes{},
	}
	if err := json.Unmarshal([]byte(checkpoint.PageJSON), &state.Page); err != nil {
		return pageHistoryMaterializedState{}, err
	}
	checkpointBlocks := []pageHistoryBlockState{}
	if parts == pageHistoryReplayWhole {
		if err := json.Unmarshal([]byte(checkpoint.BlocksJSON), &checkpointBlocks); err != nil {
			return pageHistoryMaterializedState{}, err
		}
	}
	for _, block := range checkpointBlocks {
		state.BlockTimes[block.ID] = pageHistoryBlockTimes{
			CreatedAt: block.CreatedAt, UpdatedAt: block.UpdatedAt,
		}
	}
	if parts != pageHistoryReplayText {
		if err := crdt.ApplyUpdateV1(state.Structural, checkpoint.StructuralState, nil); err != nil {
			return pageHistoryMaterializedState{}, err
		}
	}
	if parts != pageHistoryReplayStructure {
		if err := crdt.ApplyUpdateV1(state.Collaboration, checkpoint.CollaborationState, nil); err != nil {
			return pageHistoryMaterializedState{}, err
		}
	}
	if state.Collaboration.PendingStats().Items > 0 {
		if parts == pageHistoryReplayText {
			return materializePageHistoryParts(app, pageID, versionCursor, pageHistoryReplayWhole)
		}
		state.CheckpointTexts = make(map[string]string, len(checkpointBlocks))
		for _, block := range checkpointBlocks {
			state.CheckpointTexts[block.ID] = block.PlainText
		}
	}
	if parts != pageHistoryReplayText {
		if state.Databases, err = pageHistoryCheckpointDatabases(app, checkpoint); err != nil {
			return pageHistoryMaterializedState{}, err
		}
	}
	events, err := loadPageHistoryEventsUpTo(app, pageID, checkpoint.VersionCursor, versionCursor, pageHistoryCheckpointSpacingMax)
	if err != nil {
		return pageHistoryMaterializedState{}, err
	}
	if len(events) > pageHistoryCheckpointSpacingMax {
		return pageHistoryMaterializedState{}, errors.New(errorCodePageHistoryFailed)
	}
	expected := checkpoint.VersionCursor + 1
	for _, event := range events {
		if event.VersionCursor != expected {
			return pageHistoryMaterializedState{}, errors.New(errorCodePageHistoryFailed)
		}
		// Each part replays its own updates.
		skipped := (parts == pageHistoryReplayText && event.Kind == pageHistoryEventGraph) ||
			(parts == pageHistoryReplayStructure && event.Kind == pageHistoryEventCollaboration)
		if !skipped {
			if err := applyPageHistoryEvent(&state, event); err != nil {
				return pageHistoryMaterializedState{}, err
			}
		}
		expected++
	}
	if expected != versionCursor+1 {
		return pageHistoryMaterializedState{}, errors.New(errorCodePageHistoryFailed)
	}
	if parts == pageHistoryReplayText {
		return state, nil
	}
	if err := normalizePageHistoryDuplicateOrders(&state); err != nil {
		return pageHistoryMaterializedState{}, err
	}
	if err := normalizeLegacySimpleTablesInPageHistoryState(&state); err != nil {
		return pageHistoryMaterializedState{}, err
	}
	return state, nil
}

// normalizePageHistoryDuplicateOrders lets go of a block named twice in its
// parent's order, keeping the first. A page whose history began from blocks
// its baseline seeded on its own, and whose structure was seeded again when
// it was first written structurally (an agent's page made with content,
// then changed), replays those blocks twice (2026-10-16: opening such a
// version failed as a structure conflict).
func normalizePageHistoryDuplicateOrders(state *pageHistoryMaterializedState) error {
	metadata, err := structuralMetadata(state.Structural)
	if err != nil {
		return err
	}
	parents := map[string]struct{}{"": {}}
	for id := range metadata {
		parents[id] = struct{}{}
	}
	for parentID := range parents {
		root := blockOrderRoot(parentID)
		seen := map[string]struct{}{}
		duplicates := []int{}
		for index, value := range state.Structural.GetArray(root).ToSlice() {
			id, _ := value.(string)
			if _, twice := seen[id]; twice {
				duplicates = append(duplicates, index)
				continue
			}
			seen[id] = struct{}{}
		}
		if len(duplicates) == 0 {
			continue
		}
		state.Structural.Transact(func(transaction *crdt.Transaction) {
			order := transaction.GetArray(root)
			for index := len(duplicates) - 1; index >= 0; index-- {
				order.Delete(transaction, duplicates[index], 1)
			}
		})
	}
	return nil
}

func normalizeLegacySimpleTablesInPageHistoryState(state *pageHistoryMaterializedState) error {
	metadata, err := structuralMetadata(state.Structural)
	if err != nil {
		return err
	}
	for tableID, entry := range metadata {
		if entry.Type != legacySimpleTableBlockType {
			continue
		}
		children, err := structuralOrder(state.Structural, tableID)
		if err != nil || len(children) != 0 {
			return errors.Join(errors.New(errorCodePageHistoryFailed), err)
		}
		raw, err := json.Marshal(entry.Content)
		if err != nil {
			return err
		}
		tableContent, rowContents, err := nativeStructuredTableContents(raw)
		if err != nil {
			return err
		}
		encodedTable, err := json.Marshal(tableContent)
		if err != nil {
			return err
		}
		canonicalTable := map[string]any{}
		if json.Unmarshal(encodedTable, &canonicalTable) != nil {
			return errors.New(errorCodePageHistoryFailed)
		}
		rowIDs := make([]string, len(rowContents))
		rowMetadata := make([]map[string]any, len(rowContents))
		for index, content := range rowContents {
			rowID := nativeStructuredTableRowID(tableID, index)
			if _, collision := metadata[rowID]; collision {
				return errors.New(errorCodePageHistoryFailed)
			}
			encodedRow, err := json.Marshal(content)
			if err != nil {
				return err
			}
			canonicalRow := map[string]any{}
			if json.Unmarshal(encodedRow, &canonicalRow) != nil {
				return errors.New(errorCodePageHistoryFailed)
			}
			rowIDs[index] = rowID
			rowMetadata[index] = map[string]any{
				"parentId": tableID, "type": graphBlockTypeTableRow, "content": canonicalRow,
			}
			state.BlockTimes[rowID] = state.BlockTimes[tableID]
		}
		state.Structural.Transact(func(transaction *crdt.Transaction) {
			transaction.GetMap(ygoBlockMetadataRoot).Set(transaction, tableID, map[string]any{
				"parentId": entry.ParentID, "type": graphBlockTypeTable, "content": canonicalTable,
			})
			for index, rowID := range rowIDs {
				transaction.GetMap(ygoBlockMetadataRoot).Set(transaction, rowID, rowMetadata[index])
			}
			transaction.GetArray(blockOrderRoot(tableID)).Insert(transaction, 0, stringsToAny(rowIDs))
		})
	}
	return nil
}

// applyPageHistoryEvent replays an event onto a version's state.
func applyPageHistoryEvent(state *pageHistoryMaterializedState, event storedPageHistoryEvent) error {
	switch event.Kind {
	case pageHistoryEventGraph:
		envelope, err := pageHistoryEventPayload(event.Payload)
		if err != nil {
			return err
		}
		if err := applyPageHistoryStructureUpdate(state, envelope, event.CreatedAt); err != nil {
			return err
		}
	case pageHistoryEventCollaboration:
		update, err := pageHistoryEventUpdate(event.Payload)
		if err != nil {
			return err
		}
		if err := crdt.ApplyUpdateV1(state.Collaboration, update, nil); err != nil {
			return err
		}
	case pageHistoryEventPage, pageHistoryEventMetadata:
		if err := json.Unmarshal(event.Payload, &state.Page); err != nil {
			return err
		}
	case pageHistoryEventRestore, pageHistoryEventRewrite:
		// These versions keep their whole state; replay starts at them.
		return errors.New(errorCodePageHistoryFailed)
	case pageHistoryEventAgentNote:
		// A note changes nothing on the page.
	default:
		return errors.New(errorCodePageHistoryFailed)
	}
	return nil
}

func pageHistoryEventUpdate(payload []byte) ([]byte, error) {
	envelope, err := pageHistoryEventPayload(payload)
	return envelope.Update, err
}

// pageHistoryEventPayload reads an event's envelope; an event from before
// envelopes is its update alone.
func pageHistoryEventPayload(payload []byte) (pageHistoryEventEnvelope, error) {
	payload, err := unpackPageHistory(payload)
	if err != nil {
		return pageHistoryEventEnvelope{}, err
	}
	envelope := pageHistoryEventEnvelope{}
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.Format == 0 {
		return pageHistoryEventEnvelope{Update: payload}, nil
	}
	if envelope.Format != 1 || len(envelope.Update) == 0 {
		return pageHistoryEventEnvelope{}, errors.New(errorCodePageHistoryFailed)
	}
	return envelope, nil
}

func pageHistoryMetadataEqual(a, b structuralBlockMetadata) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && string(left) == string(right)
}

func pageHistoryBlocks(state pageHistoryMaterializedState) ([]pageHistoryBlockWire, error) {
	metadata, err := structuralMetadata(state.Structural)
	if err != nil {
		return nil, err
	}
	parents := map[string]struct{}{"": {}}
	for _, entry := range metadata {
		parents[entry.ParentID] = struct{}{}
	}
	parentIDs := make([]string, 0, len(parents))
	for parentID := range parents {
		parentIDs = append(parentIDs, parentID)
	}
	sort.Strings(parentIDs)
	blocks := make([]pageHistoryBlockWire, 0, len(metadata))
	seen := map[string]struct{}{}
	for _, parentID := range parentIDs {
		order, err := structuralOrder(state.Structural, parentID)
		if err != nil {
			return nil, err
		}
		for index, id := range order {
			entry, exists := metadata[id]
			if !exists || entry.ParentID != parentID {
				return nil, errors.New(errorCodePageHistoryFailed)
			}
			content, err := json.Marshal(entry.Content)
			if err != nil {
				return nil, err
			}
			times := state.BlockTimes[id]
			blocks = append(blocks, pageHistoryBlockWire{
				ID: id, ParentID: parentID, Position: fmt.Sprintf("%020d", index+1),
				Type: entry.Type, Content: content,
				PlainText: state.Collaboration.GetText(ygoBlockTextRootPrefix + id).ToString(),
				CreatedAt: times.CreatedAt, UpdatedAt: times.UpdatedAt,
			})
			seen[id] = struct{}{}
		}
	}
	if len(seen) != len(metadata) {
		return nil, errors.New(errorCodePageHistoryFailed)
	}
	return blocks, nil
}
