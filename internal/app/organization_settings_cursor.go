package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

type organizationSettingsCursor struct {
	Version          int    `json:"v"`
	PrincipalID      string `json:"principalId"`
	OrganizationID   string `json:"organizationId"`
	Revision         int    `json:"revision"`
	MemberAfter      string `json:"memberAfter,omitempty"`
	MembersDone      bool   `json:"membersDone"`
	GroupAuthority   string `json:"groupAuthority,omitempty"`
	GroupAfter       string `json:"groupAfter,omitempty"`
	GroupMemberAfter string `json:"groupMemberAfter,omitempty"`
	GroupsDone       bool   `json:"groupsDone"`
	DomainAfter      string `json:"domainAfter,omitempty"`
	DomainsDone      bool   `json:"domainsDone"`
}

func decodeOrganizationSettingsCursor(
	raw, principalID, organizationID string,
) (organizationSettingsCursor, error) {
	initial := organizationSettingsCursor{
		Version: organizationSettingsCursorVersion, PrincipalID: principalID,
		OrganizationID: organizationID,
	}
	if strings.TrimSpace(raw) == "" {
		return initial, nil
	}
	if len(raw) > organizationSettingsCursorBytes {
		return organizationSettingsCursor{}, errors.New(errorCodeOrganizationSettingsCursorInvalid)
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > organizationSettingsCursorBytes {
		return organizationSettingsCursor{}, errors.New(errorCodeOrganizationSettingsCursorInvalid)
	}
	cursor := organizationSettingsCursor{}
	if json.Unmarshal(data, &cursor) != nil || cursor.Version != organizationSettingsCursorVersion ||
		cursor.PrincipalID != principalID || cursor.OrganizationID != organizationID || cursor.Revision < 0 ||
		len(cursor.MemberAfter) > 255 || len(cursor.GroupAuthority) > 16 || len(cursor.GroupAfter) > 255 ||
		len(cursor.GroupMemberAfter) > 255 ||
		len(cursor.DomainAfter) > 253 {
		return organizationSettingsCursor{}, errors.New(errorCodeOrganizationSettingsCursorInvalid)
	}
	return cursor, nil
}

func encodeOrganizationSettingsCursor(cursor organizationSettingsCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	result := base64.RawURLEncoding.EncodeToString(encoded)
	if len(result) > organizationSettingsCursorBytes {
		return "", errors.New(errorCodeOrganizationSettingsCursorInvalid)
	}
	return result, nil
}
