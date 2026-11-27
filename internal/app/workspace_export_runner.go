package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func (r *durableJobRunner) applyWorkspaceExportChunk(ctx context.Context, job storedJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload := workspaceExportJobPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("%s: invalid export payload: %w", errorCodeWorkspaceExportCreateFailed, err)
	}
	normalizeStoredWorkspaceExportPayload(&payload)
	if payload.NotionAdmin != nil && !notionAdminSpaceExportArtifactSupported(*payload.NotionAdmin) {
		return failWorkspaceExportJob(r.app, job, errorCodeWorkspaceExportFormatUnsupported)
	}
	var err error
	switch payload.Phase {
	case workspaceExportPhaseScope:
		err = r.captureWorkspaceExportScopeTurn(job, payload)
	case workspaceExportPhaseInventory:
		err = r.captureWorkspaceExportChunk(job, payload)
	case workspaceExportPhaseArtifact:
		err = r.publishWorkspaceExportArtifact(ctx, job, payload)
	default:
		err = newWorkspaceExportFailure(errorCodeWorkspaceExportCreateFailed)
	}
	if err == nil {
		return nil
	}
	if isSQLiteBusyError(err) {
		err = newWorkspaceExportFailureWithCause(errorCodeWorkspaceExportDatabaseBusy, err)
	}
	if code := stableWorkspaceExportFailureCode(err); code != "" {
		return failWorkspaceExportJob(r.app, job, code, err)
	}
	return err
}

func (r *durableJobRunner) captureWorkspaceExportChunk(job storedJob, payload workspaceExportJobPayload) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(current.PayloadJSON), &payload); err != nil {
			return err
		}
		normalizeStoredWorkspaceExportPayload(&payload)
		scoped, err := authorizeWorkspaceExportScope(txApp, current.Owner, current.WorkspaceID, payload.Scope)
		authorization := scoped.Workspace
		if isPermissionDenied(err) && pageAccessHeld(err) == "" {
			return failWorkspaceExportJob(txApp, current, errorCodePermissionDenied)
		}
		if err != nil {
			return err
		}
		// Edits go on while an export runs: each part takes the pages as they
		// are when it reads them, as a person's export on a live workspace must
		// (2026-11-05: any edit anywhere ended it, one of 38,000 items at 5 minutes).
		if authorization.OwnerID != payload.AuthorityOwnerID {
			return failWorkspaceExportJob(txApp, current, errorCodeWorkspaceExportSourceChanged)
		}
		// An archive alone holds one moment: it is brought back whole, so a page
		// made or removed while it is read leaves it naming what it doesn't hold.
		// It stops at once, not at the end; text typed meanwhile doesn't touch it.
		// The count is the one taken when it was asked for (SourceItems), the
		// same way; the job's total counts its parts, another thing (2026-11-09:
		// every archive of a resting workspace ended "source changed").
		if payload.Format == workspaceExportFormatNativeArchive && payload.SourceItems > 0 {
			_, items, err := workspaceExportSourceRevision(txApp, authorization)
			if err != nil {
				return err
			}
			if items != payload.SourceItems {
				return failWorkspaceExportJob(txApp, current, errorCodeWorkspaceExportSourceChanged)
			}
		}

		// Windows go on in one turn while it stays short, as a page copy's do
		// (page_duplication_runner.go): the wait after each window of 32 took
		// nearly all of a large export's inventory (2026-11-11: 100,022 items
		// of a workspace with a table of 100,000 rows in 3,126 turns, four a
		// second, some 13 minutes; most of each turn the 250 ms before the next).
		deadline := time.Now().Add(workspaceExportTurnBudget)
		processed, chunkIndex, capturedBytes := current.Processed, current.ChunkCount, current.CapturedBytes
		done := false
		for {
			chunk, nextCursor, last, err := loadWorkspaceExportInventoryForJob(
				txApp,
				authorization,
				current,
				payload,
				payload.InventoryCursor,
				workspaceExportChunkItems,
			)
			if err != nil {
				return err
			}
			done = last
			if len(chunk.Items) > 0 {
				encoded, err := json.Marshal(chunk)
				if err != nil {
					return err
				}
				chunkIndex++
				if err := storeWorkspaceExportChunk(txApp, current.ID, chunkIndex, nextCursor, chunk, encoded); err != nil {
					return err
				}
				payload.InventoryCursor = nextCursor
				payload.OmittedFiles += chunk.OmittedFiles
				for _, item := range chunk.Items {
					switch item.Kind {
					case workspaceExportItemPage:
						payload.PagesExported++
					case workspaceExportItemRow:
						payload.RowsExported++
					}
				}
				capturedBytes += int64(len(encoded))
				processed += len(chunk.Items)
			}
			if done || len(chunk.Items) == 0 || !time.Now().Before(deadline) {
				break
			}
		}
		if done {
			payload.Phase = workspaceExportPhaseArtifact
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		// What was made or removed meanwhile is counted as it was found.
		total := current.Total
		if done || processed > total {
			total = processed
		}
		now := time.Now().UTC()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[cursor]] = {:cursor},
				[[processed_items]] = {:processed}, [[total_items]] = {:total},
				[[chunk_count]] = {:chunkCount},
				[[captured_bytes]] = {:capturedBytes},
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "payload": string(payloadJSON), "cursor": payload.InventoryCursor,
			"processed": processed, "total": total, "chunkCount": chunkIndex,
			"capturedBytes": capturedBytes,
			"notBefore":     now.Add(durableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		return err
	})
}

// workspaceExportTurnBudget is how long one turn of an export's inventory may
// go on with further windows before yielding to other work.
const workspaceExportTurnBudget = 150 * time.Millisecond

// storeWorkspaceExportChunk keeps one window of the inventory as the job's
// chunk at that index, with the files its pages name.
func storeWorkspaceExportChunk(
	app core.App, jobID string, chunkIndex int, cursor string, chunk workspaceExportInventoryChunk, encoded []byte,
) error {
	digest := sha256.Sum256(encoded)
	created := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_export_chunks}}
			([[job_id]], [[chunk_index]], [[cursor]], [[item_count]], [[data_json]], [[sha256]], [[created]])
		VALUES ({:jobId}, {:chunkIndex}, {:cursor}, {:itemCount}, {:data}, {:sha256}, {:created})
		ON CONFLICT ([[job_id]], [[chunk_index]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "chunkIndex": chunkIndex, "cursor": cursor,
		"itemCount": len(chunk.Items), "data": string(encoded),
		"sha256": hex.EncodeToString(digest[:]), "created": created,
	}).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_job_chunks}}
			([[job_id]], [[chunk_index]], [[item_count]], [[cursor]], [[sha256]], [[created]])
		VALUES ({:jobId}, {:chunkIndex}, {:itemCount}, {:cursor}, {:sha256}, {:created})
		ON CONFLICT ([[job_id]], [[chunk_index]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "chunkIndex": chunkIndex, "itemCount": len(chunk.Items),
		"cursor": cursor, "sha256": hex.EncodeToString(digest[:]), "created": created,
	}).Execute(); err != nil {
		return err
	}
	return storeWorkspaceExportFileSnapshots(app, jobID, chunk.Files)
}

func failWorkspaceExportJob(app core.App, job storedJob, errorCode string, causes ...error) error {
	reference := newDiagnosticReference()
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]] = 'failed', [[error_code]] = {:errorCode},
			[[diagnostic_reference]] = {:reference}, [[attempt]] = [[attempt]] + 1,
			[[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'
	`).Bind(dbx.Params{
		"id": job.ID, "errorCode": errorCode, "reference": reference,
		"updated": time.Now().UTC().UnixMilli(),
	}).Execute()
	if err != nil {
		return newWorkspaceExportFailureWithCause(errorCode, err)
	}
	var cause error = errors.New(errorCode)
	if len(causes) > 0 && causes[0] != nil {
		cause = causes[0]
	}
	logDurableJobDiagnostic(app, errorCode, reference, job.ID, "workspace-export.run", "terminal", cause)
	return nil
}

func (r *durableJobRunner) publishWorkspaceExportArtifact(
	ctx context.Context,
	job storedJob,
	payload workspaceExportJobPayload,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	artifact, path, err := buildWorkspaceExportArtifact(r.app, job, payload)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	err = r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		scopeAuthorization, err := authorizeWorkspaceExportScope(txApp, current.Owner, current.WorkspaceID, payload.Scope)
		if isPermissionDenied(err) && pageAccessHeld(err) == "" {
			return failWorkspaceExportJob(txApp, current, errorCodePermissionDenied)
		}
		if err != nil {
			return err
		}
		if scopeAuthorization.Workspace.OwnerID != payload.AuthorityOwnerID {
			return failWorkspaceExportJob(txApp, current, errorCodeWorkspaceExportSourceChanged)
		}
		now := time.Now().UTC()
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_export_artifacts}}
				([[job_id]], [[workspace_id]], [[owner]], [[format]], [[filename]], [[content_type]], [[bytes]], [[sha256]], [[created]])
			VALUES ({:jobId}, {:workspaceId}, {:owner}, {:format}, {:filename}, {:contentType}, {:bytes}, {:sha256}, {:created})
			ON CONFLICT ([[job_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"jobId": current.ID, "workspaceId": current.WorkspaceID, "owner": current.Owner,
			"format": artifact.Format, "filename": artifact.Filename,
			"contentType": artifact.ContentType, "bytes": artifact.Bytes,
			"sha256": artifact.SHA256, "created": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		if err := recordWorkspaceExportAudit(txApp, current, payload, scopeAuthorization.Workspace, now.UnixMilli()); err != nil {
			return err
		}
		payload.Phase = workspaceExportPhaseCompleted
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = 'completed', [[payload_json]] = {:payload},
				[[processed_items]] = [[total_items]], [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{"id": current.ID, "payload": string(payloadJSON), "updated": now.UnixMilli()}).Execute()
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err == nil && updated == 1 {
			keep = true
		}
		return err
	})
	return err
}

func recordWorkspaceExportAudit(app core.App, job storedJob, payload workspaceExportJobPayload, authorization workspaceAuthorization, occurredAt int64) error {
	if payload.Scope.Kind == workspaceExportScopeWorkspace {
		return nil
	}
	actions := map[string]string{
		workspaceExportFormatPageMarkdown:      "export.page_markdown",
		workspaceExportFormatNotionMarkdown:    "export.notion_markdown",
		workspaceExportFormatDatabaseCSV:       "export.database_csv",
		workspaceExportFormatPageNativeArchive: "export.page_native_archive",
		workspaceExportFormatMarkdownCSV:       "export.markdown_csv",
		workspaceExportFormatHTML:              "export.html",
		workspaceExportFormatPrintHTML:         "export.print_html",
	}
	action := actions[payload.Format]
	if action == "" {
		return newWorkspaceExportFailure(errorCodeWorkspaceExportFormatUnsupported)
	}
	return recordOrganizationAuditEvent(app, organizationAuditEvent{
		ID:             organizationAuditID(authorization.Organization.OrganizationID, action, payload.Scope.TargetID+"\x00"+job.ID),
		OrganizationID: authorization.Organization.OrganizationID,
		WorkspaceID:    job.WorkspaceID,
		ActorID:        job.Owner,
		Action:         action,
		TargetType:     payload.Scope.Kind,
		TargetID:       payload.Scope.TargetID,
		OccurredAt:     occurredAt,
		Metadata:       map[string]any{"jobId": job.ID, "format": payload.Format},
	})
}
