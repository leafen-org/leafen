package app

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// An internal connection's webhook subscriptions, as Notion's "웹훅" tab
// (seen 2026-10-31): each an address, an API version and the events it
// hears — in Notion's format and signed as notion_webhooks.go does — of the
// pages shared with the connection, comments only while it may read them.
// As Notion's, a new one is first sent a verification token and hears
// nothing until that token is given back on the page; its address changes
// only before then, its events at any time.

const (
	internalConnectionWebhooksPath      = internalConnectionItemPath + "/webhooks"
	internalConnectionWebhookPath       = internalConnectionWebhooksPath + "/{webhookId}"
	internalConnectionWebhookVerifyPath = internalConnectionWebhookPath + "/verify"
	internalConnectionWebhookResendPath = internalConnectionWebhookPath + "/resend"
	internalConnectionWebhookRequest    = "internal-webhook:"
	internalConnectionWebhooksMax       = 20
	internalConnectionWebhookPending    = "pending"

	errorCodeInternalConnectionWebhookToken = "LEAFEN_INTERNAL_CONNECTION_WEBHOOK_TOKEN"
	errorCodeInternalConnectionWebhookURL   = "LEAFEN_INTERNAL_CONNECTION_WEBHOOK_URL_VERIFIED"
)

var (
	errInternalConnectionWebhookToken = errors.New(errorCodeInternalConnectionWebhookToken)
	errInternalConnectionWebhookURL   = errors.New(errorCodeInternalConnectionWebhookURL)
)

// notionWebhookAPIVersions are the versions Notion's dialog offers, the
// newest its default (seen 2026-10-31).
var notionWebhookAPIVersions = []string{"2022-06-28", "2025-09-03", "2026-03-11"}

// notionWebhookEventTypes are Notion's events in its dialog's order (seen
// 2026-10-31). Leafen sends those its changes make (event_webhook_events.go).
var notionWebhookEventTypes = []string{
	"page.content_updated", "page.created", "page.deleted", "page.locked", "page.moved", "page.properties_updated",
	"page.undeleted", "page.unlocked",
	"database.content_updated", "database.created", "database.deleted", "database.moved", "database.schema_updated",
	"database.undeleted",
	"data_source.content_updated", "data_source.schema_updated", "data_source.created", "data_source.moved",
	"data_source.deleted", "data_source.undeleted",
	"view.created", "view.updated", "view.deleted",
	"comment.created", "comment.deleted", "comment.updated",
	"file_upload.created", "file_upload.completed", "file_upload.expired", "file_upload.upload_failed",
}

// notionWebhookDefaultOff are the events Notion's dialog leaves unchecked:
// a database's, which its data sources' now say.
var notionWebhookDefaultOff = []string{"database.content_updated", "database.schema_updated"}

type internalConnectionWebhookResponse struct {
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	APIVersion string   `json:"apiVersion"`
	EventTypes []string `json:"eventTypes"`
	Verified   bool     `json:"verified"`
	CreatedAt  int64    `json:"createdAt"`
	// Failures and LastFailedAt are its failed events and when the latest
	// failed (internalConnectionWebhookFailures).
	Failures     int   `json:"failures"`
	LastFailedAt int64 `json:"lastFailedAt"`
}

type internalConnectionWebhookInput struct {
	URL        *string   `json:"url"`
	APIVersion *string   `json:"apiVersion"`
	EventTypes *[]string `json:"eventTypes"`
}

func registerInternalConnectionWebhookRoutes(event *core.ServeEvent) {
	limit := apis.BodyLimit(internalConnectionBodyBytes)
	event.Router.GET(apiRoot+internalConnectionWebhooksPath, listInternalConnectionWebhooks)
	event.Router.POST(apiRoot+internalConnectionWebhooksPath, createInternalConnectionWebhook).Bind(limit)
	event.Router.PATCH(apiRoot+internalConnectionWebhookPath, updateInternalConnectionWebhook).Bind(limit)
	event.Router.DELETE(apiRoot+internalConnectionWebhookPath, deleteInternalConnectionWebhook)
	event.Router.POST(apiRoot+internalConnectionWebhookVerifyPath, verifyInternalConnectionWebhook).Bind(limit)
	event.Router.POST(apiRoot+internalConnectionWebhookResendPath, resendInternalConnectionWebhookToken).Bind(limit)
}

func internalConnectionWebhookOf(app core.App, stored storedEventWebhookSubscription) internalConnectionWebhookResponse {
	types := []string{}
	_ = json.Unmarshal([]byte(stored.EventTypesJSON), &types)
	failures, lastFailedAt := internalConnectionWebhookFailures(app, stored.ID)
	return internalConnectionWebhookResponse{
		ID: stored.ID, URL: stored.URL, APIVersion: stored.APIVersion, EventTypes: types,
		Verified: stored.VerifyState != internalConnectionWebhookPending, CreatedAt: stored.Created,
		Failures: failures, LastFailedAt: lastFailedAt,
	}
}

func internalConnectionWebhookConnectionID(connection storedInternalConnection) string {
	return "notion:" + connection.GrantID
}

func loadInternalConnectionWebhooks(app core.App, connection storedInternalConnection) ([]storedEventWebhookSubscription, error) {
	stored := []storedEventWebhookSubscription{}
	err := app.DB().NewQuery(`SELECT ` + eventWebhookSubscriptionColumns + ` FROM {{leafen_webhook_subscriptions}}
		WHERE [[connection_id]] = {:connection} AND [[kind]] = {:kind} AND [[request_id]] LIKE {:request}
		ORDER BY [[created]], [[id]]`).Bind(dbx.Params{
		"connection": internalConnectionWebhookConnectionID(connection), "kind": notionWebhookKind,
		"request": internalConnectionWebhookRequest + "%",
	}).All(&stored)
	return stored, err
}

// listInternalConnectionWebhooks answers the connection's subscriptions and
// what its dialog offers.
func listInternalConnectionWebhooks(event *core.RequestEvent) error {
	connection, err := managedInternalConnection(event)
	if err != nil {
		return internalConnectionError(event, err, "webhooks")
	}
	stored, err := loadInternalConnectionWebhooks(event.App, connection)
	if err != nil {
		return internalConnectionError(event, err, "webhooks")
	}
	items := make([]internalConnectionWebhookResponse, 0, len(stored))
	for _, subscription := range stored {
		items = append(items, internalConnectionWebhookOf(event.App, subscription))
	}
	defaults := []string{}
	for _, kind := range notionWebhookEventTypes {
		if !slices.Contains(notionWebhookDefaultOff, kind) {
			defaults = append(defaults, kind)
		}
	}
	versions := map[string][]string{}
	for _, version := range notionWebhookAPIVersions {
		versions[version] = notionWebhookVersionEventTypes(version)
	}
	return event.JSON(http.StatusOK, map[string]any{
		"items": items, "eventTypes": notionWebhookEventTypes, "defaultEventTypes": defaults,
		"apiVersions": notionWebhookAPIVersions, "versionEventTypes": versions,
	})
}

// notionWebhookVersionEventTypes are what Notion's dialog picks once its
// version is changed (seen 2026-10-31): for 2022-06-28 a database's six
// events, for a later version a database's made, deleted, moved and
// restored and a data source's content and schema; pages, comments and
// file uploads for each.
func notionWebhookVersionEventTypes(version string) []string {
	types := []string{}
	for _, kind := range notionWebhookEventTypes {
		group, _, _ := strings.Cut(kind, ".")
		switch group {
		case "page", "comment", "file_upload":
			types = append(types, kind)
		case "database":
			if version == "2022-06-28" || !slices.Contains(notionWebhookDefaultOff, kind) {
				types = append(types, kind)
			}
		case "data_source":
			if version != "2022-06-28" && (kind == eventWebhookSourceContentUpdated || kind == eventWebhookSchemaUpdated) {
				types = append(types, kind)
			}
		}
	}
	return types
}

func internalConnectionWebhookTypes(input *[]string) ([]string, bool) {
	if input == nil || len(*input) == 0 || len(*input) > len(notionWebhookEventTypes) {
		return nil, false
	}
	types := []string{}
	for _, kind := range notionWebhookEventTypes {
		if slices.Contains(*input, kind) {
			types = append(types, kind)
		}
	}
	for _, kind := range *input {
		if !slices.Contains(notionWebhookEventTypes, kind) {
			return nil, false
		}
	}
	return types, true
}

// queueNotionWebhookVerification sends a subscription its verification
// token, as Notion's first request is, once (eventWebhookDeliveryRetries).
func queueNotionWebhookVerification(app core.App, subscriptionID, workspaceID, token string, now time.Time) error {
	verification := storedEventWebhookEvent{
		ID: eventWebhookID(), WorkspaceID: workspaceID, Type: notionWebhookVerify,
		EntityID: subscriptionID, EntityType: "webhook", AuthorsJSON: "[]", DataJSON: "{}", OccurredAt: now.UnixMilli(),
	}
	result, err := app.DB().Insert("leafen_webhook_events", dbx.Params{
		"id": verification.ID, "workspace_id": workspaceID, "type": notionWebhookVerify,
		"entity_id": subscriptionID, "entity_type": "webhook", "page_id": "", "authors_json": "[]", "data_json": "{}",
		"occurred_at": verification.OccurredAt, "dispatch_after": verification.OccurredAt, "dispatched": 1,
	}).Execute()
	if err != nil {
		return err
	}
	if verification.Sequence, err = result.LastInsertId(); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"verification_token": token})
	return insertEventWebhookDelivery(app, subscriptionID, verification, body, now)
}

// newNotionWebhookToken is a verification token and its stored cipher.
func newNotionWebhookToken(app core.App) (string, string, error) {
	token, err := newAuthProviderToken("secret_", 32)
	if err != nil {
		return "", "", err
	}
	cipher, err := encryptAdministratorSetting(app, cipherWebhook, token)
	return token, cipher, err
}

func createInternalConnectionWebhook(event *core.RequestEvent) error {
	connection, err := managedInternalConnection(event)
	if err != nil {
		return internalConnectionError(event, err, "webhook-create")
	}
	input := internalConnectionWebhookInput{}
	if err := event.BindBody(&input); err != nil {
		return internalConnectionError(event, bodyRefusal(errInternalConnectionInvalid, "webhook subscription", err), "webhook-create-bind")
	}
	if input.URL == nil {
		return internalConnectionError(event, refuse(errInternalConnectionInvalid, "url is the public https address events are sent to"), "webhook-create-bind")
	}
	address, err := normalizeEventWebhookURL(*input.URL)
	if err != nil {
		return eventWebhookAddressError(event, err)
	}
	version := notionWebhookAPIVersions[len(notionWebhookAPIVersions)-1]
	if input.APIVersion != nil {
		version = *input.APIVersion
	}
	types, valid := internalConnectionWebhookTypes(input.EventTypes)
	if !valid {
		return internalConnectionError(event, errInternalConnectionWebhookTypes, "webhook-create-validate")
	}
	if !slices.Contains(notionWebhookAPIVersions, version) {
		return internalConnectionError(event, internalConnectionWebhookVersionRefused(version), "webhook-create-validate")
	}
	grant, found, err := findNotionOAuthGrantByID(event.App, connection.GrantID)
	if err != nil || !found {
		return internalConnectionError(event, errInternalConnectionNotFound, "webhook-create-grant")
	}
	typesJSON, _ := json.Marshal(types)
	now, id := time.Now().UTC(), "nw"+eventWebhookID()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		count := 0
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_webhook_subscriptions}} WHERE [[connection_id]] = {:connection}`).
			Bind(dbx.Params{"connection": internalConnectionWebhookConnectionID(connection)}).Row(&count); err != nil {
			return err
		}
		if count >= internalConnectionWebhooksMax {
			return errInternalConnectionLimit
		}
		token, cipher, err := newNotionWebhookToken(txApp)
		if err != nil {
			return err
		}
		if _, err := txApp.DB().Insert("leafen_webhook_subscriptions", dbx.Params{
			"id": id, "workspace_id": connection.WorkspaceID, "owner_id": grant.OwnerID,
			"connection_id": internalConnectionWebhookConnectionID(connection), "request_id": internalConnectionWebhookRequest + id,
			"url": address, "secret_cipher": cipher, "event_types_json": string(typesJSON), "description": connection.Name,
			"api_version": version, "active": 1, "created": now.UnixMilli(), "updated": now.UnixMilli(),
			"kind": notionWebhookKind, "verify_state": internalConnectionWebhookPending,
		}).Execute(); err != nil {
			return err
		}
		return queueNotionWebhookVerification(txApp, id, connection.WorkspaceID, token, now)
	})
	if err != nil {
		return internalConnectionWebhookError(event, err, "webhook-create")
	}
	notifyEventWebhookRunnerAfterCommit(event.App)
	created := storedEventWebhookSubscription{}
	if err := event.App.DB().NewQuery(`SELECT ` + eventWebhookSubscriptionColumns + ` FROM {{leafen_webhook_subscriptions}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": id}).One(&created); err != nil {
		return internalConnectionError(event, err, "webhook-create-read")
	}
	return event.JSON(http.StatusCreated, internalConnectionWebhookOf(event.App, created))
}

// managedInternalConnectionWebhook is the subscription the request names,
// of a connection its person manages.
func managedInternalConnectionWebhook(event *core.RequestEvent) (storedInternalConnection, storedEventWebhookSubscription, error) {
	connection, err := managedInternalConnection(event)
	if err != nil {
		return connection, storedEventWebhookSubscription{}, err
	}
	stored := storedEventWebhookSubscription{}
	err = event.App.DB().NewQuery(`SELECT ` + eventWebhookSubscriptionColumns + ` FROM {{leafen_webhook_subscriptions}}
		WHERE [[id]] = {:id} AND [[connection_id]] = {:connection} AND [[request_id]] LIKE {:request}`).Bind(dbx.Params{
		"id": strings.TrimSpace(event.Request.PathValue("webhookId")), "connection": internalConnectionWebhookConnectionID(connection),
		"request": internalConnectionWebhookRequest + "%",
	}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		err = errInternalConnectionNotFound
	}
	return connection, stored, err
}

// errInternalConnectionWebhookTypes is a subscription's event list that
// names no event or one Leafen doesn't send.
var errInternalConnectionWebhookTypes = refuse(errInternalConnectionInvalid, "eventTypes names one or more event types the webhooks send, such as page.created")

// internalConnectionWebhookVersionRefused is an API version the webhooks
// don't speak.
func internalConnectionWebhookVersionRefused(version string) error {
	return refuse(errInternalConnectionInvalid, "apiVersion is one of %s, not %q", strings.Join(notionWebhookAPIVersions, ", "), version)
}

func internalConnectionWebhookError(event *core.RequestEvent, err error, stage string) error {
	switch {
	case errors.Is(err, errInternalConnectionWebhookToken):
		return writeAPIError(event, http.StatusBadRequest, errorCodeInternalConnectionWebhookToken,
			"That isn't this subscription's verification token.", "internal_connection", stage, errorCodeInternalConnectionWebhookToken, err,
			diagnosticSourcePosition(1))
	case errors.Is(err, errInternalConnectionWebhookURL):
		return writeAPIError(event, http.StatusConflict, errorCodeInternalConnectionWebhookURL,
			"A verified subscription keeps its address; make another for a new one.", "internal_connection", stage,
			errorCodeInternalConnectionWebhookURL, err, diagnosticSourcePosition(1))
	}
	return internalConnectionError(event, err, stage)
}

// updateInternalConnectionWebhook changes the events a subscription hears,
// or, before it is verified, its address or version — a new address is sent
// a new token.
func updateInternalConnectionWebhook(event *core.RequestEvent) error {
	connection, stored, err := managedInternalConnectionWebhook(event)
	if err != nil {
		return internalConnectionWebhookError(event, err, "webhook-update")
	}
	input := internalConnectionWebhookInput{}
	if err := event.BindBody(&input); err != nil {
		return internalConnectionWebhookError(event, bodyRefusal(errInternalConnectionInvalid, "webhook subscription change", err), "webhook-update-bind")
	}
	pending := stored.VerifyState == internalConnectionWebhookPending
	changes := dbx.Params{"updated": time.Now().UTC().UnixMilli()}
	if input.EventTypes != nil {
		types, valid := internalConnectionWebhookTypes(input.EventTypes)
		if !valid {
			return internalConnectionWebhookError(event, errInternalConnectionWebhookTypes, "webhook-update-types")
		}
		typesJSON, _ := json.Marshal(types)
		changes["event_types_json"] = string(typesJSON)
	}
	if input.APIVersion != nil {
		if !slices.Contains(notionWebhookAPIVersions, *input.APIVersion) {
			return internalConnectionWebhookError(event, internalConnectionWebhookVersionRefused(*input.APIVersion), "webhook-update-version")
		}
		changes["api_version"] = *input.APIVersion
	}
	newAddress := ""
	if input.URL != nil {
		address, err := normalizeEventWebhookURL(*input.URL)
		if err != nil {
			return eventWebhookAddressError(event, err)
		}
		if address != stored.URL {
			if !pending {
				return internalConnectionWebhookError(event, errInternalConnectionWebhookURL, "webhook-update-url")
			}
			newAddress = address
			changes["url"] = address
		}
	}
	now := time.Now().UTC()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		token := ""
		if newAddress != "" {
			var cipher string
			var err error
			if token, cipher, err = newNotionWebhookToken(txApp); err != nil {
				return err
			}
			changes["secret_cipher"] = cipher
		}
		if _, err := txApp.DB().Update("leafen_webhook_subscriptions", changes, dbx.HashExp{"id": stored.ID}).Execute(); err != nil {
			return err
		}
		if token == "" {
			return nil
		}
		return queueNotionWebhookVerification(txApp, stored.ID, connection.WorkspaceID, token, now)
	})
	if err != nil {
		return internalConnectionWebhookError(event, err, "webhook-update")
	}
	if newAddress != "" {
		notifyEventWebhookRunnerAfterCommit(event.App)
	}
	_, stored, err = managedInternalConnectionWebhook(event)
	if err != nil {
		return internalConnectionWebhookError(event, err, "webhook-update-read")
	}
	return event.JSON(http.StatusOK, internalConnectionWebhookOf(event.App, stored))
}

func deleteInternalConnectionWebhook(event *core.RequestEvent) error {
	_, stored, err := managedInternalConnectionWebhook(event)
	if err != nil {
		return internalConnectionWebhookError(event, err, "webhook-delete")
	}
	if _, err := event.App.DB().NewQuery(`DELETE FROM {{leafen_webhook_subscriptions}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": stored.ID}).Execute(); err != nil {
		return internalConnectionWebhookError(event, err, "webhook-delete")
	}
	return event.NoContent(http.StatusNoContent)
}

// verifyInternalConnectionWebhook takes back the token the subscription was
// sent, as Notion's "구독 확인"; from then on it hears its events.
func verifyInternalConnectionWebhook(event *core.RequestEvent) error {
	_, stored, err := managedInternalConnectionWebhook(event)
	if err != nil {
		return internalConnectionWebhookError(event, err, "webhook-verify")
	}
	input := struct {
		Token string `json:"token"`
	}{}
	if err := event.BindBody(&input); err != nil {
		return internalConnectionWebhookError(event, bodyRefusal(errInternalConnectionInvalid, "verification token", err), "webhook-verify-bind")
	}
	if stored.VerifyState == internalConnectionWebhookPending {
		token, err := decryptAdministratorSetting(event.App, stored.SecretCipher)
		if err != nil {
			return internalConnectionWebhookError(event, err, "webhook-verify-secret")
		}
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(input.Token)), []byte(token)) != 1 {
			return internalConnectionWebhookError(event, errInternalConnectionWebhookToken, "webhook-verify")
		}
		if _, err := event.App.DB().NewQuery(`UPDATE {{leafen_webhook_subscriptions}} SET [[verify_state]] = '', [[updated]] = {:now}
			WHERE [[id]] = {:id}`).Bind(dbx.Params{"now": time.Now().UTC().UnixMilli(), "id": stored.ID}).Execute(); err != nil {
			return internalConnectionWebhookError(event, err, "webhook-verify")
		}
		stored.VerifyState = ""
	}
	return event.JSON(http.StatusOK, internalConnectionWebhookOf(event.App, stored))
}
