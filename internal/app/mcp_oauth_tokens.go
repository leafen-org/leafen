package app

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/sync/singleflight"
)

// An OAuth connection's access token lasts an hour; its refresh token, which
// changes at every refresh (mcp_oauth_refresh_rotation.go), lasts 180 days
// from its last refresh, so an agent that goes on using Leafen stays
// connected with nothing for its person to do again (agent key decision of
// 2026-11-16 — Notion's MCP refresh tokens last 180 days from the sign-in),
// and one unused for that long signs in again.
const (
	mcpOAuthAccessLifetime  = time.Hour
	mcpOAuthRefreshLifetime = 180 * 24 * time.Hour
)

var mcpPKCEVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

type mcpOAuthRuntime struct {
	credentialCompletion *mcpCredentialCompletionRuntime
	refresh              singleflight.Group
}

type mcpOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

type storedMCPOAuthConnection struct {
	ID               string `db:"id"`
	OwnerID          string `db:"owner_id"`
	ClientName       string `db:"client_name"`
	TokenHash        string `db:"token_hash"`
	TokenExpiresAt   int64  `db:"token_expires_at"`
	RefreshTokenHash string `db:"refresh_token_hash"`
	RefreshExpiresAt int64  `db:"refresh_expires_at"`
	OAuthClientID    string `db:"oauth_client_id"`
	OAuthResource    string `db:"oauth_resource"`
	OAuthScope       string `db:"oauth_scope"`
	RevokedAt        int64  `db:"revoked_at"`
}

type mcpOAuthTokenResult struct {
	response mcpOAuthTokenResponse
	code     string
	status   int
	err      error
}

// The words a token request that failed on the server's side is told by.
var (
	errMCPOAuthExchangeFailed = errors.New("token exchange failed")
	errMCPOAuthRefreshFailed  = errors.New("token refresh failed")
)

// mcpOAuthTokenFailure answers a token request that failed: one that failed
// on the server's side through the rules of every failure's answer
// (oauthServerError), so a database that stayed busy is told busy.
func mcpOAuthTokenFailure(event *core.RequestEvent, result mcpOAuthTokenResult) error {
	if result.status == http.StatusInternalServerError {
		return oauthServerError(event, result.err.Error(), result.err)
	}
	return oauthProtocolError(event, result.status, result.code, result.err.Error())
}

func exchangeMCPOAuthToken(event *core.RequestEvent, runtime *mcpOAuthRuntime) error {
	if err := event.Request.ParseForm(); err != nil {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request", "invalid token request")
	}
	switch event.Request.Form.Get("grant_type") {
	case "authorization_code":
		return exchangeMCPOAuthAuthorizationCode(event, runtime)
	case "refresh_token":
		return refreshMCPOAuthAccessToken(event, runtime)
	default:
		return oauthProtocolError(event, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant type")
	}
}

func exchangeMCPOAuthAuthorizationCode(event *core.RequestEvent, runtime *mcpOAuthRuntime) error {
	form := event.Request.Form
	code := strings.TrimSpace(form.Get("code"))
	clientID := strings.TrimSpace(form.Get("client_id"))
	redirectURI := strings.TrimSpace(form.Get("redirect_uri"))
	resource := strings.TrimSpace(form.Get("resource"))
	verifier := strings.TrimSpace(form.Get("code_verifier"))
	if code == "" || clientID == "" || redirectURI == "" || resource == "" || !mcpPKCEVerifierPattern.MatchString(verifier) {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_grant", "invalid authorization code")
	}
	client, err := findMCPOAuthClient(event.App, clientID)
	if err != nil || !clientAllowsRedirect(client, redirectURI) {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_grant", "invalid authorization code")
	}
	if !mcpOAuthResourceKnown(event.Request, resource) {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_target", "invalid resource")
	}

	result := runtime.credentialCompletion.completeAuthorization(
		event.App, client, code, verifier, redirectURI, resource,
	)
	if result.code == errorCodeMCPConnectionCapacity {
		return oauthProtocolErrorWithProductCode(
			event, result.status, "invalid_request", result.code, result.err.Error(),
		)
	}
	if result.code != "" {
		return mcpOAuthTokenFailure(event, result)
	}
	return writeMCPOAuthTokenResponse(event, result.response)
}

func refreshMCPOAuthAccessToken(event *core.RequestEvent, runtime *mcpOAuthRuntime) error {
	form := event.Request.Form
	refreshToken := strings.TrimSpace(form.Get("refresh_token"))
	clientID := strings.TrimSpace(form.Get("client_id"))
	resource := strings.TrimSpace(form.Get("resource"))
	if refreshToken == "" || clientID == "" {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request", "refresh_token and client_id are required")
	}
	// A refresh may leave the resource out and keeps the one it was granted
	// for (RFC 8707, 2.2).
	if resource != "" && !mcpOAuthResourceKnown(event.Request, resource) {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_target", "invalid resource")
	}
	refreshHash := mcpTokenHash(refreshToken)
	value, _, _ := runtime.refresh.Do(refreshHash, func() (any, error) {
		return rotateMCPOAuthAccessToken(event.App, refreshHash, clientID, resource), nil
	})
	result := value.(mcpOAuthTokenResult)
	if result.code != "" {
		return mcpOAuthTokenFailure(event, result)
	}
	return writeMCPOAuthTokenResponse(event, result.response)
}

func rotateMCPOAuthAccessToken(
	app core.App,
	refreshHash string,
	clientID string,
	resource string,
) mcpOAuthTokenResult {
	response := mcpOAuthTokenResponse{}
	invalid := false
	err := app.RunInTransaction(func(txApp core.App) error {
		connection, reused, err := findMCPOAuthConnectionForRefresh(txApp, refreshHash)
		if errors.Is(err, sql.ErrNoRows) {
			invalid = true
			return nil
		}
		if err != nil {
			return err
		}
		if reused || connection.RevokedAt != 0 || connection.RefreshExpiresAt <= time.Now().UnixMilli() ||
			connection.OAuthClientID != clientID ||
			(resource != "" && !sameServerResource(connection.OAuthResource, resource)) ||
			!oauthScopeFitsResource(connection.OAuthResource, connection.OAuthScope) {
			invalid = true
			return nil
		}
		workspaceIDs, err := loadMCPConnectionWorkspaces(txApp, connection.ID)
		if err != nil {
			return err
		}
		if len(workspaceIDs) == 0 {
			invalid = true
			now := time.Now().UnixMilli()
			_, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_mcp_connections}}
				SET [[revoked_at]] = {:now}, [[retire_after]] = {:retireAfter}
				WHERE [[id]] = {:id} AND [[revoked_at]] = 0
			`).Bind(dbx.Params{
				"now": now, "retireAfter": now + int64(mcpCredentialRetentionMilliseconds),
				"id": connection.ID,
			}).Execute()
			if err == nil {
				notifyMCPCredentialRegistryAfterCommit(txApp)
			}
			return err
		}
		if err := authorizeConnectionWorkspaces(txApp, connection.OwnerID, workspaceIDs); err != nil {
			if !isPermissionDenied(err) {
				return err
			}
			invalid = true
			now := time.Now().UnixMilli()
			_, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_mcp_connections}}
				SET [[revoked_at]] = {:now}, [[retire_after]] = {:retireAfter}
				WHERE [[id]] = {:id} AND [[revoked_at]] = 0
			`).Bind(dbx.Params{
				"now": now, "retireAfter": now + int64(mcpCredentialRetentionMilliseconds),
				"id": connection.ID,
			}).Execute()
			if err == nil {
				notifyMCPCredentialRegistryAfterCommit(txApp)
			}
			return err
		}
		accessToken, accessHash, err := newHashedMCPToken("lmo_")
		if err != nil {
			return err
		}
		nextRefresh, nextRefreshHash, err := newHashedMCPToken("lmr_")
		if err != nil {
			return err
		}
		expiresAt := time.Now().Add(mcpOAuthAccessLifetime).UnixMilli()
		refreshExpiresAt := time.Now().Add(mcpOAuthRefreshLifetime).UnixMilli()
		// A token granted at an earlier address is given for the address it
		// forwards to from now on (2026-10-20).
		granted := connection.OAuthResource
		if moved := movedResource(granted); moved != "" {
			granted = moved
		}
		// The refresh token is replaced too, and the one replaced kept as
		// retired; a retry within the grace replaces the pair it lost.
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_mcp_connections}}
			SET [[token_hash]] = {:tokenHash}, [[token_expires_at]] = {:expiresAt}, [[oauth_resource]] = {:resource},
				[[refresh_token_hash]] = {:nextRefreshHash}, [[refresh_expires_at]] = {:refreshExpiresAt},
				[[retire_after]] = {:retireAfter}
			WHERE [[id]] = {:id} AND [[refresh_token_hash]] = {:refreshHash} AND [[revoked_at]] = 0
		`).Bind(dbx.Params{
			"tokenHash": accessHash, "expiresAt": expiresAt, "resource": granted, "nextRefreshHash": nextRefreshHash,
			"refreshExpiresAt": refreshExpiresAt, "retireAfter": refreshExpiresAt + int64(mcpCredentialRetentionMilliseconds),
			"id": connection.ID, "refreshHash": connection.RefreshTokenHash,
		}).Execute(); err != nil {
			return err
		}
		if err := retireMCPOAuthRefresh(txApp, connection.RefreshTokenHash, connection.ID, time.Now().UnixMilli()); err != nil {
			return err
		}
		notifyMCPCredentialRegistryAfterCommit(txApp)
		response = newMCPOAuthTokenResponse(accessToken, nextRefresh)
		response.Scope = connection.OAuthScope
		return nil
	})
	if invalid {
		return mcpOAuthTokenResult{code: "invalid_grant", status: http.StatusBadRequest, err: errors.New("invalid refresh token")}
	}
	if err != nil {
		return mcpOAuthTokenResult{code: "server_error", status: http.StatusInternalServerError, err: failedBecause(errMCPOAuthRefreshFailed, err)}
	}
	return mcpOAuthTokenResult{response: response}
}

func revokeMCPOAuthToken(event *core.RequestEvent) error {
	if err := event.Request.ParseForm(); err != nil {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request", "invalid revocation request")
	}
	tokenHash := mcpTokenHash(strings.TrimSpace(event.Request.Form.Get("token")))
	clientID := strings.TrimSpace(event.Request.Form.Get("client_id"))
	if clientID != "" {
		now := time.Now().UnixMilli()
		_, err := event.App.DB().NewQuery(`
			UPDATE {{leafen_mcp_connections}}
			SET [[revoked_at]] = CASE WHEN [[revoked_at]] = 0 THEN {:now} ELSE [[revoked_at]] END,
				[[retire_after]] = CASE WHEN [[revoked_at]] = 0 THEN {:retireAfter} ELSE [[retire_after]] END
			WHERE [[oauth_client_id]] = {:clientId}
				AND ([[token_hash]] = {:tokenHash} OR [[refresh_token_hash]] = {:tokenHash})
		`).Bind(dbx.Params{
			"now": now, "retireAfter": now + int64(mcpCredentialRetentionMilliseconds),
			"clientId": clientID, "tokenHash": tokenHash,
		}).Execute()
		if err != nil {
			return oauthServerError(event, "revocation failed", err)
		}
		// What the revoked connection made ends with it.
		revoked := []string{}
		if err := event.App.DB().NewQuery(`SELECT [[id]] FROM {{leafen_mcp_connections}}
			WHERE [[oauth_client_id]] = {:clientId} AND ([[token_hash]] = {:tokenHash} OR [[refresh_token_hash]] = {:tokenHash})`).
			Bind(dbx.Params{"clientId": clientID, "tokenHash": tokenHash}).Column(&revoked); err != nil {
			return oauthServerError(event, "revocation failed", err)
		}
		if err := revokeConnectionChildren(event.App, revoked, now); err != nil {
			return oauthServerError(event, "revocation failed", err)
		}
		notifyMCPCredentialRegistryAfterCommit(event.App)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	return event.JSON(http.StatusOK, map[string]any{})
}

func findMCPOAuthConnectionByRefresh(app core.App, refreshHash string) (storedMCPOAuthConnection, error) {
	var connection storedMCPOAuthConnection
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[client_name]], [[token_hash]], [[token_expires_at]],
			[[refresh_token_hash]], [[refresh_expires_at]], [[oauth_client_id]], [[oauth_resource]],
			[[oauth_scope]], [[revoked_at]]
		FROM {{leafen_mcp_connections}}
		WHERE [[refresh_token_hash]] = {:refreshHash} AND [[refresh_token_hash]] <> ''
	`).Bind(dbx.Params{"refreshHash": refreshHash}).One(&connection)
	return connection, err
}

func loadMCPOAuthAuthorizationWorkspaces(app core.App, authorizationID string) ([]string, error) {
	rows := []storedMCPWorkspace{}
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]] FROM {{leafen_mcp_oauth_authorization_workspaces}}
		WHERE [[authorization_id]] = {:authorizationId} ORDER BY [[workspace_id]]
	`).Bind(dbx.Params{"authorizationId": authorizationID}).All(&rows)
	workspaceIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		workspaceIDs = append(workspaceIDs, row.WorkspaceID)
	}
	return workspaceIDs, err
}

func loadMCPConnectionWorkspaces(app core.App, connectionID string) ([]string, error) {
	rows := []storedMCPWorkspace{}
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]] FROM {{leafen_mcp_connection_workspaces}}
		WHERE [[connection_id]] = {:connectionId} ORDER BY [[workspace_id]]
	`).Bind(dbx.Params{"connectionId": connectionID}).All(&rows)
	workspaceIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		workspaceIDs = append(workspaceIDs, row.WorkspaceID)
	}
	return workspaceIDs, err
}

func newMCPOAuthTokenResponse(accessToken string, refreshToken string) mcpOAuthTokenResponse {
	return newMCPOAuthTokenResponseWithExpiry(
		accessToken, refreshToken, int64(mcpOAuthAccessLifetime/time.Second),
	)
}

func newMCPOAuthTokenResponseWithExpiry(accessToken string, refreshToken string, expiresIn int64) mcpOAuthTokenResponse {
	return mcpOAuthTokenResponse{
		AccessToken: accessToken, TokenType: "Bearer", ExpiresIn: expiresIn,
		RefreshToken: refreshToken, Scope: mcpOAuthScope,
	}
}

func writeMCPOAuthTokenResponse(event *core.RequestEvent, response mcpOAuthTokenResponse) error {
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	return event.JSON(http.StatusOK, response)
}
