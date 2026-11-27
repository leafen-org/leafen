package organization

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	organizationAuditExportJobKind       = "organization-audit-export"
	organizationAuditExportPayloadFormat = 2
	organizationAuditExportPhaseCapture  = "capture"
	organizationAuditExportPhasePublish  = "publish"
	organizationAuditExportPhaseCleanup  = "cleanup"
)

type organizationAuditExportJobPayload struct {
	FormatVersion     int    `json:"formatVersion"`
	OrganizationID    string `json:"organizationId"`
	MutationID        string `json:"mutationId"`
	RequestHash       string `json:"requestHash"`
	Format            string `json:"format"`
	Action            string `json:"action"`
	TargetType        string `json:"targetType"`
	Actor             string `json:"actor,omitempty"`
	Target            string `json:"target,omitempty"`
	Since             int64  `json:"since"`
	Until             int64  `json:"until"`
	EffectiveSince    int64  `json:"effectiveSince"`
	EffectiveUntil    int64  `json:"effectiveUntil"`
	RetentionDays     int    `json:"retentionDays"`
	RetentionRevision int    `json:"retentionRevision"`
	Limit             int    `json:"limit"`
	Phase             string `json:"phase"`
	ContentHashState  string `json:"contentHashState,omitempty"`
	ContentSHA256     string `json:"contentSha256,omitempty"`
	FailureCode       string `json:"failureCode,omitempty"`
	FailureReference  string `json:"failureReference,omitempty"`
}

func organizationAuditExportRequestID(exportID string) string {
	return organizationAuditExportJobKind + ":" + exportID
}

func newOrganizationAuditExportPayload(
	organizationID string,
	input leafen.OrganizationAuditExportCreateInput,
	requestHash string,
	retention leafen.AuditRetentionPolicyResponse,
	now int64,
) organizationAuditExportJobPayload {
	// An export reaches back as far as the audit log is kept: all of it
	// where the organization keeps it.
	cutoff := leafen.AuditRetentionCutoff(now, retention.RetentionDays)
	effectiveSince := input.Since
	if effectiveSince < cutoff {
		effectiveSince = cutoff
	}
	effectiveUntil := input.Until
	if effectiveUntil == 0 || effectiveUntil > now {
		effectiveUntil = now
	}
	return organizationAuditExportJobPayload{
		FormatVersion:  organizationAuditExportPayloadFormat,
		OrganizationID: organizationID, MutationID: input.MutationID, RequestHash: requestHash,
		Format: input.Format, Action: input.Action, TargetType: input.TargetType, Actor: input.Actor, Target: input.Target,
		Since: input.Since, Until: input.Until,
		EffectiveSince: effectiveSince, EffectiveUntil: effectiveUntil,
		RetentionDays: retention.RetentionDays, RetentionRevision: retention.Revision,
		Limit: input.Limit, Phase: organizationAuditExportPhaseCapture,
	}
}

func createOrganizationAuditExport(r *leafen.DurableJobRunner, event *core.RequestEvent) error {
	authorization, ok := leafen.AuthorizeOrganizationAudit(event)
	if !ok {
		return nil
	}
	input := leafen.OrganizationAuditExportCreateInput{}
	if err := event.BindBody(&input); err != nil {
		return leafen.OrganizationAuditError(event, leafen.ErrOrganizationAuditInvalid, "export-bind")
	}
	input, requestHash, err := leafen.NormalizeOrganizationAuditExportInput(input)
	if err != nil {
		return leafen.OrganizationAuditError(event, err, "export-create")
	}
	// Reads gathered in the last second go in too.
	leafen.FlushReadLog(event.App)
	exportID := leafen.OrganizationAuditID(
		authorization.OrganizationID, "organization_audit.export",
		authorization.PrincipalID+"\x00"+input.MutationID,
	)
	duplicate := false
	err = event.App.RunInTransaction(func(txApp core.App) error {
		current, err := leafen.AuthorizeOrganization(
			txApp, authorization.PrincipalID, authorization.OrganizationID, leafen.PermissionOrganizationAuditLog,
		)
		if err != nil {
			return err
		}
		stored, found, err := leafen.FindOrganizationAuditExportByMutation(
			txApp, current.OrganizationID, current.PrincipalID, input.MutationID,
		)
		if err != nil {
			return err
		}
		if found {
			if stored.RequestHash != requestHash || stored.ID != exportID {
				return leafen.ErrOrganizationAuditConflict
			}
			duplicate = true
			return nil
		}
		existing, loadErr := leafen.LoadStoredJob(txApp, exportID, current.PrincipalID)
		if loadErr == nil {
			if existing.Kind != organizationAuditExportJobKind || existing.RequestHash != requestHash ||
				existing.RequestID != organizationAuditExportRequestID(exportID) {
				return leafen.ErrOrganizationAuditConflict
			}
			duplicate = true
			return nil
		}
		if !errors.Is(loadErr, sql.ErrNoRows) {
			return loadErr
		}
		retention, err := leafen.LoadOrganizationAuditRetentionPolicy(txApp, current.OrganizationID)
		if err != nil {
			return errors.Join(leafen.ErrOrganizationAuditUnavailable, err)
		}
		nowTime := time.Now().UTC()
		now := nowTime.UnixMilli()
		payload, err := json.Marshal(newOrganizationAuditExportPayload(
			current.OrganizationID, input, requestHash, retention, now,
		))
		if err != nil {
			return errors.Join(leafen.ErrOrganizationAuditUnavailable, err)
		}
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
				 [[status]], [[error_code]], [[diagnostic_reference]], [[not_before]], [[created]], [[updated]],
				 [[payload_json]], [[cursor]], [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]],
				 [[chunk_count]], [[captured_bytes]])
			VALUES
				({:id}, {:owner}, '', {:requestId}, {:requestHash}, {:kind}, '',
				 'queued', '', '', {:notBefore}, {:now}, {:now},
				 {:payload}, '', 0, {:totalItems}, 0, 3, 0, 0)
		`).Bind(dbx.Params{
			"id": exportID, "owner": current.PrincipalID,
			"requestId": organizationAuditExportRequestID(exportID), "requestHash": requestHash,
			"kind":      organizationAuditExportJobKind,
			"notBefore": nowTime.Add(leafen.DurableJobReadyDelay).UnixMilli(), "now": now,
			"payload": string(payload), "totalItems": input.Limit,
		}).Execute()
		return err
	})
	if err != nil {
		return leafen.OrganizationAuditError(event, err, "export-admit")
	}
	r.Notify()
	response, err := organizationAuditExportContent(r,
		event.App, authorization.OrganizationID, authorization.PrincipalID, exportID, duplicate,
	)
	if err != nil {
		return leafen.OrganizationAuditError(event, err, "export-read")
	}
	status := http.StatusAccepted
	if duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func getOrganizationAuditExport(r *leafen.DurableJobRunner, event *core.RequestEvent) error {
	authorization, ok := leafen.AuthorizeOrganizationAudit(event)
	if !ok {
		return nil
	}
	response, err := organizationAuditExportContent(r,
		event.App, authorization.OrganizationID, authorization.PrincipalID,
		event.Request.PathValue("exportId"), false,
	)
	if err != nil {
		return leafen.OrganizationAuditError(event, err, "export-get")
	}
	return event.JSON(http.StatusOK, response)
}

func organizationAuditExportContent(r *leafen.DurableJobRunner,
	app core.App,
	organizationID, ownerID, exportID string,
	duplicate bool,
) (leafen.OrganizationAuditExportContentResponse, error) {
	completed, err := leafen.LoadOrganizationAuditExport(app, organizationID, exportID)
	if err == nil {
		completed.AuditExport.Duplicate = duplicate
		return completed, nil
	}
	if !errors.Is(err, leafen.ErrOrganizationAuditNotFound) {
		return leafen.OrganizationAuditExportContentResponse{}, err
	}
	job, err := leafen.LoadStoredJob(app, exportID, ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return leafen.OrganizationAuditExportContentResponse{}, leafen.ErrOrganizationAuditNotFound
	}
	if err != nil {
		return leafen.OrganizationAuditExportContentResponse{}, errors.Join(leafen.ErrOrganizationAuditUnavailable, err)
	}
	payload, err := validOrganizationAuditExportJob(job, organizationID)
	if err != nil {
		return leafen.OrganizationAuditExportContentResponse{}, err
	}
	status := leafen.OrganizationAuditExportStatusPending
	if job.Status == leafen.DurableJobStatusFailed {
		status = leafen.OrganizationAuditExportStatusFailed
	} else if job.Status != leafen.DurableJobStatusQueued {
		return leafen.OrganizationAuditExportContentResponse{}, leafen.ErrOrganizationAuditUnavailable
	} else if job.Processed > 0 || payload.Phase == organizationAuditExportPhasePublish {
		status = leafen.OrganizationAuditExportStatusRunning
	}
	return leafen.OrganizationAuditExportContentResponse{AuditExport: leafen.OrganizationAuditExportResponse{
		ID: job.ID, OrganizationID: payload.OrganizationID, Status: status,
		Format: payload.Format, Action: payload.Action, TargetType: payload.TargetType,
		Since: payload.Since, Until: payload.Until, RetentionDays: payload.RetentionDays,
		Limit: payload.Limit, ProcessedEvents: job.Processed,
		CreatedBy: job.Owner, CreatedAt: job.Created,
		ErrorCode: job.ErrorCode, DiagnosticReference: job.DiagnosticReference,
		Duplicate: duplicate,
	}}, nil
}

func validOrganizationAuditExportJob(
	job leafen.StoredJob,
	organizationID string,
) (organizationAuditExportJobPayload, error) {
	payload := organizationAuditExportJobPayload{}
	if job.Kind != organizationAuditExportJobKind ||
		json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil ||
		payload.OrganizationID == "" || (organizationID != "" && payload.OrganizationID != organizationID) ||
		payload.RequestHash != job.RequestHash || len(payload.RequestHash) != 64 ||
		payload.MutationID == "" ||
		(payload.Phase != organizationAuditExportPhaseCapture &&
			payload.Phase != organizationAuditExportPhasePublish &&
			payload.Phase != organizationAuditExportPhaseCleanup) {
		return organizationAuditExportJobPayload{}, leafen.ErrOrganizationAuditUnavailable
	}
	if payload.FormatVersion == 1 {
		if payload.Limit < 1 || payload.Limit > leafen.LegacyOrganizationAuditEventsPerExport ||
			payload.ContentHashState != "" || payload.ContentSHA256 != "" {
			return organizationAuditExportJobPayload{}, leafen.ErrOrganizationAuditUnavailable
		}
		return payload, nil
	}
	if payload.FormatVersion != organizationAuditExportPayloadFormat || payload.Limit < 0 ||
		(payload.Phase == organizationAuditExportPhaseCapture && payload.ContentSHA256 != "") ||
		(payload.Phase == organizationAuditExportPhasePublish &&
			(payload.ContentHashState != "" || len(payload.ContentSHA256) != 64)) ||
		(payload.Phase == organizationAuditExportPhaseCleanup &&
			(payload.ContentHashState != "" || payload.ContentSHA256 != "" ||
				payload.FailureCode == "" || payload.FailureReference == "")) {
		return organizationAuditExportJobPayload{}, leafen.ErrOrganizationAuditUnavailable
	}
	return payload, nil
}
