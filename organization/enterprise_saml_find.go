package organization

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The sign-in screen's "회사 계정으로 로그인" (the multi-server decision's
// 2026-10-20 "같은 날 다듬음", as Notion's "Continue with SSO"): a person's
// work email names its domain, and an organization that verified it and
// signs in with SAML is where the sign-in starts. Which domains a server has
// isn't told: an address with none gets the same one answer, and the tries
// are limited as sign-in's are (request_limits.go).

type samlFindInput struct {
	Email string `json:"email"`
}

// findSAMLOrganization answers with the organization whose SAML sign-in an
// email's domain starts.
func findSAMLOrganization(event *core.RequestEvent) error {
	input := samlFindInput{}
	if err := event.BindBody(&input); err != nil {
		return samlAPIError(event, errSAMLProviderNotFound, "find-bind")
	}
	email := strings.TrimSpace(input.Email)
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 || len(email) > 320 {
		return samlAPIError(event, errSAMLProviderNotFound, "find-email")
	}
	domain := strings.ToLower(strings.TrimSuffix(email[at+1:], "."))
	var organizationID string
	err := event.App.DB().NewQuery(`
		SELECT [[provider]].[[organization_id]] FROM {{leafen_organization_domains}} [[domain]]
		JOIN {{leafen_saml_providers}} [[provider]] ON [[provider]].[[organization_id]] = [[domain]].[[organization_id]]
		WHERE [[domain]].[[domain]] = {:domain} AND [[domain]].[[status]] = 'verified' AND [[provider]].[[enabled]] = 1
		ORDER BY [[provider]].[[organization_id]] LIMIT 1
	`).Bind(dbx.Params{"domain": domain}).Row(&organizationID)
	if errors.Is(err, sql.ErrNoRows) {
		return samlAPIError(event, errSAMLProviderNotFound, "find-domain")
	}
	if err != nil {
		return samlAPIError(event, err, "find-read")
	}
	return event.JSON(http.StatusOK, map[string]string{"organizationId": organizationID})
}

// companySignInAvailable says whether any organization signs in with SAML
// by a verified domain, for the core's runtime settings.
func companySignInAvailable(app core.App) bool {
	var found int
	err := app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_organization_domains}} [[domain]]
			JOIN {{leafen_saml_providers}} [[provider]] ON [[provider]].[[organization_id]] = [[domain]].[[organization_id]]
			WHERE [[domain]].[[status]] = 'verified' AND [[provider]].[[enabled]] = 1)
	`).Row(&found)
	return err == nil && found == 1
}
