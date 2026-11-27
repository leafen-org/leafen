//go:build !leafen_noapps

package app

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What an app did for a person, and undoing it (docs/app-platform.md 10):
// the person's runs of the app's actions in an organization, newest first
// and a window at a time to the first — from its screen, an agent or an
// automation — each with the pages the run changed (the versions it made,
// appRunVersionScope) to revert one by one in their history and the
// comments it left, both read on through the run, an undo action when the
// action declares one (it gets the original input), and a mark for what
// reached outside Leafen and can't be undone here.

const (
	// A window of the person's runs, and of one run's pages and comments;
	// each list reads on from its cursor ("더 보기" on the screen).
	appRunHistoryPerWindow = 30
	appRunHistoryPages     = 20
	appRunHistoryComments  = 20
	// A window of a run's pages looks at this many of them at most to fill
	// itself, past the rows' pages it leaves out.
	appRunHistoryPagesRead = 200
	// A database page names this many of the rows a run changed, and counts all.
	appRunHistoryRowNames = 20
	appRunUndoInputBytes  = 64 << 10
	appRunPageGraceMillis = 2000
)

// appRunConnection is the connection a run's changes come through: the
// person's own for a run as them, else the app account's in the organization.
func appRunConnection(app core.App, install storedAppInstall, run appRun) string {
	if run.Person != nil {
		return run.Person.ConnectionID
	}
	var connection string
	_ = app.DB().NewQuery(`SELECT [[account_connection_id]] FROM {{leafen_app_workspaces}} WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy}`).
		Bind(dbx.Params{"install": install.ID, "tenancy": run.Tenancy}).Row(&connection)
	return connection
}

// appRunUndoInput keeps an action's input when the action declares an undo.
func appRunUndoInput(manifest appManifest, run appRun) string {
	if run.Trigger != "action" && run.Trigger != "tool" && run.Trigger != appQueueAutomation {
		return ""
	}
	action, found := appManifestActionByKey(manifest, run.Ref)
	if !found || action.Undo == "" {
		return ""
	}
	call := struct {
		Input json.RawMessage `json:"input"`
	}{}
	if json.Unmarshal(run.Input, &call) != nil || len(call.Input) == 0 || len(call.Input) > appRunUndoInputBytes {
		return ""
	}
	return string(call.Input)
}

// storedAppRunHistory is one of the person's runs as their list reads it.
type storedAppRunHistory struct {
	ID         string `db:"id"`
	Trigger    string `db:"trigger"`
	Ref        string `db:"trigger_ref"`
	Status     string `db:"status"`
	Created    int64  `db:"created"`
	Finished   int64  `db:"finished"`
	Connection string `db:"connection_id"`
	UndoInput  string `db:"undo_input"`
	Place      string `db:"place"`
	Effects    string `db:"effects"`
	Writes     int    `db:"writes"`
	// Whether the run recorded the versions it made (page_history_app_runs.go).
	PagesRecorded int `db:"pages_recorded"`
}

// The person's runs of the app's actions in a tenancy, newest first.
const appRunHistorySQL = `SELECT [[id]], [[trigger]], [[trigger_ref]], [[status]], [[created]], [[finished]], [[connection_id]], [[undo_input]],
		[[place]], [[effects]], [[writes]], [[pages_recorded]]
	FROM {{leafen_app_runs}} WHERE [[install_id]] = {:install} AND [[subject]] = {:user} AND [[tenancy]] = {:tenancy}
		AND [[trigger]] IN ('action', 'tool', 'automation')`

// appRunHistoryRequest is who asks for an app's runs and in which tenancy —
// signed in or through their key, never an app's token.
func appRunHistoryRequest(event *core.RequestEvent) (*core.Record, storedAppInstall, appManifest, string, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return nil, storedAppInstall{}, appManifest{}, "", false
	}
	if appTokenCall(event) {
		_ = apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "An app can't see what apps did.")
		return nil, storedAppInstall{}, appManifest{}, "", false
	}
	install, err := loadAppInstall(event.App, event.Request.PathValue("installId"))
	if err != nil {
		_ = appPlatformFailure(event, err)
		return nil, storedAppInstall{}, appManifest{}, "", false
	}
	_, manifest, err := loadAppPackage(event.App, install.PackageHash)
	if err != nil {
		_ = appPlatformFailure(event, err)
		return nil, storedAppInstall{}, appManifest{}, "", false
	}
	return user, install, manifest, event.Request.URL.Query().Get("tenancy"), true
}

func registerAppRunHistoryRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPlatformRunHistoryPath, listAppRunHistoryRoute)
	event.Router.GET(apiRoot+appPlatformRunChangesPath, listAppRunChangesRoute)
}

// listAppRunHistoryRoute shows what an app did for the person, a window of
// runs from the cursor on.
func listAppRunHistoryRoute(event *core.RequestEvent) error {
	user, install, manifest, tenancy, ok := appRunHistoryRequest(event)
	if !ok {
		return nil
	}
	after, err := decodeTimeIDCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, "Give the nextCursor of the last list, or none to start.")
	}
	rows := []storedAppRunHistory{}
	if err := event.App.DB().NewQuery(appRunHistorySQL + `
			AND ({:afterId} = '' OR [[created]] < {:afterAt} OR ([[created]] = {:afterAt} AND [[id]] > {:afterId}))
		ORDER BY [[created]] DESC, [[id]] LIMIT {:limit}`).
		Bind(dbx.Params{"install": install.ID, "user": user.Id, "tenancy": tenancy, "afterAt": after.At, "afterId": after.ID,
			"limit": appRunHistoryPerWindow + 1}).All(&rows); err != nil {
		return appPlatformFailure(event, err)
	}
	rows, next := appWindowOf(rows, appRunHistoryPerWindow, func(row storedAppRunHistory) (int64, string) { return row.Created, row.ID })
	history := appRunHistoryList{Runs: []appRunHistoryWire{}, NextCursor: next}
	// A connection's next run starts where this one's changes end, newest
	// first; the first of a connection's runs in a later window finds its
	// next run in the windows before.
	nextRun := map[string]int64{}
	for _, row := range rows {
		wire := appRunHistoryWire{ID: row.ID, Trigger: row.Trigger, Place: row.Place, Action: row.Ref, ActionName: map[string]string{}, Status: row.Status,
			Created: row.Created, Pages: []appRunPageWire{}, Writes: row.Writes}
		if action, found := appManifestActionByKey(manifest, row.Ref); found {
			wire.ActionName, wire.ExternalEffect = appTextOrEmpty(action.Name), action.ExternalEffect
			if action.Undo != "" && row.UndoInput != "" && row.Status == "ok" {
				var input any
				if json.Unmarshal([]byte(row.UndoInput), &input) == nil {
					wire.UndoAction, wire.UndoInput = action.Undo, input
				}
			}
		}
		newer, known := nextRun[row.Connection]
		if !known && after.ID != "" {
			newer = appRunNewerOnConnection(event.App, install.ID, user.Id, tenancy, row)
		}
		nextRun[row.Connection] = row.Created
		changes, err := appRunChanges(event.App, user.Id, row, appRunThrough(row, newer), appRunChangesCursor{})
		if err != nil {
			return appPlatformFailure(event, err)
		}
		wire.Pages, wire.Comments, wire.MoreCursor = changes.Pages, changes.Comments, changes.NextCursor
		history.Runs = append(history.Runs, wire)
	}
	return event.JSON(http.StatusOK, history)
}

// listAppRunChangesRoute reads on through one of the person's runs: the next
// window of the pages it changed and the comments it left.
func listAppRunChangesRoute(event *core.RequestEvent) error {
	user, install, _, tenancy, ok := appRunHistoryRequest(event)
	if !ok {
		return nil
	}
	cursor, err := decodeAppRunChangesCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, "Give the run's moreCursor or the last nextCursor.")
	}
	row := storedAppRunHistory{}
	if err := event.App.DB().NewQuery(appRunHistorySQL + ` AND [[id]] = {:run}`).
		Bind(dbx.Params{"install": install.ID, "user": user.Id, "tenancy": tenancy, "run": event.Request.PathValue("runId")}).One(&row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apiError(event, http.StatusNotFound, errorCodeAppPlatformInputInvalid, "This run isn't one of yours.")
		}
		return appPlatformFailure(event, err)
	}
	through := appRunThrough(row, appRunNewerOnConnection(event.App, install.ID, user.Id, tenancy, row))
	changes, err := appRunChanges(event.App, user.Id, row, through, cursor)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, changes)
}

// appRunNewerOnConnection is when the person's next run on the same
// connection started, as their list orders runs, or 0 when none did.
func appRunNewerOnConnection(app core.App, installID, userID, tenancy string, row storedAppRunHistory) int64 {
	if row.Connection == "" {
		return 0
	}
	var newer int64
	_ = app.DB().NewQuery(`SELECT [[created]] FROM {{leafen_app_runs}} WHERE [[install_id]] = {:install} AND [[subject]] = {:user} AND [[tenancy]] = {:tenancy}
			AND [[trigger]] IN ('action', 'tool', 'automation') AND [[connection_id]] = {:connection}
			AND ([[created]] > {:at} OR ([[created]] = {:at} AND [[id]] < {:id}))
		ORDER BY [[created]], [[id]] DESC LIMIT 1`).
		Bind(dbx.Params{"install": installID, "user": userID, "tenancy": tenancy, "connection": row.Connection, "at": row.Created, "id": row.ID}).
		Row(&newer)
	return newer
}

// appRunThrough is when a run's changes end: when it finished (or now, while
// it runs) and a moment more, but before the connection's next run started.
func appRunThrough(row storedAppRunHistory, newer int64) int64 {
	finished := row.Finished
	if finished == 0 {
		finished = time.Now().UnixMilli()
	}
	through := finished + appRunPageGraceMillis
	// A run that recorded its versions has its own, whenever another began.
	if newer > 0 && newer-1 < through && row.PagesRecorded == 0 {
		through = newer - 1
	}
	return through
}

// appRunChangesCursor is where a run's lists go on: its pages' cursor ("" once
// read to the last) and the place in its effects its comments go on from (0
// once read to the last). The first window comes with the run, from the zero
// cursor; one the run gave says it started.
type appRunChangesCursor struct {
	Pages    string `json:"pages,omitempty"`
	Comments int    `json:"comments,omitempty"`
	Started  bool   `json:"started,omitzero"`
}

func decodeAppRunChangesCursor(raw string) (appRunChangesCursor, error) {
	cursor := appRunChangesCursor{}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || !cursor.Started || cursor.Comments < 0 {
		return appRunChangesCursor{}, errListWindowCursor
	}
	if _, err := decodeTimeIDCursor(cursor.Pages); err != nil {
		return appRunChangesCursor{}, err
	}
	return cursor, nil
}

// appRunChanges is a window of what a run changed in Leafen: the pages it
// changed through its connection, to revert, and the comments it left, from
// the cursor on (the first window from the zero cursor); the next cursor is
// "" once both are read to the last.
func appRunChanges(app core.App, userID string, row storedAppRunHistory, through int64, cursor appRunChangesCursor) (appRunChangesWindow, error) {
	changes := appRunChangesWindow{Pages: []appRunPageWire{}, Comments: []appRunCommentWire{}}
	next := appRunChangesCursor{Started: true}
	if appRunScopeOf(row, through).reaches() && (!cursor.Started || cursor.Pages != "") {
		after, err := decodeTimeIDCursor(cursor.Pages)
		if err != nil {
			return changes, err
		}
		if changes.Pages, next.Pages, err = appRunPagesWindow(app, userID, row, through, after); err != nil {
			return changes, err
		}
	}
	if !cursor.Started || cursor.Comments > 0 {
		changes.Comments, next.Comments = appRunComments(app, userID, row.Effects, cursor.Comments)
	}
	if next.Pages != "" || next.Comments > 0 {
		encoded, _ := json.Marshal(next)
		changes.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return changes, nil
}

// appRunPagesWindow is a window of the pages a run changed, in the order it
// first changed them, from after on, each with the version to revert from. A
// row's page its database's page names isn't one of them, so a window looks
// at up to appRunHistoryPagesRead of the run's pages to fill itself, and goes
// on from the last it looked at.
func appRunPagesWindow(app core.App, userID string, row storedAppRunHistory, through int64, after timeIDCursor) ([]appRunPageWire, string, error) {
	pages := []storedAppRunPage{}
	// The first version each page got from the run (appRunVersionScope). A
	// version's ID is random, so the first is the lowest cursor (SQLite
	// takes the bare columns from the row MIN chose); the lowest ID
	// reverted from a version in the middle (2026-10-16).
	scope := appRunScopeOf(row, through)
	params := dbx.Params{"afterAt": after.At, "afterId": after.ID, "limit": appRunHistoryPagesRead + 1}
	if err := app.DB().NewQuery(`SELECT [[version]].[[page_id]], [[version]].[[id]] AS [[version_id]], MIN([[version]].[[version_cursor]]) AS [[cursor]],
			MIN([[version]].[[created_at]]) AS [[first]], COALESCE([[page]].[[title]], '') AS [[title]]
		FROM {{leafen_page_history_versions}} [[version]] LEFT JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[version]].[[page_id]]
		WHERE ` + scope.where(params) + `
		GROUP BY [[version]].[[page_id]]
		HAVING {:afterId} = '' OR MIN([[version]].[[created_at]]) > {:afterAt}
			OR (MIN([[version]].[[created_at]]) = {:afterAt} AND [[version]].[[page_id]] > {:afterId})
		ORDER BY MIN([[version]].[[created_at]]), [[version]].[[page_id]] LIMIT {:limit}`).
		Bind(params).All(&pages); err != nil {
		return nil, "", err
	}
	wires := []appRunPageWire{}
	named := appRunNamedRows{app: app, userID: userID, scope: scope, ids: map[string][]string{}, sets: map[string]map[string]bool{}}
	for index, page := range pages {
		if len(wires) == appRunHistoryPages || index == appRunHistoryPagesRead {
			last := pages[index-1]
			return wires, encodeTimeIDCursor(last.First, last.PageID), nil
		}
		if named.under(page.PageID) && !named.bodyChanged(page.PageID) {
			continue
		}
		if _, err := authorizePageBody(app, userID, page.PageID, permissionWorkspaceRead); err != nil {
			continue
		}
		// A database's rows the run changed are named, not only the page
		// that holds the database (2026-10-16): the first few by name, and
		// how many there are.
		ids := named.of(page.PageID)
		rows := appRunChangedRowNames(app, ids)
		wire := appRunPageWire{PageID: page.PageID, Title: page.Title, VersionID: page.Version, Through: through, Rows: rows,
			Reverted: pageHistoryVersionRevertedBy(app, userID, page.PageID, page.Version)}
		if count := len(ids); count > len(rows) {
			wire.RowCount = &count
		}
		// Its kind names it while untitled (page_name_kind.go).
		kinds, err := loadPageNameKinds(app, []string{page.PageID})
		if err != nil {
			return nil, "", err
		}
		if kind := kinds[page.PageID]; kind.Kind == pageKindDatabase {
			wire.PageKind, wire.ViewBlock, wire.SourceTitles = kind.Kind, kind.ViewBlock, kind.SourceTitles
		}
		wires = append(wires, wire)
	}
	return wires, "", nil
}

// storedAppRunPage is a page a run changed: the first version it made of it.
type storedAppRunPage struct {
	PageID  string `db:"page_id"`
	Version string `db:"version_id"`
	Cursor  int64  `db:"cursor"`
	First   int64  `db:"first"`
	Title   string `db:"title"`
}

// appRunNamedRows finds the rows a run's database pages name. A row's own
// page is left out where its database's page — the page whose history keeps
// a row's changes — names the row among those the run changed: both are the
// one change, offered once, under the database. Whichever of the run's
// windows the database's page falls in, every window leaves the row's page
// out, so none repeats it. A run that wrote the page's body too keeps it, as
// that is taken back on the page.
type appRunNamedRows struct {
	app    core.App
	userID string
	scope  appRunVersionScope
	// Each page's changed rows as looked up, in order and as a set; nil for
	// a database page the person can't read, which no window lists.
	ids  map[string][]string
	sets map[string]map[string]bool
}

// of is the rows the run changed in a page's databases, in the order it
// first changed them.
func (named appRunNamedRows) of(pageID string) []string {
	if ids, known := named.ids[pageID]; known {
		return ids
	}
	ids := appRunChangedRowIDs(named.app, pageID, named.scope)
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	named.ids[pageID], named.sets[pageID] = ids, set
	return ids
}

// under is whether a page is a row the run changed that a database page
// names: its own database's, wherever that page is listed, or one this
// window has looked at.
func (named appRunNamedRows) under(pageID string) bool {
	for _, rows := range named.sets {
		if rows[pageID] {
			return true
		}
	}
	database := ""
	if err := named.app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:row}`).
		Bind(dbx.Params{"row": pageID}).Row(&database); err != nil || database == "" || database == pageID {
		return false
	}
	if _, known := named.ids[database]; known {
		return false
	}
	if _, err := authorizePageBody(named.app, named.userID, database, permissionWorkspaceRead); err != nil {
		named.ids[database], named.sets[database] = nil, nil
		return false
	}
	named.of(database)
	return named.sets[database][pageID]
}

// bodyChanged is whether the run wrote a page's body, not only its values.
func (named appRunNamedRows) bodyChanged(pageID string) bool {
	body, params := 0, dbx.Params{"page": pageID}
	_ = named.app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_page_history_events}} [[event]]
		JOIN {{leafen_page_history_versions}} [[version]] ON [[version]].[[page_id]] = [[event]].[[page_id]] AND [[version]].[[version_cursor]] = [[event]].[[version_cursor]]
		WHERE [[event]].[[page_id]] = {:page} AND [[event]].[[kind]] != 'metadata' AND ` + named.scope.where(params)).Bind(params).Row(&body)
	return body > 0
}
