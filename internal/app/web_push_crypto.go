package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Web Push as browsers expect it: the instance's VAPID key signs who sends
// (RFC 8292) and each payload is encrypted for one browser (RFC 8291), so the
// browser vendor's push service carries it without reading it. The private
// key lives beside the data directory's other secrets and never leaves it.

const (
	webPushSecretDirectory = "web_push"
	webPushKeyFilename     = "vapid-private.key"
	webPushRecordSize      = 4096
	webPushMaxPayload      = 3000
	webPushTokenLifetime   = 12 * time.Hour
)

var errWebPushKeyInvalid = errors.New("web push key is invalid")

// webPushKey reads the instance's VAPID key, creating it on first use.
func webPushKey(app core.App) (*ecdsa.PrivateKey, error) {
	directory := filepath.Join(app.DataDir(), webPushSecretDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, webPushKeyFilename)
	read := func() (*ecdsa.PrivateKey, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		scalar, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, errWebPushKeyInvalid
		}
		return ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar)
	}
	if key, err := read(); err == nil || !errors.Is(err, os.ErrNotExist) {
		return key, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	scalar, err := key.Bytes()
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return read()
	}
	if err != nil {
		return nil, err
	}
	_, writeErr := file.WriteString(base64.RawURLEncoding.EncodeToString(scalar))
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return nil, writeErr
	}
	return key, nil
}

// webPushPublicKey is the key browsers subscribe with, base64url encoded.
func webPushPublicKey(key *ecdsa.PrivateKey) (string, error) {
	public, err := key.PublicKey.Bytes()
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(public), nil
}

// webPushAuthorization signs a VAPID token for one push service origin.
func webPushAuthorization(key *ecdsa.PrivateKey, audience, subject string, now time.Time) (string, error) {
	encode := func(value any) (string, error) {
		encoded, err := json.Marshal(value)
		return base64.RawURLEncoding.EncodeToString(encoded), err
	}
	header, err := encode(map[string]string{"typ": "JWT", "alg": "ES256"})
	if err != nil {
		return "", err
	}
	claims, err := encode(map[string]any{
		"aud": audience, "exp": now.Add(webPushTokenLifetime).Unix(), "sub": subject,
	})
	if err != nil {
		return "", err
	}
	signed := header + "." + claims
	digest := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	public, err := webPushPublicKey(key)
	if err != nil {
		return "", err
	}
	return "vapid t=" + signed + "." + base64.RawURLEncoding.EncodeToString(signature) + ", k=" + public, nil
}

// encryptWebPushPayload encrypts a payload for one browser subscription as a
// single aes128gcm record.
func encryptWebPushPayload(p256dh, auth string, payload []byte) ([]byte, error) {
	if len(payload) > webPushMaxPayload {
		return nil, errWebPushKeyInvalid
	}
	receiverBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(p256dh, "="))
	if err != nil {
		return nil, errWebPushKeyInvalid
	}
	secret, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(auth, "="))
	if err != nil || len(secret) != 16 {
		return nil, errWebPushKeyInvalid
	}
	receiver, err := ecdh.P256().NewPublicKey(receiverBytes)
	if err != nil {
		return nil, errWebPushKeyInvalid
	}
	sender, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := sender.ECDH(receiver)
	if err != nil {
		return nil, err
	}
	senderPublic := sender.PublicKey().Bytes()
	keyInfo := "WebPush: info\x00" + string(receiver.Bytes()) + string(senderPublic)
	ikm, err := hkdf.Key(sha256.New, shared, secret, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	contentKey, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(contentKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// The last (and only) record ends its plaintext with the 0x02 delimiter.
	sealed := gcm.Seal(nil, nonce, append(append([]byte{}, payload...), 0x02), nil)
	header := make([]byte, 0, 16+4+1+len(senderPublic)+len(sealed))
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, webPushRecordSize)
	header = append(header, byte(len(senderPublic)))
	header = append(header, senderPublic...)
	return append(header, sealed...), nil
}
