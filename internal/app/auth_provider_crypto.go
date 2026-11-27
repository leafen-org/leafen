package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"

	"github.com/pocketbase/pocketbase/core"
)

func newAuthProviderToken(prefix string, bytes int) (string, error) {
	random := make([]byte, bytes)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(random), nil
}

func hashAuthProviderToken(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func encryptAuthProviderVerifier(app core.App, verifier string) (string, error) {
	return encryptAdministratorSetting(app, cipherProviderState, verifier)
}

func decryptAuthProviderVerifier(app core.App, cipher string) (string, error) {
	return decryptAdministratorSetting(app, cipher)
}
