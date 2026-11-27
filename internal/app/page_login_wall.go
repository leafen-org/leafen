package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The sign-in screen at a page's address says which workspace the page waits
// in, as Notion's does. It answers with that name and the workspace's icon
// alone, for a page that is there; anything else, a page or workspace on its way out too, reads as no
// such page. A page of an app's own workspace also names the app's address:
// nobody asks for such a page, as its people are those who may use the app,
// so the screen of a page someone can't open says so and opens the app
// (2026-11-08: it offered a request nobody receives).
func servePageLoginWall(event *core.RequestEvent) error {
	return answerLoginWall(event, event.Request.PathValue("pageId"))
}

// A members' form's link opened signed out is that sign-in screen too, naming
// the form's workspace, as Notion's form link does (seen 2026-11-15 in a
// signed-out Chromium and WebKit: a heading that the reader is nearly there,
// a line asking them to sign in to see the page in that workspace, and its
// sign-in). Only a link that answers its members
// says so; any other token reads as no such form.
func serveFormLoginWall(event *core.RequestEvent) error {
	link, err := loadStoredFormLinkByToken(event.App, strings.TrimSpace(event.Request.PathValue("token")))
	if errors.Is(err, errFormNotFound) || (err == nil && link.Audience != formAudienceWorkspace) {
		return apiError(event, http.StatusNotFound, errorCodeFormNotFound, "Form not found.")
	}
	if err != nil {
		return err
	}
	return answerLoginWall(event, link.PageID)
}

func answerLoginWall(event *core.RequestEvent, pageID string) error {
	place := struct {
		Name    string `db:"name"`
		Icon    string `db:"icon"`
		AppSlug string `db:"app_slug"`
	}{}
	err := event.App.DB().NewQuery(`SELECT [[workspace]].[[name]], COALESCE([[workspace]].[[icon]], '') AS [[icon]],
		COALESCE([[install]].[[slug]], '') AS [[app_slug]]
		FROM {{` + pagesCollection + `}} [[page]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[page]].[[workspace]]
		LEFT JOIN {{leafen_app_installs}} [[install]] ON [[install]].[[id]] = [[workspace]].[[app_install_id]] AND [[install]].[[removed_at]] = 0
		WHERE [[page]].[[id]]={:id} AND [[page]].[[in_trash]]=FALSE AND [[page]].[[notion_import_staging]]=FALSE
			AND COALESCE([[page]].[[deletion_job_id]], '')='' AND COALESCE([[workspace]].[[deletion_job_id]], '')=''`).
		WithContext(event.Request.Context()).Bind(dbx.Params{"id": pageID}).One(&place)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	if err != nil {
		return err
	}
	answer := map[string]string{"workspaceName": place.Name}
	// Its icon heads the card as Notion's does (a picture uploaded as it is
	// one only its people read, and the screen draws the name's letter then).
	if icon := strings.TrimSpace(place.Icon); icon != "" {
		answer["workspaceIcon"] = icon
	}
	if place.AppSlug != "" {
		answer["appSlug"] = place.AppSlug
	}
	return event.JSON(http.StatusOK, answer)
}
