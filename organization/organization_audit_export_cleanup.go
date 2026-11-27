package organization

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const organizationAuditExportCleanupChunksPerTurn = 16

func failOrganizationAuditExportJob(app core.App, job leafen.StoredJob, code string, cause error) error {
	reference := leafen.NewDiagnosticReference()
	payload, payloadErr := validOrganizationAuditExportJob(job, "")
	if payloadErr == nil && job.ChunkCount > 0 {
		payload.FormatVersion = organizationAuditExportPayloadFormat
		payload.Phase = organizationAuditExportPhaseCleanup
		payload.ContentHashState = ""
		payload.ContentSHA256 = ""
		payload.FailureCode = code
		payload.FailureReference = reference
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		result, err := app.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[attempt]] = [[attempt]] + 1,
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": job.ID, "payload": string(payloadJSON),
			"notBefore": now.Add(leafen.DurableJobReadyDelay).UnixMilli(),
			"updated":   now.UnixMilli(),
		}).Execute()
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err == nil && updated == 1 {
			leafen.LogDurableJobDiagnostic(app, code, reference, job.ID, "organization-audit.export", "cleanup", cause)
		}
		return err
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]] = 'failed', [[error_code]] = {:errorCode},
			[[diagnostic_reference]] = {:reference}, [[attempt]] = [[attempt]] + 1,
			[[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'
	`).Bind(dbx.Params{
		"id": job.ID, "errorCode": code, "reference": reference,
		"updated": time.Now().UTC().UnixMilli(),
	}).Execute()
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err == nil && updated == 1 {
		leafen.LogDurableJobDiagnostic(app, code, reference, job.ID, "organization-audit.export", "terminal", cause)
	}
	return err
}

func cleanupOrganizationAuditExportChunks(r *leafen.DurableJobRunner, job leafen.StoredJob) error {
	return r.App().RunInTransaction(func(txApp core.App) error {
		current, err := leafen.LoadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != leafen.DurableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		payload, err := validOrganizationAuditExportJob(current, "")
		if err != nil || payload.Phase != organizationAuditExportPhaseCleanup {
			return leafen.ErrOrganizationAuditUnavailable
		}
		result, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_job_chunks}} WHERE [[rowid]] IN (
				SELECT [[rowid]] FROM {{leafen_job_chunks}}
				WHERE [[job_id]] = {:jobId}
				ORDER BY [[chunk_index]] LIMIT {:limit}
			)
		`).Bind(dbx.Params{
			"jobId": current.ID, "limit": organizationAuditExportCleanupChunksPerTurn,
		}).Execute()
		if err != nil {
			return err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		remaining := current.ChunkCount - int(deleted)
		if remaining < 0 {
			return leafen.ErrOrganizationAuditUnavailable
		}
		if deleted < organizationAuditExportCleanupChunksPerTurn {
			_, err = txApp.DB().NewQuery(`
				UPDATE {{leafen_jobs}}
				SET [[status]] = 'failed', [[error_code]] = {:errorCode},
					[[diagnostic_reference]] = {:reference}, [[cursor]] = '',
					[[chunk_count]] = 0, [[captured_bytes]] = 0, [[updated]] = {:updated}
				WHERE [[id]] = {:id} AND [[status]] = 'queued'
			`).Bind(dbx.Params{
				"id": current.ID, "errorCode": payload.FailureCode,
				"reference": payload.FailureReference, "updated": now.UnixMilli(),
			}).Execute()
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[chunk_count]] = {:chunkCount}, [[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "chunkCount": remaining,
			"notBefore": now.Add(leafen.DurableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		return err
	})
}
