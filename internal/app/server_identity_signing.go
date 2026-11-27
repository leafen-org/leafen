package app

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/pocketbase/pocketbase/core"
)

// The server's signing identity for the apps (decided 2026-10-19, built-in
// Tailscale): an address whose DNS someone else answers — Tailscale's
// ts.net — could be given a certificate the server didn't get, so the
// packaged apps remember the server there. Tailscale makes a new certificate
// key at every renewal (its ACME client generates one each time), so the
// apps remember an Ed25519 key of the server's own instead, kept in the data
// folder (a backup brings it back). The server signs which certificate keys
// it serves at the address, with the app's nonce, and the app reads that
// answer over the very connection whose certificate it checks: someone in
// the middle shows another key, which the signed list doesn't hold.

const (
	serverIdentityPath    = "/server-identity"
	serverIdentityFolder  = "identity"
	serverIdentityContext = "leafen-identity/1"
	// servedPinsKept is how many recent certificate keys an address keeps.
	servedPinsKept = 4
)

var (
	servedPinsMu sync.Mutex
	servedPins   = map[string][]string{}
)

// publicKeyPin is a key's pin: SHA-256 of its SubjectPublicKeyInfo.
func publicKeyPin(spki []byte) string {
	sum := sha256.Sum256(spki)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// rememberServedCertificate keeps the key of a certificate the server serves
// at a name, for its identity's answer.
func rememberServedCertificate(name string, der []byte) {
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return
	}
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	pin := publicKeyPin(certificate.RawSubjectPublicKeyInfo)
	servedPinsMu.Lock()
	defer servedPinsMu.Unlock()
	pins := servedPins[name]
	if slices.Contains(pins, pin) {
		return
	}
	pins = append(pins, pin)
	if len(pins) > servedPinsKept {
		pins = pins[len(pins)-servedPinsKept:]
	}
	servedPins[name] = pins
}

func servedCertificatePins(name string) []string {
	servedPinsMu.Lock()
	defer servedPinsMu.Unlock()
	return slices.Clone(servedPins[strings.ToLower(name)])
}

// serverIdentityKey is the server's own Ed25519 key, made the first time.
func serverIdentityKey(app core.App) (ed25519.PrivateKey, error) {
	dir := filepath.Join(app.DataDir(), serverIdentityFolder)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	signer, err := loadOrCreateSigningKey(filepath.Join(dir, "key.pem"), func() (crypto.Signer, error) {
		_, private, err := ed25519.GenerateKey(rand.Reader)
		return private, err
	})
	if err != nil {
		return nil, err
	}
	key, ok := signer.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("the identity key isn't an Ed25519 key")
	}
	return key, nil
}

// serverIdentityMessage is what the server signs for an app.
func serverIdentityMessage(name, nonce string, pins []string) []byte {
	return []byte(serverIdentityContext + "\n" + strings.ToLower(name) + "\n" + nonce + "\n" + strings.Join(pins, ","))
}

type serverIdentityAnswer struct {
	Key       string   `json:"key"`
	Pins      []string `json:"pins"`
	Signature string   `json:"signature"`
}

func registerServerIdentityRoute(event *core.ServeEvent) {
	event.Router.GET(apiRoot+serverIdentityPath, readServerIdentityRoute)
}

// readServerIdentityRoute signs, for the app's nonce, the certificate keys
// the server serves at the name it was asked by.
func readServerIdentityRoute(event *core.RequestEvent) error {
	nonce := event.Request.URL.Query().Get("nonce")
	if len(nonce) < 16 || len(nonce) > 128 || strings.ContainsAny(nonce, "\n,") {
		return apiError(event, http.StatusBadRequest, errorCodeServerTunnelInvalid, "Ask with a nonce of 16 to 128 characters.")
	}
	key, err := serverIdentityKey(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeServerTunnelFailed, "The server's identity couldn't be read.", err)
	}
	name := event.Request.Host
	if host, _, err := net.SplitHostPort(name); err == nil {
		name = host
	}
	pins := servedCertificatePins(name)
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, serverIdentityAnswer{
		Key:       base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey)),
		Pins:      pins,
		Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, serverIdentityMessage(name, nonce, pins))),
	})
}

// loadOrCreateSigningKey reads a PKCS #8 key, made the first time.
func loadOrCreateSigningKey(path string, create func() (crypto.Signer, error)) (crypto.Signer, error) {
	if data, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("%s holds no key", filepath.Base(path))
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		signer, ok := parsed.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("%s holds no signing key", filepath.Base(path))
		}
		return signer, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	signer, err := create()
	if err != nil {
		return nil, err
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(signer)
	if err != nil {
		return nil, err
	}
	return signer, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600)
}
