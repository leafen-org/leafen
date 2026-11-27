package organization

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	organizationDiscoveryExportJobKind       = "organization-discovery-export"
	organizationDiscoveryExportPayloadFormat = 1
)

type organizationDiscoveryExportJobPayload struct {
	FormatVersion    int                                `json:"formatVersion"`
	OrganizationID   string                             `json:"organizationId"`
	MutationID       string                             `json:"mutationId"`
	RequestHash      string                             `json:"requestHash"`
	Format           string                             `json:"format"`
	Filter           leafen.OrganizationDiscoveryFilter `json:"filter"`
	SourceRevision   int                                `json:"sourceRevision"`
	Revision         int                                `json:"revision"`
	Phase            string                             `json:"phase"`
	WorkspaceIndex   int                                `json:"workspaceIndex"`
	WorkspaceAfter   string                             `json:"workspaceAfter,omitempty"`
	WorkspaceID      string                             `json:"workspaceId,omitempty"`
	WorkspaceName    string                             `json:"workspaceName,omitempty"`
	Entity           string                             `json:"entity"`
	EntityCursor     string                             `json:"entityCursor,omitempty"`
	ContentHashState string                             `json:"contentHashState,omitempty"`
}

func organizationDiscoveryRequestID(exportID string) string {
	return organizationDiscoveryExportJobKind + ":" + exportID
}

func createOrganizationDiscoveryExport(r *leafen.DurableJobRunner, event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationDiscovery(event)
	if !ok {
		return nil
	}
	input := leafen.OrganizationDiscoveryExportCreateInput{}
	if err := event.BindBody(&input); err != nil {
		return organizationDiscoveryError(event, errOrganizationDiscoveryInvalid, "export-bind")
	}
	input, filter, requestHash, err := normalizeOrganizationDiscoveryInput(input)
	if err != nil {
		return organizationDiscoveryError(event, err, "export-create")
	}
	mutationID := input.MutationID
	exportID := leafen.OrganizationAuditID(
		authorization.OrganizationID, "organization_discovery.export",
		authorization.PrincipalID+"\x00"+mutationID,
	)
	duplicate := false
	change := func(txApp core.App) error {
		current, err := leafen.AuthorizeOrganization(
			txApp, authorization.PrincipalID, authorization.OrganizationID,
			leafen.PermissionOrganizationContentSearch,
		)
		if err != nil {
			return err
		}
		stored, found, err := findOrganizationDiscoveryExportByMutation(
			txApp, current.OrganizationID, current.PrincipalID, mutationID,
		)
		if err != nil {
			return err
		}
		if found {
			if stored.RequestHash != requestHash || stored.ID != exportID {
				return errOrganizationDiscoveryConflict
			}
			duplicate = true
			return nil
		}
		existing, loadErr := leafen.LoadStoredJob(txApp, exportID, "")
		if loadErr == nil {
			if existing.Owner != current.PrincipalID || existing.Kind != organizationDiscoveryExportJobKind ||
				existing.RequestHash != requestHash || existing.RequestID != organizationDiscoveryRequestID(exportID) {
				return errOrganizationDiscoveryConflict
			}
			duplicate = true
			return nil
		}
		if !errors.Is(loadErr, sql.ErrNoRows) {
			return loadErr
		}
		if err := assertOrganizationDiscoveryWorkspaces(txApp, current.OrganizationID, filter.WorkspaceIDs); err != nil {
			return err
		}
		if err := leafen.EnsureOrganizationPolicyState(txApp, current.OrganizationID); err != nil {
			return err
		}
		sourceRevision, err := loadOrganizationDiscoveryRevision(txApp, current.OrganizationID)
		if err != nil {
			return err
		}
		payloadJSON, err := json.Marshal(organizationDiscoveryExportJobPayload{
			FormatVersion:  organizationDiscoveryExportPayloadFormat,
			OrganizationID: current.OrganizationID, MutationID: mutationID, RequestHash: requestHash,
			Format: input.Format, Filter: filter, SourceRevision: sourceRevision,
			Revision: 1, Phase: leafen.OrganizationDiscoveryExportPhaseCapture,
			Entity: organizationDiscoveryEntityPages,
		})
		if err != nil {
			return errors.Join(errOrganizationDiscoveryUnavailable, err)
		}
		nowTime := time.Now().UTC()
		now := nowTime.UnixMilli()
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]],[[owner]],[[workspace_id]],[[request_id]],[[request_hash]],[[kind]],[[page_id]],
				 [[status]],[[error_code]],[[diagnostic_reference]],[[not_before]],[[created]],[[updated]],
				 [[payload_json]],[[cursor]],[[processed_items]],[[total_items]],[[attempt]],[[max_attempts]],
				 [[chunk_count]],[[captured_bytes]])
			VALUES
				({:id},{:owner},'',{:requestId},{:requestHash},{:kind},'',
				 'queued','','',{:notBefore},{:now},{:now},
				 {:payload},'',0,{:totalItems},0,1,0,0)
		`).Bind(dbx.Params{
			"id": exportID, "owner": current.PrincipalID,
			"requestId": organizationDiscoveryRequestID(exportID), "requestHash": requestHash,
			"kind":      organizationDiscoveryExportJobKind,
			"notBefore": nowTime.Add(leafen.DurableJobReadyDelay).UnixMilli(), "now": now,
			"payload": string(payloadJSON), "totalItems": leafen.OrganizationDiscoveryMaximumItems,
		}).Execute()
		return err
	}
	// A key's call asks its person only for an export that would start
	// (internal/app/user_key_acting_ask.go).
	err = leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		duplicate = false
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return organizationDiscoveryError(event, err, "export-admit")
	}
	r.Notify()
	response, err := organizationDiscoveryExportContent(r,
		event.App, authorization.OrganizationID, exportID, duplicate,
	)
	if err != nil {
		return organizationDiscoveryError(event, err, "export-read")
	}
	status := http.StatusAccepted
	if duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func getOrganizationDiscoveryExport(r *leafen.DurableJobRunner, event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationDiscovery(event)
	if !ok {
		return nil
	}
	response, err := organizationDiscoveryExportContent(r,
		event.App, authorization.OrganizationID, event.Request.PathValue("exportId"), false,
	)
	if err != nil {
		return organizationDiscoveryError(event, err, "export-get")
	}
	return event.JSON(http.StatusOK, response)
}

func listOrganizationDiscoveryExports(r *leafen.DurableJobRunner, event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationDiscovery(event)
	if !ok {
		return nil
	}
	limit, err := organizationDiscoveryListLimit(event)
	if err != nil {
		return organizationDiscoveryError(event, err, "exports-query")
	}
	response, err := listStoredOrganizationDiscoveryExports(
		event.App, authorization.OrganizationID,
		strings.TrimSpace(event.Request.URL.Query().Get("cursor")), limit,
	)
	if err != nil {
		return organizationDiscoveryError(event, err, "exports-list")
	}
	return event.JSON(http.StatusOK, response)
}

func organizationDiscoveryExportContent(r *leafen.DurableJobRunner,
	app core.App, organizationID, exportID string, duplicate bool,
) (leafen.OrganizationDiscoveryExportContentResponse, error) {
	completed, err := loadStoredOrganizationDiscoveryExport(app, organizationID, exportID)
	if err == nil {
		response, err := organizationDiscoveryExportFromStored(completed, organizationID, duplicate)
		return leafen.OrganizationDiscoveryExportContentResponse{DiscoveryExport: response}, err
	}
	if !errors.Is(err, errOrganizationDiscoveryNotFound) {
		return leafen.OrganizationDiscoveryExportContentResponse{}, err
	}
	job, err := leafen.LoadStoredJob(app, strings.TrimSpace(exportID), "")
	if errors.Is(err, sql.ErrNoRows) {
		return leafen.OrganizationDiscoveryExportContentResponse{}, errOrganizationDiscoveryNotFound
	}
	if err != nil {
		return leafen.OrganizationDiscoveryExportContentResponse{}, errors.Join(errOrganizationDiscoveryUnavailable, err)
	}
	payload, err := validOrganizationDiscoveryExportJob(job, organizationID)
	if err != nil {
		return leafen.OrganizationDiscoveryExportContentResponse{}, err
	}
	status := leafen.OrganizationDiscoveryExportStatusPending
	phase := payload.Phase
	if job.Status == leafen.DurableJobStatusFailed {
		status, phase = leafen.OrganizationDiscoveryExportStatusFailed, leafen.OrganizationDiscoveryExportPhaseFailed
	} else if job.Status != leafen.DurableJobStatusQueued {
		return leafen.OrganizationDiscoveryExportContentResponse{}, errOrganizationDiscoveryUnavailable
	} else if payload.Revision > 1 || payload.Phase == leafen.OrganizationDiscoveryExportPhasePublish {
		status = leafen.OrganizationDiscoveryExportStatusRunning
	}
	return leafen.OrganizationDiscoveryExportContentResponse{DiscoveryExport: leafen.OrganizationDiscoveryExportResponse{
		ID: job.ID, OrganizationID: payload.OrganizationID, Status: status, Phase: phase,
		Format: payload.Format, Filter: payload.Filter, Revision: payload.Revision,
		ItemCount: job.Processed, ByteCount: job.CapturedBytes, ChunkCount: job.ChunkCount,
		CreatedBy: job.Owner, CreatedAt: job.Created,
		ErrorCode: job.ErrorCode, DiagnosticReference: job.DiagnosticReference, Duplicate: duplicate,
	}}, nil
}

func validOrganizationDiscoveryExportJob(
	job leafen.StoredJob, organizationID string,
) (organizationDiscoveryExportJobPayload, error) {
	payload := organizationDiscoveryExportJobPayload{}
	if job.Kind != organizationDiscoveryExportJobKind ||
		json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil ||
		payload.FormatVersion != organizationDiscoveryExportPayloadFormat ||
		payload.OrganizationID == "" || (organizationID != "" && payload.OrganizationID != organizationID) ||
		payload.MutationID == "" || payload.RequestHash != job.RequestHash || len(payload.RequestHash) != 64 ||
		(payload.Format != leafen.OrganizationDiscoveryExportFormatJSONL && payload.Format != leafen.OrganizationDiscoveryExportFormatJSON) ||
		(payload.Phase != leafen.OrganizationDiscoveryExportPhaseCapture && payload.Phase != leafen.OrganizationDiscoveryExportPhasePublish) ||
		payload.SourceRevision < 0 || payload.Revision < 1 || payload.WorkspaceIndex < 0 ||
		organizationDiscoveryEntityIndex(payload.Entity) < 0 ||
		(payload.WorkspaceID == "") != (payload.WorkspaceName == "") ||
		job.Processed < 0 || job.Processed > leafen.OrganizationDiscoveryMaximumItems ||
		job.CapturedBytes < 0 || job.CapturedBytes > leafen.OrganizationDiscoveryMaximumBytes || job.ChunkCount < 0 {
		return organizationDiscoveryExportJobPayload{}, errOrganizationDiscoveryUnavailable
	}
	if err := validateOrganizationDiscoveryFilter(payload.Filter); err != nil {
		return organizationDiscoveryExportJobPayload{}, errOrganizationDiscoveryUnavailable
	}
	return payload, nil
}
