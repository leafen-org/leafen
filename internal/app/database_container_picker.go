package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	databaseContainerPickerDefaultLimit = 12
	// A page may carry all one read goes through, so a window that counts the
	// hundred it holds or found reads them in one round trip.
	databaseContainerPickerMaximumLimit = databaseContainerPickerScanLimit
	databaseContainerPickerScanLimit    = 100
	databaseContainerPickerMaximumQuery = 128
	// The most sources a list asks to have first: those a block links ("추천").
	databaseContainerPickerMaximumFirst = 25
)

// databaseContainerPickerItem carries the view to copy into a new linked
// database, which owns its views instead of sharing the original's.
type databaseContainerPickerItem struct {
	ID           string          `json:"id"`
	PageID       string          `json:"pageId"`
	PageTitle    string          `json:"pageTitle"`
	DataSourceID string          `json:"dataSourceId"`
	ViewID       string          `json:"viewId"`
	ViewType     string          `json:"viewType"`
	ViewName     string          `json:"viewName"`
	ViewConfig   json.RawMessage `json:"viewConfig"`
	Title        string          `json:"title"`
	// Every source's title in its database's order, while it holds more
	// than one: the database's name in the source's path, as Notion's.
	SourceTitles []databaseSourceTitle `json:"sourceTitles,omitempty"`
	// An inline database's own name while it holds several sources, none
	// while it has none (inline_database_own_name.go).
	DatabaseTitle     string          `json:"databaseTitle,omitempty"`
	PreviewProperties []graphProperty `json:"previewProperties,omitempty"`
	// Its page's kind, for the app to name it while it has no title as it
	// names a page everywhere (page_name_kind.go): a full-page database
	// "새 데이터베이스", a view block's "제목 없음".
	PageKind  string `json:"pageKind"`
	ViewBlock bool   `json:"viewBlock,omitempty"`
	// The source's own icon, none where it has none
	// (database_container_picker_icons.go).
	Icon     string `json:"icon,omitempty"`
	IconType string `json:"iconType,omitempty"`
}

type databaseContainerPickerResponse struct {
	Items      []databaseContainerPickerItem `json:"items"`
	NextCursor string                        `json:"nextCursor,omitempty"`
}

func readDatabaseContainerPicker(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	query := strings.TrimSpace(event.Request.URL.Query().Get("q"))
	after := strings.TrimSpace(event.Request.URL.Query().Get("cursor"))
	limit := databaseContainerPickerDefaultLimit
	if rawLimit := strings.TrimSpace(event.Request.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the database search scope.")
		}
		limit = parsed
	}
	// A cursor names the last source a page went through in this order.
	var cursor *databaseContainerPickerKey
	if after != "" {
		key, ok := readDatabaseContainerPickerCursor(after)
		if !ok {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the database search scope.")
		}
		cursor = &key
	}
	first := []any{}
	for _, id := range strings.Split(event.Request.URL.Query().Get("first"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			if len(id) > maxGraphIdentifierLength {
				return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the database search scope.")
			}
			first = append(first, id)
		}
	}
	// "adds=rows" lists the databases a person may add rows to — a move's,
	// an action's that adds or edits pages — not those to link or relate to
	// (database_added_rows.go).
	adds := strings.TrimSpace(event.Request.URL.Query().Get("adds"))
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength ||
		utf8.RuneCountInString(query) > databaseContainerPickerMaximumQuery ||
		len(first) > databaseContainerPickerMaximumFirst ||
		limit < 1 || limit > databaseContainerPickerMaximumLimit || (adds != "" && adds != "rows") {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the database search scope.")
	}
	workspace, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the database.", err)
	}
	// A view block's unnamed source (a dashboard's, a waiting link's) is no
	// database to link, as Notion's view block holds none (seen 2026-11-09):
	// every list of sources to link, search or relate to reads them here, in
	// one order (database_container_picker_order.go), one more than a page
	// goes through.
	scope := newDatabaseContainerPickerScope(event.App, workspace.OwnerID, workspaceID, user.Id, first)
	keywords := readWorkspaceSearchKeywords(foldSearchText(query))
	rowless := map[string]bool{}
	if adds == "rows" {
		if rowless, err = workspaceSourcesTakingNoAddedRows(event.App, workspaceID); err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the database.", err)
		}
	}
	// Sources are read on, a window at a time (fillWindows' sizes), until
	// the page is full or none are left; a cursor follows only a full page
	// with more after it: a page that went through a hundred the reader
	// couldn't link and stopped there came back empty to a list that asks
	// once (the sidebar's "데이터베이스 보기" picker), with more to link
	// behind it (2026-11-23). Each decision on a page is asked once.
	judge := withAccessMemo(event.App)
	response := databaseContainerPickerResponse{Items: []databaseContainerPickerItem{}}
	for size := databaseContainerPickerScanLimit; ; size = min(size*2, windowFillLargest) {
		var candidates []databaseContainerPickerCandidate
		if query != "" {
			candidates, err = scope.search(query, cursor, size+1)
		} else {
			candidates, err = scope.list(cursor, size+1)
		}
		if err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the database.", err)
		}
		processed := 0
		for index, candidate := range candidates {
			if index == size || len(response.Items) == limit {
				break
			}
			processed++
			item, shown := databaseContainerPickerShows(judge, user.Id, workspace, workspaceID, candidate, keywords, rowless)
			if shown {
				response.Items = append(response.Items, item)
			}
		}
		if processed == 0 || processed == len(candidates) {
			break
		}
		if len(response.Items) == limit {
			response.NextCursor = candidates[processed-1].key.cursor()
			break
		}
		cursor = &candidates[processed-1].key
	}
	if err := fillDatabaseContainerPickerPageKinds(event.App, response.Items); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the database.", err)
	}
	if err := fillDatabaseContainerPickerIcons(event.App, response.Items); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the database.", err)
	}
	if err := fillDatabaseContainerPickerSourceTitles(event.App, readerCloser(event.App, user.Id), &response); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the database.", err)
	}
	if err := fillDatabaseContainerPickerPreviews(event.App, &response); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the database preview.", err)
	}
	return event.JSON(http.StatusOK, response)
}

// databaseContainerPickerShows is the item a found source is in the list,
// or false where it isn't: one taking no added rows a list of those asks,
// one on a page the reader may not read or in another workspace, one in a
// page in the trash or under one — gone there with it, no list to link,
// move to or relate to offers it — and one found by its place by what of
// that place the reader can't read.
func databaseContainerPickerShows(
	judge core.App, readerID string, workspace workspaceAuthorization, workspaceID string,
	candidate databaseContainerPickerCandidate, keywords workspaceSearchKeywords, rowless map[string]bool,
) (databaseContainerPickerItem, bool) {
	if rowless[candidate.DataSourceID] {
		return databaseContainerPickerItem{}, false
	}
	page, err := authorizePage(judge, readerID, candidate.PageID, permissionWorkspaceRead)
	if err != nil || page.Workspace.OwnerID != workspace.OwnerID ||
		page.Workspace.WorkspaceID != workspaceID || candidate.ViewID == "" {
		return databaseContainerPickerItem{}, false
	}
	if trashed, err := pageInTrashPath(judge, candidate.PageID); err != nil || trashed {
		return databaseContainerPickerItem{}, false
	}
	if candidate.byPlace {
		held, err := databaseContainerPickerPlaceHolds(judge, readerID, page.Page, candidate.Title, keywords)
		if err != nil || !held {
			return databaseContainerPickerItem{}, false
		}
	}
	view := struct {
		Type   string `db:"type"`
		Name   string `db:"name"`
		Config string `db:"config"`
	}{}
	if err := judge.DB().NewQuery(`
		SELECT [[type]], [[name]], [[config]] FROM {{leafen_database_views}} WHERE [[id]]={:id}
	`).Bind(dbx.Params{"id": candidate.ViewID}).One(&view); err != nil {
		return databaseContainerPickerItem{}, false
	}
	return databaseContainerPickerItem{
		ID: candidate.ID, PageID: candidate.PageID,
		PageTitle: page.Page.GetString("title"), DataSourceID: candidate.DataSourceID,
		ViewID: candidate.ViewID, ViewType: view.Type, ViewName: view.Name,
		ViewConfig: json.RawMessage(view.Config), Title: candidate.Title,
	}, true
}

// fillDatabaseContainerPickerPageKinds says each found source's page's kind.
func fillDatabaseContainerPickerPageKinds(app core.App, items []databaseContainerPickerItem) error {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.PageID)
	}
	kinds, err := loadPageNameKinds(app, ids)
	if err != nil {
		return err
	}
	for index := range items {
		named, found := kinds[items[index].PageID]
		if !found {
			named = pageNameKind{Kind: pageKindPage}
		}
		items[index].PageKind, items[index].ViewBlock = named.Kind, named.ViewBlock
	}
	return nil
}

// fillDatabaseContainerPickerSourceTitles names, for each found source whose
// database holds several, every source naming that database in its order —
// one it shows from another database among them, marked so — and an inline
// one's own name; one sent to the trash with its views doesn't count, as it
// doesn't for its database's name (databaseNamingSources).
func fillDatabaseContainerPickerSourceTitles(app core.App, closer func() searchMentionCloser, response *databaseContainerPickerResponse) error {
	if len(response.Items) == 0 {
		return nil
	}
	seen := map[string]bool{}
	ids := []any{}
	for _, item := range response.Items {
		if !seen[item.ID] {
			seen[item.ID] = true
			ids = append(ids, item.ID)
		}
	}
	rows := []struct {
		ContainerID string `db:"container_id"`
		Title       string `db:"title"`
		Gone        bool   `db:"gone"`
		Linked      bool   `db:"linked"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[place]].[[container_id]], [[source]].[[title]],
			` + databaseSourceGoneWithViewsSQL("[[place]].[[container_id]]", "[[place]].[[data_source_id]]") + ` AS [[gone]],
			[[place]].[[source_container_id]] != '' AS [[linked]]
		FROM {{leafen_database_container_sources}} [[place]]
		JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]]=[[place]].[[data_source_id]]
		WHERE [[place]].[[container_id]] IN (` + inlineDatabaseInList("container", ids) + `)
		ORDER BY [[place]].[[container_id]], [[place]].[[position]], [[place]].[[data_source_id]]
	`).Bind(inlineDatabaseInParams("container", ids)).All(&rows); err != nil {
		return err
	}
	listed := map[string][]databaseNameSource{}
	for _, row := range rows {
		listed[row.ContainerID] = append(listed[row.ContainerID], databaseNameSource{
			databaseNameRead: databaseNameRead{Title: row.Title, Linked: row.Linked}, Gone: row.Gone,
		})
	}
	// Those naming it, one it shows from another database among them
	// (databaseNamingSources).
	titles := map[string][]databaseSourceTitle{}
	for containerID, sources := range listed {
		for _, source := range databaseNamingSources(sources,
			func(source databaseNameSource) bool { return source.Gone }, func(source databaseNameSource) bool { return source.Linked }) {
			titles[containerID] = append(titles[containerID], databaseSourceTitle{Title: source.Title, Linked: source.Linked})
		}
	}
	several := []string{}
	for containerID, own := range titles {
		if len(own) > 1 {
			several = append(several, containerID)
		}
	}
	names, err := inlineDatabaseNamesOfSeveral(app, closer, several)
	if err != nil {
		return err
	}
	for index := range response.Items {
		if own := titles[response.Items[index].ID]; len(own) > 1 {
			response.Items[index].SourceTitles = own
			response.Items[index].DatabaseTitle = names[response.Items[index].ID].Title
		}
	}
	return nil
}
