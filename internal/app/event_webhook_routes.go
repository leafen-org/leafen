package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's members subscribe to its events. Each sees and changes the
// subscriptions they made; the workspace's owners see and change all. A
// subscription made with a key stays within that key.

func registerEventWebhookRoutes(event *core.ServeEvent) {
	limit := apis.BodyLimit(16 << 10)
	event.Router.GET(apiRoot+eventWebhookListPath, listEventWebhooks)
	event.Router.POST(apiRoot+eventWebhookCreatePath, createEventWebhook).Bind(limit)
	event.Router.PATCH(apiRoot+eventWebhookUpdatePath, updateEventWebhook).Bind(limit)
	event.Router.DELETE(apiRoot+eventWebhookDeletePath, deleteEventWebhook)
	event.Router.GET(apiRoot+eventWebhookDeliveriesPath, listEventWebhookDeliveries)
	event.Router.POST(apiRoot+eventWebhookTestPath, testEventWebhook).Bind(limit)
	event.Router.POST(apiRoot+eventWebhookRetryPath, retryEventWebhookDelivery).Bind(limit)
}

// normalizeEventWebhookURL accepts a public http(s) receiver, or the one
// loopback receiver a development server allows.
func normalizeEventWebhookURL(raw string) (string, error) {
	if loopback := eventWebhookAllowedLoopback(); loopback != "" {
		if parsed, err := url.Parse(strings.TrimSpace(raw)); err == nil && parsed.Scheme == "http" &&
			parsed.Host == loopback && parsed.User == nil && parsed.Fragment == "" {
			return parsed.String(), nil
		}
	}
	return normalizeOutboundPublicURL(raw, eventWebhookMaxURLRunes, false)
}

// eventWebhookAddressError says what is wrong with a webhook's address, the
// usual mistake: one the server can't send to from outside (this machine, a
// private network), or no web address at all.
func eventWebhookAddressError(event *core.RequestEvent, err error) error {
	if errors.Is(err, errOutboundUnsafeDestination) {
		return apiError(event, http.StatusBadRequest, errorCodeEventWebhookAddressNotPublic,
			refusalMessage("Give an address reachable from the internet, not this machine or a private network.", refusalReason(err)))
	}
	return apiError(event, http.StatusBadRequest, errorCodeEventWebhookAddressInvalid,
		refusalMessage("Give a web address starting with https:// or http://.", refusalReason(err)))
}

func eventWebhookError(event *core.RequestEvent, err error) error {
	switch {
	case isPermissionDenied(err), errors.Is(err, sql.ErrNoRows):
		return apiError(event, http.StatusNotFound, errorCodeEventWebhookNotFound, "Webhook not found.")
	default:
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeEventWebhookFailed,
			"Couldn't complete the webhook request.", "event_webhook", "request", errorCodeEventWebhookFailed, err)
	}
}

func authorizeEventWebhookWorkspace(event *core.RequestEvent) (workspaceAuthorization, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return workspaceAuthorization{}, false
	}
	workspace, err := authorizeWorkspace(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")), permissionWorkspaceRead)
	if err != nil {
		_ = eventWebhookError(event, err)
		return workspaceAuthorization{}, false
	}
	return workspace, true
}

// loadManagedEventWebhook reads a subscription the caller may change.
func loadManagedEventWebhook(event *core.RequestEvent, workspace workspaceAuthorization) (storedEventWebhookSubscription, error) {
	subscription := storedEventWebhookSubscription{}
	err := event.App.DB().NewQuery(`SELECT ` + eventWebhookSubscriptionColumns + `
		FROM {{leafen_webhook_subscriptions}} WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspace} AND [[kind]] = 'leafen'`).
		Bind(dbx.Params{"id": strings.TrimSpace(event.Request.PathValue("webhookId")), "workspace": workspace.WorkspaceID}).
		One(&subscription)
	if err != nil {
		return subscription, err
	}
	if subscription.OwnerID != workspace.PrincipalID && workspace.Role != workspaceRoleOwner {
		return subscription, sql.ErrNoRows
	}
	return subscription, nil
}

func eventWebhookTypesValid(types []string) ([]string, bool) {
	if len(types) == 0 || len(types) > len(eventWebhookTypes) {
		return nil, false
	}
	seen := map[string]bool{}
	for _, value := range types {
		if !containsString(eventWebhookTypes, value) || seen[value] {
			return nil, false
		}
		seen[value] = true
	}
	sorted := append([]string(nil), types...)
	sort.Strings(sorted)
	return sorted, true
}

func eventWebhookWireFor(app core.App, subscription storedEventWebhookSubscription) eventWebhookWire {
	types := []string{}
	_ = json.Unmarshal([]byte(subscription.EventTypesJSON), &types)
	wire := eventWebhookWire{
		ID: subscription.ID, WorkspaceID: subscription.WorkspaceID, URL: subscription.URL, EventTypes: types,
		Description: subscription.Description, APIVersion: subscription.APIVersion, Active: subscription.Active,
		OwnerID: subscription.OwnerID, CreatedAt: subscription.Created, UpdatedAt: subscription.Updated,
	}
	if subscription.ConnectionID != "" {
		_ = app.DB().NewQuery(`SELECT [[client_name]] FROM {{leafen_mcp_connections}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": subscription.ConnectionID}).Row(&wire.ConnectionName)
	}
	last := struct {
		State      string `db:"state"`
		StatusCode int    `db:"status_code"`
		At         int64  `db:"at"`
	}{}
	if app.DB().NewQuery(`SELECT [[state]], [[status_code]], MAX([[settled_at]], [[created]]) AS [[at]]
		FROM {{leafen_webhook_deliveries}} WHERE [[subscription_id]] = {:id}
		ORDER BY [[sequence]] DESC LIMIT 1`).Bind(dbx.Params{"id": subscription.ID}).One(&last) == nil {
		wire.LastDelivery = &eventWebhookLastDelivery{State: last.State, StatusCode: last.StatusCode, At: last.At}
	}
	return wire
}

func listEventWebhooks(event *core.RequestEvent) error {
	workspace, ok := authorizeEventWebhookWorkspace(event)
	if !ok {
		return nil
	}
	rows := []storedEventWebhookSubscription{}
	if err := event.App.DB().NewQuery(`SELECT ` + eventWebhookSubscriptionColumns + `
		FROM {{leafen_webhook_subscriptions}} WHERE [[workspace_id]] = {:workspace} AND [[kind]] = 'leafen'
			AND ({:all} OR [[owner_id]] = {:owner}) ORDER BY [[created]], [[id]]`).Bind(dbx.Params{
		"workspace": workspace.WorkspaceID, "owner": workspace.PrincipalID, "all": workspace.Role == workspaceRoleOwner,
	}).All(&rows); err != nil {
		return eventWebhookError(event, err)
	}
	response := eventWebhookListResponse{Webhooks: []eventWebhookWire{}}
	for _, row := range rows {
		response.Webhooks = append(response.Webhooks, eventWebhookWireFor(event.App, row))
	}
	return event.JSON(http.StatusOK, response)
}

func createEventWebhook(event *core.RequestEvent) error {
	workspace, ok := authorizeEventWebhookWorkspace(event)
	if !ok {
		return nil
	}
	input := eventWebhookCreateRequest{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeEventWebhookInvalid, "Check the webhook.")
	}
	input.RequestID, input.Description = strings.TrimSpace(input.RequestID), strings.TrimSpace(input.Description)
	target, urlErr := normalizeEventWebhookURL(input.URL)
	if urlErr != nil {
		return eventWebhookAddressError(event, urlErr)
	}
	types, typesOK := eventWebhookTypesValid(input.EventTypes)
	if !typesOK || input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > 255 ||
		utf8.RuneCountInString(input.Description) > eventWebhookMaxDescriptionRunes {
		return apiError(event, http.StatusBadRequest, errorCodeEventWebhookInvalid,
			"Choose at least one event type, and keep the description short.")
	}
	connectionID := ""
	if key, keyed := userKeyOf(event); keyed {
		connectionID = key.ConnectionID
	}
	response := eventWebhookCreateResponse{}
	limited := false
	err := event.App.RunInTransaction(func(txApp core.App) error {
		existing := storedEventWebhookSubscription{}
		err := txApp.DB().NewQuery(`SELECT ` + eventWebhookSubscriptionColumns + `
			FROM {{leafen_webhook_subscriptions}} WHERE [[owner_id]] = {:owner} AND [[request_id]] = {:request}`).
			Bind(dbx.Params{"owner": workspace.PrincipalID, "request": input.RequestID}).One(&existing)
		if err == nil {
			if existing.WorkspaceID != workspace.WorkspaceID {
				return sql.ErrNoRows
			}
			secret, err := decryptAdministratorSetting(txApp, existing.SecretCipher)
			if err != nil {
				return err
			}
			response = eventWebhookCreateResponse{Webhook: eventWebhookWireFor(txApp, existing), Secret: secret}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var count int
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_webhook_subscriptions}} WHERE [[workspace_id]] = {:workspace} AND [[kind]] = 'leafen'`).
			Bind(dbx.Params{"workspace": workspace.WorkspaceID}).Row(&count); err != nil {
			return err
		}
		if count >= eventWebhookMaxPerWorkspace {
			limited = true
			return nil
		}
		secret, err := newAuthProviderToken("whsec_", 32)
		if err != nil {
			return err
		}
		cipher, err := encryptAdministratorSetting(txApp, cipherWebhook, secret)
		if err != nil {
			return err
		}
		typesJSON, _ := json.Marshal(types)
		now := time.Now().UTC().UnixMilli()
		subscription := storedEventWebhookSubscription{
			ID: eventWebhookID(), WorkspaceID: workspace.WorkspaceID, OwnerID: workspace.PrincipalID,
			ConnectionID: connectionID, RequestID: input.RequestID, URL: target, SecretCipher: cipher,
			EventTypesJSON: string(typesJSON), Description: input.Description, APIVersion: eventWebhookAPIVersion,
			Active: true, Created: now, Updated: now,
		}
		if _, err := txApp.DB().Insert("leafen_webhook_subscriptions", dbx.Params{
			"id": subscription.ID, "workspace_id": subscription.WorkspaceID, "owner_id": subscription.OwnerID,
			"connection_id": subscription.ConnectionID, "request_id": subscription.RequestID, "url": subscription.URL,
			"secret_cipher": cipher, "event_types_json": subscription.EventTypesJSON,
			"description": subscription.Description, "api_version": subscription.APIVersion, "active": 1,
			"created": now, "updated": now,
		}).Execute(); err != nil {
			return err
		}
		response = eventWebhookCreateResponse{Webhook: eventWebhookWireFor(txApp, subscription), Secret: secret}
		return nil
	})
	if err != nil {
		return eventWebhookError(event, err)
	}
	if limited {
		return apiError(event, http.StatusConflict, errorCodeEventWebhookLimit, "This workspace has as many webhooks as it can.")
	}
	return event.JSON(http.StatusCreated, response)
}

func updateEventWebhook(event *core.RequestEvent) error {
	workspace, ok := authorizeEventWebhookWorkspace(event)
	if !ok {
		return nil
	}
	subscription, err := loadManagedEventWebhook(event, workspace)
	if err != nil {
		return eventWebhookError(event, err)
	}
	input := eventWebhookUpdateRequest{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeEventWebhookInvalid, "Check the webhook.")
	}
	if input.URL != nil {
		target, err := normalizeEventWebhookURL(*input.URL)
		if err != nil {
			return eventWebhookAddressError(event, err)
		}
		subscription.URL = target
	}
	if input.EventTypes != nil {
		types, valid := eventWebhookTypesValid(input.EventTypes)
		if !valid {
			return apiError(event, http.StatusBadRequest, errorCodeEventWebhookInvalid, "Choose at least one known event type.")
		}
		encoded, _ := json.Marshal(types)
		subscription.EventTypesJSON = string(encoded)
	}
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		if utf8.RuneCountInString(description) > eventWebhookMaxDescriptionRunes {
			return apiError(event, http.StatusBadRequest, errorCodeEventWebhookInvalid, "Shorten the description.")
		}
		subscription.Description = description
	}
	if input.Active != nil {
		subscription.Active = *input.Active
	}
	subscription.Updated = time.Now().UTC().UnixMilli()
	active := 0
	if subscription.Active {
		active = 1
	}
	if _, err := event.App.DB().NewQuery(`UPDATE {{leafen_webhook_subscriptions}} SET [[url]] = {:url},
		[[event_types_json]] = {:types}, [[description]] = {:description}, [[active]] = {:active}, [[updated]] = {:updated}
		WHERE [[id]] = {:id}`).Bind(dbx.Params{
		"url": subscription.URL, "types": subscription.EventTypesJSON, "description": subscription.Description,
		"active": active, "updated": subscription.Updated, "id": subscription.ID,
	}).Execute(); err != nil {
		return eventWebhookError(event, err)
	}
	notifyEventWebhookRunnerAfterCommit(event.App)
	return event.JSON(http.StatusOK, eventWebhookWireFor(event.App, subscription))
}

func deleteEventWebhook(event *core.RequestEvent) error {
	workspace, ok := authorizeEventWebhookWorkspace(event)
	if !ok {
		return nil
	}
	subscription, err := loadManagedEventWebhook(event, workspace)
	if err != nil {
		return eventWebhookError(event, err)
	}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_webhook_deliveries}} WHERE [[subscription_id]] = {:id}`).
			Bind(dbx.Params{"id": subscription.ID}).Execute(); err != nil {
			return err
		}
		_, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_webhook_subscriptions}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": subscription.ID}).Execute()
		return err
	})
	if err != nil {
		return eventWebhookError(event, err)
	}
	return event.NoContent(http.StatusNoContent)
}

func listEventWebhookDeliveries(event *core.RequestEvent) error {
	workspace, ok := authorizeEventWebhookWorkspace(event)
	if !ok {
		return nil
	}
	subscription, err := loadManagedEventWebhook(event, workspace)
	if err != nil {
		return eventWebhookError(event, err)
	}
	rows := []storedEventWebhookDelivery{}
	if err := event.App.DB().NewQuery(`SELECT * FROM {{leafen_webhook_deliveries}} WHERE [[subscription_id]] = {:id}
		ORDER BY [[sequence]] DESC LIMIT {:limit}`).Bind(dbx.Params{"id": subscription.ID, "limit": eventWebhookDeliveryPageSize}).
		All(&rows); err != nil {
		return eventWebhookError(event, err)
	}
	response := eventWebhookDeliveryListResponse{Deliveries: []eventWebhookDeliveryWire{}}
	for _, row := range rows {
		response.Deliveries = append(response.Deliveries, eventWebhookDeliveryWire{
			ID: row.ID, EventID: row.EventID, EventType: row.EventType, EntityID: row.EntityID, State: row.State,
			Attempts: row.Attempts, StatusCode: row.StatusCode, ErrorCode: row.ErrorCode, CreatedAt: row.Created,
			NextAttemptAt: row.NextAttemptAt, SettledAt: row.SettledAt,
		})
	}
	return event.JSON(http.StatusOK, response)
}

// testEventWebhook queues a "webhook.test" event for this subscription
// alone, after what it already waits for.
func testEventWebhook(event *core.RequestEvent) error {
	workspace, ok := authorizeEventWebhookWorkspace(event)
	if !ok {
		return nil
	}
	subscription, err := loadManagedEventWebhook(event, workspace)
	if err != nil {
		return eventWebhookError(event, err)
	}
	now := time.Now().UTC()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		stored := storedEventWebhookEvent{
			ID: eventWebhookID(), WorkspaceID: workspace.WorkspaceID, Type: eventWebhookTest,
			EntityID: subscription.ID, EntityType: "webhook", AuthorsJSON: "[]", DataJSON: "{}", OccurredAt: now.UnixMilli(),
		}
		authors, _ := json.Marshal([]eventWebhookAuthor{{ID: workspace.PrincipalID, Type: "person"}})
		stored.AuthorsJSON = string(authors)
		result, err := txApp.DB().Insert("leafen_webhook_events", dbx.Params{
			"id": stored.ID, "workspace_id": stored.WorkspaceID, "type": stored.Type, "entity_id": stored.EntityID,
			"entity_type": stored.EntityType, "page_id": "", "authors_json": stored.AuthorsJSON, "data_json": stored.DataJSON,
			"occurred_at": stored.OccurredAt, "dispatch_after": stored.OccurredAt, "dispatched": 1,
		}).Execute()
		if err != nil {
			return err
		}
		if stored.Sequence, err = result.LastInsertId(); err != nil {
			return err
		}
		body, err := eventWebhookBody(subscription, stored)
		if err != nil {
			return err
		}
		return insertEventWebhookDelivery(txApp, subscription.ID, stored, body, now)
	})
	if err != nil {
		return eventWebhookError(event, err)
	}
	notifyEventWebhookRunnerAfterCommit(event.App)
	return event.NoContent(http.StatusAccepted)
}

// retryEventWebhookDelivery sends a failed delivery again, in its place.
func retryEventWebhookDelivery(event *core.RequestEvent) error {
	workspace, ok := authorizeEventWebhookWorkspace(event)
	if !ok {
		return nil
	}
	subscription, err := loadManagedEventWebhook(event, workspace)
	if err != nil {
		return eventWebhookError(event, err)
	}
	result, err := event.App.DB().NewQuery(`UPDATE {{leafen_webhook_deliveries}}
		SET [[state]] = {:pending}, [[attempts]] = 0, [[settled_at]] = 0, [[next_attempt_at]] = {:now}, [[error_code]] = ''
		WHERE [[id]] = {:id} AND [[subscription_id]] = {:subscription} AND [[state]] = {:failed}`).Bind(dbx.Params{
		"pending": eventWebhookDeliveryStatePending, "now": time.Now().UTC().UnixMilli(),
		"id": strings.TrimSpace(event.Request.PathValue("deliveryId")), "subscription": subscription.ID,
		"failed": eventWebhookDeliveryFailed,
	}).Execute()
	if err != nil {
		return eventWebhookError(event, err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return apiError(event, http.StatusNotFound, errorCodeEventWebhookNotFound, "No failed delivery to retry.")
	}
	notifyEventWebhookRunnerAfterCommit(event.App)
	return event.NoContent(http.StatusAccepted)
}
