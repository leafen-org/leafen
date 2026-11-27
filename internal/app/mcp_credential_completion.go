package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"golang.org/x/sync/singleflight"
)

var (
	errMCPConnectionRequestIDReused = errors.New(errorCodeMCPConnectionRequestIDReused)
	errMCPOAuthInvalidGrant         = errors.New("invalid MCP OAuth authorization grant")
)

type mcpCredentialCompletionRuntime struct {
	manual        singleflight.Group
	authorization singleflight.Group
}

type mcpManualCompletionResult struct {
	response mcpConnectionResponse
	status   int
	err      error
}

type storedMCPCredentialCompletion struct {
	ID                    string `db:"id"`
	OwnerID               string `db:"owner_id"`
	ClientName            string `db:"client_name"`
	Created               int64  `db:"created"`
	TokenHash             string `db:"token_hash"`
	TokenExpiresAt        int64  `db:"token_expires_at"`
	RefreshTokenHash      string `db:"refresh_token_hash"`
	RefreshExpiresAt      int64  `db:"refresh_expires_at"`
	OAuthClientID         string `db:"oauth_client_id"`
	OAuthResource         string `db:"oauth_resource"`
	OAuthScope            string `db:"oauth_scope"`
	RequestHash           string `db:"request_hash"`
	AuthorizationCodeHash string `db:"authorization_code_hash"`
	ExchangeRequestHash   string `db:"exchange_request_hash"`
	RevokedAt             int64  `db:"revoked_at"`
}

func (runtime *mcpCredentialCompletionRuntime) completeManual(
	app core.App,
	ownerID string,
	input mcpConnectionInput,
	endpoint string,
) mcpManualCompletionResult {
	requestHash, err := mcpManualCompletionRequestHash(input)
	if err != nil {
		return mcpManualCompletionResult{err: err}
	}
	secret, err := mcpOAuthSecret(app)
	if err != nil {
		return mcpManualCompletionResult{err: err}
	}
	key := ownerID + "\x00" + input.RequestID + "\x00" + requestHash
	value, _, _ := runtime.manual.Do(key, func() (any, error) {
		return completeManualMCPConnection(app, ownerID, input, requestHash, endpoint, secret), nil
	})
	return value.(mcpManualCompletionResult)
}

func completeManualMCPConnection(
	app core.App,
	ownerID string,
	input mcpConnectionInput,
	requestHash string,
	endpoint string,
	secret string,
) mcpManualCompletionResult {
	connectionID := security.RandomString(15)
	created := time.Now().UnixMilli()
	// An agent connection reaches all its person's workspaces
	// (connection_person_rights.go).
	agent := agentConnectionRequest(input.RequestID)
	if agent {
		workspaceIDs, err := agentConnectionWorkspaceIDs(app, ownerID)
		if err != nil {
			return mcpManualCompletionResult{err: err}
		}
		input.WorkspaceIDs = workspaceIDs
	}
	// A key lives as long as chosen, a year unless chosen, or as an
	// organization of its workspaces holds keys to (mcp_token_lifetime.go).
	expiresAt, err := mcpTokenExpiry(app, input.WorkspaceIDs, created, input.ExpiresIn)
	if err != nil {
		return mcpManualCompletionResult{err: err}
	}
	// Its key is kept sealed for its person to see again on its page
	// (mcp_token_sealed_schema.go); its hash checks each call.
	sealed, err := encryptAdministratorSetting(app, cipherMCPToken,
		deterministicMCPToken(secret, "manual-access", connectionID, "lmc_"))
	if err != nil {
		return mcpManualCompletionResult{err: err}
	}
	for attempt := 0; attempt < 2; attempt++ {
		result := mcpManualCompletionResult{}
		err := app.RunInTransaction(func(txApp core.App) error {
			stored, found, err := findManualMCPCompletion(txApp, ownerID, input.RequestID)
			if err != nil {
				return err
			}
			if found {
				if stored.RequestHash != requestHash || stored.ClientName != input.ClientName ||
					stored.RevokedAt != 0 || stored.OAuthClientID != "" {
					return errMCPConnectionRequestIDReused
				}
				if err := authorizeManualMCPCompletion(txApp, ownerID, stored.ID, input, stored.Created, stored.TokenExpiresAt); err != nil {
					return err
				}
				token := deterministicMCPToken(secret, "manual-access", stored.ID, "lmc_")
				if stored.TokenHash != mcpTokenHash(token) {
					return errMCPConnectionRequestIDReused
				}
				response, err := manualMCPCompletionResponse(stored.ID, input, endpoint, token, stored.TokenExpiresAt)
				if err != nil {
					return err
				}
				result = mcpManualCompletionResult{response: response, status: http.StatusOK}
				return nil
			}
			if err := authorizeManualMCPCompletion(txApp, ownerID, connectionID, input, created, expiresAt); err != nil {
				return err
			}
			if err := admitMCPConnectionInTransaction(txApp, ownerID, created); err != nil {
				return err
			}
			token := deterministicMCPToken(secret, "manual-access", connectionID, "lmc_")
			if _, err := txApp.DB().Insert(mcpConnectionsTable, dbx.Params{
				"id": connectionID, "owner_id": ownerID, "client_name": input.ClientName,
				"token_hash": mcpTokenHash(token), "token_sealed": sealed, "created": created, "revoked_at": 0,
				"request_id": input.RequestID, "request_hash": requestHash,
				"retire_after": 0, "access_scope": personConnectionScopeName, "token_expires_at": expiresAt, "token_made_at": created,
				"agent_connection": agent,
			}).Execute(); err != nil {
				return err
			}
			reach := mcpConnectionReachesOne
			if agent {
				reach = mcpConnectionReachesAll
			}
			if err := insertMCPConnectionWorkspaces(txApp, connectionID, ownerID, input.WorkspaceIDs, created, reach); err != nil {
				return err
			}
			notifyMCPCredentialRegistryAfterCommit(txApp)
			response, err := manualMCPCompletionResponse(connectionID, input, endpoint, token, expiresAt)
			if err != nil {
				return err
			}
			result = mcpManualCompletionResult{response: response, status: http.StatusCreated}
			return nil
		})
		if err == nil {
			return result
		}
		if !isSQLiteConstraintError(err) || attempt == 1 {
			return mcpManualCompletionResult{err: err}
		}
	}
	return mcpManualCompletionResult{err: errMCPConnectionRequestIDReused}
}

func findManualMCPCompletion(app core.App, ownerID string, requestID string) (storedMCPCredentialCompletion, bool, error) {
	stored := storedMCPCredentialCompletion{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[client_name]], [[created]], [[token_hash]], [[token_expires_at]],
			[[oauth_client_id]], [[request_hash]], [[revoked_at]]
		FROM {{leafen_mcp_connections}}
		WHERE [[owner_id]] = {:ownerId} AND [[request_id]] = {:requestId} AND [[request_id]] <> ''
		LIMIT 1
	`).Bind(dbx.Params{"ownerId": ownerID, "requestId": requestID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return storedMCPCredentialCompletion{}, false, nil
	}
	return stored, err == nil, err
}

func manualMCPCompletionResponse(
	connectionID string,
	input mcpConnectionInput,
	endpoint string,
	token string,
	expiresAt int64,
) (mcpConnectionResponse, error) {
	connectionURI, err := newMCPConnectionURI(endpoint, token)
	if err != nil {
		return mcpConnectionResponse{}, err
	}
	return mcpConnectionResponse{
		ID: connectionID, ClientName: input.ClientName,
		WorkspaceIDs: input.WorkspaceIDs, ConnectionURI: connectionURI, ExpiresAt: expiresAt,
	}, nil
}

func mcpManualCompletionRequestHash(input mcpConnectionInput) (string, error) {
	// A key of the default life hashes as a request made before keys could
	// choose one, so a retried request keeps its hash.
	lifetime := input.ExpiresIn
	if lifetime == mcpTokenDefaultLifetime {
		lifetime = ""
	}
	// An agent connection's are its person's, whatever they are as it is
	// made again.
	workspaceIDs := input.WorkspaceIDs
	if agentConnectionRequest(input.RequestID) {
		workspaceIDs = nil
	}
	encoded, err := json.Marshal(struct {
		Version       int      `json:"version"`
		ClientName    string   `json:"clientName"`
		WorkspaceIDs  []string `json:"workspaceIds"`
		Scope         string   `json:"scope"`
		ExpiresInDays int      `json:"expiresInDays"`
		Lifetime      string   `json:"lifetime,omitempty"`
	}{Version: 2, ClientName: input.ClientName, WorkspaceIDs: workspaceIDs, Scope: personConnectionScopeName, ExpiresInDays: 0, Lifetime: lifetime})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (runtime *mcpCredentialCompletionRuntime) completeAuthorization(
	app core.App,
	client storedMCPOAuthClient,
	code string,
	verifier string,
	redirectURI string,
	resource string,
) mcpOAuthTokenResult {
	requestHash := mcpOAuthExchangeRequestHash(client.ID, verifier, redirectURI, resource)
	codeHash := mcpTokenHash(code)
	secret, err := mcpOAuthSecret(app)
	if err != nil {
		return mcpOAuthTokenResult{code: "server_error", status: http.StatusInternalServerError, err: failedBecause(errMCPOAuthExchangeFailed, err)}
	}
	key := codeHash + "\x00" + requestHash
	value, _, _ := runtime.authorization.Do(key, func() (any, error) {
		return completeMCPOAuthAuthorizationCode(
			app, client, codeHash, requestHash, verifier, redirectURI, resource, secret,
		), nil
	})
	return value.(mcpOAuthTokenResult)
}

func completeMCPOAuthAuthorizationCode(
	app core.App,
	client storedMCPOAuthClient,
	codeHash string,
	requestHash string,
	verifier string,
	redirectURI string,
	resource string,
	secret string,
) mcpOAuthTokenResult {
	connectionID := security.RandomString(15)
	now := time.Now()
	for attempt := 0; attempt < 2; attempt++ {
		response := mcpOAuthTokenResponse{}
		err := app.RunInTransaction(func(txApp core.App) error {
			stored, found, err := findMCPOAuthCompletion(txApp, codeHash)
			if err != nil {
				return err
			}
			if found {
				response, err = replayMCPOAuthCompletion(txApp, stored, client.ID, requestHash, resource, secret)
				return err
			}
			authorization, err := loadMCPOAuthAuthorizationForExchange(txApp, codeHash)
			if err != nil {
				return err
			}
			challenge := sha256.Sum256([]byte(verifier))
			if authorization.ClientID != client.ID || authorization.RedirectURI != redirectURI ||
				authorization.Resource != resource || !oauthScopeFitsResource(authorization.Resource, authorization.Scope) ||
				authorization.ExpiresAt <= time.Now().UnixMilli() ||
				base64RawURL(challenge[:]) != authorization.CodeChallenge {
				return errMCPOAuthInvalidGrant
			}
			workspaceIDs, err := loadMCPOAuthAuthorizationWorkspaces(txApp, authorization.ID)
			if err != nil {
				return err
			}
			if len(workspaceIDs) == 0 || mcpOAuthWorkspaceHash(workspaceIDs) != authorization.WorkspaceHash {
				return errMCPOAuthInvalidGrant
			}
			// An agent's program granted MCP reaches all its person's
			// workspaces; an app, those it was granted (connection_person_rights.go).
			agent, reach := oauthAgentConnection(client.ID, resource), mcpConnectionReachesAsChosen
			if agent {
				if workspaceIDs, err = agentConnectionWorkspaceIDs(txApp, authorization.OwnerID); err != nil {
					return err
				}
				reach = mcpConnectionReachesAll
			}
			if err := authorizeConnectionWorkspaces(txApp, authorization.OwnerID, workspaceIDs); err != nil {
				if isPermissionDenied(err) {
					return errMCPOAuthInvalidGrant
				}
				return err
			}
			if err := admitMCPConnectionInTransaction(txApp, authorization.OwnerID, now.UnixMilli()); err != nil {
				return err
			}
			accessToken := deterministicMCPToken(secret, "oauth-code-access", connectionID, "lmo_")
			refreshToken := deterministicMCPToken(secret, "oauth-code-refresh", connectionID, "lmr_")
			accessExpiry := now.Add(mcpOAuthAccessLifetime).UnixMilli()
			refreshExpiry := now.Add(mcpOAuthRefreshLifetime).UnixMilli()
			if _, err := txApp.DB().Insert(mcpConnectionsTable, dbx.Params{
				"id": connectionID, "owner_id": authorization.OwnerID, "client_name": client.ClientName,
				"token_hash": mcpTokenHash(accessToken), "created": now.UnixMilli(), "revoked_at": 0,
				"token_expires_at": accessExpiry, "refresh_token_hash": mcpTokenHash(refreshToken),
				"refresh_expires_at": refreshExpiry, "oauth_client_id": client.ID,
				"oauth_resource": resource, "oauth_scope": authorization.Scope, "access_scope": oauthAccessScope(authorization),
				"authorization_code_hash": codeHash, "exchange_request_hash": requestHash,
				"retire_after":          refreshExpiry + int64(mcpCredentialRetentionMilliseconds),
				"created_by_connection": authorization.CreatedBy, "agent_connection": agent,
			}).Execute(); err != nil {
				return err
			}
			if err := insertMCPConnectionWorkspaces(txApp, connectionID, authorization.OwnerID, workspaceIDs, now.UnixMilli(), reach); err != nil {
				return err
			}
			if _, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_mcp_oauth_authorization_workspaces}} WHERE [[authorization_id]] = {:id};
				DELETE FROM {{leafen_mcp_oauth_authorizations}} WHERE [[id]] = {:id}
			`).Bind(dbx.Params{"id": authorization.ID}).Execute(); err != nil {
				return err
			}
			notifyMCPCredentialRegistryAfterCommit(txApp)
			response = newMCPOAuthTokenResponse(accessToken, refreshToken)
			response.Scope = authorization.Scope
			return nil
		})
		if err == nil {
			return mcpOAuthTokenResult{response: response}
		}
		if errors.Is(err, errMCPOAuthInvalidGrant) {
			return mcpOAuthTokenResult{code: "invalid_grant", status: http.StatusBadRequest, err: errors.New("invalid authorization code")}
		}
		if errors.Is(err, errMCPConnectionCapacity) {
			return mcpOAuthTokenResult{
				code: errorCodeMCPConnectionCapacity, status: http.StatusConflict,
				err: errors.New("This account reached its limit of active MCP connections. The person can disconnect one they no longer use in Leafen settings, then connect again."),
			}
		}
		if !isSQLiteConstraintError(err) || attempt == 1 {
			return mcpOAuthTokenResult{code: "server_error", status: http.StatusInternalServerError, err: failedBecause(errMCPOAuthExchangeFailed, err)}
		}
	}
	return mcpOAuthTokenResult{code: "server_error", status: http.StatusInternalServerError, err: errMCPOAuthExchangeFailed}
}

func findMCPOAuthCompletion(app core.App, codeHash string) (storedMCPCredentialCompletion, bool, error) {
	stored := storedMCPCredentialCompletion{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[client_name]], [[token_hash]], [[token_expires_at]],
			[[refresh_token_hash]], [[refresh_expires_at]], [[oauth_client_id]], [[oauth_resource]],
			[[oauth_scope]], [[authorization_code_hash]], [[exchange_request_hash]], [[revoked_at]]
		FROM {{leafen_mcp_connections}}
		WHERE [[authorization_code_hash]] = {:codeHash} AND [[authorization_code_hash]] <> '' LIMIT 1
	`).Bind(dbx.Params{"codeHash": codeHash}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return storedMCPCredentialCompletion{}, false, nil
	}
	return stored, err == nil, err
}

func replayMCPOAuthCompletion(
	app core.App,
	stored storedMCPCredentialCompletion,
	clientID string,
	requestHash string,
	resource string,
	secret string,
) (mcpOAuthTokenResponse, error) {
	now := time.Now().UnixMilli()
	if stored.ExchangeRequestHash != requestHash || stored.OAuthClientID != clientID ||
		stored.OAuthResource != resource || !oauthScopeFitsResource(stored.OAuthResource, stored.OAuthScope) ||
		stored.RevokedAt != 0 || stored.TokenExpiresAt <= now || stored.RefreshExpiresAt <= now {
		return mcpOAuthTokenResponse{}, errMCPOAuthInvalidGrant
	}
	workspaceIDs, err := loadMCPConnectionWorkspaces(app, stored.ID)
	if err != nil || len(workspaceIDs) == 0 {
		return mcpOAuthTokenResponse{}, mcpRefusedUnlessStorage(errMCPOAuthInvalidGrant, err)
	}
	if err := authorizeConnectionWorkspaces(app, stored.OwnerID, workspaceIDs); err != nil {
		if isPermissionDenied(err) {
			return mcpOAuthTokenResponse{}, errMCPOAuthInvalidGrant
		}
		return mcpOAuthTokenResponse{}, err
	}
	accessToken := deterministicMCPToken(secret, "oauth-code-access", stored.ID, "lmo_")
	refreshToken := deterministicMCPToken(secret, "oauth-code-refresh", stored.ID, "lmr_")
	if stored.TokenHash != mcpTokenHash(accessToken) || stored.RefreshTokenHash != mcpTokenHash(refreshToken) {
		return mcpOAuthTokenResponse{}, errMCPOAuthInvalidGrant
	}
	remaining := (stored.TokenExpiresAt - now + int64(time.Second) - 1) / int64(time.Second)
	response := newMCPOAuthTokenResponseWithExpiry(accessToken, refreshToken, remaining)
	response.Scope = stored.OAuthScope
	return response, nil
}

// oauthAccessScope is what an app's connection may do: the key scope it was
// granted for the native API; an agent's connection over MCP does
// everything its person may (connection_person_rights.go).
func oauthAccessScope(authorization storedMCPOAuthAuthorization) string {
	if oauthResourceIsNative(authorization.Resource) {
		return authorization.Scope
	}
	return personConnectionScopeName
}

func loadMCPOAuthAuthorizationForExchange(app core.App, codeHash string) (storedMCPOAuthAuthorization, error) {
	authorization := storedMCPOAuthAuthorization{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[client_id]], [[owner_id]], [[redirect_uri]], [[resource]], [[scope]],
			[[code_hash]], [[code_challenge]], [[workspace_hash]], [[created]], [[expires_at]], [[created_by_connection]]
		FROM {{leafen_mcp_oauth_authorizations}} WHERE [[code_hash]] = {:codeHash} LIMIT 1
	`).Bind(dbx.Params{"codeHash": codeHash}).One(&authorization)
	if errors.Is(err, sql.ErrNoRows) {
		return storedMCPOAuthAuthorization{}, errMCPOAuthInvalidGrant
	}
	return authorization, err
}

func mcpOAuthExchangeRequestHash(clientID string, verifier string, redirectURI string, resource string) string {
	encoded, _ := json.Marshal([]string{"oauth-code-exchange:v1", clientID, verifier, redirectURI, resource})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func base64RawURL(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}
