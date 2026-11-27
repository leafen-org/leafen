package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// The unique ID runner readies one ID property at a time: it finds the
// largest number its rows already carry, in bounded windows, then numbers
// the rows that have none in the order they were created. Once every row
// has a number, new rows are numbered as they are added. It writes as the
// system (system_job_access.go), in the workspace owner's name, and changes
// no one's notifications or automations.

const (
	databaseUniqueIDRunnerStoreKey  = "leafenDatabaseUniqueIDRunner"
	databaseUniqueIDBackfillRows    = 64
	databaseUniqueIDRunnerIdle      = 5 * time.Minute
	databaseUniqueIDFailureDelay    = time.Second
	databaseUniqueIDFailureMaxDelay = 10 * time.Minute
)

type storedDatabaseUniqueIDWork struct {
	Owner         string `db:"owner"`
	WorkspaceID   string `db:"workspace_id"`
	PageID        string `db:"page_id"`
	DataSourceID  string `db:"data_source_id"`
	PropertyID    string `db:"id"`
	Updated       int64  `db:"updated"`
	HasAllocator  bool   `db:"has_allocator"`
	Revision      int64  `db:"property_revision"`
	ScanAfter     string `db:"scan_after"`
	MaximumSeen   int64  `db:"maximum_seen"`
	NextValue     int64  `db:"next_value"`
	Initialized   bool   `db:"initialized"`
	BackfillAfter string `db:"backfill_after"`
}

type databaseUniqueIDRunner struct {
	app  core.App
	ygo  *pocketBaseYgoPersistence
	wake chan struct{}

	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	failures map[string]databaseUniqueIDFailure
}

type databaseUniqueIDFailure struct {
	attempts int
	until    time.Time
}

func newDatabaseUniqueIDRunner(app core.App, ygo *pocketBaseYgoPersistence) *databaseUniqueIDRunner {
	return &databaseUniqueIDRunner{
		app: app, ygo: ygo, wake: make(chan struct{}, 1), failures: map[string]databaseUniqueIDFailure{},
	}
}

func (runner *databaseUniqueIDRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel, runner.done = cancel, make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *databaseUniqueIDRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("database unique ID shutdown: %w", ctx.Err())
	}
}

func (runner *databaseUniqueIDRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func notifyDatabaseUniqueIDRunnerAfterCommit(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(databaseUniqueIDRunnerStoreKey).(*databaseUniqueIDRunner); ok {
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

func (runner *databaseUniqueIDRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait, err := runner.runTurn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeJobRunnerFailed, "database-unique-id", err)
			wait = databaseUniqueIDFailureDelay
		}
		if wait == 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-runner.wake:
		case <-time.After(wait):
		}
	}
}

// runTurn takes one step for the first ID property that is not ready; it
// waits for a wake-up, or the next retry, when there is none. A property
// whose cells are still being converted from another type waits for the
// conversion, so numbers the conversion clears are not counted; one in an
// import's staging database, which no one may write yet, waits for the
// import to show it (2026-10-16: it failed there every few minutes for good).
func (runner *databaseUniqueIDRunner) runTurn(ctx context.Context) (time.Duration, error) {
	candidates := []storedDatabaseUniqueIDWork{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[property]].[[owner]], [[property]].[[workspace_id]], [[property]].[[page_id]],
			[[property]].[[data_source_id]], [[property]].[[id]], [[property]].[[updated]],
			[[allocator]].[[property_id]] IS NOT NULL AS [[has_allocator]],
			COALESCE([[allocator]].[[property_revision]], 0) AS [[property_revision]],
			COALESCE([[allocator]].[[scan_after]], '') AS [[scan_after]],
			COALESCE([[allocator]].[[maximum_seen]], 0) AS [[maximum_seen]],
			COALESCE([[allocator]].[[next_value]], 1) AS [[next_value]],
			COALESCE([[allocator]].[[initialized]], 0) AS [[initialized]],
			COALESCE([[allocator]].[[backfill_after]], '') AS [[backfill_after]]
		FROM {{leafen_database_properties}} [[property]]
		LEFT JOIN {{leafen_database_unique_id_allocators}} [[allocator]]
			ON [[allocator]].[[owner]] = [[property]].[[owner]]
			AND [[allocator]].[[page_id]] = [[property]].[[page_id]]
			AND [[allocator]].[[property_id]] = [[property]].[[id]]
		WHERE [[property]].[[type]] = 'unique_id'
			AND ([[allocator]].[[property_id]] IS NULL OR [[allocator]].[[initialized]] = 0
				OR [[allocator]].[[backfilled]] = 0)
			-- An import's staging database waits until the import shows it.
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_pages}} [[page]]
				WHERE [[page]].[[id]] = [[property]].[[page_id]] AND [[page]].[[notion_import_staging]] = TRUE
			)
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_property_conversions}} [[conversion]]
				WHERE [[conversion]].[[property_id]] = [[property]].[[id]] AND [[conversion]].[[status]] = 'pending'
					AND [[conversion]].[[page_id]] = [[property]].[[page_id]]
			)
		ORDER BY [[property]].[[updated]], [[property]].[[id]] LIMIT 32
	`).All(&candidates); err != nil {
		return -1, err
	}
	now := time.Now()
	retry := databaseUniqueIDRunnerIdle
	runner.mu.Lock()
	for key, failure := range runner.failures {
		if now.Sub(failure.until) > databaseUniqueIDRunnerIdle {
			delete(runner.failures, key)
		}
	}
	var work *storedDatabaseUniqueIDWork
	for index := range candidates {
		failure, failed := runner.failures[candidates[index].PropertyID]
		if failed && now.Before(failure.until) {
			retry = min(retry, failure.until.Sub(now))
			continue
		}
		work = &candidates[index]
		break
	}
	runner.mu.Unlock()
	if work == nil {
		return max(retry, 10*time.Millisecond), nil
	}
	var err error
	if !work.HasAllocator || !work.Initialized {
		err = scanDatabaseUniqueIDMaximum(runner.app, *work)
	} else {
		err = runner.ygo.backfillDatabaseUniqueIDs(ctx, *work)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if err != nil {
		if ctx.Err() != nil {
			return -1, err
		}
		failure := runner.failures[work.PropertyID]
		failure.attempts++
		failure.until = now.Add(min(databaseUniqueIDFailureDelay<<min(failure.attempts, 16), databaseUniqueIDFailureMaxDelay))
		runner.failures[work.PropertyID] = failure
		logDurableJobDiagnostic(
			runner.app, errorCodeJobRunnerFailed, newDiagnosticReference(), "",
			"database-unique-id", "property-turn", err,
		)
		return 0, nil
	}
	delete(runner.failures, work.PropertyID)
	return 0, nil
}

// scanDatabaseUniqueIDMaximum reads one window of an ID property's numbers,
// continuing the scan an earlier turn began for the same property revision.
func scanDatabaseUniqueIDMaximum(app core.App, work storedDatabaseUniqueIDWork) error {
	return app.RunInTransaction(func(txApp core.App) error {
		stored, exists, err := loadDatabaseUniqueIDAllocator(txApp, work)
		if err != nil {
			return err
		}
		if exists && stored.Initialized {
			return nil
		}
		scanAfter, maximumSeen := "", int64(0)
		if exists && stored.PropertyRevision == work.Updated {
			scanAfter, maximumSeen = stored.ScanAfter, stored.MaximumSeen
		}
		window, err := loadDatabaseUniqueIDScanWindow(txApp, work, scanAfter)
		if err != nil {
			return err
		}
		limit := min(len(window), databaseUniqueIDScanWindow)
		for _, row := range window[:limit] {
			if value, ok := parseDatabaseUniqueIDValue(row.Value); ok && value > maximumSeen {
				maximumSeen = value
			}
		}
		if maximumSeen >= maxDatabaseUniqueIDValue {
			return errDatabaseUniqueIDAllocatorInvalid
		}
		progress := storedDatabaseUniqueIDAllocator{
			Owner: work.Owner, WorkspaceID: work.WorkspaceID, PageID: work.PageID, DataSourceID: work.DataSourceID,
			PropertyID: work.PropertyID, PropertyRevision: work.Updated,
			NextValue: maximumSeen + 1, MaximumSeen: maximumSeen, Initialized: len(window) <= databaseUniqueIDScanWindow,
		}
		if !progress.Initialized {
			progress.ScanAfter = window[limit-1].RowID
		}
		return saveDatabaseUniqueIDProgress(txApp, progress)
	})
}

type databaseUniqueIDBackfillRow struct {
	ID      string `db:"id"`
	Created int64  `db:"created"`
}

// backfillDatabaseUniqueIDs numbers the next rows, in creation order, that
// have no number, or records that every row has one.
func (p *pocketBaseYgoPersistence) backfillDatabaseUniqueIDs(ctx context.Context, work storedDatabaseUniqueIDWork) error {
	workspace, err := p.app.FindRecordById(workspacesCollection, work.WorkspaceID)
	if err != nil {
		return err
	}
	system := systemJobSubjectFor(workspace)
	mutationID := "unique-id-backfill:" + work.PropertyID + ":" + strconv.FormatInt(work.NextValue, 10) + ":" + work.BackfillAfter
	digest := sha256.Sum256([]byte("database-unique-id-backfill\x00" + work.PageID + "\x00" + mutationID))
	_, err = p.applyContentGraphMutationContext(ctx, system.actorID(), work.PageID, graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			SkipRelationPairSync: true, SuppressAutomationEvents: true, SuppressNotifications: true,
			AllowTrashedPage: true, PreserveRowEditStamps: true,
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				return system.authorizePageBody(txApp, work.PageID, permissionWorkspaceWrite)
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				operations, err := databaseUniqueIDBackfillOperations(txApp, work)
				if err != nil || len(operations) == 0 {
					return contentGraphMutationPreparation{Deferred: true}, err
				}
				return contentGraphMutationPreparation{
					Operations: operations, RuntimeWritablePropertyIDs: map[string]struct{}{work.PropertyID: {}},
				}, nil
			},
		})
	return err
}

// databaseUniqueIDBackfillOperations numbers one window of unnumbered rows
// and moves the allocator past them; with none left it marks the property
// ready for new rows.
func databaseUniqueIDBackfillOperations(
	app core.App,
	work storedDatabaseUniqueIDWork,
) ([]graphOperation, error) {
	current := struct {
		NextValue     int64  `db:"next_value"`
		Initialized   bool   `db:"initialized"`
		Backfilled    bool   `db:"backfilled"`
		BackfillAfter string `db:"backfill_after"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[next_value]], [[initialized]], [[backfilled]], [[backfill_after]]
		FROM {{leafen_database_unique_id_allocators}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[property_id]] = {:propertyId}
	`).Bind(dbx.Params{"owner": work.Owner, "pageId": work.PageID, "propertyId": work.PropertyID}).One(&current); err != nil {
		return nil, err
	}
	if !current.Initialized || current.Backfilled || current.NextValue != work.NextValue ||
		current.BackfillAfter != work.BackfillAfter {
		return nil, nil
	}
	createdAfter, idAfter := int64(-1), ""
	if created, id, found := strings.Cut(current.BackfillAfter, ":"); found {
		parsed, err := strconv.ParseInt(created, 10, 64)
		if err != nil {
			return nil, errDatabaseUniqueIDAllocatorInvalid
		}
		createdAfter, idAfter = parsed, id
	}
	rows, err := loadDatabaseUniqueIDBackfillRows(app, work, createdAfter, idAfter, databaseUniqueIDBackfillRows)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{
		"owner": work.Owner, "pageId": work.PageID, "propertyId": work.PropertyID,
		"now": time.Now().UTC().UnixMilli(),
	}
	if len(rows) == 0 {
		// Rows created out of order are found by one last look from the
		// start; rows the page's content no longer has can't be numbered.
		remaining, err := loadDatabaseUniqueIDBackfillRows(app, work, -1, "", databaseUniqueIDBackfillRows)
		if err != nil {
			return nil, err
		}
		backfilled := true
		for _, row := range remaining {
			source, err := databaseRowRecordSource(app, work.PageID, row.ID)
			if err != nil {
				return nil, err
			}
			if source == work.DataSourceID {
				backfilled = false
				break
			}
		}
		params["backfilled"], params["after"] = backfilled, ""
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_database_unique_id_allocators}}
			SET [[backfilled]] = {:backfilled}, [[backfill_after]] = {:after}, [[updated_at]] = {:now}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[property_id]] = {:propertyId}
		`).Bind(params).Execute()
		return nil, err
	}
	if current.NextValue+int64(len(rows))-1 > maxDatabaseUniqueIDValue {
		return nil, errDatabaseUniqueIDAllocatorInvalid
	}
	operations := make([]graphOperation, 0, len(rows))
	value := current.NextValue
	for _, row := range rows {
		stored, exists, err := loadDatabaseRowRecord(app, work.PageID, row.ID)
		if err != nil {
			return nil, err
		}
		if !exists || stored.DataSourceID != work.DataSourceID {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var parent *string
		if parentID := stored.SubitemParentID; parentID != "" {
			parent = &parentID
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
			ID: row.ID, DataSourceID: stored.DataSourceID, SubitemParentID: parent, Position: stored.Position,
			Values: map[string]graphRowValue{work.PropertyID: {Type: graphValueTypeUniqueID, Value: encoded}},
		}})
		value++
	}
	last := rows[len(rows)-1]
	params["next"], params["seen"] = value, value-1
	params["after"] = strconv.FormatInt(last.Created, 10) + ":" + last.ID
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_database_unique_id_allocators}}
		SET [[next_value]] = {:next}, [[maximum_seen]] = MAX([[maximum_seen]], {:seen}),
			[[backfill_after]] = {:after}, [[updated_at]] = {:now}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[property_id]] = {:propertyId}
	`).Bind(params).Execute()
	return operations, err
}

func loadDatabaseUniqueIDBackfillRows(
	app core.App,
	work storedDatabaseUniqueIDWork,
	createdAfter int64, idAfter string,
	limit int,
) ([]databaseUniqueIDBackfillRow, error) {
	rows := []databaseUniqueIDBackfillRow{}
	err := app.DB().NewQuery(`
		SELECT [[row]].[[id]], [[row]].[[created]]
		FROM {{leafen_database_rows}} [[row]]
		WHERE [[row]].[[owner]] = {:owner} AND [[row]].[[page_id]] = {:pageId}
			AND [[row]].[[data_source_id]] = {:sourceId}
			AND ([[row]].[[created]] > {:createdAfter}
				OR ([[row]].[[created]] = {:createdAfter} AND [[row]].[[id]] > {:idAfter}))
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_row_values}} [[value]]
				WHERE [[value]].[[row_id]] = [[row]].[[id]] AND [[value]].[[property_id]] = {:propertyId}
			)
		ORDER BY [[row]].[[created]], [[row]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": work.Owner, "pageId": work.PageID, "sourceId": work.DataSourceID,
		"createdAfter": createdAfter, "idAfter": idAfter, "propertyId": work.PropertyID, "limit": limit,
	}).All(&rows)
	return rows, err
}
