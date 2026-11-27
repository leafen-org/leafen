package app

import (
	"errors"
	"net/http"
	"strconv"
)

const (
	notionAdminSpaceGroupDefaultPageSize = 100
	notionAdminSpaceGroupMaximumPageSize = 100
	notionAdminSpaceGroupCursorVersion   = 1
	notionAdminSpaceGroupCursorPrefix    = "hnapg1"
	notionAdminSpaceGroupCursorDomain    = "leafen:notion-admin-space-group-cursor:v1"
	notionAdminSpaceGroupCursorMaxLength = 2048
	notionAdminSpaceGroupCursorGroups    = "groups"
	notionAdminSpaceGroupCursorMembers   = "group_memberships"
)

var errInvalidNotionAdminSpaceGroupCursor = notionAdminInvalid(errors.New("invalid Notion Admin space-group cursor"), "start_cursor isn't one this list gave.")

type notionAdminSpaceGroupCursor struct {
	Version        int    `json:"v"`
	OrganizationID string `json:"o"`
	TokenID        string `json:"t"`
	WorkspaceID    string `json:"w"`
	GroupID        string `json:"g,omitempty"`
	Kind           string `json:"k"`
	Revision       int64  `json:"r"`
	AfterID        string `json:"a"`
}

func notionAdminSpaceGroupListQuery(request *http.Request) (int, string, error) {
	query := request.URL.Query()
	for key, values := range query {
		if key != "page_size" && key != "start_cursor" || len(values) != 1 {
			return 0, "", errInvalidNotionAdminSpaceGroupCursor
		}
	}
	pageSize := notionAdminSpaceGroupDefaultPageSize
	if raw := query.Get("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > notionAdminSpaceGroupMaximumPageSize {
			return 0, "", errInvalidNotionAdminSpaceGroupCursor
		}
		pageSize = parsed
	}
	return pageSize, query.Get("start_cursor"), nil
}

func encodeNotionAdminSpaceGroupCursor(
	secret string,
	identity notionAdminIdentity,
	workspaceID, groupID, kind string,
	revision int64,
	afterID string,
) (string, error) {
	return encodeNotionAdminLegalHoldScopeCursor(
		secret, notionAdminSpaceGroupCursorPrefix, notionAdminSpaceGroupCursorDomain,
		notionAdminSpaceGroupCursorMaxLength, notionAdminSpaceGroupCursor{
			Version: notionAdminSpaceGroupCursorVersion, OrganizationID: identity.OrganizationID,
			TokenID: identity.TokenID, WorkspaceID: workspaceID, GroupID: groupID,
			Kind: kind, Revision: revision, AfterID: afterID,
		},
	)
}

func decodeNotionAdminSpaceGroupCursor(
	raw, secret string,
	identity notionAdminIdentity,
	workspaceID, groupID, kind string,
) (notionAdminSpaceGroupCursor, error) {
	if raw == "" {
		return notionAdminSpaceGroupCursor{}, nil
	}
	cursor := notionAdminSpaceGroupCursor{}
	if err := decodeNotionAdminLegalHoldScopeCursor(
		raw, secret, notionAdminSpaceGroupCursorPrefix, notionAdminSpaceGroupCursorDomain,
		notionAdminSpaceGroupCursorMaxLength, &cursor,
	); err != nil || cursor.Version != notionAdminSpaceGroupCursorVersion ||
		cursor.OrganizationID != identity.OrganizationID || cursor.TokenID != identity.TokenID ||
		cursor.WorkspaceID != workspaceID || cursor.GroupID != groupID || cursor.Kind != kind ||
		cursor.Revision < 1 || !validNotionAdminSpaceGroupCursorID(cursor.AfterID) {
		return notionAdminSpaceGroupCursor{}, errInvalidNotionAdminSpaceGroupCursor
	}
	return cursor, nil
}

func validNotionAdminSpaceGroupCursorID(value string) bool {
	return validNotionCompatibleInternalID(value)
}
