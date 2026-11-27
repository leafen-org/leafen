package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	notionOAuthAccessLifetime      = time.Hour
	notionOAuthRefreshLifetime     = 180 * 24 * time.Hour
	notionOAuthSecretDirectory     = ".secrets"
	notionOAuthSecretFilename      = "notion-oauth.key"
	notionOAuthSecretRecoveryFile  = "notion-oauth.key.v0-recovery"
	notionOAuthSecretFormatVersion = 1
	notionOAuthAccessTokenType     = "leafen_notion_compat_access"
	notionOAuthInvalidClientRealm  = `Basic realm="Leafen Notion-compatible OAuth"`
)

var notionOAuthSecretMutex sync.Mutex
var errNotionOAuthWriteConflict = errors.New("Notion OAuth write conflict")

type notionOAuthTokenResponse struct {
	AccessToken        string `json:"access_token"`
	TokenType          string `json:"token_type"`
	RefreshToken       string `json:"refresh_token"`
	BotID              string `json:"bot_id"`
	WorkspaceIcon      any    `json:"workspace_icon"`
	WorkspaceName      any    `json:"workspace_name"`
	WorkspaceID        string `json:"workspace_id"`
	Owner              any    `json:"owner"`
	DuplicatedTemplate any    `json:"duplicated_template_id"`
	RequestID          string `json:"request_id"`
}

type notionOAuthAccessClaims struct {
	Type            string `json:"typ"`
	GrantID         string `json:"grant_id"`
	ClientID        string `json:"client_id"`
	WorkspaceID     string `json:"workspace_id"`
	Scope           string `json:"scope"`
	GrantGeneration int    `json:"grant_generation"`
	jwt.RegisteredClaims
}

type storedNotionOAuthRefreshToken struct {
	ID          string `db:"id"`
	TokenHash   string `db:"token_hash"`
	GrantID     string `db:"grant_id"`
	ClientID    string `db:"client_id"`
	Generation  int    `db:"generation"`
	Status      string `db:"status"`
	SuccessorID string `db:"successor_id"`
	CreatedAt   int64  `db:"created_at"`
	ExpiresAt   int64  `db:"expires_at"`
	RevokedAt   int64  `db:"revoked_at"`
}

func notionOAuthSecret(app core.App) (string, error) {
	return loadOrCreateVersionedSecretFile(versionedSecretFileConfig{
		Directory:        filepath.Join(app.DataDir(), notionOAuthSecretDirectory),
		Filename:         notionOAuthSecretFilename,
		RecoveryFilename: notionOAuthSecretRecoveryFile,
		FormatVersion:    notionOAuthSecretFormatVersion,
		Mutex:            &notionOAuthSecretMutex,
		Failure: func() error {
			return errors.New("LEAFEN_NOTION_OAUTH_SECRET_INVALID")
		},
	})
}

func notionOAuthDeterministicCredential(app core.App, purpose, id, prefix string) (string, error) {
	secret, err := notionOAuthSecret(app)
	if err != nil {
		return "", err
	}
	return deriveDeterministicCredential(secret, "notion-oauth:v1", purpose, id, prefix), nil
}

func notionOAuthTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func bindNotionOAuthJSON(event *core.RequestEvent) (map[string]any, error) {
	body := map[string]any{}
	if err := event.BindBody(&body); err != nil || len(body) == 0 {
		return nil, notionOAuthFailure(http.StatusBadRequest, "invalid_request", "A JSON request body is required.")
	}
	return body, nil
}

func notionOAuthString(body map[string]any, key string) string {
	value, _ := body[key].(string)
	return strings.TrimSpace(value)
}

func exchangeNotionOAuthToken(event *core.RequestEvent) error {
	body, err := bindNotionOAuthJSON(event)
	if err != nil {
		return writeNotionOAuthError(event, err)
	}
	client, err := authenticateNotionOAuthClient(event.Request, body)
	if err != nil {
		return writeNotionOAuthError(event, err)
	}
	switch notionOAuthString(body, "grant_type") {
	case "authorization_code":
		return exchangeNotionOAuthAuthorizationCode(event, client, body)
	case "refresh_token":
		return refreshNotionOAuthToken(event, client, body)
	default:
		return writeNotionOAuthError(event, notionOAuthFailure(
			http.StatusBadRequest, "unsupported_grant_type",
			"Only authorization_code and refresh_token are supported.",
		))
	}
}

func exchangeNotionOAuthAuthorizationCode(event *core.RequestEvent, client notionOAuthClient, body map[string]any) error {
	code := notionOAuthString(body, "code")
	redirectURI := notionOAuthString(body, "redirect_uri")
	if code == "" || redirectURI == "" {
		return writeNotionOAuthError(event, notionOAuthFailure(http.StatusBadRequest, "invalid_request", "code and redirect_uri are required."))
	}
	if err := validateNotionOAuthRedirectURI(client, redirectURI); err != nil {
		return writeNotionOAuthError(event, err)
	}
	newRefreshID := security.RandomString(24)
	newRefresh, err := notionOAuthDeterministicCredential(event.App, "refresh-token", newRefreshID, "lnr_")
	if err != nil {
		return writeNotionOAuthError(event, failedBecause(errNotionOAuthNotCompleted, err))
	}
	now := time.Now().UnixMilli()
	grant := storedNotionOAuthGrant{}
	refresh := storedNotionOAuthRefreshToken{}
	invalid := false
	err = event.App.RunInTransaction(func(txApp core.App) error {
		stored, found, err := findNotionOAuthAuthorizationCode(txApp, notionOAuthTokenHash(code))
		if err != nil {
			return err
		}
		if !found || stored.ClientID != client.ID || stored.RedirectURI != redirectURI || stored.ExpiresAt <= now {
			invalid = true
			return nil
		}
		loadedGrant, found, err := findNotionOAuthGrantByID(txApp, stored.GrantID)
		if err != nil {
			return err
		}
		if !found || loadedGrant.Status != "active" || loadedGrant.ClientID != client.ID ||
			stored.GrantGeneration != loadedGrant.CredentialGeneration {
			invalid = true
			return nil
		}
		if err := authorizeWorkspaceSet(txApp, loadedGrant.OwnerID, []string{loadedGrant.WorkspaceID}, permissionWorkspaceRead); err != nil {
			if isPermissionDenied(err) {
				invalid = true
				return revokeNotionOAuthGrant(txApp, loadedGrant.ID, now)
			}
			return err
		}
		grant = loadedGrant
		if stored.Status == "completed" {
			loadedRefresh, found, err := findNotionOAuthRefreshByID(txApp, stored.RefreshTokenID)
			if err != nil {
				return err
			}
			if !found {
				invalid = true
				return nil
			}
			refresh = loadedRefresh
			return nil
		}
		if stored.Status != "active" {
			invalid = true
			return nil
		}
		refresh = storedNotionOAuthRefreshToken{
			ID: newRefreshID, GrantID: grant.ID, ClientID: client.ID, Generation: 0,
			TokenHash: notionOAuthTokenHash(newRefresh), Status: "active", CreatedAt: now,
			ExpiresAt: now + notionOAuthRefreshLifetime.Milliseconds(),
		}
		if _, err := txApp.DB().Insert(notionOAuthRefreshTokensTable, dbx.Params{
			"id": refresh.ID, "token_hash": notionOAuthTokenHash(newRefresh),
			"grant_id": refresh.GrantID, "client_id": refresh.ClientID,
			"generation": refresh.Generation, "status": refresh.Status, "successor_id": "",
			"created_at": refresh.CreatedAt, "updated_at": refresh.CreatedAt,
			"expires_at": refresh.ExpiresAt, "revoked_at": 0,
		}).Execute(); err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_oauth_authorization_codes}}
			SET [[status]]='completed', [[completed_at]]={:completedAt}, [[refresh_token_id]]={:refreshTokenId}
			WHERE [[id]]={:id} AND [[status]]='active'
		`).Bind(dbx.Params{
			"completedAt": now, "refreshTokenId": refresh.ID, "id": stored.ID,
		}).Execute()
		if err != nil {
			return err
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return errNotionOAuthWriteConflict
		}
		return nil
	})
	if errors.Is(err, errNotionOAuthWriteConflict) {
		invalid = true
		err = nil
	}
	if invalid {
		return writeNotionOAuthError(event, notionOAuthFailure(http.StatusBadRequest, "invalid_grant", "Authorization code is invalid."))
	}
	if err != nil {
		return writeNotionOAuthError(event, failedBecause(errNotionOAuthNotCompleted, err))
	}
	return writeNotionOAuthTokenResponse(event, grant, refresh)
}

func refreshNotionOAuthToken(event *core.RequestEvent, client notionOAuthClient, body map[string]any) error {
	presented := notionOAuthString(body, "refresh_token")
	if presented == "" {
		return writeNotionOAuthError(event, notionOAuthFailure(http.StatusBadRequest, "invalid_request", "refresh_token is required."))
	}
	newID := security.RandomString(24)
	newToken, err := notionOAuthDeterministicCredential(event.App, "refresh-token", newID, "lnr_")
	if err != nil {
		return writeNotionOAuthError(event, failedBecause(errNotionOAuthNotCompleted, err))
	}
	now := time.Now().UnixMilli()
	grant := storedNotionOAuthGrant{}
	successor := storedNotionOAuthRefreshToken{}
	invalid := false
	err = event.App.RunInTransaction(func(txApp core.App) error {
		current, found, err := findNotionOAuthRefreshByHash(txApp, notionOAuthTokenHash(presented))
		if err != nil {
			return err
		}
		if !found || current.ClientID != client.ID || current.ExpiresAt <= now || current.Status == "revoked" {
			invalid = true
			return nil
		}
		loadedGrant, found, err := findNotionOAuthGrantByID(txApp, current.GrantID)
		if err != nil {
			return err
		}
		if !found || loadedGrant.Status != "active" || loadedGrant.ClientID != client.ID {
			invalid = true
			return nil
		}
		if err := authorizeWorkspaceSet(txApp, loadedGrant.OwnerID, []string{loadedGrant.WorkspaceID}, permissionWorkspaceRead); err != nil {
			if isPermissionDenied(err) {
				invalid = true
				return revokeNotionOAuthGrant(txApp, loadedGrant.ID, now)
			}
			return err
		}
		grant = loadedGrant
		if current.Status == "rotated" {
			loaded, found, err := findNotionOAuthRefreshByID(txApp, current.SuccessorID)
			if err != nil {
				return err
			}
			if !found || loaded.Status != "active" {
				invalid = true
				return revokeNotionOAuthGrant(txApp, grant.ID, now)
			}
			successor = loaded
			return nil
		}
		if current.Status != "active" {
			invalid = true
			return nil
		}
		successor = storedNotionOAuthRefreshToken{
			ID: newID, GrantID: grant.ID, ClientID: client.ID, Generation: current.Generation + 1,
			TokenHash: notionOAuthTokenHash(newToken), Status: "active", CreatedAt: now,
			ExpiresAt: now + notionOAuthRefreshLifetime.Milliseconds(),
		}
		if _, err := txApp.DB().Insert(notionOAuthRefreshTokensTable, dbx.Params{
			"id": successor.ID, "token_hash": notionOAuthTokenHash(newToken),
			"grant_id": successor.GrantID, "client_id": successor.ClientID,
			"generation": successor.Generation, "status": successor.Status, "successor_id": "",
			"created_at": successor.CreatedAt, "updated_at": successor.CreatedAt,
			"expires_at": successor.ExpiresAt, "revoked_at": 0,
		}).Execute(); err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_oauth_refresh_tokens}}
			SET [[status]]='rotated', [[successor_id]]={:successorId}, [[updated_at]]={:updatedAt}
			WHERE [[id]]={:id} AND [[status]]='active'
		`).Bind(dbx.Params{"successorId": successor.ID, "updatedAt": now, "id": current.ID}).Execute()
		if err != nil {
			return err
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return errNotionOAuthWriteConflict
		}
		_, err = txApp.DB().NewQuery(`
			DELETE FROM {{leafen_notion_oauth_refresh_tokens}}
			WHERE [[grant_id]]={:grantId} AND [[generation]] < {:oldestGeneration}
		`).Bind(dbx.Params{
			"grantId": grant.ID, "oldestGeneration": current.Generation,
		}).Execute()
		return err
	})
	if errors.Is(err, errNotionOAuthWriteConflict) {
		invalid = true
		err = nil
	}
	if invalid {
		return writeNotionOAuthError(event, notionOAuthFailure(http.StatusBadRequest, "invalid_grant", "Refresh token is invalid."))
	}
	if err != nil {
		return writeNotionOAuthError(event, failedBecause(errNotionOAuthNotCompleted, err))
	}
	return writeNotionOAuthTokenResponse(event, grant, successor)
}

func writeNotionOAuthTokenResponse(event *core.RequestEvent, grant storedNotionOAuthGrant, refresh storedNotionOAuthRefreshToken) error {
	access, err := issueNotionOAuthAccessToken(event, grant, refresh.Generation, refresh.CreatedAt)
	if err != nil {
		return writeNotionOAuthError(event, failedBecause(errNotionOAuthNotCompleted, err))
	}
	refreshToken, err := notionOAuthDeterministicCredential(event.App, "refresh-token", refresh.ID, "lnr_")
	if err != nil || !hmac.Equal([]byte(notionOAuthTokenHash(refreshToken)), []byte(refresh.TokenHash)) {
		return writeNotionOAuthError(event, failedBecause(errNotionOAuthNotCompleted, err))
	}
	workspace, err := event.App.FindRecordById(workspacesCollection, grant.WorkspaceID)
	if err != nil {
		return writeNotionOAuthError(event, failedBecause(errNotionOAuthNotCompleted, err))
	}
	owner, err := event.App.FindRecordById("users", grant.OwnerID)
	if err != nil {
		return writeNotionOAuthError(event, failedBecause(errNotionOAuthNotCompleted, err))
	}
	ownerUser, err := notionUserObject(owner, requestOrigin(event.Request))
	if err != nil {
		return writeNotionOAuthError(event, failedBecause(errNotionOAuthNotCompleted, err))
	}
	// The workspace as Notion names it, by a UUID.
	workspaceID, err := notionCompatibleWorkspaceID(grant.WorkspaceID)
	if err != nil {
		return writeNotionOAuthError(event, failedBecause(errNotionOAuthNotCompleted, err))
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	return event.JSON(http.StatusOK, notionOAuthTokenResponse{
		AccessToken: access, TokenType: "bearer", RefreshToken: refreshToken,
		BotID: notionOAuthBotID(grant.ID), WorkspaceIcon: nil, WorkspaceName: workspace.GetString("name"),
		WorkspaceID:        workspaceID,
		Owner:              notionOrderedObject{{key: "type", value: "user"}, {key: "user", value: ownerUser}},
		DuplicatedTemplate: nil, RequestID: newDiagnosticReference(),
	})
}

func issueNotionOAuthAccessToken(event *core.RequestEvent, grant storedNotionOAuthGrant, generation int, issuedAt int64) (string, error) {
	secret, err := notionOAuthSecret(event.App)
	if err != nil {
		return "", err
	}
	// The address the token is asked for at, an earlier one too (answeredAtOrigin).
	origin, err := answeredAtOrigin(event.Request)
	if err != nil {
		return "", err
	}
	issued := time.UnixMilli(issuedAt)
	claims := notionOAuthAccessClaims{
		Type: notionOAuthAccessTokenType, GrantID: grant.ID, ClientID: grant.ClientID,
		WorkspaceID: grant.WorkspaceID, Scope: grant.Scope, GrantGeneration: grant.CredentialGeneration,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: origin, Subject: grant.OwnerID, Audience: jwt.ClaimStrings{origin + notionAPIRoot},
			ExpiresAt: jwt.NewNumericDate(issued.Add(notionOAuthAccessLifetime)),
			IssuedAt:  jwt.NewNumericDate(issued),
			ID:        grant.ID + ":" + strconv.Itoa(grant.CredentialGeneration) + ":" + strconv.Itoa(generation),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func writeNotionOAuthError(event *core.RequestEvent, err error) error {
	failure := &notionOAuthProtocolError{}
	if !errors.As(err, &failure) {
		failure = &notionOAuthProtocolError{status: http.StatusInternalServerError, code: "internal_server_error", message: "OAuth request could not be completed."}
	}
	if failure.code == "invalid_client" {
		event.Response.Header().Set("WWW-Authenticate", notionOAuthInvalidClientRealm)
	}
	event.Response.Header().Set("Pragma", "no-cache")
	// What failed goes with it, so a database that stayed busy is answered as
	// the Notion-compatible API answers one (notionCompatibilityErrorWithData).
	return notionCompatibilityErrorWithData(event, failure.status, failure.code, failure.message, nil, err)
}

// errNotionOAuthNotCompleted is an OAuth request that failed on the server's
// side, carried with what failed (failedBecause).
var errNotionOAuthNotCompleted = notionOAuthFailure(
	http.StatusInternalServerError, "internal_server_error", "OAuth request could not be completed.",
)
