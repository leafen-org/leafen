package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	notionOAuthAuthorizationLifetime = 10 * time.Minute
	maxNotionOAuthResources          = 1000
)

var notionOAuthDefaultScopes = []string{
	"pages:read", "pages:write", "databases:read", "databases:write",
	"comments:read", "comments:write", "files:read", "files:write", "workspace:read",
}

var notionOAuthScopeSet = func() map[string]struct{} {
	result := make(map[string]struct{}, len(notionOAuthDefaultScopes))
	for _, scope := range notionOAuthDefaultScopes {
		result[scope] = struct{}{}
	}
	return result
}()

type notionOAuthAuthorizationInput struct {
	RequestID   string   `json:"requestId"`
	ClientID    string   `json:"clientId"`
	RedirectURI string   `json:"redirectUri"`
	WorkspaceID string   `json:"workspaceId"`
	State       string   `json:"state"`
	Scopes      []string `json:"scopes"`
	ResourceIDs []string `json:"resourceIds"`
}

type notionOAuthAuthorizationResponse struct {
	Code        string `json:"code"`
	GrantID     string `json:"grantId"`
	WorkspaceID string `json:"workspaceId"`
	ExpiresAt   int64  `json:"expiresAt"`
	RedirectURL string `json:"redirectUrl"`
}

type storedNotionOAuthAuthorization struct {
	ID              string `db:"id"`
	RequestHash     string `db:"request_hash"`
	GrantID         string `db:"grant_id"`
	GrantGeneration int    `db:"grant_generation"`
	ClientID        string `db:"client_id"`
	RedirectURI     string `db:"redirect_uri"`
	State           string `db:"state"`
	ExpiresAt       int64  `db:"expires_at"`
	Status          string `db:"status"`
	RefreshTokenID  string `db:"refresh_token_id"`
}

type storedNotionOAuthGrant struct {
	ID                   string `db:"id"`
	OwnerID              string `db:"owner_id"`
	ClientID             string `db:"client_id"`
	ClientName           string `db:"client_name"`
	WorkspaceID          string `db:"workspace_id"`
	Scope                string `db:"scope"`
	CredentialGeneration int    `db:"credential_generation"`
	Status               string `db:"status"`
	RevokedAt            int64  `db:"revoked_at"`
}

type storedNotionOAuthResource struct {
	ID   string `db:"id"`
	Kind string `db:"kind"`
}

func normalizeNotionOAuthScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return append([]string{}, notionOAuthDefaultScopes...), nil
	}
	normalized := uniqueNonemptyStrings(scopes)
	if len(normalized) == 0 {
		return nil, notionOAuthFailure(http.StatusBadRequest, "invalid_scope", "At least one supported scope is required.")
	}
	for _, scope := range normalized {
		if _, ok := notionOAuthScopeSet[scope]; !ok {
			return nil, notionOAuthFailure(http.StatusBadRequest, "invalid_scope", "One or more requested scopes are not supported.")
		}
	}
	sort.Strings(normalized)
	return normalized, nil
}

func notionOAuthAuthorizationRequestHash(input notionOAuthAuthorizationInput, scopes, resources []string) string {
	encoded, _ := json.Marshal([]any{
		"notion-oauth-authorization:v1", input.ClientID, input.RedirectURI,
		input.WorkspaceID, input.State, scopes, resources,
	})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func notionOAuthAuthorizationRedirect(redirectURI, code, state string) (string, error) {
	parsed, err := url.Parse(redirectURI)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("code", code)
	if state != "" {
		query.Set("state", state)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func issueNotionOAuthAuthorizationCode(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	var input notionOAuthAuthorizationInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, "LEAFEN_NOTION_OAUTH_REQUEST_INVALID", "Check the Notion connection request.")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.ClientID = strings.TrimSpace(input.ClientID)
	input.RedirectURI = strings.TrimSpace(input.RedirectURI)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.State = strings.TrimSpace(input.State)
	resources := uniqueNonemptyStrings(input.ResourceIDs)
	if input.RequestID == "" || len(input.RequestID) > 255 || input.ClientID == "" || input.WorkspaceID == "" ||
		len(input.State) > 1024 || len(resources) > maxNotionOAuthResources {
		return apiError(event, http.StatusBadRequest, "LEAFEN_NOTION_OAUTH_REQUEST_INVALID", "Check the Notion connection request.")
	}
	client, err := resolveNotionOAuthClient(input.ClientID)
	if err != nil {
		return apiError(event, http.StatusBadRequest, "LEAFEN_NOTION_OAUTH_REQUEST_INVALID", "Registered Notion connection not found.")
	}
	if err := validateNotionOAuthRedirectURI(client, input.RedirectURI); err != nil {
		return apiError(event, http.StatusBadRequest, "LEAFEN_NOTION_OAUTH_REQUEST_INVALID", "Check the Notion return URL.")
	}
	scopes, err := normalizeNotionOAuthScopes(input.Scopes)
	if err != nil {
		return apiError(event, http.StatusBadRequest, "LEAFEN_NOTION_OAUTH_REQUEST_INVALID", "Check the Notion permission scope.")
	}
	sort.Strings(resources)
	requestHash := notionOAuthAuthorizationRequestHash(input, scopes, resources)
	newCodeID := security.RandomString(24)
	newGrantID := security.RandomString(24)
	newCode, err := notionOAuthDeterministicCredential(event.App, "authorization-code", newCodeID, "lnc_")
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, "LEAFEN_NOTION_OAUTH_FAILED", "Couldn't approve the Notion connection.", err)
	}
	now := time.Now().UnixMilli()
	selected := storedNotionOAuthAuthorization{}
	conflict := false
	// refused is which isn't there or can't be chosen: the workspace, a
	// page chosen (refusedPage), or the person a restricted member
	// (2026-11-26: all were "Workspace or selected page not found.", its
	// target the workspace).
	var refused error
	refusedPage := ""
	// formerBotName is the connection's name before connecting again under
	// its client's name now, which its bot's mentions take.
	formerBotName := ""
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if err := authorizeWorkspaceSet(txApp, user.Id, []string{input.WorkspaceID}, permissionWorkspaceRead); err != nil {
			if isPermissionDenied(err) {
				refused = refusedFor(http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
				return nil
			}
			return err
		}
		// A restricted member connects no app, as Notion's has no "연결".
		if restricted, err := workspaceMemberRestricted(txApp, input.WorkspaceID, user.Id); err != nil || restricted {
			if restricted {
				refused = refusedFor(http.StatusForbidden, errorCodePermissionDenied, "A restricted member of this workspace "+
					"connects no app to it; one of its members or owners does.")
			}
			return err
		}
		if len(resources) > 0 {
			if _, err := authorizePageSet(txApp, user.Id, input.WorkspaceID, resources, permissionWorkspaceRead); err != nil {
				if isPermissionDenied(err) {
					refusedPage = notionOAuthPageRefused(txApp, user.Id, input.WorkspaceID, resources)
					return nil
				}
				return err
			}
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_notion_oauth_authorization_codes}}
			WHERE [[owner_id]]={:ownerId} AND [[expires_at]] <= {:now}
		`).Bind(dbx.Params{"ownerId": user.Id, "now": now}).Execute(); err != nil {
			return err
		}
		existing, found, err := findNotionOAuthAuthorizationRequest(txApp, user.Id, input.RequestID)
		if err != nil {
			return err
		}
		if found {
			if existing.RequestHash != requestHash {
				conflict = true
				return nil
			}
			selected = existing
			return nil
		}

		grant, found, err := findNotionOAuthGrantTuple(txApp, user.Id, client.ID, input.WorkspaceID)
		if err != nil {
			return err
		}
		if !found {
			grant = storedNotionOAuthGrant{ID: newGrantID, CredentialGeneration: 1}
			if _, err := txApp.DB().Insert(notionOAuthGrantsTable, dbx.Params{
				"id": newGrantID, "owner_id": user.Id, "client_id": client.ID,
				"client_name": client.Name, "workspace_id": input.WorkspaceID,
				"scope": strings.Join(scopes, " "), "credential_generation": grant.CredentialGeneration,
				"status":     "active",
				"created_at": now, "updated_at": now, "revoked_at": 0,
			}).Execute(); err != nil {
				return err
			}
		} else {
			if _, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_notion_oauth_authorization_codes}} WHERE [[grant_id]]={:grantId}
			`).Bind(dbx.Params{"grantId": grant.ID}).Execute(); err != nil {
				return err
			}
			if _, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_notion_oauth_refresh_tokens}} WHERE [[grant_id]]={:grantId}
			`).Bind(dbx.Params{"grantId": grant.ID}).Execute(); err != nil {
				return err
			}
			grant.CredentialGeneration++
			if grant.ClientName != client.Name {
				formerBotName = grant.ClientName
			}
			if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_oauth_grants}}
			SET [[client_name]]={:clientName}, [[scope]]={:scope},
				[[credential_generation]]={:credentialGeneration}, [[status]]='active',
				[[updated_at]]={:updatedAt}, [[revoked_at]]=0
			WHERE [[id]]={:id}
		`).Bind(dbx.Params{
				"clientName": client.Name, "scope": strings.Join(scopes, " "),
				"credentialGeneration": grant.CredentialGeneration,
				"updatedAt":            now, "id": grant.ID,
			}).Execute(); err != nil {
				return err
			}
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_notion_oauth_grant_resources}} WHERE [[grant_id]]={:grantId}
		`).Bind(dbx.Params{"grantId": grant.ID}).Execute(); err != nil {
			return err
		}
		// A connection whose client takes webhooks starts its subscription.
		notifyEventWebhookRunnerAfterCommit(txApp)
		if len(resources) > 0 {
			rows := loadNotionOAuthResourceKinds(resources)
			for _, row := range rows {
				kind := "page"
				if row.Kind == "database" {
					kind = "database"
				}
				if _, err := txApp.DB().Insert(notionOAuthResourcesTable, dbx.Params{
					"grant_id": grant.ID, "resource_id": row.ID, "resource_kind": kind,
				}).Execute(); err != nil {
					return err
				}
			}
		}
		if _, err := txApp.DB().Insert(notionOAuthCodesTable, dbx.Params{
			"id": newCodeID, "owner_id": user.Id, "request_id": input.RequestID,
			"request_hash": requestHash, "code_hash": notionOAuthTokenHash(newCode),
			"grant_id": grant.ID, "grant_generation": grant.CredentialGeneration,
			"client_id": client.ID, "redirect_uri": input.RedirectURI,
			"state": input.State, "status": "active", "created_at": now,
			"expires_at":   now + notionOAuthAuthorizationLifetime.Milliseconds(),
			"completed_at": 0, "refresh_token_id": "",
		}).Execute(); err != nil {
			return err
		}
		selected = storedNotionOAuthAuthorization{
			ID: newCodeID, RequestHash: requestHash, GrantID: grant.ID,
			GrantGeneration: grant.CredentialGeneration, ClientID: client.ID,
			RedirectURI: input.RedirectURI, State: input.State,
			ExpiresAt: now + notionOAuthAuthorizationLifetime.Milliseconds(), Status: "active",
		}
		return nil
	})
	if refusedPage != "" {
		apiErrorAbout(event, &apiErrorTarget{Kind: "page", ID: refusedPage},
			apiErrorField{Name: "resourceIds", Problem: refusedPage + " isn't a page you can open there"})
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Check resourceIds: the page "+refusedPage+
			" isn't one you can open in that workspace.")
	}
	if refused != nil {
		return apiErrorFrom(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.", refused)
	}
	if conflict {
		return apiError(event, http.StatusConflict, "LEAFEN_NOTION_OAUTH_REQUEST_CONFLICT", "This Notion connection request was already processed with a different scope.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, "LEAFEN_NOTION_OAUTH_FAILED", "Couldn't approve the Notion connection.", err)
	}
	if formerBotName != "" {
		refreshBotMentionsSoon(event.App, input.WorkspaceID, notionOAuthBotID(selected.GrantID), formerBotName)
	}
	code, err := notionOAuthDeterministicCredential(event.App, "authorization-code", selected.ID, "lnc_")
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, "LEAFEN_NOTION_OAUTH_FAILED", "Couldn't approve the Notion connection.", err)
	}
	redirect, err := notionOAuthAuthorizationRedirect(selected.RedirectURI, code, selected.State)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, "LEAFEN_NOTION_OAUTH_FAILED", "Couldn't approve the Notion connection.", err)
	}
	return event.JSON(http.StatusOK, notionOAuthAuthorizationResponse{
		Code: code, GrantID: selected.GrantID, WorkspaceID: input.WorkspaceID,
		ExpiresAt: selected.ExpiresAt, RedirectURL: redirect,
	})
}

func findNotionOAuthAuthorizationRequest(app core.App, ownerID, requestID string) (storedNotionOAuthAuthorization, bool, error) {
	stored := storedNotionOAuthAuthorization{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[request_hash]], [[grant_id]], [[grant_generation]], [[client_id]], [[redirect_uri]], [[state]],
			[[expires_at]], [[status]], [[refresh_token_id]]
		FROM {{leafen_notion_oauth_authorization_codes}}
		WHERE [[owner_id]]={:ownerId} AND [[request_id]]={:requestId} LIMIT 1
	`).Bind(dbx.Params{"ownerId": ownerID, "requestId": requestID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotionOAuthAuthorization{}, false, nil
	}
	return stored, err == nil, err
}

func findNotionOAuthGrantTuple(app core.App, ownerID, clientID, workspaceID string) (storedNotionOAuthGrant, bool, error) {
	stored := storedNotionOAuthGrant{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[client_id]], [[client_name]], [[workspace_id]], [[scope]],
			[[credential_generation]],
			[[status]], [[revoked_at]]
		FROM {{leafen_notion_oauth_grants}}
		WHERE [[owner_id]]={:ownerId} AND [[client_id]]={:clientId}
			AND [[workspace_id]]={:workspaceId} LIMIT 1
	`).Bind(dbx.Params{
		"ownerId": ownerID, "clientId": clientID, "workspaceId": workspaceID,
	}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotionOAuthGrant{}, false, nil
	}
	return stored, err == nil, err
}

// grantConnectionTopLevelPage gives a connection the page it made at the top
// of the workspace, as Notion gives an integration the pages it creates; a
// page it makes under one it reaches it reaches already.
func grantConnectionTopLevelPage(app core.App, identity notionOAuthBearerIdentity, parentID, pageID string) error {
	if !identity.PageScoped || identity.GrantID == "" || parentID != "" || pageID == "" {
		return nil
	}
	_, err := app.DB().NewQuery(`
		INSERT OR IGNORE INTO {{leafen_notion_oauth_grant_resources}} ([[grant_id]], [[resource_id]], [[resource_kind]])
		VALUES ({:grantId}, {:pageId}, 'page')
	`).Bind(dbx.Params{"grantId": identity.GrantID, "pageId": pageID}).Execute()
	return err
}

func loadNotionOAuthResourceKinds(resourceIDs []string) []storedNotionOAuthResource {
	rows := make([]storedNotionOAuthResource, 0, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		rows = append(rows, storedNotionOAuthResource{ID: resourceID, Kind: "page"})
	}
	return rows
}

// notionOAuthPageRefused is the first page chosen the person can't open in
// the workspace.
func notionOAuthPageRefused(app core.App, userID, workspaceID string, resources []string) string {
	for _, resource := range resources {
		if _, err := authorizePageSet(app, userID, workspaceID, []string{resource}, permissionWorkspaceRead); isPermissionDenied(err) {
			return resource
		}
	}
	return resources[0]
}
