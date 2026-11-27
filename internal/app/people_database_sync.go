package app

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// peopleDatabaseRuns keeps one run of a workspace's People rows at a time;
// a change that comes meanwhile runs it once more after.
var peopleDatabaseRuns = struct {
	sync.Mutex
	again   map[string]bool
	running map[string]bool
}{again: map[string]bool{}, running: map[string]bool{}}

// syncPeopleDatabaseSoon brings a workspace's People rows up to its people
// (syncPeopleDatabase) after the request, as someone joins, leaves, is
// deactivated, or their name, About or membership changes
// (notePeopleChangedAfterCommit).
func syncPeopleDatabaseSoon(app core.App, workspaceID string) {
	if workspaceID == "" {
		return
	}
	peopleDatabaseRuns.Lock()
	if peopleDatabaseRuns.running[workspaceID] {
		peopleDatabaseRuns.again[workspaceID] = true
		peopleDatabaseRuns.Unlock()
		return
	}
	peopleDatabaseRuns.running[workspaceID] = true
	peopleDatabaseRuns.Unlock()
	ended := func() {
		peopleDatabaseRuns.Lock()
		delete(peopleDatabaseRuns.running, workspaceID)
		delete(peopleDatabaseRuns.again, workspaceID)
		peopleDatabaseRuns.Unlock()
	}
	// The run uses the server's own app, not a transaction's that has ended.
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok {
		ended()
		return
	}
	root := persistence.app
	started := productAfterworkOf(app).run(afterworkPeopleDatabase, afterworkInput(workspaceID), func(ctx context.Context) error {
		finished := false
		defer func() {
			if !finished {
				ended()
			}
		}()
		for ctx.Err() == nil {
			if !syncPeopleDatabaseTrying(ctx, root, workspaceID) {
				break
			}
			peopleDatabaseRuns.Lock()
			if !peopleDatabaseRuns.again[workspaceID] {
				delete(peopleDatabaseRuns.running, workspaceID)
				peopleDatabaseRuns.Unlock()
				finished = true
				return nil
			}
			delete(peopleDatabaseRuns.again, workspaceID)
			peopleDatabaseRuns.Unlock()
		}
		// The server's stop ended it: it runs again as the server starts.
		return ctx.Err()
	})
	if !started {
		ended()
	}
}

// peopleSyncRetries are the waits before a run of a workspace's People that
// failed tries again: one that met a write making the same thing at once
// waited for the next change to its people, and a new workspace's People
// was not made (2026-11-18).
var peopleSyncRetries = []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}

// syncPeopleDatabaseTrying runs syncPeopleDatabase, and again after each of
// peopleSyncRetries while it fails; false when the server's stop cut a wait,
// so the run is done again as the server starts.
func syncPeopleDatabaseTrying(ctx context.Context, root core.App, workspaceID string) bool {
	for attempt := 0; ; attempt++ {
		_, err := syncPeopleDatabase(root, workspaceID)
		if err == nil {
			return true
		}
		root.Logger().Warn("leafen people database waits", "workspace", workspaceID, "error", err, "attempt", attempt+1)
		if attempt == len(peopleSyncRetries) {
			return true
		}
		wait := time.NewTimer(peopleSyncRetries[attempt])
		select {
		case <-ctx.Done():
			wait.Stop()
			return false
		case <-wait.C:
		}
	}
}

// stampPeopleRowEdit keeps who last changed a value of a person's People row
// — their About — and when, on the row's page.
func stampPeopleRowEdit(app core.App, workspaceID, userID, actorID string, now int64) error {
	var rowID string
	err := app.DB().NewQuery(`SELECT [[row_id]] FROM {{leafen_workspace_people_rows}} WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}`).
		Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID}).Row(&rowID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return stampPeopleRow(app, rowID, "", actorID, 0, now)
}

// stampPeopleRowMade names a person's new row theirs, made by the server
// when they joined, and last edited as it was — by the server, unless
// someone wrote a value of it before (people_database_moves.go).
func stampPeopleRowMade(app core.App, workspaceID, userID string, made peopleRowMade) error {
	rowID := peopleRowID(workspaceID, userID)
	if made.Created <= 0 {
		made.Created = time.Now().UTC().UnixMilli()
	}
	if made.Edited < made.Created {
		made.Edited = made.Created
	}
	if made.EditedBy == "" {
		made.EditedBy = leafenSystemAuthorID
	}
	if _, err := app.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_workspace_people_rows}}
		([[workspace_id]], [[user_id]], [[row_id]], [[created_at]]) VALUES ({:workspaceId}, {:userId}, {:rowId}, {:made})`).
		Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID, "rowId": rowID, "made": made.Created}).Execute(); err != nil {
		return err
	}
	return stampPeopleRow(app, rowID, leafenSystemAuthorID, made.EditedBy, made.Created, made.Edited)
}

// stampPeopleRow sets a People row's page's maker and when it was made (none
// for a zero), and who last edited it and when, where its page, its row and
// its row page keep them.
func stampPeopleRow(app core.App, rowID, maker, editor string, created, edited int64) error {
	format := func(at int64) string { return time.UnixMilli(at).UTC().Format("2006-01-02 15:04:05.000Z") }
	params := dbx.Params{"rowId": rowID, "editor": editor, "edited": format(edited), "editedAt": edited}
	statements := []string{
		`UPDATE {{leafen_pages}} SET [[last_edited_by]] = {:editor}, [[updated]] = {:edited} WHERE [[id]] = {:rowId}`,
		`UPDATE {{leafen_database_rows}} SET [[updated]] = {:editedAt} WHERE [[id]] = {:rowId}`,
		`UPDATE {{leafen_database_row_pages}} SET [[updated]] = {:editedAt} WHERE [[row_id]] = {:rowId}`,
	}
	if created > 0 {
		params["maker"], params["created"], params["createdAt"] = maker, format(created), created
		statements = append(statements,
			`UPDATE {{leafen_pages}} SET [[created_by]] = {:maker}, [[created]] = {:created} WHERE [[id]] = {:rowId}`,
			`UPDATE {{leafen_database_rows}} SET [[created]] = {:createdAt} WHERE [[id]] = {:rowId}`,
			`UPDATE {{leafen_database_row_pages}} SET [[created]] = {:createdAt} WHERE [[row_id]] = {:rowId}`)
	}
	for _, statement := range statements {
		if _, err := app.DB().NewQuery(statement).Bind(params).Execute(); err != nil {
			return err
		}
	}
	return nil
}
