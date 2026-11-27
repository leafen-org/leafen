package organization

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionAdminLegalHoldExportItem struct {
	Kind         string          `json:"kind"`
	ID           string          `json:"id"`
	PageID       string          `json:"page_id"`
	CreatedBy    string          `json:"created_by"`
	LastEditedBy string          `json:"last_edited_by"`
	Payload      json.RawMessage `json:"payload"`
}

func applyNotionAdminLegalHoldExportChunk(r *leafen.DurableJobRunner,
	ctx context.Context,
	job leafen.StoredJob,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := validNotionAdminLegalHoldExportJob(job)
	if err != nil {
		return failNotionAdminLegalHoldExportJob(
			r.App(), job, leafen.ErrorCodeLegalHoldUnavailable, err,
		)
	}
	if payload.Phase != notionAdminLegalHoldExportPhaseCapture {
		return nil
	}
	err = captureNotionAdminLegalHoldExportChunk(r, job)
	if err == nil {
		return nil
	}
	code := leafen.ErrorCodeLegalHoldUnavailable
	if errors.Is(err, errNotionAdminLegalHoldExportSourceChanged) {
		code = leafen.ErrorCodeWorkspaceExportSourceChanged
	}
	return failNotionAdminLegalHoldExportJob(r.App(), job, code, err)
}

var errNotionAdminLegalHoldExportSourceChanged = errors.New(
	"Notion Admin legal-hold export source changed",
)

func captureNotionAdminLegalHoldExportChunk(r *leafen.DurableJobRunner, job leafen.StoredJob) error {
	return r.App().RunInTransaction(func(txApp core.App) error {
		current, err := leafen.LoadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) ||
			(err == nil && current.Status != leafen.DurableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		payload, err := validNotionAdminLegalHoldExportJob(current)
		if err != nil || payload.Phase != notionAdminLegalHoldExportPhaseCapture {
			return leafen.ErrLegalHoldUnavailable
		}
		revision, _, err := leafen.WorkspaceExportRevision(txApp, payload.WorkspaceID)
		if err != nil || revision != payload.WorkspaceRevision {
			return errNotionAdminLegalHoldExportSourceChanged
		}

		items := []leafen.ContentCustodianItem{}
		hasMore := false
		if len(payload.CustodianIDs) > 0 {
			items, hasMore, err = listContentCustodianWindow(
				txApp, payload.WorkspaceID, payload.CustodianIDs,
				payload.AfterKind, payload.AfterID,
			)
			if err != nil {
				return err
			}
		}
		if len(items) > leafen.ContentCustodianWindowLimit {
			return leafen.ErrLegalHoldUnavailable
		}
		wireItems, err := notionAdminLegalHoldExportItems(items)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(wireItems)
		if err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		now := leafen.DurableJobClockNow(r.Clock()).UTC()
		chunkCount := current.ChunkCount
		capturedBytes := current.CapturedBytes
		cursor := current.Cursor
		if len(items) > 0 {
			last := items[len(items)-1]
			payload.AfterKind, payload.AfterID = last.Kind, last.ID
			cursor = last.Kind + ":" + last.ID
			chunkCount++
			digest := sha256.Sum256(encoded)
			result, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_job_chunks}}
					([[job_id]], [[chunk_index]], [[item_count]], [[cursor]], [[sha256]], [[created]], [[data_json]])
				VALUES
					({:jobId}, {:chunkIndex}, {:itemCount}, {:cursor}, {:sha256}, {:created}, {:data})
				ON CONFLICT ([[job_id]], [[chunk_index]]) DO NOTHING
			`).Bind(dbx.Params{
				"jobId": current.ID, "chunkIndex": chunkCount,
				"itemCount": len(items), "cursor": cursor,
				"sha256": hex.EncodeToString(digest[:]), "created": now.UnixMilli(),
				"data": string(encoded),
			}).Execute()
			if err != nil {
				return err
			}
			inserted, err := result.RowsAffected()
			if err != nil || inserted != 1 {
				return errors.Join(leafen.ErrLegalHoldUnavailable, err)
			}
			capturedBytes += int64(len(encoded))
		}
		processed := current.Processed + len(items)
		status := leafen.DurableJobStatusQueued
		notBefore := now.Add(leafen.DurableJobReadyDelay).UnixMilli()
		if !hasMore {
			payload.Phase = notionAdminLegalHoldExportPhaseComplete
			status = leafen.DurableJobStatusCompleted
			notBefore = now.UnixMilli()
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = {:status}, [[payload_json]] = {:payload},
				[[cursor]] = {:cursor}, [[processed_items]] = {:processed},
				[[total_items]] = CASE WHEN {:done} = 1 THEN {:processed} ELSE [[total_items]] END,
				[[chunk_count]] = {:chunkCount}, [[captured_bytes]] = {:capturedBytes},
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "status": status, "payload": string(payloadJSON),
			"cursor": cursor, "processed": processed, "done": leafen.BoolInt(!hasMore),
			"chunkCount": chunkCount, "capturedBytes": capturedBytes,
			"notBefore": notBefore, "updated": now.UnixMilli(),
		}).Execute()
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil || updated != 1 {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		return nil
	})
}

func notionAdminLegalHoldExportItems(
	items []leafen.ContentCustodianItem,
) ([]notionAdminLegalHoldExportItem, error) {
	result := make([]notionAdminLegalHoldExportItem, 0, len(items))
	for _, item := range items {
		payload := json.RawMessage(item.Payload)
		if item.Kind == "" || item.ID == "" || item.PageID == "" ||
			item.CreatedBy == "" || item.LastEditedBy == "" || !json.Valid(payload) {
			return nil, leafen.ErrLegalHoldUnavailable
		}
		result = append(result, notionAdminLegalHoldExportItem{
			Kind: item.Kind, ID: item.ID, PageID: item.PageID,
			CreatedBy: item.CreatedBy, LastEditedBy: item.LastEditedBy,
			Payload: payload,
		})
	}
	return result, nil
}

func failNotionAdminLegalHoldExportJob(
	app core.App,
	job leafen.StoredJob,
	errorCode string,
	cause error,
) error {
	reference := leafen.NewDiagnosticReference()
	result, err := app.DB().NewQuery(`
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
		return errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if updated == 1 {
		leafen.LogDurableJobDiagnostic(
			app, errorCode, reference, job.ID,
			"notion-admin-legal-hold-export.run", "terminal", cause,
		)
	}
	return nil
}
