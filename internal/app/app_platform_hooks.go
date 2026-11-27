package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Incoming webhooks for apps (docs/app-platform.md 7, T15): an outside
// service calls an app's hook at an address the administrator gives it,
// proving itself with the hook's secret — a token in the address, or an
// HMAC signature of the body, Leafen's own with a timestamp or the common
// X-Hub-Signature-256. The same delivery is taken once. What arrives waits,
// durably and for a week, for the app's worker; the sender gets 202.

const (
	appHookEventsTable = "leafen_app_hook_events"
	appHookVerifyToken = "token"
	appHookVerifyHMAC  = "hmac-sha256"
)

var (
	errAppHookRefused = errors.New(errorCodeAppHookRefused)
	errAppHookStopped = errors.New(errorCodeAppHookStopped)
	errAppHookLimited = errors.New(errorCodeAppHookLimited)

	appHookLimiter = newAppRelayLimits(appHookPerMinute, appHookPerMinute)
	// appHookWake tells the worker executor a call has arrived.
	appHookWake = make(chan struct{}, 1)
)

func appManifestHook(manifest appManifest, key string) (appManifestWebhook, bool) {
	for _, hook := range manifest.Webhooks {
		if hook.Key == key {
			return hook, true
		}
	}
	return appManifestWebhook{}, false
}

func appHookAddress(origin string, install storedAppInstall, key string) string {
	return origin + apiRoot + "/apps/" + url.PathEscape(install.Slug) + "/hooks/" + url.PathEscape(key)
}

// listAppHooks shows each hook with its address, whether it has a secret,
// what waits and when the last arrived.
func listAppHooks(app core.App, install storedAppInstall, manifest appManifest, origin string) ([]appHookWire, error) {
	hooks := []appHookWire{}
	for _, declared := range manifest.Webhooks {
		hook := appHookWire{Key: declared.Key, Name: declared.Name, Verify: declared.Verify, URL: appHookAddress(origin, install, declared.Key)}
		var sealed string
		err := app.DB().NewQuery(`SELECT [[last4]], [[sealed]] FROM {{leafen_app_hook_secrets}} WHERE [[install_id]] = {:install} AND [[hook]] = {:hook}`).
			Bind(dbx.Params{"install": install.ID, "hook": declared.Key}).Row(&hook.Last4, &sealed)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		hook.Set = err == nil
		if hook.Set {
			_, openErr := decryptAdministratorSetting(app, sealed)
			hook.NeedsNewSecret = openErr != nil
		}
		if err := app.DB().NewQuery(`SELECT COUNT(*) FILTER (WHERE [[status]] = 'waiting'), COALESCE(MAX([[received_at]]), 0)
			FROM {{leafen_app_hook_events}} WHERE [[install_id]] = {:install} AND [[hook]] = {:hook} AND [[kind]] = 'hook' AND [[topic]] = ''`).
			Bind(dbx.Params{"install": install.ID, "hook": declared.Key}).Row(&hook.Waiting, &hook.LastReceivedAt); err != nil {
			return nil, err
		}
		if hook.EarlierAddress, err = appHookEarlierAddress(app, install.ID, declared.Key, ""); err != nil {
			return nil, err
		}
		if hook.Organizations, err = appHookOrganizations(app, install, declared.Key, origin); err != nil {
			return nil, err
		}
		hooks = append(hooks, hook)
	}
	return hooks, nil
}

// makeAppHookSecret makes a hook's secret anew, sealed — the hook's own, or
// an organization's address's; the secret and the address to give the
// sender come back this once.
func makeAppHookSecret(app core.App, install storedAppInstall, manifest appManifest, key, tenancy, actorID, origin string) (appHookSecretWire, error) {
	declared, ok := appManifestHook(manifest, key)
	if !ok {
		return appHookSecretWire{}, refuse(errAppInputInvalid, "the app declares no hook named %q", key)
	}
	secret := "lwh_" + security.RandomString(40)
	sealed, err := encryptAdministratorSetting(app, cipherAppHook, secret)
	if err != nil {
		return appHookSecretWire{}, err
	}
	if err := app.RunInTransaction(func(tx core.App) error {
		if tenancy != "" {
			if err := storeAppHookOrgSecret(tx, install, key, tenancy, sealed, secret[len(secret)-4:], actorID); err != nil {
				return err
			}
			return recordAppAudit(tx, install, "hook-secret", actorID, map[string]any{"hook": key, "tenancy": tenancy})
		}
		if _, err := tx.DB().NewQuery(`INSERT INTO {{leafen_app_hook_secrets}}
			([[install_id]], [[hook]], [[sealed]], [[last4]], [[created_by]], [[created_at]])
			VALUES ({:install}, {:hook}, {:sealed}, {:last4}, {:actor}, {:now})
			ON CONFLICT ([[install_id]], [[hook]]) DO UPDATE SET [[sealed]] = excluded.[[sealed]], [[last4]] = excluded.[[last4]],
				[[created_by]] = excluded.[[created_by]], [[created_at]] = excluded.[[created_at]]`).Bind(dbx.Params{
			"install": install.ID, "hook": key, "sealed": sealed, "last4": secret[len(secret)-4:], "actor": actorID,
			"now": time.Now().UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		return recordAppAudit(tx, install, "hook-secret", actorID, map[string]any{"hook": key})
	}); err != nil {
		return appHookSecretWire{}, err
	}
	hooks, err := listAppHooks(app, install, manifest, origin)
	if err != nil {
		return appHookSecretWire{}, err
	}
	result := appHookSecretWire{Secret: secret, URL: appHookAddress(origin, install, key), TenancyKey: tenancy}
	for _, hook := range hooks {
		if hook.Key == key {
			result.Hook = hook
		}
	}
	separator := "?"
	if tenancy != "" {
		result.URL, separator = appHookOrgAddress(origin, install, key, tenancy), "&"
	}
	if declared.Verify == appHookVerifyToken {
		result.URL += separator + "token=" + url.QueryEscape(secret)
	}
	return result, nil
}

// receiveAppHook takes one delivery for an app's hook: the install by its
// address, the sender by the hook's secret, the delivery once.
func receiveAppHook(app core.App, slug, key string, request *http.Request, body []byte, now time.Time) (appHookReceipt, error) {
	installs, err := loadAppInstalls(app, "[[slug]] = {:slug}", dbx.Params{"slug": slug})
	if err != nil {
		return appHookReceipt{}, err
	}
	if len(installs) != 1 || installs[0].RemovedAt != 0 {
		return appHookReceipt{}, errAppInstallNotFound
	}
	install := installs[0]
	_, manifest, err := loadAppPackage(app, install.PackageHash)
	if err != nil {
		return appHookReceipt{}, err
	}
	hook, ok := appManifestHook(manifest, key)
	if !ok {
		return appHookReceipt{}, errAppInstallNotFound
	}
	if install.Status != "active" {
		return appHookReceipt{}, errAppHookStopped
	}
	tenancy := ""
	if organization := request.URL.Query().Get("org"); organization != "" {
		tenancy = "org:" + organization
	}
	sealed, err := appHookSecretFor(app, install.ID, key, tenancy)
	if err != nil {
		return appHookReceipt{}, refusedUnlessStorage(errAppHookRefused, err)
	}
	// A secret sealed under another key can't check a call; the
	// administrator makes it anew.
	secret, err := decryptAdministratorSetting(app, sealed)
	if err != nil {
		return appHookReceipt{}, errAppHookRefused
	}
	delivery := firstHeader(request, "X-Leafen-Delivery", "X-GitHub-Delivery", "Idempotency-Key")
	switch hook.Verify {
	case appHookVerifyToken:
		given := request.URL.Query().Get("token")
		if given == "" {
			given = request.Header.Get("X-Leafen-Token")
		}
		if subtle.ConstantTimeCompare([]byte(given), []byte(secret)) != 1 {
			return appHookReceipt{}, errAppHookRefused
		}
	case appHookVerifyHMAC:
		signature, ok := appHookSignatureValid(request, body, secret, now)
		if !ok {
			return appHookReceipt{}, errAppHookRefused
		}
		// A signed request seen before is the same request again.
		if delivery == "" {
			delivery = "sig:" + signature
		}
	default:
		return appHookReceipt{}, errAppHookRefused
	}
	// An organization's address keeps its deliveries apart from the others'.
	if delivery != "" && tenancy != "" {
		delivery = tenancy + "|" + delivery
	}
	if len(delivery) > 200 {
		digest := sha256.Sum256([]byte(delivery))
		delivery = "long:" + hex.EncodeToString(digest[:])
	}
	if delivery != "" {
		var existing string
		err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_app_hook_events}} WHERE [[install_id]] = {:install} AND [[hook]] = {:hook}
			AND ` + partialIndexKey(`[[delivery]]`, ` = {:delivery}`) + ` AND [[topic]] = {:tenancy}`).
			Bind(dbx.Params{"install": install.ID, "hook": key, "delivery": delivery, "tenancy": tenancy}).Row(&existing)
		if err == nil {
			return appHookReceipt{ID: existing, Duplicate: true}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return appHookReceipt{}, err
		}
	}
	release, admitted := appHookLimiter.admit(install.ID+"\x00hook", key, now)
	if !admitted {
		return appHookReceipt{}, errAppHookLimited
	}
	release()
	id := security.RandomString(20)
	contentType := request.Header.Get("Content-Type")
	if len(contentType) > 200 {
		contentType = contentType[:200]
	}
	if _, err := app.DB().Insert(appHookEventsTable, dbx.Params{
		"id": id, "install_id": install.ID, "hook": key, "delivery": delivery, "content_type": contentType,
		"body": body, "status": "waiting", "received_at": now.UnixMilli(), "topic": tenancy,
		// The earlier address it came to, if it did (app_platform_address_change.go).
		"via": appHookVia(request),
	}).Execute(); err != nil {
		return appHookReceipt{}, err
	}
	select {
	case appHookWake <- struct{}{}:
	default:
	}
	return appHookReceipt{ID: id}, nil
}

// appHookSignatureValid checks Leafen's timestamped signature or the common
// body signature, and returns the signature that identifies the request.
func appHookSignatureValid(request *http.Request, body []byte, secret string, now time.Time) (string, bool) {
	sign := func(message []byte) []byte {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(message)
		return mac.Sum(nil)
	}
	matches := func(given string, want []byte) bool {
		decoded, err := hex.DecodeString(given)
		return err == nil && hmac.Equal(decoded, want)
	}
	if given := request.Header.Get("X-Leafen-Signature"); given != "" {
		seconds, err := strconv.ParseInt(request.Header.Get("X-Leafen-Timestamp"), 10, 64)
		if err != nil || now.Sub(time.Unix(seconds, 0)).Abs() > appHookWindowSeconds*time.Second {
			return "", false
		}
		want := sign(append([]byte(strconv.FormatInt(seconds, 10)+"."), body...))
		for _, part := range strings.Split(given, ",") {
			if value, ok := strings.CutPrefix(strings.TrimSpace(part), "v1="); ok && matches(value, want) {
				return value, true
			}
		}
		return "", false
	}
	if given, ok := strings.CutPrefix(request.Header.Get("X-Hub-Signature-256"), "sha256="); ok && matches(given, sign(body)) {
		return given, true
	}
	return "", false
}

func firstHeader(request *http.Request, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(request.Header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

// pruneAppHookEvents keeps a week of deliveries.
func pruneAppHookEvents(app core.App, now time.Time) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_app_hook_events}} WHERE [[received_at]] < {:before}`).
		Bind(dbx.Params{"before": now.Add(-appHookKeepDays * 24 * time.Hour).UnixMilli()}).Execute()
	return err
}
