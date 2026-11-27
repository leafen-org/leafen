package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

var errWorkspaceRefreshTokenInvalid = errors.New(errorCodeWorkspaceChangeProbeInvalid)

type workspaceRefreshTokenPayload struct {
	FormatVersion int                     `json:"formatVersion"`
	Snapshot      workspaceChangeSnapshot `json:"snapshot"`
}

func sealWorkspaceRefreshToken(user *core.Record, snapshot workspaceChangeSnapshot) (string, error) {
	data, err := json.Marshal(workspaceRefreshTokenPayload{
		FormatVersion: workspaceRefreshTokenFormatVersion,
		Snapshot:      snapshot,
	})
	if err != nil {
		return "", err
	}
	key, err := workspaceRefreshTokenKey(user)
	if err != nil {
		return "", err
	}
	cipher, err := security.Encrypt(data, key)
	if err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(cipher))
	if len(token) < 64 || len(token) > maxWorkspaceRefreshTokenBytes {
		return "", errWorkspaceRefreshTokenInvalid
	}
	return token, nil
}

func openWorkspaceRefreshToken(user *core.Record, token string) (workspaceRefreshTokenPayload, error) {
	if len(token) < 64 || len(token) > maxWorkspaceRefreshTokenBytes {
		return workspaceRefreshTokenPayload{}, errWorkspaceRefreshTokenInvalid
	}
	cipher, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return workspaceRefreshTokenPayload{}, errWorkspaceRefreshTokenInvalid
	}
	decodedCipher, err := base64.StdEncoding.DecodeString(string(cipher))
	if err != nil || len(decodedCipher) < 29 {
		return workspaceRefreshTokenPayload{}, errWorkspaceRefreshTokenInvalid
	}
	key, err := workspaceRefreshTokenKey(user)
	if err != nil {
		return workspaceRefreshTokenPayload{}, err
	}
	data, err := security.Decrypt(string(cipher), key)
	if err != nil {
		return workspaceRefreshTokenPayload{}, errWorkspaceRefreshTokenInvalid
	}
	payload := workspaceRefreshTokenPayload{}
	if json.Unmarshal(data, &payload) != nil || payload.FormatVersion != workspaceRefreshTokenFormatVersion ||
		!validWorkspaceChangeSnapshot(payload.Snapshot) {
		return workspaceRefreshTokenPayload{}, errWorkspaceRefreshTokenInvalid
	}
	return payload, nil
}

func workspaceRefreshTokenKey(user *core.Record) (string, error) {
	if user == nil || !user.Collection().IsAuth() {
		return "", errWorkspaceRefreshTokenInvalid
	}
	material := user.TokenKey() + user.Collection().AuthToken.Secret
	if material == "" {
		return "", core.ErrMissingSigningKey
	}
	digest := sha256.Sum256([]byte("leafen-workspace-change-catchup-v1\x00" + material))
	return string(digest[:]), nil
}
