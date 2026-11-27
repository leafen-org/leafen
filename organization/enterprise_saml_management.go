package organization

import (
	"errors"
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func getSAMLManagementSnapshot(event *core.RequestEvent) error {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		return leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	response, err := loadSAMLManagementSnapshot(event.App, user.Id, organizationID)
	if leafen.IsPermissionDenied(err) {
		return leafen.APIError(event, http.StatusNotFound, leafen.ErrorCodeOrganizationNotFound, "Organization not found.")
	}
	if err != nil {
		return samlAPIError(event, err, "management-read")
	}
	fillSAMLServiceOrigins(event.App, event.Request, response.Provider)
	return event.JSON(http.StatusOK, response)
}

func loadSAMLManagementSnapshot(
	app core.App,
	principalID string,
	organizationID string,
) (leafen.SAMLManagementSnapshotResponse, error) {
	response := leafen.SAMLManagementSnapshotResponse{Domains: []leafen.SAMLDomainResponse{}, Workspaces: []leafen.SAMLWorkspaceResponse{}}
	err := app.RunInTransaction(func(txApp core.App) error {
		organization, err := leafen.AuthorizeOrganization(
			txApp, principalID, organizationID, leafen.PermissionOrganizationIdentityAdmin,
		)
		if err != nil {
			return err
		}
		provider, err := loadStoredSAMLProviderByOrganization(txApp, organization.OrganizationID)
		if err == nil {
			public := publicSAMLProviderResponse(provider)
			response.Provider = &public
		} else if !errors.Is(err, errSAMLProviderNotFound) {
			return err
		}

		domains := []storedOrganizationDomain{}
		if err := txApp.DB().NewQuery(`
			SELECT [[organization_id]], [[domain]], [[status]], [[challenge]], [[verified_at]],
				[[created_at]], [[updated_at]]
			FROM {{leafen_organization_domains}}
			WHERE [[organization_id]] = {:organizationId}
			ORDER BY [[domain]]
		`).Bind(dbx.Params{"organizationId": organization.OrganizationID}).All(&domains); err != nil {
			return err
		}
		for _, domain := range domains {
			item := leafen.SAMLDomainResponse{Domain: domain.Domain, Status: domain.Status}
			if domain.Status == leafen.SAMLDomainPending && domain.Challenge != "" {
				item.VerificationRecord = samlDomainVerificationRecord(domain.Domain, domain.Challenge)
			}
			response.Domains = append(response.Domains, item)
		}
		// The organization's workspaces, which its SAML configuration covers,
		// as Notion's 연결된 워크스페이스 table.
		response.Workspaces, err = leafen.OrganizationWorkspaceSummaries(txApp, organization.OrganizationID)
		return err
	})
	return response, err
}
