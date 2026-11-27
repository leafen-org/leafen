package organization

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	leafen "leafen.org/leafen/internal/app"
)

const (
	notionAdminLegalHoldCursorVersion   = 1
	notionAdminLegalHoldCursorPrefix    = "hnal1"
	notionAdminLegalHoldCursorMaxLength = 1024
)

var errInvalidNotionAdminLegalHoldCursor = leafen.NotionAdminInvalid(errors.New("invalid Notion Admin legal-hold cursor"), "start_cursor isn't one this list gave.")

type notionAdminLegalHoldCursor struct {
	Version        int    `json:"v"`
	OrganizationID string `json:"o"`
	TokenID        string `json:"t"`
	UserID         string `json:"u,omitempty"`
	AfterCreatedAt int64  `json:"c"`
	AfterHoldID    string `json:"h"`
}

func encodeNotionAdminLegalHoldCursor(
	secret string,
	identity leafen.NotionAdminIdentity,
	userID string,
	last storedLegalHold,
) (string, error) {
	cursor := notionAdminLegalHoldCursor{
		Version:        notionAdminLegalHoldCursorVersion,
		OrganizationID: identity.OrganizationID,
		TokenID:        identity.TokenID,
		UserID:         userID,
		AfterCreatedAt: last.CreatedAt,
		AfterHoldID:    last.ID,
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", errInvalidNotionAdminLegalHoldCursor
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := notionAdminLegalHoldCursorSignature(secret, encoded)
	result := notionAdminLegalHoldCursorPrefix + "." + encoded + "." +
		base64.RawURLEncoding.EncodeToString(signature)
	if len(result) > notionAdminLegalHoldCursorMaxLength {
		return "", errInvalidNotionAdminLegalHoldCursor
	}
	return result, nil
}

func decodeNotionAdminLegalHoldCursor(
	raw, secret string,
	identity leafen.NotionAdminIdentity,
	userID string,
) (notionAdminLegalHoldCursor, error) {
	if raw == "" {
		return notionAdminLegalHoldCursor{}, nil
	}
	if len(raw) > notionAdminLegalHoldCursorMaxLength {
		return notionAdminLegalHoldCursor{}, errInvalidNotionAdminLegalHoldCursor
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] != notionAdminLegalHoldCursorPrefix {
		return notionAdminLegalHoldCursor{}, errInvalidNotionAdminLegalHoldCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, notionAdminLegalHoldCursorSignature(secret, parts[1])) {
		return notionAdminLegalHoldCursor{}, errInvalidNotionAdminLegalHoldCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return notionAdminLegalHoldCursor{}, errInvalidNotionAdminLegalHoldCursor
	}
	cursor := notionAdminLegalHoldCursor{}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cursor) != nil || cursor.Version != notionAdminLegalHoldCursorVersion ||
		cursor.OrganizationID != identity.OrganizationID || cursor.TokenID != identity.TokenID ||
		cursor.UserID != userID || cursor.AfterCreatedAt < 0 || len(cursor.AfterHoldID) != 32 {
		return notionAdminLegalHoldCursor{}, errInvalidNotionAdminLegalHoldCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return notionAdminLegalHoldCursor{}, errInvalidNotionAdminLegalHoldCursor
	}
	return cursor, nil
}

func notionAdminLegalHoldCursorSignature(secret, payload string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("leafen:notion-admin-legal-hold-cursor:v1\x00" + payload))
	return mac.Sum(nil)
}
