package organization

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedOrganizationAuditExportChunk struct {
	Index     int    `db:"chunk_index"`
	ItemCount int    `db:"item_count"`
	DataJSON  string `db:"data_json"`
	SHA256    string `db:"sha256"`
}

func applyOrganizationAuditExportChunk(r *leafen.DurableJobRunner, ctx context.Context, job leafen.StoredJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := validOrganizationAuditExportJob(job, "")
	if err != nil {
		return failOrganizationAuditExportJob(r.App(), job, leafen.ErrorCodeOrganizationAuditUnavailable, err)
	}
	if payload.FormatVersion == 1 {
		return upgradeLegacyOrganizationAuditExportJob(r.App(), job)
	}
	switch payload.Phase {
	case organizationAuditExportPhaseCapture:
		err = captureOrganizationAuditExportChunk(r, job)
	case organizationAuditExportPhasePublish:
		err = publishOrganizationAuditExport(r, job)
	case organizationAuditExportPhaseCleanup:
		err = cleanupOrganizationAuditExportChunks(r, job)
	default:
		err = leafen.ErrOrganizationAuditUnavailable
	}
	if err == nil {
		return nil
	}
	code := leafen.ErrorCodeOrganizationAuditUnavailable
	if errors.Is(err, leafen.ErrOrganizationAuditTooLarge) {
		code = leafen.ErrorCodeOrganizationAuditTooLarge
	} else if leafen.IsPermissionDenied(err) {
		code = leafen.ErrorCodePermissionDenied
	}
	return failOrganizationAuditExportJob(r.App(), job, code, err)
}

func upgradeLegacyOrganizationAuditExportJob(app core.App, job leafen.StoredJob) error {
	return app.RunInTransaction(func(txApp core.App) error {
		current, err := leafen.LoadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != leafen.DurableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		payload, err := validOrganizationAuditExportJob(current, "")
		if err != nil || payload.FormatVersion != 1 {
			return leafen.ErrOrganizationAuditUnavailable
		}
		rows := []storedOrganizationAuditExportChunk{}
		if err := txApp.DB().NewQuery(`
			SELECT [[chunk_index]], [[item_count]], [[data_json]], [[sha256]]
			FROM {{leafen_job_chunks}}
			WHERE [[job_id]] = {:jobId}
			ORDER BY [[chunk_index]] LIMIT 51
		`).Bind(dbx.Params{"jobId": current.ID}).All(&rows); err != nil {
			return err
		}
		if len(rows) != current.ChunkCount || len(rows) > 50 {
			return leafen.ErrOrganizationAuditUnavailable
		}
		hasher := sha256.New()
		capturedBytes := int64(0)
		eventCount := 0
		for index, row := range rows {
			fragment := ""
			if row.Index != index+1 || row.ItemCount < 1 ||
				json.Unmarshal([]byte(row.DataJSON), &fragment) != nil {
				return leafen.ErrOrganizationAuditUnavailable
			}
			digest := sha256.Sum256([]byte(fragment))
			if row.SHA256 != hex.EncodeToString(digest[:]) {
				return leafen.ErrOrganizationAuditUnavailable
			}
			wireFragment := organizationAuditExportWireFragment(fragment, payload.Format, index > 0)
			_, _ = hasher.Write([]byte(wireFragment))
			capturedBytes += int64(len([]byte(wireFragment)))
			eventCount += row.ItemCount
		}
		if eventCount != current.Processed {
			return leafen.ErrOrganizationAuditUnavailable
		}
		payload.FormatVersion = organizationAuditExportPayloadFormat
		if payload.Phase == organizationAuditExportPhasePublish {
			if len(rows) == 0 {
				empty, err := leafen.SerializeOrganizationAuditEvents(nil, payload.Format)
				if err != nil {
					return err
				}
				_, _ = hasher.Write([]byte(empty))
				capturedBytes += int64(len([]byte(empty)))
			} else if payload.Format == leafen.OrganizationAuditExportFormatJSON {
				_, _ = hasher.Write([]byte("]"))
				capturedBytes++
			}
			payload.ContentSHA256 = hex.EncodeToString(hasher.Sum(nil))
		} else {
			payload.ContentHashState, err = leafen.EncodeExportContentHasher(hasher, leafen.ErrOrganizationAuditUnavailable)
			if err != nil {
				return err
			}
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[captured_bytes]] = {:capturedBytes},
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "payload": string(payloadJSON), "capturedBytes": capturedBytes,
			"notBefore": now.Add(leafen.DurableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		return err
	})
}

func captureOrganizationAuditExportChunk(r *leafen.DurableJobRunner, job leafen.StoredJob) error {
	return r.App().RunInTransaction(func(txApp core.App) error {
		current, err := leafen.LoadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != leafen.DurableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		payload, err := validOrganizationAuditExportJob(current, "")
		if err != nil || payload.Phase != organizationAuditExportPhaseCapture {
			return leafen.ErrOrganizationAuditUnavailable
		}
		if err := recheckOrganizationAuditExportPolicy(txApp, current.Owner, payload); err != nil {
			return err
		}
		pageLimit := leafen.OrganizationAuditExportEventsPerTurn
		if payload.Limit > 0 {
			remaining := payload.Limit - current.Processed
			if remaining < 0 {
				return leafen.ErrOrganizationAuditUnavailable
			}
			pageLimit = min(remaining, pageLimit)
		}
		page := leafen.OrganizationAuditEventListResponse{}
		if pageLimit > 0 {
			page, err = leafen.ListOrganizationAuditEvents(txApp, payload.OrganizationID, leafen.OrganizationAuditFilter{
				Cursor: current.Cursor, Action: payload.Action, TargetType: payload.TargetType, Actor: payload.Actor, Target: payload.Target,
				Since: payload.EffectiveSince, Until: payload.EffectiveUntil, Limit: pageLimit,
			}, leafen.OrganizationAuditExportEventsPerTurn)
			if err != nil {
				return err
			}
		}
		fragment, err := serializeOrganizationAuditExportFragment(
			page.Events, payload.Format, current.ChunkCount > 0,
		)
		if err != nil {
			return err
		}
		hasher, err := leafen.RestoreExportContentHasher(payload.ContentHashState, leafen.ErrOrganizationAuditUnavailable)
		if err != nil {
			return err
		}
		capturedBytes := current.CapturedBytes
		wireFragment := organizationAuditExportWireFragment(
			fragment, payload.Format, current.ChunkCount > 0,
		)
		if len(wireFragment) > 0 {
			_, _ = hasher.Write([]byte(wireFragment))
			capturedBytes += int64(len([]byte(wireFragment)))
		}
		now := time.Now().UTC()
		chunkCount := current.ChunkCount
		if len(page.Events) > 0 {
			encoded, err := json.Marshal(fragment)
			if err != nil || len(encoded) > leafen.OrganizationAuditExportChunkMaxBytes {
				return leafen.ErrOrganizationAuditTooLarge
			}
			chunkCount++
			digest := sha256.Sum256([]byte(fragment))
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_job_chunks}}
					([[job_id]], [[chunk_index]], [[item_count]], [[cursor]], [[sha256]], [[created]], [[data_json]])
				VALUES
					({:jobId}, {:chunkIndex}, {:itemCount}, {:cursor}, {:sha256}, {:created}, {:data})
				ON CONFLICT ([[job_id]], [[chunk_index]]) DO NOTHING
			`).Bind(dbx.Params{
				"jobId": current.ID, "chunkIndex": chunkCount, "itemCount": len(page.Events),
				"cursor": page.NextCursor, "sha256": hex.EncodeToString(digest[:]),
				"created": now.UnixMilli(), "data": string(encoded),
			}).Execute(); err != nil {
				return err
			}
		}
		processed := current.Processed + len(page.Events)
		done := page.NextCursor == "" || (payload.Limit > 0 && processed >= payload.Limit)
		if done {
			payload.Phase = organizationAuditExportPhasePublish
			if chunkCount == 0 {
				empty, err := leafen.SerializeOrganizationAuditEvents(nil, payload.Format)
				if err != nil {
					return err
				}
				_, _ = hasher.Write([]byte(empty))
				capturedBytes += int64(len([]byte(empty)))
			} else if payload.Format == leafen.OrganizationAuditExportFormatJSON {
				_, _ = hasher.Write([]byte("]"))
				capturedBytes++
			}
			payload.ContentSHA256 = hex.EncodeToString(hasher.Sum(nil))
			payload.ContentHashState = ""
		} else {
			payload.ContentHashState, err = leafen.EncodeExportContentHasher(hasher, leafen.ErrOrganizationAuditUnavailable)
			if err != nil {
				return err
			}
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[cursor]] = {:cursor},
				[[processed_items]] = {:processed}, [[chunk_count]] = {:chunkCount},
				[[captured_bytes]] = {:capturedBytes}, [[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "payload": string(payloadJSON), "cursor": page.NextCursor,
			"processed": processed, "chunkCount": chunkCount, "capturedBytes": capturedBytes,
			"notBefore": now.Add(leafen.DurableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		return err
	})
}

func organizationAuditExportWireFragment(fragment, format string, hasPrevious bool) string {
	if fragment == "" || format != leafen.OrganizationAuditExportFormatJSON {
		return fragment
	}
	return leafen.JSONArrayExportWireFragment(fragment, hasPrevious)
}

func serializeOrganizationAuditExportFragment(
	events []leafen.OrganizationAuditEventResponse,
	format string,
	hasPrevious bool,
) (string, error) {
	if len(events) == 0 {
		return "", nil
	}
	content, err := leafen.SerializeOrganizationAuditEvents(events, format)
	if err != nil {
		return "", err
	}
	switch format {
	case leafen.OrganizationAuditExportFormatJSON:
		if len(content) < 2 || content[0] != '[' || content[len(content)-1] != ']' {
			return "", leafen.ErrOrganizationAuditUnavailable
		}
		return content[1 : len(content)-1], nil
	case leafen.OrganizationAuditExportFormatCSV:
		if !hasPrevious {
			return content, nil
		}
		header, err := leafen.SerializeOrganizationAuditEvents(nil, format)
		if err != nil || !strings.HasPrefix(content, header) {
			return "", leafen.ErrOrganizationAuditUnavailable
		}
		return strings.TrimPrefix(content, header), nil
	default:
		return content, nil
	}
}

func recheckOrganizationAuditExportPolicy(
	app core.App,
	owner string,
	payload organizationAuditExportJobPayload,
) error {
	if _, err := leafen.AuthorizeOrganization(
		app, owner, payload.OrganizationID, leafen.PermissionOrganizationAuditLog,
	); err != nil {
		return err
	}
	retention, err := leafen.LoadOrganizationAuditRetentionPolicy(app, payload.OrganizationID)
	if err != nil {
		return errors.Join(leafen.ErrOrganizationAuditUnavailable, err)
	}
	if retention.RetentionDays != payload.RetentionDays || retention.Revision != payload.RetentionRevision {
		return leafen.ErrOrganizationAuditUnavailable
	}
	return nil
}

func publishOrganizationAuditExport(r *leafen.DurableJobRunner, job leafen.StoredJob) error {
	return r.App().RunInTransaction(func(txApp core.App) error {
		current, err := leafen.LoadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != leafen.DurableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		payload, err := validOrganizationAuditExportJob(current, "")
		if err != nil || payload.Phase != organizationAuditExportPhasePublish ||
			len(payload.ContentSHA256) != 64 || current.CapturedBytes < 0 {
			return leafen.ErrOrganizationAuditUnavailable
		}
		if err := recheckOrganizationAuditExportPolicy(txApp, current.Owner, payload); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		stored := leafen.StoredOrganizationAuditExport{
			ID: current.ID, OrganizationID: payload.OrganizationID, ActorID: current.Owner,
			MutationID: payload.MutationID, RequestHash: payload.RequestHash,
			Status: leafen.OrganizationAuditExportStatusCompleted, Format: payload.Format,
			Action: payload.Action, TargetType: payload.TargetType, Since: payload.Since, Until: payload.Until,
			RetentionDays: payload.RetentionDays, Limit: payload.Limit, EventCount: current.Processed,
			ArtifactVersion: leafen.OrganizationAuditExportArtifactFormat, ChunkCount: current.ChunkCount,
			ContentBytes: current.CapturedBytes, ContentSHA256: payload.ContentSHA256, Content: "",
			CreatedBy: current.Owner, CompletedAt: now, CreatedAt: current.Created,
		}
		// The log says "exported" when the file is served (the download), not
		// when it is ready: a screen that never takes it exported nothing.
		if err := leafen.InsertOrganizationAuditExport(txApp, stored); err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = 'completed', [[processed_items]] = {:processed}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "processed": current.Processed, "updated": now,
		}).Execute()
		return err
	})
}
