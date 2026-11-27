package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

var errInvalidNotionAdminLegalHoldScopeCursor = notionAdminInvalid(errors.New("invalid Notion Admin legal-hold scope cursor"), "start_cursor isn't one this list gave.")

func notionAdminLegalHoldScopeQuery(event *core.RequestEvent) (string, bool) {
	query := event.Request.URL.Query()
	for key, values := range query {
		if key != "start_cursor" || len(values) != 1 || values[0] == "" {
			return "", false
		}
	}
	return query.Get("start_cursor"), true
}

func encodeNotionAdminLegalHoldScopeCursor(
	secret, prefix, signatureDomain string,
	maxLength int,
	cursor any,
) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil || prefix == "" || signatureDomain == "" || maxLength < 1 {
		return "", errInvalidNotionAdminLegalHoldScopeCursor
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := notionAdminLegalHoldScopeCursorSignature(
		secret, signatureDomain, encoded,
	)
	result := prefix + "." + encoded + "." +
		base64.RawURLEncoding.EncodeToString(signature)
	if len(result) > maxLength {
		return "", errInvalidNotionAdminLegalHoldScopeCursor
	}
	return result, nil
}

func decodeNotionAdminLegalHoldScopeCursor(
	raw, secret, prefix, signatureDomain string,
	maxLength int,
	target any,
) error {
	if raw == "" || prefix == "" || signatureDomain == "" || maxLength < 1 ||
		len(raw) > maxLength {
		return errInvalidNotionAdminLegalHoldScopeCursor
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] != prefix {
		return errInvalidNotionAdminLegalHoldScopeCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(
		signature,
		notionAdminLegalHoldScopeCursorSignature(secret, signatureDomain, parts[1]),
	) {
		return errInvalidNotionAdminLegalHoldScopeCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errInvalidNotionAdminLegalHoldScopeCursor
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return errInvalidNotionAdminLegalHoldScopeCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errInvalidNotionAdminLegalHoldScopeCursor
	}
	return nil
}

func notionAdminLegalHoldScopeCursorSignature(
	secret, signatureDomain, payload string,
) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signatureDomain + "\x00" + payload))
	return mac.Sum(nil)
}
