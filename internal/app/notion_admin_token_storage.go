package app

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func createNotionAdminToken(
	ctx context.Context,
	app core.App,
	authorization organizationAuthorization,
	input notionAdminTokenCreateInput,
) (notionAdminTokenMutationResponse, error) {
	normalized, err := normalizeNotionAdminTokenCreate(input)
	if err != nil {
		return notionAdminTokenMutationResponse{}, err
	}
	release := acquireProductOperationLock(
		"notion-admin-token-create",
		authorization.OrganizationID+"\x00"+authorization.PrincipalID+"\x00"+normalized.RequestID,
	)
	defer release()
	requestHash, err := hashNotionAdminTokenRequest(normalized)
	if err != nil {
		return notionAdminTokenMutationResponse{}, errNotionAdminTokenInvalid
	}
	tokenID := core.GenerateDefaultRandomId()
	credential, err := notionAdminTokenCredential(app, tokenID)
	if err != nil {
		return notionAdminTokenMutationResponse{}, failedBecause(errNotionAdminTokenUnavailable, err)
	}
	stored := storedNotionAdminToken{}
	duplicate := false
	err = app.RunInTransaction(func(txApp core.App) error {
		current, err := authorizeOrganizationContext(
			ctx, txApp, authorization.PrincipalID, authorization.OrganizationID,
			permissionOrganizationOwner,
		)
		if err != nil {
			return err
		}
		receipt, found, err := findNotionAdminTokenRequest(
			ctx, txApp, current.OrganizationID, current.PrincipalID, normalized.RequestID,
		)
		if err != nil {
			return err
		}
		if found {
			if receipt.Operation != notionAdminTokenCreateOperation || receipt.RequestHash != requestHash {
				return errNotionAdminTokenRequestReused
			}
			stored, err = findNotionAdminToken(ctx, txApp, current.OrganizationID, receipt.TokenID)
			duplicate = err == nil
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if normalized.ExpiresAt > 0 && normalized.ExpiresAt <= now {
			return errNotionAdminTokenInvalid
		}
		scopes, err := encodeNotionAdminTokenScopes(notionAdminTokenScopes{
			Version:      notionAdminTokenScopeVersion,
			Capabilities: normalized.Capabilities,
			Resources: notionAdminTokenResources{
				OrganizationID: current.OrganizationID,
				WorkspaceIDs:   normalized.WorkspaceIDs, LegalHoldIDs: normalized.LegalHoldIDs,
			},
		})
		if err != nil {
			return errNotionAdminTokenInvalid
		}
		stored = storedNotionAdminToken{
			ID: tokenID, OrganizationID: current.OrganizationID, Label: normalized.Label,
			Status: notionAdminTokenStatusActive, TokenPrefix: notionAdminTokenLookupPrefix(credential),
			TokenHash: notionAdminTokenHash(credential), ScopesJSON: scopes,
			CreatedBy: current.PrincipalID, ExpiresAt: normalized.ExpiresAt, Created: now, Updated: now,
		}
		if err := insertNotionAdminToken(ctx, txApp, stored); err != nil {
			return err
		}
		if err := recordOrganizationAudit(
			txApp, current.OrganizationID, current.PrincipalID, "notion_admin.token.create",
			"notion_admin_token", stored.ID,
		); err != nil {
			return err
		}
		return insertNotionAdminTokenRequest(ctx, txApp, storedNotionAdminTokenRequest{
			OrganizationID: current.OrganizationID, ActorID: current.PrincipalID,
			RequestID: normalized.RequestID, Operation: notionAdminTokenCreateOperation,
			RequestHash: requestHash, TokenID: stored.ID, Created: now,
		})
	})
	if err != nil {
		return recoverNotionAdminTokenCreate(ctx, app, authorization, normalized.RequestID, requestHash, err)
	}
	if duplicate {
		credential, err = notionAdminTokenCredential(app, stored.ID)
		if err != nil {
			return notionAdminTokenMutationResponse{}, failedBecause(errNotionAdminTokenUnavailable, err)
		}
	}
	response, err := stored.response()
	return notionAdminTokenMutationResponse{
		AdminToken: response, AdminTokenSecret: credential, Duplicate: duplicate,
	}, err
}

func recoverNotionAdminTokenCreate(
	ctx context.Context,
	app core.App,
	authorization organizationAuthorization,
	requestID, requestHash string,
	cause error,
) (notionAdminTokenMutationResponse, error) {
	var stored storedNotionAdminToken
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		current, err := authorizeOrganizationContext(
			ctx, txApp, authorization.PrincipalID, authorization.OrganizationID,
			permissionOrganizationOwner,
		)
		if err != nil {
			return err
		}
		receipt, found, err := findNotionAdminTokenRequest(
			ctx, txApp, current.OrganizationID, current.PrincipalID, requestID,
		)
		if err != nil || !found {
			return err
		}
		if receipt.Operation != notionAdminTokenCreateOperation || receipt.RequestHash != requestHash {
			return errNotionAdminTokenRequestReused
		}
		stored, err = findNotionAdminToken(ctx, txApp, current.OrganizationID, receipt.TokenID)
		return err
	})
	if err != nil || stored.ID == "" {
		return notionAdminTokenMutationResponse{}, classifyNotionAdminTokenError(cause)
	}
	credential, err := notionAdminTokenCredential(app, stored.ID)
	if err != nil {
		return notionAdminTokenMutationResponse{}, failedBecause(errNotionAdminTokenUnavailable, err)
	}
	response, err := stored.response()
	return notionAdminTokenMutationResponse{
		AdminToken: response, AdminTokenSecret: credential, Duplicate: true,
	}, err
}

func listNotionAdminTokens(
	ctx context.Context,
	app core.App,
	authorization organizationAuthorization,
	pageSize int,
	rawCursor string,
) (notionAdminTokenListResponse, error) {
	response := notionAdminTokenListResponse{AdminTokens: []notionAdminTokenResponse{}}
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		current, err := authorizeOrganizationContext(
			ctx, txApp, authorization.PrincipalID, authorization.OrganizationID,
			permissionOrganizationOwner,
		)
		if err != nil {
			return err
		}
		cursor, err := decodeNotionAdminTokenCursor(rawCursor, current)
		if err != nil {
			return err
		}
		params := dbx.Params{"organizationId": current.OrganizationID, "limit": pageSize + 1}
		condition := ""
		if cursor.TokenID != "" {
			params["cursorCreated"], params["cursorId"] = cursor.Created, cursor.TokenID
			condition = " AND ([[created]] < {:cursorCreated} OR ([[created]] = {:cursorCreated} AND [[id]] < {:cursorId}))"
		}
		rows := []storedNotionAdminToken{}
		if err := txApp.DB().NewQuery(notionAdminTokenSelect + `
			WHERE [[organization_id]]={:organizationId}` + condition + `
			ORDER BY [[created]] DESC, [[id]] DESC LIMIT {:limit}`).
			WithContext(ctx).Bind(params).All(&rows); err != nil {
			return err
		}
		response.HasMore = len(rows) > pageSize
		if response.HasMore {
			rows = rows[:pageSize]
		}
		for _, row := range rows {
			item, err := row.response()
			if err != nil {
				return err
			}
			response.AdminTokens = append(response.AdminTokens, item)
		}
		if response.HasMore {
			encoded, err := encodeNotionAdminTokenCursor(rows[len(rows)-1], current)
			if err != nil {
				return err
			}
			response.NextCursor = &encoded
		}
		return nil
	})
	return response, classifyNotionAdminTokenError(err)
}

func revokeNotionAdminToken(
	ctx context.Context,
	app core.App,
	authorization organizationAuthorization,
	tokenID string,
	input notionAdminTokenRevokeInput,
) (notionAdminTokenMutationResponse, error) {
	tokenID, requestID := strings.TrimSpace(tokenID), strings.TrimSpace(input.RequestID)
	if tokenID == "" || requestID == "" ||
		utf8.RuneCountInString(tokenID) > notionAdminTokenMaxResourceIDRunes ||
		utf8.RuneCountInString(requestID) > notionAdminTokenMaxRequestIDRunes {
		return notionAdminTokenMutationResponse{}, errNotionAdminTokenInvalid
	}
	release := acquireProductOperationLock(
		"notion-admin-token-revoke",
		authorization.OrganizationID+"\x00"+authorization.PrincipalID+"\x00"+requestID,
	)
	defer release()
	lifecycleKey, err := findNotionAdminTokenLifecycleKey(
		ctx, app, authorization.OrganizationID, tokenID,
	)
	if err != nil {
		return notionAdminTokenMutationResponse{}, classifyNotionAdminTokenError(err)
	}
	releaseLifecycle := acquireNotionAdminTokenLifecycleLock(lifecycleKey)
	defer releaseLifecycle()
	requestHash, _ := hashNotionAdminTokenRequest(struct {
		Operation, TokenID string
	}{notionAdminTokenRevokeOperation, tokenID})
	stored := storedNotionAdminToken{}
	duplicate := false
	err = app.RunInTransaction(func(txApp core.App) error {
		current, err := authorizeOrganizationContext(
			ctx, txApp, authorization.PrincipalID, authorization.OrganizationID,
			permissionOrganizationOwner,
		)
		if err != nil {
			return err
		}
		receipt, found, err := findNotionAdminTokenRequest(
			ctx, txApp, current.OrganizationID, current.PrincipalID, requestID,
		)
		if err != nil {
			return err
		}
		if found {
			if receipt.Operation != notionAdminTokenRevokeOperation ||
				receipt.RequestHash != requestHash || receipt.TokenID != tokenID {
				return errNotionAdminTokenRequestReused
			}
			stored, err = findNotionAdminToken(ctx, txApp, current.OrganizationID, tokenID)
			duplicate = err == nil
			return err
		}
		stored, err = findNotionAdminToken(ctx, txApp, current.OrganizationID, tokenID)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if stored.Status == notionAdminTokenStatusActive {
			result, err := txApp.DB().NewQuery(`UPDATE {{leafen_notion_admin_tokens}}
				SET [[status]]={:status},[[revoked_at]]={:revokedAt},[[revoked_by]]={:revokedBy},[[updated]]={:updated}
				WHERE [[id]]={:id} AND [[organization_id]]={:organizationId} AND [[status]]={:active}`).
				WithContext(ctx).Bind(dbx.Params{
				"status": notionAdminTokenStatusRevoked, "revokedAt": now,
				"revokedBy": current.PrincipalID, "updated": now, "id": stored.ID,
				"organizationId": current.OrganizationID, "active": notionAdminTokenStatusActive,
			}).Execute()
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil || changed != 1 {
				return failedBecause(errNotionAdminTokenUnavailable, err)
			}
			stored.Status, stored.RevokedAt, stored.RevokedBy, stored.Updated =
				notionAdminTokenStatusRevoked, now, current.PrincipalID, now
			if err := recordOrganizationAudit(
				txApp, current.OrganizationID, current.PrincipalID, "notion_admin.token.revoke",
				"notion_admin_token", stored.ID,
			); err != nil {
				return err
			}
		}
		return insertNotionAdminTokenRequest(ctx, txApp, storedNotionAdminTokenRequest{
			OrganizationID: current.OrganizationID, ActorID: current.PrincipalID,
			RequestID: requestID, Operation: notionAdminTokenRevokeOperation,
			RequestHash: requestHash, TokenID: stored.ID, Created: now,
		})
	})
	if err != nil {
		return notionAdminTokenMutationResponse{}, classifyNotionAdminTokenError(err)
	}
	response, err := stored.response()
	return notionAdminTokenMutationResponse{AdminToken: response, Duplicate: duplicate}, err
}
