package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionAdminTokenCursor struct {
	Version        int    `json:"v"`
	Created        int64  `json:"created"`
	OrganizationID string `json:"organizationId"`
	PrincipalID    string `json:"principalId"`
	TokenID        string `json:"tokenId"`
}

const notionAdminTokenSelect = `SELECT [[id]],[[organization_id]],[[label]],[[status]],[[token_prefix]],
	[[token_hash]],[[scopes_json]],[[created_by]],[[last_used_at]],[[expires_at]],[[revoked_at]],
	[[revoked_by]],[[created]],[[updated]] FROM {{leafen_notion_admin_tokens}}`

func insertNotionAdminToken(ctx context.Context, app core.App, stored storedNotionAdminToken) error {
	_, err := app.DB().NewQuery(`INSERT INTO {{leafen_notion_admin_tokens}}
		([[id]],[[organization_id]],[[label]],[[status]],[[token_prefix]],[[token_hash]],[[scopes_json]],
		 [[created_by]],[[last_used_at]],[[expires_at]],[[revoked_at]],[[revoked_by]],[[created]],[[updated]])
		VALUES ({:id},{:organizationId},{:label},{:status},{:tokenPrefix},{:tokenHash},{:scopes},
		 {:createdBy},{:lastUsedAt},{:expiresAt},{:revokedAt},{:revokedBy},{:created},{:updated})`).
		WithContext(ctx).Bind(dbx.Params{
		"id": stored.ID, "organizationId": stored.OrganizationID, "label": stored.Label,
		"status": stored.Status, "tokenPrefix": stored.TokenPrefix, "tokenHash": stored.TokenHash,
		"scopes": stored.ScopesJSON, "createdBy": stored.CreatedBy, "lastUsedAt": stored.LastUsedAt,
		"expiresAt": stored.ExpiresAt, "revokedAt": stored.RevokedAt, "revokedBy": stored.RevokedBy,
		"created": stored.Created, "updated": stored.Updated,
	}).Execute()
	return err
}

func findNotionAdminToken(
	ctx context.Context, app core.App, organizationID, tokenID string,
) (storedNotionAdminToken, error) {
	stored := storedNotionAdminToken{}
	err := app.DB().NewQuery(notionAdminTokenSelect + `
		WHERE [[organization_id]]={:organizationId} AND [[id]]={:id}`).
		WithContext(ctx).Bind(dbx.Params{"organizationId": organizationID, "id": tokenID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotionAdminToken{}, errNotionAdminTokenNotFound
	}
	return stored, err
}

func findNotionAdminTokenLifecycleKey(
	ctx context.Context, app core.App, organizationID, tokenID string,
) (string, error) {
	var tokenHash string
	err := app.DB().NewQuery(`SELECT [[token_hash]] FROM {{leafen_notion_admin_tokens}}
		WHERE [[organization_id]]={:organizationId} AND [[id]]={:id}`).
		WithContext(ctx).Bind(dbx.Params{"organizationId": organizationID, "id": tokenID}).
		Row(&tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotionAdminTokenNotFound
	}
	if err != nil || len(tokenHash) != sha256.Size*2 {
		return "", failedBecause(errNotionAdminTokenUnavailable, err)
	}
	return tokenHash, nil
}

func findNotionAdminTokenRequest(
	ctx context.Context, app core.App, organizationID, actorID, requestID string,
) (storedNotionAdminTokenRequest, bool, error) {
	stored := storedNotionAdminTokenRequest{}
	err := app.DB().NewQuery(`SELECT [[organization_id]],[[actor_id]],[[request_id]],[[operation]],
		[[request_hash]],[[token_id]],[[created]] FROM {{leafen_notion_admin_token_requests}}
		WHERE [[organization_id]]={:organizationId} AND [[actor_id]]={:actorId} AND [[request_id]]={:requestId}`).
		WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID, "actorId": actorID, "requestId": requestID,
	}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotionAdminTokenRequest{}, false, nil
	}
	return stored, err == nil, err
}

func insertNotionAdminTokenRequest(
	ctx context.Context, app core.App, stored storedNotionAdminTokenRequest,
) error {
	_, err := app.DB().NewQuery(`INSERT INTO {{leafen_notion_admin_token_requests}}
		([[organization_id]],[[actor_id]],[[request_id]],[[operation]],[[request_hash]],[[token_id]],[[created]])
		VALUES ({:organizationId},{:actorId},{:requestId},{:operation},{:requestHash},{:tokenId},{:created})`).
		WithContext(ctx).Bind(dbx.Params{
		"organizationId": stored.OrganizationID, "actorId": stored.ActorID,
		"requestId": stored.RequestID, "operation": stored.Operation,
		"requestHash": stored.RequestHash, "tokenId": stored.TokenID, "created": stored.Created,
	}).Execute()
	return err
}

func encodeNotionAdminTokenCursor(
	stored storedNotionAdminToken,
	authorization organizationAuthorization,
) (string, error) {
	encoded, err := json.Marshal(notionAdminTokenCursor{
		Version: notionAdminTokenCursorVersion, Created: stored.Created,
		OrganizationID: authorization.OrganizationID, PrincipalID: authorization.PrincipalID,
		TokenID: stored.ID,
	})
	if err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(encoded)
	if len(value) > notionAdminTokenMaxCursorBytes {
		return "", errNotionAdminTokenCursorInvalid
	}
	return value, nil
}

func decodeNotionAdminTokenCursor(
	raw string,
	authorization organizationAuthorization,
) (notionAdminTokenCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return notionAdminTokenCursor{}, nil
	}
	if len(raw) > notionAdminTokenMaxCursorBytes {
		return notionAdminTokenCursor{}, errNotionAdminTokenCursorInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	cursor := notionAdminTokenCursor{}
	if err != nil || len(decoded) > notionAdminTokenMaxCursorBytes ||
		json.Unmarshal(decoded, &cursor) != nil ||
		cursor.Version != notionAdminTokenCursorVersion || cursor.Created <= 0 ||
		cursor.OrganizationID != authorization.OrganizationID ||
		cursor.PrincipalID != authorization.PrincipalID || cursor.TokenID == "" ||
		utf8.RuneCountInString(cursor.TokenID) > notionAdminTokenMaxResourceIDRunes {
		return notionAdminTokenCursor{}, errNotionAdminTokenCursorInvalid
	}
	return cursor, nil
}
