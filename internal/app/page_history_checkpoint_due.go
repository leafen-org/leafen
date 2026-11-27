package app

import (
	"database/sql"
	"errors"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// pageHistoryCheckpointDue is whether a version takes a checkpoint. Taken
// every 64 versions, a page typed into for hours kept a copy of itself every
// few minutes, each larger than the last (2026-10-15: 100 people typing, the
// checkpoints and the text settled at them grew with the square of the
// time); spaced by what changed since, they weigh about what was written,
// and a replay is never more than twice the page. A page that lost most of
// its blocks writes little doing so — a deletion names the blocks it takes —
// so it takes one once it holds fewer than half the blocks of its last
// (2026-11-18: a page whose 8,400 blocks went one subtree at a time replayed
// every version after from its checkpoint of 7,350, kept for up to 4,096
// versions: two fifths of a version restore).
func pageHistoryCheckpointDue(app core.App, pageID string, versionCursor int) (bool, error) {
	if versionCursor == 0 {
		return true, nil
	}
	// The last checkpoint's version first, from the checkpoints' index: 63
	// versions of 64 go by it, and only one a checkpoint may be due at reads
	// the checkpoint's size, all its texts (2026-10-18 soak: read for every
	// version, each change to a long page read megabytes).
	lastCursor := -1
	err := app.DB().NewQuery(`
		SELECT [[version_cursor]] FROM {{leafen_page_history_checkpoints}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]<{:cursor}
		ORDER BY [[version_cursor]] DESC LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "cursor": versionCursor}).Row(&lastCursor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return pageHistoryCheckpointDueSince(app, pageID, versionCursor, lastCursor)
}

// pageHistoryCheckpointDueSince is pageHistoryCheckpointDue given the last
// checkpoint before the version, -1 for none.
func pageHistoryCheckpointDueSince(app core.App, pageID string, versionCursor, lastCursor int) (bool, error) {
	if versionCursor == 0 {
		return true, nil
	}
	if lastCursor < 0 {
		return versionCursor%pageHistoryCheckpointInterval == 0, nil
	}
	since := versionCursor - lastCursor
	if since%pageHistoryCheckpointInterval != 0 {
		return false, nil
	}
	if since >= pageHistoryCheckpointSpacingMax {
		return true, nil
	}
	// What was written, not what is kept: deflated rows weigh by their
	// content, so checkpoints stay as far apart as before.
	parts := []pageHistoryStoredPart{}
	if err := app.DB().NewQuery(`
		SELECT substr(CAST([[part]] AS BLOB),1,8) AS [[head]], length(CAST([[part]] AS BLOB)) AS [[stored]] FROM (
			SELECT [[page_json]] AS [[part]] FROM {{leafen_page_history_checkpoints}} WHERE [[page_id]]={:pageId} AND [[version_cursor]]={:last}
			UNION ALL SELECT [[blocks_json]] FROM {{leafen_page_history_checkpoints}} WHERE [[page_id]]={:pageId} AND [[version_cursor]]={:last}
			UNION ALL SELECT [[structural_state]] FROM {{leafen_page_history_checkpoints}} WHERE [[page_id]]={:pageId} AND [[version_cursor]]={:last}
			UNION ALL SELECT [[collaboration_state]] FROM {{leafen_page_history_checkpoints}} WHERE [[page_id]]={:pageId} AND [[version_cursor]]={:last}
			UNION ALL SELECT [[databases_json]] FROM {{leafen_page_history_checkpoints}} WHERE [[page_id]]={:pageId} AND [[version_cursor]]={:last})
	`).Bind(dbx.Params{"pageId": pageID, "last": lastCursor}).All(&parts); err != nil {
		return false, err
	}
	lastBytes := pageHistoryWrittenBytes(parts)
	parts = parts[:0]
	if err := app.DB().NewQuery(`
		SELECT substr([[payload]],1,8) AS [[head]], length([[payload]]) AS [[stored]] FROM {{leafen_page_history_events}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]>{:from} AND [[version_cursor]]<={:cursor}
	`).Bind(dbx.Params{"pageId": pageID, "from": lastCursor, "cursor": versionCursor}).All(&parts); err != nil {
		return false, err
	}
	if pageHistoryWrittenBytes(parts) >= lastBytes {
		return true, nil
	}
	kept, err := pageHistoryCheckpointBlockCount(app, pageID, lastCursor)
	if err != nil || kept == 0 {
		return false, err
	}
	blocks := 0
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId}`).
		Bind(dbx.Params{"pageId": pageID}).Row(&blocks); err != nil {
		return false, err
	}
	return blocks*2 < kept, nil
}

// pageHistoryCheckpointBlockCounts is how many blocks a page's last
// checkpoint holds, known by the checkpoint's version and when it was taken:
// counted once from its blocks, not read again at each version one may be
// due at. It keeps the pages last asked about, up to
// pageHistoryCheckpointBlockCountsKept of them.
var pageHistoryCheckpointBlockCounts = struct {
	sync.Mutex
	pages map[string]pageHistoryCheckpointBlocks
}{pages: map[string]pageHistoryCheckpointBlocks{}}

const pageHistoryCheckpointBlockCountsKept = 1024

type pageHistoryCheckpointBlocks struct {
	cursor    int
	createdAt int64
	blocks    int
}

// pageHistoryCheckpointBlockCount is how many blocks a page's checkpoint at a
// version holds.
func pageHistoryCheckpointBlockCount(app core.App, pageID string, cursor int) (int, error) {
	var createdAt int64
	if err := app.DB().NewQuery(`SELECT [[created_at]] FROM {{leafen_page_history_checkpoints}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]={:cursor}`).
		Bind(dbx.Params{"pageId": pageID, "cursor": cursor}).Row(&createdAt); err != nil {
		return 0, err
	}
	counts := &pageHistoryCheckpointBlockCounts
	counts.Lock()
	known, found := counts.pages[pageID]
	counts.Unlock()
	if found && known.cursor == cursor && known.createdAt == createdAt {
		return known.blocks, nil
	}
	var stored []byte
	if err := app.DB().NewQuery(`SELECT CAST([[blocks_json]] AS BLOB) FROM {{leafen_page_history_checkpoints}}
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]={:cursor}`).
		Bind(dbx.Params{"pageId": pageID, "cursor": cursor}).Row(&stored); err != nil {
		return 0, err
	}
	raw, err := unpackPageHistory(stored)
	if err != nil {
		return 0, err
	}
	blocks := jsonArrayLength(raw)
	counts.Lock()
	defer counts.Unlock()
	if _, found := counts.pages[pageID]; !found && len(counts.pages) >= pageHistoryCheckpointBlockCountsKept {
		for other := range counts.pages {
			delete(counts.pages, other)
			break
		}
	}
	counts.pages[pageID] = pageHistoryCheckpointBlocks{cursor: cursor, createdAt: createdAt, blocks: blocks}
	return blocks, nil
}

// jsonArrayLength is how many values a JSON array holds, counted without
// decoding them; 0 for anything else.
func jsonArrayLength(raw []byte) int {
	depth, values, quoted, escaped, open := 0, 0, false, false, false
	for _, c := range raw {
		switch {
		case quoted:
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				quoted = false
			}
			continue
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			continue
		}
		if depth == 0 && c != '[' {
			return 0
		}
		if depth == 1 && c != ',' && c != ']' && !open {
			open = true
			values++
		}
		switch c {
		case '"':
			quoted = true
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				return values
			}
		case ',':
			if depth == 1 {
				open = false
			}
		}
	}
	return values
}
