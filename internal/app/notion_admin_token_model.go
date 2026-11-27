package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAdminTokenSecretDirectory     = ".secrets"
	notionAdminTokenSecretFilename      = "notion-admin-token.key"
	notionAdminTokenSecretRecoveryFile  = "notion-admin-token.key.v0-recovery"
	notionAdminTokenSecretFormatVersion = 1
	notionAdminTokenCreateOperation     = "create"
	notionAdminTokenRevokeOperation     = "revoke"
)

var (
	notionAdminTokenSecretMutex      sync.Mutex
	errNotionAdminTokenInvalid       = errors.New(errorCodeNotionAdminTokenInvalid)
	errNotionAdminTokenNotFound      = errors.New(errorCodeNotionAdminTokenNotFound)
	errNotionAdminTokenRequestReused = errors.New(errorCodeNotionAdminTokenRequestReused)
	errNotionAdminTokenUnavailable   = errors.New(errorCodeNotionAdminTokenUnavailable)
	errNotionAdminTokenCursorInvalid = errors.New(errorCodeNotionAdminTokenCursorInvalid)
)

type storedNotionAdminToken struct {
	ID             string `db:"id"`
	OrganizationID string `db:"organization_id"`
	Label          string `db:"label"`
	Status         string `db:"status"`
	TokenPrefix    string `db:"token_prefix"`
	TokenHash      string `db:"token_hash"`
	ScopesJSON     string `db:"scopes_json"`
	CreatedBy      string `db:"created_by"`
	LastUsedAt     int64  `db:"last_used_at"`
	ExpiresAt      int64  `db:"expires_at"`
	RevokedAt      int64  `db:"revoked_at"`
	RevokedBy      string `db:"revoked_by"`
	Created        int64  `db:"created"`
	Updated        int64  `db:"updated"`
}

type storedNotionAdminTokenRequest struct {
	OrganizationID string `db:"organization_id"`
	ActorID        string `db:"actor_id"`
	RequestID      string `db:"request_id"`
	Operation      string `db:"operation"`
	RequestHash    string `db:"request_hash"`
	TokenID        string `db:"token_id"`
	Created        int64  `db:"created"`
}

type normalizedNotionAdminTokenCreate struct {
	RequestID    string   `json:"requestId"`
	Label        string   `json:"label"`
	Capabilities []string `json:"capabilities"`
	WorkspaceIDs []string `json:"workspaceIds"`
	LegalHoldIDs []string `json:"legalHoldIds"`
	ExpiresAt    int64    `json:"expiresAt"`
}

func notionAdminTokenSecret(app core.App) (string, error) {
	return loadOrCreateVersionedSecretFile(versionedSecretFileConfig{
		Directory: filepath.Join(app.DataDir(), notionAdminTokenSecretDirectory),
		Filename:  notionAdminTokenSecretFilename, RecoveryFilename: notionAdminTokenSecretRecoveryFile,
		FormatVersion: notionAdminTokenSecretFormatVersion, Mutex: &notionAdminTokenSecretMutex,
		Failure:      func() error { return errNotionAdminTokenUnavailable },
		BeforeCreate: func() error { return requireNoNotionAdminTokenCredentials(app) },
	})
}

func requireNoNotionAdminTokenCredentials(app core.App) error {
	var count int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_notion_admin_tokens}}`).Row(&count); err != nil {
		return err
	}
	if count != 0 {
		return errNotionAdminTokenUnavailable
	}
	return nil
}

func notionAdminTokenCredential(app core.App, id string) (string, error) {
	secret, err := notionAdminTokenSecret(app)
	if err != nil {
		return "", err
	}
	return deriveDeterministicCredential(
		secret, "notion-admin-token:v1", "bearer", id, notionAdminTokenSecretPrefix,
	), nil
}

func normalizeNotionAdminTokenCreate(input notionAdminTokenCreateInput) (normalizedNotionAdminTokenCreate, error) {
	normalized := normalizedNotionAdminTokenCreate{
		RequestID: strings.TrimSpace(input.RequestID), Label: strings.TrimSpace(input.Label),
	}
	if normalized.RequestID == "" || normalized.Label == "" ||
		utf8.RuneCountInString(normalized.RequestID) > notionAdminTokenMaxRequestIDRunes ||
		utf8.RuneCountInString(normalized.Label) > notionAdminTokenMaxLabelRunes {
		return normalizedNotionAdminTokenCreate{}, errNotionAdminTokenInvalid
	}
	var err error
	if normalized.Capabilities, err = normalizeNotionAdminValues(input.Capabilities, true); err != nil {
		return normalizedNotionAdminTokenCreate{}, err
	}
	if normalized.WorkspaceIDs, err = normalizeNotionAdminValues(input.WorkspaceIDs, false); err != nil {
		return normalizedNotionAdminTokenCreate{}, err
	}
	if normalized.LegalHoldIDs, err = normalizeNotionAdminValues(input.LegalHoldIDs, false); err != nil {
		return normalizedNotionAdminTokenCreate{}, err
	}
	if input.ExpiresAt != nil {
		expires, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*input.ExpiresAt))
		if err != nil || expires.UnixMilli() <= 0 {
			return normalizedNotionAdminTokenCreate{}, errNotionAdminTokenInvalid
		}
		normalized.ExpiresAt = expires.UTC().UnixMilli()
	}
	return normalized, nil
}

func normalizeNotionAdminValues(values []string, capabilities bool) ([]string, error) {
	if len(values) > notionAdminTokenMaxResourceIDs || capabilities && len(values) == 0 {
		return nil, errNotionAdminTokenInvalid
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || utf8.RuneCountInString(value) > notionAdminTokenMaxResourceIDRunes {
			return nil, errNotionAdminTokenInvalid
		}
		if capabilities && !validNotionAdminCapability(value) {
			return nil, errNotionAdminTokenInvalid
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if !capabilities && len(result) > 1 {
		if _, wildcard := seen["*"]; wildcard {
			return nil, errNotionAdminTokenInvalid
		}
	}
	sort.Strings(result)
	return result, nil
}

func validNotionAdminCapability(value string) bool {
	_, valid := notionAdminCapabilitySet[value]
	return valid
}

func hashNotionAdminTokenRequest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func encodeNotionAdminTokenScopes(scopes notionAdminTokenScopes) (string, error) {
	encoded, err := json.Marshal(scopes)
	return string(encoded), err
}

func (stored storedNotionAdminToken) response() (notionAdminTokenResponse, error) {
	scopes := notionAdminTokenScopes{}
	if err := json.Unmarshal([]byte(stored.ScopesJSON), &scopes); err != nil ||
		!validStoredNotionAdminScopes(scopes, stored.OrganizationID) {
		return notionAdminTokenResponse{}, failedBecause(errNotionAdminTokenUnavailable, err)
	}
	response := notionAdminTokenResponse{
		ID: stored.ID, OrganizationID: stored.OrganizationID, Label: stored.Label,
		Status: stored.Status, TokenPrefix: stored.TokenPrefix, Scopes: scopes,
		CreatedBy: stored.CreatedBy, RevokedBy: stored.RevokedBy,
		CreatedAt: notionAdminTokenTime(stored.Created), UpdatedAt: notionAdminTokenTime(stored.Updated),
	}
	response.LastUsedAt = notionAdminTokenOptionalTime(stored.LastUsedAt)
	response.ExpiresAt = notionAdminTokenOptionalTime(stored.ExpiresAt)
	response.RevokedAt = notionAdminTokenOptionalTime(stored.RevokedAt)
	return response, nil
}

func notionAdminTokenTime(value int64) string {
	return time.UnixMilli(value).UTC().Format(time.RFC3339Nano)
}

func notionAdminTokenOptionalTime(value int64) *string {
	if value == 0 {
		return nil
	}
	formatted := notionAdminTokenTime(value)
	return &formatted
}

func classifyNotionAdminTokenError(err error) error {
	if err == nil || errors.Is(err, errNotionAdminTokenInvalid) ||
		errors.Is(err, errNotionAdminTokenNotFound) ||
		errors.Is(err, errNotionAdminTokenRequestReused) ||
		errors.Is(err, errNotionAdminTokenCursorInvalid) || isPermissionDenied(err) {
		return err
	}
	return errors.Join(errNotionAdminTokenUnavailable, err)
}
