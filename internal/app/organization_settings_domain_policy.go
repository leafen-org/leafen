package app

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// An organization verifies the domains it manages accounts on in its
// settings; the organization module keeps them (SettingsDomains). Without it
// the settings list no domains and refuse the domain actions.

func loadOrganizationSettingsDomains(
	app core.App, organizationID, afterDomain string,
) ([]organizationSettingsDomain, string, bool, error) {
	if domains := organizationPolicy.SettingsDomains; domains != nil {
		return domains.Load(app, organizationID, afterDomain)
	}
	return []organizationSettingsDomain{}, afterDomain, true, nil
}

func prepareOrganizationDomainVerification(
	ctx context.Context, app core.App, organizationID, rawDomain string,
) (OrganizationDomainCheck, error) {
	if domains := organizationPolicy.SettingsDomains; domains != nil {
		return domains.PrepareVerification(ctx, app, organizationID, rawDomain)
	}
	return OrganizationDomainCheck{}, errOrganizationSettingsInvalid
}

func mutateOrganizationDomain(
	app core.App, organizationID string, input organizationSettingsMutationInput,
	check OrganizationDomainCheck, now int64,
) (*organizationSettingsDomain, string, error) {
	if domains := organizationPolicy.SettingsDomains; domains != nil {
		return domains.Mutate(app, organizationID, input, check, now)
	}
	return nil, "", errOrganizationSettingsInvalid
}
