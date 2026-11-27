package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	mcpOAuthSecretDirectory     = ".secrets"
	mcpOAuthSecretFilename      = "mcp-oauth.key"
	mcpOAuthSecretRecoveryFile  = "mcp-oauth.key.v0-recovery"
	mcpOAuthSecretFormatVersion = 1
)

var mcpOAuthSecretMutex sync.Mutex

func mcpOAuthSecret(app core.App) (string, error) {
	directory := filepath.Join(app.DataDir(), mcpOAuthSecretDirectory)
	return loadOrCreateVersionedSecretFile(versionedSecretFileConfig{
		Directory:        directory,
		Filename:         mcpOAuthSecretFilename,
		RecoveryFilename: mcpOAuthSecretRecoveryFile,
		FormatVersion:    mcpOAuthSecretFormatVersion,
		Mutex:            &mcpOAuthSecretMutex,
		Failure: func() error {
			return errors.New(errorCodeMCPOAuthFailed)
		},
	})
}

func sealMCPOAuthRequest(app core.App, request mcpOAuthAuthorizationRequest) (string, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	key, err := mcpOAuthSecret(app)
	if err != nil {
		return "", err
	}
	cipher, err := security.Encrypt(data, key)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString([]byte(cipher)), nil
}

func openMCPOAuthRequest(app core.App, sealed string) (mcpOAuthAuthorizationRequest, error) {
	if len(sealed) < 64 || len(sealed) > maxMCPOAuthRequestBytes {
		return mcpOAuthAuthorizationRequest{}, errors.New(errorCodeMCPOAuthRequestInvalid)
	}
	cipher, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		return mcpOAuthAuthorizationRequest{}, err
	}
	decodedCipher, err := base64.StdEncoding.DecodeString(string(cipher))
	if err != nil || len(decodedCipher) < 29 {
		return mcpOAuthAuthorizationRequest{}, errors.New(errorCodeMCPOAuthRequestInvalid)
	}
	key, err := mcpOAuthSecret(app)
	if err != nil {
		return mcpOAuthAuthorizationRequest{}, err
	}
	data, err := security.Decrypt(string(cipher), key)
	if err != nil {
		return mcpOAuthAuthorizationRequest{}, err
	}
	var request mcpOAuthAuthorizationRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return mcpOAuthAuthorizationRequest{}, err
	}
	return request, nil
}

func deterministicMCPOAuthCode(app core.App, authorizationID string) (string, error) {
	key, err := mcpOAuthSecret(app)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte("authorization-code\x00" + authorizationID))
	return "lac_" + authorizationID + "_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func deterministicMCPToken(secret string, purpose string, connectionID string, prefix string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("credential-completion:v1\x00" + purpose + "\x00" + connectionID))
	return prefix + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func newHashedMCPToken(prefix string) (string, string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", "", err
	}
	token := prefix + base64.RawURLEncoding.EncodeToString(random)
	return token, mcpTokenHash(token), nil
}
