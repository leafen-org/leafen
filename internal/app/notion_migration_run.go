package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// Carrying out a migration's plan (decision "설계 우선 Notion 이사" 3절 원칙
// 1·3·4, 4절): a plan that passes its check runs on the Notion import engine
// as one import job, which keeps the revision it was started with. The
// person runs the revision they saw; the job's end ends the migration, and
// with it the agent's migration rights (mcp_migration_scope.go).

var (
	errNotionMigrationPlanBlocked = errors.New(errorCodeNotionMigrationPlanBlocked)
	errNotionMigrationRunning     = errors.New(errorCodeNotionMigrationRunning)
)

// errNotionMigrationRequestIDUsed is a run's requestId another request
// already used: another run, or this migration at another plan revision.
var errNotionMigrationRequestIDUsed = refuse(errNotionMigrationInvalid,
	"requestId was used for another request: give a new one for this run")

// runNotionMigration starts the run of a migration's plan at the revision
// the person saw. The same request again answers with the run it started.
func runNotionMigration(
	ctx context.Context, app core.App, ownerID, workspaceID, id string, input notionMigrationRunInput,
) (notionMigration, error) {
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" {
		return notionMigration{}, refuse(errNotionMigrationInvalid, "requestId is empty: give one for this run, the same again to ask after it")
	}
	if len([]rune(requestID)) > maxDurableJobRequestIDRunes {
		return notionMigration{}, refuse(errNotionMigrationInvalid, "requestId is at most %d characters", maxDurableJobRequestIDRunes)
	}
	stored, err := loadNotionMigration(app, ownerID, workspaceID, id)
	if err != nil {
		return notionMigration{}, err
	}
	release := acquireProductOperationLock(notionMigrationLockNamespace, stored.ID)
	defer release()
	if stored, err = loadNotionMigration(app, ownerID, workspaceID, id); err != nil {
		return notionMigration{}, err
	}
	requestHash := notionMigrationRunRequestHash(workspaceID, stored.ID, input.PlanRevision, requestID)
	if existing, err := loadStoredJobByRequest(app, ownerID, requestID); err == nil {
		if existing.ID != stored.ImportJobID || existing.RequestHash != requestHash {
			return notionMigration{}, errNotionMigrationRequestIDUsed
		}
		return notionMigrationWireWithTop(app, stored)
	}
	switch {
	case stored.Status != notionMigrationStatusScanned:
		return notionMigration{}, errNotionMigrationNotScanned
	case stored.RunState == notionMigrationRunRunning:
		return notionMigration{}, errNotionMigrationRunning
	case stored.PlanJSON == "" || input.PlanRevision != stored.PlanRevision:
		return notionMigration{}, errNotionMigrationPlanConflict
	}
	state, err := notionMigrationPlanStateOf(ctx, app, stored)
	if err != nil {
		return notionMigration{}, err
	}
	if !state.Check.OK {
		return notionMigration{}, errNotionMigrationPlanBlocked
	}
	roots := []notionImportRoot{}
	if err := json.Unmarshal([]byte(stored.RootsJSON), &roots); err != nil {
		return notionMigration{}, err
	}
	// A first run brings nothing another move already brought here
	// (notion_migration_moved_before.go).
	if err := notionMigrationFirstRunMovedBefore(app, stored, roots, input.SecondCopy); err != nil {
		return notionMigration{}, err
	}
	rootIDs, rootKinds := make([]string, 0, len(roots)), make([]string, 0, len(roots))
	for _, root := range roots {
		rootIDs, rootKinds = append(rootIDs, root.ID), append(rootKinds, root.Kind)
	}
	// A failed run's job is let go; the new run takes its place.
	stoppedJob := ""
	var stopped *notionImportJobPayload
	if stored.RunState == notionMigrationRunFailed {
		stoppedJob = stored.ImportJobID
		stopped = loadNotionMigrationStoppedPayload(app, stoppedJob)
	}
	// A run after the first starts from what the earlier ones made, a
	// stopped first run's too (notion_migration_run_again.go).
	again := notionMigrationRunAgainOf(stored, stopped)
	rerunSince := again.MigrationRerunSince
	// A run after the first goes through what reaches the source now.
	connectionID := stored.ConnectionID
	if stored.RunState != "" {
		connectionID = notionMigrationReachingConnection(ctx, app, stored)
	}
	jobID := security.RandomString(15)
	// What a stopped run made joins the ledger first, so the new run goes on
	// from it instead of making it again, and the new run's job starts from
	// the ledger: both in the job's first turn, not in this request
	// (notion_migration_run_ledger_start.go).
	again.LedgerFrom = notionMigrationLedgerFrom(stopped, stoppedJob)
	again.LedgerPreload = rerunSince > 0 || stoppedJob != ""
	err = app.RunInTransaction(func(tx core.App) error {
		authorization, err := authorizeWorkspace(tx, ownerID, workspaceID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(notionImportJobPayload{
			ConnectionID: connectionID, RootIDs: rootIDs, RootKinds: rootKinds,
			AuthorityOwnerID: authorization.OwnerID, Phase: notionImportPhaseScope,
			MigrationID: stored.ID, MigrationPlanRevision: stored.PlanRevision, notionMigrationRunAgain: again,
			notionMigrationRunOrder:      notionMigrationRunOrder{CommentsLast: true},
			notionImportUnmentionedSweep: notionImportUnmentionedSweepFromStart(),
		})
		if err != nil {
			return err
		}
		inserted, err := insertNotionImportJob(tx, jobID, ownerID, workspaceID, requestID, requestHash, payload)
		if err != nil {
			return err
		}
		if !inserted {
			return errNotionMigrationRequestIDUsed
		}
		// The run counts what it makes, changes and leaves as it was anew.
		if err := updateNotionMigrationRunReport(tx, stored.ID, func(report *notionMigrationRunReport) {
			report.Tally = nil
		}); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		_, err = tx.DB().NewQuery(`
			UPDATE {{leafen_notion_migrations}}
			SET [[run_state]]={:running},[[run_plan_revision]]={:revision},[[import_job_id]]={:jobId},
				[[run_started_at]]={:now},[[updated_at]]={:now},[[connection_id]]={:connectionId}
			WHERE [[id]]={:id} AND ([[run_state]] IN ('', {:done})
				OR ([[run_state]]={:running} AND {:stoppedJob} != '' AND [[import_job_id]]={:stoppedJob}))
		`).Bind(dbx.Params{
			"running": notionMigrationRunRunning, "revision": stored.PlanRevision, "jobId": jobID,
			"now": now, "id": stored.ID, "done": notionMigrationRunDone, "stoppedJob": stoppedJob,
			"connectionId": connectionID,
		}).Execute()
		return err
	})
	if err != nil {
		return notionMigration{}, err
	}
	// The run keeps the time it was said to take, to say it again only
	// when that changes (notion_migration_estimate_run.go).
	if stored, err = loadNotionMigration(app, ownerID, workspaceID, id); err != nil {
		return notionMigration{}, err
	}
	if err := startNotionMigrationEstimate(app, stored, stored.RunStarted); err != nil {
		return notionMigration{}, err
	}
	if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
		runner.notify()
	}
	if stored, err = loadNotionMigration(app, ownerID, workspaceID, id); err != nil {
		return notionMigration{}, err
	}
	// It answers with the migration as the list has it, its top page too: a
	// row running again took this answer and lost "열기" until the list was
	// read again (2026-11-13).
	return notionMigrationWireWithTop(app, stored)
}

func notionMigrationRunRequestHash(workspaceID, migrationID string, revision int, requestID string) string {
	digest := sha256.Sum256([]byte(workspaceID + "\x00notion-migration\x00" + migrationID + "\x00" +
		strconv.Itoa(revision) + "\x00" + requestID))
	return hex.EncodeToString(digest[:])
}

// finishNotionMigrationRun ends the run of the migration a finished import
// job carried out, and says whether its report page, written before the last
// step, is behind: a stop it showed is over, or the last step passed by what
// a person took away meanwhile (notion_migration_failure.go).
func finishNotionMigrationRun(app core.App, payload notionImportJobPayload, jobID string) (bool, error) {
	if payload.MigrationID == "" {
		return false, nil
	}
	var skipped int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_notion_import_file_tasks}} WHERE [[job_id]]={:jobId} AND [[error_code]]={:code}
	`).Bind(dbx.Params{"jobId": jobID, "code": errorCodeNotionMigrationFileSkipped}).Row(&skipped); err != nil {
		return false, err
	}
	// The report keeps when the run ended: a plan changed since moves the
	// migration's updated time, not this (notionMigrationRunEndedAt); and its
	// estimate's stage is done, which no look at the run sees after the end.
	now, behind := time.Now().UTC().UnixMilli(), false
	if err := updateNotionMigrationRunReport(app, payload.MigrationID, func(report *notionMigrationRunReport) {
		behind = report.Failure != nil || report.ChangedMeanwhile > 0
		report.FilesSkipped, report.EndedAt, report.Failure = skipped, now, nil
		if report.Estimate != nil {
			notionMigrationStageTook(report.Estimate, report.Estimate.Stage, now-report.Estimate.StageAt)
			report.Estimate.Stage, report.Estimate.StageAt = notionMigrationStageDone, now
		}
	}); err != nil {
		return false, err
	}
	// The ledger step recorded what the run made, in windows outside the
	// turns (notion_migration_removed.go), and no step after it makes more;
	// only a run that passed it by records it here, in this last transaction.
	if !payload.LedgerRecorded {
		if err := recordNotionMigrationLedger(app, payload.MigrationID, jobID); err != nil {
			return false, err
		}
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_migrations}} SET [[run_state]]={:done},[[last_done_run_started_at]]=[[run_started_at]],[[updated_at]]={:now}
		WHERE [[id]]={:id} AND [[import_job_id]]={:jobId} AND [[run_state]]={:running}
	`).Bind(dbx.Params{
		"done": notionMigrationRunDone, "running": notionMigrationRunRunning, "now": now,
		"id": payload.MigrationID, "jobId": jobID,
	}).Execute()
	if err != nil {
		return behind, err
	}
	// The person hears it is done (notion_migration_notices.go), and the
	// import screen says where the people it kept are matched later.
	if job, err := loadStoredJob(app, jobID, ""); err == nil {
		if err := recordNotionMigrationPeopleKept(app, job, payload.MigrationID); err != nil {
			logDurableJobDiagnostic(app, errorCodeJobRunnerFailed, "", jobID, "notion-migration.people", "kept", err)
		}
		if err := notifyNotionMigrationDone(app, job, payload.MigrationID); err != nil {
			logDurableJobDiagnostic(app, errorCodeJobRunnerFailed, "", jobID, "notion-migration.notice", "done", err)
		}
	}
	return behind, nil
}

func runNotionMigrationRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := notionMigrationRunInput{}
	if err := event.BindBody(&input); err != nil {
		return notionMigrationRouteError(event, bodyRefusal(errNotionMigrationInvalid, "migration run", err), "run-input")
	}
	migration, err := runNotionMigration(event.Request.Context(), event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")),
		event.Request.PathValue("migrationId"), input)
	if err != nil {
		return notionMigrationRouteError(event, err, "run")
	}
	return event.JSON(http.StatusAccepted, migration)
}
