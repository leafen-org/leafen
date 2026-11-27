package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a database's change did to its properties, views and rows, for its
// webhook events (event_webhook_database_events.go): each read against the
// data source as the page's version before held it.

// recordDatabaseViewWebhookEvents tells the views a change made, changed
// or deleted: which of its name, filter, sorts or other settings changed.
func recordDatabaseViewWebhookEvents(
	app core.App, sourceID string, before, after []pageHistoryDatabaseView,
	event func(eventType, entityID, entityType string, data map[string]any),
) {
	parent := func(view pageHistoryDatabaseView) map[string]any {
		container := view.ContainerID
		if container == "" {
			container = ownDatabaseContainer(app, sourceID)
		}
		return map[string]any{"id": container, "type": "database", "data_source": sourceID}
	}
	previous := map[string]pageHistoryDatabaseView{}
	for _, view := range before {
		previous[view.ID] = view
	}
	for _, view := range after {
		old, existed := previous[view.ID]
		delete(previous, view.ID)
		if !existed {
			event(eventWebhookViewCreated, view.ID, "view", map[string]any{"parent": parent(view), "view_type": view.Type})
			continue
		}
		if fields := databaseViewChangedFields(old, view); len(fields) > 0 {
			event(eventWebhookViewUpdated, view.ID, "view", map[string]any{"parent": parent(view), "updated_fields": fields})
		}
	}
	for _, view := range before {
		if _, gone := previous[view.ID]; gone {
			event(eventWebhookViewDeleted, view.ID, "view", map[string]any{"parent": parent(view)})
		}
	}
}

// databaseViewChangedFields names what changed of a view in Notion's
// words: its name, filter, sorts, or else its configuration — its own order
// of rows is none of them (databaseViewRowOrderKey).
func databaseViewChangedFields(before, after pageHistoryDatabaseView) []any {
	fields := []any{}
	if before.Name != after.Name {
		fields = append(fields, "name")
	}
	left, right := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	_ = json.Unmarshal(before.Config, &left)
	_ = json.Unmarshal(after.Config, &right)
	same := func(key string) bool { return pageHistorySameJSON(string(left[key]), string(right[key])) }
	if !same("filterGroup") || !same("filters") {
		fields = append(fields, "filter")
	}
	if !same("sorts") {
		fields = append(fields, "sorts")
	}
	configured := before.Type != after.Type
	for key := range unionKeys(left, right) {
		if key != "filterGroup" && key != "filters" && key != "sorts" && key != databaseViewRowOrderKey && !same(key) {
			configured = true
		}
	}
	if configured {
		fields = append(fields, "configuration")
	}
	return fields
}

// databaseSchemaChanges names the properties a change made, changed or
// deleted, as Notion's schema events do; a property only moved is the same.
func databaseSchemaChanges(before, after []pageHistoryDatabaseProperty) []any {
	changes := []any{}
	previous := map[string]pageHistoryDatabaseProperty{}
	for _, property := range before {
		previous[property.ID] = property
	}
	for _, property := range after {
		old, existed := previous[property.ID]
		delete(previous, property.ID)
		switch {
		case !existed:
			changes = append(changes, map[string]any{"id": property.ID, "name": property.Name, "action": "created"})
		case old.Name != property.Name || old.Type != property.Type || !pageHistorySameJSON(string(old.Config), string(property.Config)):
			changes = append(changes, map[string]any{"id": property.ID, "name": property.Name, "action": "updated"})
		}
	}
	for _, property := range before {
		if _, gone := previous[property.ID]; gone {
			changes = append(changes, map[string]any{"id": property.ID, "name": nil, "action": "deleted"})
		}
	}
	return changes
}

// mergeEventWebhookSchema joins a later change's properties into an
// aggregated schema event's.
func mergeEventWebhookSchema(current, next []any) []any {
	merged := slices.Clone(current)
	for _, item := range next {
		change, _ := item.(map[string]any)
		index := slices.IndexFunc(merged, func(existing any) bool {
			held, _ := existing.(map[string]any)
			return held["id"] == change["id"]
		})
		if index < 0 {
			merged = append(merged, item)
			continue
		}
		held, _ := merged[index].(map[string]any)
		switch {
		case change["action"] == "deleted" && held["action"] == "created":
			merged = slices.Delete(merged, index, index+1)
		case change["action"] == "deleted":
			merged[index] = item
		default:
			merged[index] = map[string]any{"id": held["id"], "name": change["name"], "action": held["action"]}
		}
	}
	return merged
}

// recordDatabaseRowsWebhookEvents tells each data source, and the database
// that owns it, the rows a change added to it or took away.
func recordDatabaseRowsWebhookEvents(app core.App, workspaceID string, rowIDs []string, input eventWebhookEventInput) {
	if len(rowIDs) == 0 || !eventWebhookWantsAny(app, workspaceID, eventWebhookSourceContentUpdated, eventWebhookDatabaseContentUpdated) {
		return
	}
	encoded, _ := json.Marshal(rowIDs)
	rows := []struct {
		ID         string `db:"id"`
		DataSource string `db:"data_source_id"`
		PageID     string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[data_source_id]], [[page_id]] FROM {{leafen_database_rows}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) ORDER BY [[id]]`).
		Bind(dbx.Params{"ids": string(encoded)}).All(&rows); err != nil {
		app.Logger().Warn("leafen webhook rows not read", "error", err)
		return
	}
	bySource, pages := map[string][]string{}, map[string]string{}
	order := []string{}
	for _, row := range rows {
		if _, seen := bySource[row.DataSource]; !seen {
			order = append(order, row.DataSource)
		}
		bySource[row.DataSource] = append(bySource[row.DataSource], row.ID)
		pages[row.DataSource] = row.PageID
	}
	for _, sourceID := range order {
		recordDatabaseSourceContentWebhookEvents(app, workspaceID, sourceID, pages[sourceID], bySource[sourceID], input)
	}
}

// recordDatabaseSourceContentWebhookEvents tells a data source, on its
// database's page, and the database that owns it of rows it gained or lost.
func recordDatabaseSourceContentWebhookEvents(
	app core.App, workspaceID, sourceID, pageID string, rowIDs []string, input eventWebhookEventInput,
) {
	parent := map[string]any{"id": pageID, "type": "page"}
	if page, err := app.FindRecordById(pagesCollection, pageID); err == nil && isFullPageDatabase(page) {
		parent = eventWebhookParent(page)
	}
	blocks := eventWebhookBlockList(rowIDs)
	container := ownDatabaseContainer(app, sourceID)
	next := input
	next.WorkspaceID, next.PageID = workspaceID, pageID
	next.Type, next.EntityID, next.EntityType = eventWebhookSourceContentUpdated, sourceID, "data_source"
	next.Data = map[string]any{"parent": map[string]any{"id": container, "type": "database"}, "updated_blocks": blocks}
	if container == "" {
		next.Data["parent"] = parent
	}
	recordEventWebhookEvent(app, next)
	if container != "" {
		next.Type, next.EntityID, next.EntityType = eventWebhookDatabaseContentUpdated, container, "database"
		next.Data = map[string]any{"parent": parent, "updated_blocks": blocks}
		recordEventWebhookEvent(app, next)
	}
}

// recordDatabaseRowsMovedWebhookEvents tells of database pages moved to
// another database: each page moved, and both data sources' rows.
func recordDatabaseRowsMovedWebhookEvents(app core.App, authority databaseRowPageAuthority, rowIDs []string, actorID string) {
	input := eventWebhookEventInput{ActorID: actorID, Connection: pageHistoryConnectionOf(app)}
	recordDatabaseSourceContentWebhookEvents(app, authority.WorkspaceID, authority.DataSourceID, authority.ContainerPageID, rowIDs, input)
	recordDatabaseRowsWebhookEvents(app, authority.WorkspaceID, rowIDs, input)
	for _, rowID := range rowIDs {
		if moved, err := loadDatabaseRowPageAuthority(app, rowID); err == nil {
			recordDatabaseRowPageWebhookEvent(app, eventWebhookPageMoved, moved, actorID)
		}
	}
}

// recordDatabaseRowPagesWebhookEvents tells of the database pages a change
// added or took away, as Notion's page events do theirs: made or deleted,
// or moved when the change moves them between databases, into one or out
// of one to a page.
func recordDatabaseRowPagesWebhookEvents(app core.App, workspaceID string, changes []pageHistoryRowChange, input eventWebhookEventInput) {
	if len(changes) == 0 || !eventWebhookWantsAny(app, workspaceID, eventWebhookPageCreated, eventWebhookPageDeleted, eventWebhookPageMoved) {
		return
	}
	for _, change := range changes {
		added, removed := change.Change == pageHistoryChangeRowAdded, change.Change == pageHistoryChangeRowRemoved
		if !added && !removed {
			continue
		}
		page, err := app.FindRecordById(pagesCollection, change.ID)
		if err != nil {
			continue
		}
		moving := databaseRowMoving(app, change.ID)
		next := input
		switch {
		case added && moving, removed && moving && page.GetString("data_source_id") == "":
			next.Type = eventWebhookPageMoved
		case added:
			next.Type = eventWebhookPageCreated
		case !moving:
			next.Type = eventWebhookPageDeleted
		default:
			continue
		}
		next.EntityID, next.EntityType = page.Id, "page"
		next.Data = map[string]any{"parent": eventWebhookParent(page)}
		recordEventWebhookEvent(app, next)
	}
}

// recordDatabaseRowTrashWebhookEvent tells of a database page sent to the
// trash or back from it, and its data source of the row it lost or gained.
func recordDatabaseRowTrashWebhookEvent(app core.App, authority databaseRowPageAuthority, actorID string, trashed bool) {
	input := eventWebhookEventInput{ActorID: actorID, Connection: pageHistoryConnectionOf(app)}
	recordDatabaseRowsWebhookEvents(app, authority.WorkspaceID, []string{authority.RowID}, input)
	kind := eventWebhookPageUndeleted
	if trashed {
		kind = eventWebhookPageDeleted
	}
	recordDatabaseRowPageWebhookEvent(app, kind, authority, actorID)
}

// recordDatabaseRowPageWebhookEvent records a database page's own event,
// seen by those who can read its database.
func recordDatabaseRowPageWebhookEvent(app core.App, eventType string, authority databaseRowPageAuthority, actorID string) {
	recordEventWebhookEvent(app, eventWebhookEventInput{
		WorkspaceID: authority.WorkspaceID, Type: eventType, EntityID: authority.RowID, EntityType: "page",
		PageID: authority.ContainerPageID, ActorID: actorID, Connection: pageHistoryConnectionOf(app),
		Data: map[string]any{"parent": map[string]any{"id": authority.DataSourceID, "type": "data_source"}},
	})
}

// pageHistoryDatabaseStatesBefore reads, for each of a page's data sources,
// its state before a version: from the latest earlier version that changed
// it, or else from the checkpoint before those; nil when the page didn't
// hold it.
func pageHistoryDatabaseStatesBefore(
	app core.App, pageID string, versionCursor int, ids []string,
) (map[string]*pageHistoryDatabaseState, error) {
	checkpoint := pageHistoryCheckpoint{}
	if err := app.DB().NewQuery(`SELECT [[page_id]], [[version_cursor]], [[databases_json]] FROM {{leafen_page_history_checkpoints}}
		WHERE [[page_id]] = {:page} AND [[version_cursor]] < {:cursor} ORDER BY [[version_cursor]] DESC LIMIT 1`).
		Bind(dbx.Params{"page": pageID, "cursor": versionCursor}).One(&checkpoint); err != nil {
		return nil, err
	}
	encoded, err := unpackPageHistoryText(checkpoint.DatabasesJSON)
	if err != nil {
		return nil, err
	}
	checkpoint.DatabasesJSON = encoded
	events := []storedPageHistoryEvent{}
	if err := app.DB().NewQuery(`SELECT [[version_cursor]], [[kind]], [[payload]], [[created_at]] FROM {{leafen_page_history_events}}
		WHERE [[page_id]] = {:page} AND [[version_cursor]] > {:from} AND [[version_cursor]] < {:cursor}
			AND [[kind]] IN ({:graph}, {:collaboration}, {:restore}, {:rewrite})
		ORDER BY [[version_cursor]] DESC`).Bind(dbx.Params{
		"page": pageID, "from": checkpoint.VersionCursor, "cursor": versionCursor,
		"graph": pageHistoryEventGraph, "collaboration": pageHistoryEventCollaboration,
		"restore": pageHistoryEventRestore, "rewrite": pageHistoryEventRewrite,
	}).All(&events); err != nil {
		return nil, err
	}
	states := map[string]*pageHistoryDatabaseState{}
	remaining := slices.Clone(ids)
	for _, stored := range events {
		if len(remaining) == 0 {
			break
		}
		envelope, err := pageHistoryEventPayload(stored.Payload)
		if err != nil {
			return nil, err
		}
		remaining = slices.DeleteFunc(remaining, func(id string) bool {
			state, changed := envelope.Databases[id]
			if changed {
				states[id] = state
			}
			return changed
		})
	}
	if len(remaining) > 0 {
		kept, err := pageHistoryCheckpointDatabases(app, checkpoint)
		if err != nil {
			return nil, err
		}
		for _, id := range remaining {
			if state, held := kept[id]; held {
				states[id] = &state
			}
		}
	}
	return states, nil
}
