package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAdminSpaceUsersDefaultPageSize = 100
	notionAdminSpaceUsersMaximumPageSize = 100
	notionAdminSpaceUsersCursorVersion   = 1
	notionAdminSpaceUsersCursorPrefix    = "hnasu1"
	notionAdminSpaceUsersCursorMaxLength = 1024
	notionAdminSpaceUsersCursorDomain    = "leafen:notion-admin-space-users-cursor:v1"
)

var (
	errInvalidNotionAdminSpaceUsersCursor = notionAdminInvalid(errors.New("invalid Notion Admin space-users cursor"), "start_cursor isn't one this list gave.")
	errNotionAdminSpaceUsersNotFound      = errors.New("Notion Admin space-users workspace not found")
)

// notionAdminMemberCursorKind tells a members list's cursors apart: the
// people list's and the credit usage list's take turns over the same members,
// and neither takes the other's.
type notionAdminMemberCursorKind struct{ prefix, domain string }

var notionAdminSpaceUsersCursorKind = notionAdminMemberCursorKind{
	prefix: notionAdminSpaceUsersCursorPrefix, domain: notionAdminSpaceUsersCursorDomain,
}

type notionAdminSpaceUsersCursor struct {
	Version            int    `json:"v"`
	OrganizationID     string `json:"o"`
	TokenID            string `json:"t"`
	WorkspaceID        string `json:"w"`
	PermissionRevision int    `json:"r"`
	AfterUserID        string `json:"a"`
}

type notionAdminSpaceUsersRow struct {
	WorkspaceID        string         `db:"workspace_id"`
	PermissionRevision int            `db:"permission_revision"`
	ID                 sql.NullString `db:"id"`
	Name               sql.NullString `db:"name"`
	Avatar             sql.NullString `db:"avatar"`
	Email              sql.NullString `db:"email"`
	EmailVerified      sql.NullBool   `db:"email_verified"`
}

type notionAdminSpaceUsersPage struct {
	Users              []map[string]any
	InternalUserIDs    []string
	PermissionRevision int
	HasMore            bool
}

func serveNotionAdminSpaceUsersList(event *core.RequestEvent, rawWorkspaceID string) error {
	secret := bearerToken(event.Request)
	release := acquireNotionAdminTokenLifecycleLock(notionAdminTokenHash(secret))
	defer release()
	identity, authorized := authorizeNotionAdminPublicAPI(
		event, secret, notionAdminCapabilityUserRead,
	)
	if !authorized {
		return nil
	}
	if event.Request.ContentLength > 0 {
		return notionAdminSpaceUsersValidationError(event, notionAdminInvalid(nil, "This request takes no body."))
	}
	workspaceID, err := resolveNotionCompatibleWorkspaceID(strings.TrimSpace(rawWorkspaceID))
	if err != nil {
		return notionAdminSpaceUsersValidationError(event, notionAdminInvalid(err, "The workspace ID should be a UUID."))
	}
	if !identity.CanAccessWorkspace(workspaceID) {
		return notionAdminSpaceUsersNotFoundError(event, nil)
	}
	pageSize, rawCursor, err := notionAdminSpaceUsersQuery(event.Request)
	if err != nil {
		return notionAdminSpaceUsersValidationError(event, err)
	}
	cursor, err := decodeNotionAdminSpaceUsersCursor(notionAdminSpaceUsersCursorKind, rawCursor, secret, identity, workspaceID)
	if err != nil {
		return notionAdminSpaceUsersValidationError(event, err)
	}
	page, err := listNotionAdminSpaceUsers(
		context.WithValue(event.Request.Context(), requestOriginContextKey{}, requestOrigin(event.Request)),
		event.App, identity, workspaceID, cursor, pageSize,
	)
	if errors.Is(err, errInvalidNotionAdminSpaceUsersCursor) {
		return notionAdminSpaceUsersValidationError(event, err)
	}
	if errors.Is(err, errNotionAdminSpaceUsersNotFound) {
		return notionAdminSpaceUsersNotFoundError(event, err)
	}
	if err != nil {
		return notionAdminPublicAPIError(
			event, http.StatusInternalServerError, notionAdminErrorInternal,
			notionAdminInternalErrorMessage, err,
		)
	}
	var nextCursor *string
	if page.HasMore {
		encoded, err := encodeNotionAdminSpaceUsersCursor(
			notionAdminSpaceUsersCursorKind, secret, identity, workspaceID, page.PermissionRevision,
			page.InternalUserIDs[len(page.InternalUserIDs)-1],
		)
		if err != nil {
			return notionAdminPublicAPIError(
				event, http.StatusInternalServerError, notionAdminErrorInternal,
				notionAdminInternalErrorMessage, err,
			)
		}
		nextCursor = &encoded
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{
		"object": "list", "type": "user", "results": page.Users,
		"has_more": page.HasMore, "next_cursor": nextCursor,
	})
}

func notionAdminSpaceUsersQuery(request *http.Request) (int, string, error) {
	query := request.URL.Query()
	for key, values := range query {
		if key != "page_size" && key != "start_cursor" || len(values) != 1 {
			return 0, "", notionAdminInvalid(errInvalidNotionAdminSpaceUsersCursor, "The query takes page_size and start_cursor, once each.")
		}
	}
	pageSize := notionAdminSpaceUsersDefaultPageSize
	if raw := query.Get("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > notionAdminSpaceUsersMaximumPageSize {
			return 0, "", notionAdminInvalid(errInvalidNotionAdminSpaceUsersCursor, fmt.Sprintf("page_size is a whole number from 1 to %d.", notionAdminSpaceUsersMaximumPageSize))
		}
		pageSize = parsed
	}
	return pageSize, query.Get("start_cursor"), nil
}

func listNotionAdminSpaceUsers(
	ctx context.Context,
	app core.App,
	identity notionAdminIdentity,
	workspaceID string,
	cursor notionAdminSpaceUsersCursor,
	pageSize int,
) (notionAdminSpaceUsersPage, error) {
	rows := []notionAdminSpaceUsersRow{}
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		return txApp.DB().NewQuery(`
			WITH [[authority]] AS (
				SELECT [[workspace]].[[id]] AS [[workspace_id]],
					COALESCE([[permission]].[[cursor]], 0) AS [[permission_revision]]
				FROM {{leafen_workspaces}} [[workspace]]
				LEFT JOIN {{leafen_workspace_permission_state}} [[permission]]
					ON [[permission]].[[workspace_id]] = [[workspace]].[[id]]
				WHERE [[workspace]].[[id]] = {:workspaceId}
					AND [[workspace]].[[organization]] = {:organizationId}
			), [[page_ids]] AS (
				SELECT [[workspace]].[[workspace_owner]] AS [[id]]
				FROM [[authority]] JOIN {{leafen_workspaces}} [[workspace]]
					ON [[workspace]].[[id]] = [[authority]].[[workspace_id]]
				WHERE [[workspace]].[[workspace_owner]] > {:afterUserId}
				UNION
				SELECT [[membership]].[[user_id]] AS [[id]]
				FROM [[authority]] JOIN {{leafen_workspace_memberships}} [[membership]]
					ON [[membership]].[[workspace_id]] = [[authority]].[[workspace_id]]
				WHERE [[membership]].[[role]] = 'member'
					AND [[membership]].[[user_id]] > {:afterUserId}
				ORDER BY [[id]] LIMIT {:limit}
			)
			SELECT [[authority]].[[workspace_id]], [[authority]].[[permission_revision]],
				[[user]].[[id]], [[user]].[[name]],
				[[user]].[[profile_avatar]] AS [[avatar]], [[user]].[[email]],
				[[user]].[[verified]] AS [[email_verified]]
			FROM [[authority]]
			LEFT JOIN [[page_ids]] ON TRUE
			LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[page_ids]].[[id]]
			ORDER BY [[user]].[[id]]
		`).WithContext(ctx).Bind(dbx.Params{
			"workspaceId": workspaceID, "organizationId": identity.OrganizationID,
			"afterUserId": cursor.AfterUserID, "limit": pageSize + 1,
		}).All(&rows)
	})
	if err != nil {
		return notionAdminSpaceUsersPage{}, err
	}
	if len(rows) == 0 {
		return notionAdminSpaceUsersPage{}, errNotionAdminSpaceUsersNotFound
	}
	page := notionAdminSpaceUsersPage{PermissionRevision: rows[0].PermissionRevision}
	if cursor.Version != 0 && cursor.PermissionRevision != page.PermissionRevision {
		return notionAdminSpaceUsersPage{}, errInvalidNotionAdminSpaceUsersCursor
	}
	for _, row := range rows {
		if row.WorkspaceID != workspaceID || row.PermissionRevision != page.PermissionRevision {
			return notionAdminSpaceUsersPage{}, errors.New("inconsistent Notion Admin space-users snapshot")
		}
		if !row.ID.Valid {
			continue
		}
		if !row.Email.Valid || !row.EmailVerified.Valid {
			return notionAdminSpaceUsersPage{}, errors.New("invalid Notion Admin space-user identity")
		}
		page.InternalUserIDs = append(page.InternalUserIDs, row.ID.String)
		if len(page.Users) == pageSize {
			continue
		}
		user, err := notionUserObjectFromFields(
			row.ID.String, row.Name.String, row.Avatar.String, row.Email.String, row.EmailVerified.Bool,
			requestOriginFromContext(ctx),
		)
		if err != nil {
			return notionAdminSpaceUsersPage{}, err
		}
		page.Users = append(page.Users, user)
	}
	page.HasMore = len(page.InternalUserIDs) > pageSize
	if page.HasMore {
		page.InternalUserIDs = page.InternalUserIDs[:pageSize]
	}
	return page, nil
}

func encodeNotionAdminSpaceUsersCursor(
	kind notionAdminMemberCursorKind,
	secret string,
	identity notionAdminIdentity,
	workspaceID string,
	permissionRevision int,
	afterUserID string,
) (string, error) {
	cursor := notionAdminSpaceUsersCursor{
		Version:        notionAdminSpaceUsersCursorVersion,
		OrganizationID: identity.OrganizationID, TokenID: identity.TokenID,
		WorkspaceID: workspaceID, PermissionRevision: permissionRevision,
		AfterUserID: afterUserID,
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", errInvalidNotionAdminSpaceUsersCursor
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	result := kind.prefix + "." + encoded + "." +
		base64.RawURLEncoding.EncodeToString(notionAdminSpaceUsersCursorSignature(kind, secret, encoded))
	if len(result) > notionAdminSpaceUsersCursorMaxLength {
		return "", errInvalidNotionAdminSpaceUsersCursor
	}
	return result, nil
}

func decodeNotionAdminSpaceUsersCursor(
	kind notionAdminMemberCursorKind,
	raw, secret string,
	identity notionAdminIdentity,
	workspaceID string,
) (notionAdminSpaceUsersCursor, error) {
	if raw == "" {
		return notionAdminSpaceUsersCursor{}, nil
	}
	if len(raw) > notionAdminSpaceUsersCursorMaxLength {
		return notionAdminSpaceUsersCursor{}, errInvalidNotionAdminSpaceUsersCursor
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] != kind.prefix {
		return notionAdminSpaceUsersCursor{}, errInvalidNotionAdminSpaceUsersCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, notionAdminSpaceUsersCursorSignature(kind, secret, parts[1])) {
		return notionAdminSpaceUsersCursor{}, errInvalidNotionAdminSpaceUsersCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return notionAdminSpaceUsersCursor{}, errInvalidNotionAdminSpaceUsersCursor
	}
	cursor := notionAdminSpaceUsersCursor{}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cursor) != nil || cursor.Version != notionAdminSpaceUsersCursorVersion ||
		cursor.OrganizationID != identity.OrganizationID || cursor.TokenID != identity.TokenID ||
		cursor.WorkspaceID != workspaceID || cursor.PermissionRevision < 0 || cursor.AfterUserID == "" {
		return notionAdminSpaceUsersCursor{}, errInvalidNotionAdminSpaceUsersCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return notionAdminSpaceUsersCursor{}, errInvalidNotionAdminSpaceUsersCursor
	}
	return cursor, nil
}

func notionAdminSpaceUsersCursorSignature(kind notionAdminMemberCursorKind, secret, payload string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(kind.domain + "\x00" + payload))
	return mac.Sum(nil)
}

func notionAdminSpaceUsersValidationError(event *core.RequestEvent, cause error) error {
	return notionAdminPublicAPIError(
		event, http.StatusBadRequest, notionAdminErrorValidation,
		notionAdminValidationReason(cause), cause,
	)
}

func notionAdminSpaceUsersNotFoundError(event *core.RequestEvent, cause error) error {
	return notionAdminPublicAPIError(
		event, http.StatusNotFound, notionErrorObjectNotFound,
		notionAdminNotFoundMessage, cause,
	)
}
