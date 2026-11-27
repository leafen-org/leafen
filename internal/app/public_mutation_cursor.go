package app

import (
	"encoding/base64"
	"encoding/json"
)

const (
	publicMutationPermissionCursorVersion = 1
	publicMutationPrincipalCursorVersion  = 1
)

type publicMutationPermissionCursor struct {
	Version     int    `json:"v"`
	PrincipalID string `json:"u"`
	WorkspaceID string `json:"w"`
	PageID      string `json:"p"`
	Revision    int    `json:"r"`
	CreatedAt   int64  `json:"c"`
	ID          string `json:"i"`
}

type publicMutationPrincipalCursor struct {
	Version            int    `json:"v"`
	PrincipalID        string `json:"u"`
	WorkspaceID        string `json:"w"`
	PageID             string `json:"p"`
	MutationRevision   int    `json:"m"`
	PermissionRevision int    `json:"a"`
	PersonRevision     int64  `json:"n"`
	GroupRevision      int    `json:"g"`
	ID                 string `json:"i"`
}

func encodePublicMutationPermissionCursor(cursor publicMutationPermissionCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	if len(encoded) > publicMutationPermissionCursorBytes {
		return "", errPublicMutationCursor
	}
	return encoded, nil
}

func decodePublicMutationPermissionCursor(
	raw, principalID, workspaceID, pageID string, revision int,
) (publicMutationPermissionCursor, error) {
	if raw == "" {
		return publicMutationPermissionCursor{}, nil
	}
	if len(raw) > publicMutationPermissionCursorBytes {
		return publicMutationPermissionCursor{}, errPublicMutationCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return publicMutationPermissionCursor{}, errPublicMutationCursor
	}
	cursor := publicMutationPermissionCursor{}
	if json.Unmarshal(payload, &cursor) != nil ||
		cursor.Version != publicMutationPermissionCursorVersion || cursor.PrincipalID != principalID ||
		cursor.WorkspaceID != workspaceID || cursor.PageID != pageID || cursor.ID == "" || cursor.CreatedAt < 0 {
		return publicMutationPermissionCursor{}, errPublicMutationCursor
	}
	if cursor.Revision != revision {
		return publicMutationPermissionCursor{}, errPublicMutationRevisionConflict
	}
	canonical, err := encodePublicMutationPermissionCursor(cursor)
	if err != nil || canonical != raw {
		return publicMutationPermissionCursor{}, errPublicMutationCursor
	}
	return cursor, nil
}

func encodePublicMutationPrincipalCursor(cursor publicMutationPrincipalCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	if len(encoded) > publicMutationPrincipalCursorBytes {
		return "", errPublicMutationCursor
	}
	return encoded, nil
}

func decodePublicMutationPrincipalCursor(
	raw, principalID, workspaceID, pageID string,
	mutationRevision int,
	authority workspacePrincipalDirectoryAuthority,
) (publicMutationPrincipalCursor, error) {
	if raw == "" {
		return publicMutationPrincipalCursor{}, nil
	}
	if len(raw) > publicMutationPrincipalCursorBytes {
		return publicMutationPrincipalCursor{}, errPublicMutationCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return publicMutationPrincipalCursor{}, errPublicMutationCursor
	}
	cursor := publicMutationPrincipalCursor{}
	if json.Unmarshal(payload, &cursor) != nil ||
		cursor.Version != publicMutationPrincipalCursorVersion || cursor.PrincipalID != principalID ||
		cursor.WorkspaceID != workspaceID || cursor.PageID != pageID || cursor.ID == "" {
		return publicMutationPrincipalCursor{}, errPublicMutationCursor
	}
	if cursor.MutationRevision != mutationRevision ||
		cursor.PermissionRevision != authority.PermissionRevision ||
		cursor.PersonRevision != authority.PersonRevision ||
		cursor.GroupRevision != authority.GroupRevision {
		return publicMutationPrincipalCursor{}, errPublicMutationRevisionConflict
	}
	canonical, err := encodePublicMutationPrincipalCursor(cursor)
	if err != nil || canonical != raw {
		return publicMutationPrincipalCursor{}, errPublicMutationCursor
	}
	return cursor, nil
}

// errPublicMutationCursor is a sharing list's cursor that isn't the one its
// last page gave.
var errPublicMutationCursor = publicMutationRefused(
	"permissionCursor or principalCursor is the next cursor the last list gave, for the same page and revision, or none to start")
