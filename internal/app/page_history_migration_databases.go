package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A migration changes a page's databases as no one does. Its change kept in
// the page's history without the databases it left (2026-11-17: no capture
// went with it), the history held them as before the migration, and the
// next person's change, read against the version before it, took the
// migration's settings as theirs — in its version's changes and its
// webhooks' updated fields. A migration's change keeps the page's databases
// as the migration left them, against what the history last knew of them,
// which also takes in what earlier migrations changed without a version.

// pageHistoryKnownDatabaseCapture compares a page's databases now with
// those its history last knew: its latest checkpoint's, with every change
// since. A page with no history yet has none to compare.
func pageHistoryKnownDatabaseCapture(app core.App, owner, pageID string) (*pageHistoryDatabaseCapture, error) {
	checkpoint := pageHistoryCheckpoint{}
	err := app.DB().NewQuery(`SELECT [[page_id]], [[version_cursor]], [[databases_json]] FROM {{leafen_page_history_checkpoints}}
		WHERE [[page_id]] = {:page} ORDER BY [[version_cursor]] DESC LIMIT 1`).
		Bind(dbx.Params{"page": pageID}).One(&checkpoint)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err == nil {
		checkpoint.DatabasesJSON, err = unpackPageHistoryText(checkpoint.DatabasesJSON)
	}
	if err != nil {
		return nil, err
	}
	known, err := pageHistoryCheckpointDatabases(app, checkpoint)
	if err != nil {
		return nil, err
	}
	// Only a change to the page's blocks carries databases (applyPageHistoryEvent).
	events := []storedPageHistoryEvent{}
	if err := app.DB().NewQuery(`SELECT [[version_cursor]], [[kind]], [[payload]], [[created_at]] FROM {{leafen_page_history_events}}
		WHERE [[page_id]] = {:page} AND [[version_cursor]] > {:from} AND [[kind]] = {:graph}
		ORDER BY [[version_cursor]]`).Bind(dbx.Params{
		"page": pageID, "from": checkpoint.VersionCursor, "graph": pageHistoryEventGraph,
	}).All(&events); err != nil {
		return nil, err
	}
	for _, stored := range events {
		envelope, err := pageHistoryEventPayload(stored.Payload)
		if err != nil {
			return nil, err
		}
		for id, state := range envelope.Databases {
			if state == nil {
				delete(known, id)
			} else {
				known[id] = *state
			}
		}
	}
	return &pageHistoryDatabaseCapture{
		owner: owner, pageID: pageID, before: known, activityTrashed: map[string]bool{},
	}, nil
}
