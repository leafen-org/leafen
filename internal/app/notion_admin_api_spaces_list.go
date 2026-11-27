package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// GET /admin/v1/spaces lists the organization's live workspaces, as Notion's
// Admin API "List workspaces in the organization" (its published OpenAPI,
// read 2026-11-07: scope workspace:read, page_size 1–100 default 100,
// start_cursor, results {object "space", id, name} ordered by ID, the public
// error shape). A token narrowed to some workspaces lists those alone, as its
// other requests reach those alone.

const (
	notionAdminSpacesCursorVersion   = 1
	notionAdminSpacesCursorPrefix    = "hnasp1"
	notionAdminSpacesCursorMaxLength = 1024
	notionAdminSpacesCursorDomain    = "leafen:notion-admin-spaces-cursor:v1"
)

var errInvalidNotionAdminSpacesCursor = notionAdminInvalid(
	errors.New("invalid Notion Admin spaces cursor"), "start_cursor isn't one this list gave.",
)

type notionAdminSpacesCursor struct {
	Version        int    `json:"v"`
	OrganizationID string `json:"o"`
	TokenID        string `json:"t"`
	AfterID        string `json:"a"`
}

type notionAdminSpace struct {
	ID   string
	Name string
}

func serveNotionAdminSpacesList(event *core.RequestEvent) error {
	secret := bearerToken(event.Request)
	release := acquireNotionAdminTokenLifecycleLock(notionAdminTokenHash(secret))
	defer release()
	identity, authorized := authorizeNotionAdminPublicAPI(event, secret, notionAdminCapabilityWorkspaceRead)
	if !authorized {
		return nil
	}
	if event.Request.ContentLength > 0 {
		return notionAdminSpaceUsersValidationError(event, notionAdminInvalid(nil, "This request takes no body."))
	}
	pageSize, rawCursor, err := notionAdminSpaceUsersQuery(event.Request)
	if err != nil {
		return notionAdminSpaceUsersValidationError(event, err)
	}
	cursor, err := decodeNotionAdminSpacesCursor(rawCursor, secret, identity)
	if err != nil {
		return notionAdminSpaceUsersValidationError(event, err)
	}
	spaces, err := listNotionAdminSpaces(event, identity)
	if err != nil {
		return notionAdminPublicAPIError(
			event, http.StatusInternalServerError, notionAdminErrorInternal, notionAdminInternalErrorMessage, err,
		)
	}
	start, _ := slices.BinarySearchFunc(spaces, cursor.AfterID, func(space notionAdminSpace, after string) int {
		return strings.Compare(space.ID, after)
	})
	if cursor.AfterID != "" && start < len(spaces) && spaces[start].ID == cursor.AfterID {
		start++
	}
	page := spaces[start:]
	hasMore := len(page) > pageSize
	if hasMore {
		page = page[:pageSize]
	}
	results := make([]map[string]any, 0, len(page))
	for _, space := range page {
		var name any
		if space.Name != "" {
			name = space.Name
		}
		results = append(results, map[string]any{"object": "space", "id": space.ID, "name": name})
	}
	var nextCursor *string
	if hasMore {
		encoded, err := encodeNotionAdminSpacesCursor(secret, identity, page[len(page)-1].ID)
		if err != nil {
			return notionAdminPublicAPIError(
				event, http.StatusInternalServerError, notionAdminErrorInternal, notionAdminInternalErrorMessage, err,
			)
		}
		nextCursor = &encoded
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{
		"object": "list", "type": "space", "space": map[string]any{}, "results": results,
		"has_more": hasMore, "next_cursor": nextCursor,
	})
}

// listNotionAdminSpaces is every live workspace of the token's organization it
// may reach, by its Notion-compatible ID.
func listNotionAdminSpaces(event *core.RequestEvent, identity notionAdminIdentity) ([]notionAdminSpace, error) {
	rows := []struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}{}
	if err := event.App.DB().NewQuery(`
		SELECT [[id]], [[name]] FROM {{leafen_workspaces}}
		WHERE [[organization]] = {:organizationId} AND COALESCE([[deletion_job_id]], '') = ''
	`).WithContext(event.Request.Context()).Bind(dbx.Params{"organizationId": identity.OrganizationID}).All(&rows); err != nil {
		return nil, err
	}
	spaces := make([]notionAdminSpace, 0, len(rows))
	for _, row := range rows {
		if !identity.CanAccessWorkspace(row.ID) {
			continue
		}
		id, err := notionCompatibleWorkspaceID(row.ID)
		if err != nil {
			return nil, err
		}
		spaces = append(spaces, notionAdminSpace{ID: id, Name: strings.TrimSpace(row.Name)})
	}
	slices.SortFunc(spaces, func(left, right notionAdminSpace) int { return strings.Compare(left.ID, right.ID) })
	return spaces, nil
}

func encodeNotionAdminSpacesCursor(secret string, identity notionAdminIdentity, afterID string) (string, error) {
	payload, err := json.Marshal(notionAdminSpacesCursor{
		Version: notionAdminSpacesCursorVersion, OrganizationID: identity.OrganizationID,
		TokenID: identity.TokenID, AfterID: afterID,
	})
	if err != nil {
		return "", errInvalidNotionAdminSpacesCursor
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	result := notionAdminSpacesCursorPrefix + "." + encoded + "." +
		base64.RawURLEncoding.EncodeToString(notionAdminSpacesCursorSignature(secret, encoded))
	if len(result) > notionAdminSpacesCursorMaxLength {
		return "", errInvalidNotionAdminSpacesCursor
	}
	return result, nil
}

func decodeNotionAdminSpacesCursor(raw, secret string, identity notionAdminIdentity) (notionAdminSpacesCursor, error) {
	if raw == "" {
		return notionAdminSpacesCursor{}, nil
	}
	parts := strings.Split(raw, ".")
	if len(raw) > notionAdminSpacesCursorMaxLength || len(parts) != 3 || parts[0] != notionAdminSpacesCursorPrefix {
		return notionAdminSpacesCursor{}, errInvalidNotionAdminSpacesCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, notionAdminSpacesCursorSignature(secret, parts[1])) {
		return notionAdminSpacesCursor{}, errInvalidNotionAdminSpacesCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return notionAdminSpacesCursor{}, errInvalidNotionAdminSpacesCursor
	}
	cursor := notionAdminSpacesCursor{}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cursor) != nil || cursor.Version != notionAdminSpacesCursorVersion ||
		cursor.OrganizationID != identity.OrganizationID || cursor.TokenID != identity.TokenID || cursor.AfterID == "" {
		return notionAdminSpacesCursor{}, errInvalidNotionAdminSpacesCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return notionAdminSpacesCursor{}, errInvalidNotionAdminSpacesCursor
	}
	return cursor, nil
}

func notionAdminSpacesCursorSignature(secret, payload string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(notionAdminSpacesCursorDomain + "\x00" + payload))
	return mac.Sum(nil)
}
