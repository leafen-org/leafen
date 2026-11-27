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
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A scope's read log as the people with full access to it read it, or only
// the organization's compliance administrators where one of them left it
// to them, for the scope or a place above it (read_log.go). A scope is
// "page:<id>". Compliance administrators read any scope's.

var (
	errReadLogScope          = errors.New("read log scope invalid")
	errReadLogComplianceOnly = errors.New("read log compliance only")
)

const (
	readLogPageSize          = 50
	readLogReadersAll        = "fullAccess"
	readLogReadersCompliance = "compliance"
)

func registerReadLogRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+readLogListPath, readReadLog)
	event.Router.PUT(apiRoot+readLogReadersPath, setReadLogReaders).Bind(apis.BodyLimit(8 << 10))
}

type readLogScope struct {
	Kind           string
	ID             string
	Name           string
	WorkspaceID    string
	OrganizationID string
	// Compliance is a compliance administrator of the workspace's organization.
	Compliance bool
}

func (scope readLogScope) key() string { return scope.Kind + ":" + scope.ID }

// authorizeReadLogScope is a scope of the workspace someone holds full
// access to, or any of its scopes for a compliance administrator: a page of
// the workspace.
func authorizeReadLogScope(app core.App, principalID, workspaceID, scope string) (readLogScope, error) {
	kind, id, _ := strings.Cut(strings.TrimSpace(scope), ":")
	resolved := readLogScope{Kind: kind, ID: id, WorkspaceID: workspaceID}
	if id == "" || kind != "page" {
		return resolved, errReadLogScope
	}
	if err := app.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": workspaceID}).Row(&resolved.OrganizationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return resolved, permissionDenied()
		}
		return resolved, err
	}
	if resolved.OrganizationID != "" {
		_, err := authorizeOrganization(app, principalID, resolved.OrganizationID, permissionOrganizationAuditLog)
		if err != nil && !isPermissionDenied(err) {
			return resolved, err
		}
		resolved.Compliance = err == nil
	}
	authorization, err := authorizePage(app, principalID, id, permissionWorkspaceRead)
	if err != nil && !(isPermissionDenied(err) && resolved.Compliance) {
		return resolved, err
	}
	if err == nil && authorization.Workspace.WorkspaceID == workspaceID && authorization.AccessRole >= pageAccessRoleFullAccess {
		resolved.Name = strings.TrimSpace(authorization.Page.GetString("title"))
		return resolved, nil
	}
	if !resolved.Compliance {
		return resolved, permissionDenied()
	}
	if err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_pages}} WHERE [[id]] = {:id} AND [[workspace]] = {:workspaceId}`).
		Bind(dbx.Params{"id": id, "workspaceId": workspaceID}).Row(&resolved.Name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return resolved, permissionDenied()
		}
		return resolved, err
	}
	resolved.Name = strings.TrimSpace(resolved.Name)
	return resolved, nil
}

// readLogReadersOf is who reads a scope's log: the readers set for it or
// for the nearest place above it that has them, and that place.
func readLogReadersOf(app core.App, scope readLogScope) (string, string, error) {
	above, err := newReadPlaces(app).page(scope.ID)
	if err != nil {
		return "", "", err
	}
	chain := joinPlaces([]string{scope.key()}, above)
	encoded, _ := json.Marshal(chain)
	set := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[scope]] FROM {{leafen_read_log_readers}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[scope]] IN (SELECT [[value]] FROM json_each({:chain}))
	`).Bind(dbx.Params{"workspaceId": scope.WorkspaceID, "chain": string(encoded)}).Column(&set); err != nil {
		return "", "", err
	}
	for _, place := range chain {
		for _, restricted := range set {
			if restricted == place {
				return readLogReadersCompliance, place, nil
			}
		}
	}
	return readLogReadersAll, "", nil
}

// readLogPlaceName is the name of a page.
func readLogPlaceName(app core.App, place string) string {
	_, id, _ := strings.Cut(place, ":")
	var name string
	_ = app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": id}).Row(&name)
	if strings.TrimSpace(name) == "" {
		name = databaseRowPageTitle(app, id)
	}
	return strings.TrimSpace(name)
}

type readLogCursor struct {
	At int64  `json:"at"`
	ID string `json:"id"`
}

func readReadLog(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	scope, err := authorizeReadLogScope(event.App, user.Id, workspaceID, event.Request.URL.Query().Get("scope"))
	if err != nil {
		return readLogRouteError(event, err, "authorize")
	}
	readers, readersFrom, err := readLogReadersOf(event.App, scope)
	if err != nil {
		return readLogRouteError(event, err, "readers")
	}
	if readers == readLogReadersCompliance && !scope.Compliance {
		return readLogRouteError(event, errReadLogComplianceOnly, "readers")
	}
	cursor := readLogCursor{}
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("cursor")); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || json.Unmarshal(decoded, &cursor) != nil {
			return readLogRouteError(event, errReadLogScope, "cursor")
		}
	}
	now := time.Now().UTC()
	policy, err := loadOrganizationAuditRetentionPolicy(event.App, scope.OrganizationID)
	if err != nil {
		return readLogRouteError(event, err, "retention")
	}
	key := scope.key()
	recording := readLogRecording(event.App)
	// Reading the log is itself recorded, before it is read.
	if recording && scope.OrganizationID != "" {
		if err := recordReadLogEvent(event.App, scope.OrganizationID, workspaceID, user.Id, readLogViewedAction, key, nil, now); err != nil {
			return readLogRouteError(event, err, "record")
		}
	}
	// Reads gathered in the last second show too (read_log_buffer.go).
	flushReadLog(event.App)
	response := readLogResponse{Scope: key, Name: scope.Name, Recording: recording, RetentionDays: policy.RetentionDays,
		Readers: readers, CanChooseReaders: scope.Compliance, Events: []readLogEventWire{}}
	if readersFrom != "" && readersFrom != key {
		response.ReadersFrom = readLogPlaceName(event.App, readersFrom)
	}
	cutoff := int64(0)
	if policy.RetentionDays > 0 {
		cutoff = now.AddDate(0, 0, -policy.RetentionDays).UnixMilli()
	}
	rows := []struct {
		ID       string `db:"id"`
		ActorID  string `db:"actor_id"`
		Action   string `db:"action"`
		Subject  string `db:"subject_type"`
		Target   string `db:"subject_id"`
		Created  int64  `db:"created"`
		Metadata string `db:"metadata_json"`
		Name     string `db:"name"`
	}{}
	if err := event.App.DB().NewQuery(`
		SELECT [[event]].[[id]], [[event]].[[actor_id]], [[event]].[[action]], [[event]].[[subject_type]], [[event]].[[subject_id]],
			[[event]].[[created]], [[event]].[[metadata_json]],
			COALESCE(NULLIF(trim([[user]].[[name]]), ''), [[user]].[[email]], '') AS [[name]]
		FROM {{leafen_read_log_places}} [[place]]
		JOIN {{leafen_organization_audit_events}} [[event]] ON [[event]].[[id]] = [[place]].[[event_id]]
		LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[event]].[[actor_id]]
		WHERE [[place]].[[workspace_id]] = {:workspaceId} AND [[place]].[[scope]] = {:scope} AND [[place]].[[created]] >= {:cutoff}
			AND ({:cursorAt} = 0 OR [[place]].[[created]] < {:cursorAt} OR ([[place]].[[created]] = {:cursorAt} AND [[place]].[[event_id]] < {:cursorId}))
		ORDER BY [[place]].[[created]] DESC, [[place]].[[event_id]] DESC LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "scope": key, "cutoff": cutoff,
		"cursorAt": cursor.At, "cursorId": cursor.ID, "limit": readLogPageSize + 1,
	}).All(&rows); err != nil {
		return readLogRouteError(event, err, "list")
	}
	if len(rows) > readLogPageSize {
		rows = rows[:readLogPageSize]
		last := rows[len(rows)-1]
		encoded, _ := json.Marshal(readLogCursor{At: last.Created, ID: last.ID})
		next := base64.RawURLEncoding.EncodeToString(encoded)
		response.NextCursor = next
	}
	for _, row := range rows {
		metadata := struct {
			Via    string `json:"via"`
			How    string `json:"how"`
			Client string `json:"client"`
			Title  string `json:"title"`
			Count  int    `json:"count"`
			LastAt int64  `json:"lastAt"`
			To     string `json:"to"`
		}{}
		_ = json.Unmarshal([]byte(row.Metadata), &metadata)
		kind := map[string]string{readLogAction: "read", readLogViewedAction: "viewedLog", readLogWatchOnAction: "watchOn", readLogWatchOffAction: "watchOff"}[row.Action]
		if row.Action == readLogReadersAction {
			kind = "readersFullAccess"
			if metadata.To == readLogReadersCompliance {
				kind = "readersCompliance"
			}
		}
		if kind == "" {
			continue
		}
		reader := row.ActorID
		if strings.HasPrefix(reader, "share:") {
			reader = ""
		}
		response.Events = append(response.Events, readLogEventWire{
			ID: row.ID, Kind: kind, At: row.Created, LastAt: max(metadata.LastAt, row.Created), Count: max(metadata.Count, 1),
			ReaderID: reader, ReaderName: row.Name, Via: metadata.Via, Client: metadata.Client, How: metadata.How,
			TargetKind: row.Subject, TargetID: row.Target, Title: metadata.Title,
		})
	}
	readLogTitlesAsRead(event.App, user.Id, scope, &response)
	return event.JSON(http.StatusOK, response)
}

// readLogTitlesAsRead puts the log's page name and each page read in it as
// its reader reads them, with their spans while they hold a mention, for a
// plain title to say each one (page_title_spans.go). An event keeps the
// title its page had then, which takes the page's spans only while they
// spell it.
func readLogTitlesAsRead(app core.App, readerID string, scope readLogScope, response *readLogResponse) {
	titles := map[string]*string{}
	if scope.ID != "" {
		titles[scope.ID] = &response.Name
	}
	for index := range response.Events {
		event := &response.Events[index]
		if event.TargetKind == "page" && event.TargetID != "" && titles[event.TargetID] == nil {
			titles[event.TargetID] = &event.Title
		}
	}
	before := map[string]string{}
	for id, title := range titles {
		before[id] = *title
	}
	pageTitlesAsRead(app, readerID, titles, true)
	spans := pageTitlesSpansAsRead(app, readerID, titles)
	response.NameSpans = spans[scope.ID]
	// Each page's kind names it while untitled (page_name_kind.go): a
	// full-page database "새 데이터베이스".
	ids := make([]string, 0, len(titles))
	for id := range titles {
		ids = append(ids, id)
	}
	if kinds, err := loadPageNameKinds(app, ids); err == nil {
		if kind := kinds[scope.ID]; kind.Kind == pageKindDatabase {
			response.PageKind, response.ViewBlock, response.SourceTitles = kind.Kind, kind.ViewBlock, kind.SourceTitles
		}
		for index := range response.Events {
			event := &response.Events[index]
			if kind := kinds[event.TargetID]; event.TargetKind == "page" && kind.Kind == pageKindDatabase {
				event.PageKind, event.ViewBlock, event.SourceTitles = kind.Kind, kind.ViewBlock, kind.SourceTitles
			}
		}
	}
	for index := range response.Events {
		event := &response.Events[index]
		if event.TargetKind != "page" || titles[event.TargetID] == &event.Title {
			if event.TargetKind == "page" {
				event.TitleSpans = spans[event.TargetID]
			}
			continue
		}
		// Another event of the same page, with the same title then, reads as that one.
		if event.Title == before[event.TargetID] {
			event.Title, event.TitleSpans = *titles[event.TargetID], spans[event.TargetID]
		}
	}
}

// setReadLogReaders is a compliance administrator choosing who reads a
// scope's log, the choice recorded in it and in the audit log.
func setReadLogReaders(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := readLogReadersRequest{}
	if err := event.BindBody(&input); err != nil {
		return readLogRouteError(event, errReadLogScope, "body")
	}
	// One left out is told so, and readers by what it takes (api_missing_input.go).
	if strings.TrimSpace(input.Scope) == "" {
		return refuseMissingInput(event, errorCodeReadLogInvalid, "scope", "the page whose read log it is, as page:<id>.")
	}
	if input.Readers == "" {
		return refuseMissingInput(event, errorCodeReadLogInvalid, "readers", "fullAccess or compliance.")
	}
	if input.Readers != readLogReadersAll && input.Readers != readLogReadersCompliance {
		return apiError(event, http.StatusBadRequest, errorCodeReadLogInvalid, "Check readers: fullAccess or compliance.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	scope, err := authorizeReadLogScope(event.App, user.Id, workspaceID, input.Scope)
	if err != nil {
		return readLogRouteError(event, err, "authorize")
	}
	if !scope.Compliance {
		return readLogRouteError(event, errReadLogComplianceOnly, "readers")
	}
	now := time.Now().UTC()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		var result sql.Result
		var err error
		if input.Readers == readLogReadersCompliance {
			result, err = txApp.DB().NewQuery(`
				INSERT INTO {{leafen_read_log_readers}} ([[workspace_id]], [[scope]], [[readers]], [[changed_by]], [[changed_at]])
				VALUES ({:workspaceId}, {:scope}, 'compliance', {:actorId}, {:now}) ON CONFLICT DO NOTHING
			`).Bind(dbx.Params{"workspaceId": workspaceID, "scope": scope.key(), "actorId": user.Id, "now": now.UnixMilli()}).Execute()
		} else {
			result, err = txApp.DB().NewQuery(`
				DELETE FROM {{leafen_read_log_readers}} WHERE [[workspace_id]] = {:workspaceId} AND [[scope]] = {:scope}
			`).Bind(dbx.Params{"workspaceId": workspaceID, "scope": scope.key()}).Execute()
		}
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			return nil
		}
		from := readLogReadersCompliance
		if input.Readers == readLogReadersCompliance {
			from = readLogReadersAll
		}
		return recordReadLogEvent(txApp, scope.OrganizationID, workspaceID, user.Id, readLogReadersAction, scope.key(),
			map[string]any{"from": from, "to": input.Readers, "name": scope.Name}, now)
	})
	if err != nil {
		return readLogRouteError(event, err, "readers")
	}
	query := event.Request.URL.Query()
	query.Set("scope", scope.key())
	query.Del("cursor")
	event.Request.URL.RawQuery = query.Encode()
	return readReadLog(event)
}

func readLogRouteError(event *core.RequestEvent, err error, step string) error {
	switch {
	case errors.Is(err, errReadLogScope):
		return apiError(event, http.StatusBadRequest, errorCodeReadLogInvalid, "Name a page as page:<id>.")
	case errors.Is(err, errReadLogComplianceOnly):
		return apiError(event, http.StatusForbidden, errorCodeReadLogComplianceOnly, "Only compliance administrators read this read log or choose who reads it.")
	case isPermissionDenied(err):
		return apiError(event, http.StatusNotFound, errorCodeReadLogNotFound, "Only someone with full access to it reads its read log.")
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeReadLogFailed, "Couldn't read the read log.", "read-log", step, errorCodeReadLogFailed, err)
}
