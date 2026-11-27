package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A page's bookmarked versions, as Notion's version history keeps them (seen
// 2026-10-01): a named mark on one version, listed in the history above that
// version and alone under "Group by 북마크". Renaming or deleting a bookmark
// leaves the version in the history; a bookmark goes with its version.

const (
	pageHistoryBookmarksTable = "leafen_page_history_bookmarks"
)

var errPageHistoryBookmarkMissing = errors.New(errorCodePageHistoryBookmarkMissing)

func registerPageHistoryBookmarkRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+pageHistoryBookmarkListPath, listPageHistoryBookmarks)
	event.Router.POST(apiRoot+pageHistoryBookmarkCreatePath, createPageHistoryBookmark).
		Bind(apis.BodyLimit(pageHistoryBookmarkMaxBodyBytes))
	event.Router.PATCH(apiRoot+pageHistoryBookmarkUpdatePath, updatePageHistoryBookmark).
		Bind(apis.BodyLimit(pageHistoryBookmarkMaxBodyBytes))
	event.Router.DELETE(apiRoot+pageHistoryBookmarkDeletePath, deletePageHistoryBookmark)
}

type storedPageHistoryBookmark struct {
	ID               string `db:"id"`
	PageID           string `db:"page_id"`
	VersionID        string `db:"version_id"`
	Name             string `db:"name"`
	AuthorID         string `db:"author_id"`
	VersionCreatedAt int64  `db:"version_created_at"`
	VersionCursor    int64  `db:"version_cursor"`
	CreatedAt        int64  `db:"created_at"`
}

func (bookmark storedPageHistoryBookmark) wire() pageHistoryBookmarkWire {
	return pageHistoryBookmarkWire{
		ID: bookmark.ID, PageID: bookmark.PageID, VersionID: bookmark.VersionID, Name: bookmark.Name,
		AuthorID: bookmark.AuthorID, VersionCreatedAt: bookmark.VersionCreatedAt, CreatedAt: bookmark.CreatedAt,
	}
}

const pageHistoryBookmarkColumns = `
	[[bookmark]].[[id]],[[bookmark]].[[page_id]],[[bookmark]].[[version_id]],[[bookmark]].[[name]],
	[[bookmark]].[[author_id]],[[bookmark]].[[created_at]],[[version]].[[created_at]] AS [[version_created_at]],
	[[version]].[[version_cursor]]`

// pageHistoryBookmarkName is a bookmark's name as kept: trimmed, one line, and
// within its bound. An empty name leaves the version's time to name it.
func pageHistoryBookmarkName(raw string) (string, bool) {
	name := strings.Join(strings.Fields(raw), " ")
	return name, utf8.RuneCountInString(name) <= pageHistoryBookmarkNameMaxChars
}

func readPageHistoryBookmark(app core.App, pageID, where string, params dbx.Params) (storedPageHistoryBookmark, error) {
	bookmark := storedPageHistoryBookmark{}
	params["pageId"] = pageID
	err := app.DB().NewQuery(`
		SELECT ` + pageHistoryBookmarkColumns + `
		FROM {{leafen_page_history_bookmarks}} AS [[bookmark]]
		JOIN {{leafen_page_history_versions}} AS [[version]] ON [[version]].[[id]] = [[bookmark]].[[version_id]]
		WHERE [[bookmark]].[[page_id]] = {:pageId} AND ` + where + `
	`).Bind(params).One(&bookmark)
	if errors.Is(err, sql.ErrNoRows) {
		return bookmark, errPageHistoryBookmarkMissing
	}
	return bookmark, err
}

func listPageHistoryBookmarks(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	if pageID == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the page history bookmark.")
	}
	after, err := decodeTimeIDCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryCursorInvalid, "Give the nextCursor of the last list, or none to start.")
	}
	response := pageHistoryBookmarkListResponse{PageID: pageID, Bookmarks: []pageHistoryBookmarkWire{}}
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		if _, err := authorizePage(txApp, user.Id, pageID, permissionWorkspaceRead); err != nil {
			return err
		}
		rows := []storedPageHistoryBookmark{}
		if err := txApp.DB().NewQuery(`
			SELECT ` + pageHistoryBookmarkColumns + `
			FROM {{leafen_page_history_bookmarks}} AS [[bookmark]]
			JOIN {{leafen_page_history_versions}} AS [[version]] ON [[version]].[[id]] = [[bookmark]].[[version_id]]
			WHERE [[bookmark]].[[page_id]] = {:pageId}
				AND ({:afterId} = '' OR [[version]].[[version_cursor]] < {:afterAt}
					OR ([[version]].[[version_cursor]] = {:afterAt} AND [[bookmark]].[[id]] < {:afterId}))
			ORDER BY [[version]].[[version_cursor]] DESC, [[bookmark]].[[id]] DESC LIMIT {:limit}
		`).Bind(dbx.Params{"pageId": pageID, "afterAt": after.At, "afterId": after.ID,
			"limit": pageHistoryBookmarkListWindow + 1}).All(&rows); err != nil {
			return err
		}
		if len(rows) > pageHistoryBookmarkListWindow {
			rows = rows[:pageHistoryBookmarkListWindow]
			response.NextCursor = encodeTimeIDCursor(rows[len(rows)-1].VersionCursor, rows[len(rows)-1].ID)
		}
		for _, row := range rows {
			response.Bookmarks = append(response.Bookmarks, row.wire())
		}
		return nil
	})
	return writePageHistoryBookmarkResult(event, response, err, "bookmark_list")
}

func createPageHistoryBookmark(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := pageHistoryBookmarkCreateRequest{}
	if err := event.BindBody(&input); err != nil || pageID == "" || strings.TrimSpace(input.VersionID) == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the page history bookmark.")
	}
	name, valid := pageHistoryBookmarkName(input.Name)
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the page history bookmark.")
	}
	response := pageHistoryBookmarkResponse{}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizePageHistoryRestore(txApp, user.Id, pageID); err != nil {
			return err
		}
		version := storedPageHistoryVersion{}
		if err := txApp.DB().NewQuery(`
			SELECT [[id]],[[page_id]],[[version_cursor]],[[actor_id]],[[source]],[[created_at]]
			FROM {{leafen_page_history_versions}} WHERE [[id]] = {:versionId} AND [[page_id]] = {:pageId}
		`).Bind(dbx.Params{"versionId": input.VersionID, "pageId": pageID}).One(&version); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errPageHistoryVersionMissing
			}
			return err
		}
		// A version has one bookmark; bookmarking it again keeps the one it has.
		existing, err := readPageHistoryBookmark(txApp, pageID, `[[bookmark]].[[version_id]] = {:versionId}`,
			dbx.Params{"versionId": version.ID})
		if err == nil {
			response.Bookmark = existing.wire()
			return nil
		}
		if !errors.Is(err, errPageHistoryBookmarkMissing) {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		bookmark := storedPageHistoryBookmark{
			ID: core.GenerateDefaultRandomId(), PageID: pageID, VersionID: version.ID, Name: name,
			AuthorID: user.Id, VersionCreatedAt: version.CreatedAt, CreatedAt: now,
		}
		if _, err := txApp.DB().Insert(pageHistoryBookmarksTable, dbx.Params{
			"id": bookmark.ID, "page_id": pageID, "version_id": version.ID, "name": name,
			"author_id": user.Id, "created_at": now, "updated_at": now,
		}).Execute(); err != nil {
			return err
		}
		response.Bookmark = bookmark.wire()
		return nil
	})
	return writePageHistoryBookmarkResult(event, response, err, "bookmark_create")
}

func updatePageHistoryBookmark(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	bookmarkID := strings.TrimSpace(event.Request.PathValue("bookmarkId"))
	input := pageHistoryBookmarkUpdateRequest{}
	// The name is what this change writes: a request without it changes
	// nothing rather than leaving the version unnamed (page_metadata_sent_fields.go).
	var sent struct {
		Name *string `json:"name"`
	}
	if err := rereadableRequestBody(event); err != nil || event.BindBody(&input) != nil || event.BindBody(&sent) != nil ||
		pageID == "" || bookmarkID == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the page history bookmark.")
	}
	if sent.Name == nil {
		return refuseMissingInput(event, errorCodePageHistoryInvalid, "name", "the bookmark's new name, \"\" to clear it.")
	}
	name, valid := pageHistoryBookmarkName(input.Name)
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the page history bookmark.")
	}
	response := pageHistoryBookmarkResponse{}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizePageHistoryRestore(txApp, user.Id, pageID); err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_page_history_bookmarks}} SET [[name]] = {:name}, [[updated_at]] = {:now}
			WHERE [[id]] = {:id} AND [[page_id]] = {:pageId}
		`).Bind(dbx.Params{
			"name": name, "now": time.Now().UTC().UnixMilli(), "id": bookmarkID, "pageId": pageID,
		}).Execute()
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			if err != nil {
				return err
			}
			return errPageHistoryBookmarkMissing
		}
		bookmark, err := readPageHistoryBookmark(txApp, pageID, `[[bookmark]].[[id]] = {:id}`, dbx.Params{"id": bookmarkID})
		if err != nil {
			return err
		}
		response.Bookmark = bookmark.wire()
		return nil
	})
	return writePageHistoryBookmarkResult(event, response, err, "bookmark_update")
}

func deletePageHistoryBookmark(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	bookmarkID := strings.TrimSpace(event.Request.PathValue("bookmarkId"))
	if pageID == "" || bookmarkID == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the page history bookmark.")
	}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizePageHistoryRestore(txApp, user.Id, pageID); err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_page_history_bookmarks}} WHERE [[id]] = {:id} AND [[page_id]] = {:pageId}
		`).Bind(dbx.Params{"id": bookmarkID, "pageId": pageID}).Execute()
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			if err != nil {
				return err
			}
			return errPageHistoryBookmarkMissing
		}
		return nil
	})
	if err == nil {
		return event.NoContent(http.StatusNoContent)
	}
	return writePageHistoryBookmarkResult(event, nil, err, "bookmark_delete")
}

func writePageHistoryBookmarkResult(event *core.RequestEvent, response any, err error, stage string) error {
	if errors.Is(err, errPageHistoryBookmarkMissing) {
		return apiError(event, http.StatusNotFound, errorCodePageHistoryBookmarkMissing, "Page history bookmark not found.")
	}
	return writePageHistoryResult(event, response, err, stage)
}
