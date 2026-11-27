package organization

import (
	"database/sql"
	"errors"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func revokeSessionsForSAMLEnforcement(
	app core.App,
	organizationID string,
	_ string,
	enforcement string,
	now int64,
) error {
	if enforcement == leafen.SAMLEnforcementOptional {
		return nil
	}
	verifiedDomainPredicate := ""
	if enforcement == leafen.SAMLEnforcementVerifiedDomains {
		verifiedDomainPredicate = `
			AND EXISTS (
				SELECT 1 FROM {{leafen_organization_domains}} [[domain]]
				WHERE [[domain]].[[organization_id]] = {:organizationId}
					AND [[domain]].[[status]] = 'verified'
					AND [[domain]].[[domain]] IN (` + leafen.ManagedAccountDomainSQL("[[user]].[[id]]") + `)
			)
		`
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_auth_sessions}}
		SET [[revoked_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[revoked_at]] = 0 AND [[owner_id]] IN (
			SELECT [[membership]].[[user]]
			FROM {{leafen_organization_memberships}} [[membership]]
			JOIN {{users}} [[user]] ON [[user]].[[id]] = [[membership]].[[user]]
			WHERE [[membership]].[[organization]] = {:organizationId}
				AND [[membership]].[[status]] = 'active'
				AND [[membership]].[[role]] NOT IN ('owner', 'guest')
				` + verifiedDomainPredicate + `
		)
	`).Bind(dbx.Params{
		"now": now, "organizationId": organizationID,
	}).Execute()
	if err == nil {
		leafen.RecheckCollaborationAccessAfterCommit(app)
	}
	return err
}

// authorizeSSOSignIn refuses a sign-in other than through the SAML provider
// of an organization that requires single sign-on of the person.
func authorizeSSOSignIn(
	app core.App,
	userID string,
	method string,
	providerID string,
) error {
	method = strings.TrimSpace(method)
	providerID = strings.TrimSpace(providerID)
	var blockedOrganizationID string
	err := app.DB().NewQuery(`
		SELECT [[membership]].[[organization]]
		FROM {{leafen_organization_memberships}} [[membership]]
		JOIN {{leafen_saml_providers}} [[provider]]
			ON [[provider]].[[organization_id]] = [[membership]].[[organization]]
		JOIN {{users}} [[user]] ON [[user]].[[id]] = [[membership]].[[user]]
		WHERE [[membership]].[[user]] = {:userId}
			AND [[membership]].[[status]] = 'active'
			AND [[membership]].[[role]] NOT IN ('owner', 'guest')
			AND [[provider]].[[enabled]] = 1
			AND [[provider]].[[enforcement]] != 'optional'
			AND (
				[[provider]].[[enforcement]] = 'required_for_all_members'
				OR EXISTS (
					SELECT 1 FROM {{leafen_organization_domains}} [[domain]]
					WHERE [[domain]].[[organization_id]] = [[provider]].[[organization_id]]
						AND [[domain]].[[status]] = 'verified'
						AND [[domain]].[[domain]] IN (` + leafen.ManagedAccountDomainSQL("[[user]].[[id]]") + `)
				)
			)
			AND NOT ({:method} = 'saml' AND [[provider]].[[id]] = {:providerId})
		ORDER BY [[membership]].[[organization]]
		LIMIT 1
	`).Bind(dbx.Params{
		"userId": userID, "method": method, "providerId": providerID,
	}).Row(&blockedOrganizationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return leafen.ErrSAMLRequired
}
