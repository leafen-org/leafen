package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// The relation pair runner applies recorded work to the other side's page,
// as the system (system_job_access.go) and in the name of the person whose
// change caused it, reaching rows that person may not change themselves
// (MirrorsRelationPair). Its own writes record no further work, since they
// only mirror the change they came from. Work that keeps failing stops after a
// few attempts and stays for diagnosis.

const (
	databaseRelationPairRunnerStoreKey = "leafenDatabaseRelationPairRunner"
	databaseRelationPairBackfillRows   = 100
	databaseRelationPairMaxAttempts    = 8
	databaseRelationPairFailureDelay   = time.Second
	databaseRelationPairFailedLifetime = 30 * 24 * time.Hour
)

type storedDatabaseRelationPairWork struct {
	ID          string `db:"id"`
	OwnerID     string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	ActorID     string `db:"actor_id"`
	Kind        string `db:"kind"`
	PayloadJSON string `db:"payload_json"`
	Attempts    int    `db:"attempts"`
}

type databaseRelationPairTargetProperty struct {
	ID           string `db:"id"`
	DataSourceID string `db:"data_source_id"`
	Type         string `db:"type"`
	Name         string `db:"name"`
	Position     string `db:"position"`
	ConfigJSON   string `db:"config"`
}

type databaseRelationPairRunner struct {
	app  core.App
	ygo  *pocketBaseYgoPersistence
	wake chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newDatabaseRelationPairRunner(app core.App, ygo *pocketBaseYgoPersistence) *databaseRelationPairRunner {
	return &databaseRelationPairRunner{app: app, ygo: ygo, wake: make(chan struct{}, 1)}
}

func (runner *databaseRelationPairRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel, runner.done = cancel, make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *databaseRelationPairRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("database relation pair shutdown: %w", ctx.Err())
	}
}

func (runner *databaseRelationPairRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *databaseRelationPairRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait, err := runner.runTurn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeJobRunnerFailed, "database-relation-pair", err)
			wait = databaseRelationPairFailureDelay
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

// runTurn applies the oldest due work; it waits for a wake-up or the next
// retry when nothing is due.
func (runner *databaseRelationPairRunner) runTurn(ctx context.Context) (time.Duration, error) {
	now := time.Now().UTC().UnixMilli()
	if _, err := runner.app.DB().NewQuery(`DELETE FROM {{leafen_database_relation_pair_work}}
		WHERE [[status]]='failed' AND [[updated_at]]<={:expired}`).Bind(dbx.Params{
		"expired": now - databaseRelationPairFailedLifetime.Milliseconds(),
	}).Execute(); err != nil {
		return -1, err
	}
	work := storedDatabaseRelationPairWork{}
	err := runner.app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[workspace_id]], [[actor_id]], [[kind]], [[payload_json]], [[attempts]]
		FROM {{leafen_database_relation_pair_work}}
		WHERE [[status]]='pending' AND [[not_before]]<={:now}
		ORDER BY [[not_before]], [[created_at]], [[id]] LIMIT 1
	`).Bind(dbx.Params{"now": now}).One(&work)
	if errors.Is(err, sql.ErrNoRows) {
		next := sql.NullInt64{}
		if err := runner.app.DB().NewQuery(`SELECT MIN([[not_before]]) FROM {{leafen_database_relation_pair_work}}
			WHERE [[status]]='pending'`).Row(&next); err != nil || !next.Valid {
			return -1, err
		}
		return max(time.Duration(next.Int64-now)*time.Millisecond, 10*time.Millisecond), nil
	}
	if err != nil {
		return -1, err
	}
	finished, applyErr := runner.ygo.applyDatabaseRelationPairWork(ctx, work)
	if applyErr != nil {
		if ctx.Err() != nil {
			return -1, applyErr
		}
		status := "pending"
		if work.Attempts+1 >= databaseRelationPairMaxAttempts {
			status = "failed"
		}
		_, err := runner.app.DB().NewQuery(`UPDATE {{leafen_database_relation_pair_work}}
			SET [[status]]={:status}, [[attempts]]=[[attempts]]+1, [[not_before]]={:next},
				[[updated_at]]={:now}, [[error_code]]={:code}
			WHERE [[id]]={:id}`).Bind(dbx.Params{
			"status": status, "next": now + (databaseRelationPairFailureDelay << min(work.Attempts, 8)).Milliseconds(),
			"now": now, "code": stableDatabaseRelationPairError(applyErr), "id": work.ID,
		}).Execute()
		return 0, err
	}
	if finished {
		_, err = runner.app.DB().NewQuery(`DELETE FROM {{leafen_database_relation_pair_work}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": work.ID}).Execute()
	}
	return 0, err
}

func stableDatabaseRelationPairError(err error) string {
	message := err.Error()
	if len(message) > 120 {
		message = message[:120]
	}
	return message
}

func notifyDatabaseRelationPairAfterCommit(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(databaseRelationPairRunnerStoreKey).(*databaseRelationPairRunner); ok {
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

// applyDatabaseRelationPairWork applies one work item to the page of the
// data source it changes and reports whether the work is finished.
func (p *pocketBaseYgoPersistence) applyDatabaseRelationPairWork(
	ctx context.Context, work storedDatabaseRelationPairWork,
) (bool, error) {
	payload := databaseRelationPairPayload{}
	if err := json.Unmarshal([]byte(work.PayloadJSON), &payload); err != nil {
		return true, nil
	}
	var targetPageID string
	err := p.app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}}
		WHERE [[owner]]={:owner} AND [[id]]={:id}`).Bind(dbx.Params{
		"owner": work.OwnerID, "id": payload.TargetDataSourceID,
	}).Row(&targetPageID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	// Properties made in the target hide in the tables another page has of it.
	if work.Kind == databaseRelationPairHideNew {
		var tables int
		if err := p.app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_views}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:page} AND [[data_source_id]]={:source}`).Bind(dbx.Params{
			"owner": work.OwnerID, "page": payload.PageID, "source": payload.TargetDataSourceID,
		}).Row(&tables); err != nil || tables == 0 {
			return err == nil, err
		}
		targetPageID = payload.PageID
	}
	workspace, err := p.app.FindRecordById(workspacesCollection, work.WorkspaceID)
	if err != nil {
		return true, nil
	}
	system := systemJobSubjectFor(workspace)
	// Its rows' values are the other side of the person's change and pass as
	// such; making, renaming or letting go of the pair property meets what the
	// person's own change of it would.
	mirrorsRows := work.Kind == databaseRelationPairLinks || work.Kind == databaseRelationPairBackfill
	mutate := func(turn string, build func(core.App) ([]graphOperation, error)) error {
		mutationID := "relation-pair:" + work.ID + ":" + turn
		digest := sha256.Sum256([]byte("database-relation-pair\x00" + targetPageID + "\x00" + mutationID))
		_, err := p.applyContentGraphMutationContext(ctx, work.ActorID, targetPageID, graphMutationInput{MutationID: mutationID},
			contentGraphMutationHooks{
				SkipRelationPairSync: true, SuppressAutomationEvents: true, SuppressNotifications: true,
				AllowTrashedPage: true, PreserveRowEditStamps: true, MirrorsRelationPair: mirrorsRows,
				Authorize: func(txApp core.App) (pageAuthorization, error) {
					return system.authorizePageBody(txApp, targetPageID, permissionWorkspaceWrite)
				},
				ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
					return hex.EncodeToString(digest[:]), nil
				},
				Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
					operations, err := build(txApp)
					if err != nil || len(operations) == 0 {
						return contentGraphMutationPreparation{Deferred: true}, err
					}
					return contentGraphMutationPreparation{Operations: operations}, nil
				},
			})
		return err
	}
	switch work.Kind {
	case databaseRelationPairEnsure:
		ensured := false
		if err := mutate("ensure", func(txApp core.App) ([]graphOperation, error) {
			operations, err := databaseRelationPairEnsureOperations(txApp, work.OwnerID, targetPageID, payload)
			if errors.Is(err, errDatabaseRelationPairStale) {
				return nil, nil
			}
			ensured = err == nil
			return operations, err
		}); err != nil {
			return false, err
		}
		if !ensured {
			return true, nil
		}
		// The pair's existing values reach the other side in turns.
		err := p.app.RunInTransaction(func(txApp core.App) error {
			backfill := payload
			backfill.Name = ""
			return admitDatabaseRelationPairWork(txApp, work.OwnerID, work.WorkspaceID, work.ActorID, "backfill:"+work.ID,
				[]databaseRelationPairWork{{Kind: databaseRelationPairBackfill, Payload: backfill}})
		})
		return err == nil, err
	case databaseRelationPairBackfill:
		// The backfill once queued for every relation to its own database
		// ends without changing its values: one made before the symmetric
		// mark relates one way (databaseRelationSymmetricKey).
		if payload.PairPropertyID == payload.PropertyID {
			return true, nil
		}
		// The window's end is read first, so a replayed turn still moves on.
		_, next, err := databaseRelationPairBackfillOperations(p.app, work.OwnerID, payload)
		if err != nil {
			return false, err
		}
		if err := mutate("backfill:"+payload.Cursor, func(txApp core.App) ([]graphOperation, error) {
			operations, _, err := databaseRelationPairBackfillOperations(txApp, work.OwnerID, payload)
			return operations, err
		}); err != nil {
			return false, err
		}
		if next == "" {
			return true, nil
		}
		payload.Cursor = next
		encoded, err := json.Marshal(payload)
		if err != nil {
			return false, err
		}
		_, err = p.app.DB().NewQuery(`UPDATE {{leafen_database_relation_pair_work}}
			SET [[payload_json]]={:payload}, [[updated_at]]={:now} WHERE [[id]]={:id}`).Bind(dbx.Params{
			"payload": string(encoded), "now": time.Now().UTC().UnixMilli(), "id": work.ID,
		}).Execute()
		return false, err
	case databaseRelationPairLinks:
		return true, mutate("links", func(txApp core.App) ([]graphOperation, error) {
			taken := map[string][]string{}
			operations, err := databaseRelationPairLinkOperationsTaking(txApp, work.OwnerID, payload, taken)
			if err != nil || len(taken) == 0 {
				return operations, err
			}
			// A row its other side keeps to one page leaves the pages it let go,
			// as their own lists, in the same turns.
			reverse := databaseRelationPairPayload{
				SourceDataSourceID: payload.TargetDataSourceID, PropertyID: payload.PairPropertyID,
				TargetDataSourceID: payload.SourceDataSourceID, PairPropertyID: payload.PropertyID,
			}
			for _, rowID := range workspaceExportSortedKeys(taken) {
				reverse.Links = append(reverse.Links, databaseRelationPairLink{RowID: rowID, Remove: taken[rowID]})
			}
			return operations, admitDatabaseRelationPairWork(txApp, work.OwnerID, work.WorkspaceID, work.ActorID, "limit:"+work.ID,
				[]databaseRelationPairWork{{Kind: databaseRelationPairLinks, Payload: reverse}})
		})
	case databaseRelationPairDelete:
		return true, p.deleteDatabaseRelationPair(ctx, work, payload, targetPageID, system, mutate)
	case databaseRelationPairHideNew:
		return true, mutate("hide-new", func(txApp core.App) ([]graphOperation, error) {
			return databaseNewPropertiesHiddenOnPage(txApp, work.OwnerID, payload)
		})
	}
	return true, nil
}

func loadDatabaseRelationPairTargetProperty(app core.App, ownerID, id string) (databaseRelationPairTargetProperty, map[string]any, bool, error) {
	property := databaseRelationPairTargetProperty{}
	err := app.DB().NewQuery(`SELECT [[id]], [[data_source_id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_properties}} WHERE [[owner]]={:owner} AND [[id]]={:id}`).Bind(dbx.Params{
		"owner": ownerID, "id": id,
	}).One(&property)
	if errors.Is(err, sql.ErrNoRows) {
		return property, nil, false, nil
	}
	if err != nil {
		return property, nil, false, err
	}
	config := map[string]any{}
	_ = json.Unmarshal([]byte(property.ConfigJSON), &config)
	return property, config, true, nil
}

// databaseRelationPairEnsureOperations creates or relinks the pair property
// while the source property still names it, keeping its name unless the
// source asks for another one.
func databaseRelationPairEnsureOperations(app core.App, ownerID, targetPageID string, payload databaseRelationPairPayload) ([]graphOperation, error) {
	source, sourceConfig, exists, err := loadDatabaseRelationPairTargetProperty(app, ownerID, payload.PropertyID)
	if err != nil {
		return nil, err
	}
	pair := databaseRelationPairOf(source.ID, source.DataSourceID, source.Type, sourceConfig)
	if !exists || pair == nil || pair.TargetDataSourceID != payload.TargetDataSourceID || pair.PairPropertyID != payload.PairPropertyID {
		return nil, errDatabaseRelationPairStale
	}
	current, config, exists, err := loadDatabaseRelationPairTargetProperty(app, ownerID, payload.PairPropertyID)
	if err != nil {
		return nil, err
	}
	if exists && (current.DataSourceID != payload.TargetDataSourceID || current.Type != graphValueTypeRelation) {
		return nil, errDatabaseRelationPairStale
	}
	// A side another relation pairs with stays that one's (database_relation_half_pairs.go).
	if taken, err := databaseRelationPairTakenElsewhere(app, ownerID, payload.PropertyID, payload.PairPropertyID); err != nil {
		return nil, err
	} else if taken {
		return nil, errDatabaseRelationPairStale
	}
	if config == nil {
		config = map[string]any{}
	}
	delete(config, "reciprocalName")
	linked := config["relationDatabaseId"] == payload.SourceDataSourceID && config["relatedPropertyId"] == payload.PropertyID
	config["relationDatabaseId"], config["relatedPropertyId"] = payload.SourceDataSourceID, payload.PropertyID
	name := strings.TrimSpace(payload.Name)
	if name == "" && exists {
		name = current.Name
	}
	if name == "" {
		if name, err = databaseRelationPairDefaultName(app, ownerID, payload.SourceDataSourceID); err != nil {
			return nil, err
		}
	}
	position := current.Position
	if !exists {
		var last string
		if err := app.DB().NewQuery(`SELECT COALESCE(MAX([[position]]), '') FROM {{leafen_database_properties}}
			WHERE [[owner]]={:owner} AND [[data_source_id]]={:source}`).Bind(dbx.Params{
			"owner": ownerID, "source": payload.TargetDataSourceID,
		}).Row(&last); err != nil {
			return nil, err
		}
		position = last + "r"
	}
	if exists && linked && name == current.Name {
		return nil, nil
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	operations := []graphOperation{{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
		ID: payload.PairPropertyID, DataSourceID: payload.TargetDataSourceID, Type: graphValueTypeRelation,
		Name: name, Position: position, Config: encoded,
	}}}
	if exists {
		return operations, nil
	}
	hidden, err := databaseNewPropertiesHiddenInTables(app, targetPageID, payload.TargetDataSourceID, []string{payload.PairPropertyID})
	if err != nil {
		return nil, err
	}
	return append(operations, hidden...), nil
}

var errDatabaseRelationPairStale = errors.New("database relation pair is stale")

// databaseRelationPairDefaultName is what a pair made without a name is
// called: the source data source's title.
func databaseRelationPairDefaultName(app core.App, ownerID, sourceDataSourceID string) (string, error) {
	name := ""
	if err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[owner]]={:owner} AND [[id]]={:id}`).
		Bind(dbx.Params{"owner": ownerID, "id": sourceDataSourceID}).Row(&name); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		return "Related", nil
	}
	return strings.TrimSpace(name), nil
}

// databaseRelationPairRowOperations rewrites each target row's pair value
// with the source rows it gains and loses.
func databaseRelationPairRowOperations(
	app core.App, ownerID string, payload databaseRelationPairPayload, changes map[string][2][]string,
) ([]graphOperation, error) {
	return databaseRelationPairRowOperationsTaking(app, ownerID, payload, changes, nil)
}

// databaseRelationPairRowOperationsTaking is databaseRelationPairRowOperations
// that, given taken, keeps a side limited to one page to the page just added
// (keepDatabaseRelationLimit), and names in taken the pages each row let go.
func databaseRelationPairRowOperationsTaking(
	app core.App, ownerID string, payload databaseRelationPairPayload, changes map[string][2][]string, taken map[string][]string,
) ([]graphOperation, error) {
	pair, pairConfig, exists, err := loadDatabaseRelationPairTargetProperty(app, ownerID, payload.PairPropertyID)
	if err != nil || !exists || pair.DataSourceID != payload.TargetDataSourceID || pair.Type != graphValueTypeRelation {
		return nil, err
	}
	operations := []graphOperation{}
	for _, targetID := range workspaceExportSortedKeys(changes) {
		row := struct {
			DataSourceID    string `db:"data_source_id"`
			Position        string `db:"position"`
			SubitemParentID string `db:"subitem_parent_id"`
		}{}
		err := app.DB().NewQuery(`SELECT [[data_source_id]], [[position]], [[subitem_parent_id]] FROM {{leafen_database_rows}}
			WHERE [[owner]]={:owner} AND [[id]]={:id}`).Bind(dbx.Params{"owner": ownerID, "id": targetID}).One(&row)
		if errors.Is(err, sql.ErrNoRows) || err == nil && row.DataSourceID != payload.TargetDataSourceID {
			continue
		}
		if err != nil {
			return nil, err
		}
		current, err := loadDatabaseRelationPairTargets(app, ownerID, targetID, payload.PairPropertyID)
		if err != nil {
			return nil, err
		}
		next := mergeDatabaseRelationPairTargets(current, changes[targetID])
		if taken != nil {
			var letGo []string
			if next, letGo = keepDatabaseRelationLimit(current, next, databaseRelationLimit(pairConfig)); len(letGo) > 0 {
				taken[targetID] = append(taken[targetID], letGo...)
			}
		}
		if add, remove := databaseRelationPairDiff(current, next); len(add) == 0 && len(remove) == 0 {
			continue
		}
		encoded, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		var subitemParentID *string
		if row.SubitemParentID != "" {
			subitemParentID = &row.SubitemParentID
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
			ID: targetID, DataSourceID: row.DataSourceID, SubitemParentID: subitemParentID, Position: row.Position,
			Values: map[string]graphRowValue{payload.PairPropertyID: {Type: graphValueTypeRelation, Value: encoded}},
		}})
	}
	return operations, nil
}

func databaseRelationPairLinkOperations(app core.App, ownerID string, payload databaseRelationPairPayload) ([]graphOperation, error) {
	return databaseRelationPairLinkOperationsTaking(app, ownerID, payload, nil)
}

func databaseRelationPairLinkOperationsTaking(
	app core.App, ownerID string, payload databaseRelationPairPayload, taken map[string][]string,
) ([]graphOperation, error) {
	// A side naming another relation is not this one's to write.
	if named, err := databaseRelationPairNamesBack(app, ownerID, payload); err != nil || !named {
		return nil, err
	}
	changes := map[string][2][]string{}
	for _, link := range payload.Links {
		for _, targetID := range link.Add {
			entry := changes[targetID]
			entry[0] = append(entry[0], link.RowID)
			changes[targetID] = entry
		}
		for _, targetID := range link.Remove {
			entry := changes[targetID]
			entry[1] = append(entry[1], link.RowID)
			changes[targetID] = entry
		}
	}
	return databaseRelationPairRowOperationsTaking(app, ownerID, payload, changes, taken)
}

// databaseRelationPairBackfillOperations gives a window of target rows the
// source rows that already relate to them; the returned cursor continues it.
func databaseRelationPairBackfillOperations(
	app core.App, ownerID string, payload databaseRelationPairPayload,
) ([]graphOperation, string, error) {
	if named, err := databaseRelationPairNamesBack(app, ownerID, payload); err != nil || !named {
		return nil, "", err
	}
	rows := []struct {
		TargetID string `db:"target_row_id"`
		Sources  string `db:"sources"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[items]].[[target_row_id]], json_group_array([[items]].[[row_id]]) AS [[sources]]
		FROM {{leafen_database_relation_items}} AS [[items]]
		WHERE [[items]].[[owner]]={:owner} AND [[items]].[[property_id]]={:property}
			AND [[items]].[[target_row_id]]>{:cursor}
		GROUP BY [[items]].[[target_row_id]] ORDER BY [[items]].[[target_row_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": ownerID, "property": payload.PropertyID, "cursor": payload.Cursor,
		"limit": databaseRelationPairBackfillRows,
	}).All(&rows)
	if err != nil {
		return nil, "", err
	}
	changes := map[string][2][]string{}
	for _, row := range rows {
		sources := []string{}
		if err := json.Unmarshal([]byte(row.Sources), &sources); err != nil {
			return nil, "", err
		}
		changes[row.TargetID] = [2][]string{sources, nil}
	}
	operations, err := databaseRelationPairRowOperations(app, ownerID, payload, changes)
	next := ""
	if len(rows) == databaseRelationPairBackfillRows {
		next = rows[len(rows)-1].TargetID
	}
	return operations, next, err
}

// deleteDatabaseRelationPair deletes the pair property while it still names
// the source property, through the database deletion lifecycle; a pair the
// lifecycle refuses to delete becomes one-way instead.
func (p *pocketBaseYgoPersistence) deleteDatabaseRelationPair(
	ctx context.Context,
	work storedDatabaseRelationPairWork,
	payload databaseRelationPairPayload,
	targetPageID string, system systemJobSubject,
	mutate func(string, func(core.App) ([]graphOperation, error)) error,
) error {
	pair, config, exists, err := loadDatabaseRelationPairTargetProperty(p.app, work.OwnerID, payload.PairPropertyID)
	if err != nil || !exists {
		return err
	}
	if linked, _ := config["relatedPropertyId"].(string); linked != payload.PropertyID || pair.DataSourceID != payload.TargetDataSourceID {
		return nil
	}
	unpair := func() error {
		return mutate("unpair", func(txApp core.App) ([]graphOperation, error) {
			current, currentConfig, exists, err := loadDatabaseRelationPairTargetProperty(txApp, work.OwnerID, payload.PairPropertyID)
			if err != nil || !exists || currentConfig["relatedPropertyId"] != payload.PropertyID {
				return nil, err
			}
			delete(currentConfig, "relatedPropertyId")
			encoded, err := json.Marshal(currentConfig)
			if err != nil {
				return nil, err
			}
			return []graphOperation{{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
				ID: current.ID, DataSourceID: current.DataSourceID, Type: current.Type,
				Name: current.Name, Position: current.Position, Config: encoded,
			}}}, nil
		})
	}
	if payload.Unpair {
		return unpair()
	}
	var pending int
	if err := p.app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_mutation_jobs}}
		WHERE [[target_id]]={:id} AND [[status]]={:pending}`).Bind(dbx.Params{
		"id": payload.PairPropertyID, "pending": databaseMutationStatusPending,
	}).Row(&pending); err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}
	_, err = admitDatabaseMutationDeletionBy(p.app, system, targetPageID, databaseMutationInput{
		RequestID: "relation-pair-" + work.ID, MutationID: "relation-pair-delete:" + work.ID,
		TargetKind: databaseMutationTargetProperty, TargetID: payload.PairPropertyID,
	})
	if err == nil {
		notifyDatabaseMutationDeletionAfterCommit(p.app)
		return nil
	}
	if ctx.Err() != nil {
		return err
	}
	return unpair()
}
