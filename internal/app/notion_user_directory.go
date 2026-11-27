package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionUserDirectoryDefaultPageSize = 100
	notionUserDirectoryMaximumPageSize = 100
	notionUserDirectoryCursorVersion   = 2
	notionUserDirectoryCursorPrefix    = "hnuc1"
	notionUserDirectoryCursorMaxLength = 1024
	notionErrorObjectNotFound          = "object_not_found"
)

var errInvalidNotionUserDirectoryCursor = errors.New("invalid Notion-compatible user directory cursor")

type notionUserDirectoryCursor struct {
	Version          int    `json:"v"`
	WorkspaceID      string `json:"w"`
	PrincipalID      string `json:"p"`
	GrantID          string `json:"g"`
	GrantGeneration  int    `json:"n"`
	PermissionCursor int    `json:"r"`
	AfterUserID      string `json:"a"`
	// AfterBotGrantID is the connection of the last bot listed, once the
	// people are; the bots follow in their connections' order.
	AfterBotGrantID string `json:"b,omitempty"`
}

type storedNotionDirectoryPageRow struct {
	ID               sql.NullString `db:"id"`
	Name             sql.NullString `db:"name"`
	Avatar           sql.NullString `db:"avatar"`
	Email            sql.NullString `db:"email"`
	EmailVerified    sql.NullBool   `db:"email_verified"`
	PermissionCursor int            `db:"permission_cursor"`
}

type storedNotionDirectoryUser struct {
	ID            string `db:"id"`
	Name          string `db:"name"`
	Avatar        string `db:"avatar"`
	Email         string `db:"email"`
	EmailVerified bool   `db:"email_verified"`
}

// authorizeNotionUsersMe lets a token read its own bot as Notion's "Retrieve
// your token's bot user" does (developers.notion.com/reference/get-self,
// read 2026-11-22): any token that may read or change content or comments,
// whatever its user information capability — which decides only whether
// the owner's email shows (serveNotionOAuthUsersMe). A connection, an
// internal connection and a personal access token alike; one with none of
// those capabilities is refused (2026-11-22: an OAuth connection without
// user information was refused, as if listing the workspace's people).
func authorizeNotionUsersMe(event *core.RequestEvent) (notionOAuthBearerIdentity, bool, error) {
	identity, ok, err := notionPublicAPIBearerFromRequest(event)
	if err != nil {
		return notionOAuthBearerIdentity{}, false, notionRequestNotCompleted(event, err)
	}
	if !ok {
		return notionOAuthBearerIdentity{}, false, notionUnauthorized(event)
	}
	if valid, err := validateNotionUserDirectoryVersion(event); !valid {
		return notionOAuthBearerIdentity{}, false, err
	}
	if !notionTokenReadsOrChangesContent(identity) {
		return notionOAuthBearerIdentity{}, false, notionCompatibilityError(
			event, http.StatusForbidden, notionErrorRestrictedResource, notionInsufficientPermissions,
		)
	}
	return identity, true, nil
}

// notionTokenReadsOrChangesContent is whether a token has any of the
// content and comment capabilities: reading, updating or inserting content,
// reading or inserting comments.
func notionTokenReadsOrChangesContent(identity notionOAuthBearerIdentity) bool {
	for _, scope := range []string{"pages:read", "pages:write", "databases:read", "databases:write", "files:read", "files:write",
		notionContentUpdateScope, notionContentInsertScope, "comments:read", "comments:write"} {
		if notionOAuthHasScope(identity, scope) {
			return true
		}
	}
	return false
}

// A connection's token and a personal access token both read the
// workspace's people (2026-10-16: a personal access token was refused with
// the header-format message); only a connection's lists them all, as
// Notion's (notionRouteShape.patRefused).
func authorizeNotionUserDirectory(event *core.RequestEvent) (notionOAuthBearerIdentity, bool, error) {
	identity, ok, err := notionPublicAPIBearerFromRequest(event)
	if err != nil {
		return notionOAuthBearerIdentity{}, false, notionRequestNotCompleted(event, err)
	}
	if !ok {
		return notionOAuthBearerIdentity{}, false, notionUnauthorized(event)
	}
	if valid, err := validateNotionUserDirectoryVersion(event); !valid {
		return notionOAuthBearerIdentity{}, false, err
	}
	if !notionOAuthHasScope(identity, "workspace:read") {
		return notionOAuthBearerIdentity{}, false, notionCompatibilityError(
			event, http.StatusForbidden, notionErrorRestrictedResource, notionInsufficientPermissions,
		)
	}
	return identity, true, nil
}

func serveNotionUserList(event *core.RequestEvent) error {
	identity, authorized, err := authorizeNotionUserDirectory(event)
	if err != nil || !authorized {
		return err
	}
	pageSize, err := notionUserDirectoryPageSize(event.Request)
	if err != nil {
		return notionCompatibilityError(event, http.StatusBadRequest, notionErrorValidation, "page_size must be a number.")
	}
	rawCursor := event.Request.URL.Query().Get("start_cursor")
	cursor, err := decodeNotionUserDirectoryCursor(event.App, rawCursor, identity)
	if err != nil {
		return notionStartCursorRefused(event, rawCursor)
	}

	rows, err := loadNotionUserDirectoryRows(
		event.Request.Context(), event.App, identity.PrincipalID, identity.WorkspaceID,
		cursor.AfterUserID, pageSize+1, "",
	)
	if isPermissionDenied(err) {
		return notionUnauthorized(event)
	}
	if err != nil || len(rows) == 0 {
		return notionRequestNotCompleted(event, err)
	}
	permissionCursor := rows[0].PermissionCursor
	// One whose workspace's people changed since can't go on either.
	if cursor.Version != 0 && cursor.PermissionCursor != permissionCursor {
		return notionStartCursorRefused(event, rawCursor)
	}

	users := make([]map[string]any, 0, pageSize)
	internalIDs := make([]string, 0, pageSize+1)
	// Each with their email for a token that may read it (notionIdentityReadsPeople).
	emails := func(email string) string { return notionIdentityEmail(event.App, identity, email) }
	for _, row := range rows {
		if !row.ID.Valid {
			continue
		}
		if !row.Email.Valid || !row.EmailVerified.Valid {
			return notionRequestNotCompleted(event, nil)
		}
		internalIDs = append(internalIDs, row.ID.String)
		if len(users) == pageSize {
			continue
		}
		user, err := notionUserObjectFromFields(
			row.ID.String, row.Name.String, row.Avatar.String, emails(row.Email.String), row.EmailVerified.Bool,
			requestOrigin(event.Request),
		)
		if err != nil {
			return notionRequestNotCompleted(event, err)
		}
		users = append(users, user)
	}
	hasMore := len(internalIDs) > pageSize
	next := notionUserDirectoryCursor{
		Version: notionUserDirectoryCursorVersion, WorkspaceID: identity.WorkspaceID,
		PrincipalID: identity.PrincipalID, GrantID: identity.GrantID,
		GrantGeneration: identity.GrantGeneration, PermissionCursor: permissionCursor,
	}
	if hasMore && pageSize == 0 {
		// An empty page goes on from where it stood.
		next.AfterUserID = cursor.AfterUserID
	} else if hasMore {
		next.AfterUserID = internalIDs[pageSize-1]
	} else {
		// The workspace's connections follow its people.
		bots, err := loadNotionDirectoryBots(event.App, identity.WorkspaceID, cursor.AfterBotGrantID, pageSize-len(users)+1)
		if err != nil {
			return notionRequestNotCompleted(event, err)
		}
		next.AfterUserID, next.AfterBotGrantID = notionUserDirectoryBotPhase, cursor.AfterBotGrantID
		for _, bot := range bots {
			if len(users) == pageSize {
				hasMore = true
				break
			}
			object, err := notionDirectoryBotObject(event.App, bot, identity, requestOrigin(event.Request))
			if err != nil {
				return notionRequestNotCompleted(event, err)
			}
			users = append(users, object)
			next.AfterBotGrantID = bot.GrantID
		}
	}
	var nextCursor *string
	if hasMore {
		encoded, err := encodeNotionUserDirectoryCursor(event.App, next)
		if err != nil {
			return notionRequestNotCompleted(event, err)
		}
		nextCursor = &encoded
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{
		"object": "list", "results": users, "next_cursor": nextCursor,
		"has_more": hasMore, "type": "user", "user": map[string]any{},
	})
}

func loadNotionUserDirectoryRows(
	ctx context.Context,
	app core.App,
	principalID string,
	workspaceID string,
	afterUserID string,
	limit int,
	query string,
) ([]storedNotionDirectoryPageRow, error) {
	rows := []storedNotionDirectoryPageRow{}
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, principalID, workspaceID, permissionWorkspaceRead)
		if err != nil {
			return err
		}
		// A restricted member lists the people they share a place with.
		visible := "1"
		if authorization.Restricted {
			visible = `[[listed]].[[id]] IN (` + restrictedVisiblePeopleSQL() + `)`
		}
		return txApp.DB().NewQuery(`
			WITH [[page_ids]] AS (SELECT [[listed]].[[id]] FROM (
				SELECT [[workspace]].[[workspace_owner]] AS [[id]]
				FROM {{leafen_workspaces}} [[workspace]]
				JOIN {{users}} [[owner]] ON [[owner]].[[id]]=[[workspace]].[[workspace_owner]]
				WHERE [[workspace]].[[id]] = {:workspaceId}
					AND [[workspace]].[[workspace_owner]] > {:afterUserId}
					AND ({:query}='' OR instr(lower(
						COALESCE(NULLIF(trim([[owner]].[[name]]),''),[[owner]].[[email]]) || char(10) ||
						[[owner]].[[email]] || char(10) || [[owner]].[[id]]
					),{:query})>0)
				UNION ALL
				SELECT [[membership]].[[user_id]] AS [[id]]
				FROM {{leafen_workspace_memberships}} [[membership]]
				JOIN {{users}} [[member]] ON [[member]].[[id]]=[[membership]].[[user_id]]
				WHERE [[membership]].[[workspace_id]] = {:workspaceId}
					AND [[membership]].[[role]] = 'member'
					AND [[membership]].[[user_id]] > {:afterUserId}
					AND ({:query}='' OR instr(lower(
						COALESCE(NULLIF(trim([[member]].[[name]]),''),[[member]].[[email]]) || char(10) ||
						[[member]].[[email]] || char(10) || [[member]].[[id]]
					),{:query})>0)
				) [[listed]] WHERE ` + visible + `
				ORDER BY [[listed]].[[id]]
				LIMIT {:pageLimit}
			), [[page]] AS (
				SELECT [[user]].[[id]], [[user]].[[name]],
					[[user]].[[profile_avatar]] AS [[avatar]], [[user]].[[email]],
					[[user]].[[verified]] AS [[email_verified]]
				FROM [[page_ids]]
				JOIN {{users}} [[user]] ON [[user]].[[id]] = [[page_ids]].[[id]]
			), [[frontier]] AS (
				SELECT COALESCE((
					SELECT [[cursor]] FROM {{leafen_workspace_permission_state}}
					WHERE [[workspace_id]] = {:workspaceId}
				), 0) AS [[permission_cursor]]
			)
			SELECT [[page]].[[id]], [[page]].[[name]], [[page]].[[avatar]], [[page]].[[email]],
				[[page]].[[email_verified]], [[frontier]].[[permission_cursor]]
			FROM [[frontier]] LEFT JOIN [[page]] ON 1 = 1
			ORDER BY [[page]].[[id]]
		`).WithContext(ctx).Bind(dbx.Params{
			"workspaceId": workspaceID,
			"readerId":    principalID,
			"afterUserId": afterUserID,
			"pageLimit":   limit,
			"query":       query,
		}).All(&rows)
	})
	return rows, err
}

func serveNotionUserRetrieve(event *core.RequestEvent, publicUserID string) error {
	identity, authorized, err := authorizeNotionUserDirectory(event)
	if err != nil || !authorized {
		return err
	}
	internalUserID, err := resolveNotionCompatibleUserID(publicUserID)
	if err != nil {
		return serveNotionDirectoryBotRetrieve(event, identity, publicUserID)
	}
	user, found, err := loadNotionUserDirectoryUser(
		event.App, identity.PrincipalID, identity.WorkspaceID, internalUserID,
	)
	if isPermissionDenied(err) {
		return notionUnauthorized(event)
	}
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	if !found {
		return notionUserNotFound(event)
	}
	response, err := notionUserObjectFromFields(
		user.ID, user.Name, user.Avatar, notionIdentityEmail(event.App, identity, user.Email), user.EmailVerified, requestOrigin(event.Request),
	)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func loadNotionUserDirectoryUser(
	app core.App,
	principalID string,
	workspaceID string,
	internalUserID string,
) (storedNotionDirectoryUser, bool, error) {
	rows := []storedNotionDirectoryUser{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		if _, err := authorizeWorkspace(txApp, principalID, workspaceID, permissionWorkspaceRead); err != nil {
			return err
		}
		return txApp.DB().NewQuery(`
			SELECT [[user]].[[id]], [[user]].[[name]],
				[[user]].[[profile_avatar]] AS [[avatar]], [[user]].[[email]],
				[[user]].[[verified]] AS [[email_verified]]
			FROM {{users}} [[user]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = {:workspaceId}
			WHERE [[user]].[[id]] = {:userId} AND ` + notionPersonInWorkspaceSQL + `
			LIMIT 2
		`).Bind(dbx.Params{
			"workspaceId": workspaceID,
			"userId":      internalUserID,
		}).All(&rows)
	})
	if err != nil {
		return storedNotionDirectoryUser{}, false, err
	}
	if len(rows) != 1 {
		return storedNotionDirectoryUser{}, false, nil
	}
	return rows[0], true, nil
}

func notionUserDirectoryPageSize(request *http.Request) (int, error) {
	raw := notionQueryDigits(request.URL.Query().Get("page_size"))
	if raw == "" {
		return notionUserDirectoryDefaultPageSize, nil
	}
	pageSize, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("invalid page_size")
	}
	return notionListPageSize(pageSize, notionUserDirectoryDefaultPageSize), nil
}

func encodeNotionUserDirectoryCursor(app core.App, cursor notionUserDirectoryCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	secret, err := notionOAuthSecret(app)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := notionUserDirectoryCursorSignature(secret, encodedPayload)
	return notionUserDirectoryCursorPrefix + "." + encodedPayload + "." +
		base64.RawURLEncoding.EncodeToString(signature), nil
}

func decodeNotionUserDirectoryCursor(
	app core.App,
	raw string,
	identity notionOAuthBearerIdentity,
) (notionUserDirectoryCursor, error) {
	if raw == "" {
		return notionUserDirectoryCursor{}, nil
	}
	if len(raw) > notionUserDirectoryCursorMaxLength {
		return notionUserDirectoryCursor{}, errInvalidNotionUserDirectoryCursor
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] != notionUserDirectoryCursorPrefix {
		return notionUserDirectoryCursor{}, errInvalidNotionUserDirectoryCursor
	}
	secret, err := notionOAuthSecret(app)
	if err != nil {
		return notionUserDirectoryCursor{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, notionUserDirectoryCursorSignature(secret, parts[1])) {
		return notionUserDirectoryCursor{}, errInvalidNotionUserDirectoryCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return notionUserDirectoryCursor{}, errInvalidNotionUserDirectoryCursor
	}
	cursor := notionUserDirectoryCursor{}
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return notionUserDirectoryCursor{}, errInvalidNotionUserDirectoryCursor
	}
	canonical, err := json.Marshal(cursor)
	if err != nil || !hmac.Equal(canonical, payload) || cursor.Version != notionUserDirectoryCursorVersion ||
		cursor.WorkspaceID != identity.WorkspaceID || cursor.PrincipalID != identity.PrincipalID ||
		cursor.GrantID != identity.GrantID || cursor.GrantGeneration != identity.GrantGeneration ||
		cursor.PermissionCursor < 0 {
		return notionUserDirectoryCursor{}, errInvalidNotionUserDirectoryCursor
	}
	// None yet is an empty first page's: the list starts over.
	if cursor.AfterUserID == "" && cursor.AfterBotGrantID == "" || cursor.AfterUserID == notionUserDirectoryBotPhase {
		return cursor, nil
	}
	if _, err := notionCompatibleUserID(cursor.AfterUserID); err != nil || cursor.AfterBotGrantID != "" {
		return notionUserDirectoryCursor{}, errInvalidNotionUserDirectoryCursor
	}
	return cursor, nil
}

func notionUserDirectoryCursorSignature(secret, payload string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("leafen:notion-user-directory-cursor:v1\x00" + payload))
	return mac.Sum(nil)
}

func notionUserNotFound(event *core.RequestEvent) error {
	return notionCompatibilityError(
		event, http.StatusNotFound, notionErrorObjectNotFound,
		"There's no such user in the connected workspace.",
	)
}
