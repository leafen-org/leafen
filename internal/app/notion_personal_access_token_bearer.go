package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionPersonalAccessTokenLifecycleLockNamespace = "notion-personal-access-token-lifecycle"
	notionPersonalAccessTokenLastUsedInterval       = time.Hour
	// notionPersonalAccessTokenUseWait is how long a request waits for the
	// write connection to write its token's last use, and
	// notionPersonalAccessTokenUseRetry how soon after a busy one another
	// request tries.
	notionPersonalAccessTokenUseWait  = 500 * time.Millisecond
	notionPersonalAccessTokenUseRetry = 5 * time.Second
)

var notionPersonalAccessTokenSecretPattern = regexp.MustCompile(`^ntn_pat_[A-Za-z0-9_-]{32,}$`)

func notionPersonalAccessTokenHash(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(digest[:])
}

func acquireNotionPersonalAccessTokenLifecycleLock(tokenID string) func() {
	return acquireProductOperationLock(notionPersonalAccessTokenLifecycleLockNamespace, tokenID)
}

func resolveNotionPersonalAccessTokenBearer(
	event *core.RequestEvent,
	rawToken string,
) (notionOAuthBearerIdentity, bool, error) {
	if !notionPersonalAccessTokenSecretPattern.MatchString(rawToken) {
		return notionOAuthBearerIdentity{}, false, nil
	}
	tokenID := ""
	err := event.App.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_notion_personal_access_tokens}}
		WHERE [[token_hash]] = {:tokenHash} LIMIT 2
	`).Bind(dbx.Params{"tokenHash": notionPersonalAccessTokenHash(rawToken)}).Row(&tokenID)
	if errors.Is(err, sql.ErrNoRows) {
		return notionOAuthBearerIdentity{}, false, nil
	}
	if err != nil || !validNotionCompatibleInternalID(tokenID) {
		return notionOAuthBearerIdentity{}, false, err
	}
	// The token is read, not written: a request waits for no write before it
	// is let in (2026-11-14: twenty readers' every request took the one write
	// connection here, and one answered 500 when a long write held it ten
	// seconds). Its last use is written after, once an interval.
	stored, authorized, err := readNotionPersonalAccessTokenBearer(event.App, tokenID, rawToken)
	if err != nil || !authorized {
		return notionOAuthBearerIdentity{}, false, err
	}
	noteNotionPersonalAccessTokenUse(event.App, stored.ID, stored.LastUsedAt)
	return notionOAuthBearerIdentity{
		PrincipalID: stored.CreatorID, ClientID: "personal-access-token",
		GrantID: stored.ID, GrantGeneration: stored.CredentialGeneration,
		WorkspaceID: stored.WorkspaceID, Scopes: append([]string{}, notionOAuthDefaultScopes...),
		IssuedAt: stored.CreatedAt / 1000,
	}, true, nil
}

// readNotionPersonalAccessTokenBearer reads a token as it stands: active, its
// maker able to act in its workspace, which still lets members hold tokens.
// It reads under the token's lifecycle lock, so a revocation under way is
// waited for.
func readNotionPersonalAccessTokenBearer(
	app core.App, tokenID, rawToken string,
) (storedNotionPersonalAccessToken, bool, error) {
	release := acquireNotionPersonalAccessTokenLifecycleLock(tokenID)
	defer release()
	stored := storedNotionPersonalAccessToken{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[workspace_id]], [[creator_id]], [[token_hash]], [[token_prefix]],
			[[name]], [[status]], [[capabilities_json]], [[credential_generation]],
			[[created_at]], [[expires_at]], [[last_used_at]], [[revoked_at]], [[updated_at]],
			[[status]] AS [[projected_status]]
		FROM {{leafen_notion_personal_access_tokens}}
		WHERE [[id]] = {:tokenId} AND [[token_hash]] = {:tokenHash} LIMIT 2
	`).Bind(dbx.Params{
		"tokenId": tokenID, "tokenHash": notionPersonalAccessTokenHash(rawToken),
	}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, false, nil
	}
	if err != nil {
		return stored, false, err
	}
	now := time.Now().UTC().UnixMilli()
	if stored.Status != "active" || stored.RevokedAt.Valid || stored.ExpiresAt <= now {
		return stored, false, nil
	}
	// A token stops while its maker can't act: deactivated or suspended.
	if active, err := accountMayAct(app, stored.CreatorID); err != nil || !active {
		return stored, false, err
	}
	capabilities := []string{}
	if json.Unmarshal([]byte(stored.CapabilitiesJSON), &capabilities) != nil ||
		!containsNotionPersonalAccessTokenCapability(capabilities, "notion-api") {
		return stored, false, nil
	}
	authorization, err := authorizeWorkspace(app, stored.CreatorID, stored.WorkspaceID, permissionWorkspaceRead)
	if err != nil {
		if isPermissionDenied(err) {
			return stored, false, nil
		}
		return stored, false, err
	}
	// A member's tokens stop once the workspace limits tokens to its owners.
	if err := requireWorkspacePolicy(
		app, stored.WorkspaceID, stored.CreatorID, authorization.Role, personalAccessTokensLimit, errPersonalAccessTokenOwners,
	); err != nil {
		if errors.Is(err, errPersonalAccessTokenOwners) {
			return stored, false, nil
		}
		return stored, false, err
	}
	return stored, true, nil
}

// notionPersonalAccessTokenUseTries holds when a token's last use was last
// tried, by token ID, while the write connection kept it from being written.
var notionPersonalAccessTokenUseTries sync.Map

// noteNotionPersonalAccessTokenUse writes a token's last use when the one
// written is an interval old, without holding up the request: it waits for
// the write connection a moment only, and a busy one leaves the use to a
// request some seconds later.
func noteNotionPersonalAccessTokenUse(app core.App, tokenID string, lastUsedAt int64) {
	now := time.Now().UTC().UnixMilli()
	if lastUsedAt != 0 && now-lastUsedAt < notionPersonalAccessTokenLastUsedInterval.Milliseconds() {
		return
	}
	if tried, ok := notionPersonalAccessTokenUseTries.Load(tokenID); ok &&
		now-tried.(int64) < notionPersonalAccessTokenUseRetry.Milliseconds() {
		return
	}
	notionPersonalAccessTokenUseTries.Store(tokenID, now)
	ctx, cancel := context.WithTimeout(context.Background(), notionPersonalAccessTokenUseWait)
	defer cancel()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_personal_access_tokens}}
		SET [[last_used_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[id]] = {:tokenId} AND [[status]] = 'active' AND [[last_used_at]] < {:now}
	`).WithContext(ctx).Bind(dbx.Params{"now": now, "tokenId": tokenID}).Execute(); err == nil {
		notionPersonalAccessTokenUseTries.Delete(tokenID)
	}
}

func containsNotionPersonalAccessTokenCapability(capabilities []string, wanted string) bool {
	for _, capability := range capabilities {
		if capability == wanted {
			return true
		}
	}
	return false
}

func notionPersonalAccessTokenBearerFromRequest(
	event *core.RequestEvent,
) (notionOAuthBearerIdentity, bool, error) {
	parts := strings.Fields(strings.TrimSpace(event.Request.Header.Get("Authorization")))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return notionOAuthBearerIdentity{}, false, nil
	}
	return resolveNotionPersonalAccessTokenBearer(event, parts[1])
}
