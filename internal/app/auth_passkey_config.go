package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errPasskeyUnavailable = errors.New(errorCodePasskeyUnavailable)
	errPasskeyInvalid     = errors.New(errorCodePasskeyInvalid)
	errPasskeyConflict    = errors.New(errorCodePasskeyConflict)
	errPasskeyLimit       = errors.New(errorCodePasskeyLimit)
)

// A revoked/shortened native session still consumes its WebAuthn challenge.
// Cleanup must retain that receipt through the ticket lifetime; other sessions
// retain their ordinary expiry. The expression index and cleanup share this.
var passkeySessionCleanupTimeSQL = fmt.Sprintf("CASE WHEN [[request_id]] GLOB 'passkey-login:*' THEN max([[expires_at]], [[created_at]] + %d) ELSE [[expires_at]] END", passkeyCeremonyTTLSeconds*1000)

// The request Host is never a passkey authority. An operator declares the
// public origin; the signed client data must name precisely that origin.
func configuredPasskeyRP() (*webauthn.WebAuthn, passkeyCapability, error) {
	capability := passkeyCapability{}
	raw := strings.TrimSpace(os.Getenv("LEAFEN_PUBLIC_URL"))
	// The address an administrator chose in the settings declares it as well,
	// so a server with an address of its own offers passkeys; a quick tunnel's
	// lasts only until it restarts.
	if chosen := serverTunnelChosen.Load(); raw == "" && chosen != nil && chosen.Mode != serverTunnelQuick {
		raw = chosenServerOrigin()
	}
	origin, err := url.Parse(raw)
	if err != nil || raw == "" || origin.Host == "" || origin.User != nil || origin.Opaque != "" ||
		(origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.Fragment != "" ||
		strings.ContainsAny(raw, "\\\x00\r\n\t ") {
		return nil, capability, errPasskeyUnavailable
	}
	host := strings.ToLower(origin.Hostname())
	if origin.Scheme != "https" && !(origin.Scheme == "http" && host == "localhost") {
		return nil, capability, errPasskeyUnavailable
	}
	origin.Host = strings.ToLower(origin.Host)
	origin.Path = ""
	origin.RawPath = ""
	capability = passkeyCapability{Available: true, Origin: origin.String(), RPID: host}
	timeout := webauthn.TimeoutConfig{Enforce: true, Timeout: passkeyCeremonyTTLSeconds * time.Second}
	rp, err := webauthn.New(&webauthn.Config{
		RPID: host, RPDisplayName: "Leafen", RPOrigins: []string{capability.Origin},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{Login: timeout, Registration: timeout},
	})
	if err != nil {
		return nil, passkeyCapability{}, errPasskeyUnavailable
	}
	return rp, capability, nil
}

type passkeyUser struct {
	record      *core.Record
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte { return []byte(u.record.Id) }

// The name a passkey is kept under in the person's password manager: the
// email, or the account's name where it has none.
func (u passkeyUser) WebAuthnName() string {
	if email := u.record.Email(); email != "" {
		return email
	}
	return accountLabel(u.record)
}
func (u passkeyUser) WebAuthnDisplayName() string                { return accountLabel(u.record) }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func passkeyOptions(value any) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	options := map[string]any{}
	err = json.Unmarshal(raw, &options)
	return options, err
}
