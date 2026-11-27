package organization

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func applyOrganizationDiscoveryExport(r *leafen.DurableJobRunner, ctx context.Context, job leafen.StoredJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := validOrganizationDiscoveryExportJob(job, "")
	if err != nil {
		return failOrganizationDiscoveryExportJob(r.App(), job, leafen.ErrorCodeOrganizationDiscoveryUnavailable, err)
	}
	switch payload.Phase {
	case leafen.OrganizationDiscoveryExportPhaseCapture:
		err = captureOrganizationDiscoveryExport(r, job)
	case leafen.OrganizationDiscoveryExportPhasePublish:
		err = publishOrganizationDiscoveryExport(r, job)
	default:
		err = errOrganizationDiscoveryUnavailable
	}
	if err == nil {
		return nil
	}
	code := leafen.ErrorCodeOrganizationDiscoveryUnavailable
	if errors.Is(err, errOrganizationDiscoveryTooLarge) {
		code = leafen.ErrorCodeOrganizationDiscoveryTooLarge
	} else if errors.Is(err, errOrganizationDiscoverySourceChanged) {
		code = leafen.ErrorCodeOrganizationDiscoverySourceChanged
	} else if leafen.IsPermissionDenied(err) {
		code = leafen.ErrorCodePermissionDenied
	}
	return failOrganizationDiscoveryExportJob(r.App(), job, code, err)
}

func captureOrganizationDiscoveryExport(r *leafen.DurableJobRunner, job leafen.StoredJob) error {
	return r.App().RunInTransaction(func(txApp core.App) error {
		current, err := leafen.LoadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != leafen.DurableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		payload, err := validOrganizationDiscoveryExportJob(current, "")
		if err != nil || payload.Phase != leafen.OrganizationDiscoveryExportPhaseCapture {
			return errOrganizationDiscoveryUnavailable
		}
		if err := recheckOrganizationDiscoveryAuthority(txApp, current.Owner, payload); err != nil {
			return err
		}
		items, nextPayload, done, err := nextOrganizationDiscoveryWindow(txApp, payload)
		if err != nil {
			return err
		}
		if len(items) > leafen.OrganizationDiscoveryItemsPerTurn {
			return errOrganizationDiscoveryUnavailable
		}
		hasher, err := leafen.RestoreExportContentHasher(payload.ContentHashState, errOrganizationDiscoveryUnavailable)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if done {
			nextPayload.Phase = leafen.OrganizationDiscoveryExportPhasePublish
			nextPayload.Revision++
			nextPayload.ContentHashState, err = leafen.EncodeExportContentHasher(hasher, errOrganizationDiscoveryUnavailable)
			if err != nil {
				return err
			}
			return updateOrganizationDiscoveryProgress(txApp, current, nextPayload, current.Processed,
				current.ChunkCount, current.CapturedBytes, now)
		}
		nextItemCount := current.Processed + len(items)
		if nextItemCount > leafen.OrganizationDiscoveryMaximumItems {
			return errOrganizationDiscoveryTooLarge
		}
		fragment, err := serializeOrganizationDiscoveryFragment(items, payload.Format, current.Processed)
		if err != nil {
			return err
		}
		closingBytes := int64(0)
		if payload.Format == leafen.OrganizationDiscoveryExportFormatJSON {
			closingBytes = 1
			if nextItemCount == 0 {
				closingBytes = 2
			}
		}
		nextBytes := current.CapturedBytes + int64(len(fragment))
		if nextBytes+closingBytes > leafen.OrganizationDiscoveryMaximumBytes {
			return errOrganizationDiscoveryTooLarge
		}
		if fragment != "" {
			_, _ = hasher.Write([]byte(fragment))
		}
		addedChunks, err := insertOrganizationDiscoveryChunks(
			txApp, current.ID, current.ChunkCount, fragment, len(items), now.UnixMilli(),
		)
		if err != nil {
			return err
		}
		nextPayload.Revision++
		nextPayload.ContentHashState, err = leafen.EncodeExportContentHasher(hasher, errOrganizationDiscoveryUnavailable)
		if err != nil {
			return err
		}
		return updateOrganizationDiscoveryProgress(
			txApp, current, nextPayload, nextItemCount,
			current.ChunkCount+addedChunks, nextBytes, now,
		)
	})
}

func updateOrganizationDiscoveryProgress(
	app core.App,
	current leafen.StoredJob,
	payload organizationDiscoveryExportJobPayload,
	processed, chunkCount int,
	capturedBytes int64,
	now time.Time,
) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	cursor, err := json.Marshal(struct {
		WorkspaceIndex int    `json:"workspaceIndex"`
		WorkspaceID    string `json:"workspaceId,omitempty"`
		Entity         string `json:"entity"`
		After          string `json:"after,omitempty"`
	}{payload.WorkspaceIndex, payload.WorkspaceID, payload.Entity, payload.EntityCursor})
	if err != nil {
		return err
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[payload_json]]={:payload},[[cursor]]={:cursor},
			[[processed_items]]={:processed},[[chunk_count]]={:chunkCount},
			[[captured_bytes]]={:capturedBytes},[[not_before]]={:notBefore},[[updated]]={:updated}
		WHERE [[id]]={:id} AND [[status]]='queued' AND [[payload_json]]={:previousPayload}
	`).Bind(dbx.Params{
		"id": current.ID, "payload": string(payloadJSON), "previousPayload": current.PayloadJSON,
		"cursor": string(cursor), "processed": processed, "chunkCount": chunkCount,
		"capturedBytes": capturedBytes,
		"notBefore":     now.Add(leafen.DurableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
	}).Execute()
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil || updated != 1 {
		return errOrganizationDiscoveryUnavailable
	}
	return nil
}

func recheckOrganizationDiscoveryAuthority(
	app core.App, owner string, payload organizationDiscoveryExportJobPayload,
) error {
	if _, err := leafen.AuthorizeOrganization(
		app, owner, payload.OrganizationID, leafen.PermissionOrganizationContentSearch,
	); err != nil {
		return err
	}
	revision, err := loadOrganizationDiscoveryRevision(app, payload.OrganizationID)
	if err != nil {
		return err
	}
	if revision != payload.SourceRevision {
		return errOrganizationDiscoverySourceChanged
	}
	return nil
}

func publishOrganizationDiscoveryExport(r *leafen.DurableJobRunner, job leafen.StoredJob) error {
	return r.App().RunInTransaction(func(txApp core.App) error {
		current, err := leafen.LoadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != leafen.DurableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		payload, err := validOrganizationDiscoveryExportJob(current, "")
		if err != nil || payload.Phase != leafen.OrganizationDiscoveryExportPhasePublish {
			return errOrganizationDiscoveryUnavailable
		}
		if err := recheckOrganizationDiscoveryAuthority(txApp, current.Owner, payload); err != nil {
			return err
		}
		suffix, err := organizationDiscoverySuffix(payload.Format, current.Processed)
		if err != nil || current.CapturedBytes+int64(len(suffix)) > leafen.OrganizationDiscoveryMaximumBytes {
			return errOrganizationDiscoveryTooLarge
		}
		hasher, err := leafen.RestoreExportContentHasher(payload.ContentHashState, errOrganizationDiscoveryUnavailable)
		if err != nil {
			return err
		}
		if suffix != "" {
			_, _ = hasher.Write([]byte(suffix))
		}
		now := time.Now().UTC().UnixMilli()
		addedChunks, err := insertOrganizationDiscoveryChunks(
			txApp, current.ID, current.ChunkCount, suffix, 0, now,
		)
		if err != nil {
			return err
		}
		filterJSON, err := json.Marshal(payload.Filter)
		if err != nil {
			return err
		}
		digest := hex.EncodeToString(hasher.Sum(nil))
		revision := payload.Revision + 1
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_organization_discovery_exports}}
				([[id]],[[organization_id]],[[owner_id]],[[mutation_id]],[[request_hash]],[[format]],[[filter_json]],
				 [[status]],[[phase]],[[revision]],[[item_count]],[[byte_count]],[[chunk_count]],[[content_sha256]],
				 [[source_revision]],[[created_at]],[[completed_at]])
			VALUES
				({:id},{:organizationId},{:ownerId},{:mutationId},{:requestHash},{:format},{:filter},
				 'completed','completed',{:revision},{:itemCount},{:byteCount},{:chunkCount},{:sha256},
				 {:sourceRevision},{:createdAt},{:completedAt})
		`).Bind(dbx.Params{
			"id": current.ID, "organizationId": payload.OrganizationID, "ownerId": current.Owner,
			"mutationId": payload.MutationID, "requestHash": payload.RequestHash,
			"format": payload.Format, "filter": string(filterJSON), "revision": revision,
			"itemCount": current.Processed, "byteCount": current.CapturedBytes + int64(len(suffix)),
			"chunkCount": current.ChunkCount + addedChunks, "sha256": digest,
			"sourceRevision": payload.SourceRevision, "createdAt": current.Created, "completedAt": now,
		}).Execute()
		if err != nil {
			return err
		}
		// Its audit line is written when the file is served (the download),
		// as the audit log export's: an export no one took exported nothing.
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}} SET [[status]]='completed',[[updated]]={:updated}
			WHERE [[id]]={:id} AND [[status]]='queued' AND [[payload_json]]={:payload}
		`).Bind(dbx.Params{"id": current.ID, "payload": current.PayloadJSON, "updated": now}).Execute()
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil || updated != 1 {
			return errOrganizationDiscoveryUnavailable
		}
		return nil
	})
}

func failOrganizationDiscoveryExportJob(app core.App, job leafen.StoredJob, code string, cause error) error {
	reference := leafen.NewDiagnosticReference()
	transitioned := false
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := leafen.LoadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != leafen.DurableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_organization_discovery_export_chunks}} WHERE [[export_id]]={:exportId}
		`).Bind(dbx.Params{"exportId": current.ID}).Execute(); err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}} SET [[status]]='failed',[[error_code]]={:errorCode},
				[[diagnostic_reference]]={:reference},[[attempt]]=[[attempt]]+1,
				[[cursor]]='',[[processed_items]]=0,[[chunk_count]]=0,[[captured_bytes]]=0,[[updated]]={:updated}
			WHERE [[id]]={:id} AND [[status]]='queued'
		`).Bind(dbx.Params{
			"id": current.ID, "errorCode": code, "reference": reference,
			"updated": time.Now().UTC().UnixMilli(),
		}).Execute()
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil || updated != 1 {
			return errOrganizationDiscoveryUnavailable
		}
		transitioned = true
		return nil
	})
	if err == nil && transitioned {
		leafen.LogDurableJobDiagnostic(app, code, reference, job.ID, "organization-discovery.export", "terminal", cause)
	}
	return err
}
