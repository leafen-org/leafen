package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errFileManualCleanupInvalid         = errors.New(errorCodeFileManualCleanupInvalid)
	errFileManualCleanupNotFound        = errors.New(errorCodeFileManualCleanupNotFound)
	errFileManualCleanupRequestIDReused = errors.New(errorCodeFileManualCleanupRequestIDReused)
	errFileManualCleanupUnavailable     = errors.New(errorCodeFileManualCleanupUnavailable)
)

type authorizedProductFileManualCleanupRequest struct {
	ActorID     string
	WorkspaceID string
	RequestID   string
	NowMillis   int64
}

type storedProductFileManualCleanupJob struct {
	ID                  string `db:"id"`
	ActorID             string `db:"actor_id"`
	WorkspaceID         string `db:"workspace_id"`
	RequestID           string `db:"request_id"`
	RequestHash         string `db:"request_hash"`
	Status              string `db:"status"`
	Revision            int    `db:"revision"`
	Scanned             int    `db:"scanned"`
	Expired             int    `db:"expired"`
	DeletedObjects      int    `db:"deleted_objects"`
	FailedObjects       int    `db:"failed_objects"`
	DiagnosticReference string `db:"diagnostic_reference"`
	ScheduledAt         int64  `db:"scheduled_at"`
	Attempt             int    `db:"attempt"`
	NotBefore           int64  `db:"not_before"`
	CompletedAt         int64  `db:"completed_at"`
	CreatedAt           int64  `db:"created_at"`
	UpdatedAt           int64  `db:"updated_at"`
}

const productFileManualCleanupSelect = `
	SELECT [[id]],[[actor_id]],[[workspace_id]],[[request_id]],[[request_hash]],
		[[status]],[[revision]],[[scanned]],[[expired]],[[deleted_objects]],
		[[failed_objects]],[[diagnostic_reference]],[[scheduled_at]],[[attempt]],
		[[not_before]],[[completed_at]],[[created_at]],[[updated_at]]
	FROM {{leafen_file_manual_cleanup_jobs}}
`

func normalizeProductFileManualCleanupRequest(
	request *authorizedProductFileManualCleanupRequest,
) error {
	request.ActorID = strings.TrimSpace(request.ActorID)
	request.WorkspaceID = strings.TrimSpace(request.WorkspaceID)
	request.RequestID = strings.TrimSpace(request.RequestID)
	if request.NowMillis <= 0 {
		request.NowMillis = time.Now().UTC().UnixMilli()
	}
	if request.ActorID == "" || request.WorkspaceID == "" || request.RequestID == "" ||
		utf8.RuneCountInString(request.RequestID) > maxWorkspacePermissionIdentifierLength {
		return errFileManualCleanupInvalid
	}
	return nil
}

func productFileManualCleanupRequestHash(request authorizedProductFileManualCleanupRequest) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"file-manual-cleanup-v1", request.WorkspaceID,
	}, "\x1f")))
	return hex.EncodeToString(digest[:])
}

func loadProductFileManualCleanupByRequest(
	ctx context.Context,
	app core.App,
	actorID string,
	requestID string,
) (storedProductFileManualCleanupJob, error) {
	job := storedProductFileManualCleanupJob{}
	err := app.DB().NewQuery(productFileManualCleanupSelect + `
		WHERE [[actor_id]]={:actorId} AND [[request_id]]={:requestId} LIMIT 1`,
	).WithContext(ctx).Bind(dbx.Params{
		"actorId": actorID, "requestId": requestID,
	}).One(&job)
	return job, err
}

func loadProductFileManualCleanupJob(
	ctx context.Context,
	app core.App,
	jobID string,
) (storedProductFileManualCleanupJob, error) {
	job := storedProductFileManualCleanupJob{}
	err := app.DB().NewQuery(productFileManualCleanupSelect + `
		WHERE [[id]]={:jobId} LIMIT 1`,
	).WithContext(ctx).Bind(dbx.Params{"jobId": strings.TrimSpace(jobID)}).One(&job)
	return job, err
}

func validProductFileManualCleanupJob(job storedProductFileManualCleanupJob) bool {
	terminal := job.Status == fileManualCleanupStatusCompleted ||
		job.Status == fileManualCleanupStatusPartialFailure ||
		job.Status == fileManualCleanupStatusFailed
	return job.ID != "" && job.ActorID != "" && job.WorkspaceID != "" &&
		job.RequestID != "" && len(job.RequestHash) == 64 &&
		(job.Status == fileManualCleanupStatusRunning || terminal) &&
		job.Revision >= 0 && job.Scanned >= 0 && job.Expired >= 0 &&
		job.DeletedObjects >= 0 && job.FailedObjects >= 0 &&
		job.Expired <= job.Scanned && job.FailedObjects <= job.Scanned &&
		job.DeletedObjects+job.FailedObjects <= job.Scanned &&
		job.ScheduledAt > 0 && job.Attempt >= 0 && job.NotBefore > 0 &&
		job.CreatedAt > 0 && job.UpdatedAt > 0 &&
		terminal == (job.CompletedAt > 0)
}

func admitProductFileManualCleanup(
	ctx context.Context,
	app core.App,
	request authorizedProductFileManualCleanupRequest,
) (storedProductFileManualCleanupJob, bool, error) {
	job := storedProductFileManualCleanupJob{}
	if err := normalizeProductFileManualCleanupRequest(&request); err != nil {
		return job, false, err
	}
	hash := productFileManualCleanupRequestHash(request)
	existing, err := loadProductFileManualCleanupByRequest(
		ctx, app, request.ActorID, request.RequestID,
	)
	if err == nil {
		if !validProductFileManualCleanupJob(existing) || existing.RequestHash != hash ||
			existing.WorkspaceID != request.WorkspaceID {
			return job, false, errFileManualCleanupRequestIDReused
		}
		return existing, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return job, false, err
	}
	job = storedProductFileManualCleanupJob{
		ID: core.GenerateDefaultRandomId(), ActorID: request.ActorID,
		WorkspaceID: request.WorkspaceID, RequestID: request.RequestID,
		RequestHash: hash, Status: fileManualCleanupStatusRunning,
		ScheduledAt: request.NowMillis, NotBefore: request.NowMillis,
		CreatedAt: request.NowMillis, UpdatedAt: request.NowMillis,
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		_, insertErr := txApp.DB().Insert(productFileManualCleanupJobsTable, dbx.Params{
			"id": job.ID, "actor_id": job.ActorID, "workspace_id": job.WorkspaceID,
			"request_id": job.RequestID, "request_hash": job.RequestHash,
			"status": job.Status, "revision": 0, "scanned": 0, "expired": 0,
			"deleted_objects": 0, "failed_objects": 0, "diagnostic_reference": "",
			"scheduled_at": job.ScheduledAt, "attempt": 0, "not_before": job.NotBefore,
			"completed_at": 0, "created_at": job.CreatedAt, "updated_at": job.UpdatedAt,
		}).WithContext(ctx).Execute()
		if insertErr != nil {
			return insertErr
		}
		return pruneProductFileManualCleanupJobs(ctx, txApp, job.WorkspaceID)
	})
	if err == nil {
		return job, false, nil
	}
	existing, loadErr := loadProductFileManualCleanupByRequest(
		ctx, app, request.ActorID, request.RequestID,
	)
	if loadErr == nil {
		if validProductFileManualCleanupJob(existing) && existing.RequestHash == hash &&
			existing.WorkspaceID == request.WorkspaceID {
			return existing, true, nil
		}
		return storedProductFileManualCleanupJob{}, false, errFileManualCleanupRequestIDReused
	}
	var active int
	countErr := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_file_manual_cleanup_jobs}}
		WHERE [[workspace_id]]={:workspaceId} AND [[status]]='running'`,
	).WithContext(ctx).Bind(dbx.Params{"workspaceId": request.WorkspaceID}).Row(&active)
	if countErr == nil && active > 0 {
		return storedProductFileManualCleanupJob{}, false, errFileManualCleanupUnavailable
	}
	return storedProductFileManualCleanupJob{}, false, err
}

func pruneProductFileManualCleanupJobs(
	ctx context.Context,
	app core.App,
	workspaceID string,
) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_file_manual_cleanup_jobs}} AS [[candidate]]
		WHERE [[candidate]].[[workspace_id]]={:workspaceId}
			AND [[candidate]].[[status]]!='running'
			AND [[candidate]].[[id]] NOT IN (
				SELECT [[retained]].[[id]] FROM {{leafen_file_manual_cleanup_jobs}} AS [[retained]]
				WHERE [[retained]].[[workspace_id]]={:workspaceId}
					AND [[retained]].[[status]]!='running'
				ORDER BY [[retained]].[[completed_at]] DESC,[[retained]].[[id]] DESC
				LIMIT {:retained}
			)
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "retained": fileManualCleanupRetentionPerWorkspace,
	}).Execute()
	return err
}

func productFileManualCleanupResponse(
	job storedProductFileManualCleanupJob,
	duplicate bool,
) (fileManualCleanupResponse, error) {
	if !validProductFileManualCleanupJob(job) {
		return fileManualCleanupResponse{}, errFileManualCleanupUnavailable
	}
	return fileManualCleanupResponse{Job: fileManualCleanupJob{
		ID: job.ID, RequestID: job.RequestID, WorkspaceID: job.WorkspaceID,
		Status: job.Status, Revision: job.Revision, Scanned: job.Scanned,
		Expired: job.Expired, DeletedObjects: job.DeletedObjects,
		FailedObjects: job.FailedObjects, DiagnosticReference: job.DiagnosticReference,
		StartedAt: job.ScheduledAt, CompletedAt: job.CompletedAt,
	}, Duplicate: duplicate}, nil
}

func finalizeProductFileManualCleanup(
	ctx context.Context,
	app core.App,
	jobID string,
	now int64,
) error {
	job, err := loadProductFileManualCleanupJob(ctx, app, jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return errFileManualCleanupNotFound
	}
	if err != nil {
		return err
	}
	if job.Status != fileManualCleanupStatusRunning {
		return nil
	}
	var pendingOutcomes int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_file_manual_cleanup_outcomes}}
		WHERE [[job_id]]={:jobId}`,
	).WithContext(ctx).Bind(dbx.Params{"jobId": job.ID}).Row(&pendingOutcomes); err != nil {
		return err
	}
	if pendingOutcomes != 0 {
		return errFileManualCleanupUnavailable
	}
	status := fileManualCleanupStatusCompleted
	if job.FailedObjects > 0 {
		status = fileManualCleanupStatusPartialFailure
		if job.FailedObjects >= job.Scanned {
			status = fileManualCleanupStatusFailed
		}
	}
	updated, err := app.DB().NewQuery(`
		UPDATE {{leafen_file_manual_cleanup_jobs}} SET [[status]]={:status},
			[[revision]]=[[revision]]+1,[[completed_at]]={:now},[[not_before]]={:now},
			[[updated_at]]={:now}
		WHERE [[id]]={:jobId} AND [[status]]='running' AND [[revision]]={:revision}
	`).WithContext(ctx).Bind(dbx.Params{
		"status": status, "now": now, "jobId": job.ID, "revision": job.Revision,
	}).Execute()
	if err != nil {
		return err
	}
	count, err := updated.RowsAffected()
	if err != nil || count != 1 {
		return errors.Join(errFileManualCleanupUnavailable, err)
	}
	return pruneProductFileManualCleanupJobs(ctx, app, job.WorkspaceID)
}
