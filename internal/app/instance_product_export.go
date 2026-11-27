package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	instanceProductExportJobKind   = "instance-product-export"
	instanceProductExportRetention = 7 * 24 * time.Hour
)

type instanceProductExportJobPayload struct {
	FormatVersion int                                 `json:"formatVersion"`
	Phase         string                              `json:"phase"`
	TableIndex    int                                 `json:"tableIndex"`
	RowID         int64                               `json:"rowId"`
	Capture       *instanceProductExportCaptureCursor `json:"capture,omitempty"`
	TableCounts   map[string]int                      `json:"tableCounts"`
	GeneratedAt   string                              `json:"generatedAt"`
}

type storedInstanceProductExportArtifact struct {
	JobID         string `db:"job_id"`
	Owner         string `db:"owner"`
	Format        string `db:"format"`
	FormatVersion int    `db:"format_version"`
	Filename      string `db:"filename"`
	ContentType   string `db:"content_type"`
	Bytes         int64  `db:"bytes"`
	SHA256        string `db:"sha256"`
	Created       int64  `db:"created"`
}

func registerInstanceProductExportRoutes(event *core.ServeEvent, jobs *durableJobRunner) {
	event.Router.POST(apiRoot+"/admin/product-exports", jobs.createInstanceProductExport)
	event.Router.GET(apiRoot+"/admin/product-exports/current", jobs.getCurrentInstanceProductExport)
	event.Router.GET(apiRoot+"/admin/product-exports/{jobId}", jobs.getInstanceProductExport)
	event.Router.GET(apiRoot+"/admin/product-exports/{jobId}/download", jobs.downloadInstanceProductExport)
}

func (r *durableJobRunner) createInstanceProductExport(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	input := instanceProductExportInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the product data export details.")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Format = strings.TrimSpace(input.Format)
	if input.RequestID == "" || len([]rune(input.RequestID)) > maxInstanceProductExportRequestIDRunes || input.Format != instanceProductExportInputFormat {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the product data export details.")
	}
	if err := cleanupExpiredInstanceProductExports(event.App, time.Now().UTC()); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeInstanceProductExportCreateFailed, "Couldn't clean up expired product data exports.", "instance-product-export.create", "retention", errorCodeInstanceProductExportCreateFailed, err)
	}

	// The read log's gathered records go in too (read_log_buffer.go).
	flushReadLog(event.App)
	requestHash := instanceProductExportRequestHash(input)
	jobID := "instance-product-export-" + security.RandomString(15)
	replayed := false
	identityReused := false
	change := func(txApp core.App) error {
		replayed, identityReused = false, false
		administrator, err := isAdministrator(txApp, user.Id)
		if err != nil {
			return err
		}
		if !administrator {
			return errors.New(errorCodeAdministratorNotFound)
		}
		existing, loadErr := loadStoredJobByRequest(txApp, user.Id, input.RequestID)
		if loadErr == nil {
			if existing.Kind != instanceProductExportJobKind || existing.RequestHash != requestHash {
				identityReused = true
				return nil
			}
			jobID, replayed = existing.ID, true
			return nil
		}
		if !errors.Is(loadErr, sql.ErrNoRows) {
			return loadErr
		}
		now := time.Now().UTC()
		payload, err := json.Marshal(instanceProductExportJobPayload{
			FormatVersion: instanceProductExportFormatVersion,
			Phase:         instanceProductExportPhaseCapture, TableCounts: map[string]int{},
			GeneratedAt: now.Format(time.RFC3339Nano),
		})
		if err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
				 [[status]], [[error_code]], [[diagnostic_reference]], [[not_before]], [[created]], [[updated]],
				 [[payload_json]], [[cursor]], [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]],
				 [[chunk_count]], [[captured_bytes]])
			VALUES
				({:id}, {:owner}, '', {:requestId}, {:requestHash}, {:kind}, '',
				 'queued', '', '', {:notBefore}, {:now}, {:now},
				 {:payload}, '', 0, 0, 0, {:maxAttempts}, 0, 0)
			ON CONFLICT ([[owner]], [[request_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": jobID, "owner": user.Id, "requestId": input.RequestID,
			"requestHash": requestHash, "kind": instanceProductExportJobKind,
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "now": now.UnixMilli(),
			"payload": string(payload), "maxAttempts": defaultInstanceProductExportMaxAttempts,
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
		if existing.Kind != instanceProductExportJobKind || existing.RequestHash != requestHash {
			identityReused = true
			return nil
		}
		jobID, replayed = existing.ID, true
		return nil
	}
	// A key's call asks its person only for an export that would start, or
	// one its request ID already started (user_key_acting_ask.go).
	err := rehearseBeforeAsking(event, change)
	if err == nil && !identityReused {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if identityReused {
		return apiError(event, http.StatusConflict, errorCodeInstanceProductExportIdentityReused, "The job request ID was used for a different export.")
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeInstanceProductExportCreateFailed, "Couldn't start the product data export.", "instance-product-export.create", "admission", errorCodeInstanceProductExportCreateFailed, err)
	}
	r.notify()
	job, err := r.instanceProductExportResponse(jobID, user.Id)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeInstanceProductExportReadFailed, "Couldn't read the product data export.", err)
	}
	status := http.StatusAccepted
	if replayed {
		status = http.StatusOK
	}
	return event.JSON(status, instanceProductExportJobResult{Replayed: replayed, Job: job})
}

func (r *durableJobRunner) getInstanceProductExport(event *core.RequestEvent) error {
	user, ok := authorizeInstanceProductExportRequest(event)
	if !ok {
		return nil
	}
	job, err := r.instanceProductExportResponse(strings.TrimSpace(event.Request.PathValue("jobId")), user.Id)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeInstanceProductExportNotFound, "Product data export not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeInstanceProductExportReadFailed, "Couldn't read the product data export.", err)
	}
	return event.JSON(http.StatusOK, instanceProductExportJobResult{Job: job})
}

func (r *durableJobRunner) getCurrentInstanceProductExport(event *core.RequestEvent) error {
	user, ok := authorizeInstanceProductExportRequest(event)
	if !ok {
		return nil
	}
	rows := []readyJob{}
	err := event.App.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_jobs}}
		WHERE [[owner]] = {:owner} AND [[kind]] = {:kind}
		ORDER BY [[updated]] DESC, [[id]] DESC LIMIT 1
	`).Bind(dbx.Params{"owner": user.Id, "kind": instanceProductExportJobKind}).All(&rows)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeInstanceProductExportReadFailed, "Couldn't read the product data export.", err)
	}
	if len(rows) == 0 {
		return event.JSON(http.StatusOK, instanceProductExportCurrentResult{})
	}
	job, err := r.instanceProductExportResponse(rows[0].ID, user.Id)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeInstanceProductExportReadFailed, "Couldn't read the product data export.", err)
	}
	return event.JSON(http.StatusOK, instanceProductExportCurrentResult{Job: &job})
}

func authorizeInstanceProductExportRequest(event *core.RequestEvent) (*core.Record, bool) {
	if !authorizeAdministratorRequest(event) {
		return nil, false
	}
	user, ok := authenticatedUser(event)
	return user, ok
}

func (r *durableJobRunner) instanceProductExportResponse(jobID, owner string) (instanceProductExportJob, error) {
	job, err := loadStoredJob(r.app, jobID, owner)
	if err != nil {
		return instanceProductExportJob{}, err
	}
	if job.Kind != instanceProductExportJobKind {
		return instanceProductExportJob{}, sql.ErrNoRows
	}
	payload := instanceProductExportJobPayload{}
	if json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil || payload.FormatVersion != instanceProductExportFormatVersion {
		return instanceProductExportJob{}, errors.New(errorCodeInstanceProductExportReadFailed)
	}
	artifact, artifactErr := loadInstanceProductExportArtifact(r.app, job.ID, owner)
	if artifactErr != nil && !errors.Is(artifactErr, sql.ErrNoRows) {
		return instanceProductExportJob{}, artifactErr
	}
	status := instanceProductExportStatusPending
	if job.Status == durableJobStatusCompleted {
		status = instanceProductExportStatusCompleted
	} else if job.Status == durableJobStatusFailed {
		status = instanceProductExportStatusFailed
	} else if job.Status != durableJobStatusQueued {
		return instanceProductExportJob{}, errors.New(errorCodeInstanceProductExportReadFailed)
	} else if job.Processed > 0 || job.ChunkCount > 0 || payload.Phase != instanceProductExportPhaseCapture {
		status = instanceProductExportStatusRunning
	}
	createdAt := time.UnixMilli(job.Created).UTC().Format(time.RFC3339Nano)
	updatedAt := time.UnixMilli(job.Updated).UTC().Format(time.RFC3339Nano)
	var completedAt *string
	if artifactErr == nil {
		value := time.UnixMilli(artifact.Created).UTC().Format(time.RFC3339Nano)
		completedAt = &value
	}
	return instanceProductExportJob{
		ID: job.ID, RequestID: job.RequestID, Status: status, Phase: payload.Phase,
		ProcessedItems: job.Processed, TotalItems: job.Total, ChunkCount: job.ChunkCount,
		ArtifactBytes: artifact.Bytes, ArtifactSHA256: artifact.SHA256,
		ErrorCode: job.ErrorCode, DiagnosticReference: job.DiagnosticReference,
		CanRetry: false, CanCancel: false,
		CreatedAt: createdAt, UpdatedAt: updatedAt, CompletedAt: completedAt,
	}, nil
}

func loadInstanceProductExportArtifact(app core.App, jobID, owner string) (storedInstanceProductExportArtifact, error) {
	artifact := storedInstanceProductExportArtifact{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]], [[owner]], [[format]], [[format_version]], [[filename]],
			[[content_type]], [[bytes]], [[sha256]], [[created]]
		FROM {{leafen_instance_product_export_artifacts}}
		WHERE [[job_id]] = {:jobId} AND [[owner]] = {:owner}
	`).Bind(dbx.Params{"jobId": jobID, "owner": owner}).One(&artifact)
	return artifact, err
}

func instanceProductExportRequestHash(input instanceProductExportInput) string {
	digest := sha256.Sum256([]byte(instanceProductExportJobKind + "\x00" + input.RequestID + "\x00" + input.Format))
	return hex.EncodeToString(digest[:])
}
