package app

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// A backlinks cursor names the reader, the workspace, the page and the
// revision a window was read at, and the source it stops at; a cursor for
// anything else is refused.

type pageBacklinksCursor struct {
	PrincipalID    string `json:"principalId"`
	WorkspaceID    string `json:"workspaceId"`
	TargetPageID   string `json:"targetPageId"`
	Revision       int    `json:"revision"`
	SourcePageID   string `json:"sourcePageId"`
	SourcePosition string `json:"sourcePosition"`
	BlockID        string `json:"blockId"`
	Limit          int    `json:"limit"`
	Direction      string `json:"direction"`
}

func encodePageBacklinksCursor(cursor pageBacklinksCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func encodePageBacklinksRowCursor(
	principalID string,
	workspaceID string,
	targetPageID string,
	revision int,
	row storedPageBacklink,
	limit int,
	direction string,
) (string, error) {
	return encodePageBacklinksCursor(pageBacklinksCursor{
		PrincipalID: principalID, WorkspaceID: workspaceID, TargetPageID: targetPageID,
		Revision: revision, SourcePageID: row.SourcePageID, SourcePosition: row.SourcePosition,
		BlockID: row.BlockID, Limit: limit, Direction: direction,
	})
}

func decodePageBacklinksCursor(
	raw string,
	principalID string,
	workspaceID string,
	targetPageID string,
	limit int,
) (pageBacklinksCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return pageBacklinksCursor{Direction: pageBacklinksCursorAfter}, nil
	}
	if len(raw) > pageBacklinksCursorBytes {
		return pageBacklinksCursor{}, errPageBacklinksCursorInvalid
	}
	encoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(encoded) == 0 || len(encoded) > pageBacklinksCursorBytes {
		return pageBacklinksCursor{}, errPageBacklinksCursorInvalid
	}
	cursor := pageBacklinksCursor{}
	if json.Unmarshal(encoded, &cursor) != nil || cursor.PrincipalID != principalID ||
		cursor.WorkspaceID != workspaceID || cursor.TargetPageID != targetPageID ||
		cursor.Revision <= 0 || cursor.SourcePageID == "" || cursor.SourcePosition == "" ||
		cursor.BlockID == "" || cursor.Limit != limit ||
		(cursor.Direction != pageBacklinksCursorAfter && cursor.Direction != pageBacklinksCursorBefore) {
		return pageBacklinksCursor{}, errPageBacklinksCursorInvalid
	}
	return cursor, nil
}
