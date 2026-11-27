package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A full-page database turned inline leaves its page behind as a signpost
// to the database, as in Notion a full-page database's page ID stays its
// database's: the page stays in the trash with its comments and the shares
// given to it, out of the trash's list, and links to it lead to the
// database wherever the database is now — its address opens the database
// alone while it is inline and its page once it is a page again
// (loadSignpostDatabaseAddress), a linked database's as a source's. Shares
// given only to the page don't reach the database, which follows the page
// it is in. A signpost lasts while the database lives in another page; once
// the database is gone, the page is an ordinary page in the trash.

// databasePageSignpostContainerSQL is the database container the page row
// the alias names leads to while it is a signpost, in the trash with the
// database it held in another page, and NULL otherwise.
func databasePageSignpostContainerSQL(page string) string {
	return `(SELECT [[signpost]].[[container_id]] FROM {{leafen_database_page_signposts}} AS [[signpost]]
				JOIN {{leafen_database_containers}} AS [[signpost_container]]
					ON [[signpost_container]].[[id]] = [[signpost]].[[container_id]]
				WHERE [[signpost]].[[page_id]] = ` + page + `.[[id]] AND ` + page + `.[[in_trash]]
					AND [[signpost_container]].[[page_id]] != [[signpost]].[[page_id]])`
}

// databasePageSignpostSQL is true for the page row [[page]] while it is a
// signpost.
var databasePageSignpostSQL = "(" + databasePageSignpostContainerSQL("[[page]]") + " IS NOT NULL)"

// databasePagePlace is where a signpost's database is now: the page it is
// in and its block there.
type databasePagePlace struct {
	ContainerID string `db:"container_id"`
	PageID      string `db:"page_id"`
	BlockID     string `db:"block_id"`
}

// databasePageLeftovers is what stays with a full-page database's page as
// its database turns inline: the shares given to the page itself, its
// public link among them, and the page's own comments.
type databasePageLeftovers struct {
	Shares   int `db:"shares" json:"shares"`
	Comments int `db:"comments" json:"comments"`
}

// recordDatabasePageSignpost notes the database a full-page database's page
// holds as the page becomes a page again, so the page leads to the database
// once the database leaves it and the page goes to the trash: its own or,
// for a linked database's page, the link (2026-11-13: a linked one's page
// left nothing, and its address opened the emptied page in the trash).
func recordDatabasePageSignpost(app core.App, page *core.Record) error {
	var containerID string
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_containers}}
		WHERE [[page_id]] = {:pageId} AND [[parent_block_id]] = ''
		ORDER BY [[position]], [[id]] LIMIT 1
	`).Bind(dbx.Params{"pageId": page.Id}).Row(&containerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return putDatabasePageSignpost(app, page.Id, page.GetString("workspace"), containerID)
}

// putDatabasePageSignpost notes the database a page leads to once it is a
// signpost.
func putDatabasePageSignpost(app core.App, pageID, workspaceID, containerID string) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_page_signposts}} ([[page_id]], [[workspace_id]], [[container_id]], [[created]])
		VALUES ({:pageId}, {:workspaceId}, {:containerId}, {:created})
		ON CONFLICT ([[page_id]]) DO UPDATE SET [[workspace_id]] = excluded.[[workspace_id]],
			[[container_id]] = excluded.[[container_id]], [[created]] = excluded.[[created]]
	`).Bind(dbx.Params{
		"pageId": pageID, "workspaceId": workspaceID, "containerId": containerID,
		"created": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

// isDatabasePageSignpost reports whether a page is a signpost now.
func isDatabasePageSignpost(app core.App, pageID string) (bool, error) {
	var signpost bool
	err := app.DB().NewQuery(`SELECT ` + databasePageSignpostSQL + ` FROM {{leafen_pages}} AS [[page]] WHERE [[page]].[[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&signpost)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return signpost, err
}

// loadDatabasePagePlace reports where a signpost's database is now.
func loadDatabasePagePlace(app core.App, pageID string) (databasePagePlace, bool, error) {
	place := databasePagePlace{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[id]] AS [[container_id]], [[container]].[[page_id]], [[container]].[[block_id]]
		FROM {{leafen_pages}} AS [[page]]
		JOIN {{leafen_database_page_signposts}} AS [[marker]] ON [[marker]].[[page_id]] = [[page]].[[id]]
		JOIN {{leafen_database_containers}} AS [[container]] ON [[container]].[[id]] = [[marker]].[[container_id]]
		WHERE [[page]].[[id]] = {:id} AND ` + databasePageSignpostSQL + `
	`).Bind(dbx.Params{"id": pageID}).One(&place)
	if errors.Is(err, sql.ErrNoRows) {
		return databasePagePlace{}, false, nil
	}
	return place, err == nil, err
}

// readDatabasePageLeftovers tells the person turning a full-page database
// inline what stays with its page.
func readDatabasePageLeftovers(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	if _, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead); err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageCreateFailed, "Couldn't read the page.", err)
	}
	leftovers := databasePageLeftovers{}
	if err := event.App.DB().NewQuery(`
		SELECT
			(SELECT COUNT(*) FROM {{leafen_page_permissions}} WHERE [[page_id]] = {:id})
				+ (SELECT COUNT(*) FROM {{leafen_public_share_links}} WHERE [[root_page_id]] = {:id} AND [[enabled]] = 1)
				AS [[shares]],
			(SELECT COUNT(*) FROM {{leafen_comments}} WHERE [[page_id]] = {:id} AND [[target_kind]] = 'page'
				AND [[target_id]] = {:id} AND [[block_id]] = '') AS [[comments]]
	`).Bind(dbx.Params{"id": pageID}).One(&leftovers); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageCreateFailed, "Couldn't read the page.", err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, leftovers)
}
