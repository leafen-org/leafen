package organization

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	leafen "leafen.org/leafen/internal/app"
)

// Notion's "워크스페이스 생성" in Identity → domain management (its help,
// read 2026-10-02; the row, seen 2026-10-02 behind its upgrade, lets an
// organization "사용자별 새 워크스페이스 생성 가능 여부 사용자 지정하기"): once
// an organization verifies a domain, the people with emails on it create new
// workspaces only if it lets them; otherwise only its owners and admins
// may, the default. Leafen keeps the choice in the organization's workspace
// creation policy. A server administrator may always create workspaces.

// authorizeDomainWorkspaceCreation refuses someone a new workspace when an
// organization that verified their email's domain keeps workspace creation
// to its owners and admins (members with an admin role) and they are neither.
func domainWorkspaceCreation(app core.App, user *core.Record) error {
	administrator, err := leafen.IsAdministrator(app, user.Id)
	if err != nil || administrator {
		return err
	}
	var restricted int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_organization_domains}} [[domain]]
		JOIN {{leafen_organization_policy_state}} [[policy]] ON [[policy]].[[organization_id]] = [[domain]].[[organization_id]]
		WHERE [[domain]].[[status]] = 'verified' AND [[domain]].[[domain]] IN (` + leafen.ManagedAccountDomainSQL("{:userId}") + `)
			AND [[policy]].[[workspace_creation_policy]] = 'owners_admins'
			AND NOT EXISTS (SELECT 1 FROM {{leafen_organization_memberships}} [[membership]]
				WHERE [[membership]].[[organization]] = [[domain]].[[organization_id]] AND [[membership]].[[user]] = {:userId}
					AND [[membership]].[[status]] = {:active}
					AND ([[membership]].[[role]] IN ({:owner}, {:admin}, {:securityAdmin}, {:billingAdmin})
						OR ([[membership]].[[role]] = {:member} AND EXISTS (
							SELECT 1 FROM {{leafen_organization_admin_roles}} [[admin]]
							WHERE [[admin]].[[organization_id]] = [[membership]].[[organization]]
								AND [[admin]].[[user_id]] = [[membership]].[[user]]))))
	`).Bind(dbx.Params{
		"userId": user.Id, "active": leafen.OrganizationMemberStatusActive,
		"owner": leafen.OrganizationRoleOwner, "admin": leafen.OrganizationRoleAdmin,
		"securityAdmin": leafen.OrganizationRoleSecurityAdmin, "billingAdmin": leafen.OrganizationRoleBillingAdmin,
		"member": leafen.OrganizationRoleMember,
	}).Row(&restricted); err != nil {
		return err
	}
	if restricted > 0 {
		return leafen.ErrWorkspaceCreationRestricted
	}
	return nil
}
