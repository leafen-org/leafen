package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errNotionAdminTokenUnauthorized = errors.New("notion Admin API token is unauthorized")
	notionAdminTokenSecretPattern   = regexp.MustCompile(`^ntn_admin_[A-Za-z0-9_-]{32,}$`)
)

const notionAdminTokenLifecycleLockNamespace = "notion-admin-token-lifecycle"

type notionAdminIdentity struct {
	OrganizationID string
	TokenID        string
	CreatedBy      string
	capabilities   map[string]struct{}
	workspaceIDs   map[string]struct{}
	legalHoldIDs   map[string]struct{}
}

type storedNotionAdminBearerCandidate struct {
	ID             string `db:"id"`
	OrganizationID string `db:"organization_id"`
	Status         string `db:"status"`
	TokenHash      string `db:"token_hash"`
	ScopesJSON     string `db:"scopes_json"`
	CreatedBy      string `db:"created_by"`
	ExpiresAt      int64  `db:"expires_at"`
	LastUsedAt     int64  `db:"last_used_at"`
}

func authenticateNotionAdminToken(
	ctx context.Context,
	app core.App,
	secret string,
	now time.Time,
) (notionAdminIdentity, error) {
	if !notionAdminTokenSecretPattern.MatchString(secret) {
		return notionAdminIdentity{}, errNotionAdminTokenUnauthorized
	}
	candidates := []storedNotionAdminBearerCandidate{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[organization_id]], [[status]], [[token_hash]], [[scopes_json]],
			[[created_by]], [[expires_at]], [[last_used_at]]
		FROM {{leafen_notion_admin_tokens}}
		WHERE [[token_prefix]]={:prefix}
		ORDER BY [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"prefix": notionAdminTokenLookupPrefix(secret), "limit": notionAdminTokenLookupCandidateLimit,
	}).All(&candidates)
	if err != nil {
		return notionAdminIdentity{}, errors.Join(errNotionAdminTokenUnauthorized, err)
	}
	wantedHash := notionAdminTokenHash(secret)
	matching := make([]storedNotionAdminBearerCandidate, 0, 1)
	for _, candidate := range candidates {
		if subtle.ConstantTimeCompare([]byte(wantedHash), []byte(candidate.TokenHash)) == 1 {
			matching = append(matching, candidate)
		}
	}
	if len(matching) != 1 {
		return notionAdminIdentity{}, errNotionAdminTokenUnauthorized
	}
	selected := matching[0]
	if selected.Status != notionAdminTokenStatusActive ||
		selected.ExpiresAt > 0 && selected.ExpiresAt <= now.UTC().UnixMilli() {
		return notionAdminIdentity{}, errNotionAdminTokenUnauthorized
	}
	scopes := notionAdminTokenScopes{}
	if selected.CreatedBy == "" || json.Unmarshal([]byte(selected.ScopesJSON), &scopes) != nil ||
		!validStoredNotionAdminScopes(scopes, selected.OrganizationID) {
		return notionAdminIdentity{}, errNotionAdminTokenUnauthorized
	}
	// The settings' "마지막 사용" reads this, written as a personal access
	// token's is (2026-11-07: a token the Admin API had just answered still
	// said "사용 기록 없음").
	if at := now.UTC().UnixMilli(); selected.LastUsedAt == 0 ||
		at-selected.LastUsedAt >= notionPersonalAccessTokenLastUsedInterval.Milliseconds() {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_notion_admin_tokens}}
			SET [[last_used_at]] = {:at}, [[updated]] = {:at}
			WHERE [[id]] = {:id} AND [[status]] = 'active'
		`).WithContext(ctx).Bind(dbx.Params{"at": at, "id": selected.ID}).Execute(); err != nil {
			return notionAdminIdentity{}, errors.Join(errNotionAdminTokenUnauthorized, err)
		}
	}
	return notionAdminIdentity{
		OrganizationID: selected.OrganizationID,
		TokenID:        selected.ID,
		CreatedBy:      selected.CreatedBy,
		capabilities:   notionAdminTokenValueSet(scopes.Capabilities),
		workspaceIDs:   notionAdminTokenValueSet(scopes.Resources.WorkspaceIDs),
		legalHoldIDs:   notionAdminTokenValueSet(scopes.Resources.LegalHoldIDs),
	}, nil
}

func notionAdminTokenHash(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(digest[:])
}

func notionAdminTokenLookupPrefix(secret string) string {
	if len(secret) <= notionAdminTokenPrefixLength {
		return secret
	}
	return secret[:notionAdminTokenPrefixLength]
}

func validStoredNotionAdminScopes(scopes notionAdminTokenScopes, organizationID string) bool {
	if scopes.Version != notionAdminTokenScopeVersion || organizationID == "" ||
		scopes.Resources.OrganizationID != organizationID {
		return false
	}
	normalizedCapabilities, err := normalizeNotionAdminValues(scopes.Capabilities, true)
	if err != nil || !equalNotionAdminValues(normalizedCapabilities, scopes.Capabilities) {
		return false
	}
	normalizedWorkspaces, err := normalizeNotionAdminValues(scopes.Resources.WorkspaceIDs, false)
	if err != nil || !equalNotionAdminValues(normalizedWorkspaces, scopes.Resources.WorkspaceIDs) {
		return false
	}
	normalizedLegalHolds, err := normalizeNotionAdminValues(scopes.Resources.LegalHoldIDs, false)
	return err == nil && equalNotionAdminValues(normalizedLegalHolds, scopes.Resources.LegalHoldIDs)
}

func equalNotionAdminValues(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func notionAdminTokenValueSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func (identity notionAdminIdentity) HasCapability(capability string) bool {
	_, allowed := identity.capabilities[capability]
	return allowed
}

func (identity notionAdminIdentity) CanAccessWorkspace(workspaceID string) bool {
	return notionAdminIdentityCanAccess(identity.workspaceIDs, workspaceID)
}

func (identity notionAdminIdentity) CanAccessLegalHold(legalHoldID string) bool {
	return notionAdminIdentityCanAccess(identity.legalHoldIDs, legalHoldID)
}

func notionAdminIdentityCanAccess(allowed map[string]struct{}, resourceID string) bool {
	if resourceID == "" {
		return false
	}
	if _, wildcard := allowed["*"]; wildcard {
		return true
	}
	_, present := allowed[resourceID]
	return present
}

func acquireNotionAdminTokenLifecycleLock(lifecycleKey string) func() {
	return acquireProductOperationLock(notionAdminTokenLifecycleLockNamespace, lifecycleKey)
}
