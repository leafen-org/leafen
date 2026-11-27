package app

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A server administrator sees what is on the server: any workspace's pages, read only, its
// files and what it has made public, and may turn the public off and put
// pages and files away. They do so here alone, without becoming a member:
// nothing they see lists them, and every opening and change is kept with the
// workspace (administrator_workspace_content_schema.go) for its owner to
// see. A change is made as the workspace's owner would make it, by the
// owner's authority, so it follows every rule the owner's would — a legal
// hold, a page in use — and the record says who made it.

const (
	administratorContentPageLimit = 200
	administratorContentLogLimit  = 100
	administratorContentLogPrefix = "hacl1."
)

// administratorContentWorkspace is a workspace a server administrator opens.
type administratorContentWorkspace struct {
	ID             string `db:"id"`
	Name           string `db:"name"`
	OwnerID        string `db:"owner"`
	PersonID       string `db:"person_id"`
	OrganizationID string `db:"organization"`
}

func registerAdministratorWorkspaceContentRoutes(event *core.ServeEvent, roomInvalidator workspaceRoomInvalidator) {
	base := apiRoot + "/admin/workspaces/{workspaceId}"
	event.Router.GET(base+"/pages", listAdministratorWorkspacePages)
	event.Router.GET(base+"/pages/{pageId}", readAdministratorWorkspacePage)
	event.Router.POST(base+"/pages/{pageId}/trash", trashAdministratorWorkspacePage)
	event.Router.POST(base+"/pages/{pageId}/take-over", takeOverAdministratorWorkspacePage)
	event.Router.POST(base+"/pages/{pageId}/delete-permanently", deleteAdministratorWorkspacePage(roomInvalidator))
	event.Router.GET(base+"/files", listAdministratorWorkspaceFiles)
	event.Router.GET(base+"/storage", listAdministratorWorkspaceStorage)
	event.Router.GET(base+"/files/{fileId}/download", downloadAdministratorWorkspaceFile)
	event.Router.POST(base+"/files/{fileId}/delete", deleteAdministratorWorkspaceFile)
	event.Router.GET(base+"/public", listAdministratorWorkspacePublic)
	event.Router.POST(base+"/public/{kind}/{itemId}/disable", disableAdministratorWorkspacePublic)
	event.Router.GET(base+"/log", listAdministratorWorkspaceLog)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/server-admin-log", listWorkspaceServerAdministratorLog)
}

// administratorContentRequest is a server administrator's request about a
// workspace: the administrator and the workspace, or false once answered.
func administratorContentRequest(event *core.RequestEvent) (string, administratorContentWorkspace, bool) {
	if !authorizeAdministratorRequest(event) {
		return "", administratorContentWorkspace{}, false
	}
	user, _ := authenticatedUser(event)
	workspace := administratorContentWorkspace{}
	err := event.App.DB().NewQuery(`
		SELECT [[id]], [[name]], [[owner]], COALESCE(NULLIF([[workspace_owner]], ''), [[owner]]) AS [[person_id]],
			COALESCE([[organization]], '') AS [[organization]]
		FROM {{leafen_workspaces}} WHERE [[id]] = {:id} AND COALESCE([[deletion_job_id]], '') = ''
	`).Bind(dbx.Params{"id": strings.TrimSpace(event.Request.PathValue("workspaceId"))}).One(&workspace)
	if errors.Is(err, sql.ErrNoRows) {
		_ = apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		return "", workspace, false
	}
	if err != nil {
		_ = administratorContentError(event, err, "workspace")
		return "", workspace, false
	}
	return user.Id, workspace, true
}

func administratorContentError(event *core.RequestEvent, err error, stage string) error {
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed,
		"Couldn't read the workspace.", "administrator-workspace-content", stage, errorCodeAdministratorReadFailed, err)
}

// titleAsRead is a page's title as the administrator reads it — as the
// workspace's owner reads it, as its Markdown is read
// (readAdministratorWorkspacePage): a mention in it closed to them in marks
// (page_name_mentions.go), which the screen and the log say "@찾을 수 없음".
func (workspace administratorContentWorkspace) titleAsRead(app core.App, pageID, title string) string {
	return rowTitleAsRead(app, workspace.OwnerID, pageID, title)
}

// recordAdministratorContentEvent keeps what a server administrator did in
// a workspace.
func recordAdministratorContentEvent(app core.App, workspaceID, actorID, action, targetKind, targetID, title string) error {
	_, err := app.DB().Insert(serverAdministratorWorkspaceEventsTable, dbx.Params{
		"id": core.GenerateDefaultRandomId(), "workspace_id": workspaceID, "actor_id": actorID, "action": action,
		"target_kind": targetKind, "target_id": targetID, "target_title": title,
		"created_at": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

type storedAdministratorContentPage struct {
	ID          string `db:"id"`
	Title       string `db:"title"`
	Icon        string `db:"icon"`
	IconType    string `db:"icon_type"`
	TeamspaceID string `db:"teamspace_id"`
	PrivateID   string `db:"private_owner_id"`
	Teamspace   string `db:"teamspace_name"`
	Private     string `db:"private_owner_name"`
	IsRow       bool   `db:"is_row"`
	InTrash     bool   `db:"in_trash"`
	HasChildren bool   `db:"has_children"`
	IsPublic    bool   `db:"is_public"`
	TakeOver    bool   `db:"take_over"`
	Kind        string `db:"kind"`
	ViewBlock   bool   `db:"view_block"`
}

// loadAdministratorContentPages reads a workspace's pages that where says,
// teamspace pages first, then private pages by their owner, then the rest,
// each in its order. A page deleted for good, in its 30 days or going, has
// left everyone's sight (page_deletion_hold.go), the administrator's too: the
// workspace's owner finds it under 보안 › 데이터 보존.
func loadAdministratorContentPages(
	app core.App, workspace administratorContentWorkspace, where string, params dbx.Params, limit, offset int,
) ([]administratorWorkspacePage, error) {
	params["workspaceId"], params["ownerId"], params["web"] = workspace.ID, workspace.OwnerID, formAudienceWeb
	params["personId"] = workspace.PersonID
	params["limit"], params["offset"] = limit, offset
	rows := []storedAdministratorContentPage{}
	// A page's children are found by their parent, the owner and the import
	// mark read after the row (+): SQLite, which has no statistics, took the
	// owner's index and read every page of the owner's for each page listed.
	if err := app.DB().NewQuery(`
		SELECT [[page]].[[id]], [[page]].[[title]], COALESCE([[page]].[[icon]], '') AS [[icon]],
			COALESCE([[page]].[[icon_type]], '') AS [[icon_type]], COALESCE([[page]].[[teamspace_id]], '') AS [[teamspace_id]],
			` + administratorContentPrivateOwnerSQL + ` AS [[private_owner_id]],
			COALESCE([[team]].[[name]], '') AS [[teamspace_name]],
			COALESCE(` + accountNameSQL("person") + `, '') AS [[private_owner_name]],
			COALESCE([[page]].[[data_source_id]], '') != '' AS [[is_row]], [[page]].[[in_trash]],
			(EXISTS (SELECT 1 FROM {{leafen_pages}} [[child]] WHERE [[child]].[[parent_id]] = [[page]].[[id]]
					AND +[[child]].[[owner]] = [[page]].[[owner]] AND +[[child]].[[notion_import_staging]] = FALSE)
				OR EXISTS (SELECT 1 FROM {{leafen_data_sources}} [[source]]
					WHERE [[source]].[[page_id]] = [[page]].[[id]] AND [[source]].[[owner]] = [[page]].[[owner]])) AS [[has_children]],
			(EXISTS (SELECT 1 FROM {{leafen_public_share_links}} [[link]]
					WHERE [[link]].[[root_page_id]] = [[page]].[[id]] AND [[link]].[[enabled]] = 1)
				OR EXISTS (SELECT 1 FROM {{leafen_public_sites}} [[site]]
					WHERE [[site]].[[root_page_id]] = [[page]].[[id]] AND [[site]].[[published]] = 1)
				OR EXISTS (SELECT 1 FROM {{leafen_form_links}} [[form]] WHERE [[form]].[[page_id]] = [[page]].[[id]]
					AND [[form]].[[enabled]] = 1 AND [[form]].[[audience]] = {:web})) AS [[is_public]],
			` + administratorContentTakeOverSQL + ` AS [[take_over]],
			COALESCE([[page]].[[kind]], '') AS [[kind]], ` + viewBlockDatabasePageSQL + ` AS [[view_block]]
		FROM {{leafen_pages}} [[page]]
		LEFT JOIN {{leafen_teamspaces}} [[team]] ON [[team]].[[id]] = [[page]].[[teamspace_id]]
		LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[page]].[[private_owner_id]]
		WHERE [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[owner]] = {:ownerId}
			AND [[page]].[[notion_import_staging]] = FALSE AND (` + where + `)
			AND COALESCE([[page]].[[deletion_job_id]], '') = '' AND ` + pageNotHeldForDeletionSQL("[[page]]", "{:workspaceId}") + `
		ORDER BY CASE WHEN COALESCE([[page]].[[teamspace_id]], '') != '' THEN 0
				WHEN ` + administratorContentPrivateOwnerSQL + ` != '' THEN 1 ELSE 2 END,
			lower([[teamspace_name]]), lower([[private_owner_name]]), [[is_row]], [[page]].[[position]], [[page]].[[id]]
		LIMIT {:limit} OFFSET {:offset}
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	titles := make(map[string]*string, len(rows))
	for index := range rows {
		titles[rows[index].ID] = &rows[index].Title
	}
	pageTitlesAsRead(app, workspace.OwnerID, titles, true)
	spans := pageTitlesSpansAsRead(app, workspace.OwnerID, titles)
	pages := make([]administratorWorkspacePage, 0, len(rows))
	for _, row := range rows {
		page := administratorWorkspacePage{
			ID: row.ID, Title: row.Title, TitleSpans: spans[row.ID], Icon: row.Icon, IconType: row.IconType, Place: administratorPagePlaceWorkspace,
			IsRow: row.IsRow, HasChildren: row.HasChildren, InTrash: row.InTrash, IsPublic: row.IsPublic,
			TakeOver: row.TakeOver, PageKind: readPageKind(row.Kind), ViewBlock: row.ViewBlock,
		}
		if row.TeamspaceID != "" {
			page.Place, page.PlaceName = administratorPagePlaceTeamspace, row.Teamspace
		} else if row.PrivateID != "" {
			page.Place, page.PlaceName = administratorPagePlacePrivate, row.Private
		}
		pages = append(pages, page)
	}
	// An untitled database of several sources is named by them (page_name_kind.go).
	ids := make([]string, 0, len(pages))
	for _, page := range pages {
		if page.PageKind == pageKindDatabase && !page.ViewBlock && page.Title == "" {
			ids = append(ids, page.ID)
		}
	}
	sources, err := untitledFullPageDatabaseSourceTitles(app, ids)
	for index := range pages {
		pages[index].SourceTitles = sources[pages[index].ID]
	}
	return pages, err
}

// listAdministratorWorkspacePages lists a workspace's top pages, or the
// pages and database items under a page, a window at a time.
func listAdministratorWorkspacePages(event *core.RequestEvent) error {
	actorID, workspace, ok := administratorContentRequest(event)
	if !ok {
		return nil
	}
	query := event.Request.URL.Query()
	parentID := strings.TrimSpace(query.Get("parentId"))
	offset, valid := decodeAdministratorContentOffset(query.Get("cursor"))
	if !valid || len(parentID) > maxWorkspacePermissionIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the page list position.")
	}
	where := `[[page]].[[parent_id]] = '' AND COALESCE([[page]].[[data_source_id]], '') = ''`
	if parentID != "" {
		// Its database pages have their data source as their parent
		// (database_row_page_record_schema.go), so every page listed is found
		// by its parent's key; the OR alone read every page of the workspace.
		where = `[[page]].[[parent_id]] IN (SELECT {:parentId} UNION ALL
				SELECT [[id]] FROM {{leafen_data_sources}} WHERE [[page_id]] = {:parentId} AND [[owner]] = {:ownerId})
			AND ([[page]].[[parent_id]] = {:parentId} OR [[page]].[[data_source_id]] IN (
				SELECT [[id]] FROM {{leafen_data_sources}} WHERE [[page_id]] = {:parentId} AND [[owner]] = {:ownerId}))`
	}
	pages, err := loadAdministratorContentPages(event.App, workspace, where, dbx.Params{"parentId": parentID},
		administratorContentPageLimit+1, offset)
	if err != nil {
		return administratorContentError(event, err, "pages")
	}
	list := administratorWorkspacePageList{Items: pages}
	if len(pages) > administratorContentPageLimit {
		list.Items = pages[:administratorContentPageLimit]
		next := "o:" + strconv.Itoa(offset+administratorContentPageLimit)
		list.NextCursor = &next
	}
	// Looking through a workspace's pages is kept once, at its top.
	if parentID == "" && offset == 0 {
		if err := recordAdministratorContentEvent(event.App, workspace.ID, actorID, administratorContentBrowsePages,
			"workspace", workspace.ID, workspace.Name); err != nil {
			return administratorContentError(event, err, "record")
		}
	}
	return event.JSON(http.StatusOK, list)
}

// readAdministratorWorkspacePage reads one page of a workspace, read only:
// its title and its body as Markdown.
func readAdministratorWorkspacePage(event *core.RequestEvent) error {
	actorID, workspace, ok := administratorContentRequest(event)
	if !ok {
		return nil
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	content := administratorWorkspacePageContent{}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		pages, err := loadAdministratorContentPages(txApp, workspace, `[[page]].[[id]] = {:pageId}`, dbx.Params{"pageId": pageID}, 1, 0)
		if err != nil {
			return err
		}
		if len(pages) == 0 {
			return sql.ErrNoRows
		}
		content.Page = pages[0]
		projection, err := projectNotionPageMarkdown(txApp, workspace.OwnerID, pageID, pageID, notionMarkdownReader{InternalIDs: true, Sources: true})
		if err != nil {
			return err
		}
		content.Markdown, content.Truncated = projection.Markdown, projection.Truncated
		return recordAdministratorContentEvent(txApp, workspace.ID, actorID, administratorContentOpenPage, "page", pageID, content.Page.Title)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	if err != nil {
		return administratorContentError(event, err, "page")
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, content)
}

func decodeAdministratorContentOffset(cursor string) (int, bool) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, true
	}
	value, found := strings.CutPrefix(cursor, "o:")
	offset, err := strconv.Atoi(value)
	return offset, found && err == nil && offset >= 0 && offset <= 1_000_000
}

type storedAdministratorContentEvent struct {
	ID          string `db:"id"`
	Action      string `db:"action"`
	ActorID     string `db:"actor_id"`
	ActorName   string `db:"actor_name"`
	TargetKind  string `db:"target_kind"`
	TargetID    string `db:"target_id"`
	TargetTitle string `db:"target_title"`
	CreatedAt   int64  `db:"created_at"`
}

// administratorContentLog is a window of what server administrators did in
// a workspace, newest first.
func administratorContentLog(app core.App, workspaceID, cursor string) (administratorWorkspaceContentLog, bool, error) {
	at, id := int64(0), ""
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		encoded, found := strings.CutPrefix(cursor, administratorContentLogPrefix)
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		value, key, split := strings.Cut(string(decoded), "|")
		parsed, parseErr := strconv.ParseInt(value, 10, 64)
		if !found || err != nil || !split || parseErr != nil || key == "" {
			return administratorWorkspaceContentLog{}, false, nil
		}
		at, id = parsed, key
	}
	rows := []storedAdministratorContentEvent{}
	if err := app.DB().NewQuery(`
		SELECT [[event]].[[id]], [[event]].[[action]], [[event]].[[actor_id]],
			` + accountNameSQL("person") + ` AS [[actor_name]],
			[[event]].[[target_kind]], [[event]].[[target_id]], [[event]].[[target_title]], [[event]].[[created_at]]
		FROM {{leafen_server_admin_workspace_events}} [[event]]
		LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[event]].[[actor_id]]
		WHERE [[event]].[[workspace_id]] = {:workspaceId}
			AND ({:id} = '' OR [[event]].[[created_at]] < {:at} OR ([[event]].[[created_at]] = {:at} AND [[event]].[[id]] < {:id}))
		ORDER BY [[event]].[[created_at]] DESC, [[event]].[[id]] DESC LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "at": at, "id": id, "limit": administratorContentLogLimit + 1}).All(&rows); err != nil {
		return administratorWorkspaceContentLog{}, true, err
	}
	log := administratorWorkspaceContentLog{Items: []administratorWorkspaceContentEvent{}}
	if len(rows) > administratorContentLogLimit {
		rows = rows[:administratorContentLogLimit]
		last := rows[len(rows)-1]
		next := administratorContentLogPrefix + base64.RawURLEncoding.EncodeToString(
			[]byte(strconv.FormatInt(last.CreatedAt, 10)+"|"+last.ID))
		log.NextCursor = &next
	}
	for _, row := range rows {
		item := administratorWorkspaceContentEvent{
			ID: row.ID, Action: row.Action, ActorID: row.ActorID, ActorName: row.ActorName,
			TargetKind: row.TargetKind, TargetID: row.TargetID, TargetTitle: row.TargetTitle,
			OccurredAt: time.UnixMilli(row.CreatedAt).UTC().Format(time.RFC3339Nano),
		}
		log.Items = append(log.Items, item)
	}
	return log, true, nil
}

func listAdministratorWorkspaceLog(event *core.RequestEvent) error {
	_, workspace, ok := administratorContentRequest(event)
	if !ok {
		return nil
	}
	return answerAdministratorContentLog(event, workspace.ID)
}

// listWorkspaceServerAdministratorLog shows a workspace's owner, and its
// organization's administrators, what server administrators did in it.
func listWorkspaceServerAdministratorLog(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if err == nil {
		var owner bool
		if owner, err = workspaceOwnerIs(event.App, workspaceID, user.Id); err == nil && !owner &&
			(authorization.Organization.OrganizationID == "" || !authorization.Organization.allows(permissionOrganizationAdmin)) {
			err = permissionDenied()
		}
	}
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return administratorContentError(event, err, "owner")
	}
	return answerAdministratorContentLog(event, workspaceID)
}

func answerAdministratorContentLog(event *core.RequestEvent, workspaceID string) error {
	log, valid, err := administratorContentLog(event.App, workspaceID, event.Request.URL.Query().Get("cursor"))
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the list position.")
	}
	if err != nil {
		return administratorContentError(event, err, "log")
	}
	return event.JSON(http.StatusOK, log)
}
