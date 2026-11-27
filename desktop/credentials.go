package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"sync"
)

// CredentialBroker keeps the packaged app's sign-ins on other Leafen servers
// in the platform's secret storage: the packaged client owns the complete
// profile registry and its credentials, and gives its window only
// the active profile's when the window asks. A profile is a server's origin.
// The window calls it through Wails' bindings; Android's window has the
// app's own Keystore store instead (LeafenCredentials.java). The app's own
// server's sign-in is kept too, under the "local" profile. Each call has the
// store to itself, as an item's update and its first add are two steps.
type CredentialBroker struct {
	store secretStore
	mu    sync.Mutex
}

// secretStore is the platform's secret storage, by account name.
type secretStore interface {
	load(account string) (secret string, found bool, err error)
	save(account, secret string) error
	forget(account string) error
}

// credentialService names the app's items in the platform's secret storage.
const credentialService = "org.leafen.app.profile"

const maxCredentialBytes = 16 << 10

var (
	errCredentialProfile  = errors.New("a profile is a Leafen server's origin or the app's own")
	errCredentialTooLarge = errors.New("the login is too large to keep")
	errCredentialStore    = errors.New("this platform keeps no logins")
)

func newCredentialBroker() *CredentialBroker {
	return &CredentialBroker{store: platformSecretStore()}
}

// localProfile is the app's own server's profile, kept under one name as
// that server's address can change.
const localProfile = "local"

// profileAccount checks that a profile is an http(s) origin, or the app's
// own server's, and names its item. A development build pointed at other
// data keeps its own server's sign-in apart.
func profileAccount(profile string) (string, error) {
	if profile == localProfile {
		if dir := os.Getenv(localDataDirEnvironment); dir != "" {
			sum := sha256.Sum256([]byte(dir))
			return localProfile + ":" + hex.EncodeToString(sum[:6]), nil
		}
		return localProfile, nil
	}
	parsed, err := url.Parse(profile)
	if err != nil || len(profile) > 512 || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errCredentialProfile
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

// Load gives a profile's kept sign-in, or "" when none is kept.
func (broker *CredentialBroker) Load(profile string) (string, error) {
	account, err := profileAccount(profile)
	if err != nil {
		return "", err
	}
	if broker.store == nil {
		return "", errCredentialStore
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	secret, found, err := broker.store.load(account)
	if err != nil || !found {
		return "", err
	}
	return secret, nil
}

// Save keeps a profile's sign-in in place of the one kept before.
func (broker *CredentialBroker) Save(profile, secret string) error {
	account, err := profileAccount(profile)
	if err != nil {
		return err
	}
	if len(secret) > maxCredentialBytes {
		return errCredentialTooLarge
	}
	if broker.store == nil {
		return errCredentialStore
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	if secret == "" {
		return broker.store.forget(account)
	}
	return broker.store.save(account, secret)
}

// Asking tells whether macOS shows its own permission prompt now. On a Mac,
// reading an item another build kept — one signed otherwise, as a
// development build's or one from before the app was signed — waits for the
// person to answer that prompt (2026-11-24: a Developer ID build read its own
// items and its update's without one), and the window, which can't see it,
// asks this while a call is slow to answer so it can say what it waits for
// (web/src/lib/keychainPrompt.ts). It takes no turn with the other calls.
func (broker *CredentialBroker) Asking() bool {
	return systemPromptShowing()
}

// ReadyWebKitKey reads the key WebKit keeps in the keychain for the keys a
// page stores (its WebCrypto master key), before the page's step that has
// WebKit read it. WebKit's own read, when macOS asks first, stops the page's
// script until the prompt is answered, so the page can't say what it waits
// for; read here first, the prompt comes while the window still can. The
// key is only read, and dropped.
func (broker *CredentialBroker) ReadyWebKitKey() {
	readWebKitKey()
}

// Forget drops a profile's kept sign-in; one never kept is already gone.
func (broker *CredentialBroker) Forget(profile string) error {
	account, err := profileAccount(profile)
	if err != nil {
		return err
	}
	if broker.store == nil {
		return errCredentialStore
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	return broker.store.forget(account)
}
