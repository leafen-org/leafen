package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Who opened what (the 2026-10-14 decision, 8; kept everywhere since the
// audit decision of 2026-10-21, 22:42): the server records each page, item
// and file it gives someone, as it gives it — in the app, through the API,
// to an agent's connection, through a share link or in an export — as the
// edit history keeps every edit. Only the server's administrators turn it
// off, for the whole server (administrator settings). A person's reads of
// one thing the same way within ten minutes are one record that counts
// them. The records are the organization's audit events, kept, read back to
// its administrators and exported as the rest of its audit log. Each record
// also notes the places it falls in — the thing itself, and each page
// above it (leafen_read_log_places) — so the people with full access to a
// page read the reads of all that is in it, unless a compliance
// administrator left that to compliance administrators
// (read_log_routes.go); their reading is recorded too.

const (
	readLogAction         = "content.read"
	readLogViewedAction   = "content.read_log.viewed"
	readLogReadersAction  = "content.read_log.readers"
	readLogWatchOnAction  = "content.read_log.watch_on"
	readLogWatchOffAction = "content.read_log.watch_off"
	readLogBucket         = 10 * time.Minute
	readLogTitleRunes     = 200

	contentReadViaApp      = "app"
	contentReadViaAPI      = "api"
	contentReadViaMCP      = "mcp"
	contentReadViaShare    = "share"
	contentReadViaExport   = "export"
	contentReadViaFileLink = "fileLink"

	contentReadOpen = "open"
	contentReadList = "list"

	readLogRecordingKey = "leafenReadLogRecording"
)

// contentReader is who reads, and how: a person (or a share link's
// visitor, with no person) through a way in, and the connection, app or
// link they came through.
type contentReader struct {
	UserID string
	Via    string
	Client string
}

// contentRead is one thing read: a page, a database item or a file, opened
// on its own or listed with others.
type contentRead struct {
	Kind string
	ID   string
	How  string
}

// readLogRecording is the server's switch, on unless its administrators
// turned it off; it is read once and kept until they change it.
func readLogRecording(app core.App) bool {
	if cached, ok := app.Store().Get(readLogRecordingKey).(bool); ok {
		return cached
	}
	var enabled int
	if err := app.DB().NewQuery(`SELECT [[read_log_enabled]] FROM {{leafen_server_settings}} WHERE [[id]] = 'default'`).Row(&enabled); err != nil {
		// Unread, the reads are still kept.
		return true
	}
	app.Store().Set(readLogRecordingKey, enabled == 1)
	return enabled == 1
}

func rememberReadLogRecording(app core.App, enabled bool) {
	app.Store().Set(readLogRecordingKey, enabled)
}

// readPlaces finds the places reads fall in, from the thing itself up: a
// page and the pages above it; a database item, its database's page and
// the pages above that; a file, wherever it is held.
type readPlaces struct {
	app   core.App
	pages map[string][]string
	// rows holds the database items found, nil for an ID that is none.
	rows map[string]*readRow
}

type readRow struct {
	page, title string
}

func newReadPlaces(app core.App) *readPlaces {
	return &readPlaces{app: app, pages: map[string][]string{}, rows: map[string]*readRow{}}
}

// joinPlaces is the places of lists in order, each once, however many: a
// page however deep, and a file however many pages hold it, falls in every
// one above it.
func joinPlaces(lists ...[]string) []string {
	seen := map[string]bool{}
	joined := []string{}
	for _, list := range lists {
		for _, scope := range list {
			if !seen[scope] {
				seen[scope] = true
				joined = append(joined, scope)
			}
		}
	}
	return joined
}

// page is a page and the places above it; a database item's page goes on
// up through its database's page.
func (places *readPlaces) page(pageID string) ([]string, error) {
	if found, known := places.pages[pageID]; known {
		return found, nil
	}
	own := []string{"page:" + pageID}
	// A page met again on its own way up stops there.
	places.pages[pageID] = own
	stored := struct {
		Parent string `db:"parent_id"`
		Source string `db:"data_source_id"`
	}{}
	err := places.app.DB().NewQuery(`SELECT [[parent_id]], [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).One(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var above []string
	if err != nil || stored.Source != "" {
		if err := places.loadRows([]string{pageID}); err != nil {
			return nil, err
		}
		if row := places.rows[pageID]; row != nil && row.page != "" && row.page != pageID {
			if above, err = places.page(row.page); err != nil {
				return nil, err
			}
		}
	} else if stored.Parent != "" {
		if above, err = places.page(stored.Parent); err != nil {
			return nil, err
		}
	}
	scopes := joinPlaces(own, above)
	places.pages[pageID] = scopes
	return scopes, nil
}

// loadRows finds database items at once: the page holding each one's
// database and its title.
func (places *readPlaces) loadRows(rowIDs []string) error {
	wanted := []string{}
	for _, id := range rowIDs {
		if _, known := places.rows[id]; !known {
			wanted = append(wanted, id)
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	encoded, _ := json.Marshal(wanted)
	found := []struct {
		RowID  string `db:"id"`
		PageID string `db:"page_id"`
		Title  string `db:"title"`
	}{}
	if err := places.app.DB().NewQuery(`
		SELECT [[row]].[[id]], [[row]].[[page_id]],
			COALESCE((SELECT [[title]].[[value]] FROM {{leafen_database_row_values}} [[title]]
				WHERE [[title]].[[row_id]] = [[row]].[[id]] AND [[title]].[[type]] = 'title' LIMIT 1), '') AS [[title]]
		FROM {{leafen_database_rows}} [[row]] WHERE [[row]].[[id]] IN (SELECT [[value]] FROM json_each({:rows}))
	`).Bind(dbx.Params{"rows": string(encoded)}).All(&found); err != nil {
		return err
	}
	for _, id := range wanted {
		places.rows[id] = nil
	}
	for _, row := range found {
		places.rows[row.RowID] = &readRow{page: row.PageID, title: databaseRowTitleText(row.Title)}
	}
	return nil
}

// file is every place that holds a file: each of its references, not the
// first few.
func (places *readPlaces) file(fileID string) ([]string, error) {
	references := []struct {
		PageID    string `db:"page_id"`
		OwnerKind string `db:"owner_kind"`
		OwnerID   string `db:"owner_id"`
	}{}
	if err := places.app.DB().NewQuery(`
		SELECT [[page_id]], [[owner_kind]], [[owner_id]] FROM {{leafen_file_references}}
		WHERE [[file_id]] = {:fileId} ORDER BY [[page_id]], [[owner_kind]], [[owner_id]]
	`).Bind(dbx.Params{"fileId": fileID}).All(&references); err != nil {
		return nil, err
	}
	scopes := []string{}
	for _, reference := range references {
		holder := reference.PageID
		if reference.OwnerKind == productFileReferenceOwnerRow {
			holder = reference.OwnerID
		}
		found, err := places.page(holder)
		if err != nil {
			return nil, err
		}
		scopes = joinPlaces(scopes, found)
	}
	return scopes, nil
}

// of is where a read falls; a scope read (a whole workspace's export) is
// in the scope it names.
func (places *readPlaces) of(read contentRead) ([]string, error) {
	switch read.Kind {
	case "row", "page":
		return places.page(read.ID)
	case "file":
		return places.file(read.ID)
	case "scope":
		if kind, id, _ := strings.Cut(read.ID, ":"); id != "" && kind == "page" {
			return []string{read.ID}, nil
		}
	}
	return nil, nil
}

func (places *readPlaces) title(read contentRead) string {
	app := places.app
	title := ""
	switch read.Kind {
	case "row":
		if row := places.rows[read.ID]; row != nil {
			title = row.title
		}
	case "page":
		_ = app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": read.ID}).Row(&title)
	case "file":
		_ = app.DB().NewQuery(`SELECT [[original_name]] FROM {{leafen_files}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": read.ID}).Row(&title)
	}
	title = strings.TrimSpace(title)
	if runes := []rune(title); len(runes) > readLogTitleRunes {
		title = string(runes[:readLogTitleRunes])
	}
	return title
}

// recordReadLogEvent records something done with a scope's read log:
// reading it (once per ten minutes, counted) or choosing who reads it.
func recordReadLogEvent(app core.App, organizationID, workspaceID, actorID, action, scope string, extra map[string]any, now time.Time) error {
	key := strconv.FormatInt(now.UnixNano(), 10)
	if action == readLogViewedAction {
		key = strconv.FormatInt(now.UnixMilli()/readLogBucket.Milliseconds(), 10)
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{"leafen-read-log", organizationID, actorID, action, scope, key}, "\x00")))
	values := map[string]any{"scope": scope, "count": 1, "lastAt": now.UnixMilli()}
	for name, value := range extra {
		values[name] = value
	}
	metadata, _ := json.Marshal(values)
	id := hex.EncodeToString(digest[:16])
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_organization_audit_events}}
			([[id]], [[organization_id]], [[actor_id]], [[action]], [[subject_type]], [[subject_id]], [[created]], [[workspace_id]], [[metadata_json]])
		VALUES ({:id}, {:organizationId}, {:actorId}, {:action}, 'scope', {:scope}, {:now}, {:workspaceId}, {:metadata})
		ON CONFLICT ([[id]]) DO UPDATE SET [[metadata_json]] = json_set([[metadata_json]],
			'$.count', COALESCE(json_extract([[metadata_json]], '$.count'), 1) + 1, '$.lastAt', excluded.[[created]])
	`).Bind(dbx.Params{
		"id": id, "organizationId": organizationID, "actorId": actorID, "action": action,
		"scope": scope, "now": now.UnixMilli(), "workspaceId": workspaceID, "metadata": string(metadata),
	}).Execute(); err != nil {
		return err
	}
	return writeReadLogPlaces(app, map[string][]string{id: {scope}})
}

// writeReadLogPlaces notes the places of records already written, at each
// record's own time.
func writeReadLogPlaces(app core.App, places map[string][]string) error {
	pairs := [][2]string{}
	for id, scopes := range places {
		for _, scope := range scopes {
			pairs = append(pairs, [2]string{id, scope})
		}
	}
	if len(pairs) == 0 {
		return nil
	}
	encoded, _ := json.Marshal(pairs)
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_read_log_places}} ([[workspace_id]], [[scope]], [[created]], [[event_id]])
		SELECT [[event]].[[workspace_id]], json_extract([[pair]].[[value]], '$[1]'), [[event]].[[created]], [[event]].[[id]]
		FROM json_each({:pairs}) [[pair]]
		JOIN {{leafen_organization_audit_events}} [[event]] ON [[event]].[[id]] = json_extract([[pair]].[[value]], '$[0]')
		WHERE [[event]].[[workspace_id]] != ''
		ON CONFLICT DO NOTHING
	`).Bind(dbx.Params{"pairs": string(encoded)}).Execute()
	return err
}
