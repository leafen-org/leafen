package organization

import (
	"context"
	"database/sql"
	"errors"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func loadSettingsDomains(
	app core.App, organizationID, afterDomain string,
) ([]leafen.OrganizationSettingsDomain, string, bool, error) {
	rows := []storedOrganizationDomain{}
	err := app.DB().NewQuery(`SELECT ` + storedOrganizationDomainColumns + ` FROM {{leafen_organization_domains}}
		WHERE [[organization_id]]={:organizationId} AND [[domain]]>{:afterDomain}
		ORDER BY [[domain]] LIMIT {:limit}`).Bind(dbx.Params{
		"organizationId": organizationID, "afterDomain": afterDomain,
		"limit": leafen.OrganizationSettingsDomainPageSize + 1,
	}).All(&rows)
	if err != nil {
		return nil, "", false, err
	}
	hasMore := len(rows) > leafen.OrganizationSettingsDomainPageSize
	if hasMore {
		rows = rows[:leafen.OrganizationSettingsDomainPageSize]
	}
	domains := make([]leafen.OrganizationSettingsDomain, 0, len(rows))
	for _, row := range rows {
		domains = append(domains, organizationSettingsDomainResponse(row))
	}
	if !hasMore || len(rows) == 0 {
		return domains, "", true, nil
	}
	return domains, rows[len(rows)-1].Domain, false, nil
}

func organizationSettingsDomainResponse(row storedOrganizationDomain) leafen.OrganizationSettingsDomain {
	response := leafen.OrganizationSettingsDomain{
		ID: row.Domain, Domain: row.Domain, Status: row.Status,
		VerifiedAt: leafen.OrganizationSettingsTime(row.VerifiedAt),
	}
	if row.Status == leafen.SAMLDomainPending && row.Challenge != "" {
		response.RecordName = "_leafen-challenge." + row.Domain
		response.RecordValue = row.Challenge
		if row.CheckedAt > 0 {
			response.VerificationCheckedAt = leafen.OrganizationSettingsTime(row.CheckedAt)
			response.VerificationErrorCode, response.VerificationFound = row.CheckCode, row.CheckFound
		}
	}
	return response
}

func prepareDomainVerification(
	ctx context.Context, app core.App, organizationID, rawDomain string,
) (leafen.OrganizationDomainCheck, error) {
	domain, err := leafen.NormalizeOrganizationDomain(rawDomain)
	if err != nil {
		return leafen.OrganizationDomainCheck{}, leafen.ErrOrganizationSettingsInvalid
	}
	stored, err := organizationDomain(app, organizationID, domain)
	if errors.Is(err, sql.ErrNoRows) {
		return leafen.OrganizationDomainCheck{}, leafen.ErrOrganizationSettingsInvalid
	}
	if err != nil || stored.Status == leafen.SAMLDomainVerified {
		return leafen.OrganizationDomainCheck{Domain: domain, Challenge: stored.Challenge}, err
	}
	code, found := checkDomainChallenge(ctx, domain, stored.Challenge)
	if code != "" {
		return leafen.OrganizationDomainCheck{Domain: domain, FailureCode: code, Found: found}, nil
	}
	return leafen.OrganizationDomainCheck{Domain: domain, Challenge: stored.Challenge}, nil
}

func mutateSettingsDomain(
	app core.App, organizationID string, input leafen.OrganizationSettingsMutationInput,
	check leafen.OrganizationDomainCheck, now int64,
) (*leafen.OrganizationSettingsDomain, string, error) {
	rawDomain := input.Value
	if rawDomain == "" {
		rawDomain = input.TargetID
	}
	domain, err := leafen.NormalizeOrganizationDomain(rawDomain)
	if err != nil {
		return nil, "", leafen.ErrOrganizationSettingsInvalid
	}
	switch input.Action {
	case leafen.OrganizationSettingsActionDomainAdd:
		token, err := leafen.NewAuthProviderToken("ldv_", 24)
		if err != nil {
			return nil, "", err
		}
		if err := upsertOrganizationDomain(
			app, organizationID, domain, leafen.SAMLDomainPending, samlDomainChallengePrefix+token, now,
		); err != nil {
			return nil, "", err
		}
	case leafen.OrganizationSettingsActionDomainVerify:
		if domain != check.Domain {
			return nil, "", errors.New(leafen.ErrorCodeOrganizationSettingsVerificationFailed)
		}
		// A check that proves nothing is kept and shown on the domain.
		if check.Challenge == "" {
			if err := recordOrganizationDomainCheck(app, organizationID, domain, check.FailureCode, check.Found, now); err != nil {
				return nil, "", err
			}
			break
		}
		if err := markOrganizationDomainVerified(app, organizationID, domain, check.Challenge, now); err != nil {
			return nil, "", errors.New(leafen.ErrorCodeOrganizationSettingsVerificationFailed)
		}
		if err := recordOrganizationDomainCheck(app, organizationID, domain, "", "", now); err != nil {
			return nil, "", err
		}
	case leafen.OrganizationSettingsActionDomainRemove:
		result, err := app.DB().Delete(leafen.OrganizationDomainsTable,
			dbx.HashExp{"organization_id": organizationID, "domain": domain}).Execute()
		if err != nil {
			return nil, "", err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return nil, "", sql.ErrNoRows
		}
		return nil, domain, nil
	default:
		return nil, "", leafen.ErrOrganizationSettingsInvalid
	}
	stored, err := organizationDomain(app, organizationID, domain)
	if err != nil {
		return nil, "", err
	}
	response := organizationSettingsDomainResponse(stored)
	if input.Action == leafen.OrganizationSettingsActionDomainVerify {
		response.VerificationCheckedAt = leafen.OrganizationSettingsTime(now)
	}
	return &response, "", nil
}

// settingsDomains are the domains part of the organization's settings.
type settingsDomains struct{}

func (settingsDomains) Load(app core.App, organizationID, afterDomain string) ([]leafen.OrganizationSettingsDomain, string, bool, error) {
	return loadSettingsDomains(app, organizationID, afterDomain)
}

func (settingsDomains) PrepareVerification(ctx context.Context, app core.App, organizationID, rawDomain string) (leafen.OrganizationDomainCheck, error) {
	return prepareDomainVerification(ctx, app, organizationID, rawDomain)
}

func (settingsDomains) Mutate(
	app core.App, organizationID string, input leafen.OrganizationSettingsMutationInput,
	check leafen.OrganizationDomainCheck, now int64,
) (*leafen.OrganizationSettingsDomain, string, error) {
	return mutateSettingsDomain(app, organizationID, input, check, now)
}
