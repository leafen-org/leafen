package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Desktop push, as in Notion's Settings → Notifications, is turned on per
// browser: the browser subscribes with the instance's VAPID key and the
// server keeps the subscription for the signed-in person. Only the browser
// vendors' push services are accepted as endpoints, so a subscription
// cannot point the server at any other host. Leafen runs no push relay: on a
// phone, real-time notifications come to Leafen added to the home screen.

const (
	// A person's subscriptions are read this many at a time to send to.
	webPushSubscriptionWindow = 50
	webPushEndpointBytes      = 2048
)

var webPushServiceHosts = []string{
	"fcm.googleapis.com", "push.services.mozilla.com", "push.apple.com", "notify.windows.com",
}

type webPushStateResponse struct {
	Available bool   `json:"available"`
	PublicKey string `json:"publicKey"`
}

type webPushSubscriptionInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func registerWebPushRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/account/web-push", readWebPushState)
	event.Router.PUT(apiRoot+"/account/web-push/subscription", saveWebPushSubscription)
	event.Router.DELETE(apiRoot+"/account/web-push/subscription", deleteWebPushSubscription)
}

// webPushEndpointAllowed accepts an HTTPS endpoint on a browser push service.
func webPushEndpointAllowed(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > webPushEndpointBytes || parsed.Scheme != "https" || parsed.User != nil ||
		(parsed.Port() != "" && parsed.Port() != "443") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, service := range webPushServiceHosts {
		if host == service || strings.HasSuffix(host, "."+service) {
			return true
		}
	}
	return false
}

func webPushSubscriptionValid(input webPushSubscriptionInput) bool {
	receiver, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(input.Keys.P256DH, "="))
	if err != nil || len(receiver) != 65 || receiver[0] != 4 {
		return false
	}
	secret, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(input.Keys.Auth, "="))
	if err != nil || len(secret) != 16 {
		return false
	}
	return webPushEndpointAllowed(input.Endpoint)
}

func webPushSubscriptionID(endpoint string) string {
	digest := sha256.Sum256([]byte("web-push-subscription\x00" + endpoint))
	return hex.EncodeToString(digest[:16])
}

// saveWebPushSubscriptionFor keeps a browser's subscription for a person,
// moving it from whoever used that browser before. Every device keeps its
// subscription until it turns it off or its push service says it is gone.
func saveWebPushSubscriptionFor(app core.App, userID string, input webPushSubscriptionInput, now time.Time) error {
	return app.RunInTransaction(func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_web_push_subscriptions}}
				([[id]], [[user_id]], [[endpoint]], [[p256dh]], [[auth]], [[failures]], [[created_at]], [[updated_at]])
			VALUES ({:id}, {:userId}, {:endpoint}, {:p256dh}, {:auth}, 0, {:now}, {:now})
			ON CONFLICT ([[endpoint]]) DO UPDATE SET
				[[user_id]] = excluded.[[user_id]], [[p256dh]] = excluded.[[p256dh]], [[auth]] = excluded.[[auth]],
				[[failures]] = 0, [[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{
			"id": webPushSubscriptionID(input.Endpoint), "userId": userID, "endpoint": input.Endpoint,
			"p256dh": input.Keys.P256DH, "auth": input.Keys.Auth,
			"now": now.UnixMilli(),
		}).Execute()
		return err
	})
}

func readWebPushState(event *core.RequestEvent) error {
	if _, ok := authenticatedUser(event); !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	key, err := webPushKey(event.App)
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't prepare push notifications.", "web_push.key", "key", errorCodeNotificationSettingsFailed, err,
		)
	}
	publicKey, err := webPushPublicKey(key)
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't prepare push notifications.", "web_push.key", "public-key", errorCodeNotificationSettingsFailed, err,
		)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, webPushStateResponse{Available: true, PublicKey: publicKey})
}

func saveWebPushSubscription(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := webPushSubscriptionInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeNotificationSettingsInvalid, "Check the push notification subscription.")
	}
	if strings.TrimSpace(input.Endpoint) == "" {
		return refuseMissingInput(event, errorCodeNotificationSettingsInvalid, "endpoint", "the push service address the browser's subscription gives.")
	}
	if input.Keys.P256DH == "" || input.Keys.Auth == "" {
		return refuseMissingInput(event, errorCodeNotificationSettingsInvalid, "keys", "p256dh and auth, as the browser's subscription gives them.")
	}
	if !webPushSubscriptionValid(input) {
		return apiError(event, http.StatusBadRequest, errorCodeNotificationSettingsInvalid, "Check the push notification subscription.")
	}
	if err := saveWebPushSubscriptionFor(event.App, user.Id, input, time.Now().UTC()); err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't turn on push notifications.", "web_push.subscription", "storage-write",
			errorCodeNotificationSettingsFailed, err,
		)
	}
	return event.NoContent(http.StatusNoContent)
}

func deleteWebPushSubscription(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := webPushSubscriptionInput{}
	if err := event.BindBody(&input); err != nil || input.Endpoint == "" || len(input.Endpoint) > webPushEndpointBytes {
		return apiError(event, http.StatusBadRequest, errorCodeNotificationSettingsInvalid, "Check the push notification subscription.")
	}
	if _, err := event.App.DB().NewQuery(`
		DELETE FROM {{leafen_web_push_subscriptions}} WHERE [[user_id]] = {:userId} AND [[endpoint]] = {:endpoint}
	`).Bind(dbx.Params{"userId": user.Id, "endpoint": input.Endpoint}).Execute(); err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't turn off push notifications.", "web_push.subscription", "storage-delete",
			errorCodeNotificationSettingsFailed, err,
		)
	}
	return event.NoContent(http.StatusNoContent)
}
