package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func findNotionOAuthAuthorizationCode(app core.App, codeHash string) (storedNotionOAuthAuthorization, bool, error) {
	stored := storedNotionOAuthAuthorization{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[request_hash]], [[grant_id]], [[grant_generation]], [[client_id]], [[redirect_uri]], [[state]],
			[[expires_at]], [[status]], [[refresh_token_id]]
		FROM {{leafen_notion_oauth_authorization_codes}} WHERE [[code_hash]]={:codeHash} LIMIT 1
	`).Bind(dbx.Params{"codeHash": codeHash}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotionOAuthAuthorization{}, false, nil
	}
	return stored, err == nil, err
}

func findNotionOAuthGrantByID(app core.App, id string) (storedNotionOAuthGrant, bool, error) {
	stored := storedNotionOAuthGrant{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[client_id]], [[client_name]], [[workspace_id]], [[scope]],
			[[credential_generation]],
			[[status]], [[revoked_at]]
		FROM {{leafen_notion_oauth_grants}} WHERE [[id]]={:id} LIMIT 1
	`).Bind(dbx.Params{"id": id}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotionOAuthGrant{}, false, nil
	}
	return stored, err == nil, err
}

func findNotionOAuthRefreshByHash(app core.App, tokenHash string) (storedNotionOAuthRefreshToken, bool, error) {
	return findNotionOAuthRefresh(app, "[[token_hash]]={:value}", tokenHash)
}

func findNotionOAuthRefreshByID(app core.App, id string) (storedNotionOAuthRefreshToken, bool, error) {
	return findNotionOAuthRefresh(app, "[[id]]={:value}", id)
}

func findNotionOAuthRefresh(app core.App, predicate, value string) (storedNotionOAuthRefreshToken, bool, error) {
	stored := storedNotionOAuthRefreshToken{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[token_hash]], [[grant_id]], [[client_id]], [[generation]], [[status]], [[successor_id]],
			[[created_at]], [[expires_at]], [[revoked_at]]
		FROM {{leafen_notion_oauth_refresh_tokens}} WHERE ` + predicate + ` LIMIT 1
	`).Bind(dbx.Params{"value": value}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotionOAuthRefreshToken{}, false, nil
	}
	return stored, err == nil, err
}

func revokeNotionOAuthGrant(app core.App, grantID string, now int64) error {
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_oauth_grants}}
		SET [[status]]='revoked', [[revoked_at]]={:now}, [[updated_at]]={:now}
		WHERE [[id]]={:grantId} AND [[status]]='active'
	`).Bind(dbx.Params{"now": now, "grantId": grantID}).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_oauth_refresh_tokens}}
		SET [[status]]='revoked', [[revoked_at]]={:now}, [[updated_at]]={:now}
		WHERE [[grant_id]]={:grantId} AND [[status]]!='revoked'
	`).Bind(dbx.Params{"now": now, "grantId": grantID}).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_oauth_refresh_tokens}}
		WHERE [[grant_id]]={:grantId} AND [[generation]] < (
			SELECT MAX([[generation]]) FROM {{leafen_notion_oauth_refresh_tokens}}
			WHERE [[grant_id]]={:grantId}
		)
	`).Bind(dbx.Params{"grantId": grantID}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_oauth_authorization_codes}}
		WHERE [[grant_id]]={:grantId} AND [[id]] NOT IN (
			SELECT [[id]] FROM {{leafen_notion_oauth_authorization_codes}}
			WHERE [[grant_id]]={:grantId} ORDER BY [[created_at]] DESC, [[id]] DESC LIMIT 1
		)
	`).Bind(dbx.Params{"grantId": grantID}).Execute()
	return err
}
