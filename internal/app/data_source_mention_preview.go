package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A data source's mention (data_source_mentions.go) reads its source as its
// app draws and opens it: the source's name as its reader reads it, how many
// sources its database has (one without a name is named by them,
// databaseTitles.ts), its icon — the source's own, else, for the only source
// of a full-page database, its page's, which its page's head shows and gives
// that source as Notion's does (seen 2026-11-22: a source's own icon set
// apart from its database's is the mention's); a database's of several is not
// the mention's (database_own_icon.go) — whether it is in the trash, and its database with the
// page holding it and the database's first view of the source, which a press
// opens, as Notion's does (seen 2026-11-22: a full-page database's at its
// view, an inline one's as a page at its source's view tab, one in the trash
// in the trash; one sent to the trash alone its database as it opens, with
// a note that it is in the trash). A reader reads it while the page holding its database opens
// to them, as the mention's words do (mcpDatabaseHolderPageID).

const dataSourceMentionPreviewPath = "/data-sources/{dataSourceId}/mention-preview"

func registerDataSourceMentionPreviewRoute(event *core.ServeEvent) {
	event.Router.GET(apiRoot+dataSourceMentionPreviewPath, readDataSourceMentionPreview)
}

func readDataSourceMentionPreview(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	address := notionMarkdownDataSourceAddress + strings.TrimSpace(event.Request.PathValue("dataSourceId"))
	dataSourceID, containerID, pageID, found, err := mentionedDataSource(event.App, address)
	if err == nil && found && pageReader(user.Id).closes(event.App)("database", address) != mentionOpens {
		found = false
	}
	if err == nil && !found {
		return apiError(event, http.StatusNotFound, errorCodeDatabaseDataSourceNotFound, "Data source not found.")
	}
	var preview dataSourceMentionPreviewResponse
	if err == nil {
		preview, err = dataSourceMentionPreviewOf(event.App, user.Id, dataSourceID, containerID, pageID)
	}
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "data-source-mention",
			errorCodeWorkspaceSearchFailed, "Couldn't read the data source.",
		)
	}
	return event.JSON(http.StatusOK, preview)
}

// dataSourceMentionPreviewOf is a data source's mention's preview for its
// reader, of the database and page holding it (mentionedDataSource).
func dataSourceMentionPreviewOf(app core.App, readerID, dataSourceID, containerID, pageID string) (dataSourceMentionPreviewResponse, error) {
	row := struct {
		Title        string `db:"title"`
		Icon         string `db:"icon"`
		IconType     string `db:"icon_type"`
		Sources      int    `db:"sources"`
		FullPage     bool   `db:"full_page"`
		PageIcon     string `db:"page_icon"`
		PageIconType string `db:"page_icon_type"`
		PageInTrash  bool   `db:"page_in_trash"` // in the trash, or under a page there
		Deleted      bool   `db:"deleted"`
		Held         bool   `db:"container_trashed"`
		Alone        bool   `db:"alone"`
		ViewID       string `db:"view_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[title]],
			COALESCE(` + dataSourceIconSQL("[[source]]") + `, '') AS [[icon]],
			COALESCE(` + dataSourceIconTypeSQL("[[source]]") + `, '') AS [[icon_type]],
			(SELECT COUNT(*) FROM {{leafen_database_container_sources}} [[member]]
				WHERE [[member]].[[container_id]] = {:containerId}) AS [[sources]],
			COALESCE((SELECT [[holder]].[[kind]] = '` + pageKindDatabase + `' AND [[container]].[[parent_block_id]] = ''
				FROM {{leafen_database_containers}} [[container]]
				JOIN {{leafen_pages}} [[holder]] ON [[holder]].[[id]] = [[container]].[[page_id]]
				WHERE [[container]].[[id]] = {:containerId}), 0) AS [[full_page]],
			COALESCE([[page]].[[icon]], '') AS [[page_icon]], COALESCE([[page]].[[icon_type]], '') AS [[page_icon_type]],
			NOT ` + pageOutOfTrashPathSQL("{:pageId}") + ` AS [[page_in_trash]],
			EXISTS (SELECT 1 FROM {{leafen_deleted_databases}} [[gone]]
				WHERE [[gone]].[[block_id]] = {:containerId}) AS [[deleted]],
			EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} [[held]]
				WHERE [[held]].[[container_id]] = {:containerId}) AS [[container_trashed]],
			` + databaseSourceTrashedInPlaceSQL("{:id}") + ` AS [[alone]],
			COALESCE((SELECT [[view]].[[id]] FROM {{leafen_database_container_views}} [[place]]
				JOIN {{leafen_database_views}} [[view]] ON [[view]].[[id]] = [[place]].[[view_id]]
				WHERE [[place]].[[container_id]] = {:containerId} AND [[view]].[[data_source_id]] = [[source]].[[id]]
				ORDER BY [[place]].[[position]], [[view]].[[id]] LIMIT 1), '') AS [[view_id]]
		FROM {{leafen_data_sources}} [[source]]
		LEFT JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = {:pageId}
		WHERE [[source]].[[id]] = {:id}
	`).Bind(dbx.Params{"id": dataSourceID, "containerId": containerID, "pageId": pageID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return dataSourceMentionPreviewResponse{}, err
	}
	// Its name as its reader reads it, a mention in it closed to them in marks.
	title := row.Title
	sourceNamesAsReadBy(app, readerCloser(app, readerID), map[string]*string{dataSourceID: &title})
	trashed, err := databaseSourceIsTrashed(app, dataSourceID)
	if err != nil {
		return dataSourceMentionPreviewResponse{}, err
	}
	preview := dataSourceMentionPreviewResponse{
		PageID: pageID, DatabaseID: containerID, ViewID: row.ViewID, Title: title, Sources: row.Sources,
		InTrash:      trashed || row.PageInTrash || row.Deleted,
		TrashedAlone: row.Alone && !row.Held && !row.PageInTrash && !row.Deleted,
	}
	// One sent to the trash alone opens its database as it opens, saying so
	// (textBlockMentionClicks.ts), as Notion's does (seen 2026-11-22, twice).
	if preview.TrashedAlone {
		preview.ViewID = ""
	}
	// Who sent it there and when, which its mention says under the pointer
	// (mention_trash_mark.go); the trash keeps none for one sent there alone.
	if preview.InTrash {
		mark, err := databaseMentionTrashMarkOf(app, containerID, pageID)
		if err != nil {
			return dataSourceMentionPreviewResponse{}, err
		}
		preview.TrashedAt, preview.TrashedBy = mark.At, mark.By
	}
	switch {
	case databaseMentionIconSet(row.Icon, row.IconType):
		preview.Icon, preview.IconType = row.Icon, row.IconType
	case row.FullPage && row.Sources <= 1 && databaseMentionIconSet(row.PageIcon, row.PageIconType):
		preview.Icon, preview.IconType = row.PageIcon, row.PageIconType
	}
	return preview, nil
}
