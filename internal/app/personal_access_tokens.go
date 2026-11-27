package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's member or owner makes personal access tokens for the Public
// API in the settings' Developer item, as in Notion (seen 2026-10-02): a
// token has a name and lasts 7, 30, 90 or 180 days or a year, acts as its
// maker in its workspace, and shows its secret only once, when it is made.
// The maker lists and revokes their own tokens here; the workspace's owner
// lists (`?scope=workspace`) and revokes all of them, as Notion's "모든 개인
// 액세스 토큰" in the connections' "관리" does. A token can only use
// the Public API ("notion-api"); Leafen has no Workers. Guests
// can't make one, and the schema's triggers revoke a maker's tokens when
// they leave the workspace (notion_personal_access_token_schema.go).

const (
	personalAccessTokenCollectionPath = "/workspaces/{workspaceId}/personal-access-tokens"
	personalAccessTokenRevokePath     = personalAccessTokenCollectionPath + "/{tokenId}/revoke"
	personalAccessTokenSecretPrefix   = "ntn_pat_"
	personalAccessTokenMaxNameRunes   = 200
	// A person keeps at most this many working tokens in a workspace; the
	// list reads the newest first a window at a time, to the last.
	personalAccessTokenMaxActive  = 50
	personalAccessTokenListLimit  = 200
	personalAccessTokenCursorSize = 256
	personalAccessTokenBodyBytes  = 4 << 10
	personalAccessTokenSecretSize = 32

	errorCodePersonalAccessTokenInvalid  = "LEAFEN_PERSONAL_ACCESS_TOKEN_INVALID"
	errorCodePersonalAccessTokenNotFound = "LEAFEN_PERSONAL_ACCESS_TOKEN_NOT_FOUND"
	errorCodePersonalAccessTokenLimit    = "LEAFEN_PERSONAL_ACCESS_TOKEN_LIMIT"
	errorCodePersonalAccessTokenFailed   = "LEAFEN_PERSONAL_ACCESS_TOKEN_FAILED"
	errorCodePersonalAccessTokenOwners   = "LEAFEN_PERSONAL_ACCESS_TOKEN_OWNERS_ONLY"
)

var (
	errPersonalAccessTokenInvalid  = errors.New(errorCodePersonalAccessTokenInvalid)
	errPersonalAccessTokenNotFound = errors.New(errorCodePersonalAccessTokenNotFound)
	errPersonalAccessTokenLimit    = errors.New(errorCodePersonalAccessTokenLimit)
	errPersonalAccessTokenOwners   = errors.New(errorCodePersonalAccessTokenOwners)
)

// The lifetimes Notion offers; a year ends on the same date a year later.
// errPersonalAccessTokenWorkspace is a workspace in the path that isn't one.
var errPersonalAccessTokenWorkspace = refuse(errPersonalAccessTokenInvalid, "the workspace ID in the path isn't a workspace's id")

var personalAccessTokenLifetimes = map[string]func(time.Time) time.Time{
	"7d":   func(now time.Time) time.Time { return now.AddDate(0, 0, 7) },
	"30d":  func(now time.Time) time.Time { return now.AddDate(0, 0, 30) },
	"90d":  func(now time.Time) time.Time { return now.AddDate(0, 0, 90) },
	"180d": func(now time.Time) time.Time { return now.AddDate(0, 0, 180) },
	"1y":   func(now time.Time) time.Time { return now.AddDate(1, 0, 0) },
}

type personalAccessTokenCreateInput struct {
	Name      string `json:"name"`
	ExpiresIn string `json:"expiresIn"`
}

type personalAccessTokenResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Only in the owner's list of the workspace's tokens.
	Creator    *workspaceConnectionPerson `json:"creator,omitempty"`
	Status     string                     `json:"status"`
	CreatedAt  int64                      `json:"createdAt"`
	ExpiresAt  int64                      `json:"expiresAt"`
	LastUsedAt *int64                     `json:"lastUsedAt,omitempty"`
	RevokedAt  *int64                     `json:"revokedAt,omitempty"`
}

type personalAccessTokenListResponse struct {
	Items      []personalAccessTokenResponse `json:"items"`
	NextCursor string                        `json:"nextCursor,omitempty"`
	// How many working tokens a person may keep in the workspace.
	ActiveLimit int `json:"activeLimit"`
}

// personalAccessTokenCursor is where the next window of the list starts.
type personalAccessTokenCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

type personalAccessTokenCreateResponse struct {
	Token personalAccessTokenResponse `json:"token"`
	// The secret is in this response only; the server keeps its hash.
	Secret string `json:"secret"`
}

func registerPersonalAccessTokenRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+personalAccessTokenCollectionPath, listPersonalAccessTokens)
	event.Router.POST(apiRoot+personalAccessTokenCollectionPath, createPersonalAccessToken).
		Bind(apis.BodyLimit(personalAccessTokenBodyBytes))
	event.Router.POST(apiRoot+personalAccessTokenRevokePath, revokePersonalAccessToken).
		Bind(apis.BodyLimit(personalAccessTokenBodyBytes))
}

func personalAccessTokenWorkspaceID(event *core.RequestEvent) (string, bool) {
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	return workspaceID, workspaceID != "" && len(workspaceID) <= maxWorkspacePermissionIdentifierLength
}

func listPersonalAccessTokens(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID, ok := personalAccessTokenWorkspaceID(event)
	if !ok {
		return personalAccessTokenError(event, errPersonalAccessTokenWorkspace, "list-validate")
	}
	requirement := permissionWorkspaceRead
	everyone := event.Request.URL.Query().Get("scope") == "workspace"
	if everyone {
		requirement = permissionWorkspaceOwner
	}
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, requirement); err != nil {
		return personalAccessTokenError(event, err, "list")
	}
	after := personalAccessTokenCursor{At: math.MaxInt64}
	if raw := event.Request.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if len(raw) > personalAccessTokenCursorSize || err != nil || json.Unmarshal(decoded, &after) != nil || after.ID == "" {
			return personalAccessTokenError(event, refuse(errPersonalAccessTokenInvalid, "cursor isn't one this list gave: leave it out to read from the start"), "list-cursor")
		}
	}
	type listedToken struct {
		storedNotionPersonalAccessToken
		CreatorName string `db:"creator_name"`
	}
	rows := []listedToken{}
	err := event.App.DB().NewQuery(`
		SELECT [[token]].[[id]], [[token]].[[workspace_id]], [[token]].[[creator_id]], [[token]].[[token_hash]],
			[[token]].[[token_prefix]], [[token]].[[name]], [[token]].[[status]], [[token]].[[capabilities_json]],
			[[token]].[[credential_generation]], [[token]].[[created_at]], [[token]].[[expires_at]],
			[[token]].[[last_used_at]], [[token]].[[revoked_at]], [[token]].[[updated_at]],
			[[token]].[[status]] AS [[projected_status]], COALESCE([[person]].[[name]], '') AS [[creator_name]]
		FROM {{leafen_notion_personal_access_tokens}} [[token]]
		LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[token]].[[creator_id]]
		WHERE [[token]].[[workspace_id]] = {:workspaceId} AND ({:everyone} OR [[token]].[[creator_id]] = {:userId})
			AND ([[token]].[[created_at]], [[token]].[[id]]) < ({:afterAt}, {:afterId})
		ORDER BY [[token]].[[created_at]] DESC, [[token]].[[id]] DESC
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "userId": user.Id, "everyone": everyone, "limit": personalAccessTokenListLimit + 1,
		"afterAt": after.At, "afterId": after.ID,
	}).All(&rows)
	if err != nil {
		return personalAccessTokenError(event, err, "list")
	}
	response := personalAccessTokenListResponse{ActiveLimit: personalAccessTokenMaxActive}
	if len(rows) > personalAccessTokenListLimit {
		rows = rows[:personalAccessTokenListLimit]
		last := rows[len(rows)-1]
		encoded, err := json.Marshal(personalAccessTokenCursor{At: last.CreatedAt, ID: last.ID})
		if err != nil {
			return personalAccessTokenError(event, err, "list-cursor")
		}
		response.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	now := time.Now().UTC().UnixMilli()
	response.Items = make([]personalAccessTokenResponse, 0, len(rows))
	for _, row := range rows {
		view := personalAccessTokenView(row.storedNotionPersonalAccessToken, now)
		if everyone {
			view.Creator = &workspaceConnectionPerson{ID: row.CreatorID, Name: row.CreatorName}
		}
		response.Items = append(response.Items, view)
	}
	return event.JSON(http.StatusOK, response)
}

func createPersonalAccessToken(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID, ok := personalAccessTokenWorkspaceID(event)
	input := personalAccessTokenCreateInput{}
	if err := event.BindBody(&input); err != nil {
		return personalAccessTokenError(event, bodyRefusal(errPersonalAccessTokenInvalid, "personal access token", err), "create-bind")
	}
	if !ok {
		return personalAccessTokenError(event, errPersonalAccessTokenWorkspace, "create-bind")
	}
	name := strings.TrimSpace(input.Name)
	lifetime, known := personalAccessTokenLifetimes[input.ExpiresIn]
	if name == "" || utf8.RuneCountInString(name) > personalAccessTokenMaxNameRunes {
		return personalAccessTokenError(event, refuse(errPersonalAccessTokenInvalid, "name is 1–%d characters", personalAccessTokenMaxNameRunes), "create-validate")
	}
	if !known {
		return personalAccessTokenError(event, refuse(errPersonalAccessTokenInvalid, "expiresIn is 7d, 30d, 90d, 180d or 1y, not %q", input.ExpiresIn), "create-validate")
	}
	secret, err := newPersonalAccessTokenSecret()
	if err != nil {
		return personalAccessTokenError(event, err, "create-secret")
	}

	created := storedNotionPersonalAccessToken{}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		var issueErr error
		created, issueErr = issuePersonalAccessToken(txApp, user.Id, workspaceID, name, lifetime, secret)
		return issueErr
	})
	if err != nil {
		return personalAccessTokenError(event, err, "create")
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusCreated, personalAccessTokenCreateResponse{
		Token: personalAccessTokenView(created, created.CreatedAt), Secret: secret,
	})
}

func revokePersonalAccessToken(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID, ok := personalAccessTokenWorkspaceID(event)
	tokenID := strings.TrimSpace(event.Request.PathValue("tokenId"))
	if !ok || !validNotionCompatibleInternalID(tokenID) {
		return personalAccessTokenError(event, errPersonalAccessTokenNotFound, "revoke-validate")
	}
	release := acquireNotionPersonalAccessTokenLifecycleLock(tokenID)
	defer release()
	stored := storedNotionPersonalAccessToken{}
	revoke := func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceRead)
		if err != nil {
			return err
		}
		load := func() error {
			err := txApp.DB().NewQuery(`
				SELECT [[id]], [[workspace_id]], [[creator_id]], [[token_hash]], [[token_prefix]],
					[[name]], [[status]], [[capabilities_json]], [[credential_generation]],
					[[created_at]], [[expires_at]], [[last_used_at]], [[revoked_at]], [[updated_at]],
					[[status]] AS [[projected_status]]
				FROM {{leafen_notion_personal_access_tokens}}
				WHERE [[id]] = {:tokenId} AND [[workspace_id]] = {:workspaceId}
					AND ({:owner} OR [[creator_id]] = {:userId})
				LIMIT 2
			`).Bind(dbx.Params{
				"tokenId": tokenID, "workspaceId": workspaceID, "userId": user.Id,
				"owner": authorization.Role == workspaceRoleOwner,
			}).One(&stored)
			if errors.Is(err, sql.ErrNoRows) {
				return errPersonalAccessTokenNotFound
			}
			return err
		}
		if err := load(); err != nil || stored.Status == "revoked" {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_personal_access_tokens}}
			SET [[status]] = 'revoked', [[revoked_at]] = {:now}, [[updated_at]] = {:now}
			WHERE [[id]] = {:tokenId} AND [[workspace_id]] = {:workspaceId} AND [[status]] = 'active'
		`).Bind(dbx.Params{"now": now, "tokenId": tokenID, "workspaceId": workspaceID}).Execute(); err != nil {
			return err
		}
		if err := insertOrganizationAuditEvent(txApp, organizationAuditEvent{
			OrganizationID: authorization.Organization.OrganizationID, WorkspaceID: workspaceID,
			ActorID: user.Id, Action: "personal_access_token.revoke",
			TargetType: "personal_access_token", TargetID: tokenID, OccurredAt: now,
		}); err != nil {
			return err
		}
		return load()
	}
	// A key's call asks its person only for a token it would revoke
	// (user_key_acting_ask.go).
	err := rehearseBeforeAsking(event, revoke)
	if err == nil {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(revoke)
	}
	if err != nil {
		return personalAccessTokenError(event, err, "revoke")
	}
	return event.JSON(http.StatusOK, personalAccessTokenView(stored, time.Now().UTC().UnixMilli()))
}

func newPersonalAccessTokenSecret() (string, error) {
	random := make([]byte, personalAccessTokenSecretSize)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return personalAccessTokenSecretPrefix + base64.RawURLEncoding.EncodeToString(random), nil
}

// personalAccessTokenView shows a stored token without its secret or hash;
// a working token past its expiry shows as expired.
func personalAccessTokenView(stored storedNotionPersonalAccessToken, now int64) personalAccessTokenResponse {
	view := personalAccessTokenResponse{
		ID: stored.ID, Name: stored.Name, Status: stored.Status,
		CreatedAt: stored.CreatedAt, ExpiresAt: stored.ExpiresAt,
	}
	if stored.Status == "active" && stored.ExpiresAt <= now {
		view.Status = "expired"
	}
	if stored.LastUsedAt > 0 {
		lastUsedAt := stored.LastUsedAt
		view.LastUsedAt = &lastUsedAt
	}
	if stored.RevokedAt.Valid {
		revokedAt := stored.RevokedAt.Int64
		view.RevokedAt = &revokedAt
	}
	return view
}

func personalAccessTokenError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusInternalServerError,
		errorCodePersonalAccessTokenFailed, "Couldn't complete the personal access token request."
	switch {
	case errors.Is(err, errPersonalAccessTokenInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodePersonalAccessTokenInvalid, refusalMessage("Check the token's name and expiry.", refusalReason(err))
	case errors.Is(err, errPersonalAccessTokenNotFound), isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodePersonalAccessTokenNotFound, "Personal access token not found."
	case errors.Is(err, errPersonalAccessTokenLimit):
		status, code, message = http.StatusConflict,
			errorCodePersonalAccessTokenLimit, "Revoke a token you no longer use before making another."
	case errors.Is(err, errPersonalAccessTokenOwners):
		status, code, message = http.StatusForbidden,
			errorCodePersonalAccessTokenOwners, "Only the workspace's owners can make personal access tokens."
	}
	return writeAPIError(
		event, status, code, message, "personal_access_token", stage, code, err,
		diagnosticSourcePosition(1),
	)
}

// issuePersonalAccessToken makes one of a person's tokens for a workspace,
// as the settings screen does: within the workspace's policy and the
// person's limit, audited.
func issuePersonalAccessToken(
	txApp core.App, userID, workspaceID, name string, lifetime func(time.Time) time.Time, secret string,
) (storedNotionPersonalAccessToken, error) {
	created := storedNotionPersonalAccessToken{}
	authorization, err := authorizeWorkspace(txApp, userID, workspaceID, permissionWorkspaceRead)
	if err != nil {
		return created, err
	}
	// The workspace may limit tokens to its owners.
	if err := requireWorkspacePolicy(
		txApp, workspaceID, userID, authorization.Role, personalAccessTokensLimit, errPersonalAccessTokenOwners,
	); err != nil {
		return created, err
	}
	now := time.Now().UTC()
	active := 0
	if err := txApp.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_notion_personal_access_tokens}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[creator_id]] = {:userId}
			AND [[status]] = 'active' AND [[expires_at]] > {:now}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "userId": userID, "now": now.UnixMilli(),
	}).Row(&active); err != nil {
		return created, err
	}
	if active >= personalAccessTokenMaxActive {
		return created, errPersonalAccessTokenLimit
	}
	created = storedNotionPersonalAccessToken{
		ID: core.GenerateDefaultRandomId(), WorkspaceID: workspaceID, CreatorID: userID,
		TokenHash: notionPersonalAccessTokenHash(secret), TokenPrefix: secret[:12],
		Name: name, Status: "active", CapabilitiesJSON: `["notion-api"]`, CredentialGeneration: 1,
		CreatedAt: now.UnixMilli(), ExpiresAt: lifetime(now).UnixMilli(), UpdatedAt: now.UnixMilli(),
	}
	if _, err := txApp.DB().Insert(notionPersonalAccessTokensTable, dbx.Params{
		"id": created.ID, "workspace_id": created.WorkspaceID, "creator_id": created.CreatorID,
		"token_hash": created.TokenHash, "token_prefix": created.TokenPrefix, "name": created.Name,
		"status": created.Status, "capabilities_json": created.CapabilitiesJSON,
		"credential_generation": created.CredentialGeneration, "created_at": created.CreatedAt,
		"expires_at": created.ExpiresAt, "last_used_at": 0, "updated_at": created.UpdatedAt,
	}).Execute(); err != nil {
		return created, err
	}
	return created, insertOrganizationAuditEvent(txApp, organizationAuditEvent{
		OrganizationID: authorization.Organization.OrganizationID, WorkspaceID: workspaceID,
		ActorID: userID, Action: "personal_access_token.create",
		TargetType: "personal_access_token", TargetID: created.ID,
		Metadata: map[string]any{"expiresAt": created.ExpiresAt}, OccurredAt: created.CreatedAt,
	})
}
