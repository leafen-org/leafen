package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A person's design-first Notion migrations (B5): one is made from their
// Notion connection and the pages and databases it moves, then scanned step
// by step (notion_migration_scan.go). The person's screen and their agent's
// MCP tools read and step the same record; only its owner sees it.

const (
	notionMigrationStatusScanning = "scanning"
	notionMigrationStatusScanned  = "scanned"
	notionMigrationStatusFailed   = "failed"

	// A scanned migration's run: its plan being carried out, then done. A
	// run whose import job failed or was canceled reads as failed (kept as
	// running with that job): it may run again.
	notionMigrationRunRunning = "running"
	notionMigrationRunDone    = "done"
	notionMigrationRunFailed  = "failed"

	notionMigrationIssuePageUnavailable   = "page_unavailable"
	notionMigrationIssueBlocksUnavailable = "blocks_unavailable"
	notionMigrationLockNamespace          = "notion-migration"
	notionMigrationStepTimeout            = 25 * time.Second
)

var (
	errNotionMigrationInvalid  = errors.New(errorCodeNotionMigrationInvalid)
	errNotionMigrationNotFound = errors.New(errorCodeNotionMigrationNotFound)
	// A plan is made from a finished scan, over the revision it was read at.
	errNotionMigrationNotScanned   = errors.New(errorCodeNotionMigrationNotScanned)
	errNotionMigrationPlanConflict = errors.New(errorCodeNotionMigrationPlanConflict)
)

type storedNotionMigration struct {
	ID           string `db:"id"`
	WorkspaceID  string `db:"workspace_id"`
	Owner        string `db:"owner"`
	ConnectionID string `db:"connection_id"`
	RootsJSON    string `db:"roots_json"`
	Status       string `db:"status"`
	ScanState    string `db:"scan_state_json"`
	SummaryJSON  string `db:"summary_json"`
	ErrorCode    string `db:"error_code"`
	PlanJSON     string `db:"plan_json"`
	PlanRevision int    `db:"plan_revision"`
	RunState     string `db:"run_state"`
	RunRevision  int    `db:"run_plan_revision"`
	ImportJobID  string `db:"import_job_id"`
	RunReport    string `db:"run_report_json"`
	RunStarted   int64  `db:"run_started_at"`
	// LastDoneStarted is when the last run that finished began: a run after
	// it moves what changed since, though a failed run came between.
	LastDoneStarted int64 `db:"last_done_run_started_at"`
	AgentHandoff    bool  `db:"agent_handoff"`
	// SourceLabel names the tool a bundle source's migration moves from.
	SourceLabel string `db:"source_label"`
	CreatedAt   int64  `db:"created_at"`
	UpdatedAt   int64  `db:"updated_at"`
	// JobChanged is when the run's import job last changed, while it is kept.
	JobChanged int64 `db:"-"`
}

func registerNotionMigrationRoutes(event *core.ServeEvent, jobs *durableJobRunner) {
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/notion-migrations", createNotionMigrationRoute)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/notion-migrations", listNotionMigrationsRoute)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/notion-migrations/{migrationId}", readNotionMigrationRoute)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/notion-migrations/{migrationId}/scan", jobs.scanNotionMigrationRoute)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/notion-migrations/{migrationId}/scan-items", readNotionMigrationScanItemsRoute)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/notion-migrations/{migrationId}/plan", readNotionMigrationPlanRoute)
	event.Router.PUT(apiRoot+"/workspaces/{workspaceId}/notion-migrations/{migrationId}/plan", saveNotionMigrationPlanRoute)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/notion-migrations/{migrationId}/run", runNotionMigrationRoute)
}

func notionMigrationRouteError(event *core.RequestEvent, err error, step string) error {
	switch {
	case errors.Is(err, errNotionMigrationInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeNotionMigrationInvalid, refusalMessage("Check the migration.", refusalReason(err)))
	case errors.Is(err, errNotionMigrationNotFound), isPermissionDenied(err):
		return apiError(event, http.StatusNotFound, errorCodeNotionMigrationNotFound, "Migration not found.")
	case errors.Is(err, errNotionMigrationNotScanned):
		return apiError(event, http.StatusConflict, errorCodeNotionMigrationNotScanned, "Scan the migration first.")
	case errors.Is(err, errNotionMigrationPlanConflict):
		return apiError(event, http.StatusConflict, errorCodeNotionMigrationPlanConflict, "The plan changed; read it again.")
	case errors.Is(err, errNotionMigrationPlanBlocked):
		return apiError(event, http.StatusConflict, errorCodeNotionMigrationPlanBlocked, "Fix the plan's problems first.")
	case errors.Is(err, errNotionMigrationRunning):
		return apiError(event, http.StatusConflict, errorCodeNotionMigrationRunning, "The migration is running.")
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeNotionMigrationFailed,
		"Couldn't run the migration.", "notion_migration", step, errorCodeNotionMigrationFailed, err)
}

// createNotionMigration starts a migration of the given roots from one of
// the person's Notion connections in the workspace.
func createNotionMigration(app core.App, ownerID, workspaceID string, input notionMigrationInput, agent bool) (notionMigration, error) {
	if _, err := authorizeWorkspace(app, ownerID, workspaceID, permissionWorkspaceWrite); err != nil {
		return notionMigration{}, err
	}
	input.ConnectionID = strings.TrimSpace(input.ConnectionID)
	if input.ConnectionID == "" {
		return notionMigration{}, refuse(errNotionMigrationInvalid, "connectionId is empty: name one of your Notion connections in this workspace")
	}
	roots, err := notionMigrationStartRoots(input.Roots)
	if err != nil {
		return notionMigration{}, err
	}
	if _, err := loadNotionConnection(app, input.ConnectionID, ownerID, workspaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notionMigration{}, refuse(errNotionMigrationInvalid, "connectionId names none of your Notion connections in this workspace")
		}
		return notionMigration{}, err
	}
	// Roots moved here before move again by running that move again
	// (notion_migration_moved_before.go).
	if err := notionMigrationStartMovedBefore(app, ownerID, workspaceID, roots, input.SecondCopy); err != nil {
		return notionMigration{}, err
	}
	rootsJSON, err := json.Marshal(roots)
	if err != nil {
		return notionMigration{}, err
	}
	summary, err := json.Marshal(notionMigrationSummary{})
	if err != nil {
		return notionMigration{}, err
	}
	now := time.Now().UTC().UnixMilli()
	stored := storedNotionMigration{
		ID: security.RandomString(15), WorkspaceID: workspaceID, Owner: ownerID, ConnectionID: input.ConnectionID,
		RootsJSON: string(rootsJSON), Status: notionMigrationStatusScanning, ScanState: "{}",
		SummaryJSON: string(summary), AgentHandoff: agent, CreatedAt: now, UpdatedAt: now,
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_notion_migrations}}
				([[id]],[[workspace_id]],[[owner]],[[connection_id]],[[roots_json]],[[status]],[[scan_state_json]],
				 [[summary_json]],[[agent_handoff]],[[second_copy]],[[created_at]],[[updated_at]])
			VALUES ({:id},{:workspaceId},{:owner},{:connectionId},{:roots},{:status},'{}',{:summary},{:agent},{:secondCopy},{:now},{:now})
		`).Bind(dbx.Params{
			"id": stored.ID, "workspaceId": workspaceID, "owner": ownerID, "connectionId": input.ConnectionID, "secondCopy": input.SecondCopy,
			"roots": stored.RootsJSON, "status": stored.Status, "summary": stored.SummaryJSON, "agent": agent, "now": now,
		}).Execute(); err != nil {
			return err
		}
		store := notionMigrationScanStore{app: txApp, migrationID: stored.ID, workspaceID: workspaceID}
		return store.pushFront(newNotionMigrationScanState(roots).Queue...)
	})
	if err != nil {
		return notionMigration{}, err
	}
	// As read back, naming the tool a bundle source moves from.
	stored, err = loadNotionMigration(app, ownerID, workspaceID, stored.ID)
	if err != nil {
		return notionMigration{}, err
	}
	return notionMigrationWire(stored)
}

// saveNotionMigrationCounts keeps a scan's counts in its migration's row.
func saveNotionMigrationCounts(app core.App, migrationID string, counts notionMigrationCounts) error {
	encoded, err := json.Marshal(notionMigrationSummary{Counts: counts})
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_notion_migrations}} SET [[summary_json]]={:summary} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"summary": string(encoded), "id": migrationID}).Execute()
	return err
}

// notionMigrationCountsOf reads the counts kept in a migration's row.
func notionMigrationCountsOf(stored storedNotionMigration) (notionMigrationCounts, error) {
	summary := notionMigrationSummary{}
	err := json.Unmarshal([]byte(stored.SummaryJSON), &summary)
	return summary.Counts, err
}

// notionMigrationScanOf is a migration's scan items.
func notionMigrationScanOf(app core.App, stored storedNotionMigration) notionMigrationScanStore {
	return notionMigrationScanStore{app: app, migrationID: stored.ID, workspaceID: stored.WorkspaceID}
}

// loadNotionMigrationSummary is a migration's whole scan with its counts.
func loadNotionMigrationSummary(app core.App, stored storedNotionMigration) (notionMigrationSummary, error) {
	counts, err := notionMigrationCountsOf(stored)
	if err != nil {
		return notionMigrationSummary{}, err
	}
	return notionMigrationScanOf(app, stored).summary(counts)
}

func emptyNotionMigrationSummary() notionMigrationSummary {
	return notionMigrationSummary{
		Pages: []notionMigrationPage{}, Databases: []notionMigrationDatabase{}, Issues: []notionMigrationIssue{},
		People: []notionMigrationPerson{},
	}
}

func loadNotionMigration(app core.App, ownerID, workspaceID, id string) (storedNotionMigration, error) {
	if _, err := authorizeWorkspace(app, ownerID, workspaceID, permissionWorkspaceWrite); err != nil {
		return storedNotionMigration{}, err
	}
	stored := storedNotionMigration{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[owner]],[[connection_id]],[[roots_json]],[[status]],[[scan_state_json]],
			[[summary_json]],[[error_code]],[[plan_json]],[[plan_revision]],[[run_state]],[[run_plan_revision]],
			[[import_job_id]],[[run_report_json]],[[run_started_at]],[[last_done_run_started_at]],[[agent_handoff]],[[created_at]],[[updated_at]],
			COALESCE((SELECT [[bundle]].[[label]] FROM {{leafen_bundle_import_sources}} AS [[bundle]]
				WHERE [[bundle]].[[connection_id]] = {{leafen_notion_migrations}}.[[connection_id]]), '') AS [[source_label]]
		FROM {{leafen_notion_migrations}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[owner]]={:owner}
	`).Bind(dbx.Params{"id": strings.TrimSpace(id), "workspaceId": workspaceID, "owner": ownerID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, errNotionMigrationNotFound
	}
	if err != nil {
		return stored, err
	}
	// A run whose import job failed or was canceled has stopped (2026-10-13:
	// it stayed running, and the migration could never run again).
	if (stored.RunState == notionMigrationRunRunning || stored.RunState == notionMigrationRunDone) && stored.ImportJobID != "" {
		var status string
		jobErr := app.DB().NewQuery(`SELECT [[status]],[[updated]] FROM {{leafen_jobs}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": stored.ImportJobID}).Row(&status, &stored.JobChanged)
		if jobErr != nil && !errors.Is(jobErr, sql.ErrNoRows) {
			return stored, jobErr
		}
		if stored.RunState == notionMigrationRunRunning &&
			(errors.Is(jobErr, sql.ErrNoRows) || status == durableJobStatusFailed || status == durableJobStatusCanceled) {
			stored.RunState = notionMigrationRunFailed
		}
	}
	return stored, nil
}

func notionMigrationWire(stored storedNotionMigration) (notionMigration, error) {
	migration := notionMigration{
		ID: stored.ID, WorkspaceID: stored.WorkspaceID, ConnectionID: stored.ConnectionID, Status: stored.Status,
		ErrorCode: stored.ErrorCode, RunState: stored.RunState, RunPlanRevision: stored.RunRevision,
		ImportJobID: stored.ImportJobID, AgentHandoff: stored.AgentHandoff, EverDone: stored.LastDoneStarted > 0, Source: stored.SourceLabel, CreatedAt: stored.CreatedAt, UpdatedAt: stored.UpdatedAt,
		Roots: []notionImportRoot{}, FormatsLeft: []notionMigrationFormatsLeftKind{},
	}
	if err := json.Unmarshal([]byte(stored.RootsJSON), &migration.Roots); err != nil {
		return notionMigration{}, err
	}
	migration.RunReport = emptyNotionMigrationRunReport()
	if stored.RunReport != "" {
		if err := json.Unmarshal([]byte(stored.RunReport), &migration.RunReport); err != nil {
			return notionMigration{}, err
		}
	}
	migration.RunEndedAt = notionMigrationRunEnd(stored, &migration.RunReport)
	counts, err := notionMigrationCountsOf(stored)
	if err != nil {
		return notionMigration{}, err
	}
	migration.Counts = counts
	return migration, nil
}

// stepNotionMigration scans one step of a migration that is still
// scanning, with the client its caller makes for the connection's token.
func stepNotionMigration(
	ctx context.Context, app core.App, ownerID, workspaceID, id string,
	clientFor func(connectionID, token string) notionProviderClient,
) (notionMigration, error) {
	stored, err := loadNotionMigration(app, ownerID, workspaceID, id)
	if err != nil {
		return notionMigration{}, err
	}
	release := acquireProductOperationLock(notionMigrationLockNamespace, stored.ID)
	defer release()
	if stored, err = loadNotionMigration(app, ownerID, workspaceID, id); err != nil || stored.Status != notionMigrationStatusScanning {
		if err != nil {
			return notionMigration{}, err
		}
		return notionMigrationWire(stored)
	}
	connection, err := loadNotionConnection(app, stored.ConnectionID, ownerID, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return finishNotionMigration(app, stored, nil, notionMigrationStatusFailed, errorCodeNotionImportConnectionNotFound)
	} else if err != nil {
		return notionMigration{}, err
	}
	token, err := decryptNotionConnection(app, connection)
	if err != nil {
		return notionMigration{}, err
	}
	counts, err := notionMigrationCountsOf(stored)
	if err != nil {
		return notionMigration{}, err
	}
	store := notionMigrationScanOf(app, stored)
	stepContext, cancel := context.WithTimeout(ctx, notionMigrationStepTimeout)
	defer cancel()
	client := clientFor(connection.ID, token)
	done := false
	for !done {
		done, err = scanNotionMigrationStep(stepContext, client, store, &counts)
		if err == nil || !notionProviderRefused(err) {
			break
		}
		// What Notion won't give this connection is reported and passed by.
		item, ordinal, found, frontErr := store.front()
		if frontErr != nil || !found {
			err = frontErr
			break
		}
		code := notionMigrationIssuePageUnavailable
		switch item.Kind {
		case "database", "rows", "views":
			code = notionImportIssueDatabaseUnavailable
		case "blocks":
			// The page was read; what's inside it wasn't given.
			code = notionMigrationIssueBlocksUnavailable
		}
		if err = applyNotionMigrationScanChange(store, ordinal, notionMigrationScanChange{
			issues: []notionMigrationIssue{{Code: code, SourceID: item.ID, ParentID: item.ParentID}},
		}, &counts); err != nil {
			break
		}
	}
	status, code := notionMigrationStatusScanning, ""
	if done {
		status = notionMigrationStatusScanned
	}
	// A step cut short by a failure that may pass (Notion asking to wait,
	// busy or down for a moment, the answer lost on the way) or by its time
	// keeps what it read, and the next one goes on; any other failure ends
	// the scan, a token Notion stopped taking named as such.
	if err != nil && !notionProviderMayPass(err) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		status, code = notionMigrationStatusFailed, errorCodeNotionImportProviderFailed
		if notionProviderTokenRejected(err) {
			code = errorCodeNotionImportTokenRejected
		}
	}
	return finishNotionMigration(app, store.withRootTitles(stored), &counts, status, code)
}

func finishNotionMigration(
	app core.App, stored storedNotionMigration, counts *notionMigrationCounts, status, errorCode string,
) (notionMigration, error) {
	if counts != nil {
		encoded, err := json.Marshal(notionMigrationSummary{Counts: *counts})
		if err != nil {
			return notionMigration{}, err
		}
		stored.SummaryJSON = string(encoded)
	}
	stored.Status, stored.ErrorCode, stored.UpdatedAt = status, errorCode, time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_migrations}} SET [[status]]={:status},
			[[summary_json]]={:summary},[[error_code]]={:errorCode},[[updated_at]]={:updatedAt}
		WHERE [[id]]={:id}
	`).Bind(dbx.Params{
		"status": stored.Status, "summary": stored.SummaryJSON, "errorCode": stored.ErrorCode,
		"updatedAt": stored.UpdatedAt, "id": stored.ID,
	}).Execute(); err != nil {
		return notionMigration{}, err
	}
	return notionMigrationWire(stored)
}

func createNotionMigrationRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := notionMigrationInput{}
	if err := event.BindBody(&input); err != nil {
		return notionMigrationRouteError(event, bodyRefusal(errNotionMigrationInvalid, "migration", err), "create-input")
	}
	migration, err := createNotionMigration(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")), input, false)
	if err != nil {
		return notionMigrationRouteError(event, err, "create")
	}
	return event.JSON(http.StatusCreated, migration)
}

// notionMigrationListItems is how many migrations one window of a person's
// list holds.
const notionMigrationListItems = 20

// listNotionMigrationsRoute lists the person's migrations in the workspace,
// latest first and without their scans, a window at a time from the cursor
// (the last one's change time and id), so a screen finds the one going on,
// the finished ones and a source's earlier one however many there are.
func listNotionMigrationsRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite); err != nil {
		return notionMigrationRouteError(event, err, "list")
	}
	keyset, params := "", dbx.Params{"workspaceId": workspaceID, "owner": user.Id, "limit": notionMigrationListItems + 1}
	if cursor := strings.TrimSpace(event.Request.URL.Query().Get("cursor")); cursor != "" {
		at, id, found := strings.Cut(cursor, ":")
		updated, err := strconv.ParseInt(at, 10, 64)
		if !found || err != nil || id == "" {
			return notionMigrationRouteError(event,
				refuse(errNotionMigrationInvalid, "cursor is the nextCursor of the last list, or none to start"), "list")
		}
		keyset, params["at"], params["id"] = `AND ([[updated_at]] < {:at} OR ([[updated_at]] = {:at} AND [[id]] < {:id}))`, updated, id
	}
	ids := []struct {
		ID        string `db:"id"`
		UpdatedAt int64  `db:"updated_at"`
	}{}
	if err := event.App.DB().NewQuery(`
		SELECT [[id]],[[updated_at]] FROM {{leafen_notion_migrations}} INDEXED BY [[idx_leafen_notion_migrations_owner]]
		WHERE [[workspace_id]]={:workspaceId} AND [[owner]]={:owner} ` + keyset + `
		ORDER BY [[updated_at]] DESC, [[id]] DESC LIMIT {:limit}
	`).Bind(params).All(&ids); err != nil {
		return notionMigrationRouteError(event, err, "list")
	}
	list := notionMigrationList{Items: make([]notionMigration, 0, len(ids))}
	if len(ids) > notionMigrationListItems {
		ids = ids[:notionMigrationListItems]
		last := ids[len(ids)-1]
		list.NextCursor = strconv.FormatInt(last.UpdatedAt, 10) + ":" + last.ID
	}
	for _, row := range ids {
		stored, err := loadNotionMigration(event.App, user.Id, workspaceID, row.ID)
		if err != nil {
			return notionMigrationRouteError(event, err, "list")
		}
		migration, err := notionMigrationWireWithTop(event.App, stored)
		if err != nil {
			return notionMigrationRouteError(event, err, "list")
		}
		list.Items = append(list.Items, migration)
	}
	return event.JSON(http.StatusOK, list)
}

func readNotionMigrationRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	stored, err := loadNotionMigration(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")),
		event.Request.PathValue("migrationId"))
	if err != nil {
		return notionMigrationRouteError(event, err, "read")
	}
	migration, err := notionMigrationWireWithTop(event.App, stored)
	if err != nil {
		return notionMigrationRouteError(event, err, "read")
	}
	// A finished Notion run's end shows what Notion's API didn't give and
	// leads to an agent that can match it (NotionMigrationFormatsLeft.tsx);
	// another tool's bundle has none of Notion's page settings to match.
	if stored.RunState == notionMigrationRunDone && stored.SourceLabel == "" {
		if migration.FormatsLeft, err = notionMigrationFormatsLeftKinds(event.App, stored.ImportJobID); err != nil {
			return notionMigrationRouteError(event, err, "read")
		}
	}
	return event.JSON(http.StatusOK, withNotionMigrationLive(migration))
}

// readNotionMigrationScanItemsRoute gives a window of one kind of the scan's
// findings (kind pages, databases, issues or people) from the cursor on.
func readNotionMigrationScanItemsRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	stored, err := loadNotionMigration(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")),
		event.Request.PathValue("migrationId"))
	if err != nil {
		return notionMigrationRouteError(event, err, "scan-items")
	}
	query := event.Request.URL.Query()
	items, err := readNotionMigrationScanItems(event.App, stored, query.Get("kind"), query.Get("cursor"), apiQueryLimit(query))
	if err != nil {
		return notionMigrationRouteError(event, err, "scan-items")
	}
	return event.JSON(http.StatusOK, items)
}

func (r *durableJobRunner) scanNotionMigrationRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	migration, err := scanNotionMigrationOn(event.Request.Context(), event.App, user.Id,
		strings.TrimSpace(event.Request.PathValue("workspaceId")), event.Request.PathValue("migrationId"), r.notionProviderClient)
	if err != nil {
		return notionMigrationRouteError(event, err, "scan")
	}
	return event.JSON(http.StatusOK, withNotionMigrationLive(migration))
}

func emptyNotionMigrationRunReport() notionMigrationRunReport {
	return notionMigrationRunReport{Reported: []notionMigrationReportedProperty{}}
}

// updateNotionMigrationRunReport changes what a migration's run has
// recorded for its report.
func updateNotionMigrationRunReport(app core.App, migrationID string, change func(*notionMigrationRunReport)) error {
	var stored string
	if err := app.DB().NewQuery(`SELECT [[run_report_json]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": migrationID}).Row(&stored); err != nil {
		return err
	}
	report := emptyNotionMigrationRunReport()
	if err := json.Unmarshal([]byte(stored), &report); err != nil {
		return err
	}
	change(&report)
	encoded, err := json.Marshal(report)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_notion_migrations}} SET [[run_report_json]]={:report} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"report": string(encoded), "id": migrationID}).Execute()
	return err
}

// endNotionMigrationsOfConnection ends the migrations a removed Notion
// connection was moving: they can't read Notion any more, and the agent's
// migration rights go with them.
func endNotionMigrationsOfConnection(app core.App, connectionID string, now int64) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_migrations}}
		SET [[status]]={:failed},[[error_code]]={:code},[[updated_at]]={:now}
		WHERE [[connection_id]]={:connectionId} AND [[status]]!={:failed} AND [[run_state]]!={:done}
	`).Bind(dbx.Params{
		"failed": notionMigrationStatusFailed, "code": errorCodeNotionImportConnectionNotFound, "now": now,
		"connectionId": connectionID, "done": notionMigrationRunDone,
	}).Execute()
	return err
}
