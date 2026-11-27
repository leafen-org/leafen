package organization

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A domain is verified by a DNS TXT record under _leafen-challenge.<domain>
// holding its challenge.
const samlDomainChallengePrefix = "leafen-domain-verification="

var samlTXTLookup = net.DefaultResolver.LookupTXT

var errOrganizationDomainNotVerified = errors.New(leafen.ErrorCodeSCIMConfigurationInvalid)

type storedOrganizationDomain struct {
	OrganizationID string `db:"organization_id"`
	Domain         string `db:"domain"`
	Status         string `db:"status"`
	Challenge      string `db:"challenge"`
	VerifiedAt     int64  `db:"verified_at"`
	CreatedAt      int64  `db:"created_at"`
	UpdatedAt      int64  `db:"updated_at"`
	// The last check: when, why it proved nothing, and the value found.
	CheckedAt  int64  `db:"verification_checked_at"`
	CheckCode  string `db:"verification_error_code"`
	CheckFound string `db:"verification_found"`
}

// storedOrganizationDomainColumns are the columns a domain is read with.
const storedOrganizationDomainColumns = `[[organization_id]], [[domain]], [[status]], [[challenge]], [[verified_at]],
	[[created_at]], [[updated_at]], [[verification_checked_at]], [[verification_error_code]], [[verification_found]]`

// checkDomainChallenge looks the domain's TXT record up: the reason it
// doesn't hold the challenge, and the first value found instead, or "" when
// it does.
func checkDomainChallenge(ctx context.Context, domain, challenge string) (string, string) {
	records, err := samlTXTLookup(ctx, "_leafen-challenge."+domain)
	var dnsError *net.DNSError
	switch {
	case errors.As(err, &dnsError) && dnsError.IsNotFound:
		return leafen.OrganizationDomainRecordMissing, ""
	case err != nil:
		return leafen.OrganizationDomainLookupFailed, ""
	}
	for _, record := range records {
		if strings.TrimSpace(record) == challenge {
			return "", ""
		}
	}
	if len(records) == 0 {
		return leafen.OrganizationDomainRecordMissing, ""
	}
	found := strings.TrimSpace(records[0])
	if len(found) > 255 {
		found = found[:255]
	}
	return leafen.OrganizationDomainRecordMismatch, found
}

// recordOrganizationDomainCheck keeps a check's outcome on the domain.
func recordOrganizationDomainCheck(app core.App, organizationID, domain, code, found string, now int64) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_organization_domains}}
		SET [[verification_checked_at]] = {:now}, [[verification_error_code]] = {:code}, [[verification_found]] = {:found}
		WHERE [[organization_id]] = {:organizationId} AND [[domain]] = {:domain}
	`).Bind(dbx.Params{"now": now, "code": code, "found": found, "organizationId": organizationID, "domain": domain}).Execute()
	return err
}

func upsertOrganizationDomain(
	app core.App,
	organizationID string,
	domain string,
	status string,
	challenge string,
	now int64,
) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_organization_domains}} (
			[[organization_id]], [[domain]], [[status]], [[challenge]],
			[[verified_at]], [[created_at]], [[updated_at]]
		) VALUES (
			{:organizationId}, {:domain}, {:status}, {:challenge}, 0, {:now}, {:now}
		)
		ON CONFLICT ([[organization_id]], [[domain]]) DO UPDATE SET
			[[status]] = excluded.[[status]], [[challenge]] = excluded.[[challenge]],
			[[verified_at]] = 0, [[updated_at]] = excluded.[[updated_at]]
	`).Bind(dbx.Params{
		"organizationId": organizationID, "domain": domain, "status": status,
		"challenge": challenge, "now": now,
	}).Execute()
	return err
}

func organizationDomain(app core.App, organizationID string, domain string) (storedOrganizationDomain, error) {
	row := storedOrganizationDomain{}
	err := app.DB().NewQuery(`
		SELECT ` + storedOrganizationDomainColumns + `
		FROM {{leafen_organization_domains}}
		WHERE [[organization_id]] = {:organizationId} AND [[domain]] = {:domain}
	`).Bind(dbx.Params{"organizationId": organizationID, "domain": domain}).One(&row)
	return row, err
}

func markOrganizationDomainVerified(
	app core.App,
	organizationID string,
	domain string,
	challenge string,
	now int64,
) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_organization_domains}}
		SET [[status]] = 'verified', [[verified_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[organization_id]] = {:organizationId} AND [[domain]] = {:domain}
			AND [[status]] = 'pending' AND [[challenge]] = {:challenge}
	`).Bind(dbx.Params{
		"now": now, "organizationId": organizationID, "domain": domain, "challenge": challenge,
	}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return leafen.ErrSAMLConfigurationInvalid
	}
	// The domain's people are now the organization's managed users, whose
	// sign-ins last no longer than its session duration.
	return applyManagedUserSessionDeadlines(app, organizationID, now)
}

func requireVerifiedOrganizationDomain(app core.App, organizationID string, email string) error {
	email, ok := leafen.NormalizeEmailIdentity(email)
	if !ok {
		return errOrganizationDomainNotVerified
	}
	separator := strings.LastIndexByte(email, '@')
	if separator < 1 || separator == len(email)-1 {
		return errOrganizationDomainNotVerified
	}
	domain, err := leafen.NormalizeOrganizationDomain(email[separator+1:])
	if err != nil {
		return errOrganizationDomainNotVerified
	}
	row, err := organizationDomain(app, organizationID, domain)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (row.Status != leafen.SAMLDomainVerified || row.VerifiedAt <= 0)) {
		return errOrganizationDomainNotVerified
	}
	return err
}
