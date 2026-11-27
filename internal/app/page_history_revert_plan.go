package app

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Reverting changes takes each block and page field that the reverted
// versions changed back to how it was before them, while the changes others
// made stay: a block or field someone else changed afterwards is kept as it
// is, and the revert names it. The history is replayed from the first
// reverted version, so what each version changed is read from the states
// themselves, whatever kind of change made it. A database's rows are taken
// back the same way (page_history_revert_rows.go); what the database itself
// became is left, and named.

const (
	pageHistoryRevertChangedLater    = "changedLater"
	pageHistoryRevertHoldsLaterBlock = "holdsLaterBlocks"
	pageHistoryRevertDatabase        = "database"
)

var errPageHistoryRevertTooFar = errors.New(errorCodePageHistoryRevertTooFar)

// pageHistoryRevertBlock is a block as one state of the page holds it.
type pageHistoryRevertBlock struct {
	Present  bool
	ParentID string
	AfterID  string
	Type     string
	Content  json.RawMessage
	Spans    []graphTextSpan
	// key is what the block is, apart from where it sits among its siblings.
	key string
}

type pageHistoryRevertPlan struct {
	// Blocks are the blocks to take back, each to its state before the
	// reverted changes, beside its state now.
	Blocks  map[string]pageHistoryRevertBlock
	Current map[string]pageHistoryRevertBlock
	// Moved are the blocks whose place the reverted changes changed.
	Moved map[string]bool
	// Fields are the page's fields to take back, by name, to their values
	// before the reverted changes; Page is the page now.
	Fields map[string]json.RawMessage
	Page   pageHistoryPageState
	// Rows are the database rows to take back, by ID.
	Rows map[string]*pageHistoryRevertRow
	Kept []pageHistoryRevertKept
	// Views are the views each linked database's block owned before the
	// reverted changes took it away, and Linked the linked databases' blocks
	// the revert brings back or takes away as blocks, with their views
	// (page_history_revert_linked_views.go).
	Views  map[string][]graphDatabaseView
	Linked map[string]bool
}

// pageHistoryRevertFields reads a page state's fields one by one, each
// database property value as its own.
func pageHistoryRevertFields(page pageHistoryPageState) (map[string]json.RawMessage, error) {
	encoded, err := json.Marshal(page)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	delete(fields, "values")
	for propertyID, value := range page.Values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields["values."+propertyID] = encoded
	}
	return fields, nil
}

// planPageHistoryRevert plans taking back the changes of the versions
// reverts picks, from the version at from through the latest, frontier.
func planPageHistoryRevert(
	app core.App, pageID string, from, frontier int, reverts func(storedPageHistoryVersion) bool,
) (pageHistoryRevertPlan, error) {
	plan := pageHistoryRevertPlan{
		Blocks: map[string]pageHistoryRevertBlock{}, Moved: map[string]bool{},
		Fields: map[string]json.RawMessage{}, Views: map[string][]graphDatabaseView{},
	}
	if frontier-from+1 > pageHistoryMaxReplayEvents {
		return plan, errPageHistoryRevertTooFar
	}
	start, err := pageHistoryStartCursor(app, pageID)
	if err != nil {
		return plan, err
	}
	if from <= start || from > frontier {
		return plan, errPageHistoryVersionMissing
	}
	// The replay starts where the first reverted version's write began, its
	// earlier versions replayed and not reverted (pageHistoryWriteText).
	replayFrom, err := pageHistoryWriteStart(app, pageID, from, start)
	if err != nil {
		return plan, err
	}
	versions := []storedPageHistoryVersion{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[version_cursor]],[[actor_id]],[[source]],[[created_at]],
			[[connection_name]],[[connection_id]]
		FROM {{leafen_page_history_versions}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]>={:from} AND [[version_cursor]]<={:frontier}
		ORDER BY [[version_cursor]]
	`).Bind(dbx.Params{"pageId": pageID, "from": replayFrom, "frontier": frontier}).All(&versions); err != nil {
		return plan, err
	}
	events, err := loadPageHistoryEvents(app, pageID, replayFrom-1, frontier)
	if err != nil {
		return plan, err
	}
	if len(events) != len(versions) {
		return plan, errors.New(errorCodePageHistoryFailed)
	}
	state, err := materializePageHistoryVersion(app, pageID, replayFrom-1)
	if err != nil {
		return plan, err
	}
	// The tracker keeps the blocks as the replay goes (page_history_revert_blocks.go).
	tracker := &pageHistoryRevertTracker{}
	if _, err := tracker.sync(state); err != nil {
		return plan, err
	}
	// writeBlocks are the blocks as the write the replay is in found them,
	// those it changed so far.
	writeBlocks := map[string]pageHistoryRevertBlock{}
	fields, err := pageHistoryRevertFields(state.Page)
	if err != nil {
		return plan, err
	}
	// A thing (a block or a field) is open while the reverted versions were
	// the last to change it; before holds its state from before them.
	open := map[string]bool{}
	touched := map[string]bool{}
	beforeBlocks := map[string]pageHistoryRevertBlock{}
	beforeFields := map[string]json.RawMessage{}
	databases := map[string]bool{}
	rows := map[string]*pageHistoryRevertRow{}
	for index, version := range versions {
		event := events[index]
		if event.VersionCursor != version.VersionCursor {
			return plan, errors.New(errorCodePageHistoryFailed)
		}
		if index > 0 && !pageHistorySameWrite(versions[index-1], version) {
			clear(writeBlocks)
		}
		reverted := version.VersionCursor >= from && reverts(version)
		moved := map[string]bool{}
		// The databases the event changes, as they were before it and after.
		prior, changedDatabases := map[string]*pageHistoryDatabaseState{}, pageHistoryDatabaseChanges(nil)
		switch event.Kind {
		case pageHistoryEventRestore, pageHistoryEventRewrite:
			prior = pageHistoryPriorDatabases(state, nil)
			if state, err = materializePageHistoryVersion(app, pageID, version.VersionCursor); err != nil {
				return plan, err
			}
		default:
			if event.Kind == pageHistoryEventGraph {
				envelope, err := pageHistoryEventPayload(event.Payload)
				if err != nil {
					return plan, err
				}
				for _, delta := range envelope.GraphDeltas {
					moved[delta.BlockID] = delta.Moved
				}
				if len(envelope.Databases) > 0 {
					prior, changedDatabases = pageHistoryPriorDatabases(state, envelope.Databases), envelope.Databases
				}
				for _, row := range envelope.Rows {
					notePageHistoryRevertRow(rows, row, reverted)
				}
			}
			if err := applyPageHistoryEvent(&state, event); err != nil {
				return plan, err
			}
		}
		changes, err := tracker.sync(state)
		if err != nil {
			return plan, err
		}
		// A database the reverted versions changed stays as it is, named;
		// the views of a linked database they added or took away come and
		// go with its block.
		if reverted {
			linked := pageHistoryLinkedBlocksChanged(changes)
			for sourceID, after := range changedDatabases {
				if !pageHistoryOnlyLinkedViewsChanged(prior[sourceID], after, linked) {
					databases[sourceID] = true
				}
			}
		}
		nextFields, err := pageHistoryRevertFields(state.Page)
		if err != nil {
			return plan, err
		}
		changed := func(thing string, prior func()) {
			if !reverted {
				open[thing] = false
				return
			}
			touched[thing] = true
			if !open[thing] {
				open[thing] = true
				prior()
			}
		}
		// A block the version moved counts though it reads the same.
		listed := make(map[string]struct{}, len(changes))
		for _, change := range changes {
			listed[change.id] = struct{}{}
		}
		for id, moves := range moved {
			block, placed := tracker.blocks[id]
			if _, seen := listed[id]; moves && placed && !seen {
				changes = append(changes, pageHistoryRevertChange{id: id, before: block, after: block})
			}
		}
		for _, change := range changes {
			id, before, after := change.id, change.before, change.after
			if _, kept := writeBlocks[id]; !kept {
				writeBlocks[id] = before
			}
			if before.Present == after.Present && before.key == after.key && !moved[id] {
				continue
			}
			at := writeBlocks[id]
			changed("block:"+id, func() {
				beforeBlocks[id] = pageHistoryWriteText(before, after, at)
				if before.Present && pageHistoryLinkedBlock(before) {
					plan.Views[id] = pageHistoryViewsOwnedBy(id, state.Databases, prior)
				}
			})
			if reverted && moved[id] {
				plan.Moved[id] = true
			}
		}
		for name := range unionKeys(fields, nextFields) {
			before, after := fields[name], nextFields[name]
			if pageHistorySameJSON(string(before), string(after)) {
				continue
			}
			changed("field:"+name, func() { beforeFields[name] = before })
		}
		fields, plan.Page = nextFields, state.Page
	}
	plan.Current = tracker.blocks
	if len(versions) == 0 {
		plan.Page = state.Page
	}
	things := make([]string, 0, len(touched))
	for thing := range touched {
		things = append(things, thing)
	}
	sort.Strings(things)
	for _, thing := range things {
		kind, name := thing[:5], thing[6:]
		switch {
		case kind == "block" && open[thing]:
			plan.Blocks[name] = beforeBlocks[name]
		case kind == "block":
			plan.Kept = append(plan.Kept, pageHistoryRevertKept{BlockID: name, Reason: pageHistoryRevertChangedLater})
		case open[thing]:
			plan.Fields[name] = beforeFields[name]
		default:
			plan.Kept = append(plan.Kept, pageHistoryRevertKept{Field: name, Reason: pageHistoryRevertChangedLater})
		}
	}
	plan.Rows = map[string]*pageHistoryRevertRow{}
	for id, row := range rows {
		if row.open {
			plan.Rows[id] = row
		} else {
			plan.Kept = append(plan.Kept, pageHistoryRevertKept{Field: id, Reason: pageHistoryRevertChangedLater})
		}
	}
	sources := make([]string, 0, len(databases))
	for sourceID := range databases {
		sources = append(sources, sourceID)
	}
	sort.Strings(sources)
	for _, sourceID := range sources {
		plan.Kept = append(plan.Kept, pageHistoryRevertKept{Field: sourceID, Reason: pageHistoryRevertDatabase})
	}
	return plan, nil
}

func pageHistoryRevertBlockKey(parentID, blockType string, content, text []byte) string {
	return parentID + "\x00" + blockType + "\x00" + string(content) + "\x00" + string(text)
}

// pageHistoryWriteVersionsRead is how far back a write's versions are looked
// for: one write records a version for each kind of change it makes.
const pageHistoryWriteVersionsRead = 16

// pageHistorySameWrite is whether two versions are one write's: one write
// is one moment in a page's history (page_history_moments.go), by one
// person through one connection.
func pageHistorySameWrite(left, right storedPageHistoryVersion) bool {
	return left.Source != pageHistoryVersionBaseline && right.Source != pageHistoryVersionBaseline &&
		left.CreatedAt == right.CreatedAt && left.ActorID == right.ActorID &&
		left.ConnectionID == right.ConnectionID && left.ConnectionName == right.ConnectionName
}

// pageHistoryWriteStart is the cursor of the first version of the write the
// version at cursor is part of, after the history's start.
func pageHistoryWriteStart(app core.App, pageID string, cursor, start int) (int, error) {
	versions := []storedPageHistoryVersion{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[version_cursor]],[[actor_id]],[[source]],[[created_at]],
			[[connection_name]],[[connection_id]]
		FROM {{leafen_page_history_versions}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]>{:start} AND [[version_cursor]]<={:cursor}
		ORDER BY [[version_cursor]] DESC LIMIT {:limit}
	`).Bind(dbx.Params{
		"pageId": pageID, "start": start, "cursor": cursor, "limit": pageHistoryWriteVersionsRead,
	}).All(&versions); err != nil {
		return 0, err
	}
	first := cursor
	for index := 1; index < len(versions) && versions[0].VersionCursor == cursor; index++ {
		if versions[index].VersionCursor != first-1 || !pageHistorySameWrite(versions[0], versions[index]) {
			break
		}
		first = versions[index].VersionCursor
	}
	return first, nil
}

// pageHistoryWriteText is a block as it stood before a version changed it
// from before to after; at is the block as the version's write found it. A
// block's removal empties its text first, as a version of its own at the
// removal's moment: the content graph empties a removed block's text
// (applyContentGraphTextUpdates), and the page's editor empties it in its
// collaborative text, stored with the removal. So a block a version removes
// empty comes back from the revert with the text its write found it with;
// reverted alone, the removal brought its block back empty, where restoring
// the version before it kept its text (2026-11-17).
func pageHistoryWriteText(before, after, at pageHistoryRevertBlock) pageHistoryRevertBlock {
	if !before.Present || after.Present || len(before.Spans) > 0 || !at.Present || at.Type != before.Type {
		return before
	}
	text, err := json.Marshal(at.Spans)
	if err != nil {
		return before
	}
	before.Spans = at.Spans
	before.key = pageHistoryRevertBlockKey(before.ParentID, before.Type, before.Content, text)
	return before
}

func unionKeys[V any](left, right map[string]V) map[string]struct{} {
	keys := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		keys[key] = struct{}{}
	}
	for key := range right {
		keys[key] = struct{}{}
	}
	return keys
}
