package app

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Credentials people give an app (the mail decision of 2026-10-07): an app
// declares what it needs — a mail account's user name and password — and a
// person types it into a window Leafen draws outside the app's frame. Leafen
// seals it like an outside sign-in's token. The app never sees it: its
// worker writes {{credential:KEY.FIELD}} to a socket or a relay header whose
// entry names the credential, and Leafen fills it in a run made for that
// person; the fields marked secret are masked in what comes back (a user
// name, often in the mail itself, isn't). |quoted and |base64 after the field
// give it as a protocol needs it (an IMAP quoted string, SMTP's base64), and
// two fields joined, KEY.USER:PASSWORD|base64, give HTTP Basic's. It
// is off until the administrator turns it on for the app; people remove
// theirs when they like and the administrator takes them all back; putting,
// using, removing and taking back are in the app's audit.

const (
	appCredentialFieldsMax  = 4
	appCredentialValueBytes = 1024
	appCredentialSecretMin  = 4
	appCredentialUseEvery   = int64(time.Minute / time.Millisecond)
)

var (
	appCredentialPattern     = regexp.MustCompile(`\{\{credential:([a-z][a-zA-Z0-9_-]{0,47})\.([a-z][a-zA-Z0-9_-]{0,47})(?::([a-z][a-zA-Z0-9_-]{0,47}))?(?:\|(quoted|base64))?\}\}`)
	errAppCredentialRefused  = errors.New("the credential can't be used here")
	errAppCredentialsOff     = errors.New("the administrator hasn't let people give this app credentials")
	errAppCredentialNotGiven = errors.New("the person hasn't given this credential")
)

type appManifestCredential struct {
	Key    string                       `json:"key"`
	Name   map[string]string            `json:"name"`
	Help   map[string]string            `json:"help,omitempty"`
	Fields []appManifestCredentialField `json:"fields"`
}

type appManifestCredentialField struct {
	Key    string            `json:"key"`
	Name   map[string]string `json:"name"`
	Secret bool              `json:"secret,omitempty"`
}

func appCredentialProblems(manifest appManifest, add func(string, ...any)) {
	declared := map[string]bool{}
	for _, credential := range manifest.Credentials {
		if !appKeyPattern.MatchString(credential.Key) || declared[credential.Key] {
			add("credentials key %q is invalid or repeated", credential.Key)
		}
		declared[credential.Key] = true
		if len(credential.Name) == 0 {
			add("credentials %q needs a name", credential.Key)
		}
		if len(credential.Fields) == 0 || len(credential.Fields) > appCredentialFieldsMax {
			add("credentials %q has 1-%d fields", credential.Key, appCredentialFieldsMax)
		}
		fields := map[string]bool{}
		for _, field := range credential.Fields {
			if !appKeyPattern.MatchString(field.Key) || fields[field.Key] || len(field.Name) == 0 {
				add("credentials %q field %q is invalid, repeated or unnamed", credential.Key, field.Key)
			}
			fields[field.Key] = true
		}
	}
	check := func(where string, names []string) {
		for _, name := range names {
			if !declared[name] {
				add("%s carries credential %q, which credentials doesn't declare", where, name)
			}
		}
	}
	for _, socket := range manifest.Network.Sockets {
		check("network.sockets "+socket.Host, socket.Credentials)
	}
	for _, relay := range manifest.Network.Relay {
		check("network.relay "+relay.URL, relay.Credentials)
	}
}

func appCredentialDeclared(manifest appManifest, key string) (appManifestCredential, bool) {
	for _, credential := range manifest.Credentials {
		if credential.Key == key {
			return credential, true
		}
	}
	return appManifestCredential{}, false
}

// appCredentialValuesRefusal is why what a person typed isn't the
// credential's every field and nothing else, naming the field and never
// what was typed.
func appCredentialValuesRefusal(credential appManifestCredential, values map[string]string) error {
	keys := make([]string, 0, len(credential.Fields))
	for _, field := range credential.Fields {
		keys = append(keys, field.Key)
		value, given := values[field.Key]
		switch {
		case !given || value == "":
			return refuse(errAppInputInvalid, "values.%s is empty", field.Key)
		case len(value) > appCredentialValueBytes:
			return refuse(errAppInputInvalid, "values.%s is at most %d bytes", field.Key, appCredentialValueBytes)
		case !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\x00"):
			return refuse(errAppInputInvalid, "values.%s is one line of text", field.Key)
		case field.Secret && len(value) < appCredentialSecretMin:
			return refuse(errAppInputInvalid, "values.%s is at least %d characters", field.Key, appCredentialSecretMin)
		}
	}
	for key := range values {
		if !slices.Contains(keys, key) {
			return refuse(errAppInputInvalid, "values.%s isn't one of the fields %s takes: %s", key, credential.Key, strings.Join(keys, ", "))
		}
	}
	return nil
}

func appCredentialsEnabled(app core.App, installID string) (bool, error) {
	var enabled bool
	err := app.DB().NewQuery(`SELECT [[enabled]] FROM {{leafen_app_credential_policies}} WHERE [[install_id]] = {:install}`).
		Bind(dbx.Params{"install": installID}).Row(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return enabled, err
}

// loadAppPersonCredential opens what a person gave for one credential.
func loadAppPersonCredential(app core.App, installID, userID, key string) (map[string]string, error) {
	var sealed string
	err := app.DB().NewQuery(`SELECT [[sealed]] FROM {{leafen_app_person_credentials}}
		WHERE [[install_id]] = {:install} AND [[user_id]] = {:user} AND [[key]] = {:key}`).
		Bind(dbx.Params{"install": installID, "user": userID, "key": key}).Row(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errAppCredentialNotGiven
	}
	if err != nil {
		return nil, err
	}
	opened, err := decryptAdministratorSetting(app, sealed)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	return values, json.Unmarshal([]byte(opened), &values)
}

// appCredentialFiller fills a run's credential placeholders for the person
// it runs for, and remembers what it filled to mask it in what comes back.
type appCredentialFiller struct {
	app      core.App
	install  storedAppInstall
	manifest appManifest
	personID string
	mu       sync.Mutex
	opened   map[string]map[string]string
	// What it filled from secret fields, as written, to mask coming back.
	secrets []string
}

func newAppCredentialFiller(app core.App, install storedAppInstall, manifest appManifest, personID string) *appCredentialFiller {
	return &appCredentialFiller{app: app, install: install, manifest: manifest, personID: personID, opened: map[string]map[string]string{}}
}

func appMentionsCredential(text string) bool { return strings.Contains(text, "{{credential:") }

// fill replaces each {{credential:KEY.FIELD}} with the person's value; the
// entry it goes to must name the credential.
func (filler *appCredentialFiller) fill(text string, allowed []string) (string, error) {
	if filler == nil || filler.personID == "" {
		return "", fmt.Errorf("%w: only a run for a person carries their credentials", errAppCredentialRefused)
	}
	filler.mu.Lock()
	defer filler.mu.Unlock()
	var failure error
	filled := appCredentialPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := appCredentialPattern.FindStringSubmatch(match)
		key, field, paired, form := parts[1], parts[2], parts[3], parts[4]
		if !appOneOf(key, allowed) {
			failure = fmt.Errorf("%w: this address doesn't take the %s credential", errAppCredentialRefused, key)
			return ""
		}
		values, err := filler.open(key)
		if err != nil {
			failure = err
			return ""
		}
		value, ok := values[field]
		if !ok {
			failure = fmt.Errorf("%w: the %s credential has no %s", errAppCredentialRefused, key, field)
			return ""
		}
		secret := appCredentialSecretField(filler.manifest, key, field)
		filler.remember(secret, value)
		if paired != "" {
			// Two fields as HTTP Basic joins them: "user:password".
			other, ok := values[paired]
			if !ok {
				failure = fmt.Errorf("%w: the %s credential has no %s", errAppCredentialRefused, key, paired)
				return ""
			}
			pairedSecret := appCredentialSecretField(filler.manifest, key, paired)
			filler.remember(pairedSecret, other)
			value, secret = value+":"+other, secret || pairedSecret
			filler.remember(secret, value)
		}
		switch form {
		case "quoted":
			value = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
		case "base64":
			value = base64.StdEncoding.EncodeToString([]byte(value))
		}
		filler.remember(secret, value)
		return value
	})
	if failure != nil {
		return "", failure
	}
	if appMentionsCredential(filled) {
		return "", fmt.Errorf("%w: a credential named in the wrong form", errAppCredentialRefused)
	}
	return filled, nil
}

func (filler *appCredentialFiller) open(key string) (map[string]string, error) {
	if values, ok := filler.opened[key]; ok {
		return values, nil
	}
	if _, declared := appCredentialDeclared(filler.manifest, key); !declared {
		return nil, fmt.Errorf("%w: the app doesn't declare the %s credential", errAppCredentialRefused, key)
	}
	enabled, err := appCredentialsEnabled(filler.app, filler.install.ID)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, errAppCredentialsOff
	}
	values, err := loadAppPersonCredential(filler.app, filler.install.ID, filler.personID, key)
	if err != nil {
		return nil, err
	}
	if err := filler.noteUse(key); err != nil {
		return nil, err
	}
	filler.opened[key] = values
	return values, nil
}

// noteUse keeps when the credential was last used, in the app's audit at
// most once a minute; a use the audit can't keep isn't made.
func (filler *appCredentialFiller) noteUse(key string) error {
	now := time.Now().UnixMilli()
	return filler.app.RunInTransaction(func(tx core.App) error {
		result, err := tx.DB().NewQuery(`UPDATE {{leafen_app_person_credentials}} SET [[used_at]] = {:now}
			WHERE [[install_id]] = {:install} AND [[user_id]] = {:user} AND [[key]] = {:key} AND [[used_at]] < {:since}`).
			Bind(dbx.Params{"now": now, "install": filler.install.ID, "user": filler.personID, "key": key, "since": now - appCredentialUseEvery}).Execute()
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			return nil
		}
		return recordAppAudit(tx, filler.install, "credential-use", filler.personID, map[string]any{"key": key})
	})
}

func appCredentialSecretField(manifest appManifest, key, field string) bool {
	credential, _ := appCredentialDeclared(manifest, key)
	for _, declared := range credential.Fields {
		if declared.Key == field {
			return declared.Secret
		}
	}
	return false
}

func (filler *appCredentialFiller) remember(secret bool, value string) {
	if secret && len(value) >= appCredentialSecretMin && !appOneOf(value, filler.secrets) {
		filler.secrets = append(filler.secrets, value)
	}
}

// pass masks what a socket sent for the run: each secret it filled becomes
// as many asterisks, so a protocol that counts bytes still can; an end that
// may be the start of one waits for the rest — however small the reads — and
// is masked when the connection ends.
func (filler *appCredentialFiller) pass(pending []byte, ended bool) ([]byte, []byte) {
	if filler == nil {
		return pending, nil
	}
	filler.mu.Lock()
	defer filler.mu.Unlock()
	if len(filler.secrets) == 0 {
		return pending, nil
	}
	masked := append([]byte(nil), pending...)
	for _, secret := range filler.secrets {
		masked = bytes.ReplaceAll(masked, []byte(secret), bytes.Repeat([]byte("*"), len(secret)))
	}
	hold := 0
	for _, secret := range filler.secrets {
		for size := min(len(secret)-1, len(masked)); size > hold; size-- {
			if bytes.HasSuffix(masked, []byte(secret[:size])) {
				hold = size
				break
			}
		}
	}
	if ended {
		copy(masked[len(masked)-hold:], bytes.Repeat([]byte("*"), hold))
		return masked, nil
	}
	return masked[:len(masked)-hold], append([]byte(nil), masked[len(masked)-hold:]...)
}

// values are the secrets the run filled, masked where the relay masks.
func (filler *appCredentialFiller) values() []string {
	if filler == nil {
		return nil
	}
	filler.mu.Lock()
	defer filler.mu.Unlock()
	return append([]string(nil), filler.secrets...)
}
