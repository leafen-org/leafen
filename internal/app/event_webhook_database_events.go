package app

import (
	"encoding/json"
	"maps"
	"slices"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database's, its data sources' and its views' events, as Notion's
// webhooks send them (its event reference, read 2026-10-31). A page's
// recorded version tells them, with the version's author: the databases
// its blocks made, deleted, brought back or moved, the data sources a
// database gained or lost, the properties and views each changed and the
// rows each gained or lost. Before and after are the version's own record
// and the one before it; a database's block projected during the change
// notes what it was, so a move or a restore is told as one.

const (
	eventWebhookDatabaseCreated        = "database.created"
	eventWebhookDatabaseDeleted        = "database.deleted"
	eventWebhookDatabaseUndeleted      = "database.undeleted"
	eventWebhookDatabaseMoved          = "database.moved"
	eventWebhookDatabaseContentUpdated = "database.content_updated"
	eventWebhookDatabaseSchemaUpdated  = "database.schema_updated"
	eventWebhookSourceCreated          = "data_source.created"
	eventWebhookSourceDeleted          = "data_source.deleted"
	eventWebhookSourceUndeleted        = "data_source.undeleted"
	eventWebhookSourceMoved            = "data_source.moved"
	eventWebhookSourceContentUpdated   = "data_source.content_updated"
	eventWebhookViewCreated            = "view.created"
	eventWebhookViewUpdated            = "view.updated"
	eventWebhookViewDeleted            = "view.deleted"
)

var eventWebhookContainerTypes = []string{
	eventWebhookDatabaseCreated, eventWebhookDatabaseUndeleted, eventWebhookDatabaseMoved,
	eventWebhookSourceCreated, eventWebhookSourceDeleted, eventWebhookSourceUndeleted, eventWebhookSourceMoved,
}

// databaseContainerNote is what one change did to a database's block: how
// its first projection found it and how its last left it, its own data
// sources only (a linked one isn't its own).
type databaseContainerNote struct {
	Existed bool
	Revived bool
	// Trashed is a data source's database of its own in the trash, which
	// it leaves to go back to the database it came from.
	Trashed       bool
	PageBefore    string
	SourcesBefore []string
	PageAfter     string
	SourcesAfter  []string
	Linked        bool
	// TrashedInPlace are the own sources the database kept in the trash
	// where they stand before the change, TrashedAfter those it keeps after
	// (database_source_trash_in_place_record.go).
	TrashedInPlace []string
	TrashedAfter   []string
	// Told is a note whose page's version told its events already.
	Told bool
}

// databaseTransactionNotes are a transaction's database notes, and the data
// sources it moves from one database to another.
type databaseTransactionNotes struct {
	containers map[string]*databaseContainerNote
	moving     map[string]bool
	// movingRows are database pages moved to another database or out to a
	// page, or pages moved into one: told of as moved, not made or deleted.
	movingRows map[string]bool
}

var databaseContainerNotes = struct {
	sync.Mutex
	byTransaction map[*core.TxAppInfo]*databaseTransactionNotes
}{byTransaction: map[*core.TxAppInfo]*databaseTransactionNotes{}}

// transactionDatabaseNotes are app's transaction's notes, kept until it
// ends when some subscription in the workspace wants to hear of them; the
// caller holds the lock.
func transactionDatabaseNotes(app core.App, workspaceID string, eventTypes ...string) *databaseTransactionNotes {
	transaction := app.TxInfo()
	if len(eventTypes) == 0 {
		eventTypes = eventWebhookContainerTypes
	}
	if transaction == nil || !eventWebhookWantsAny(app, workspaceID, eventTypes...) {
		return nil
	}
	notes, known := databaseContainerNotes.byTransaction[transaction]
	if !known {
		notes = &databaseTransactionNotes{containers: map[string]*databaseContainerNote{}, moving: map[string]bool{}, movingRows: map[string]bool{}}
		databaseContainerNotes.byTransaction[transaction] = notes
		transaction.OnComplete(func(error) error {
			databaseContainerNotes.Lock()
			delete(databaseContainerNotes.byTransaction, transaction)
			databaseContainerNotes.Unlock()
			return nil
		})
	}
	return notes
}

// noteDatabaseContainerProjection keeps what a projection changed of a
// database's block.
func noteDatabaseContainerProjection(
	app core.App, workspaceID, containerID string, before databaseContainerNote, pageID string, sources, trashed []string, linked bool,
) {
	databaseContainerNotes.Lock()
	defer databaseContainerNotes.Unlock()
	notes := transactionDatabaseNotes(app, workspaceID)
	if notes == nil {
		return
	}
	// A database told of already starts again from how this finds it.
	note, held := notes.containers[containerID]
	if !held || note.Told {
		kept := before
		note = &kept
		notes.containers[containerID] = note
	}
	note.Revived = note.Revived || before.Revived
	note.PageAfter, note.SourcesAfter, note.Linked = pageID, slices.Clone(sources), linked
	note.TrashedAfter = slices.Clone(trashed)
}

// noteDataSourceLeavesTrash notes a data source's own database in the
// trash giving it back to the database it came from.
func noteDataSourceLeavesTrash(app core.App, workspaceID, containerID, pageID, sourceID string) {
	noteDatabaseContainerProjection(app, workspaceID, containerID,
		databaseContainerNote{Existed: true, Trashed: true, PageBefore: pageID, SourcesBefore: []string{sourceID}}, pageID, nil, nil, false)
}

// noteDataSourceMoving notes a data source a change moves to a database on
// another page: the database it leaves first lets it go, unheard, and the
// one that takes it tells of the move.
func noteDataSourceMoving(app core.App, workspaceID, sourceID string) {
	databaseContainerNotes.Lock()
	defer databaseContainerNotes.Unlock()
	if notes := transactionDatabaseNotes(app, workspaceID); notes != nil {
		notes.moving[sourceID] = true
	}
}

// noteDatabaseRowsMoving notes database pages a change moves between
// databases, out of one or into one.
func noteDatabaseRowsMoving(app core.App, workspaceID string, rowIDs ...string) {
	databaseContainerNotes.Lock()
	defer databaseContainerNotes.Unlock()
	if notes := transactionDatabaseNotes(app, workspaceID, eventWebhookPageMoved, eventWebhookPageCreated, eventWebhookPageDeleted); notes != nil {
		for _, rowID := range rowIDs {
			notes.movingRows[rowID] = true
		}
	}
}

// databaseRowMoving is whether a change moves a database page.
func databaseRowMoving(app core.App, rowID string) bool {
	transaction := app.TxInfo()
	if transaction == nil {
		return false
	}
	databaseContainerNotes.Lock()
	defer databaseContainerNotes.Unlock()
	notes := databaseContainerNotes.byTransaction[transaction]
	return notes != nil && notes.movingRows[rowID]
}

// takeDatabaseContainerNotes hands over the notes of a transaction's
// databases now on a page, with all its notes and the sources it moves.
func takeDatabaseContainerNotes(app core.App, pageID string) (map[string]databaseContainerNote, map[string]databaseContainerNote, map[string]bool) {
	transaction := app.TxInfo()
	if transaction == nil {
		return nil, nil, nil
	}
	databaseContainerNotes.Lock()
	defer databaseContainerNotes.Unlock()
	notes := databaseContainerNotes.byTransaction[transaction]
	if notes == nil {
		return nil, nil, nil
	}
	taken, all := map[string]databaseContainerNote{}, map[string]databaseContainerNote{}
	for id, note := range notes.containers {
		all[id] = *note
		if note.PageAfter == pageID && !note.Told {
			taken[id] = *note
			note.Told = true
		}
	}
	return taken, all, maps.Clone(notes.moving)
}

// recordDatabaseHistoryWebhookEvents tells what a page's version did to
// the databases on it.
func recordDatabaseHistoryWebhookEvents(
	app core.App, page *core.Record, version storedPageHistoryVersion, envelope pageHistoryEventEnvelope, input eventWebhookEventInput,
) {
	workspaceID := page.GetString("workspace")
	fullPage := isFullPageDatabase(page)
	event := func(eventType, entityID, entityType string, data map[string]any) {
		next := input
		next.Type, next.EntityID, next.EntityType, next.Data = eventType, entityID, entityType, data
		recordEventWebhookEvent(app, next)
	}
	databaseParent := func(parentBlockID string) map[string]any {
		switch {
		case fullPage:
			return eventWebhookParent(page)
		case parentBlockID != "":
			return map[string]any{"id": parentBlockID, "type": "block"}
		}
		return map[string]any{"id": page.Id, "type": "page"}
	}
	sourceParent := func(sourceID string) map[string]any {
		if container := ownDatabaseContainer(app, sourceID); container != "" {
			return map[string]any{"id": container, "type": "database"}
		}
		return map[string]any{"id": page.Id, "type": "page"}
	}
	containerEntity := func(linked bool) string {
		if linked {
			return "block"
		}
		return "database"
	}

	// Databases made, brought back or moved here, and data sources gained.
	taken, all, moving := takeDatabaseContainerNotes(app, page.Id)
	for containerID, note := range taken {
		parentBlockID := databaseContainerParentBlock(app, containerID)
		// A data source trashed out of its database is kept as one of its
		// own, in the trash from the start; its parent is still the
		// database it left.
		origin, told := "", false
		if trashed, _ := databaseContainerIsTrashed(app, containerID); trashed && !note.Existed {
			for otherID, other := range all {
				if len(note.SourcesAfter) > 0 && slices.Contains(other.SourcesBefore, note.SourcesAfter[0]) &&
					!slices.Contains(other.SourcesAfter, note.SourcesAfter[0]) {
					origin = otherID
					// One already in the trash where it stood was told of
					// as it went (database_source_trash_in_place_retention.go).
					told = slices.Contains(other.TrashedInPlace, note.SourcesAfter[0])
				}
			}
		}
		switch {
		case told:
			continue
		case origin != "":
			for _, sourceID := range note.SourcesAfter {
				event(eventWebhookSourceDeleted, sourceID, "data_source", map[string]any{"parent": map[string]any{"id": origin, "type": "database"}})
			}
			continue
		case note.Revived:
			event(eventWebhookDatabaseUndeleted, containerID, containerEntity(note.Linked), map[string]any{"parent": databaseParent(parentBlockID)})
		case !note.Existed:
			event(eventWebhookDatabaseCreated, containerID, containerEntity(note.Linked), map[string]any{"parent": databaseParent(parentBlockID)})
		case note.PageBefore != note.PageAfter:
			event(eventWebhookDatabaseMoved, containerID, containerEntity(note.Linked), map[string]any{"parent": databaseParent(parentBlockID)})
		}
		if !note.Existed || note.Revived || note.Linked {
			continue
		}
		// A source its database keeps in the trash where it stands goes to
		// the trash, or comes back from it, there.
		for _, sourceID := range note.SourcesAfter {
			before, after := slices.Contains(note.TrashedInPlace, sourceID), slices.Contains(note.TrashedAfter, sourceID)
			if before == after || !slices.Contains(note.SourcesBefore, sourceID) {
				continue
			}
			kind := eventWebhookSourceDeleted
			if before {
				kind = eventWebhookSourceUndeleted
			}
			event(kind, sourceID, "data_source", map[string]any{"parent": map[string]any{"id": containerID, "type": "database"}})
		}
		for _, sourceID := range note.SourcesAfter {
			if slices.Contains(note.SourcesBefore, sourceID) {
				continue
			}
			kind := eventWebhookSourceCreated
			data := map[string]any{"parent": sourceParent(sourceID)}
			if moving[sourceID] {
				kind, data = eventWebhookSourceMoved, map[string]any{"parent": map[string]any{"id": containerID, "type": "database"}}
			}
			for otherID, other := range all {
				if otherID == containerID || !slices.Contains(other.SourcesBefore, sourceID) || slices.Contains(other.SourcesAfter, sourceID) {
					continue
				}
				kind = eventWebhookSourceMoved
				data = map[string]any{"parent": map[string]any{"id": containerID, "type": "database"}}
				if other.Trashed {
					kind, data = eventWebhookSourceUndeleted, map[string]any{"parent": sourceParent(sourceID)}
				}
			}
			event(kind, sourceID, "data_source", data)
		}
		for _, sourceID := range note.SourcesBefore {
			if moving[sourceID] || slices.Contains(note.SourcesAfter, sourceID) || slices.ContainsFunc(mapsValues(all), func(other databaseContainerNote) bool {
				return slices.Contains(other.SourcesAfter, sourceID)
			}) {
				continue
			}
			event(eventWebhookSourceDeleted, sourceID, "data_source", map[string]any{"parent": sourceParent(sourceID)})
		}
	}

	// Databases whose blocks left the page, unless they went on elsewhere.
	// A deleted block's record keeps neither its type nor its content; its
	// database is read where it was, for all the deleted blocks at once (read
	// for each, a 20,100-block deletion read 20,101 times while it held the
	// one write connection, 2026-11-08).
	deleted := []string{}
	for _, delta := range envelope.GraphDeltas {
		if delta.Deleted {
			deleted = append(deleted, delta.BlockID)
		}
	}
	containers := []struct {
		ID     string `db:"id"`
		PageID string `db:"page_id"`
		Parent string `db:"parent_block_id"`
		Kind   string `db:"kind"`
	}{}
	if encoded, err := json.Marshal(deleted); err == nil && len(deleted) > 0 {
		_ = app.DB().NewQuery(`SELECT [[container]].[[id]], [[container]].[[page_id]], [[container]].[[parent_block_id]], [[container]].[[kind]]
			FROM json_each({:ids}) AS [[gone]] JOIN {{leafen_database_containers}} AS [[container]] ON [[container]].[[id]] = [[gone]].[[value]]
			ORDER BY [[gone]].[[key]]`).Bind(dbx.Params{"ids": string(encoded)}).All(&containers)
	}
	for _, container := range containers {
		if container.PageID != page.Id {
			continue
		}
		event(eventWebhookDatabaseDeleted, container.ID, containerEntity(container.Kind == databaseContainerKindLinked),
			map[string]any{"parent": databaseParent(container.Parent)})
	}

	// Properties, views and rows.
	ids := make([]string, 0, len(envelope.Databases))
	for id := range envelope.Databases {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if len(ids) > 0 && eventWebhookWantsAny(app, workspaceID, eventWebhookSchemaUpdated, eventWebhookDatabaseSchemaUpdated,
		eventWebhookViewCreated, eventWebhookViewUpdated, eventWebhookViewDeleted) {
		before, err := pageHistoryDatabaseStatesBefore(app, page.Id, version.VersionCursor, ids)
		if err != nil {
			app.Logger().Warn("leafen webhook database states not read", "page", page.Id, "error", err)
			before = nil
		}
		for _, sourceID := range ids {
			previous, current := before[sourceID], envelope.Databases[sourceID]
			if previous == nil || current == nil {
				continue
			}
			if schema := databaseSchemaChanges(previous.Properties, current.Properties); len(schema) > 0 {
				// Only a page that held the source before and after holds
				// its properties.
				if previous.Source != nil && current.Source != nil {
					event(eventWebhookSchemaUpdated, sourceID, "data_source",
						map[string]any{"parent": sourceParent(sourceID), "updated_schema": schema})
					if container := ownDatabaseContainer(app, sourceID); container != "" {
						event(eventWebhookDatabaseSchemaUpdated, container, "database",
							map[string]any{"parent": databaseParent(databaseContainerParentBlock(app, container)), "updated_schema": schema})
					}
				}
			}
			recordDatabaseViewWebhookEvents(app, sourceID, previous.Views, current.Views, event)
		}
	}
	rows := []string{}
	for _, row := range envelope.Rows {
		if row.Change == pageHistoryChangeRowAdded || row.Change == pageHistoryChangeRowRemoved {
			rows = append(rows, row.ID)
		}
	}
	recordDatabaseRowsWebhookEvents(app, workspaceID, rows, input)
	recordDatabaseRowPagesWebhookEvents(app, workspaceID, envelope.Rows, input)
}

// recordFullPageDatabaseWebhookEvent tells of a full-page database sent to
// the trash, back from it, or moved, as a database's event.
func recordFullPageDatabaseWebhookEvent(app core.App, eventType string, page *core.Record, actorID string) {
	container := ""
	err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_containers}}
		WHERE [[page_id]] = {:page} AND [[kind]] = 'source' ORDER BY [[position]], [[id]] LIMIT 1`).
		Bind(dbx.Params{"page": page.Id}).Row(&container)
	if err != nil {
		return
	}
	recordEventWebhookEvent(app, eventWebhookEventInput{
		WorkspaceID: page.GetString("workspace"), Type: eventType, EntityID: container, EntityType: "database",
		PageID: page.Id, ActorID: actorID, Connection: pageHistoryConnectionOf(app),
		Data: map[string]any{"parent": eventWebhookParent(page)},
	})
}

// ownDatabaseContainer is the database that owns a data source.
func ownDatabaseContainer(app core.App, sourceID string) string {
	container := ""
	_ = app.DB().NewQuery(`SELECT [[membership]].[[container_id]] FROM {{leafen_database_container_sources}} [[membership]]
		JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[id]] = [[membership]].[[container_id]]
		WHERE [[membership]].[[data_source_id]] = {:source} AND [[membership]].[[source_container_id]] = ''
			AND [[container]].[[kind]] = 'source'
		ORDER BY [[container]].[[id]] LIMIT 1`).Bind(dbx.Params{"source": sourceID}).Row(&container)
	return container
}

func databaseContainerParentBlock(app core.App, containerID string) string {
	parent := ""
	_ = app.DB().NewQuery(`SELECT [[parent_block_id]] FROM {{leafen_database_containers}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": containerID}).Row(&parent)
	return parent
}

func mapsValues[K comparable, V any](values map[K]V) []V {
	list := make([]V, 0, len(values))
	for _, value := range values {
		list = append(list, value)
	}
	return list
}
