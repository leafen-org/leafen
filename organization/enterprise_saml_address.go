package organization

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An organization's SAML sign-in after the server's address changes (the
// multi-server decision's 2026-10-20 change "서버 주소는 바뀔 수 있다"): the
// identity provider holds the server's entity ID and ACS at the old address,
// so while that forwards here the service provider keeps them — a sign-in
// starts under the old entity ID, and the answer, posted to the old ACS and
// carried on by the forward, is checked against it. The organization's
// identity administrators are shown the new values to add and say once they
// are; the first sign-in answered at the new ACS ends the note. The kept
// values are the core's (leafen.KeptReturnFor), as for its own sign-in
// providers.

const samlACSPath = "/auth/saml/acs"

func samlReturnKey(providerID string) string {
	return "saml:" + providerID
}

// samlServiceOrigin is the address the service provider names itself at: the
// one the identity provider has while it forwards to current, else current.
func samlServiceOrigin(app core.App, config samlProviderConfig, current string) string {
	acs := leafen.KeptReturnFor(app, samlReturnKey(config.ID), config.Issuer, current+leafen.APIRoot+samlACSPath)
	return strings.TrimSuffix(acs, leafen.APIRoot+samlACSPath)
}

// keepSAMLServiceOrigin notes the ACS a verified answer was checked against.
func keepSAMLServiceOrigin(app core.App, config samlProviderConfig, acs string) {
	leafen.KeepReturn(app, samlReturnKey(config.ID), config.Issuer, acs)
}

// followSAMLAddressChange keeps, for each enabled SAML sign-in with nothing
// kept, the ACS at the address just left, and tells the organization's
// owners when its identity provider needs the new one.
func followSAMLAddressChange(app core.App, previous, address string) {
	providers := []struct {
		ID             string `db:"id"`
		OrganizationID string `db:"organization_id"`
		Issuer         string `db:"issuer"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[organization_id]], [[issuer]] FROM {{leafen_saml_providers}} WHERE [[enabled]] = 1`).
		All(&providers); err != nil {
		app.Logger().Warn("leafen SAML return addresses not kept", "error", err)
		return
	}
	for _, provider := range providers {
		if !leafen.KeepEarlierReturn(app, samlReturnKey(provider.ID), provider.Issuer,
			previous+leafen.APIRoot+samlACSPath, address+leafen.APIRoot+samlACSPath) {
			continue
		}
		owners := []string{}
		if err := app.DB().NewQuery(`SELECT [[user]] FROM {{leafen_organization_memberships}}
			WHERE [[organization]] = {:organization} AND [[role]] = 'owner' AND [[status]] = 'active'`).
			Bind(dbx.Params{"organization": provider.OrganizationID}).Column(&owners); err != nil {
			app.Logger().Warn("leafen SAML return notice failed", "error", err)
			continue
		}
		leafen.NotifyReturnChange(app, owners, provider.OrganizationID, address, "SAML", "/settings?section=identity")
	}
}

// fillSAMLServiceOrigins fills in what the identity administrators see: the
// address the service provider's values are at, and after a change the one
// the identity provider still has and whether they said the new one is added.
func fillSAMLServiceOrigins(app core.App, request *http.Request, provider *leafen.SAMLProviderResponse) {
	if provider == nil || provider.ID == "" {
		return
	}
	origin, err := leafen.OutwardServerOrigin(request)
	if err != nil {
		return
	}
	provider.ServiceOrigin = origin
	earlier, added := leafen.ProjectKeptReturn(app, samlReturnKey(provider.ID), provider.Issuer, origin+leafen.APIRoot+samlACSPath)
	if earlier != "" {
		provider.EarlierServiceOrigin = strings.TrimSuffix(earlier, leafen.APIRoot+samlACSPath)
		provider.ServiceOriginAdded = added
	}
}

// setSAMLServiceReturn is an identity administrator saying the identity
// provider has the new values, or taking it back when it refused them.
func setSAMLServiceReturn(event *core.RequestEvent) error {
	organization, ok := authorizeSAMLManagement(event)
	if !ok {
		return nil
	}
	input := leafen.SAMLServiceReturnInput{}
	if err := event.BindBody(&input); err != nil {
		return samlAPIError(event, leafen.ErrSAMLConfigurationInvalid, "return-bind")
	}
	provider, err := loadStoredSAMLProviderByOrganization(event.App, organization.OrganizationID)
	if err != nil {
		return samlAPIError(event, err, "return-provider")
	}
	origin, err := leafen.OutwardServerOrigin(event.Request)
	if err != nil {
		return samlAPIError(event, err, "return-origin")
	}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		changed, err := leafen.SetKeptReturnAdded(txApp, samlReturnKey(provider.ID), origin+leafen.APIRoot+samlACSPath, input.Added)
		if err != nil || !changed {
			return err
		}
		return leafen.RecordOrganizationAudit(txApp, organization.OrganizationID, organization.PrincipalID,
			"organization.saml.return_address", "saml_provider", provider.ID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return samlAPIError(event, errSAMLProviderNotFound, "return-kept")
	}
	if err != nil {
		return samlAPIError(event, err, "return-write")
	}
	return getSAMLManagementSnapshot(event)
}
