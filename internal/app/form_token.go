package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const formTokenPrefix = "lfm_"

func formToken(app core.App, linkID string) (string, error) {
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("leafen-form-link-v1\x00" + linkID))
	return formTokenPrefix + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func validFormToken(token string) bool {
	if len(token) != len(formTokenPrefix)+43 || !strings.HasPrefix(token, formTokenPrefix) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, formTokenPrefix))
	return err == nil && len(decoded) == sha256.Size
}

func formTokenDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
