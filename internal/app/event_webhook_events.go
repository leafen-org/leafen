package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A change a subscription can ask for becomes an event in its workspace,
// recorded with the change itself, when some subscription there wants its
// kind. Changes a few seconds apart gather into one event, or cancel out, as
// Notion's webhooks aggregate them (event_webhook_aggregation.go). An event
// that can't be recorded is logged and the change goes on: a webhook never
// stops a save.

const (
	eventWebhookPageCreated       = "page.created"
	eventWebhookContentUpdated    = "page.content_updated"
	eventWebhookPropertiesUpdated = "page.properties_updated"
	eventWebhookPageMoved         = "page.moved"
	eventWebhookPageDeleted       = "page.deleted"
	eventWebhookPageUndeleted     = "page.undeleted"
	eventWebhookPageLocked        = "page.locked"
	eventWebhookPageUnlocked      = "page.unlocked"
	eventWebhookCommentCreated    = "comment.created"
	eventWebhookCommentUpdated    = "comment.updated"
	eventWebhookCommentDeleted    = "comment.deleted"
	eventWebhookSchemaUpdated     = "data_source.schema_updated"
	eventWebhookTest              = "webhook.test"
)

type eventWebhookEventInput struct {
	WorkspaceID string
	Type        string
	EntityID    string
	EntityType  string
	// PageID is the page whose readers may see the event.
	PageID     string
	ActorID    string
	Connection pageHistoryConnection
	Data       map[string]any
	// DispatchAt holds an event back until then, as an upload's expiry is
	// told when it comes.
	DispatchAt int64
}

func eventWebhookID() string {
	return security.RandomStringWithAlphabet(20, "abcdefghijklmnopqrstuvwxyz0123456789")
}

func recordEventWebhookEvent(app core.App, input eventWebhookEventInput) {
	if err := recordEventWebhookEventStrict(app, input); err != nil {
		app.Logger().Warn("leafen webhook event not recorded", "type", input.Type, "error", err)
	}
}

// webhookWantedKey is whether a workspace's webhooks want a kind of event, as
// a content change asks it once (write_lookup_memo.go).
type webhookWantedKey struct{ workspaceID, eventType string }

// transactionsKeptFromApps are the open transactions no webhook and no app
// hears of: Leafen's own upkeep of the pages it keeps, its workspace's People
// and its people's pages, which no one made or edited
// (contentGraphMutationHooks.LeafenOwn).
var transactionsKeptFromApps = struct {
	sync.Mutex
	held map[*core.TxAppInfo]bool
}{held: map[*core.TxAppInfo]bool{}}

// keepTransactionFromApps keeps every event of txApp's transaction from its
// workspace's webhooks and apps, until the transaction ends.
func keepTransactionFromApps(txApp core.App) {
	transaction := txApp.TxInfo()
	if transaction == nil {
		return
	}
	transactionsKeptFromApps.Lock()
	defer transactionsKeptFromApps.Unlock()
	if transactionsKeptFromApps.held[transaction] {
		return
	}
	transactionsKeptFromApps.held[transaction] = true
	transaction.OnComplete(func(error) error {
		transactionsKeptFromApps.Lock()
		delete(transactionsKeptFromApps.held, transaction)
		transactionsKeptFromApps.Unlock()
		return nil
	})
}

func transactionKeptFromApps(app core.App) bool {
	transaction := app.TxInfo()
	if transaction == nil {
		return false
	}
	transactionsKeptFromApps.Lock()
	defer transactionsKeptFromApps.Unlock()
	return transactionsKeptFromApps.held[transaction]
}

func recordEventWebhookEventStrict(app core.App, input eventWebhookEventInput) error {
	if input.WorkspaceID == "" || input.EntityID == "" || input.ActorID == "" || transactionKeptFromApps(app) {
		return nil
	}
	// The same change may call an app whose workspace it is.
	if input.Type != eventWebhookTest {
		recordAppDataEvent(app, input)
	}
	if wanted, err := eventWebhookWanted(app, input.WorkspaceID, input.Type); err != nil || !wanted {
		return err
	}
	if input.Data == nil {
		input.Data = map[string]any{}
	}
	author := eventWebhookAuthor{ID: input.ActorID, Type: "person", Connection: input.Connection.Name}
	now := time.Now().UTC().UnixMilli()
	// A change waiting events take in is told by them
	// (event_webhook_aggregation.go).
	if gathered, err := gatherEventWebhookEvent(app, input, author); err != nil || gathered {
		return err
	}
	authorsJSON, _ := json.Marshal([]eventWebhookAuthor{author})
	dataJSON, err := json.Marshal(input.Data)
	if err != nil {
		return err
	}
	dispatchAfter, err := eventWebhookDispatchAfter(app, input, now)
	if err != nil {
		return err
	}
	if _, err := app.DB().Insert("leafen_webhook_events", dbx.Params{
		"id": eventWebhookID(), "workspace_id": input.WorkspaceID, "type": input.Type,
		"entity_id": input.EntityID, "entity_type": input.EntityType, "page_id": input.PageID,
		"authors_json": string(authorsJSON), "data_json": string(dataJSON),
		"occurred_at": now, "dispatch_after": dispatchAfter,
	}).Execute(); err != nil {
		return err
	}
	notifyEventWebhookRunnerAfterCommit(app)
	return nil
}

// eventWebhookWanted is whether some subscription in a workspace wants a
// kind of event.
func eventWebhookWanted(app core.App, workspaceID, eventType string) (bool, error) {
	wanted, err := writeLookup(app, webhookWantedKey{workspaceID: workspaceID, eventType: eventType}, func() (int, error) {
		wanted := 0
		err := app.DB().NewQuery(`
			SELECT EXISTS(SELECT 1 FROM {{leafen_webhook_subscriptions}}
				WHERE [[workspace_id]] = {:workspace} AND [[active]] = 1 AND [[verify_state]] = '' AND [[event_types_json]] LIKE {:pattern})
		`).Bind(dbx.Params{"workspace": workspaceID, "pattern": `%"` + eventType + `"%`}).Row(&wanted)
		return wanted, err
	})
	return wanted == 1, err
}

// eventWebhookWantsAny is whether some subscription wants any of the kinds.
func eventWebhookWantsAny(app core.App, workspaceID string, eventTypes ...string) bool {
	for _, eventType := range eventTypes {
		if wanted, err := eventWebhookWanted(app, workspaceID, eventType); err == nil && wanted {
			return true
		}
	}
	return false
}

// mergeEventWebhookData joins a later change's lists into an aggregated
// event's: the blocks and properties either changed. A schema's properties
// are one each, the earlier change's action standing unless the later
// deletes it; one made and deleted again is gone.
func mergeEventWebhookData(into, from map[string]any) {
	for key, value := range from {
		next, list := value.([]any)
		current, held := into[key].([]any)
		if !list || !held {
			into[key] = value
			continue
		}
		if key == "updated_schema" {
			into[key] = mergeEventWebhookSchema(current, next)
			continue
		}
		for _, item := range next {
			encoded, _ := json.Marshal(item)
			if !slices.ContainsFunc(current, func(existing any) bool {
				other, _ := json.Marshal(existing)
				return string(other) == string(encoded)
			}) {
				current = append(current, item)
			}
		}
		into[key] = current
	}
}

// eventWebhookParent names where a page sits: in a data source's database,
// under a page, or at the top of its workspace.
func eventWebhookParent(page *core.Record) map[string]any {
	if source := page.GetString("data_source_id"); source != "" {
		return map[string]any{"id": source, "type": "data_source"}
	}
	if parentID := page.GetString("parent_id"); parentID != "" {
		return map[string]any{"id": parentID, "type": "page"}
	}
	return map[string]any{"id": page.GetString("workspace"), "type": "workspace"}
}

func eventWebhookBlockList(ids []string) []any {
	list := []any{}
	seen := map[string]bool{}
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			list = append(list, map[string]any{"id": id, "type": "block"})
		}
	}
	return list
}

// recordPageHistoryWebhookEvent turns a page's recorded version into its
// event: its blocks or text changed, or its properties did; and the events
// of the databases on it (event_webhook_database_events.go).
func recordPageHistoryWebhookEvent(app core.App, page *core.Record, version storedPageHistoryVersion, payload []byte) {
	input := eventWebhookEventInput{
		WorkspaceID: page.GetString("workspace"), EntityID: page.Id, EntityType: "page", PageID: page.Id,
		ActorID: version.ActorID, Connection: pageHistoryConnection{Name: version.ConnectionName, ID: version.ConnectionID},
	}
	switch version.Source {
	case pageHistoryEventGraph, pageHistoryEventCollaboration, pageHistoryEventRestore, pageHistoryEventRewrite:
		blocks := []string{}
		if envelope, err := pageHistoryEventPayload(payload); err == nil {
			for _, delta := range envelope.GraphDeltas {
				blocks = append(blocks, delta.BlockID)
			}
			for _, delta := range envelope.TextDeltas {
				blocks = append(blocks, delta.BlockID)
			}
			recordDatabaseHistoryWebhookEvents(app, page, version, envelope, input)
		}
		if version.Source == pageHistoryEventGraph && len(blocks) == 0 {
			return
		}
		input.Type = eventWebhookContentUpdated
		input.Data = map[string]any{"parent": eventWebhookParent(page), "updated_blocks": eventWebhookBlockList(blocks)}
	case pageHistoryEventPage, pageHistoryEventMetadata:
		changed, err := pageHistoryChangedFields(app, page.Id, version.VersionCursor, payload)
		if err != nil || len(changed) == 0 {
			return
		}
		input.Type = eventWebhookPropertiesUpdated
		input.Data = map[string]any{"parent": eventWebhookParent(page), "updated_properties": changed}
	default:
		return
	}
	// A full-page database is a database to Notion, not a page.
	if isFullPageDatabase(page) {
		return
	}
	recordEventWebhookEvent(app, input)
}

// pageHistoryChangedFields names the page fields a page-state version
// changed against the page's state before it.
func pageHistoryChangedFields(app core.App, pageID string, cursor int, payload []byte) ([]any, error) {
	before := ""
	err := app.DB().NewQuery(`
		SELECT [[payload]] FROM {{leafen_page_history_events}}
		WHERE [[page_id]] = {:page} AND [[version_cursor]] < {:cursor} AND [[kind]] IN ({:page_kind}, {:metadata})
		ORDER BY [[version_cursor]] DESC LIMIT 1
	`).Bind(dbx.Params{"page": pageID, "cursor": cursor, "page_kind": pageHistoryEventPage, "metadata": pageHistoryEventMetadata}).Row(&before)
	if errors.Is(err, sql.ErrNoRows) {
		err = app.DB().NewQuery(`SELECT [[page_json]] FROM {{leafen_page_history_checkpoints}}
			WHERE [[page_id]] = {:page} AND [[version_cursor]] = 0`).Bind(dbx.Params{"page": pageID}).Row(&before)
	}
	if err == nil {
		before, err = unpackPageHistoryText(before)
	}
	if err != nil {
		return nil, err
	}
	previous, next := pageHistoryPageState{}, pageHistoryPageState{}
	if json.Unmarshal([]byte(before), &previous) != nil || json.Unmarshal(payload, &next) != nil {
		return nil, errors.New(errorCodeEventWebhookFailed)
	}
	beforeFields, err := pageHistoryRevertFields(previous)
	if err != nil {
		return nil, err
	}
	afterFields, err := pageHistoryRevertFields(next)
	if err != nil {
		return nil, err
	}
	changed := []any{}
	names := make([]string, 0, len(afterFields)+len(beforeFields))
	for name := range unionKeys(beforeFields, afterFields) {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !pageHistorySameJSON(string(beforeFields[name]), string(afterFields[name])) {
			changed = append(changed, name)
		}
	}
	return changed, nil
}

// recordPageWebhookEvent records a change to where a page is.
func recordPageWebhookEvent(app core.App, eventType string, page *core.Record, actorID string) {
	recordEventWebhookEvent(app, eventWebhookEventInput{
		WorkspaceID: page.GetString("workspace"), Type: eventType, EntityID: page.Id, EntityType: "page",
		PageID: page.Id, ActorID: actorID, Connection: pageHistoryConnectionOf(app),
		Data: map[string]any{"parent": eventWebhookParent(page)},
	})
}

// recordCommentWebhookEvent records a comment made, edited or deleted, seen
// by those who can read its page.
func recordCommentWebhookEvent(app core.App, eventType string, comment storedComment, actorID string) {
	parent := map[string]any{"id": comment.PageID, "type": "page"}
	if comment.BlockID != "" {
		parent = map[string]any{"id": comment.BlockID, "type": "block"}
	}
	recordEventWebhookEvent(app, eventWebhookEventInput{
		WorkspaceID: comment.WorkspaceID, Type: eventType, EntityID: comment.ID, EntityType: "comment",
		PageID: comment.PageID, ActorID: actorID, Connection: pageHistoryConnectionOf(app),
		Data: map[string]any{"page_id": comment.PageID, "parent": parent},
	})
}
