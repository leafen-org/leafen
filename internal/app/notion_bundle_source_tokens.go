package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A bundle source's token: made, sealed for the connection, before the
// transaction that issues it.
type bundleSourceToken struct {
	name, secret, cipher string
}

func newBundleSourceToken(app core.App, label string) (bundleSourceToken, error) {
	secret, err := newPersonalAccessTokenSecret()
	if err != nil {
		return bundleSourceToken{}, err
	}
	key, err := notionImportEncryptionKey(app)
	if err != nil {
		return bundleSourceToken{}, err
	}
	cipher, err := security.Encrypt([]byte(secret), key)
	if err != nil {
		return bundleSourceToken{}, err
	}
	return bundleSourceToken{name: "Migration from " + label, secret: secret, cipher: cipher}, nil
}

func (token bundleSourceToken) issue(txApp core.App, userID, stagingID string) (string, error) {
	stored, err := issuePersonalAccessToken(txApp, userID, stagingID, token.name, personalAccessTokenLifetimes[bundleSourceTokenWindow], token.secret)
	return stored.ID, err
}

// revokeBundleSourceToken revokes the source's token, as its maker would in
// settings; a workspace deleted took its tokens along.
func revokeBundleSourceToken(txApp core.App, userID, tokenID string) error {
	var workspaceID string
	err := txApp.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_notion_personal_access_tokens}}
		WHERE [[id]] = {:id} AND [[creator_id]] = {:user} AND [[status]] = 'active'`).
		Bind(dbx.Params{"id": tokenID, "user": userID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := txApp.DB().NewQuery(`
		UPDATE {{leafen_notion_personal_access_tokens}}
		SET [[status]] = 'revoked', [[revoked_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[id]] = {:id} AND [[status]] = 'active'
	`).Bind(dbx.Params{"now": now, "id": tokenID}).Execute(); err != nil {
		return err
	}
	authorization, err := authorizeWorkspace(txApp, userID, workspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) || errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return insertOrganizationAuditEvent(txApp, organizationAuditEvent{
		OrganizationID: authorization.Organization.OrganizationID, WorkspaceID: workspaceID,
		ActorID: userID, Action: "personal_access_token.revoke",
		TargetType: "personal_access_token", TargetID: tokenID, OccurredAt: now,
	})
}
