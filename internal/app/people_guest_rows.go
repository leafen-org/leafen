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

// People's guests come and go with the writes that make them
// (people_guest_rows_schema.go), and a look about once a second reads each
// workspace those writes marked again: someone newly a guest is a page added
// to People (its automations' "페이지 추가"), a member who left and stays as a
// guest, or a former guest shared with again, has their Membership Type
// edited, and someone no longer a guest stays in it as deactivated
// (keepFormerPeopleGuest). Each sets an open People list reading itself again
// (notePeopleChangedAfterCommit). A guest who joins is a Membership Type
// edited too, where they join (joinWorkspaceAsMember).

const (
	peopleGuestLookEvery = time.Second
	peopleGuestLookChunk = 50
)

type peopleGuestRowsRunner struct {
	app    core.App
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newPeopleGuestRowsRunner(app core.App) *peopleGuestRowsRunner {
	return &peopleGuestRowsRunner{app: app}
}

func (runner *peopleGuestRowsRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel, runner.done = cancel, make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *peopleGuestRowsRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	cancel, done := runner.cancel, runner.done
	runner.cancel, runner.done = nil, nil
	runner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (runner *peopleGuestRowsRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	look := time.NewTicker(peopleGuestLookEvery)
	defer look.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-look.C:
			runGuarded(runner.app, "leafen people guest rows", func() {
				if err := lookAtPeopleGuests(ctx, runner.app); err != nil {
					runner.app.Logger().Warn("people guest rows not read", "error", err)
				}
			})
		}
	}
}

// lookAtPeopleGuests reads again each workspace a write marked.
func lookAtPeopleGuests(ctx context.Context, app core.App) error {
	for ctx.Err() == nil {
		workspaceIDs := []string{}
		if err := app.DB().NewQuery(`
			SELECT [[workspace_id]] FROM {{leafen_people_guest_checks}} INDEXED BY [[idx_leafen_people_guest_checks_dirty]]
			WHERE [[dirty]] = 1 LIMIT {:limit}
		`).Bind(dbx.Params{"limit": peopleGuestLookChunk}).Column(&workspaceIDs); err != nil || len(workspaceIDs) == 0 {
			return err
		}
		for _, workspaceID := range workspaceIDs {
			if err := readPeopleGuestsAgain(app, workspaceID); err != nil {
				return err
			}
		}
		if len(workspaceIDs) < peopleGuestLookChunk {
			return nil
		}
	}
	return nil
}

// readPeopleGuestsAgain compares a workspace's guests with those it kept and
// tells People of each difference, in one transaction; a write meanwhile
// marks it again.
func readPeopleGuestsAgain(app core.App, workspaceID string) error {
	return app.RunInTransaction(func(txApp core.App) error {
		params := dbx.Params{"workspaceId": workspaceID}
		var seeded bool
		err := txApp.DB().NewQuery(`
			SELECT [[seeded]] FROM {{leafen_people_guest_checks}} WHERE [[workspace_id]] = {:workspaceId}
		`).Bind(params).Row(&seeded)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_people_guest_checks}} SET [[dirty]] = 0, [[seeded]] = 1 WHERE [[workspace_id]] = {:workspaceId}
		`).Bind(params).Execute(); err != nil {
			return err
		}
		// Each guest with an account, but an app's, and who last shared a page
		// with them there: whom People's automations tell.
		current := []struct {
			UserID  string `db:"account_id"`
			ActorID string `db:"created_by"`
		}{}
		if err := txApp.DB().NewQuery(workspaceGuestSharesSQL + `
			SELECT [[account_id]], [[created_by]] FROM (
				SELECT [[account_id]], [[created_by]], ROW_NUMBER() OVER (
					PARTITION BY [[account_id]] ORDER BY [[created_at]] DESC, [[page_id]]) AS [[latest]]
				FROM [[guest_share]] WHERE [[account_id]] IS NOT NULL
			) WHERE [[latest]] = 1 AND NOT ` + integrationAccountSQL("[[account_id]]"),
		).Bind(params).All(&current); err != nil {
			return err
		}
		kept := []struct {
			UserID     string `db:"user_id"`
			Membership string `db:"membership"`
		}{}
		if err := txApp.DB().NewQuery(`
			SELECT [[user_id]], [[membership]] FROM {{leafen_people_guests}} WHERE [[workspace_id]] = {:workspaceId}
		`).Bind(params).All(&kept); err != nil {
			return err
		}
		was := make(map[string]string, len(kept))
		for _, person := range kept {
			was[person.UserID] = person.Membership
		}
		for _, guest := range current {
			membership := was[guest.UserID]
			delete(was, guest.UserID)
			if membership == personMembershipGuest {
				continue
			}
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_people_guests}} ([[workspace_id]], [[user_id]], [[membership]])
				VALUES ({:workspaceId}, {:userId}, 'guest')
				ON CONFLICT ([[workspace_id]], [[user_id]]) DO UPDATE SET [[membership]] = 'guest'
			`).Bind(dbx.Params{"workspaceId": workspaceID, "userId": guest.UserID}).Execute(); err != nil {
				return err
			}
			if !seeded {
				continue
			}
			// A member who left and stays on as a guest, or a former guest
			// back as one, keeps their People page; anyone else is a new one —
			// whichever People's sync finds, the sharer's change.
			notePeopleChange(txApp, workspaceID, guest.UserID, guest.ActorID)
		}
		for userID, membership := range was {
			if err := keepFormerPeopleGuest(txApp, workspaceID, userID, membership); err != nil {
				return err
			}
		}
		return nil
	})
}

// keepFormerPeopleGuest settles someone People kept who is no guest now: a
// guest who left stays as deactivated (people_former_rows_schema.go), one
// kept so already stays, and a member who left — or anyone who is the
// workspace's owner or a member now, whose own row People reads — goes.
func keepFormerPeopleGuest(app core.App, workspaceID, userID, membership string) error {
	params := dbx.Params{"workspaceId": workspaceID, "userId": userID}
	var member bool
	if err := app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId} AND [[workspace_owner]] = {:userId})
			OR EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}}
				WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId})
	`).Bind(params).Row(&member); err != nil {
		return err
	}
	switch {
	case !member && membership == peopleMembershipKeptDeactivated:
		return nil
	case !member && membership == personMembershipGuest:
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_people_guests}} SET [[membership]] = '` + peopleMembershipKeptDeactivated + `'
			WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}
		`).Bind(params).Execute()
		if err == nil {
			notePeopleChangedAfterCommit(app, workspaceID)
		}
		return err
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_people_guests}} WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}
	`).Bind(params).Execute()
	if err == nil && membership != personMembershipMember {
		notePeopleChangedAfterCommit(app, workspaceID)
	}
	return err
}
