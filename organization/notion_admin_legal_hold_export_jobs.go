package organization

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAdminLegalHoldExportJobKind       = "notion-admin-legal-hold-export"
	notionAdminLegalHoldExportPayloadFormat = 1
	notionAdminLegalHoldExportPhaseCapture  = "capture"
	notionAdminLegalHoldExportPhaseComplete = "complete"
)

type notionAdminLegalHoldExportJobPayload struct {
	FormatVersion         int      `json:"formatVersion"`
	OrganizationID        string   `json:"organizationId"`
	LegalHoldID           string   `json:"legalHoldId"`
	WorkspaceID           string   `json:"workspaceId"`
	RequestingUserID      string   `json:"requestingUserId"`
	CustodianIDs          []string `json:"custodianIds"`
	HoldAuthorityRevision int64    `json:"holdAuthorityRevision"`
	WorkspaceRevision     int64    `json:"workspaceRevision"`
	RequestHash           string   `json:"requestHash"`
	Phase                 string   `json:"phase"`
	AfterKind             string   `json:"afterKind,omitempty"`
	AfterID               string   `json:"afterId,omitempty"`
}

type notionAdminLegalHoldExportAuthority struct {
	Status             string `db:"status"`
	NotionAdminVersion int    `db:"notion_admin_version"`
	CreatedAt          int64  `db:"created_at"`
	PolicyRevision     int64  `db:"policy_revision"`
}

type notionAdminLegalHoldExportScope struct {
	Kind     string `db:"kind"`
	TargetID string `db:"target_id"`
}

func admitNotionAdminLegalHoldExport(r *leafen.DurableJobRunner,
	ctx context.Context,
	secret string,
	expected leafen.NotionAdminIdentity,
	legalHoldID, workspaceID, requestingUserID string,
) (string, error) {
	exportID := ""
	err := r.App().RunInTransaction(func(txApp core.App) error {
		current, err := leafen.AuthenticateNotionAdminToken(ctx, txApp, secret, leafen.DurableJobClockNow(r.Clock()))
		if err != nil {
			return err
		}
		if !sameNotionAdminLegalHoldExportIdentity(current, expected) ||
			!current.HasCapability(leafen.NotionAdminCapabilityLegalHoldExport) {
			return errNotionAdminLegalHoldForbidden
		}
		if !current.CanAccessLegalHold(legalHoldID) ||
			!current.CanAccessWorkspace(workspaceID) {
			return errLegalHoldNotFound
		}
		if _, err := leafen.AuthorizeOrganization(
			txApp, requestingUserID, current.OrganizationID, leafen.PermissionOrganizationActive,
		); err != nil {
			return errLegalHoldNotFound
		}
		workspaceRevision, _, err := leafen.WorkspaceExportRevision(txApp, workspaceID)
		if err != nil {
			return errLegalHoldNotFound
		}
		snapshot, err := loadNotionAdminLegalHoldExportSnapshot(
			txApp, current.OrganizationID, legalHoldID, workspaceID,
		)
		if err != nil {
			return err
		}
		payload := notionAdminLegalHoldExportJobPayload{
			FormatVersion:  notionAdminLegalHoldExportPayloadFormat,
			OrganizationID: current.OrganizationID, LegalHoldID: legalHoldID,
			WorkspaceID: workspaceID, RequestingUserID: requestingUserID,
			CustodianIDs:          snapshot.CustodianIDs,
			HoldAuthorityRevision: snapshot.AuthorityRevision,
			WorkspaceRevision:     workspaceRevision,
			Phase:                 notionAdminLegalHoldExportPhaseCapture,
		}
		payload.RequestHash, err = notionAdminLegalHoldExportRequestHash(payload)
		if err != nil {
			return err
		}
		exportID = deriveNotionAdminLegalHoldExportID(payload.RequestHash)
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		now := leafen.DurableJobClockNow(r.Clock()).UTC()
		requestID := notionAdminLegalHoldExportJobKind + ":" + exportID
		result, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
				 [[status]], [[error_code]], [[diagnostic_reference]], [[not_before]], [[created]], [[updated]],
				 [[payload_json]], [[cursor]], [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]],
				 [[chunk_count]], [[captured_bytes]])
			VALUES
				({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind}, '',
				 'queued', '', '', {:notBefore}, {:now}, {:now},
				 {:payload}, '', 0, 0, 0, {:maxAttempts}, 0, 0)
			ON CONFLICT ([[owner]], [[request_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": exportID, "owner": requestingUserID, "workspaceId": workspaceID,
			"requestId": requestID, "requestHash": payload.RequestHash,
			"kind":      notionAdminLegalHoldExportJobKind,
			"notBefore": now.Add(leafen.DurableJobReadyDelay).UnixMilli(), "now": now.UnixMilli(),
			"payload": string(payloadJSON), "maxAttempts": leafen.DefaultDurableJobMaxAttempts,
		}).Execute()
		if err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		if inserted == 0 {
			existing, err := leafen.LoadStoredJob(txApp, exportID, requestingUserID)
			if err != nil {
				return errors.Join(leafen.ErrLegalHoldUnavailable, err)
			}
			stored, err := validNotionAdminLegalHoldExportJob(existing)
			if err != nil || stored.RequestHash != payload.RequestHash {
				return leafen.ErrLegalHoldUnavailable
			}
			// The same snapshot was already asked for and audited; a replay or
			// an overlapping request joins that export without a second event.
			return nil
		}
		if err := leafen.RecordOrganizationAudit(
			txApp, current.OrganizationID, current.CreatedBy,
			"notion_admin.legal_hold.export", "legal_hold_export", exportID,
		); err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		return nil
	})
	if err != nil {
		return "", notionAdminLegalHoldExportAdmissionError(err)
	}
	r.Notify()
	return exportID, nil
}

func readNotionAdminLegalHoldExport(
	ctx context.Context,
	app core.App,
	secret string,
	expected leafen.NotionAdminIdentity,
	legalHoldID, exportID, requestingUserID string,
) error {
	return app.RunInTransaction(func(txApp core.App) error {
		current, err := leafen.AuthenticateNotionAdminToken(ctx, txApp, secret, time.Now().UTC())
		if err != nil {
			return err
		}
		if !sameNotionAdminLegalHoldExportIdentity(current, expected) ||
			!current.HasCapability(leafen.NotionAdminCapabilityLegalHoldExport) {
			return errNotionAdminLegalHoldForbidden
		}
		if !current.CanAccessLegalHold(legalHoldID) {
			return errLegalHoldNotFound
		}
		if _, err := leafen.AuthorizeOrganization(
			txApp, requestingUserID, current.OrganizationID, leafen.PermissionOrganizationActive,
		); err != nil {
			return errLegalHoldNotFound
		}
		job, err := leafen.LoadStoredJob(txApp, exportID, requestingUserID)
		if errors.Is(err, sql.ErrNoRows) {
			return errLegalHoldNotFound
		}
		if err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		payload, err := validNotionAdminLegalHoldExportJob(job)
		if err != nil {
			return err
		}
		if payload.OrganizationID != current.OrganizationID ||
			payload.LegalHoldID != legalHoldID ||
			payload.RequestingUserID != requestingUserID ||
			!current.CanAccessWorkspace(payload.WorkspaceID) {
			return errLegalHoldNotFound
		}
		return nil
	})
}

type notionAdminLegalHoldExportSnapshot struct {
	AuthorityRevision int64
	CustodianIDs      []string
}

func loadNotionAdminLegalHoldExportSnapshot(
	app core.App,
	organizationID, legalHoldID, workspaceID string,
) (notionAdminLegalHoldExportSnapshot, error) {
	authority := notionAdminLegalHoldExportAuthority{}
	if err := app.DB().NewQuery(`
		SELECT [[hold]].[[status]], [[hold]].[[notion_admin_version]],
			[[hold]].[[created_at]], [[policy]].[[version]] AS [[policy_revision]]
		FROM {{leafen_organization_legal_holds}} AS [[hold]]
		JOIN {{leafen_organization_policy_state}} AS [[policy]]
			ON [[policy]].[[organization_id]] = [[hold]].[[organization_id]]
		JOIN {{leafen_workspaces}} AS [[workspace]]
			ON [[workspace]].[[id]] = {:workspaceId}
			AND [[workspace]].[[organization]] = [[hold]].[[organization_id]]
		WHERE [[hold]].[[organization_id]] = {:organizationId}
			AND [[hold]].[[id]] = {:legalHoldId}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "legalHoldId": legalHoldID,
		"workspaceId": workspaceID,
	}).One(&authority); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notionAdminLegalHoldExportSnapshot{}, errLegalHoldNotFound
		}
		return notionAdminLegalHoldExportSnapshot{}, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if authority.Status != leafen.LegalHoldStatusActive || authority.PolicyRevision < 0 {
		return notionAdminLegalHoldExportSnapshot{}, errLegalHoldNotFound
	}
	scopes := []notionAdminLegalHoldExportScope{}
	if err := app.DB().NewQuery(`
		SELECT [[kind]], [[target_id]]
		FROM {{leafen_organization_legal_hold_scopes}}
		WHERE [[organization_id]] = {:organizationId} AND [[hold_id]] = {:legalHoldId}
		ORDER BY [[kind]], [[target_id]]
	`).Bind(dbx.Params{
		"organizationId": organizationID, "legalHoldId": legalHoldID,
	}).All(&scopes); err != nil {
		return notionAdminLegalHoldExportSnapshot{}, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if len(scopes) == 0 {
		return notionAdminLegalHoldExportSnapshot{}, leafen.ErrLegalHoldUnavailable
	}
	covered := authority.NotionAdminVersion == leafen.NotionAdminLegalHoldMetadataVersion
	custodians := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if scope.Kind == leafen.LegalHoldScopeWorkspace && scope.TargetID == workspaceID {
			covered = true
		}
		if scope.Kind == leafen.LegalHoldScopeCustodian {
			custodians = append(custodians, scope.TargetID)
		}
	}
	if !covered {
		return notionAdminLegalHoldExportSnapshot{}, errLegalHoldNotFound
	}
	sort.Strings(custodians)
	return notionAdminLegalHoldExportSnapshot{
		AuthorityRevision: authority.PolicyRevision,
		CustodianIDs:      custodians,
	}, nil
}

func notionAdminLegalHoldExportRequestHash(
	payload notionAdminLegalHoldExportJobPayload,
) (string, error) {
	payload.RequestHash = ""
	payload.Phase = ""
	payload.AfterKind = ""
	payload.AfterID = ""
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func deriveNotionAdminLegalHoldExportID(requestHash string) string {
	digest := sha256.Sum256([]byte("leafen:notion-admin-legal-hold-export:v1\x00" + requestHash))
	id := append([]byte(nil), digest[:16]...)
	id[6] = (id[6] & 0x0f) | 0x80
	id[8] = (id[8] & 0x3f) | 0x80
	return hex.EncodeToString(id)
}

func validNotionAdminLegalHoldExportJob(
	job leafen.StoredJob,
) (notionAdminLegalHoldExportJobPayload, error) {
	payload := notionAdminLegalHoldExportJobPayload{}
	if job.Kind != notionAdminLegalHoldExportJobKind ||
		json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil ||
		payload.FormatVersion != notionAdminLegalHoldExportPayloadFormat ||
		payload.OrganizationID == "" || payload.LegalHoldID == "" ||
		payload.WorkspaceID == "" || payload.RequestingUserID == "" ||
		payload.RequestingUserID != job.Owner || payload.WorkspaceID != job.WorkspaceID ||
		payload.RequestHash != job.RequestHash || len(payload.RequestHash) != 64 ||
		job.ID != deriveNotionAdminLegalHoldExportID(payload.RequestHash) ||
		job.RequestID != notionAdminLegalHoldExportJobKind+":"+job.ID ||
		payload.HoldAuthorityRevision < 0 || payload.WorkspaceRevision < 0 ||
		(payload.Phase != notionAdminLegalHoldExportPhaseCapture &&
			payload.Phase != notionAdminLegalHoldExportPhaseComplete) ||
		(payload.AfterKind == "") != (payload.AfterID == "") {
		return notionAdminLegalHoldExportJobPayload{}, leafen.ErrLegalHoldUnavailable
	}
	for index, id := range payload.CustodianIDs {
		if id == "" || index > 0 && payload.CustodianIDs[index-1] >= id {
			return notionAdminLegalHoldExportJobPayload{}, leafen.ErrLegalHoldUnavailable
		}
	}
	return payload, nil
}

func sameNotionAdminLegalHoldExportIdentity(
	current, expected leafen.NotionAdminIdentity,
) bool {
	return current.OrganizationID == expected.OrganizationID &&
		current.TokenID == expected.TokenID && current.CreatedBy == expected.CreatedBy
}

func notionAdminLegalHoldExportAdmissionError(err error) error {
	if errors.Is(err, leafen.ErrNotionAdminTokenUnauthorized) ||
		errors.Is(err, errNotionAdminLegalHoldForbidden) ||
		errors.Is(err, errLegalHoldNotFound) || errors.Is(err, errLegalHoldInvalid) {
		return err
	}
	return errors.Join(leafen.ErrLegalHoldUnavailable, err)
}
