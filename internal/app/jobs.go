package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	durableJobReadyDelay = 250 * time.Millisecond
)

type storedJob struct {
	ID                  string `db:"id"`
	Owner               string `db:"owner"`
	WorkspaceID         string `db:"workspace_id"`
	RequestID           string `db:"request_id"`
	RequestHash         string `db:"request_hash"`
	Kind                string `db:"kind"`
	PageID              string `db:"page_id"`
	Status              string `db:"status"`
	ErrorCode           string `db:"error_code"`
	DiagnosticReference string `db:"diagnostic_reference"`
	PayloadJSON         string `db:"payload_json"`
	Cursor              string `db:"cursor"`
	Processed           int    `db:"processed_items"`
	Total               int    `db:"total_items"`
	Attempt             int    `db:"attempt"`
	MaxAttempts         int    `db:"max_attempts"`
	ChunkCount          int    `db:"chunk_count"`
	CapturedBytes       int64  `db:"captured_bytes"`
	Created             int64  `db:"created"`
	Updated             int64  `db:"updated"`
}

type storedJobEffect struct {
	PageID   string `db:"page_id"`
	Revision int    `db:"revision"`
	SHA256   string `db:"sha256"`
}

type readyJob struct {
	ID string `db:"id"`
}

type durableJobRunner struct {
	app                                core.App
	clock                              durableJobClock
	ygo                                *pocketBaseYgoPersistence
	rooms                              workspaceRoomInvalidator
	wake                               chan struct{}
	notionProviderAdmission            *notionProviderAdmissionOwner
	databaseAutomationDeliveryProvider databaseAutomationDeliveryProvider

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}

	instanceProductExportPolicyMu           sync.Mutex
	instanceProductExportPolicySchemaCookie int
	instanceProductExportPolicies           []instanceProductExportTablePolicy

	// taskPairsRequested has the runner look again for data sources whose
	// sub-items or dependencies wait for their pair of properties.
	taskPairsRequested atomic.Bool
	// childPageBlocksRequested has the runner fill the bodies waiting for
	// their sub-pages' blocks.
	childPageBlocksRequested atomic.Bool
	// databaseBlocksRequested has the runner look for databases an archive
	// import brought with no block (database_block_backfill.go).
	databaseBlocksRequested atomic.Bool
}

func newDurableJobRunner(app core.App) *durableJobRunner {
	return newDurableJobRunnerWithClock(app, realDurableJobClock{})
}

func newDurableJobRunnerWithClock(app core.App, clock durableJobClock) *durableJobRunner {
	return &durableJobRunner{
		app: app, clock: clock, wake: make(chan struct{}, 1),
		notionProviderAdmission:            newNotionProviderAdmissionOwner(clock),
		databaseAutomationDeliveryProvider: newNativeDatabaseAutomationDeliveryProvider(),
	}
}

func (r *durableJobRunner) bindYgoPersistence(ygo *pocketBaseYgoPersistence) {
	r.ygo = ygo
}

func (r *durableJobRunner) bindRoomInvalidator(rooms workspaceRoomInvalidator) {
	r.rooms = rooms
}

func (r *durableJobRunner) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	go r.run(ctx, r.done)
}

func (r *durableJobRunner) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.cancel = nil
	r.done = nil
	r.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", errorCodeJobShutdown, ctx.Err())
	}
}

func (r *durableJobRunner) notify() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *durableJobRunner) fail(id string, reference string, errorCode string) error {
	_, err := r.app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]] = 'failed', [[error_code]] = {:errorCode},
			[[diagnostic_reference]] = {:diagnosticReference},
			[[attempt]] = [[attempt]] + 1, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'
	`).Bind(dbx.Params{
		"id": id, "errorCode": errorCode,
		"diagnosticReference": reference, "updated": r.clock.now().UnixMilli(),
	}).Execute()
	if err == nil {
		notifyNotionImportRetentionAfterCommit(r.app)
	}
	return err
}

func (r *durableJobRunner) apply(ctx context.Context, id string) error {
	job, err := loadStoredJob(r.app, id, "")
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != durableJobStatusQueued) {
		return nil
	}
	if err != nil {
		return err
	}
	if handled, err := r.applyAdministratorScheduledJob(ctx, job); handled {
		return err
	}
	if job.Kind == notionImportJobKind {
		return r.applyNotionImportChunk(ctx, job)
	}
	if job.Kind == workspaceImportJobKind {
		return r.applyWorkspaceImportChunk(ctx, job)
	}
	if job.Kind == workspaceExportJobKind {
		return r.applyWorkspaceExportChunk(ctx, job)
	}
	if handler, ok := organizationJobHandlers[job.Kind]; ok {
		return handler(r, ctx, job)
	}
	if job.Kind == instanceProductExportJobKind {
		return r.applyInstanceProductExportChunk(ctx, job)
	}
	if job.Kind == databaseAutomationEventJobKind {
		return r.applyDatabaseAutomationEventChunk(ctx, job)
	}
	if job.Kind == interactiveAutomationOtherPageJobKind {
		return r.applyInteractiveAutomationOtherPageJob(ctx, job)
	}
	if job.Kind == interactiveAutomationTemplateRowJobKind {
		return r.applyInteractiveAutomationTemplateRowJob(ctx, job)
	}
	if job.Kind == databaseTemplateRepeatJobKind {
		return r.applyDatabaseTemplateRepeatJob(ctx, job)
	}
	if job.Kind == rowPageDatabaseCopyJobKind {
		return r.applyRowPageDatabaseCopyJob(ctx, job)
	}
	if job.Kind == pageDuplicationJobKind {
		return advancePageDuplication(ctx, r.app, job.ID)
	}
	if job.Kind == blockMoveJobKind {
		return advanceBlockMove(ctx, r.app, job.ID)
	}
	if job.Kind == pageWorkspaceMoveJobKind {
		return advancePageWorkspaceMove(ctx, r.app, job.ID)
	}
	if job.Kind == wikiMutationJobKind {
		if r.ygo != nil {
			if handled, err := r.ygo.advanceWikiDatabaseMutation(ctx, job.ID); handled || err != nil {
				return err
			}
		}
		return advanceWikiMutation(ctx, r.app, job.ID)
	}
	if job.Kind == workspaceMembershipRevocationJobKind {
		_, err := r.advanceWorkspaceMembershipRevocation(ctx, job.ID)
		return err
	}
	if job.Kind == workspaceOwnerTransferJobKind {
		_, err := r.advanceWorkspaceOwnerTransfer(ctx, job.ID)
		return err
	}
	if job.Kind == administratorAccountCleanupJobKind {
		return r.advanceAdministratorAccountCleanup(ctx, job.ID)
	}
	if job.Kind != pageCheckpointJobKind {
		return fmt.Errorf("%s: unsupported kind %q", errorCodeJobRunnerFailed, job.Kind)
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, id, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		return applyPageCheckpointChunk(txApp, current)
	})
}

func (r *durableJobRunner) create(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	var input jobInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the job details.")
	}
	input.Kind = strings.TrimSpace(input.Kind)
	input.PageID = strings.TrimSpace(input.PageID)
	input.RequestID = strings.TrimSpace(input.RequestID)
	if input.Kind != pageCheckpointJobKind || input.PageID == "" || input.RequestID == "" || len([]rune(input.RequestID)) > maxDurableJobRequestIDRunes {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the job details.")
	}

	requestHash := jobRequestHash(input)
	jobID := ""
	duplicate := false
	pageMissing := false
	identityReused := false
	err := event.App.RunInTransaction(func(txApp core.App) error {
		existing, err := loadStoredJobByRequest(txApp, user.Id, input.RequestID)
		if err == nil {
			if existing.RequestHash != requestHash {
				identityReused = true
				return nil
			}
			jobID = existing.ID
			duplicate = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		page, err := txApp.FindRecordById(pagesCollection, input.PageID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && page.GetString("owner") != user.Id) {
			pageMissing = true
			return nil
		}
		if err != nil {
			return err
		}
		var totalItems int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_blocks}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
		`).Bind(dbx.Params{"owner": user.Id, "pageId": page.Id}).Row(&totalItems); err != nil {
			return err
		}
		payload, err := json.Marshal(pageCheckpointPayload{PageID: page.Id})
		if err != nil {
			return err
		}

		now := r.clock.now()
		jobID = security.RandomString(15)
		result, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
				 [[status]], [[error_code]], [[not_before]], [[created]], [[updated]],
				 [[payload_json]], [[cursor]], [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]], [[chunk_count]])
			VALUES
				({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind}, {:pageId},
				 'queued', '', {:notBefore}, {:created}, {:updated},
				 {:payload}, '', 0, {:totalItems}, 0, {:maxAttempts}, 0)
			ON CONFLICT ([[owner]], [[request_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": jobID, "owner": user.Id, "workspaceId": page.GetString("workspace"), "requestId": input.RequestID,
			"requestHash": requestHash, "kind": input.Kind, "pageId": page.Id,
			"payload": string(payload), "totalItems": totalItems,
			"maxAttempts": defaultDurableJobMaxAttempts,
			"notBefore":   now.Add(durableJobReadyDelay).UnixMilli(),
			"created":     now.UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		if err != nil {
			return err
		}
		inserted, err := result.RowsAffected()
		if err != nil || inserted == 1 {
			return err
		}
		existing, err = loadStoredJobByRequest(txApp, user.Id, input.RequestID)
		if err != nil {
			return err
		}
		if existing.RequestHash != requestHash {
			identityReused = true
			return nil
		}
		jobID = existing.ID
		duplicate = true
		return nil
	})
	if pageMissing {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	if identityReused {
		return apiError(event, http.StatusConflict, errorCodeJobIdentityReused, "The job request ID was used for a different request.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobCreateFailed, "Couldn't create the job.", err)
	}
	r.notify()
	response, err := r.response(jobID, user.Id, duplicate)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobReadFailed, "Couldn't read the job.", err)
	}
	return event.JSON(http.StatusAccepted, response)
}

func (r *durableJobRunner) get(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	id := strings.TrimSpace(event.Request.PathValue("id"))
	response, err := r.response(id, user.Id, false)
	if err == nil {
		return event.JSON(http.StatusOK, response)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return r.followOtherJob(event, user, id) // a job another table keeps (jobs_follow.go)
	}
	return apiErrorFrom(event, http.StatusInternalServerError, errorCodeJobReadFailed, "Couldn't read the job.", err)
}

func (r *durableJobRunner) response(id string, owner string, duplicate bool) (jobResponse, error) {
	job, err := loadStoredJob(r.app, id, owner)
	if err != nil {
		return jobResponse{}, err
	}
	response := jobResponse{
		ID: job.ID, Kind: job.Kind, PageID: job.PageID, RequestID: job.RequestID,
		Status: job.Status, ErrorCode: job.ErrorCode,
		DiagnosticReference: job.DiagnosticReference, Duplicate: duplicate,
		ChunkCount: job.ChunkCount,
		CanRetry:   job.Status == durableJobStatusFailed, // a person's retry restores its attempts (jobs_control.go)
		CanCancel:  job.Status == durableJobStatusQueued,
		Progress: jobProgress{
			Cursor: job.Cursor, ProcessedItems: job.Processed, TotalItems: job.Total,
			Attempt: job.Attempt, MaxAttempts: job.MaxAttempts,
		},
	}
	var effect storedJobEffect
	err = r.app.DB().NewQuery(`
		SELECT [[page_id]], [[revision]], [[sha256]]
		FROM {{leafen_job_effects}}
		WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": job.ID}).One(&effect)
	if errors.Is(err, sql.ErrNoRows) {
		return response, nil
	}
	if err != nil {
		return jobResponse{}, err
	}
	response.EffectCount = 1
	response.Result = &jobResult{PageID: effect.PageID, Revision: effect.Revision, SHA256: effect.SHA256}
	return response, nil
}

func loadStoredJob(app core.App, id string, owner string) (storedJob, error) {
	params := dbx.Params{"id": id}
	ownerClause := ""
	if owner != "" {
		ownerClause = " AND [[owner]] = {:owner}"
		params["owner"] = owner
	}
	var job storedJob
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
			[[status]], [[error_code]], [[diagnostic_reference]], [[payload_json]], [[cursor]], [[processed_items]],
			[[total_items]], [[attempt]], [[max_attempts]], [[chunk_count]], [[captured_bytes]], [[created]], [[updated]]
		FROM {{leafen_jobs}}
		WHERE [[id]] = {:id}` + ownerClause + `
	`).Bind(params).One(&job)
	return job, err
}

func loadStoredJobByRequest(app core.App, owner string, requestID string) (storedJob, error) {
	var job storedJob
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
			[[status]], [[error_code]], [[diagnostic_reference]], [[payload_json]], [[cursor]], [[processed_items]],
			[[total_items]], [[attempt]], [[max_attempts]], [[chunk_count]], [[captured_bytes]], [[created]], [[updated]]
		FROM {{leafen_jobs}}
		WHERE [[owner]] = {:owner} AND [[request_id]] = {:requestId}
	`).Bind(dbx.Params{"owner": owner, "requestId": requestID}).One(&job)
	return job, err
}

func jobRequestHash(input jobInput) string {
	digest := sha256.Sum256([]byte(input.Kind + "\x00" + input.PageID + "\x00" + input.RequestID))
	return hex.EncodeToString(digest[:])
}
