package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const formOptionCursorFormatVersion = 1

type formOptionSnapshot struct {
	PrincipalID        string `json:"principalId"`
	WorkspaceID        string `json:"workspaceId"`
	LinkID             string `json:"linkId"`
	PropertyID         string `json:"propertyId"`
	PropertyType       string `json:"propertyType"`
	DefinitionRevision string `json:"definitionRevision"`
	PermissionCursor   int64  `json:"permissionCursor"`
	PersonRevision     int64  `json:"personRevision"`
	ContentRevision    int64  `json:"contentRevision"`
	TargetPageID       string `json:"targetPageId"`
	TargetSourceID     string `json:"targetSourceId"`
	// What the respondent typed, the window walking only what has it.
	Query string `json:"query,omitempty"`
	// Which pages a relation's list reads: "all", "verified", "archived" or,
	// empty, those not archived.
	PageFilter string `json:"pageFilter,omitempty"`
}

type formOptionCursorPayload struct {
	FormatVersion int                `json:"formatVersion"`
	Snapshot      formOptionSnapshot `json:"snapshot"`
	LastPosition  string             `json:"lastPosition"`
	LastID        string             `json:"lastId"`
}

func sealFormOptionCursor(app core.App, payload formOptionCursorPayload) (string, error) {
	payload.FormatVersion = formOptionCursorFormatVersion
	if !validFormOptionCursorPayload(payload) {
		return "", errFormConflict
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	key, err := formOptionCursorKey(app)
	if err != nil {
		return "", err
	}
	cipher, err := security.Encrypt(encoded, key)
	if err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(cipher))
	if len(token) < 64 || len(token) > maxFormCursorCharacters {
		return "", errFormConflict
	}
	return token, nil
}

func openFormOptionCursor(app core.App, token string) (formOptionCursorPayload, error) {
	if len(token) < 64 || len(token) > maxFormCursorCharacters || strings.TrimSpace(token) != token {
		return formOptionCursorPayload{}, errFormConflict
	}
	cipher, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return formOptionCursorPayload{}, errFormConflict
	}
	key, err := formOptionCursorKey(app)
	if err != nil {
		return formOptionCursorPayload{}, err
	}
	plain, err := security.Decrypt(string(cipher), key)
	if err != nil {
		return formOptionCursorPayload{}, errFormConflict
	}
	payload := formOptionCursorPayload{}
	if json.Unmarshal(plain, &payload) != nil || !validFormOptionCursorPayload(payload) {
		return formOptionCursorPayload{}, errFormConflict
	}
	return payload, nil
}

func formOptionCursorKey(app core.App) (string, error) {
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte("leafen-form-option-cursor-v1\x00" + secret))
	return string(digest[:]), nil
}

func validFormOptionCursorPayload(payload formOptionCursorPayload) bool {
	snapshot := payload.Snapshot
	if payload.FormatVersion != formOptionCursorFormatVersion ||
		snapshot.PrincipalID == "" || snapshot.WorkspaceID == "" || snapshot.LinkID == "" ||
		snapshot.PropertyID == "" || snapshot.DefinitionRevision == "" || payload.LastID == "" ||
		snapshot.PermissionCursor < 0 || snapshot.PersonRevision < 0 || snapshot.ContentRevision < 0 {
		return false
	}
	switch snapshot.PropertyType {
	case graphValueTypePerson:
		return payload.LastPosition == "" && snapshot.TargetPageID == "" && snapshot.TargetSourceID == ""
	case graphValueTypeRelation:
		return payload.LastPosition != "" && snapshot.TargetPageID != "" && snapshot.TargetSourceID != ""
	default:
		return false
	}
}
