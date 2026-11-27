package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// An internal connection's token, as Notion's "내부 통합 시크릿": made with the
// connection, shown to its owners any time ("보기") and made again ("새로
// 만들기"), when the old one stops at once or in seven days. It is checked
// by its hash and kept sealed with a key file of its own beside the
// database, so a copy of the database alone doesn't give it away (judgement
// principles 3 and 5).

const (
	internalConnectionTokenKeyDirectory     = ".secrets"
	internalConnectionTokenKeyFilename      = "notion-internal-connection.key"
	internalConnectionTokenKeyRecoveryFile  = "notion-internal-connection.key.v0-recovery"
	internalConnectionTokenKeyFormatVersion = 1
)

var internalConnectionTokenKeyMutex sync.Mutex

func internalConnectionTokenKey(app core.App) (string, error) {
	return loadOrCreateVersionedSecretFile(versionedSecretFileConfig{
		Directory: filepath.Join(app.DataDir(), internalConnectionTokenKeyDirectory),
		Filename:  internalConnectionTokenKeyFilename, RecoveryFilename: internalConnectionTokenKeyRecoveryFile,
		FormatVersion: internalConnectionTokenKeyFormatVersion, Mutex: &internalConnectionTokenKeyMutex,
		Failure: func() error {
			return errors.New(errorCodeInternalConnectionFailed)
		},
	})
}

// renewInternalConnectionToken makes the token again. The old one stops at
// once, or works seven more days (notion_internal_connection_token_grace.go),
// as the person chose; a request that names neither stops it at once.
func renewInternalConnectionToken(event *core.RequestEvent) error {
	stored, err := managedInternalConnection(event)
	if err != nil {
		return internalConnectionError(event, err, "token")
	}
	input := struct {
		RevokeOld string `json:"revokeOld"`
	}{}
	if err := event.BindBody(&input); err != nil {
		return internalConnectionError(event, bodyRefusal(errInternalConnectionInvalid, "token refresh", err), "token-bind")
	}
	if input.RevokeOld != "" && input.RevokeOld != "now" && input.RevokeOld != "later" {
		return internalConnectionError(event, refuse(errInternalConnectionInvalid, "revokeOld is now or later, not %q", input.RevokeOld), "token-bind")
	}
	secret, err := newInternalConnectionSecret()
	if err != nil {
		return internalConnectionError(event, err, "token-secret")
	}
	sealed, err := sealInternalConnectionSecret(event.App, secret)
	if err != nil {
		return internalConnectionError(event, err, "token-seal")
	}
	now := time.Now().UTC().UnixMilli()
	until := int64(0)
	if input.RevokeOld == "later" {
		until = now + internalConnectionRevokeLaterMillis
	}
	// The token it had becomes the one that works until then; one kept from
	// an earlier refresh stops now.
	if _, err := event.App.DB().NewQuery(`UPDATE {{leafen_notion_internal_connections}}
		SET [[previous_token_hash]] = CASE WHEN {:until} > 0 THEN [[token_hash]] ELSE '' END, [[previous_token_until]] = {:until},
			[[token_hash]] = {:hash}, [[token_hint]] = {:hint}, [[token_sealed]] = {:sealed}, [[token_made_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[id]] = {:id}`).Bind(dbx.Params{
		"hash": notionPersonalAccessTokenHash(secret), "hint": internalConnectionTokenHint(secret), "sealed": sealed,
		"now": now, "until": until, "id": stored.ID,
	}).Execute(); err != nil {
		return internalConnectionError(event, err, "token")
	}
	response := internalConnectionResponseOf(stored)
	response.TokenHint, response.Secret = internalConnectionTokenHint(secret), secret
	response.TokenMadeAt, response.PreviousUntil = now, until
	return event.JSON(http.StatusOK, response)
}

// revealInternalConnectionToken shows the token to one of its owners, as
// Notion's "보기".
func revealInternalConnectionToken(event *core.RequestEvent) error {
	stored, err := managedInternalConnection(event)
	if err != nil {
		return internalConnectionError(event, err, "reveal")
	}
	sealed := ""
	if err := event.App.DB().NewQuery(`SELECT [[token_sealed]] FROM {{leafen_notion_internal_connections}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": stored.ID}).Row(&sealed); err != nil {
		return internalConnectionError(event, err, "reveal")
	}
	secret, err := openInternalConnectionSecret(event.App, sealed)
	if err != nil {
		return internalConnectionError(event, err, "reveal-open")
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]string{"secret": secret})
}

func sealInternalConnectionSecret(app core.App, secret string) (string, error) {
	key, err := internalConnectionTokenKey(app)
	if err != nil {
		return "", err
	}
	return security.Encrypt([]byte(secret), key)
}

func openInternalConnectionSecret(app core.App, sealed string) (string, error) {
	if sealed == "" {
		return "", errInternalConnectionNotFound
	}
	key, err := internalConnectionTokenKey(app)
	if err != nil {
		return "", err
	}
	// A token sealed with a key this server no longer has (a database moved
	// without its key file) can't be shown; making it again shows one.
	secret, err := security.Decrypt(sealed, key)
	if err != nil {
		return "", errInternalConnectionNotFound
	}
	return string(secret), nil
}

func newInternalConnectionSecret() (string, error) {
	random := make([]byte, internalConnectionSecretSize)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return internalConnectionSecretPrefix + base64.RawURLEncoding.EncodeToString(random), nil
}

// internalConnectionTokenHint is what the page shows of a token after it
// is made: its start and last four.
func internalConnectionTokenHint(secret string) string {
	if len(secret) < 12 {
		return internalConnectionSecretPrefix
	}
	return secret[:len(internalConnectionSecretPrefix)+4] + "…" + secret[len(secret)-4:]
}

// resolveNotionInternalConnectionBearer reads an internal connection's
// token as its grant: its bot, scopes and page list, while its maker can
// act in the workspace.
func resolveNotionInternalConnectionBearer(event *core.RequestEvent, rawToken string) (notionOAuthBearerIdentity, bool, error) {
	if !strings.HasPrefix(rawToken, internalConnectionSecretPrefix) || strings.HasPrefix(rawToken, personalAccessTokenSecretPrefix) ||
		len(rawToken) > 128 {
		return notionOAuthBearerIdentity{}, false, nil
	}
	stored := storedInternalConnection{}
	// The token it had before a refresh works until its date.
	err := event.App.DB().NewQuery(`SELECT [[id]], [[workspace_id]], [[creator_id]], [[grant_id]] FROM {{leafen_notion_internal_connections}}
		WHERE [[token_hash]] = {:hash} OR ` + partialIndexKey(`[[previous_token_hash]]`, ` = {:hash}`) + ` AND [[previous_token_until]] > {:now}
		LIMIT 1`).Bind(dbx.Params{"hash": notionPersonalAccessTokenHash(rawToken), "now": time.Now().UTC().UnixMilli()}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return notionOAuthBearerIdentity{}, false, nil
	}
	if err != nil {
		return notionOAuthBearerIdentity{}, false, err
	}
	grant, found, err := findNotionOAuthGrantByID(event.App, stored.GrantID)
	if err != nil || !found || grant.Status != "active" {
		return notionOAuthBearerIdentity{}, false, err
	}
	if active, err := accountMayAct(event.App, stored.CreatorID); err != nil || !active {
		return notionOAuthBearerIdentity{}, false, err
	}
	if _, err := authorizeWorkspace(event.App, stored.CreatorID, stored.WorkspaceID, permissionWorkspaceRead); err != nil {
		if isPermissionDenied(err) {
			return notionOAuthBearerIdentity{}, false, nil
		}
		return notionOAuthBearerIdentity{}, false, err
	}
	resources, err := loadNotionOAuthGrantResources(event.App, grant.ID)
	if err != nil {
		return notionOAuthBearerIdentity{}, false, err
	}
	return notionOAuthBearerIdentity{
		PrincipalID: grant.OwnerID, ClientID: grant.ClientID, GrantID: grant.ID, GrantGeneration: grant.CredentialGeneration,
		WorkspaceID: grant.WorkspaceID, Scopes: strings.Fields(grant.Scope), ResourceIDs: resources, PageScoped: true,
	}, true, nil
}

// resolveNotionStaticTokenBearer reads a personal access token, or else an
// internal connection's token.
func resolveNotionStaticTokenBearer(event *core.RequestEvent, rawToken string) (notionOAuthBearerIdentity, bool, error) {
	identity, ok, err := resolveNotionPersonalAccessTokenBearer(event, rawToken)
	if err != nil || ok {
		return identity, ok, err
	}
	return resolveNotionInternalConnectionBearer(event, rawToken)
}
