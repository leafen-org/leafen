package app

import (
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a Notion move reads or makes now, by the items' own names, for
// Leafen's import window to show live (user decision 2026-11-23 "묶어서
// 올려", a screen of Leafen's own apart from Notion's import window, which
// shows a bar alone): every stage — the scan, the settings reading in the
// person's Notion, the run's reading, making, comments and check — the names
// passing about a second apart, as a copy window's do, with what is left
// under them. The person follows the move there, where an agent relayed its
// counts a minute apart before. Only a name read or made is noted — none is
// made up — so a stage that stops leaves its last name standing. It is kept
// in memory: a server started again shows names from its next item on, and
// the counts stay the stored ones.

const (
	// The names kept, newest last, and how often one is taken: about one a
	// second, the window's pace, whatever the stage's own.
	notionMigrationLiveItems = 8
	notionMigrationLiveGap   = 800 * time.Millisecond
	// Past this with nothing noted, nothing shows as going on.
	notionMigrationLiveFresh = 20 * time.Second
	// A name as long as a line of the window shows.
	notionMigrationLiveNameRunes = 120
)

type notionMigrationLiveState struct {
	mu   sync.Mutex
	live notionMigrationLive
	last time.Time
}

var notionMigrationLives sync.Map

func notionMigrationLiveStateOf(migrationID string) *notionMigrationLiveState {
	value, _ := notionMigrationLives.LoadOrStore(migrationID, &notionMigrationLiveState{})
	return value.(*notionMigrationLiveState)
}

// notionMigrationLiveDue says whether a stage's next name would be taken now,
// so a name read for it alone isn't read in vain.
func notionMigrationLiveDue(migrationID string) bool {
	if migrationID == "" {
		return false
	}
	state := notionMigrationLiveStateOf(migrationID)
	state.mu.Lock()
	defer state.mu.Unlock()
	return time.Since(state.last) >= notionMigrationLiveGap
}

// noteNotionMigrationLive notes the item a migration's stage is on now: a
// page, database or row by its name. A stage's first name starts it anew.
func noteNotionMigrationLive(migrationID, stage, kind, name string) {
	name = strings.Join(strings.Fields(name), " ")
	if migrationID == "" || name == "" {
		return
	}
	if runes := []rune(name); len(runes) > notionMigrationLiveNameRunes {
		name = string(runes[:notionMigrationLiveNameRunes-1]) + "…"
	}
	state := notionMigrationLiveStateOf(migrationID)
	now := time.Now()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.live.Stage != stage {
		state.live, state.last = notionMigrationLive{Stage: stage, Items: []notionMigrationLiveItem{}}, time.Time{}
	}
	state.live.At = now.UnixMilli()
	if now.Sub(state.last) < notionMigrationLiveGap {
		return
	}
	state.last = now
	items := append(state.live.Items, notionMigrationLiveItem{Name: name, Kind: kind, At: now.UnixMilli()})
	if len(items) > notionMigrationLiveItems {
		items = items[len(items)-notionMigrationLiveItems:]
	}
	state.live.Items = items
}

// noteNotionMigrationLiveCounts notes how far a stage that counts itself is
// — the settings reading, which Leafen doesn't count —, done and left, and
// how long Notion asked it to wait.
func noteNotionMigrationLiveCounts(migrationID, stage string, done, left, waitSeconds int) {
	if migrationID == "" {
		return
	}
	state := notionMigrationLiveStateOf(migrationID)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.live.Stage != stage {
		state.live, state.last = notionMigrationLive{Stage: stage, Items: []notionMigrationLiveItem{}}, time.Time{}
	}
	state.live.Done, state.live.Left, state.live.WaitSeconds = max(0, done), max(0, left), max(0, waitSeconds)
	state.live.At = time.Now().UnixMilli()
}

// notionMigrationLiveOf is what a migration does now, nil when nothing went
// on lately.
func notionMigrationLiveOf(migrationID string) *notionMigrationLive {
	value, found := notionMigrationLives.Load(migrationID)
	if !found {
		return nil
	}
	state := value.(*notionMigrationLiveState)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.live.At == 0 || time.Since(time.UnixMilli(state.live.At)) > notionMigrationLiveFresh {
		return nil
	}
	live := state.live
	live.Items = append([]notionMigrationLiveItem{}, state.live.Items...)
	return &live
}

// withNotionMigrationLive is a migration as Leafen's import window reads it:
// with what it does now.
func withNotionMigrationLive(migration notionMigration) notionMigration {
	migration.Live = notionMigrationLiveOf(migration.ID)
	return migration
}

// noteNotionMigrationLivePage notes a stage on a page or row Leafen made, by
// the name it has here, read only when a name is due.
func noteNotionMigrationLivePage(app core.App, migrationID, stage, kind, pageID string) {
	if pageID == "" || !notionMigrationLiveDue(migrationID) {
		return
	}
	var title string
	if err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_pages}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&title); err != nil {
		return
	}
	noteNotionMigrationLive(migrationID, stage, kind, title)
}

// notionMigrationLiveKind is a staged or scanned item's kind as the window
// names it: a page, a database or a row; "" for what it doesn't show.
func notionMigrationLiveKind(kind string) string {
	switch kind {
	case "page", "database", "row":
		return kind
	case "template":
		return "page"
	case "database_page", "data_source":
		return "database"
	}
	return ""
}

// noteNotionMigrationLiveSource notes the page or row whose body a run reads
// now, by the name the run staged it with, read only when a name is due.
func noteNotionMigrationLiveSource(app core.App, migrationID, jobID, sourceID, kind string) {
	live := notionMigrationLiveKind(kind)
	if live == "" || !notionMigrationLiveDue(migrationID) {
		return
	}
	var title string
	if err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_notion_import_sources}} WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId}`).
		Bind(dbx.Params{"jobId": jobID, "sourceId": sourceID}).Row(&title); err != nil {
		return
	}
	noteNotionMigrationLive(migrationID, notionMigrationStageReading, live, title)
}
