package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

type organizationListCursor struct {
	Version        int    `json:"v"`
	PrincipalID    string `json:"principalId"`
	Created        string `json:"created"`
	OrganizationID string `json:"organizationId"`
}

func decodeOrganizationListCursor(raw string, principalID string) (organizationListCursor, error) {
	empty := organizationListCursor{Version: organizationListCursorVersion, PrincipalID: principalID}
	if strings.TrimSpace(raw) == "" {
		return empty, nil
	}
	if len(raw) > organizationListMaxCursorBytes {
		return organizationListCursor{}, errors.New("organization list cursor is too large")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > organizationListMaxCursorBytes {
		return organizationListCursor{}, errors.New("organization list cursor is malformed")
	}
	cursor := organizationListCursor{}
	if err := json.Unmarshal(data, &cursor); err != nil ||
		cursor.Version != organizationListCursorVersion ||
		cursor.PrincipalID != principalID ||
		cursor.Created == "" || len(cursor.Created) > 64 ||
		cursor.OrganizationID == "" || len(cursor.OrganizationID) > organizationIdentifierMaxRunes {
		return organizationListCursor{}, errors.New("organization list cursor is invalid")
	}
	return cursor, nil
}

func encodeOrganizationListCursor(row storedOrganizationListRow, principalID string) (string, error) {
	data, err := json.Marshal(organizationListCursor{
		Version:        organizationListCursorVersion,
		PrincipalID:    principalID,
		Created:        row.Created,
		OrganizationID: row.OrganizationID,
	})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	if len(encoded) > organizationListMaxCursorBytes {
		return "", errors.New("organization list cursor is too large")
	}
	return encoded, nil
}
