package app

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	mcpConsentAuthorityRevisionTable = "leafen_mcp_consent_authority_revision"
)

type storedMCPConsentSnapshot struct {
	MembershipRevision int64 `db:"membership_revision"`
	AuthorityRevision  int64 `db:"authority_revision"`
}

func readMCPConsentMembershipSnapshot(app core.App, principalID string) (string, error) {
	var stored storedMCPConsentSnapshot
	err := app.DB().NewQuery(`
		SELECT COALESCE((
			SELECT [[revision]] FROM {{leafen_mcp_consent_principal_revisions}}
			WHERE [[principal_id]] = {:principalId}
		), 0) AS [[membership_revision]],
		(
			SELECT [[revision]] FROM {{leafen_mcp_consent_authority_revision}}
			WHERE [[singleton]] = 'authority'
		) AS [[authority_revision]]
	`).Bind(dbx.Params{"principalId": principalID}).One(&stored)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", stored.MembershipRevision, stored.AuthorityRevision), nil
}

func coordinateMCPConsentMembershipSnapshot(
	app core.App,
	principalID string,
	expected string,
) (string, error) {
	current, err := readMCPConsentMembershipSnapshot(app, principalID)
	if err != nil {
		return "", err
	}
	if expected != "" && expected != current {
		return "", errMCPOAuthConsentSnapshotChanged
	}
	return current, nil
}
