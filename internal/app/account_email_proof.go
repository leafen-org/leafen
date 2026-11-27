package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// markAccountEmailUnproven keeps an account's address as not proven.
func markAccountEmailUnproven(app core.App, userID, email string) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_unproven_account_emails}} ([[email]], [[user_id]]) VALUES ({:email}, {:userId})
		ON CONFLICT ([[email]]) DO UPDATE SET [[user_id]] = excluded.[[user_id]]
	`).Bind(dbx.Params{"email": normalizePageShareEmail(email), "userId": userID}).Execute()
	return err
}

// markAccountEmailProven takes an address off the unproven list once it is
// proven.
func markAccountEmailProven(app core.App, email string) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_unproven_account_emails}} WHERE [[email]] = {:email}`).
		Bind(dbx.Params{"email": normalizePageShareEmail(email)}).Execute()
	return err
}

// claimUnprovenAddress hands an account to the owner of its address once
// someone proves the address the account holds unproven — a sign-in
// provider, an organization's SSO or SCIM: whoever chose the account's
// password and devices before had never shown the address was theirs, so
// the password is replaced, every browser session ends, its passkeys,
// personal access tokens and connections go, and the owner signs in as the
// proof did (2026-10-18: an account signed up
// with someone else's address on an open server was adopted as theirs).
func claimUnprovenAddress(app core.App, user *core.Record, email string) error {
	held := 0
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_unproven_account_emails}}
		WHERE [[email]] = {:email} AND [[user_id]] = {:userId}`).
		Bind(dbx.Params{"email": normalizePageShareEmail(email), "userId": user.Id}).Row(&held); err != nil {
		return err
	}
	if held == 0 {
		return nil
	}
	if err := markAccountEmailProven(app, email); err != nil {
		return err
	}
	password, err := newAuthProviderToken("lcp_", 32)
	if err != nil {
		return err
	}
	user.SetPassword(password)
	user.SetVerified(true)
	if err := app.Save(user); err != nil {
		return err
	}
	if err := markAccountPasswordUnknown(app, user.Id); err != nil {
		return err
	}
	if err := revokeAllBrowserSessions(app, user.Id); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_passkeys}} WHERE [[user_id]] = {:userId}`).
		Bind(dbx.Params{"userId": user.Id}).Execute(); err != nil {
		return err
	}
	// Its keys and connections, made by whoever held it, end too, with
	// what they made (mcp_connections.go).
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_personal_access_tokens}}
		SET [[status]] = 'revoked', [[revoked_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[creator_id]] = {:userId} AND [[status]] = 'active'
	`).Bind(dbx.Params{"userId": user.Id, "now": now}).Execute(); err != nil {
		return err
	}
	connections := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_mcp_connections}}
		WHERE [[owner_id]] = {:userId} AND [[revoked_at]] = 0`).
		Bind(dbx.Params{"userId": user.Id}).Column(&connections); err != nil {
		return err
	}
	if len(connections) == 0 {
		return nil
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_mcp_connections}} SET [[revoked_at]] = {:now}, [[retire_after]] = {:retireAfter}
		WHERE [[owner_id]] = {:userId} AND [[revoked_at]] = 0
	`).Bind(dbx.Params{
		"userId": user.Id, "now": now, "retireAfter": now + int64(mcpCredentialRetentionMilliseconds),
	}).Execute(); err != nil {
		return err
	}
	if err := revokeConnectionChildren(app, connections, now); err != nil {
		return err
	}
	notifyMCPCredentialRegistryAfterCommit(app)
	return nil
}
