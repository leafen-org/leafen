package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Someone removed from an organization takes none of their places in it
// along, to come back if they joined it again: their memberships of its
// workspaces go as a workspace owner's removal does, and the pages shared
// with them or an address they proved, their teamspaces,
// permission groups, property access, admin roles, agent access and their
// pending requests go with them — as Notion's removal of a guest takes the
// pages shared with them. Shared with afresh, they open only that. A
// deactivation keeps it all for a reactivation. A teamspace they alone own
// keeps them as its owner, as a teamspace's last owner can't be removed.

const organizationWorkspacesSQL = `[[workspace_id]] IN (SELECT [[id]] FROM {{leafen_workspaces}} WHERE [[organization]] = {:organization})`

// clearOrganizationPlaces takes a person's places in an organization's
// workspaces as they are removed from it, and returns the jobs that drop
// the live rooms of the workspaces they were a member of.
func clearOrganizationPlaces(txApp core.App, authorization organizationAuthorization, userID string) ([]string, error) {
	jobs, err := demoteOrganizationMemberToGuest(txApp, authorization, userID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	params := dbx.Params{"organization": authorization.OrganizationID, "user": userID, "now": now}
	for _, statement := range []string{
		`DELETE FROM {{leafen_page_permissions}} WHERE ` + organizationWorkspacesSQL + ` AND ` + pageShareNamesAccountSQL("user"),
		`DELETE FROM {{leafen_page_share_access_receipts}} WHERE ` + organizationWorkspacesSQL + ` AND [[principal_id]] = {:user}`,
		`DELETE FROM {{leafen_page_share_access_pending}} WHERE ` + organizationWorkspacesSQL + ` AND [[principal_id]] = {:user}`,
		`DELETE FROM {{leafen_workspace_restricted_members}} WHERE ` + organizationWorkspacesSQL + ` AND [[user_id]] = {:user}`,
		`DELETE FROM {{leafen_database_property_access_exceptions}} WHERE ` + organizationWorkspacesSQL + ` AND [[principal_id]] = {:user}`,
		`DELETE FROM {{leafen_access_requests}} WHERE ` + organizationWorkspacesSQL + ` AND [[requester_id]] = {:user} AND [[status]] = 'pending'`,
		`DELETE FROM {{leafen_workspace_permission_group_members}} WHERE [[user_id]] = {:user}
			AND [[group_id]] IN (SELECT [[id]] FROM {{leafen_workspace_permission_groups}} WHERE ` + organizationWorkspacesSQL + `)`,
		`UPDATE {{leafen_teamspace_members}} AS [[member]] SET [[deleted_at]] = {:now}, [[revision]] = [[revision]] + 1, [[updated_at]] = {:now}
			WHERE [[member]].` + organizationWorkspacesSQL + ` AND [[member]].[[principal_type]] = 'user'
				AND [[member]].[[principal_id]] = {:user} AND [[member]].[[deleted_at]] = 0
				AND ([[member]].[[role]] != 'owner' OR (SELECT COUNT(*) FROM {{leafen_teamspace_members}} [[owner]]
					WHERE [[owner]].[[teamspace_id]] = [[member]].[[teamspace_id]] AND [[owner]].[[role]] = 'owner'
						AND [[owner]].[[deleted_at]] = 0) > 1)`,
		`DELETE FROM {{leafen_organization_admin_roles}} WHERE [[organization_id]] = {:organization} AND [[user_id]] = {:user}`,
		`DELETE FROM {{leafen_mcp_enterprise_member_access}} WHERE [[organization_id]] = {:organization} AND [[user_id]] = {:user}`,
	} {
		if _, err := txApp.DB().NewQuery(statement).Bind(params).Execute(); err != nil {
			return nil, err
		}
	}
	return jobs, nil
}
