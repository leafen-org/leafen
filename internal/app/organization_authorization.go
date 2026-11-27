package app

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type organizationPermissionRequirement int

// An organization's owner may do all of these; a member, what the admin roles
// the owner gave them cover (organization_admin_roles.go), as Notion's admin
// roles do.
const (
	permissionOrganizationActive organizationPermissionRequirement = iota
	// People: members, guests and groups.
	permissionOrganizationPeopleAdmin
	// IT: security (sharing security, DLP).
	permissionOrganizationSecurityAdmin
	// Billing: storage and file usage.
	permissionOrganizationBillingAdmin
	// Any admin role: reading the parts of the settings it covers.
	permissionOrganizationAdmin
	permissionOrganizationOwner
	// IT: identity (SAML, SCIM, domains, workspace creation, managed users).
	permissionOrganizationIdentityAdmin
	// IT: API integrations (MCP governance).
	permissionOrganizationIntegrationsAdmin
	// Compliance: the audit log, its exports and retention.
	permissionOrganizationAuditLog
	// Compliance: content search.
	permissionOrganizationContentSearch
	// Compliance: legal holds.
	permissionOrganizationLegalHolds
	// Analytics: every workspace's analytics.
	permissionOrganizationAnalytics
)

type organizationAuthorization struct {
	OrganizationID string
	OwnerID        string
	PrincipalID    string
	Role           string
	Status         string
	// AdminRoles are the member's admin roles, sorted; none for the owner,
	// who may do everything, or a guest.
	AdminRoles []string
}

type storedOrganizationAuthorization struct {
	OrganizationID string `db:"organization_id"`
	OwnerID        string `db:"owner_id"`
	UserID         string `db:"user_id"`
	Role           string `db:"role"`
	Status         string `db:"status"`
}

func authorizeOrganization(
	app core.App,
	principalID string,
	organizationID string,
	requirement organizationPermissionRequirement,
) (organizationAuthorization, error) {
	return authorizeOrganizationContext(
		context.Background(), app, principalID, organizationID, requirement,
	)
}

func authorizeOrganizationContext(
	ctx context.Context,
	app core.App,
	principalID string,
	organizationID string,
	requirement organizationPermissionRequirement,
) (organizationAuthorization, error) {
	// A person's membership is read once a read scope and each requirement
	// judged against it: a member's page read read it for the workspace and
	// again for the page's own shares (2026-10-17).
	authorization, err := remembered(app, organizationMembershipMemoKey{principalID, organizationID}, func() (organizationAuthorization, error) {
		return readOrganizationMembership(ctx, app, principalID, organizationID)
	})
	if err != nil {
		return organizationAuthorization{}, err
	}
	if !authorization.allows(requirement) {
		return organizationAuthorization{}, permissionDenied()
	}
	return authorization, nil
}

type organizationMembershipMemoKey struct{ principalID, organizationID string }

// readOrganizationMembership is a person's active membership in an
// organization, whatever it allows.
func readOrganizationMembership(
	ctx context.Context,
	app core.App,
	principalID string,
	organizationID string,
) (organizationAuthorization, error) {
	authorizations, err := authorizeOrganizationSetContext(
		ctx, app, principalID, []string{organizationID}, permissionOrganizationActive,
	)
	if err != nil {
		return organizationAuthorization{}, err
	}
	memberships := make([]organizationAuthorization, 0, len(authorizations))
	for _, authorization := range authorizations {
		memberships = append(memberships, authorization)
	}
	if len(memberships) != 1 {
		return organizationAuthorization{}, permissionDenied()
	}
	return memberships[0], nil
}

func authorizeOrganizationSet(
	app core.App,
	principalID string,
	organizationIDs []string,
	requirement organizationPermissionRequirement,
) (map[string]organizationAuthorization, error) {
	return authorizeOrganizationSetContext(
		context.Background(), app, principalID, organizationIDs, requirement,
	)
}

func authorizeOrganizationSetContext(
	ctx context.Context,
	app core.App,
	principalID string,
	organizationIDs []string,
	requirement organizationPermissionRequirement,
) (map[string]organizationAuthorization, error) {
	if principalID == "" || len(organizationIDs) == 0 {
		return nil, permissionDenied()
	}
	unique := make(map[string]struct{}, len(organizationIDs))
	ids := make([]string, 0, len(organizationIDs))
	for _, organizationID := range organizationIDs {
		if organizationID == "" {
			return nil, permissionDenied()
		}
		if _, exists := unique[organizationID]; exists {
			continue
		}
		unique[organizationID] = struct{}{}
		ids = append(ids, organizationID)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{"principalId": principalID, "organizationIds": string(encoded)}
	rows := []storedOrganizationAuthorization{}
	err = app.DB().NewQuery(`
		SELECT [[organization]].[[id]] AS [[organization_id]],
			[[organization]].[[owner]] AS [[owner_id]],
			COALESCE([[membership]].[[user]], '') AS [[user_id]],
			COALESCE([[membership]].[[role]], '') AS [[role]],
			COALESCE([[membership]].[[status]], '') AS [[status]]
		FROM {{leafen_organizations}} [[organization]]
		LEFT JOIN {{leafen_organization_memberships}} [[membership]]
			ON [[membership]].[[organization]] = [[organization]].[[id]]
			AND [[membership]].[[user]] = {:principalId}
		WHERE [[organization]].[[id]] IN (SELECT [[value]] FROM json_each({:organizationIds}))
		ORDER BY [[organization]].[[id]], [[membership]].[[id]]
	`).WithContext(ctx).Bind(params).All(&rows)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(unique) {
		return nil, permissionDenied()
	}
	adminRoles, err := loadOrganizationAdminRoles(app, principalID, ids)
	if err != nil {
		return nil, err
	}
	result := make(map[string]organizationAuthorization, len(rows))
	for _, row := range rows {
		if _, duplicate := result[row.OrganizationID]; duplicate {
			return nil, permissionDenied()
		}
		authorization, err := validateOrganizationMembership(row, principalID, requirement, adminRoles[row.OrganizationID])
		if err != nil {
			return nil, err
		}
		result[row.OrganizationID] = authorization
	}
	return result, nil
}

func validateOrganizationMembership(
	row storedOrganizationAuthorization,
	principalID string,
	requirement organizationPermissionRequirement,
	assigned []string,
) (organizationAuthorization, error) {
	if row.OrganizationID == "" || row.OwnerID == "" || row.UserID != principalID ||
		row.Status != organizationMemberStatusActive {
		return organizationAuthorization{}, permissionDenied()
	}
	if !validOrganizationRole(row.Role) ||
		(row.OwnerID == principalID) != (row.Role == organizationRoleOwner) {
		return organizationAuthorization{}, permissionDenied()
	}
	authorization := organizationAuthorization{
		OrganizationID: row.OrganizationID,
		OwnerID:        row.OwnerID,
		PrincipalID:    principalID,
		Role:           row.Role,
		Status:         row.Status,
		AdminRoles:     effectiveOrganizationAdminRoles(row.Role, assigned),
	}
	if !authorization.allows(requirement) {
		return organizationAuthorization{}, permissionDenied()
	}
	return authorization, nil
}

func validOrganizationRole(role string) bool {
	switch role {
	case organizationRoleOwner, organizationRoleAdmin, organizationRoleSecurityAdmin,
		organizationRoleBillingAdmin, organizationRoleMember, organizationRoleGuest:
		return true
	default:
		return false
	}
}

func (authorization organizationAuthorization) allows(requirement organizationPermissionRequirement) bool {
	if !validOrganizationRole(authorization.Role) {
		return false
	}
	if authorization.Role == organizationRoleOwner || requirement == permissionOrganizationActive {
		return true
	}
	has := func(role string) bool { return slices.Contains(authorization.AdminRoles, role) }
	switch requirement {
	case permissionOrganizationPeopleAdmin:
		return has(organizationAdminRolePeople)
	case permissionOrganizationSecurityAdmin, permissionOrganizationIdentityAdmin, permissionOrganizationIntegrationsAdmin:
		return has(organizationAdminRoleIT)
	case permissionOrganizationBillingAdmin:
		return has(organizationAdminRoleBilling)
	case permissionOrganizationAuditLog, permissionOrganizationContentSearch, permissionOrganizationLegalHolds:
		return has(organizationAdminRoleCompliance)
	case permissionOrganizationAnalytics:
		return has(organizationAdminRoleAnalytics)
	case permissionOrganizationAdmin:
		return len(authorization.AdminRoles) > 0
	default:
		return false
	}
}
