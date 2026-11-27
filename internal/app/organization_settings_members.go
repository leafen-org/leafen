package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// errOrganizationMemberOwnsWorkspaces is a member removed or made a guest
// who owns workspaces of the organization still.
var errOrganizationMemberOwnsWorkspaces = organizationSettingsRefused(
	"the member owns workspaces of the organization: pass them to someone else first")

func mutateOrganizationMember(
	app core.App, authorization organizationAuthorization,
	input organizationSettingsMutationInput,
) ([]organizationSettingsMember, string, []string, error) {
	target, err := app.FindRecordById(organizationMembershipsCollection, strings.TrimSpace(input.TargetID))
	if err != nil || target.GetString("organization") != authorization.OrganizationID {
		return nil, "", nil, errors.Join(organizationSettingsRefused("targetId names no member of this organization; its settings list the members"), err)
	}
	targetRole := target.GetString("role")
	targetStatus := target.GetString("status")
	if !validOrganizationRole(targetRole) ||
		(targetRole == organizationRoleOwner) != (target.GetString("user") == authorization.OwnerID) {
		return nil, "", nil, errors.New(errorCodeOrganizationSettingsStateInvalid)
	}
	targetAdminRoles, err := loadOrganizationMemberAdminRoles(app, authorization.OrganizationID, target.GetString("user"), targetRole)
	if err != nil {
		return nil, "", nil, err
	}
	if input.Action != organizationSettingsActionOwnerTransfer &&
		(target.GetString("user") == authorization.PrincipalID ||
			!organizationSettingsCanManageTarget(authorization, targetRole, targetAdminRoles)) {
		return nil, "", nil, permissionDenied()
	}
	var revocations []string
	switch input.Action {
	case organizationSettingsActionMemberDeactivate:
		// The durable deactivated state remains readable and can be explicitly reactivated.
		if targetStatus != organizationMemberStatusDeactivated {
			target.Set("status", organizationMemberStatusDeactivated)
			if err := app.Save(target); err != nil {
				return nil, "", nil, err
			}
		}
	case organizationSettingsActionMemberReactivate:
		if targetStatus != organizationMemberStatusActive {
			target.Set("status", organizationMemberStatusActive)
			if err := app.Save(target); err != nil {
				return nil, "", nil, err
			}
		}
	case organizationSettingsActionMemberRole:
		role := strings.ToLower(strings.TrimSpace(input.Value))
		if !validOrganizationSettingsAssignableRole(role) {
			return nil, "", nil, organizationSettingsRefused("value is the role to give, %s or %s", organizationRoleMember, organizationRoleGuest)
		}
		if targetStatus != organizationMemberStatusActive {
			return nil, "", nil, organizationSettingsRefused("the member is deactivated: reactivate them before changing their role")
		}
		if targetRole != role {
			if role == organizationRoleGuest {
				if revocations, err = demoteOrganizationMemberToGuest(app, authorization, target.GetString("user")); err != nil {
					return nil, "", nil, err
				}
			}
			target.Set("role", role)
			if err := app.Save(target); err != nil {
				return nil, "", nil, err
			}
		}
	case organizationSettingsActionMemberRemove:
		var owned int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspaces}}
			WHERE [[organization]]={:organizationId} AND [[workspace_owner]]={:userId} AND [[deletion_job_id]]=''`).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "userId": target.GetString("user"),
		}).Row(&owned); err != nil {
			return nil, "", nil, err
		}
		if owned > 0 {
			// Workspace ownership is its own durable lifecycle. Complete those bounded transfers first.
			return nil, "", nil, errOrganizationMemberOwnsWorkspaces
		}
		if revocations, err = clearOrganizationPlaces(app, authorization, target.GetString("user")); err != nil {
			return nil, "", nil, err
		}
		if err := app.Delete(target); err != nil {
			return nil, "", nil, err
		}
		return []organizationSettingsMember{}, target.Id, revocations, nil
	case organizationSettingsActionOwnerTransfer:
		if authorization.Role != organizationRoleOwner || targetRole == organizationRoleOwner ||
			targetStatus != organizationMemberStatusActive || target.GetString("user") == authorization.PrincipalID {
			return nil, "", nil, organizationSettingsRefused("the organization passes from its owner to another active member")
		}
		previous, err := app.FindFirstRecordByFilter(
			organizationMembershipsCollection,
			"organization = {:organization} && user = {:user}",
			dbx.Params{"organization": authorization.OrganizationID, "user": authorization.PrincipalID},
		)
		if err != nil {
			return nil, "", nil, err
		}
		// The previous owner stays a member who manages people, as the admin
		// they became before admin roles; the new owner needs no admin role.
		previous.Set("role", organizationRoleMember)
		target.Set("role", organizationRoleOwner)
		if err := app.Save(previous); err != nil {
			return nil, "", nil, err
		}
		if err := addOrganizationAdminRole(
			app, authorization.OrganizationID, authorization.PrincipalID, organizationAdminRolePeople, authorization.PrincipalID,
		); err != nil {
			return nil, "", nil, err
		}
		if err := app.Save(target); err != nil {
			return nil, "", nil, err
		}
		organization, err := app.FindRecordById(organizationsCollection, authorization.OrganizationID)
		if err != nil {
			return nil, "", nil, err
		}
		organization.Set("owner", target.GetString("user"))
		if err := app.Save(organization); err != nil {
			return nil, "", nil, err
		}
	case organizationSettingsActionAdminRoleAdd, organizationSettingsActionAdminRoleRemove:
		// The owner gives admin roles to active members, never to a guest —
		// with the organization module, which keeps them.
		role := strings.ToLower(strings.TrimSpace(input.Value))
		if organizationPolicy.AdminRoles == nil || authorization.Role != organizationRoleOwner || !validOrganizationAdminRole(role) ||
			targetRole != organizationRoleMember || targetStatus != organizationMemberStatusActive {
			return nil, "", nil, organizationSettingsRefused("the owner gives an admin role (%s) to an active member, never a guest",
				strings.Join(organizationAdminRoles, ", "))
		}
		if input.Action == organizationSettingsActionAdminRoleAdd {
			err = addOrganizationAdminRole(app, authorization.OrganizationID, target.GetString("user"), role, authorization.PrincipalID)
		} else {
			err = removeOrganizationAdminRole(app, authorization.OrganizationID, target.GetString("user"), role)
		}
		if err != nil {
			return nil, "", nil, err
		}
	default:
		return nil, "", nil, organizationSettingsRefused("action %q isn't a member change", input.Action)
	}
	members, err := loadOrganizationSettingsMembersByIDs(app, authorization, []string{target.Id})
	return members, "", revocations, err
}

// demoteOrganizationMemberToGuest takes a person turned guest out of the
// organization's workspaces, as a Notion guest is in none: each membership
// goes as a workspace owner's removal of it does, the workspace's rooms
// invalidated by the jobs it returns, while the pages shared with them stay
// shared. Someone who owns one of the workspaces stays a member until it
// passes to someone else.
func demoteOrganizationMemberToGuest(app core.App, authorization organizationAuthorization, userID string) ([]string, error) {
	var owned int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspaces}}
		WHERE [[organization]]={:organizationId} AND [[workspace_owner]]={:userId} AND [[deletion_job_id]]=''`).Bind(dbx.Params{
		"organizationId": authorization.OrganizationID, "userId": userID,
	}).Row(&owned); err != nil {
		return nil, err
	}
	if owned > 0 {
		return nil, errOrganizationMemberOwnsWorkspaces
	}
	workspaceIDs := []string{}
	if err := app.DB().NewQuery(`SELECT [[membership]].[[workspace_id]] FROM {{leafen_workspace_memberships}} AS [[membership]]
		JOIN {{leafen_workspaces}} AS [[workspace]] ON [[workspace]].[[id]]=[[membership]].[[workspace_id]]
		WHERE [[workspace]].[[organization]]={:organizationId} AND [[membership]].[[user_id]]={:userId}
			AND [[membership]].[[role]]={:role}
		ORDER BY [[membership]].[[workspace_id]]`).Bind(dbx.Params{
		"organizationId": authorization.OrganizationID, "userId": userID, "role": workspaceRoleMember,
	}).Column(&workspaceIDs); err != nil {
		return nil, err
	}
	jobs := []string{}
	for _, workspaceID := range workspaceIDs {
		admission, err := revokeWorkspaceMembershipInTransaction(app, authorization.PrincipalID, workspaceID, userID)
		if err != nil {
			return nil, err
		}
		if admission.Job.ID != "" {
			jobs = append(jobs, admission.Job.ID)
		}
	}
	return jobs, nil
}

// validOrganizationSettingsAssignableRole is a member or a guest; the owner
// uses the transfer lifecycle and admin roles their own actions.
func validOrganizationSettingsAssignableRole(role string) bool {
	return role == organizationRoleMember || role == organizationRoleGuest
}

// loadOrganizationMemberAdminRoles is one person's admin roles.
func loadOrganizationMemberAdminRoles(app core.App, organizationID, userID, role string) ([]string, error) {
	assigned, err := loadOrganizationAdminRoles(app, userID, []string{organizationID})
	if err != nil {
		return nil, err
	}
	return effectiveOrganizationAdminRoles(role, assigned[organizationID]), nil
}

func loadOrganizationSettingsMembersByIDs(
	app core.App, authorization organizationAuthorization, ids []string,
) ([]organizationSettingsMember, error) {
	if len(ids) == 0 {
		return []organizationSettingsMember{}, nil
	}
	params := dbx.Params{"organizationId": authorization.OrganizationID}
	placeholders := make([]string, 0, len(ids))
	for index, id := range ids {
		key := "member" + string(rune('a'+index))
		params[key] = id
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []storedOrganizationSettingsMember{}
	err := app.DB().NewQuery(`SELECT [[membership]].[[id]], [[membership]].[[user]] AS [[user_id]],
		[[user]].[[name]] AS [[display_name]], [[user]].[[email]], [[user]].[[profile_avatar]] AS [[avatar]],
		[[membership]].[[role]], [[membership]].[[status]], [[membership]].[[created]] AS [[created_at]],
		[[membership]].[[updated]] AS [[updated_at]], EXISTS(SELECT 1 FROM {{leafen_workspaces}} [[workspace]]
			WHERE [[workspace]].[[organization]]=[[membership]].[[organization]]
				AND [[workspace]].[[workspace_owner]]=[[membership]].[[user]]
				AND [[workspace]].[[deletion_job_id]]='') AS [[owns_content]]
		FROM {{leafen_organization_memberships}} [[membership]]
		JOIN {{users}} [[user]] ON [[user]].[[id]]=[[membership]].[[user]]
		WHERE [[membership]].[[organization]]={:organizationId} AND [[membership]].[[id]] IN (` + joinSQL(placeholders) + `)
		ORDER BY [[membership]].[[id]]`).Bind(params).All(&rows)
	if errors.Is(err, sql.ErrNoRows) {
		return []organizationSettingsMember{}, nil
	}
	if err != nil {
		return nil, err
	}
	adminRoles, err := loadOrganizationMembersAdminRoles(app, authorization.OrganizationID)
	if err != nil {
		return nil, err
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
	return members, nil
}
