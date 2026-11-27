package app

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's database rows are read in more places than its own room: a row's
// own page open alone or in a peek, and the pages whose graphs carry its
// sources — the linked databases showing them, the relations pointing to them
// and the rollups reaching them. Once a change to a page's graph is committed
// — by a person, another tab, the API, an automation, or the server matching
// a two-way relation's other side — the open rooms reading it are told
// within a moment, as Notion's open views show such a change within two
// seconds (seen 2026-11-02 and 2026-11-11): a row's own page reads its row
// again ("leafenRows"), and a page carrying the changed sources asks for
// their latest changes ("leafenExternal", useExternalGraphRefresh). Before,
// a row's own page never followed its values changed elsewhere, and a page
// carrying another page's rows asked only every twenty seconds.

// graphReaderSettle gathers the changes of a moment into one look.
const graphReaderSettle = 150 * time.Millisecond

type graphReaderWatch struct {
	mu        sync.Mutex
	pages     map[string]struct{}
	scheduled bool
}

var (
	graphReaderWatches sync.Map
	// graphReaderRevision only grows, so a room's readers can tell a new
	// word from one they heard; it starts from the clock so a restarted
	// server's words are newer than the last one's.
	graphReaderRevision atomic.Int64
)

func nextGraphReaderRevision() int64 {
	for {
		last := graphReaderRevision.Load()
		next := max(last+1, time.Now().UnixMilli())
		if graphReaderRevision.CompareAndSwap(last, next) {
			return next
		}
	}
}

// announceGraphReadersAfterCommit tells the open rooms reading a page's
// rows, once the current change is committed, that they changed.
func announceGraphReadersAfterCommit(app core.App, pageID string) {
	if pageID == "" {
		return
	}
	note := func() {
		// The screens drawing the page in their sidebars are told too (workspace_change_graphs.go).
		noteWatchedGraphChange(pageID)
		// The change's own app may be a transaction's, done by the look.
		authorization, ok := app.Store().Get(ygoRoomAuthorizationStoreKey).(*ygoRoomAuthorization)
		if !ok || authorization.server == nil || authorization.app == nil {
			return
		}
		value, _ := graphReaderWatches.LoadOrStore(authorization, &graphReaderWatch{pages: map[string]struct{}{}})
		watch := value.(*graphReaderWatch)
		watch.mu.Lock()
		watch.pages[pageID] = struct{}{}
		if !watch.scheduled {
			watch.scheduled = true
			time.AfterFunc(graphReaderSettle, func() { watch.look(authorization) })
		}
		watch.mu.Unlock()
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				note()
			}
			return nil
		})
		return
	}
	note()
}

// announcePageTreeGraphReadersAfterCommit tells the open rooms reading the
// databases of a page, and of the pages under it, that they changed once the
// current change is committed: as the page goes to the trash or comes back,
// the linked databases showing them elsewhere turn read only or editable
// within a moment, as Notion's do (seen 2026-11-14, within two seconds).
// Rows are not walked; the databases in their bodies are not told.
func announcePageTreeGraphReadersAfterCommit(app core.App, pageID string) error {
	pages := []string{}
	if err := app.DB().NewQuery(`
		WITH RECURSIVE [[tree]]([[id]]) AS (
			SELECT {:pageId}
			UNION
			SELECT [[page]].[[id]] FROM {{leafen_pages}} AS [[page]]
			JOIN [[tree]] ON [[page]].[[parent_id]] = [[tree]].[[id]]
			WHERE [[page]].[[data_source_id]] = ''
		)
		SELECT DISTINCT [[source]].[[page_id]] FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[page_id]] IN (SELECT [[id]] FROM [[tree]])
	`).Bind(dbx.Params{"pageId": pageID}).Column(&pages); err != nil {
		return err
	}
	for _, page := range pages {
		announceGraphReadersAfterCommit(app, page)
	}
	return nil
}

func (watch *graphReaderWatch) look(authorization *ygoRoomAuthorization) {
	watch.mu.Lock()
	changed := watch.pages
	watch.pages = map[string]struct{}{}
	watch.scheduled = false
	watch.mu.Unlock()
	app := authorization.app
	defer func() {
		if recovered := recover(); recovered != nil {
			logRecoveredPanic(app, "graph reader announcement", recovered)
		}
	}()
	if len(changed) == 0 {
		return
	}
	rooms := authorization.server.Rooms()
	if len(rooms) == 0 {
		return
	}
	rowRooms, readers, err := graphReaderRooms(app, rooms)
	if err != nil {
		return
	}
	reading, err := graphReaderPagesReading(app, readers, changed)
	if err != nil {
		return
	}
	revision := nextGraphReaderRevision()
	open := make(map[string]struct{}, len(rooms))
	for _, room := range rooms {
		open[room] = struct{}{}
	}
	for page := range reading {
		if _, isRoom := open[page]; isRoom {
			announceRoomStateAfterCommit(app, page, "leafenExternal", map[string]any{"revision": revision})
		}
	}
	for row, container := range rowRooms {
		_, ownChanged := changed[container]
		_, readsChanged := reading[container]
		if ownChanged || readsChanged {
			announceRoomStateAfterCommit(app, row, "leafenRows", map[string]any{"revision": revision})
		}
	}
}

// graphReaderRooms sorts the open rooms into rows' own pages, by the page
// holding their database, and the pages whose graphs may read other pages:
// the rooms that aren't rows, and the pages holding the open rows.
func graphReaderRooms(app core.App, rooms []string) (map[string]string, []string, error) {
	encoded, err := json.Marshal(rooms)
	if err != nil {
		return nil, nil, err
	}
	rows := []struct {
		ID   string `db:"id"`
		Page string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[page_id]] FROM {{leafen_database_rows}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:rooms}))
	`).Bind(dbx.Params{"rooms": string(encoded)}).All(&rows); err != nil {
		return nil, nil, err
	}
	rowRooms := make(map[string]string, len(rows))
	readers := map[string]struct{}{}
	for _, row := range rows {
		rowRooms[row.ID] = row.Page
		readers[row.Page] = struct{}{}
	}
	for _, room := range rooms {
		if _, isRow := rowRooms[room]; !isRow {
			readers[room] = struct{}{}
		}
	}
	return rowRooms, workspaceExportSortedKeys(readers), nil
}

// graphReaderPagesReading names the pages among readers whose graphs carry
// a source of a changed page, as their own read takes them
// (authorizeLinkedDatabaseReadScope): a linked database showing it, a
// relation of the page's or of a linked database's source pointing to it, a
// page layout's tab showing it, or a rollup reaching it through another
// rollup.
func graphReaderPagesReading(app core.App, readers []string, changed map[string]struct{}) (map[string]struct{}, error) {
	reading := map[string]struct{}{}
	if len(readers) == 0 {
		return reading, nil
	}
	encodedReaders, err := json.Marshal(readers)
	if err != nil {
		return nil, err
	}
	changedPages := workspaceExportSortedKeys(changed)
	encodedChanged, err := json.Marshal(changedPages)
	if err != nil {
		return nil, err
	}
	pages := []string{}
	if err := app.DB().NewQuery(`
		WITH [[changed]] AS (
			SELECT [[id]], [[page_id]] FROM {{leafen_data_sources}}
			WHERE [[page_id]] IN (SELECT [[value]] FROM json_each({:changed}))
		), [[readers]] AS (SELECT [[value]] AS [[page_id]] FROM json_each({:readers}))
		SELECT DISTINCT [[page_id]] FROM (
			SELECT [[member]].[[page_id]] FROM {{leafen_database_container_sources}} AS [[member]]
			WHERE [[member]].[[page_id]] IN (SELECT [[page_id]] FROM [[readers]])
				AND [[member]].[[data_source_id]] IN (SELECT [[id]] FROM [[changed]] WHERE [[changed]].[[page_id]] != [[member]].[[page_id]])
			UNION ALL
			SELECT [[target]].[[page_id]] FROM {{leafen_database_relation_property_targets}} AS [[target]]
			WHERE [[target]].[[page_id]] IN (SELECT [[page_id]] FROM [[readers]])
				AND [[target]].[[target_source_id]] IN (SELECT [[id]] FROM [[changed]] WHERE [[changed]].[[page_id]] != [[target]].[[page_id]])
			UNION ALL
			SELECT [[member]].[[page_id]] FROM {{leafen_database_container_sources}} AS [[member]]
			JOIN {{leafen_database_relation_property_targets}} AS [[target]]
				ON [[target]].[[owner]] = [[member]].[[owner]] AND [[target]].[[source_id]] = [[member]].[[data_source_id]]
			WHERE [[member]].[[page_id]] IN (SELECT [[page_id]] FROM [[readers]])
				AND [[target]].[[target_source_id]] IN (SELECT [[id]] FROM [[changed]] WHERE [[changed]].[[page_id]] != [[member]].[[page_id]])
			UNION ALL
			SELECT [[own]].[[page_id]] FROM {{leafen_database_page_layouts}} AS [[layout]]
			JOIN {{leafen_data_sources}} AS [[own]] ON [[own]].[[id]] = [[layout]].[[data_source_id]]
			JOIN json_each([[layout]].[[layout]], '$.tabs') AS [[tab]]
			WHERE [[own]].[[page_id]] IN (SELECT [[page_id]] FROM [[readers]])
				AND json_extract([[tab]].[[value]], '$.dataSourceId') IN (SELECT [[id]] FROM [[changed]] WHERE [[changed]].[[page_id]] != [[own]].[[page_id]])
		)
	`).Bind(dbx.Params{"changed": string(encodedChanged), "readers": string(encodedReaders)}).Column(&pages); err != nil {
		return nil, err
	}
	for _, page := range pages {
		reading[page] = struct{}{}
	}
	// A rollup reaching further through another rollup carries that source
	// too; only pages with databases can.
	holders := []struct {
		Owner     string `db:"owner"`
		Workspace string `db:"workspace_id"`
		Page      string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[owner]], [[workspace_id]], [[page_id]] FROM {{leafen_database_container_sources}}
		WHERE [[page_id]] IN (SELECT [[value]] FROM json_each({:readers}))
	`).Bind(dbx.Params{"readers": string(encodedReaders)}).All(&holders); err != nil {
		return nil, err
	}
	for _, holder := range holders {
		if _, found := reading[holder.Page]; found {
			continue
		}
		reached, err := loadRollupReachedSources(app, holder.Owner, holder.Workspace, holder.Page)
		if err != nil {
			return nil, err
		}
		for _, source := range reached {
			if _, isChanged := changed[source.SourcePageID]; isChanged && source.SourcePageID != holder.Page {
				reading[holder.Page] = struct{}{}
				break
			}
		}
	}
	return reading, nil
}
