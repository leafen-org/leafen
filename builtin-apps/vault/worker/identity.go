package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// Signing in: a Bitwarden app derives a master key from the master
// password and the email, and sends only a hash of it. The server keeps
// its own salted hash of that hash, so a copy of the store opens nothing.

const (
	serverIterations = 100000
	accessTokenTTL   = 2 * 60 * 60
	// A prepared vault waits a day for its Bitwarden app.
	preparedFor     = 24 * 60 * 60 * 1000
	triesPermitted  = 10
	triesWindowSecs = 15 * 60
	fieldMaxRunes   = 20000
)

// kdfDefaults are what a Bitwarden app is told for an email with no vault,
// so the answer says nothing of who has one.
var kdfDefaults = kdfSettings{Kdf: 0, Iterations: 600000}

type kdfSettings struct {
	Kdf, Iterations, Memory, Parallelism int64
}

func (k kdfSettings) valid() bool {
	switch k.Kdf {
	case 0:
		return k.Iterations >= 100000 && k.Iterations <= 2000000
	case 1:
		return k.Iterations >= 1 && k.Iterations <= 10 && k.Memory >= 15 && k.Memory <= 1024 && k.Parallelism >= 1 && k.Parallelism <= 16
	}
	return false
}

func nullable(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func (c *call) prelogin() (leafen.EndpointAnswer, error) {
	input := struct {
		Email string `json:"email"`
	}{}
	if !c.decode(&input) {
		return problem(400, "Give an email."), nil
	}
	kdf := kdfDefaults
	acct, err := c.store.accountByEmail(input.Email)
	if err != nil && !errors.Is(err, errMissing) {
		return leafen.EndpointAnswer{}, err
	}
	if err == nil && acct.Status == statusActive {
		kdf = kdfSettings{acct.Kdf, acct.Iterations, acct.Memory, acct.Parallelism}
	}
	// The apps of 2026 on read the settings and the salt (the email) as one
	// part; older ones read the flat fields.
	return reply(200, map[string]any{"kdf": kdf.Kdf, "kdfIterations": kdf.Iterations, "kdfMemory": nullable(kdf.Memory),
		"kdfParallelism": nullable(kdf.Parallelism), "salt": normalEmail(input.Email),
		"kdfSettings": map[string]any{"kdfType": kdf.Kdf, "iterations": kdf.Iterations, "memory": nullable(kdf.Memory), "parallelism": nullable(kdf.Parallelism)}}), nil
}

// verifyEmail is the first step of a Bitwarden app's account making. Only
// an email someone prepared in Leafen's Vault app may go on, and the answer
// is the token the app's next step brings back.
func (c *call) verifyEmail() (leafen.EndpointAnswer, error) {
	input := struct {
		Email string `json:"email"`
	}{}
	if !c.decode(&input) {
		return problem(400, "Give an email."), nil
	}
	acct, err := c.store.accountByEmail(input.Email)
	if err != nil && !errors.Is(err, errMissing) {
		return leafen.EndpointAnswer{}, err
	}
	if err != nil || !acct.waiting(nowMs()) {
		return problem(400, notPrepared), nil
	}
	key, err := c.store.secret()
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return reply(200, emailToken(key, acct)), nil
}

const notPrepared = "Prepare your vault in Leafen's Vault app first, with this email; it waits a day for your Bitwarden app."

// emailToken binds the account making to the prepared vault.
func emailToken(key []byte, acct account) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("register\x00" + acct.row.ID + "\x00" + acct.Email))
	return hex.EncodeToString(mac.Sum(nil))
}

// register makes the account a person prepared: from a Bitwarden app's
// last step of account making, or the older one-step form.
func (c *call) register() (leafen.EndpointAnswer, error) {
	input := struct {
		Email              string   `json:"email"`
		Name               string   `json:"name"`
		Hint               string   `json:"masterPasswordHint"`
		Hash               string   `json:"masterPasswordHash"`
		Key                string   `json:"key"`
		UserSymmetricKey   string   `json:"userSymmetricKey"`
		EmailToken         string   `json:"emailVerificationToken"`
		Kdf                int64    `json:"kdf"`
		KdfIterations      int64    `json:"kdfIterations"`
		KdfMemory          *int64   `json:"kdfMemory"`
		KdfParallelism     *int64   `json:"kdfParallelism"`
		Keys               *keyPair `json:"keys"`
		UserAsymmetricKeys *keyPair `json:"userAsymmetricKeys"`
	}{}
	if !c.decode(&input) {
		return problem(400, "The account details couldn't be read."), nil
	}
	acct, err := c.store.accountByEmail(input.Email)
	if err != nil && !errors.Is(err, errMissing) {
		return leafen.EndpointAnswer{}, err
	}
	now := nowMs()
	if err != nil || !acct.waiting(now) {
		return problem(400, notPrepared), nil
	}
	if strings.HasSuffix(c.path, "/finish") {
		key, err := c.store.secret()
		if err != nil {
			return leafen.EndpointAnswer{}, err
		}
		if subtle.ConstantTimeCompare([]byte(input.EmailToken), []byte(emailToken(key, acct))) != 1 {
			return problem(400, notPrepared), nil
		}
	}
	userKey := firstOf(input.Key, input.UserSymmetricKey)
	keys := input.Keys
	if keys == nil {
		keys = input.UserAsymmetricKeys
	}
	if keys == nil {
		keys = &keyPair{}
	}
	kdf := kdfSettings{Kdf: input.Kdf, Iterations: input.KdfIterations}
	if input.KdfMemory != nil {
		kdf.Memory = *input.KdfMemory
	}
	if input.KdfParallelism != nil {
		kdf.Parallelism = *input.KdfParallelism
	}
	switch {
	case input.Hash == "" || userKey == "":
		return problem(400, "Your Bitwarden app sent no master password; update it and try again."), nil
	case !kdf.valid():
		return problem(400, "Choose PBKDF2 with 100,000 or more iterations, or Argon2id."), nil
	case tooLong(input.Name, input.Hint, input.Hash, userKey, keys.PublicKey, keys.EncryptedPrivateKey):
		return problem(400, "The account details are too long."), nil
	}
	salt := randomToken(16)
	values := map[string]any{"Status": statusActive, "Name": input.Name, "Hint": input.Hint, "Key": userKey,
		"Public key": keys.PublicKey, "Private key": keys.EncryptedPrivateKey, "Stamp": randomToken(18),
		"Hash": serverHash(input.Hash, salt), "Salt": salt, "Kdf": float64(kdf.Kdf), "Iterations": float64(kdf.Iterations),
		"Memory": float64(kdf.Memory), "Parallelism": float64(kdf.Parallelism), "Revision": float64(now), "Expires": float64(0)}
	if err := c.store.accounts.Update(acct.row.ID, values); err != nil {
		return leafen.EndpointAnswer{}, err
	}
	return reply(200, map[string]any{"object": "register", "captchaBypassToken": ""}), nil
}

type keyPair struct {
	PublicKey           string `json:"publicKey"`
	EncryptedPrivateKey string `json:"encryptedPrivateKey"`
}

func firstOf(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func tooLong(values ...string) bool {
	for _, value := range values {
		if utf8.RuneCountInString(value) > fieldMaxRunes {
			return true
		}
	}
	return false
}

// serverHash is the server's hash of the app's password hash.
func serverHash(clientHash, salt string) string {
	key, _ := pbkdf2.Key(sha256.New, clientHash, []byte(salt), serverIterations, 32)
	return base64.StdEncoding.EncodeToString(key)
}

func (acct account) passwordIs(clientHash string) bool {
	if acct.Hash == "" || clientHash == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(serverHash(clientHash, acct.Salt)), []byte(acct.Hash)) == 1
}

// token signs an app in with its master password's hash, or renews its
// access with the refresh token it was given.
func (c *call) token() (leafen.EndpointAnswer, error) {
	form, err := url.ParseQuery(string(c.body))
	if err != nil {
		return grantRefused("The login couldn't be read."), nil
	}
	switch form.Get("grant_type") {
	case "password":
		return c.passwordGrant(form)
	case "refresh_token":
		return c.refreshGrant(form.Get("refresh_token"))
	}
	return reply(400, map[string]any{"error": "unsupported_grant_type"}), nil
}

// grantRefused is Bitwarden's answer to a sign-in it refuses.
func grantRefused(message string) leafen.EndpointAnswer {
	return reply(400, map[string]any{"error": "invalid_grant", "error_description": message,
		"ErrorModel": map[string]any{"Message": message, "Object": "error"}})
}

func (c *call) passwordGrant(form url.Values) (leafen.EndpointAnswer, error) {
	email := normalEmail(form.Get("username"))
	// Each try costs the server a slow hash, so an email gets ten in a
	// quarter hour.
	tries := "tries:" + c.store.tenancy + ":" + email
	if count, _ := leafen.KV(leafen.KVRequest{Command: "GET", Key: tries, Tenancy: c.store.tenancy}); countOf(count) >= triesPermitted {
		return grantRefused("Too many tries; wait a quarter of an hour and try again."), nil
	}
	acct, err := c.store.accountByEmail(email)
	if err != nil && !errors.Is(err, errMissing) {
		return leafen.EndpointAnswer{}, err
	}
	if err != nil || acct.Status != statusActive || !acct.passwordIs(form.Get("password")) {
		_, _ = leafen.KV(leafen.KVRequest{Command: "INCR", Key: tries, TTL: triesWindowSecs, Tenancy: c.store.tenancy})
		return grantRefused("Username or password is incorrect. Try again."), nil
	}
	if allowed, err := c.mayUse(acct); err != nil || !allowed {
		return grantRefused(noLongerAllowed), err
	}
	identifier := strings.TrimSpace(form.Get("deviceIdentifier"))
	if identifier == "" || utf8.RuneCountInString(identifier) > 100 {
		return grantRefused("Your Bitwarden app sent no device; update it and try again."), nil
	}
	deviceType, _ := strconv.Atoi(form.Get("deviceType"))
	name := form.Get("deviceName")
	if utf8.RuneCountInString(name) > 100 {
		name = string([]rune(name)[:100])
	}
	secret := randomToken(32)
	// A row's account is set once, when it is made.
	values := map[string]any{"Identifier": identifier, "Name": name, "Type": float64(deviceType),
		"Refresh": hashOf(secret), "Stamp": acct.Stamp, "Seen": float64(nowMs())}
	deviceRow := ""
	devices, err := owned(c.store.devices, acct.row.ID)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	for _, device := range devices {
		if device.Text("Identifier") == identifier {
			deviceRow = device.ID
		}
	}
	if deviceRow != "" {
		err = c.store.devices.Update(deviceRow, values)
	} else {
		values["Account"], values["Created"] = []string{acct.row.ID}, float64(nowMs())
		deviceRow, err = c.store.devices.Add(values)
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	_, _ = leafen.KV(leafen.KVRequest{Command: "DEL", Key: tries, Tenancy: c.store.tenancy})
	return c.tokens(acct, deviceRow, identifier, deviceType, form.Get("client_id"), deviceRow+"."+secret)
}

const noLongerAllowed = "Your organization doesn't let you use Leafen's Vault app now; ask your administrator."

func countOf(raw json.RawMessage) int {
	var text string
	var number int
	if json.Unmarshal(raw, &number) == nil {
		return number
	}
	if json.Unmarshal(raw, &text) == nil {
		number, _ = strconv.Atoi(text)
	}
	return number
}

func hashOf(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(digest[:])
}

func (c *call) refreshGrant(refresh string) (leafen.EndpointAnswer, error) {
	deviceRow, secret, ok := strings.Cut(refresh, ".")
	if !ok || len(deviceRow) != 24 {
		return grantRefused("Log in again."), nil
	}
	device, err := c.store.devices.Row(deviceRow)
	if errors.Is(err, leafen.ErrNoRow) {
		return grantRefused("Log in again."), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	owners := device.IDs("Account")
	if len(owners) != 1 || subtle.ConstantTimeCompare([]byte(hashOf(secret)), []byte(device.Text("Refresh"))) != 1 {
		return grantRefused("Log in again."), nil
	}
	acct, err := c.store.accountByRow(owners[0])
	if errors.Is(err, errMissing) {
		return grantRefused("Log in again."), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	if acct.Status != statusActive || acct.Stamp != device.Text("Stamp") {
		return grantRefused("Log in again."), nil
	}
	if allowed, err := c.mayUse(acct); err != nil || !allowed {
		return grantRefused(noLongerAllowed), err
	}
	_ = c.store.devices.Update(deviceRow, map[string]any{"Seen": float64(nowMs())})
	return c.tokens(acct, deviceRow, device.Text("Identifier"), int(device.Number("Type")), "", refresh)
}

// mayUse asks Leafen whether the vault's person may still use the app in
// the organization.
func (c *call) mayUse(acct account) (bool, error) {
	answer, err := leafen.API(leafen.APIRequest{Tenancy: c.store.tenancy, Method: "GET", Path: "/app-people", Query: "ids=" + url.QueryEscape(acct.Person)})
	if err != nil {
		return false, err
	}
	people := struct {
		People []struct {
			ID string `json:"id"`
		} `json:"people"`
	}{}
	if answer.Status != 200 || json.Unmarshal(answer.Body, &people) != nil {
		return false, errors.New("the app's people couldn't be read")
	}
	for _, person := range people.People {
		if person.ID == acct.Person {
			return true, nil
		}
	}
	return false, nil
}

// tokens answers a sign-in with an access token, the refresh token and
// what the app needs to open the vault: its keys and key settings.
func (c *call) tokens(acct account, deviceRow, identifier string, deviceType int, clientID, refresh string) (leafen.EndpointAnswer, error) {
	key, err := c.store.secret()
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	now := nowMs() / 1000
	if clientID == "" {
		clientID = "mobile"
	}
	access := signToken(key, tokenClaims{
		NotBefore: now, Expires: now + accessTokenTTL, Issuer: c.base + "|login", Subject: acct.id(), Premium: true,
		Name: acct.Name, Email: acct.Email, EmailVerified: true, Stamp: acct.Stamp, Device: identifier, DeviceType: deviceType,
		ClientID: clientID, Scope: []string{"api", "offline_access"}, Methods: []string{"Application"},
	})
	kdf := map[string]any{"KdfType": acct.Kdf, "Iterations": acct.Iterations, "Memory": nullable(acct.Memory), "Parallelism": nullable(acct.Parallelism)}
	return reply(200, map[string]any{
		"access_token": access, "expires_in": accessTokenTTL, "token_type": "Bearer", "refresh_token": refresh, "scope": "api offline_access",
		"Key": acct.Key, "PrivateKey": acct.PrivateKey, "Kdf": acct.Kdf, "KdfIterations": acct.Iterations,
		"KdfMemory": nullable(acct.Memory), "KdfParallelism": nullable(acct.Parallelism),
		"ResetMasterPassword": false, "ForcePasswordReset": false, "MasterPasswordPolicy": map[string]any{"Object": "masterPasswordPolicy"},
		"UserDecryptionOptions": map[string]any{"HasMasterPassword": true, "Object": "userDecryptionOptions",
			"MasterPasswordUnlock": map[string]any{"Kdf": kdf, "MasterKeyEncryptedUserKey": acct.Key, "Salt": acct.Email}},
		"AccountKeys": accountKeys(acct),
	}), nil
}

// accountKeys are the account's key pair as Bitwarden's apps of 2025 on read
// it: the public key and the private key its user key encrypts (the first
// form; no signing keys).
func accountKeys(acct account) any {
	if acct.PublicKey == "" || acct.PrivateKey == "" {
		return nil
	}
	return map[string]any{
		"publicKeyEncryptionKeyPair": map[string]any{"wrappedPrivateKey": acct.PrivateKey, "publicKey": acct.PublicKey, "object": "publicKeyEncryptionKeyPair"},
		"signatureKeyPair":           nil, "securityState": nil, "object": "privateKeys",
	}
}

// knownDevice tells an app whether this device signed in to the email
// before; Leafen's vault asks nothing more of a new one, so the answer
// only spares the app a question.
func (c *call) knownDevice() (leafen.EndpointAnswer, error) {
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimRight(c.headers["x-request-email"], "="))
	identifier := c.headers["x-device-identifier"]
	acct, err := c.store.accountByEmail(string(raw))
	if errors.Is(err, errMissing) || identifier == "" {
		return reply(200, false), nil
	}
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	devices, err := owned(c.store.devices, acct.row.ID)
	if err != nil {
		return leafen.EndpointAnswer{}, err
	}
	for _, device := range devices {
		if device.Text("Identifier") == identifier {
			return reply(200, true), nil
		}
	}
	return reply(200, false), nil
}

// tokenClaims are an access token's, as Bitwarden's apps read them.
type tokenClaims struct {
	NotBefore     int64    `json:"nbf"`
	Expires       int64    `json:"exp"`
	Issuer        string   `json:"iss"`
	Subject       string   `json:"sub"`
	Premium       bool     `json:"premium"`
	Name          string   `json:"name"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Stamp         string   `json:"sstamp"`
	Device        string   `json:"device"`
	DeviceType    int      `json:"devicetype"`
	ClientID      string   `json:"client_id"`
	Scope         []string `json:"scope"`
	Methods       []string `json:"amr"`
}

var tokenHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

func signToken(key []byte, claims tokenClaims) string {
	payload, _ := json.Marshal(claims)
	signed := tokenHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signed))
	return signed + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func readToken(key []byte, token string, nowMs int64) (tokenClaims, bool) {
	claims := tokenClaims{}
	head, rest, ok := strings.Cut(token, ".")
	if !ok || head != tokenHeader {
		return claims, false
	}
	payload, signature, ok := strings.Cut(rest, ".")
	if !ok {
		return claims, false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(head + "." + payload))
	given, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(given, mac.Sum(nil)) {
		return claims, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || json.Unmarshal(raw, &claims) != nil {
		return claims, false
	}
	now := nowMs / 1000
	return claims, claims.NotBefore <= now+60 && now < claims.Expires
}
