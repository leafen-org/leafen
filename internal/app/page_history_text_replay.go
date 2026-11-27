package app

import (
	"database/sql"
	"errors"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The history keeps each block's text as it stood at the checkpoints
// (settlePageHistoryTextSpans); typing in between writes none of it, and a
// version after a checkpoint takes the text of the blocks typed in since
// from the page replayed to that version: from the checkpoint, its typing's
// updates, whose order makes no difference. Replays are kept for a few
// versions while a snapshot of one is made a window at a time, and made
// again once a version took more typing (its newest change is later).

const pageHistoryTextReplaysKept = 16

type pageHistoryReplayedText struct {
	plain, rich string
}

type pageHistoryTextReplayKey struct {
	pageID  string
	version int
	newest  int64
}

var pageHistoryTextReplays = struct {
	sync.Mutex
	entries map[pageHistoryTextReplayKey]map[string]pageHistoryReplayedText
	order   []pageHistoryTextReplayKey
}{entries: map[pageHistoryTextReplayKey]map[string]pageHistoryReplayedText{}}

// pageHistoryReplayedTexts is the text, at a version, of each block whose
// text changed since the checkpoint before it.
func pageHistoryReplayedTexts(app core.App, pageID string, version int) (map[string]pageHistoryReplayedText, error) {
	checkpointCursor, _, err := pageHistoryCheckpointPage(app, pageID, version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil || checkpointCursor == version {
		return nil, err
	}
	events := []struct {
		Payload []byte `db:"payload"`
		Created int64  `db:"created_at"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[payload]],[[created_at]] FROM {{leafen_page_history_events}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]>{:from} AND [[version_cursor]]<={:to} AND [[kind]]={:kind}
	`).Bind(dbx.Params{
		"pageId": pageID, "from": checkpointCursor, "to": version, "kind": pageHistoryEventCollaboration,
	}).All(&events); err != nil {
		return nil, err
	}
	changed := map[string]bool{}
	key := pageHistoryTextReplayKey{pageID: pageID, version: version}
	for _, event := range events {
		envelope, err := pageHistoryEventPayload(event.Payload)
		if err != nil {
			return nil, err
		}
		for _, delta := range envelope.TextDeltas {
			changed[delta.BlockID] = true
		}
		key.newest = max(key.newest, event.Created)
	}
	if len(changed) == 0 {
		return nil, nil
	}
	pageHistoryTextReplays.Lock()
	texts, kept := pageHistoryTextReplays.entries[key]
	pageHistoryTextReplays.Unlock()
	if kept {
		return texts, nil
	}
	state, err := materializePageHistoryParts(app, pageID, version, pageHistoryReplayText)
	if err != nil {
		return nil, err
	}
	// A checkpoint taken while the page's stored text held words it couldn't
	// place yet — sent with a block change ahead of the typing they build on
	// (ygoStoredTextChanges) — kept those blocks without their text, and the
	// typing stored after it names only its own blocks: they take the text
	// the replay placed wherever it differs from the checkpoint's.
	if state.CheckpointTexts != nil {
		blocks, err := structuralMetadata(state.Structural)
		if err != nil {
			return nil, err
		}
		for blockID := range blocks {
			if !changed[blockID] && state.Collaboration.GetText(ygoBlockTextRootPrefix+blockID).ToString() != state.CheckpointTexts[blockID] {
				changed[blockID] = true
			}
		}
	}
	texts = make(map[string]pageHistoryReplayedText, len(changed))
	for blockID := range changed {
		runs := pageHistoryBlockTextRuns(state.Collaboration, blockID)
		rich, err := encodePageHistoryTextRuns(runs)
		if err != nil {
			return nil, err
		}
		texts[blockID] = pageHistoryReplayedText{plain: pageHistoryTextRunsText(runs), rich: rich}
	}
	pageHistoryTextReplays.Lock()
	defer pageHistoryTextReplays.Unlock()
	if _, kept := pageHistoryTextReplays.entries[key]; !kept {
		pageHistoryTextReplays.entries[key] = texts
		pageHistoryTextReplays.order = append(pageHistoryTextReplays.order, key)
		for len(pageHistoryTextReplays.order) > pageHistoryTextReplaysKept {
			delete(pageHistoryTextReplays.entries, pageHistoryTextReplays.order[0])
			pageHistoryTextReplays.order = pageHistoryTextReplays.order[1:]
		}
	}
	return texts, nil
}
