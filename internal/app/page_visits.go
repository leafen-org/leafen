package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A person's own visits, for the library's "최근" and its "마지막 방문
// 시간" (Notion, seen 2026-10-01). Unlike page analytics, which a workspace
// may turn off and which shows views to others, these are kept whatever the
// workspace's analytics say and read only by the person: the latest visit to
// each page, the newest 200 per workspace, listed newest first while the
// person can still read the page.

const (
	pageVisitsTable = "leafen_page_visits"
)

func registerPageVisitRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+pageVisitRecordPath, recordPageVisitRoute)
	event.Router.GET(apiRoot+workspacePageVisitsReadPath, readPageVisitsRoute)
	event.Router.POST(apiRoot+workspaceLibraryPagesReadPath, readLibraryPagesRoute).
		Bind(apis.BodyLimit(libraryPagesMaxBodyBytes))
	event.Router.POST(apiRoot+workspacePageVisitsRemovePath, removePageVisitsRoute).
		Bind(apis.BodyLimit(pageVisitsRemoveMaxBodyBytes))
}

func recordPageVisitRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	if pageID == "" {
		return apiError(event, http.StatusNotFound, errorCodePageVisitNotFound, "Page not found.")
	}
	workspaceID, _, _, err := pageVisitPlace(event.App, user.Id, pageID)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodePageVisitNotFound, "Page not found.")
	}
	if err != nil {
		return pageVisitFailure(event, err)
	}
	now := time.Now()
	visit := pageVisitWire{PageID: pageID, VisitedAt: now.UTC().Format(time.RFC3339Nano)}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		params := dbx.Params{
			"ownerId": user.Id, "workspaceId": workspaceID,
			"pageId": pageID, "now": now.UnixMilli(), "kept": pageVisitsKept,
		}
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_page_visits}} ([[owner_id]], [[workspace_id]], [[page_id]], [[visited_at]])
			VALUES ({:ownerId}, {:workspaceId}, {:pageId}, {:now})
			ON CONFLICT ([[owner_id]], [[page_id]]) DO UPDATE SET
				[[workspace_id]] = excluded.[[workspace_id]], [[visited_at]] = excluded.[[visited_at]]
		`).Bind(params).Execute(); err != nil {
			return err
		}
		// Only the newest visits in the workspace are kept.
		_, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_page_visits}}
			WHERE [[owner_id]] = {:ownerId} AND [[workspace_id]] = {:workspaceId}
				AND [[page_id]] NOT IN (
					SELECT [[page_id]] FROM {{leafen_page_visits}}
					WHERE [[owner_id]] = {:ownerId} AND [[workspace_id]] = {:workspaceId}
					ORDER BY [[visited_at]] DESC, [[page_id]] ASC LIMIT {:kept}
				)
		`).Bind(params).Execute()
		return err
	})
	if err != nil {
		return pageVisitFailure(event, err)
	}
	return event.JSON(http.StatusOK, visit)
}

func readPageVisitsRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspaceReader(event.App, user.Id, workspaceID); err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return pageVisitFailure(event, err)
	}
	stored := []struct {
		PageID    string `db:"page_id"`
		VisitedAt int64  `db:"visited_at"`
	}{}
	// The visits are read first, newest first, and each page by its key, the
	// trash mark after the row (+): SQLite, which has no statistics, began at
	// the (workspace, trash) index and read the workspace's every page.
	if err := event.App.DB().NewQuery(`
		SELECT [[visit]].[[page_id]], [[visit]].[[visited_at]]
		FROM {{leafen_page_visits}} [[visit]]
		CROSS JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[visit]].[[page_id]]
		WHERE [[visit]].[[owner_id]] = {:ownerId} AND [[visit]].[[workspace_id]] = {:workspaceId}
			AND [[page]].[[workspace]] = {:workspaceId} AND +[[page]].[[in_trash]] = 0
		ORDER BY [[visit]].[[visited_at]] DESC, [[visit]].[[page_id]] ASC
		LIMIT {:kept}
	`).Bind(dbx.Params{"ownerId": user.Id, "workspaceId": workspaceID, "kept": pageVisitsKept}).All(&stored); err != nil {
		return pageVisitFailure(event, err)
	}
	// A page under a page in the trash went there with it
	// (loadPageTrashEffects): it leaves the list as a trashed page does, and
	// takes no place of the pages listed.
	visitedIDs := make([]string, 0, len(stored))
	for _, row := range stored {
		visitedIDs = append(visitedIDs, row.PageID)
	}
	trashed, err := loadPageTrashEffects(event.App, visitedIDs)
	if err != nil {
		return pageVisitFailure(event, err)
	}
	visits := pageVisitsWire{Visits: []pageVisitWire{}}
	for _, row := range stored {
		if len(visits.Visits) == pageVisitsListed {
			break
		}
		if _, inTrash := trashed[row.PageID]; inTrash {
			continue
		}
		// A page the person can no longer read leaves the list.
		if _, _, err := pageVisitWorkspace(event.App, user.Id, row.PageID); err != nil {
			if isPermissionDenied(err) {
				continue
			}
			return pageVisitFailure(event, err)
		}
		visits.Visits = append(visits.Visits, pageVisitWire{
			PageID: row.PageID, VisitedAt: time.UnixMilli(row.VisitedAt).UTC().Format(time.RFC3339Nano),
		})
	}
	return event.JSON(http.StatusOK, visits)
}

// removePageVisitsRoute takes pages off the reader's own "최근", as the
// library's "최근 항목에서 제거" does in Notion (seen 2026-10-07): the pages
// stay where they are, and a later visit lists them again.
func removePageVisitsRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := pageVisitsRemoveInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil ||
		len(input.PageIDs) == 0 || len(input.PageIDs) > pageVisitsRemoveLimit {
		return apiError(event, http.StatusBadRequest, errorCodePageVisitsRemoveInvalid, "Check the pages to remove from recents.")
	}
	if _, err := authorizeWorkspaceReader(event.App, user.Id, workspaceID); err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return pageVisitFailure(event, err)
	}
	removed, err := removePageVisits(event.App, user.Id, workspaceID, input.PageIDs)
	if err != nil {
		return pageVisitFailure(event, err)
	}
	return event.JSON(http.StatusOK, pageVisitsRemovedWire{RemovedCount: removed})
}

// removePageVisits forgets the person's visits to the pages in the
// workspace, and only theirs.
func removePageVisits(app core.App, ownerID, workspaceID string, pageIDs []string) (int, error) {
	ids := make([]any, 0, len(pageIDs))
	seen := map[string]bool{}
	for _, id := range pageIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result, err := app.DB().Delete(pageVisitsTable, dbx.And(
		dbx.HashExp{"owner_id": ownerID, "workspace_id": workspaceID},
		dbx.In("page_id", ids...),
	)).Execute()
	if err != nil {
		return 0, err
	}
	removed, err := result.RowsAffected()
	return int(removed), err
}

// readLibraryPagesRoute gives the library's rows what the page list doesn't
// carry: who made each page, who edited it last and when the reader last
// visited it, for the pages the reader can read among those asked for.
func readLibraryPagesRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := libraryPagesInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil || len(input.PageIDs) > libraryPagesLimit {
		return apiError(event, http.StatusBadRequest, errorCodeLibraryPagesInvalid, "Check the library pages.")
	}
	if _, err := authorizeWorkspaceReader(event.App, user.Id, workspaceID); err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return pageVisitFailure(event, err)
	}
	type storedLibraryPage struct {
		CreatedBy    string `db:"created_by"`
		LastEditedBy string `db:"last_edited_by"`
		VisitedAt    int64  `db:"visited_at"`
	}
	result := libraryPagesWire{Pages: []libraryPageWire{}}
	// Each row's creator and last editor, by the row's place, until their
	// names are read.
	creatorIDs := []string{}
	editorIDs := []string{}
	names := map[string]string{}
	seen := map[string]bool{}
	for _, raw := range input.PageIDs {
		pageID := strings.TrimSpace(raw)
		if pageID == "" || seen[pageID] {
			continue
		}
		seen[pageID] = true
		visitWorkspaceID, rowPage, err := pageVisitWorkspace(event.App, user.Id, pageID)
		if isPermissionDenied(err) || (err == nil && visitWorkspaceID != workspaceID) {
			continue
		}
		if err != nil {
			return pageVisitFailure(event, err)
		}
		stored := storedLibraryPage{}
		if err := event.App.DB().NewQuery(`
			SELECT COALESCE([[page]].[[created_by]], '') AS [[created_by]],
				COALESCE([[page]].[[last_edited_by]], '') AS [[last_edited_by]],
				COALESCE([[visit]].[[visited_at]], 0) AS [[visited_at]]
			FROM {{leafen_pages}} [[page]]
			LEFT JOIN {{leafen_page_visits}} [[visit]]
				ON [[visit]].[[page_id]] = [[page]].[[id]] AND [[visit]].[[owner_id]] = {:ownerId}
			WHERE [[page]].[[id]] = {:pageId} AND [[page]].[[workspace]] = {:workspaceId}
		`).Bind(dbx.Params{"ownerId": user.Id, "pageId": pageID, "workspaceId": workspaceID}).One(&stored); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return pageVisitFailure(event, err)
		}
		row := libraryPageWire{PageID: pageID}
		if !rowPage {
			if row.PeopleDatabase, err = libraryPeopleDatabaseOf(event.App, pageID); err != nil {
				return pageVisitFailure(event, err)
			}
		}
		if rowPage {
			// A database row's page tells the library what the page list
			// doesn't carry for it: its title, icon and database.
			authority, err := authorizeDatabaseRowOrTemplatePage(event.App, user.Id, pageID, permissionWorkspaceRead)
			if err != nil {
				return pageVisitFailure(event, err)
			}
			// A title naming a page closed to the reader (content_graph_reader_mentions.go).
			title := rowTitleAsRead(event.App, user.Id, pageID, databaseRowPageTitle(event.App, pageID))
			if authority.Template {
				title = databaseTemplatePageTitle(event.App, pageID)
			}
			// Its spans, read so, say its mentions where it is named (closedMentions.ts).
			spans := []graphTextSpan(nil)
			if !authority.Template {
				spans = pageTitleSpansAsReadBy(event.App, readerCloser(event.App, user.Id), pageID, title)
			}
			row.Row = &libraryRowPageWire{
				Title: title, TitleSpans: spans, Icon: authority.Icon, IconType: authority.IconType, Template: authority.Template,
				ContainerPageID: authority.ContainerPageID, DataSourceID: authority.DataSourceID,
				CreatedAt: databaseRowPageTime(authority.CreatedAt), UpdatedAt: databaseRowPageTime(authority.UpdatedAt),
				CreatedBy: authority.CreatedBy, LastEditedBy: authority.LastEditedBy,
			}
			// A row without a title is called by its database's word for its
			// untitled pages (databaseUntitledRowNameFor), and so is a template
			// with neither a title nor the name it was made with, as its
			// database's template menu calls it (pagePathUntitledName).
			if row.Row.UntitledName, err = databaseUntitledRowNameFor(event.Request.Context(), event.App, user.Id, authority.DataSourceID); err != nil {
				return pageVisitFailure(event, err)
			}
			// A People row names its person, whose profile it opens as from the
			// first frame, not through People's own page (usePersonRowPage).
			if !authority.Template {
				if row.Row.Person, err = peopleRowPersonOf(event.App, authority.DataSourceID, pageID); err != nil {
					return pageVisitFailure(event, err)
				}
				// Without an icon of its own it shows its person's picture (people_row_pictures.go).
				if row.Row.Person != "" && strings.TrimSpace(row.Row.Icon) == "" {
					if row.Row.Icon, err = peopleRowPictureOf(event.App, row.Row.Person); err != nil {
						return pageVisitFailure(event, err)
					} else if row.Row.Icon != "" {
						row.Row.IconType = "image"
					}
				}
			}
		}
		if stored.VisitedAt > 0 {
			row.VisitedAt = time.UnixMilli(stored.VisitedAt).UTC().Format(time.RFC3339Nano)
		}
		for _, id := range []string{stored.CreatedBy, stored.LastEditedBy} {
			if _, known := names[id]; id != "" && !known {
				names[id] = ""
			}
		}
		creatorIDs = append(creatorIDs, stored.CreatedBy)
		editorIDs = append(editorIDs, stored.LastEditedBy)
		result.Pages = append(result.Pages, row)
	}
	if len(names) > 0 {
		ids := make([]string, 0, len(names))
		for id := range names {
			ids = append(ids, id)
		}
		profiles, err := pageHistoryActorProfiles(event.App, ids)
		if err != nil {
			return pageVisitFailure(event, err)
		}
		for _, profile := range profiles {
			names[profile.UserID] = profile.DisplayName
		}
		// What Leafen made itself — People's page and its people's — is by
		// "Leafen", as Notion's library names its own "Notion App" (seen 2026-11-16).
		if _, made := names[leafenSystemAuthorID]; made {
			names[leafenSystemAuthorID] = "Leafen"
		}
	}
	for index, creatorID := range creatorIDs {
		result.Pages[index].CreatorName = names[creatorID]
		result.Pages[index].LastEditorName = names[editorIDs[index]]
	}
	// A row's page shows the empty document while its body holds nothing,
	// as the sidebar's pages do (page_has_content.go).
	rowIDs := []string{}
	for _, page := range result.Pages {
		if page.Row != nil {
			rowIDs = append(rowIDs, page.PageID)
		}
	}
	contents, err := pageContentsByID(event.App, rowIDs)
	if err != nil {
		return pageVisitFailure(event, err)
	}
	for _, page := range result.Pages {
		if has, found := contents[page.PageID]; found && page.Row != nil {
			page.Row.HasContent = boolPointer(has)
		}
	}
	return event.JSON(http.StatusOK, result)
}

func pageVisitFailure(event *core.RequestEvent, err error) error {
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePageVisitFailed,
		"Couldn't keep the page visit.", "page_visit", "storage", errorCodePageVisitFailed, err)
}

// pageVisitWorkspace is the workspace of a page, or of a database row's
// page, the person can read; a row in the trash — on its own, with its data
// source or with the page holding its database — is out of reach as a
// trashed page is.
func pageVisitWorkspace(app core.App, userID, pageID string) (string, bool, error) {
	workspaceID, rowPage, trashed, err := pageVisitPlace(app, userID, pageID)
	if err == nil && rowPage && trashed {
		return "", true, permissionDenied()
	}
	return workspaceID, rowPage, err
}

// pageVisitPlace is pageVisitWorkspace with a row's page in the trash too,
// read as it opens from there (authorizeDatabaseRowPageRead): a visit to it
// is kept as a trashed page's is, and lists once it is restored.
func pageVisitPlace(app core.App, userID, pageID string) (string, bool, bool, error) {
	rowPage, err := isDatabaseRowPageRoom(app, pageID)
	if err != nil {
		return "", false, false, err
	}
	// A template's page lists as a database page does (database_template_row_page.go).
	if !rowPage {
		if rowPage, err = isDatabaseTemplatePageID(app, pageID); err != nil {
			return "", false, false, err
		}
	}
	if rowPage {
		authority, _, err := authorizeDatabaseRowPageRead(app, userID, pageID)
		if errors.Is(err, errDatabaseRowPageNotFound) {
			return "", true, false, permissionDenied()
		}
		if err != nil {
			return "", true, false, err
		}
		return authority.WorkspaceID, true, authority.TrashedAt != 0, nil
	}
	// A page an operation holds a while is still the visitor's (page_access_hold.go).
	authorization, err := pageAccessDespiteHold(authorizePage(app, userID, pageID, permissionWorkspaceRead))
	if err != nil {
		return "", false, false, err
	}
	return authorization.Workspace.WorkspaceID, false, false, nil
}
