package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

type storedOrganizationSettingsSummary struct {
	Name                    string `db:"name"`
	OwnerID                 string `db:"owner_id"`
	Revision                int    `db:"revision"`
	WorkspaceCreationPolicy string `db:"workspace_creation_policy"`
}

type storedOrganizationSettingsMember struct {
	ID          string `db:"id"`
	UserID      string `db:"user_id"`
	DisplayName string `db:"display_name"`
	Email       string `db:"email"`
	Avatar      string `db:"avatar"`
	Role        string `db:"role"`
	Status      string `db:"status"`
	CreatedAt   string `db:"created_at"`
	UpdatedAt   string `db:"updated_at"`
	OwnsContent int    `db:"owns_content"`
}

func registerOrganizationSettingsRoutes(event *core.ServeEvent) {
	event.Router.Route(
		organizationSettingsSnapshotMethod, apiRoot+organizationSettingsSnapshotPath,
		organizationSettingsSnapshot,
	)
	event.Router.Route(
		organizationSettingsMutationMethod, apiRoot+organizationSettingsMutationPath,
		mutateOrganizationSettings,
	).Bind(apis.BodyLimit(organizationSettingsMutationBodyBytes))
}

func organizationSettingsSnapshot(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	if organizationID == "" || utf8.RuneCountInString(organizationID) > organizationIdentifierMaxRunes {
		return organizationSettingsError(event, errOrganizationSettingsInvalid, "snapshot-validate")
	}
	cursor, err := decodeOrganizationSettingsCursor(
		event.Request.URL.Query().Get("cursor"), user.Id, organizationID,
	)
	if err != nil {
		return organizationSettingsError(event, err, "snapshot-cursor")
	}
	response := organizationSettingsSnapshotResponse{}
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		authorization, err := authorizeOrganization(
			txApp, user.Id, organizationID, permissionOrganizationAdmin,
		)
		if err != nil {
			return err
		}
		summary, err := readOrganizationSettingsSummary(txApp, authorization)
		if err != nil {
			return err
		}
		if cursor.Revision == 0 && strings.TrimSpace(event.Request.URL.Query().Get("cursor")) == "" {
			cursor.Revision = summary.Revision
		} else if cursor.Revision != summary.Revision {
			return errors.New(errorCodeOrganizationSettingsConflict)
		}
		response.Organization = summary
		// Each admin role sees only the parts of the settings it covers.
		if !authorization.allows(permissionOrganizationPeopleAdmin) {
			cursor.MembersDone, cursor.GroupsDone = true, true
		}
		if !authorization.allows(permissionOrganizationIdentityAdmin) {
			cursor.DomainsDone = true
		}
		if !cursor.MembersDone {
			response.Members, cursor.MemberAfter, cursor.MembersDone, err =
				loadOrganizationSettingsMembers(txApp, authorization, cursor.MemberAfter)
			if err != nil {
				return err
			}
		} else {
			response.Members = []organizationSettingsMember{}
		}
		if !cursor.GroupsDone {
			response.Groups, cursor.GroupAuthority, cursor.GroupAfter,
				cursor.GroupMemberAfter, cursor.GroupsDone, err = loadOrganizationSettingsGroups(
				txApp, organizationID, cursor.GroupAuthority, cursor.GroupAfter, cursor.GroupMemberAfter,
			)
			if err != nil {
				return err
			}
		} else {
			response.Groups = []organizationSettingsGroup{}
		}
		if !cursor.DomainsDone {
			response.Domains, cursor.DomainAfter, cursor.DomainsDone, err =
				loadOrganizationSettingsDomains(txApp, organizationID, cursor.DomainAfter)
			if err != nil {
				return err
			}
		} else {
			response.Domains = []organizationSettingsDomain{}
		}
		response.SnapshotIdentity = organizationSettingsSnapshotIdentity(cursor)
		// The audit log is no part of it: the log reads its own pages as it
		// is opened (organization_audit_storage.go), and walking all of it
		// here took hundreds of reads on each open of the settings.
		response.HasMore = !(cursor.MembersDone && cursor.GroupsDone && cursor.DomainsDone)
		if response.HasMore {
			next, err := encodeOrganizationSettingsCursor(cursor)
			if err != nil {
				return err
			}
			response.NextCursor = &next
		}
		return nil
	})
	if err != nil {
		return organizationSettingsError(event, err, "snapshot")
	}
	return event.JSON(http.StatusOK, response)
}

func readOrganizationSettingsSummary(
	app core.App, authorization organizationAuthorization,
) (organizationSettingsSummary, error) {
	row := storedOrganizationSettingsSummary{}
	err := app.DB().NewQuery(`SELECT [[organization]].[[name]], [[organization]].[[owner]] AS [[owner_id]],
		[[policy]].[[settings_revision]] AS [[revision]], [[policy]].[[workspace_creation_policy]]
		FROM {{leafen_organizations}} [[organization]]
		JOIN {{leafen_organization_policy_state}} [[policy]] ON [[policy]].[[organization_id]]=[[organization]].[[id]]
		WHERE [[organization]].[[id]]={:organizationId}`).Bind(dbx.Params{
		"organizationId": authorization.OrganizationID,
	}).One(&row)
	if err != nil {
		return organizationSettingsSummary{}, err
	}
	retention, err := loadOrganizationActivityRetention(app, authorization.OrganizationID)
	if err != nil {
		return organizationSettingsSummary{}, err
	}
	return organizationSettingsSummary{
		OrganizationID: authorization.OrganizationID, Name: row.Name, OwnerID: row.OwnerID,
		Role: authorization.Role, Status: authorization.Status, Revision: row.Revision,
		AdminRoles:              append([]string{}, authorization.AdminRoles...),
		WorkspaceCreationPolicy: row.WorkspaceCreationPolicy,
		FeedRetentionDays:       retention.FeedDays, AnalyticsRetentionDays: retention.AnalyticsDays,
		Capabilities: organizationSettingsCapabilitiesFor(authorization),
	}, nil
}

func loadOrganizationSettingsMembers(
	app core.App, authorization organizationAuthorization, afterID string,
) ([]organizationSettingsMember, string, bool, error) {
	rows := []storedOrganizationSettingsMember{}
	err := app.DB().NewQuery(`SELECT [[membership]].[[id]], [[membership]].[[user]] AS [[user_id]],
		[[user]].[[name]] AS [[display_name]], [[user]].[[email]], [[user]].[[profile_avatar]] AS [[avatar]],
		[[membership]].[[role]], [[membership]].[[status]],
		[[membership]].[[created]] AS [[created_at]], [[membership]].[[updated]] AS [[updated_at]],
		EXISTS(SELECT 1 FROM {{leafen_workspaces}} [[workspace]]
			WHERE [[workspace]].[[organization]]=[[membership]].[[organization]]
				AND [[workspace]].[[workspace_owner]]=[[membership]].[[user]]
				AND [[workspace]].[[deletion_job_id]]='') AS [[owns_content]]
		FROM {{leafen_organization_memberships}} [[membership]]
		JOIN {{users}} [[user]] ON [[user]].[[id]]=[[membership]].[[user]]
		WHERE [[membership]].[[organization]]={:organizationId} AND [[membership]].[[id]]>{:afterId}
			AND NOT ` + integrationAccountSQL("[[membership]].[[user]]") + `
		ORDER BY [[membership]].[[id]] LIMIT {:limit}`).Bind(dbx.Params{
		"organizationId": authorization.OrganizationID, "afterId": afterID,
		"limit": organizationSettingsMemberPageSize + 1,
	}).All(&rows)
	if err != nil {
		return nil, "", false, err
	}
	hasMore := len(rows) > organizationSettingsMemberPageSize
	if hasMore {
		rows = rows[:organizationSettingsMemberPageSize]
	}
	adminRoles, err := loadOrganizationMembersAdminRoles(app, authorization.OrganizationID)
	if err != nil {
		return nil, "", false, err
	}
	members := make([]organizationSettingsMember, 0, len(rows))
	for _, row := range rows {
		member := organizationSettingsMember{
			ID: row.ID, UserID: row.UserID, DisplayName: row.DisplayName, Email: row.Email,
			Avatar: row.Avatar, Role: row.Role, Status: row.Status,
			AdminRoles:                  effectiveOrganizationAdminRoles(row.Role, adminRoles[row.UserID]),
			RequiresContentReassignment: row.OwnsContent == 1,
			CreatedAt:                   row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		applyOrganizationSettingsMemberCapabilities(&member, authorization)
		members = append(members, member)
	}
	if !hasMore || len(rows) == 0 {
		return members, "", true, nil
	}
	return members, rows[len(rows)-1].ID, false, nil
}

func organizationSettingsSnapshotIdentity(cursor organizationSettingsCursor) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", cursor.OrganizationID, cursor.Revision)))
	return hex.EncodeToString(digest[:])
}

func organizationSettingsError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusInternalServerError,
		errorCodeOrganizationSettingsStateInvalid, "Couldn't process the organization settings."
	switch {
	case isPermissionDenied(err):
		status, code, message = http.StatusNotFound, errorCodeOrganizationNotFound, "Organization not found."
	case errors.Is(err, errOrganizationSettingsInvalid):
		status, code, message = http.StatusBadRequest, errorCodeOrganizationSettingsInvalid,
			refusalMessage("Check the organization settings request.", refusalReason(err))
	case strings.Contains(err.Error(), errorCodeOrganizationSettingsCursorInvalid):
		status, code, message = http.StatusBadRequest, errorCodeOrganizationSettingsCursorInvalid, "The organization settings query position is invalid."
	case strings.Contains(err.Error(), errorCodeOrganizationSettingsRequestIDReused):
		status, code, message = http.StatusConflict, errorCodeOrganizationSettingsRequestIDReused, "The same change ID can't be used for a different request."
	case strings.Contains(err.Error(), errorCodeOrganizationSettingsConflict):
		status, code, message = http.StatusConflict, errorCodeOrganizationSettingsConflict, "The organization settings changed. Refresh, then try again."
	case strings.Contains(err.Error(), errorCodeOrganizationSettingsGroupConflict):
		status, code, message = http.StatusConflict, errorCodeOrganizationSettingsGroupConflict, "A group with the same name already exists."
	case strings.Contains(err.Error(), errorCodeOrganizationSettingsVerificationFailed):
		status, code, message = http.StatusConflict, errorCodeOrganizationSettingsVerificationFailed, "Domain verification record not found."
	}
	return apiErrorWithCause(event, status, code, message, "organization_settings", stage, code, err)
}

// organizationAuditTargetNameSQL names what an audit event touched, as
// Notion's audit log does, for the log and its CSV alike: a person by name —
// by their account, or by their membership of the organization — a group,
// the organization, a workspace, page, row or teamspace by its name or title
// (the organization's own only), a database's data source, view or property
// by its name now (one without a name is ”, as an untitled page), a SCIM
// token or personal access token by its name, an invitation by its address. A target gone since, or one no
// record names, takes the name its line kept from then (metadata's name). It
// is NULL when there is no name to show; a page without a title is ”.
// A group's line — the organization's or a workspace's — names the group as
// it was then, so a line written before a rename reads the same right after
// it and once the log is read again; a line that kept no name takes the
// group's name now. An account's deletion line names the account by the
// email it had then (recordAccountDeletionAudit), and no other line by it:
// a member's line names them by their account now (metadata's userId), and
// one whose account is gone by nothing (organization_audit_gone_people.go).
// A legal hold's line names the hold as it was then, and the SAML lines the
// identity provider's entity ID; an export's line names its file, which its
// line kept (2026-11-13: these read with no target at all).
var organizationAuditTargetNameSQL = `COALESCE((CASE
		WHEN {{leafen_organization_audit_events}}.[[action]] = 'user.deleted'
			THEN NULLIF(json_extract({{leafen_organization_audit_events}}.[[metadata_json]], '$.email'), '')
		WHEN {{leafen_organization_audit_events}}.[[action]] LIKE 'organization\_settings.group\_%' ESCAPE '\'
			THEN COALESCE(NULLIF(json_extract({{leafen_organization_audit_events}}.[[metadata_json]], '$.name'), ''),
				(SELECT [[org_group]].[[name]] FROM {{leafen_organization_groups}} [[org_group]]
					WHERE [[org_group]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
						AND [[org_group]].[[organization_id]] = {{leafen_organization_audit_events}}.[[organization_id]]))
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] IN ('permission_group', 'permission_group_member')
			THEN COALESCE(NULLIF(json_extract({{leafen_organization_audit_events}}.[[metadata_json]], '$.name'), ''),
				(SELECT [[space_group]].[[name]] FROM {{leafen_workspace_permission_groups}} [[space_group]]
					JOIN {{leafen_workspaces}} [[space]] ON [[space]].[[id]] = [[space_group]].[[workspace_id]]
					WHERE [[space_group]].[[id]] = (CASE WHEN instr({{leafen_organization_audit_events}}.[[subject_id]], ':') > 0
							THEN substr({{leafen_organization_audit_events}}.[[subject_id]], 1, instr({{leafen_organization_audit_events}}.[[subject_id]], ':') - 1)
							ELSE {{leafen_organization_audit_events}}.[[subject_id]] END)
						AND [[space]].[[organization]] = {{leafen_organization_audit_events}}.[[organization_id]]))
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] IN ('user', 'workspace_member', 'workspace_owner', 'organization_owner', 'organization_settings', 'organization_membership')
			THEN COALESCE(
				(SELECT ` + accountNameSQL("target") + ` FROM {{users}} [[target]]
					WHERE [[target]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]),
				(SELECT ` + accountNameSQL("member_now") + ` FROM {{users}} [[member_now]]
					WHERE [[member_now]].[[id]] = json_extract({{leafen_organization_audit_events}}.[[metadata_json]], '$.userId')),
				(SELECT ` + accountNameSQL("member_account") + ` FROM {{leafen_organization_memberships}} [[membership]]
					JOIN {{users}} [[member_account]] ON [[member_account]].[[id]] = [[membership]].[[user]]
					WHERE [[membership]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
						AND [[membership]].[[organization]] = {{leafen_organization_audit_events}}.[[organization_id]]),
				(SELECT [[org_group]].[[name]] FROM {{leafen_organization_groups}} [[org_group]]
					WHERE [[org_group]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
						AND [[org_group]].[[organization_id]] = {{leafen_organization_audit_events}}.[[organization_id]]))
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] = 'scim_group'
			THEN (SELECT [[scim_group]].[[display_name]] FROM {{leafen_scim_groups}} [[scim_group]]
				WHERE [[scim_group]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
					AND [[scim_group]].[[organization_id]] = {{leafen_organization_audit_events}}.[[organization_id]]
					AND [[scim_group]].[[deleted_at]] = 0)
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] = 'organization'
			THEN (SELECT [[org]].[[name]] FROM {{leafen_organizations}} [[org]]
				WHERE [[org]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
					AND [[org]].[[id]] = {{leafen_organization_audit_events}}.[[organization_id]])
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] = 'scim_token'
			THEN (SELECT [[scim_token]].[[label]] FROM {{leafen_scim_tokens}} [[scim_token]]
				WHERE [[scim_token]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
					AND [[scim_token]].[[organization_id]] = {{leafen_organization_audit_events}}.[[organization_id]])
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] = 'personal_access_token'
			THEN (SELECT [[key]].[[name]] FROM {{leafen_notion_personal_access_tokens}} [[key]]
				JOIN {{leafen_workspaces}} [[space]] ON [[space]].[[id]] = [[key]].[[workspace_id]]
				WHERE [[key]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
					AND [[space]].[[organization]] = {{leafen_organization_audit_events}}.[[organization_id]])
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] = 'workspace'
			THEN (SELECT [[space]].[[name]] FROM {{leafen_workspaces}} [[space]]
				WHERE [[space]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
					AND [[space]].[[organization]] = {{leafen_organization_audit_events}}.[[organization_id]])
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] IN ('page', 'row')
			THEN (SELECT [[page]].[[title]] FROM {{leafen_pages}} [[page]]
				JOIN {{leafen_workspaces}} [[space]] ON [[space]].[[id]] = [[page]].[[workspace]]
				WHERE [[page]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
					AND [[space]].[[organization]] = {{leafen_organization_audit_events}}.[[organization_id]])
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] = 'dataSource'
			THEN (SELECT [[source]].[[title]] FROM {{leafen_data_sources}} [[source]]
				JOIN {{leafen_workspaces}} [[space]] ON [[space]].[[id]] = [[source]].[[workspace_id]]
				WHERE [[source]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
					AND [[space]].[[organization]] = {{leafen_organization_audit_events}}.[[organization_id]])
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] = 'view'
			THEN (SELECT [[view]].[[name]] FROM {{leafen_database_views}} [[view]]
				JOIN {{leafen_workspaces}} [[space]] ON [[space]].[[id]] = [[view]].[[workspace_id]]
				WHERE [[view]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
					AND [[space]].[[organization]] = {{leafen_organization_audit_events}}.[[organization_id]])
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] = 'property'
			THEN (SELECT [[property]].[[name]] FROM {{leafen_database_properties}} [[property]]
				JOIN {{leafen_workspaces}} [[space]] ON [[space]].[[id]] = [[property]].[[workspace_id]]
				WHERE [[property]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
					AND [[property]].[[data_source_id]] = json_extract({{leafen_organization_audit_events}}.[[metadata_json]], '$.sourceId')
					AND [[space]].[[organization]] = {{leafen_organization_audit_events}}.[[organization_id]])
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] = 'teamspace'
			THEN (SELECT [[team]].[[name]] FROM {{leafen_teamspaces}} [[team]]
				JOIN {{leafen_workspaces}} [[space]] ON [[space]].[[id]] = [[team]].[[workspace_id]]
				WHERE [[team]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
					AND [[space]].[[organization]] = {{leafen_organization_audit_events}}.[[organization_id]])
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] = 'workspace_invitation'
			THEN (SELECT [[invite]].[[email]] FROM {{leafen_workspace_invitations}} [[invite]]
				WHERE [[invite]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]])
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] = 'legal_hold'
			THEN COALESCE(NULLIF(json_extract({{leafen_organization_audit_events}}.[[metadata_json]], '$.name'), ''),
				(SELECT [[hold]].[[name]] FROM {{leafen_organization_legal_holds}} [[hold]]
					WHERE [[hold]].[[id]] = {{leafen_organization_audit_events}}.[[subject_id]]
						AND [[hold]].[[organization_id]] = {{leafen_organization_audit_events}}.[[organization_id]]))
		WHEN {{leafen_organization_audit_events}}.[[subject_type]] = 'saml_provider'
			THEN COALESCE(NULLIF(json_extract({{leafen_organization_audit_events}}.[[metadata_json]], '$.name'), ''),
				(SELECT [[saml]].[[issuer]] FROM {{leafen_saml_providers}} [[saml]]
					WHERE [[saml]].[[id]] = (CASE WHEN instr({{leafen_organization_audit_events}}.[[subject_id]], ':') > 0
							THEN substr({{leafen_organization_audit_events}}.[[subject_id]], 1, instr({{leafen_organization_audit_events}}.[[subject_id]], ':') - 1)
							ELSE {{leafen_organization_audit_events}}.[[subject_id]] END)
						AND [[saml]].[[organization_id]] = {{leafen_organization_audit_events}}.[[organization_id]]))
		ELSE NULL END),
	NULLIF(json_extract({{leafen_organization_audit_events}}.[[metadata_json]], '$.name'), ''))`

func organizationAuditTargetName(name sql.NullString) *string {
	if !name.Valid {
		return nil
	}
	return &name.String
}
