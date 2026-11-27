package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Which events gather, as Notion's webhooks gather them (developers.notion.com
// "Event types & delivery", read 2026-11-21): an aggregated event takes in
// the later changes of its kind to its entity while it waits out the window
// (eventWebhookAggregateMillis, Leafen's; Notion's is about a minute). A
// deletion and a restore of one entity in the window cancel out, and
// neither is sent. A page's first property change goes at once when nothing
// else of the page waits, and opens the window; the property changes in it
// go together when it closes. A page made, a lock, a comment, a view deleted
// and an upload go at once, alone, and the changes after a page is made are
// their own events. The subscription window's event help says the same
// (developerTools.internal.webhooks.eventHelp).

// eventWebhookAggregatedTypes wait out the window, taking in the later
// changes of their kind to their entity.
var eventWebhookAggregatedTypes = []string{
	eventWebhookContentUpdated, eventWebhookPropertiesUpdated, eventWebhookPageMoved,
	eventWebhookPageDeleted, eventWebhookPageUndeleted,
	eventWebhookDatabaseCreated, eventWebhookDatabaseContentUpdated, eventWebhookDatabaseSchemaUpdated,
	eventWebhookDatabaseMoved, eventWebhookDatabaseDeleted, eventWebhookDatabaseUndeleted,
	eventWebhookSourceCreated, eventWebhookSourceContentUpdated, eventWebhookSchemaUpdated,
	eventWebhookSourceMoved, eventWebhookSourceDeleted, eventWebhookSourceUndeleted,
	eventWebhookViewCreated, eventWebhookViewUpdated,
}

// eventWebhookCancelledBy pairs a deletion and a restore: each, waiting for
// its entity, cancels out the other.
var eventWebhookCancelledBy = map[string]string{
	eventWebhookPageDeleted: eventWebhookPageUndeleted, eventWebhookPageUndeleted: eventWebhookPageDeleted,
	eventWebhookDatabaseDeleted: eventWebhookDatabaseUndeleted, eventWebhookDatabaseUndeleted: eventWebhookDatabaseDeleted,
	eventWebhookSourceDeleted: eventWebhookSourceUndeleted, eventWebhookSourceUndeleted: eventWebhookSourceDeleted,
}

// eventWebhookPageTypesJSON are a page's own events, the ones that wait for
// it, as a JSON list.
var eventWebhookPageTypesJSON = func() string {
	encoded, _ := json.Marshal([]string{
		eventWebhookPageCreated, eventWebhookContentUpdated, eventWebhookPropertiesUpdated, eventWebhookPageMoved,
		eventWebhookPageDeleted, eventWebhookPageUndeleted, eventWebhookPageLocked, eventWebhookPageUnlocked,
	})
	return string(encoded)
}()

// gatherEventWebhookEvent takes a change into the event waiting for it: the
// opposite one it cancels out, or the one of its kind it joins. It is false
// when the change is an event of its own.
func gatherEventWebhookEvent(app core.App, input eventWebhookEventInput, author eventWebhookAuthor) (bool, error) {
	if opposite, cancels := eventWebhookCancelledBy[input.Type]; cancels {
		waiting := ""
		err := app.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_webhook_events}}
			WHERE [[workspace_id]] = {:workspace} AND [[type]] = {:type} AND [[entity_id]] = {:entity}
				AND [[dispatched]] = 0
			ORDER BY [[sequence]] DESC LIMIT 1
		`).Bind(dbx.Params{"workspace": input.WorkspaceID, "type": opposite, "entity": input.EntityID}).Row(&waiting)
		switch {
		case err == nil:
			if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_webhook_events}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": waiting}).Execute(); err != nil {
				return false, err
			}
			// The events it held back go now.
			notifyEventWebhookRunnerAfterCommit(app)
			return true, nil
		case !errors.Is(err, sql.ErrNoRows):
			return false, err
		}
	}
	if !slices.Contains(eventWebhookAggregatedTypes, input.Type) {
		return false, nil
	}
	pending := struct {
		ID          string `db:"id"`
		AuthorsJSON string `db:"authors_json"`
		DataJSON    string `db:"data_json"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[authors_json]], [[data_json]] FROM {{leafen_webhook_events}}
		WHERE [[workspace_id]] = {:workspace} AND [[type]] = {:type} AND [[entity_id]] = {:entity}
			AND [[dispatched]] = 0
		ORDER BY [[sequence]] DESC LIMIT 1
	`).Bind(dbx.Params{"workspace": input.WorkspaceID, "type": input.Type, "entity": input.EntityID}).One(&pending)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	authors, data := []eventWebhookAuthor{}, map[string]any{}
	if json.Unmarshal([]byte(pending.AuthorsJSON), &authors) != nil || json.Unmarshal([]byte(pending.DataJSON), &data) != nil {
		return false, errors.New(errorCodeEventWebhookFailed)
	}
	if !slices.Contains(authors, author) {
		authors = append(authors, author)
	}
	mergeEventWebhookData(data, input.Data)
	authorsJSON, _ := json.Marshal(authors)
	dataJSON, _ := json.Marshal(data)
	_, err = app.DB().NewQuery(`UPDATE {{leafen_webhook_events}} SET [[authors_json]] = {:authors}, [[data_json]] = {:data}
		WHERE [[id]] = {:id}`).Bind(dbx.Params{"authors": string(authorsJSON), "data": string(dataJSON), "id": pending.ID}).Execute()
	return true, err
}

// eventWebhookDispatchAfter is when a new event goes out: at once, or when
// its window closes. A page's property change goes at once unless an
// earlier one sent at once opened a window still open — then it goes when
// that closes — or another event of the page waits, which it waits with.
func eventWebhookDispatchAfter(app core.App, input eventWebhookEventInput, now int64) (int64, error) {
	dispatchAfter := now
	switch {
	case input.Type == eventWebhookPropertiesUpdated:
		var opened sql.NullInt64
		if err := app.DB().NewQuery(`
			SELECT MAX([[occurred_at]]) FROM {{leafen_webhook_events}}
			WHERE [[workspace_id]] = {:workspace} AND [[type]] = {:type} AND [[entity_id]] = {:entity}
				AND [[dispatched]] = 1 AND [[dispatch_after]] = [[occurred_at]] AND [[occurred_at]] > {:since}
		`).Bind(dbx.Params{
			"workspace": input.WorkspaceID, "type": input.Type, "entity": input.EntityID, "since": now - eventWebhookAggregateMillis,
		}).Row(&opened); err != nil {
			return 0, err
		}
		if opened.Valid {
			dispatchAfter = opened.Int64 + eventWebhookAggregateMillis
			break
		}
		waiting := 0
		if err := app.DB().NewQuery(`
			SELECT EXISTS(SELECT 1 FROM {{leafen_webhook_events}}
				WHERE [[workspace_id]] = {:workspace} AND [[entity_id]] = {:entity} AND [[dispatched]] = 0
					AND [[type]] IN (SELECT [[value]] FROM json_each({:types})))
		`).Bind(dbx.Params{
			"workspace": input.WorkspaceID, "entity": input.EntityID, "types": eventWebhookPageTypesJSON,
		}).Row(&waiting); err != nil {
			return 0, err
		}
		if waiting == 1 {
			dispatchAfter += eventWebhookAggregateMillis
		}
	case slices.Contains(eventWebhookAggregatedTypes, input.Type):
		dispatchAfter += eventWebhookAggregateMillis
	}
	return max(dispatchAfter, input.DispatchAt), nil
}
