package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's integration webhooks on the compatible surface. A configured
// client that names a webhook address has, for each connection made with
// it, a subscription that hears — in Notion's format, signed with
// X-Notion-Signature — of changes to the pages shared with that
// connection. As Notion does when a subscription is made, the first thing
// it sends is a verification request holding the token the rest are
// signed with; the client's configuration stands for Notion's "Verify".

const (
	notionWebhookKind       = "notion"
	notionWebhookAPIVersion = "2025-09-03"
	notionWebhookSignature  = "X-Notion-Signature"
	notionWebhookVerify     = "verification"
)

// syncNotionWebhookSubscriptions keeps one Notion subscription for each
// active connection whose client names a webhook address, and stops those
// whose connection or address went away.
func syncNotionWebhookSubscriptions(app core.App, now time.Time) error {
	clients, err := configuredNotionOAuthClients()
	if err != nil {
		return nil
	}
	wanted := map[string]notionOAuthClient{}
	for _, client := range clients {
		if client.WebhookURL == "" {
			continue
		}
		if _, err := normalizeEventWebhookURL(client.WebhookURL); err != nil {
			continue
		}
		wanted[client.ID] = client
	}
	grants := []storedNotionOAuthGrant{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[owner_id]], [[client_id]], [[client_name]], [[workspace_id]], [[scope]],
			[[credential_generation]], [[status]], [[revoked_at]]
		FROM {{leafen_notion_oauth_grants}} WHERE [[status]] = 'active'`).All(&grants); err != nil {
		return err
	}
	active := map[string]bool{}
	for _, grant := range grants {
		client, configured := wanted[grant.ClientID]
		if !configured {
			continue
		}
		id := "notion_" + grant.ID
		active[id] = true
		types := client.WebhookEvents
		if len(types) == 0 {
			types = eventWebhookTypes
		}
		typesJSON, _ := json.Marshal(types)
		existing := storedEventWebhookSubscription{}
		err := app.DB().NewQuery(`SELECT ` + eventWebhookSubscriptionColumns + `
			FROM {{leafen_webhook_subscriptions}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": id}).One(&existing)
		if err == nil && existing.URL == client.WebhookURL && existing.EventTypesJSON == string(typesJSON) && existing.Active {
			continue
		}
		if err := app.RunInTransaction(func(txApp core.App) error {
			if err == nil && existing.URL == client.WebhookURL {
				_, err := txApp.DB().NewQuery(`UPDATE {{leafen_webhook_subscriptions}} SET [[event_types_json]] = {:types},
					[[active]] = 1, [[updated]] = {:now} WHERE [[id]] = {:id}`).
					Bind(dbx.Params{"types": string(typesJSON), "now": now.UnixMilli(), "id": id}).Execute()
				return err
			}
			// A new subscription, or one whose address changed, verifies
			// itself anew with a new token.
			token, err := newAuthProviderToken("secret_", 32)
			if err != nil {
				return err
			}
			cipher, err := encryptAdministratorSetting(txApp, cipherWebhook, token)
			if err != nil {
				return err
			}
			if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_webhook_subscriptions}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": id}).Execute(); err != nil {
				return err
			}
			if _, err := txApp.DB().Insert("leafen_webhook_subscriptions", dbx.Params{
				"id": id, "workspace_id": grant.WorkspaceID, "owner_id": grant.OwnerID,
				"connection_id": "notion:" + grant.ID, "request_id": "notion:" + grant.ID, "url": client.WebhookURL,
				"secret_cipher": cipher, "event_types_json": string(typesJSON), "description": client.Name,
				"api_version": notionWebhookAPIVersion, "active": 1, "created": now.UnixMilli(), "updated": now.UnixMilli(),
				"kind": notionWebhookKind,
			}).Execute(); err != nil {
				return err
			}
			verification := storedEventWebhookEvent{
				ID: eventWebhookID(), WorkspaceID: grant.WorkspaceID, Type: notionWebhookVerify,
				EntityID: id, EntityType: "webhook", AuthorsJSON: "[]", DataJSON: "{}", OccurredAt: now.UnixMilli(),
			}
			result, err := txApp.DB().Insert("leafen_webhook_events", dbx.Params{
				"id": verification.ID, "workspace_id": grant.WorkspaceID, "type": notionWebhookVerify,
				"entity_id": id, "entity_type": "webhook", "page_id": "", "authors_json": "[]", "data_json": "{}",
				"occurred_at": verification.OccurredAt, "dispatch_after": verification.OccurredAt, "dispatched": 1,
			}).Execute()
			if err != nil {
				return err
			}
			if verification.Sequence, err = result.LastInsertId(); err != nil {
				return err
			}
			body, _ := json.Marshal(map[string]string{"verification_token": token})
			return insertEventWebhookDelivery(txApp, id, verification, body, now)
		}); err != nil {
			return err
		}
	}
	// Only the subscriptions a client's configuration made; an internal
	// connection's own are made on its page.
	rows := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_webhook_subscriptions}} WHERE [[kind]] = {:kind} AND [[active]] = 1
		AND [[request_id]] LIKE 'notion:%'`).
		Bind(dbx.Params{"kind": notionWebhookKind}).Column(&rows); err != nil {
		return err
	}
	for _, id := range rows {
		if !active[id] {
			if _, err := app.DB().NewQuery(`UPDATE {{leafen_webhook_subscriptions}} SET [[active]] = 0, [[updated]] = {:now} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"now": now.UnixMilli(), "id": id}).Execute(); err != nil {
				return err
			}
		}
	}
	return nil
}

// notionWebhookGrantMaySee reports whether a Notion subscription's
// connection may still read a page: the connection is active, its person
// can read the page (while an operation holds it too, page_access_hold.go),
// and the page is shared with the connection.
func notionWebhookGrantMaySee(app core.App, subscription storedEventWebhookSubscription, pageID string) (storedNotionOAuthGrant, bool) {
	grant, found, err := findNotionOAuthGrantByID(app, strings.TrimPrefix(subscription.ConnectionID, "notion:"))
	if err != nil || !found || grant.Status != "active" {
		return grant, false
	}
	if pageID == "" {
		return grant, true
	}
	authorization, err := pageAccessDespiteHold(authorizePage(app, grant.OwnerID, pageID, permissionWorkspaceRead))
	if err != nil {
		return grant, false
	}
	resources, err := loadNotionOAuthGrantResources(app, grant.ID)
	if err != nil {
		return grant, false
	}
	identity := notionOAuthBearerIdentity{
		PrincipalID: grant.OwnerID, ClientID: grant.ClientID, GrantID: grant.ID, WorkspaceID: grant.WorkspaceID,
		Scopes: strings.Fields(grant.Scope), ResourceIDs: resources, PageScoped: true,
	}
	return grant, notionOAuthIdentityAllowsPage(app, identity, authorization)
}

// notionWebhookUUID turns an internal id into a stable UUID for an event's
// or subscription's id.
func notionWebhookUUID(value string) string {
	digest := sha256.Sum256([]byte("leafen:notion-webhook:v1\x00" + value))
	digest[6] = digest[6]&0x0f | 0x40
	digest[8] = digest[8]&0x3f | 0x80
	encoded := hex.EncodeToString(digest[:16])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

// notionWebhookBody is an event as Notion's webhook delivers it, with the
// compatible surface's ids; it leaves out what Notion wouldn't send.
func notionWebhookBody(app core.App, subscription storedEventWebhookSubscription, grant storedNotionOAuthGrant, event storedEventWebhookEvent) ([]byte, bool, error) {
	workspace, err := app.FindRecordById(workspacesCollection, event.WorkspaceID)
	if err != nil {
		return nil, false, err
	}
	workspaceID, err := notionCompatibleWorkspaceID(event.WorkspaceID)
	if err != nil {
		return nil, false, err
	}
	authors := []eventWebhookAuthor{}
	_ = json.Unmarshal([]byte(event.AuthorsJSON), &authors)
	notionAuthors := []map[string]any{}
	for _, author := range authors {
		if author.Connection != "" && author.Connection == grant.ClientName && author.ID == grant.OwnerID {
			notionAuthors = append(notionAuthors, map[string]any{"id": notionOAuthBotID(grant.ID), "type": "bot"})
			continue
		}
		if id, err := notionCompatibleUserID(author.ID); err == nil {
			notionAuthors = append(notionAuthors, map[string]any{"id": id, "type": "person"})
		}
	}
	data := map[string]any{}
	_ = json.Unmarshal([]byte(event.DataJSON), &data)
	version, integration := subscription.APIVersion, grant.ClientID
	if version == "" {
		version = notionWebhookAPIVersion
	}
	parent := func(raw any) map[string]any {
		value, _ := raw.(map[string]any)
		id, _ := value["id"].(string)
		switch value["type"] {
		case "page":
			if publicID, err := notionPagePublicID(app, id); err == nil {
				return map[string]any{"id": publicID, "type": "page"}
			}
		case "block":
			if publicID, err := notionBlockPublicID(app, event.WorkspaceID, id); err == nil {
				return map[string]any{"id": publicID, "type": "block"}
			}
		case "data_source":
			// A database page's parent is its database, named by its data source.
			value = map[string]any{"id": ownDatabaseContainer(app, id), "type": "database", "data_source": id}
			fallthrough
		case "database":
			// A database's data source is named from 2025-09-03, which has them.
			id, _ = value["id"].(string)
			if publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDatabase, event.WorkspaceID, id); id != "" && err == nil {
				named := map[string]any{"id": publicID, "type": "database"}
				if source, _ := value["data_source"].(string); source != "" && version != "2022-06-28" {
					if sourceID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDataSource, event.WorkspaceID, source); err == nil {
						named["data_source_id"] = sourceID
					}
				}
				return named
			}
		}
		return map[string]any{"id": workspaceID, "type": "space"}
	}
	entity := map[string]any{"id": event.EntityID, "type": event.EntityType}
	notionData := map[string]any{}
	identified := func(kind string) error {
		publicID, err := ensureNotionObjectIdentity(app, kind, event.WorkspaceID, event.EntityID)
		entity["id"] = publicID
		return err
	}
	switch event.EntityType {
	case "page":
		publicID, err := notionPagePublicID(app, event.EntityID)
		if err != nil {
			return nil, false, err
		}
		entity["id"] = publicID
		notionData["parent"] = parent(data["parent"])
	case "database":
		if err := identified(notionObjectIdentityDatabase); err != nil {
			return nil, false, err
		}
		notionData["parent"] = parent(data["parent"])
	case "block":
		// A linked database is its block, as Notion's.
		publicID, err := notionBlockPublicID(app, event.WorkspaceID, event.EntityID)
		if err != nil {
			return nil, false, err
		}
		entity["id"] = publicID
		notionData["parent"] = parent(data["parent"])
	case "view":
		if err := identified(notionObjectIdentityView); err != nil {
			return nil, false, err
		}
		notionData["parent"] = parent(data["parent"])
		if viewType, ok := data["view_type"]; ok {
			notionData["view_type"] = viewType
		}
		if fields, ok := data["updated_fields"]; ok {
			notionData["updated_fields"] = fields
		}
	case "file_upload":
		// An upload is heard of by the connection that made it.
		owner, _ := data["owner"].(string)
		maker, _ := data["connection"].(string)
		if owner != grant.OwnerID || maker != "notion:"+grant.ID || !notionFileUploadEventStands(app, event) {
			return nil, false, nil
		}
		if err := identified(notionObjectIdentityFileUpload); err != nil {
			return nil, false, err
		}
		if result, ok := data["file_import_result"]; ok {
			notionData["file_import_result"] = result
		}
	case "comment":
		// Comment events need the connection's comment reading, as Notion's.
		if !slices.Contains(strings.Fields(grant.Scope), "comments:read") {
			return nil, false, nil
		}
		publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityComment, event.WorkspaceID, event.EntityID)
		if err != nil {
			return nil, false, err
		}
		entity["id"] = publicID
		pageID, _ := data["page_id"].(string)
		if publicPage, err := notionPagePublicID(app, pageID); err == nil {
			notionData["page_id"] = publicPage
		}
		notionData["parent"] = parent(data["parent"])
	case "data_source":
		publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDataSource, event.WorkspaceID, event.EntityID)
		if err != nil {
			return nil, false, err
		}
		entity["id"] = publicID
		notionData["parent"] = parent(data["parent"])
	default:
		return nil, false, nil
	}
	if schema, ok := data["updated_schema"].([]any); ok {
		// Each property by its public ID, as the schema names it.
		ids := make([]string, 0, len(schema))
		for _, item := range schema {
			change, _ := item.(map[string]any)
			if id, _ := change["id"].(string); id != "" {
				ids = append(ids, id)
			}
		}
		public, err := notionPublicPropertyIDsOf(app, ids)
		if err != nil {
			return nil, false, err
		}
		updated := make([]any, 0, len(schema))
		for _, item := range schema {
			change, _ := item.(map[string]any)
			id, _ := change["id"].(string)
			updated = append(updated, map[string]any{"id": public[id], "name": change["name"], "action": change["action"]})
		}
		notionData["updated_properties"] = updated
	}
	if blocks, ok := data["updated_blocks"].([]any); ok {
		// A database's are its pages, named by their page IDs, as a Notion
		// page and its block share one.
		rows := event.EntityType == "data_source" || event.EntityType == "database"
		updated := []any{}
		for _, item := range blocks {
			block, _ := item.(map[string]any)
			id, _ := block["id"].(string)
			publicID, err := notionBlockPublicID(app, event.WorkspaceID, id)
			if rows {
				publicID, err = notionPagePublicID(app, id)
			}
			if err == nil {
				updated = append(updated, map[string]any{"id": publicID, "type": "block"})
			}
		}
		notionData["updated_blocks"] = updated
	}
	if fields, ok := data["updated_properties"].([]any); ok {
		// Notion names the properties that changed: a page's title, or a
		// database page's values by the IDs its properties answer with (its
		// title one of them), not the page's look.
		properties, changed := []any{}, []string{}
		values := slices.ContainsFunc(fields, func(item any) bool {
			name, _ := item.(string)
			return strings.HasPrefix(name, "values.")
		})
		for _, item := range fields {
			name, _ := item.(string)
			switch {
			case name == "title" && !values:
				properties = append(properties, "title")
			case strings.HasPrefix(name, "values."):
				changed = append(changed, strings.TrimPrefix(name, "values."))
			}
		}
		public, err := notionPublicPropertyIDsOf(app, changed)
		if err != nil {
			return nil, false, err
		}
		for _, id := range changed {
			properties = append(properties, public[id])
		}
		if len(properties) == 0 {
			return nil, false, nil
		}
		notionData["updated_properties"] = properties
	}
	payload := map[string]any{
		"id": notionWebhookUUID(event.ID), "timestamp": time.UnixMilli(event.OccurredAt).UTC().Format("2006-01-02T15:04:05.000Z"),
		"workspace_id": workspaceID, "workspace_name": workspace.GetString("name"),
		"subscription_id": notionWebhookUUID(subscription.ID), "integration_id": integration,
		"type": event.Type, "authors": notionAuthors,
		"attempt_number": 1, "api_version": version, "entity": entity, "data": notionData,
	}
	// An internal connection is named by a UUID, as Notion's integrations
	// are; who may read the entity is told to a public connection only.
	if strings.HasPrefix(integration, internalConnectionClientPrefix) {
		payload["integration_id"] = notionWebhookUUID(integration)
	} else {
		payload["accessible_by"] = []map[string]any{{"id": notionOAuthBotID(grant.ID), "type": "bot"}}
	}
	body, err := json.Marshal(payload)
	return body, true, err
}

// notionWebhookSignatureValue is Notion's X-Notion-Signature: sha256= and
// the hex HMAC-SHA256 of the body under the verification token.
func notionWebhookSignatureValue(token string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
