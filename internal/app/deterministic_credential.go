package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

func deriveDeterministicCredential(secret, domain, purpose, id, prefix string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(domain + "\x00" + purpose + "\x00" + id))
	return prefix + id + "_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
