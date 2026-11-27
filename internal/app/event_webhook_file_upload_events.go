package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A Notion-compatible file upload's events, as Notion's webhooks send them:
// made, its contents complete (sent, or fetched from its address), its
// fetch failed with the import's result, or an hour gone by without it
// being attached. Only the connection that made the upload hears of it, as
// a Notion upload is its bot's (notion_webhooks.go). Its expiry is told
// when it comes, and only if the upload by then is neither attached nor
// done with.

const (
	eventWebhookFileUploadCreated   = "file_upload.created"
	eventWebhookFileUploadCompleted = "file_upload.completed"
	eventWebhookFileUploadExpired   = "file_upload.expired"
	eventWebhookFileUploadFailed    = "file_upload.upload_failed"
)

// recordFileUploadWebhookEvent records an upload's event, by the connection
// that made the upload: named when it is made, and as its first event
// recorded it after.
func recordFileUploadWebhookEvent(
	app core.App, eventType string, upload storedProductFileUpload, connection pageHistoryConnection, data map[string]any,
) {
	if upload.NotionMode == "" {
		return
	}
	if connection.ID == "" {
		first := struct {
			AuthorsJSON string `db:"authors_json"`
			DataJSON    string `db:"data_json"`
		}{}
		// An upload's events are in its workspace under these types, as the
		// pending events' index leads with them.
		_ = app.DB().NewQuery(`SELECT [[authors_json]], [[data_json]] FROM {{leafen_webhook_events}}
			WHERE [[workspace_id]] = {:workspace} AND [[type]] IN ({:created}, {:completed}, {:expired}, {:failed})
				AND [[entity_id]] = {:upload} AND [[entity_type]] = 'file_upload' ORDER BY [[sequence]] LIMIT 1`).
			Bind(dbx.Params{
				"workspace": upload.WorkspaceID, "upload": upload.UploadID,
				"created": eventWebhookFileUploadCreated, "completed": eventWebhookFileUploadCompleted,
				"expired": eventWebhookFileUploadExpired, "failed": eventWebhookFileUploadFailed,
			}).One(&first)
		authors, made := []eventWebhookAuthor{}, map[string]any{}
		if json.Unmarshal([]byte(first.AuthorsJSON), &authors) == nil && len(authors) > 0 {
			connection.Name = authors[0].Connection
		}
		if json.Unmarshal([]byte(first.DataJSON), &made) == nil {
			connection.ID, _ = made["connection"].(string)
		}
	}
	if connection.ID == "" {
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	data["owner"], data["connection"] = upload.ActorID, connection.ID
	input := eventWebhookEventInput{
		WorkspaceID: upload.WorkspaceID, Type: eventType, EntityID: upload.UploadID, EntityType: "file_upload",
		ActorID: upload.ActorID, Connection: connection, Data: data,
	}
	if eventType == eventWebhookFileUploadExpired {
		input.DispatchAt = notionFileUploadExpiresAt(upload)
	}
	recordEventWebhookEvent(app, input)
}

// recordFileUploadAdmittedWebhookEvents records a new upload's event, and
// its expiry to be told when it comes.
func recordFileUploadAdmittedWebhookEvents(app core.App, upload storedProductFileUpload, identity notionOAuthBearerIdentity) {
	connection := pageHistoryConnection{}
	if grant, found, err := findNotionOAuthGrantByID(app, notionConnectionGrant(identity)); err == nil && found {
		connection = pageHistoryConnection{Name: pageHistoryConnectionLabel(grant.ClientName, "API"), ID: "notion:" + grant.ID}
	}
	recordFileUploadWebhookEvent(app, eventWebhookFileUploadCreated, upload, connection, nil)
	recordFileUploadWebhookEvent(app, eventWebhookFileUploadExpired, upload, connection, nil)
}

// recordFileUploadFailedWebhookEvent tells of an upload whose address
// couldn't be fetched, with Notion's import result.
func recordFileUploadFailedWebhookEvent(app core.App, upload storedProductFileUpload) {
	result := notionFileUploadFailureObject(upload)
	result["imported_time"] = notionFileUploadTimestamp(upload.UpdatedAt)
	recordFileUploadWebhookEvent(app, eventWebhookFileUploadFailed, upload, pageHistoryConnection{}, map[string]any{"file_import_result": result})
}

// notionFileUploadEventStands is whether an upload's event still holds when
// it is sent: an expiry only for an upload expired and never attached.
func notionFileUploadEventStands(app core.App, event storedEventWebhookEvent) bool {
	if event.Type != eventWebhookFileUploadExpired {
		return true
	}
	upload, err := loadProductFileUploadByUploadID(app, event.EntityID)
	if err != nil || upload.NotionAttachedAt > 0 || upload.Status == fileUploadStatusFailed {
		return false
	}
	return notionFileUploadPublicStatus(upload, time.Now().UTC().UnixMilli()) == "expired"
}
