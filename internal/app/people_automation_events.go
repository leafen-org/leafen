package app

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A change to a person, as People shows them — someone joining the
// workspace (joinWorkspaceAsMember) or becoming a guest
// (people_guest_rows.go), their About saved (savePersonAbout), their
// membership changed (setWorkspaceMemberRestricted, an ownership transfer, an
// organization deactivating them or lifting it —
// PeopleMembershipChangedEverywhere) or their name (personRenamed) — is
// noted with who made it (notePeopleChange) as it commits. People's sync
// writes it to the person's row in People's database (people_database.go),
// and what it writes sets off the database's automations
// (admitPeopleRowAutomationEvents): a row added is "페이지 추가", a value
// changed is that property edited, set off by whoever made the change — the
// person themselves when the sync finds a change no one noted.

// peopleChangeActors are who made each person's change in a workspace, as
// noted, until the sync that writes it takes them.
var peopleChangeActors = struct {
	sync.Mutex
	byWorkspace map[string]map[string]string
}{byWorkspace: map[string]map[string]string{}}

// notePeopleChange notes, once the change commits (at once outside a
// transaction), that the actor changed the person as People shows them, and
// brings People up to it.
func notePeopleChange(app core.App, workspaceID, userID, actorID string) {
	note := func() {
		peopleChangeActors.Lock()
		actors := peopleChangeActors.byWorkspace[workspaceID]
		if actors == nil {
			actors = map[string]string{}
			peopleChangeActors.byWorkspace[workspaceID] = actors
		}
		actors[userID] = actorID
		peopleChangeActors.Unlock()
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				note()
			}
			return nil
		})
	} else {
		note()
	}
	notePeopleChangedAfterCommit(app, workspaceID)
}

// takePeopleChangeActors is who made the changes noted in a workspace so
// far, which a sync about to write them takes.
func takePeopleChangeActors(workspaceID string) map[string]string {
	peopleChangeActors.Lock()
	defer peopleChangeActors.Unlock()
	actors := peopleChangeActors.byWorkspace[workspaceID]
	delete(peopleChangeActors.byWorkspace, workspaceID)
	return actors
}

// keepPeopleChangeActorsIf gives back, when kept, who made the changes a
// sync took and did not write, for the sync after it, under any change noted
// since.
func keepPeopleChangeActorsIf(kept bool, workspaceID string, actors map[string]string) {
	if !kept || len(actors) == 0 {
		return
	}
	peopleChangeActors.Lock()
	defer peopleChangeActors.Unlock()
	noted := peopleChangeActors.byWorkspace[workspaceID]
	if noted == nil {
		noted = map[string]string{}
		peopleChangeActors.byWorkspace[workspaceID] = noted
	}
	for userID, actorID := range actors {
		if _, since := noted[userID]; !since {
			noted[userID] = actorID
		}
	}
}

// peopleRowEvent is a person's row the sync wrote: added, with the values it
// was made with, or with the values it changed.
type peopleRowEvent struct {
	UserID, RowID string
	Added         bool
	Changed       []string
}

// admitPeopleRowAutomationEvents queues the rows People's sync wrote for
// its database's automations, in the sync's transaction, when any wait on
// changes: each actor's as a change of their own.
func admitPeopleRowAutomationEvents(
	app core.App, place pageAuthorization, database peopleDatabase, events []peopleRowEvent, actors map[string]string,
) error {
	if len(events) == 0 {
		return nil
	}
	var frontier int
	err := app.DB().NewQuery(`
		SELECT [[frontier]] FROM {{leafen_database_automation_frontiers}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[database_id]] = {:sourceId} AND [[active_count]] > 0
	`).Bind(dbx.Params{"owner": place.Workspace.OwnerID, "pageId": database.PageID, "sourceId": database.DataSourceID}).Row(&frontier)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	byActor := map[string][]peopleRowEvent{}
	for _, event := range events {
		actorID := actors[event.UserID]
		if actorID == "" {
			actorID = event.UserID
		}
		byActor[actorID] = append(byActor[actorID], event)
	}
	actorIDs := make([]string, 0, len(byActor))
	for actorID := range byActor {
		actorIDs = append(actorIDs, actorID)
	}
	sort.Strings(actorIDs)
	owner, pageID := place.Workspace.OwnerID, database.PageID
	for _, actorID := range actorIDs {
		// Each change is its own; nothing replays it.
		mutationID := "people:" + core.GenerateDefaultRandomId()
		facts := databaseAutomationMutationFacts{
			JobID:      automationStableID("haj", owner, pageID, mutationID),
			AddedJobID: automationStableID("haj", owner, pageID, mutationID, "page-added"),
		}
		for _, event := range byActor[actorID] {
			eventType, jobID := databaseAutomationTriggerPropertyEdited, facts.JobID
			if event.Added {
				eventType, jobID = databaseAutomationTriggerRowAdded, facts.AddedJobID
			}
			changed := append([]string{}, event.Changed...)
			sort.Strings(changed)
			facts.Events = append(facts.Events, plannedDatabaseAutomationEvent{
				ID:         automationStableID("hae", owner, pageID, mutationID, event.RowID),
				DatabaseID: database.DataSourceID, RowID: event.RowID, EventType: eventType,
				ChangedPropertyIDs: strings.Join(changed, "\x00"), CapturedFrontier: frontier, JobID: jobID,
			})
		}
		if _, err := admitDatabaseAutomationEvents(app, place, actorID, mutationID, automationDigest(mutationID), facts); err != nil {
			return err
		}
	}
	notifyDurableJobsAfterCommit(app)
	return nil
}

// notifyDurableJobsAfterCommit wakes the job runner once the transaction
// that queued a job commits, or at once outside one.
func notifyDurableJobsAfterCommit(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
			runner.notify()
		}
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				notify()
			}
			return nil
		})
		return
	}
	notify()
}

// savePersonAbout saves a person's About as a member writes their own or
// the workspace's owner writes anyone's, and notes the change as the one
// who wrote it, when it changed.
func savePersonAbout(app core.App, workspaceID, userID, actorID, about string) error {
	return app.RunInTransaction(func(txApp core.App) error {
		var former string
		err := txApp.DB().NewQuery(`
			SELECT [[about]] FROM {{leafen_workspace_person_profiles}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}
		`).Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID}).Row(&former)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := time.Now().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_person_profiles}} ([[workspace_id]], [[user_id]], [[about]], [[updated_at]])
			VALUES ({:workspaceId}, {:userId}, {:about}, {:now})
			ON CONFLICT ([[workspace_id]], [[user_id]]) DO UPDATE SET [[about]] = excluded.[[about]], [[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID, "about": about,
			"now": now}).Execute(); err != nil {
			return err
		}
		if former == about {
			return nil
		}
		// Their row was last edited now, by the one who wrote it (people_database.go).
		if err := stampPeopleRowEdit(txApp, workspaceID, userID, actorID, now); err != nil {
			return err
		}
		notePeopleChange(txApp, workspaceID, userID, actorID)
		return nil
	})
}

// notePeopleMembershipChanged notes, as the actor's, a change to people's
// Membership Type.
func notePeopleMembershipChanged(app core.App, workspaceID, actorID string, userIDs ...string) {
	for _, userID := range userIDs {
		notePeopleChange(app, workspaceID, userID, actorID)
	}
}

// PeopleMembershipChangedEverywhere notes, as the change commits, a change
// to the person's Membership Type in each workspace they are in — an
// organization deactivating them ("Deactivated") or lifting it, which every
// workspace's People shows (organizationSuspendedSQL).
func PeopleMembershipChangedEverywhere(app core.App, userID, actorID string) error {
	workspaceIDs, err := peopleWorkspacesOf(app, userID)
	if err != nil {
		return err
	}
	for _, workspaceID := range workspaceIDs {
		notePeopleChange(app, workspaceID, userID, actorID)
	}
	return nil
}

// peopleWorkspacesOf is each workspace whose People lists the person as its
// owner or a member.
func peopleWorkspacesOf(app core.App, userID string) ([]string, error) {
	workspaceIDs := []string{}
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_workspaces}} WHERE [[workspace_owner]] = {:userId} AND [[deletion_job_id]] = ''
		UNION SELECT [[workspace_id]] FROM {{leafen_workspace_memberships}}
		WHERE [[user_id]] = {:userId} AND [[role]] = 'member'
	`).Bind(dbx.Params{"userId": userID}).Column(&workspaceIDs)
	return workspaceIDs, err
}

// admitPeopleRenamedSoon notes, once a rename is saved, a change to the
// person's Name in each workspace they are in, as their own.
func admitPeopleRenamedSoon(app core.App, userID string) {
	productAfterworkOf(app).run(afterworkPeopleRenamed, afterworkInput(userID), func(context.Context) error {
		workspaceIDs, err := peopleWorkspacesOf(app, userID)
		if err != nil {
			app.Logger().Warn("leafen people automations miss a rename", "user", userID, "error", err)
			return err
		}
		for _, workspaceID := range workspaceIDs {
			notePeopleChange(app, workspaceID, userID, userID)
		}
		return nil
	})
}
