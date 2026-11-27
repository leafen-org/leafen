package organization

import (
	"context"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func normalizeSAMLDomain(value string) (string, error) {
	return leafen.NormalizeOrganizationDomain(value)
}

func createSAMLDomainChallenge(
	app core.App,
	organizationID string,
	actorID string,
	domainInput string,
) (leafen.SAMLDomainResponse, error) {
	domain, err := normalizeSAMLDomain(domainInput)
	if err != nil {
		return leafen.SAMLDomainResponse{}, err
	}
	if _, err := loadStoredSAMLProviderByOrganization(app, organizationID); err != nil {
		return leafen.SAMLDomainResponse{}, err
	}
	token, err := leafen.NewAuthProviderToken("ldv_", 24)
	if err != nil {
		return leafen.SAMLDomainResponse{}, err
	}
	challenge := samlDomainChallengePrefix + token
	now := time.Now().UTC().UnixMilli()
	err = app.RunInTransaction(func(txApp core.App) error {
		if err := upsertOrganizationDomain(
			txApp, organizationID, domain, leafen.SAMLDomainPending, challenge, now,
		); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_organization_policy_state}}
			SET [[version]] = [[version]] + 1, [[updated]] = {:now}
			WHERE [[organization_id]] = {:organizationId}
		`).Bind(dbx.Params{"now": now, "organizationId": organizationID}).Execute(); err != nil {
			return err
		}
		return leafen.RecordOrganizationAudit(
			txApp, organizationID, actorID, "organization.saml.domain.challenge", "domain",
			domain+":"+leafen.HashAuthProviderToken(challenge)[:16],
		)
	})
	if err != nil {
		return leafen.SAMLDomainResponse{}, err
	}
	return leafen.SAMLDomainResponse{
		Domain: domain, Status: leafen.SAMLDomainPending,
		VerificationRecord: samlDomainVerificationRecord(domain, challenge),
	}, nil
}

func verifySAMLDomain(
	ctx context.Context,
	app core.App,
	organizationID string,
	actorID string,
	domainInput string,
) (leafen.SAMLDomainResponse, error) {
	domain, err := normalizeSAMLDomain(domainInput)
	if err != nil {
		return leafen.SAMLDomainResponse{}, err
	}
	provider, err := loadStoredSAMLProviderByOrganization(app, organizationID)
	if err != nil {
		return leafen.SAMLDomainResponse{}, err
	}
	stored, err := organizationDomain(app, organizationID, domain)
	if err != nil {
		return leafen.SAMLDomainResponse{}, leafen.ErrSAMLConfigurationInvalid
	}
	if stored.Status == leafen.SAMLDomainVerified {
		return leafen.SAMLDomainResponse{Domain: domain, Status: leafen.SAMLDomainVerified}, nil
	}
	now := time.Now().UTC().UnixMilli()
	// The organization's domain list shows this check's outcome too.
	if code, found := checkDomainChallenge(ctx, domain, stored.Challenge); code != "" {
		if err := recordOrganizationDomainCheck(app, organizationID, domain, code, found, now); err != nil {
			return leafen.SAMLDomainResponse{}, err
		}
		return leafen.SAMLDomainResponse{}, leafen.ErrSAMLConfigurationInvalid
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		if err := markOrganizationDomainVerified(
			txApp, organizationID, domain, stored.Challenge, now,
		); err != nil {
			return err
		}
		if provider.Enabled == 1 && provider.Enforcement == leafen.SAMLEnforcementVerifiedDomains {
			if err := revokeSessionsForSAMLEnforcement(
				txApp, organizationID, provider.ID, provider.Enforcement, now,
			); err != nil {
				return err
			}
		}
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_organization_policy_state}}
			SET [[version]] = [[version]] + 1, [[updated]] = {:now}
			WHERE [[organization_id]] = {:organizationId}
		`).Bind(dbx.Params{"now": now, "organizationId": organizationID}).Execute(); err != nil {
			return err
		}
		return leafen.RecordOrganizationAudit(
			txApp, organizationID, actorID, "organization.saml.domain.verified", "domain",
			domain+":"+leafen.HashAuthProviderToken(stored.Challenge)[:16],
		)
	})
	if err != nil {
		return leafen.SAMLDomainResponse{}, err
	}
	return leafen.SAMLDomainResponse{Domain: domain, Status: leafen.SAMLDomainVerified}, nil
}

func samlDomainVerificationRecord(domain string, challenge string) string {
	return "_leafen-challenge." + domain + " TXT \"" + challenge + "\""
}
