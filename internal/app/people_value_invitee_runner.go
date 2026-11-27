package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Someone invited into a person value becomes that person once their
// account proves the address (people_value_invitees.go), as Notion's invited
// person is the one who joins: the runner turns the workspace's person
// values and its views' filters naming the invitee into the account, a page
// at a time in page order. It is no one's act: the values read as the
// account from the moment it proves the address, in the app and to a token
// (people_value_invitees.go, notion_database_row_page_people.go), so turning
// them changes nothing anyone reads, and no one edited the pages. It is
// Leafen's own upkeep under the workspace owner's authority, as People's is
// (contentGraphMutationHooks.LeafenOwn): no notice, no automation, the
// rows' edit stamps kept, its version Leafen's, no line in the workspace's
// updates, no page interaction and no event for a webhook or an app
// (2026-11-17: each page it turned was kept as the owner's edit — a page
// interaction, versions by the owner, and a page.properties_updated by the
// owner delivered to the workspace's webhook). An account may prove an
// address in many ways — signing up, confirming it, a sign-in provider — so
// the runner looks for proven invitees when woken and, while any invitee
// waits, once a minute.

const (
	peopleValueInviteeRunnerStoreKey = "leafenPeopleValueInviteeRunner"
	peopleValueInviteeWaitingPoll    = time.Minute
	peopleValueInviteeFailureDelay   = 5 * time.Second
)

type peopleValueInviteeRunner struct {
	app  core.App
	ygo  *pocketBaseYgoPersistence
	wake chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newPeopleValueInviteeRunner(app core.App, ygo *pocketBaseYgoPersistence) *peopleValueInviteeRunner {
	return &peopleValueInviteeRunner{app: app, ygo: ygo, wake: make(chan struct{}, 1)}
}

func (runner *peopleValueInviteeRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel, runner.done = cancel, make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *peopleValueInviteeRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("people value invitee shutdown: %w", ctx.Err())
	}
}

func (runner *peopleValueInviteeRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

// notifyPeopleValueInviteeRunner wakes the runner, after the transaction
// commits when there is one.
func notifyPeopleValueInviteeRunner(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(peopleValueInviteeRunnerStoreKey).(*peopleValueInviteeRunner); ok {
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

func (runner *peopleValueInviteeRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	failures := 0
	for {
		wait, err := runner.runTurn(ctx)
		if err == nil {
			failures = 0
		} else {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeJobRunnerFailed, "people-value-invitee", err)
			wait = min(peopleValueInviteeFailureDelay<<min(failures, 6), 5*time.Minute)
			failures++
		}
		if wait == 0 {
			continue
		}
		var timer <-chan time.Time
		if wait > 0 {
			timer = time.After(wait)
		}
		select {
		case <-ctx.Done():
			return
		case <-runner.wake:
		case <-timer:
		}
	}
}

type peopleValueInviteeWork struct {
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
	AccountID   string `db:"account_id"`
	ScanAfter   string `db:"scan_after"`
}

// runTurn turns the next page of the first proven invitee's values into its
// account, or records it done once no page is left.
func (runner *peopleValueInviteeRunner) runTurn(ctx context.Context) (time.Duration, error) {
	work := peopleValueInviteeWork{}
	err := runner.app.DB().NewQuery(`
		SELECT [[invitee]].[[id]], [[invitee]].[[workspace_id]], [[invitee]].[[scan_after]],
			(SELECT [[proof]].[[user_id]] FROM {{leafen_account_emails}} [[proof]]
				WHERE [[proof]].[[email]] = [[invitee]].[[email]] AND ` + provenAccountEmailSQL("[[proof]].") + `
				ORDER BY [[proof]].[[user_id]] LIMIT 1) AS [[account_id]]
		FROM {{leafen_people_value_invitees}} [[invitee]]
		WHERE [[invitee]].[[converted_at]] = 0 AND EXISTS (SELECT 1 FROM {{leafen_account_emails}} [[proof]]
			WHERE [[proof]].[[email]] = [[invitee]].[[email]] AND ` + provenAccountEmailSQL("[[proof]].") + `)
		ORDER BY [[invitee]].[[created_at]], [[invitee]].[[id]] LIMIT 1
	`).WithContext(ctx).One(&work)
	if errors.Is(err, sql.ErrNoRows) {
		waiting := 0
		if err := runner.app.DB().NewQuery(`SELECT COUNT(*) FROM (SELECT 1 FROM {{leafen_people_value_invitees}}
			WHERE [[converted_at]] = 0 LIMIT 1)`).WithContext(ctx).Row(&waiting); err != nil {
			return -1, err
		}
		if waiting > 0 {
			return peopleValueInviteeWaitingPoll, nil
		}
		return -1, nil
	}
	if err != nil {
		return -1, err
	}
	var pageID string
	err = runner.app.DB().NewQuery(`
		SELECT [[page_id]] FROM (
			SELECT [[page_id]] FROM {{leafen_database_row_values}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[type]] = 'person' AND instr([[value]], {:id}) > 0
			UNION SELECT [[page_id]] FROM {{leafen_database_views}}
			WHERE [[workspace_id]] = {:workspaceId} AND instr([[config]], {:id}) > 0
		) WHERE [[page_id]] > {:after} ORDER BY [[page_id]] LIMIT 1
	`).WithContext(ctx).Bind(dbx.Params{"workspaceId": work.WorkspaceID, "id": work.ID, "after": work.ScanAfter}).Row(&pageID)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = runner.app.DB().NewQuery(`UPDATE {{leafen_people_value_invitees}}
			SET [[joined_user_id]] = {:account}, [[converted_at]] = {:now} WHERE [[id]] = {:id}`).WithContext(ctx).
			Bind(dbx.Params{"account": work.AccountID, "now": time.Now().UnixMilli(), "id": work.ID}).Execute()
		return 0, err
	}
	if err != nil {
		return -1, err
	}
	if err := runner.ygo.turnPeopleValueInviteePage(ctx, work, pageID); err != nil && !isPermissionDenied(err) && !isRecordMissing(err) {
		return -1, err
	}
	_, err = runner.app.DB().NewQuery(`UPDATE {{leafen_people_value_invitees}} SET [[scan_after]] = {:page} WHERE [[id]] = {:id}`).
		WithContext(ctx).Bind(dbx.Params{"page": pageID, "id": work.ID}).Execute()
	return 0, err
}

func isRecordMissing(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// turnPeopleValueInviteePage turns one page's person values and view filters
// naming the invitee into its account, as Leafen's own upkeep under the
// workspace owner's authority.
func (p *pocketBaseYgoPersistence) turnPeopleValueInviteePage(ctx context.Context, work peopleValueInviteeWork, pageID string) error {
	workspace, err := p.app.FindRecordById(workspacesCollection, work.WorkspaceID)
	if err != nil {
		return err
	}
	system := systemJobSubjectFor(workspace)
	mutationID := "people-invitee:" + work.ID + ":" + pageID
	digest := sha256.Sum256([]byte("people-value-invitee\x00" + mutationID + "\x00" + work.AccountID))
	_, err = p.applyContentGraphMutationContext(ctx, system.actorID(), pageID, graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			SkipRelationPairSync: true, SuppressAutomationEvents: true, SuppressNotifications: true,
			AllowTrashedPage: true, PreserveRowEditStamps: true, KeepTrashedRowsInStep: true, LeafenOwn: true,
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				return system.authorizePageBody(txApp, pageID, permissionWorkspaceWrite)
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				operations, err := peopleValueInviteeOperations(txApp, pageID, work.ID, work.AccountID)
				return contentGraphMutationPreparation{Operations: operations, Deferred: len(operations) == 0}, err
			},
		})
	return err
}

// peopleValueInviteeOperations are the page's rows and views with the
// invitee turned into the account: a value already naming the account
// keeps it once.
func peopleValueInviteeOperations(app core.App, pageID, inviteeID, accountID string) ([]graphOperation, error) {
	rows := []importedPeopleRowValue{}
	if err := app.DB().NewQuery(`
		SELECT [[value]].[[row_id]], [[value]].[[property_id]], [[row]].[[data_source_id]], [[row]].[[position]],
			[[row]].[[subitem_parent_id]], [[value]].[[type]], [[value]].[[value]], [[value]].[[spans]]
		FROM {{leafen_database_row_values}} [[value]] JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]] = [[value]].[[row_id]]
		WHERE [[value]].[[page_id]] = {:pageId} AND [[value]].[[type]] = 'person' AND instr([[value]].[[value]], {:id}) > 0
		ORDER BY [[value]].[[row_id]], [[value]].[[property_id]]
	`).Bind(dbx.Params{"pageId": pageID, "id": inviteeID}).All(&rows); err != nil {
		return nil, err
	}
	operations := []graphOperation{}
	byRow := map[string]*graphDatabaseRow{}
	for _, row := range rows {
		people := []string{}
		if json.Unmarshal([]byte(row.Value), &people) != nil || !slices.Contains(people, inviteeID) {
			continue
		}
		turned := make([]string, 0, len(people))
		for _, id := range people {
			if id == inviteeID {
				id = accountID
			}
			if !slices.Contains(turned, id) {
				turned = append(turned, id)
			}
		}
		encoded, err := json.Marshal(turned)
		if err != nil {
			return nil, err
		}
		graphRow := byRow[row.RowID]
		if graphRow == nil {
			var parent *string
			if row.SubitemParentID != "" {
				value := row.SubitemParentID
				parent = &value
			}
			graphRow = &graphDatabaseRow{ID: row.RowID, DataSourceID: row.DataSourceID, Position: row.Position,
				SubitemParentID: parent, Values: map[string]graphRowValue{}}
			byRow[row.RowID] = graphRow
			operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: graphRow})
		}
		graphRow.Values[row.PropertyID] = graphRowValue{Type: row.Type, Value: encoded}
	}
	views := []struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
		ContainerID  string `db:"container_id"`
		Type         string `db:"type"`
		Name         string `db:"name"`
		Position     string `db:"position"`
		Config       string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]], [[container_id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_views}} WHERE [[page_id]] = {:pageId} AND instr([[config]], {:id}) > 0 ORDER BY [[id]]
	`).Bind(dbx.Params{"pageId": pageID, "id": inviteeID}).All(&views); err != nil {
		return nil, err
	}
	for _, view := range views {
		var config any
		if json.Unmarshal([]byte(view.Config), &config) != nil {
			continue
		}
		turned, changed := turnPeopleValueInviteeStrings(config, inviteeID, accountID)
		if !changed {
			continue
		}
		encoded, err := json.Marshal(turned)
		if err != nil {
			return nil, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: view.ID, DataSourceID: view.DataSourceID, ContainerID: view.ContainerID, Type: view.Type,
			Name: view.Name, Position: view.Position, Config: encoded,
		}})
	}
	return operations, nil
}

// turnPeopleValueInviteeStrings turns each string naming the invitee, as a
// view's filter names a person, into the account.
func turnPeopleValueInviteeStrings(value any, inviteeID, accountID string) (any, bool) {
	switch typed := value.(type) {
	case string:
		if typed == inviteeID {
			return accountID, true
		}
	case []any:
		changed := false
		for index, item := range typed {
			turned, itemChanged := turnPeopleValueInviteeStrings(item, inviteeID, accountID)
			typed[index], changed = turned, changed || itemChanged
		}
		return typed, changed
	case map[string]any:
		changed := false
		for key, item := range typed {
			turned, itemChanged := turnPeopleValueInviteeStrings(item, inviteeID, accountID)
			typed[key], changed = turned, changed || itemChanged
		}
		return typed, changed
	}
	return value, false
}
