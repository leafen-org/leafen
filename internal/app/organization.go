package app

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/net/idna"
)

type storedOrganizationListRow struct {
	OrganizationID string `db:"organization_id"`
	Created        string `db:"created"`
	Name           string `db:"name"`
	UserID         string `db:"user_id"`
	Role           string `db:"role"`
	Status         string `db:"status"`
	PolicyVersion  int    `db:"policy_version"`
	// SettingsRevision moves with every change to the organization's
	// settings, members, roles, groups and domains (organization_settings_schema.go),
	// so an open 조직 window reads them again (useWorkspaceOrganizationSettings).
	SettingsRevision int    `db:"settings_revision"`
	StorageLimit     string `db:"storage_limit_bytes"`
}

func registerOrganizationRoutes(event *core.ServeEvent) {
	event.Router.Route(organizationListMethod, apiRoot+organizationListPath, listOrganizations)
	registerOrganizationSettingsRoutes(event)
	registerOrganizationPeopleRoutes(event)
	registerOrganizationStoragePolicyRoutes(event)
}

func listOrganizations(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	cursor, err := decodeOrganizationListCursor(event.Request.URL.Query().Get("cursor"), user.Id)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeOrganizationListCursorInvalid, "The organization list query position is invalid.")
	}
	rows := []storedOrganizationListRow{}
	err = event.App.DB().NewQuery(`
		SELECT [[organization]].[[id]] AS [[organization_id]],
			[[projection]].[[organization_created]] AS [[created]], [[organization]].[[name]],
			[[membership]].[[user]] AS [[user_id]], [[membership]].[[role]], [[membership]].[[status]],
			[[policy]].[[version]] AS [[policy_version]], [[policy]].[[settings_revision]],
			[[organization]].[[storage_limit_bytes]]
		FROM {{leafen_organization_list_projection}} [[projection]]
		JOIN {{leafen_organization_memberships}} [[membership]]
			ON [[membership]].[[id]] = [[projection]].[[membership_id]]
		JOIN {{leafen_organizations}} [[organization]]
			ON [[organization]].[[id]] = [[projection]].[[organization_id]]
		JOIN {{leafen_organization_policy_state}} [[policy]]
			ON [[policy]].[[organization_id]] = [[projection]].[[organization_id]]
		WHERE [[projection]].[[user_id]] = {:userId}
			AND [[projection]].[[status]] = 'active'
			AND ([[projection]].[[organization_created]] > {:afterCreated}
				OR ([[projection]].[[organization_created]] = {:afterCreated}
					AND [[projection]].[[organization_id]] > {:afterOrganizationId}))
		ORDER BY [[projection]].[[organization_created]], [[projection]].[[organization_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"userId": user.Id, "afterCreated": cursor.Created,
		"afterOrganizationId": cursor.OrganizationID, "limit": maxOrganizationsPerResponse + 1,
	}).All(&rows)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeOrganizationAuthorityInvalid, "Couldn't load the organization details.", err)
	}
	hasMore := len(rows) > maxOrganizationsPerResponse
	if hasMore {
		rows = rows[:maxOrganizationsPerResponse]
	}
	organizationIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		organizationIDs = append(organizationIDs, row.OrganizationID)
	}
	authorizations := map[string]organizationAuthorization{}
	if len(organizationIDs) > 0 {
		if authorizations, err = authorizeOrganizationSet(event.App, user.Id, organizationIDs, permissionOrganizationActive); err != nil {
			if isPermissionDenied(err) {
				return apiError(event, http.StatusNotFound, errorCodeOrganizationNotFound, "Organization not found.")
			}
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeOrganizationAuthorityInvalid, "Couldn't load the organization details.", err)
		}
	}
	response := organizationListResponse{
		Organizations: []organizationResponse{},
		Memberships:   []organizationMembershipResponse{},
	}
	for _, row := range rows {
		storageLimit, err := decodeOrganizationStorageLimit(row.StorageLimit)
		if err != nil {
			return apiErrorWithCause(
				event, http.StatusInternalServerError, errorCodeOrganizationStorageStateInvalid,
				"Couldn't check the organization storage state.", "organization.list", "storage-policy",
				errorCodeOrganizationStorageStateInvalid, err,
			)
		}
		authorization := authorizations[row.OrganizationID]
		capabilities := organizationSettingsCapabilitiesFor(authorization)
		adminRoles := authorization.AdminRoles
		if adminRoles == nil {
			adminRoles = []string{}
		}
		response.Organizations = append(response.Organizations, organizationResponse{
			ID: row.OrganizationID, Name: row.Name, Role: row.Role,
			Status: row.Status, AdminRoles: adminRoles, PolicyVersion: row.PolicyVersion,
			SettingsRevision: row.SettingsRevision, StorageLimitBytes: storageLimit,
			Capabilities: &capabilities,
		})
		response.Memberships = append(response.Memberships, organizationMembershipResponse{
			OrganizationID: row.OrganizationID, UserID: row.UserID,
			Role: row.Role, Status: row.Status,
		})
	}
	if hasMore {
		response.HasMore = true
		nextCursor, err := encodeOrganizationListCursor(rows[len(rows)-1], user.Id)
		if err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeOrganizationAuthorityInvalid, "Couldn't load the organization details.", err)
		}
		response.NextCursor = &nextCursor
	}
	return event.JSON(http.StatusOK, response)
}

// bumpOrganizationPolicy moves the organization's policy version on, so
// what was read under the old one is read again.
func bumpOrganizationPolicy(app core.App, organizationID string, now int64) error {
	return bumpOrganizationPolicyContext(context.Background(), app, organizationID, now)
}

func bumpOrganizationPolicyContext(
	ctx context.Context,
	app core.App,
	organizationID string,
	now int64,
) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_organization_policy_state}}
		SET [[version]] = [[version]] + 1, [[updated]] = {:now}
		WHERE [[organization_id]] = {:organizationId}
	`).WithContext(ctx).Bind(dbx.Params{"now": now, "organizationId": organizationID}).Execute()
	return err
}

const organizationDomainsTable = "leafen_organization_domains"

func normalizeOrganizationDomain(value string) (string, error) {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if value == "" || len(value) > 253 || strings.ContainsAny(value, "@/*:_ ") || net.ParseIP(value) != nil {
		return "", errSAMLConfigurationInvalid
	}
	ascii, err := idna.Lookup.ToASCII(value)
	if err != nil || ascii == "" || len(ascii) > 253 || !strings.Contains(ascii, ".") {
		return "", errSAMLConfigurationInvalid
	}
	for _, label := range strings.Split(ascii, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", errSAMLConfigurationInvalid
		}
	}
	return ascii, nil
}
