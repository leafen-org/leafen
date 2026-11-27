package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	errorCodeUserKeyConfirmationRequired    = "LEAFEN_USER_KEY_CONFIRMATION_REQUIRED"
	errorCodeOrganizationUserKeyPolicyInput = "LEAFEN_ORGANIZATION_USER_KEY_POLICY_INVALID"
	userKeyConfirmationHeader               = "Leafen-Confirmation"
	userKeyConfirmationLifetime             = 5 * time.Minute
	maxUserKeyDays                          = 3650
)

// userKeyPolicyError is an organization's refusal of a key being made.
type userKeyPolicyError struct{ reason string }

func (err userKeyPolicyError) Error() string { return err.reason }

var defaultOrganizationUserKeyPolicy = organizationUserKeyPolicy{KeysAllowed: true, MaxScope: "admin", MaxDays: 0}

func loadOrganizationUserKeyPolicy(app core.App, organizationID string) (organizationUserKeyPolicy, error) {
	stored := struct {
		KeysAllowed int    `db:"keys_allowed"`
		MaxScope    string `db:"max_scope"`
		MaxDays     int    `db:"max_days"`
	}{}
	err := app.DB().NewQuery(`SELECT [[keys_allowed]], [[max_scope]], [[max_days]]
		FROM {{leafen_organization_user_key_policies}} WHERE [[organization_id]] = {:id}`).
		Bind(dbx.Params{"id": organizationID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultOrganizationUserKeyPolicy, nil
	}
	if err != nil {
		return organizationUserKeyPolicy{}, err
	}
	return organizationUserKeyPolicy{KeysAllowed: stored.KeysAllowed == 1, MaxScope: stored.MaxScope, MaxDays: stored.MaxDays}, nil
}

// userKeyPolicyDenial says why the organizations of the workspaces a call
// acts in refuse this key now, or nothing: keys may be turned off or held
// to a shorter life after a key was made. A lower scope doesn't refuse the
// key; it holds what the key does (userKeyPolicyCap).
func userKeyPolicyDenial(app core.App, key userKeyAuthority, required userKeyScope, workspaceIDs []string) (string, error) {
	// An organization decides on apps through their grants, not its key policy.
	if key.AppInstallID != "" {
		return "", nil
	}
	for _, workspaceID := range workspaceIDs {
		var organizationID string
		if err := app.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": workspaceID}).Row(&organizationID); err != nil || organizationID == "" {
			if errors.Is(err, sql.ErrNoRows) {
				err = nil
			}
			if err != nil {
				return "", err
			}
			continue
		}
		policy, err := loadOrganizationUserKeyPolicy(app, organizationID)
		if err != nil {
			return "", err
		}
		if denial := organizationUserKeyDenial(policy, key.Created, key.ExpiresAt); denial != "" {
			return denial, nil
		}
	}
	return "", nil
}

// organizationKeysOffDenial is the denial of an organization that allows no keys.
const organizationKeysOffDenial = "This workspace's organization doesn't allow keys."

func organizationUserKeyDenial(policy organizationUserKeyPolicy, created int64, expiresAt int64) string {
	if !policy.KeysAllowed {
		return organizationKeysOffDenial
	}
	if policy.MaxDays > 0 && (expiresAt == 0 || expiresAt-created > int64(policy.MaxDays)*24*int64(time.Hour/time.Millisecond)) {
		return fmt.Sprintf("This workspace's organization requires keys to expire within %d days.", policy.MaxDays)
	}
	return ""
}

func registerOrganizationUserKeyPolicyRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+organizationUserKeyPolicyReadPath, readOrganizationUserKeyPolicy)
	event.Router.PUT(apiRoot+organizationUserKeyPolicyUpdatePath, saveOrganizationUserKeyPolicy)
}

func authorizeUserKeyPolicyManagement(event *core.RequestEvent) (organizationAuthorization, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return organizationAuthorization{}, false
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	authorization, err := authorizeOrganization(event.App, user.Id, organizationID, permissionOrganizationSecurityAdmin)
	if isPermissionDenied(err) {
		_ = apiError(event, http.StatusNotFound, errorCodeOrganizationNotFound, "Organization not found.")
		return organizationAuthorization{}, false
	}
	if err != nil {
		_ = apiErrorFrom(event, http.StatusInternalServerError, errorCodeOrganizationAuthorityInvalid, "Couldn't check organization permission.", err)
		return organizationAuthorization{}, false
	}
	return authorization, true
}

func readOrganizationUserKeyPolicy(event *core.RequestEvent) error {
	authorization, ok := authorizeUserKeyPolicyManagement(event)
	if !ok {
		return nil
	}
	policy, err := loadOrganizationUserKeyPolicy(event.App, authorization.OrganizationID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeUserKeyFailed, "Couldn't read the key policy.", err)
	}
	return event.JSON(http.StatusOK, policy)
}

func saveOrganizationUserKeyPolicy(event *core.RequestEvent) error {
	authorization, ok := authorizeUserKeyPolicyManagement(event)
	if !ok {
		return nil
	}
	var input organizationUserKeyPolicy
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeOrganizationUserKeyPolicyInput, "Check the key policy.")
	}
	if _, known := parseUserKeyScope(input.MaxScope); !known || input.MaxDays < 0 || input.MaxDays > maxUserKeyDays {
		return apiError(event, http.StatusBadRequest, errorCodeOrganizationUserKeyPolicyInput, "Check the key policy.")
	}
	// A key's call asks its person once the policy reads (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	allowed := 0
	if input.KeysAllowed {
		allowed = 1
	}
	if _, err := event.App.DB().NewQuery(`
		INSERT INTO {{leafen_organization_user_key_policies}}
			([[organization_id]], [[keys_allowed]], [[max_scope]], [[max_days]], [[updated_at]], [[updated_by]])
		VALUES ({:organization}, {:allowed}, {:scope}, {:days}, {:now}, {:by})
		ON CONFLICT ([[organization_id]]) DO UPDATE SET [[keys_allowed]] = excluded.[[keys_allowed]],
			[[max_scope]] = excluded.[[max_scope]], [[max_days]] = excluded.[[max_days]],
			[[updated_at]] = excluded.[[updated_at]], [[updated_by]] = excluded.[[updated_by]]
	`).Bind(dbx.Params{
		"organization": authorization.OrganizationID, "allowed": allowed, "scope": input.MaxScope,
		"days": input.MaxDays, "now": time.Now().UnixMilli(), "by": authorization.PrincipalID,
	}).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeUserKeyFailed, "Couldn't save the key policy.", err)
	}
	if err := insertOrganizationAuditEvent(event.App, organizationAuditEvent{
		OrganizationID: authorization.OrganizationID, ActorID: authorization.PrincipalID,
		Action: "user_key_policy.update", TargetType: "organization", TargetID: authorization.OrganizationID,
		Metadata: map[string]any{"keysAllowed": input.KeysAllowed, "maxScope": input.MaxScope, "maxDays": input.MaxDays},
	}); err != nil {
		event.App.Logger().Warn("leafen key policy audit failed", "error", err)
	}
	return event.JSON(http.StatusOK, input)
}

// A call the key rules confirm runs only when sent again with the
// confirmation its first sending returned: the same key, method, path and
// body, within five minutes, once. An MCP token that asks its person waits
// for their approval instead (mcp_action_approvals.go).
type userKeyConfirmation struct {
	ConnectionID string
	Request      string
	ExpiresAt    time.Time
}

var userKeyConfirmations = struct {
	sync.Mutex
	entries map[string]userKeyConfirmation
}{entries: map[string]userKeyConfirmation{}}

func requireUserKeyConfirmation(event *core.RequestEvent, key userKeyAuthority, rule userKeyRule, body []byte) (bool, error) {
	digest := sha256.Sum256(append([]byte(event.Request.Method+" "+event.Request.URL.RequestURI()+"\x00"), body...))
	request := hex.EncodeToString(digest[:])
	if asks, err := mcpTokenAsksAction(event.App, key.ConnectionID); err != nil {
		return true, apiErrorFrom(event, http.StatusInternalServerError, errorCodeUserKeyFailed, "Couldn't check the key.", err)
	} else if asks || rule.everyTime {
		return requireMCPActionApproval(event, key, rule, request, body)
	}
	now := time.Now()
	userKeyConfirmations.Lock()
	defer userKeyConfirmations.Unlock()
	for token, entry := range userKeyConfirmations.entries {
		if !entry.ExpiresAt.After(now) {
			delete(userKeyConfirmations.entries, token)
		}
	}
	if token := strings.TrimSpace(event.Request.Header.Get(userKeyConfirmationHeader)); token != "" {
		entry, found := userKeyConfirmations.entries[token]
		if found && entry.ConnectionID == key.ConnectionID && entry.Request == request {
			delete(userKeyConfirmations.entries, token)
			return false, nil
		}
	}
	token := security.RandomString(32)
	userKeyConfirmations.entries[token] = userKeyConfirmation{
		ConnectionID: key.ConnectionID, Request: request, ExpiresAt: now.Add(userKeyConfirmationLifetime),
	}
	event.Response.Header().Set(userKeyConfirmationHeader, token)
	return true, apiError(event, http.StatusPreconditionRequired, errorCodeUserKeyConfirmationRequired,
		"This goes ahead on a second request: send the same one again within five minutes with the "+
			userKeyConfirmationHeader+" header this response carries.")
}

// userKeyPolicyCap is the highest scope the organizations of the workspaces
// allow a key: a key or agent connection does everything its person may
// (agent full-permission decision of 2026-10-16), less only what an
// organization chose to hold back.
func userKeyPolicyCap(app core.App, workspaceIDs []string) (userKeyScope, error) {
	capped := userKeyScopeAdmin
	seen := map[string]bool{}
	for _, workspaceID := range workspaceIDs {
		var organizationID string
		if err := app.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": workspaceID}).Row(&organizationID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return userKeyScopeClosed, err
		}
		if organizationID == "" || seen[organizationID] {
			continue
		}
		seen[organizationID] = true
		policy, err := loadOrganizationUserKeyPolicy(app, organizationID)
		if err != nil {
			return userKeyScopeClosed, err
		}
		if limit, ok := parseUserKeyScope(policy.MaxScope); ok && limit < capped {
			capped = limit
		}
	}
	return capped, nil
}
