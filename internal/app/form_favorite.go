package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerFormFavoriteRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+databaseFormFavoritesReadPath, readFormFavoritesRoute)
	event.Router.PUT(apiRoot+databaseFormFavoriteSavePath, saveFormFavoriteRoute).Bind(apis.BodyLimit(pageFavoriteMaxBodyBytes))
}

// readFormFavoritesRoute lists a person's favorite forms in the workspace, the
// last made favorite first; a form closed or out of reach is left out.
func readFormFavoritesRoute(event *core.RequestEvent) error {
	user, workspace, ok, err := myTaskWorkspace(event)
	if !ok {
		return err
	}
	rows := []struct {
		LinkID string `db:"form_link_id"`
	}{}
	if err := event.App.DB().NewQuery(`SELECT [[form_link_id]] FROM {{leafen_form_favorites}}
		WHERE [[owner_id]] = {:owner} AND [[workspace_id]] = {:workspace} ORDER BY [[updated_at]] DESC, [[form_link_id]]
	`).Bind(dbx.Params{"owner": user.Id, "workspace": workspace.WorkspaceID}).All(&rows); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the favorites.", err)
	}
	response := databaseFormFavoritesResponse{Favorites: []databaseFormFavoriteWire{}}
	for _, row := range rows {
		token, err := formToken(event.App, row.LinkID)
		if err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the favorites.", err)
		}
		definition, err := readFormDefinition(event.App, user.Id, token)
		if err != nil {
			continue
		}
		response.Favorites = append(response.Favorites, databaseFormFavoriteWire{Token: token, Title: definition.Form.Title, Icon: definition.Form.Icon})
	}
	// "내 작업" is kept among them, its view settings saying so.
	if err := event.App.DB().NewQuery(`SELECT COALESCE(json_extract([[views]], '$[0].config.myTasksFavorite'), 0)
		FROM {{leafen_my_task_views}} WHERE [[owner_id]] = {:owner} AND [[workspace_id]] = {:workspace}
	`).Bind(dbx.Params{"owner": user.Id, "workspace": workspace.WorkspaceID}).Row(&response.MyTasks); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeGraphReadFailed, "Couldn't load the favorites.", err)
	}
	return event.JSON(http.StatusOK, response)
}

func saveFormFavoriteRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := databaseFormFavoriteInput{}
	if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.Token) == "" || len(input.Token) > maxFormTokenCharacters {
		return apiError(event, http.StatusBadRequest, errorCodePageFavoriteInvalid, "Check the favorite change details.")
	}
	link, err := loadStoredFormLinkByToken(event.App, input.Token)
	if err != nil {
		return apiError(event, http.StatusNotFound, errorCodeFormNotFound, "Form not found.")
	}
	if err := authorizeFormDefinition(event.App, user.Id, link); err != nil {
		return apiError(event, http.StatusNotFound, errorCodeFormNotFound, "Form not found.")
	}
	params := dbx.Params{"owner": user.Id, "workspace": link.WorkspaceID, "link": link.ID, "now": time.Now().UnixMilli()}
	query := `DELETE FROM {{leafen_form_favorites}} WHERE [[owner_id]] = {:owner} AND [[form_link_id]] = {:link}`
	if input.Favorite {
		query = `INSERT INTO {{leafen_form_favorites}} ([[owner_id]], [[workspace_id]], [[form_link_id]], [[updated_at]])
			VALUES ({:owner}, {:workspace}, {:link}, {:now})
			ON CONFLICT ([[owner_id]], [[form_link_id]]) DO UPDATE SET [[updated_at]] = excluded.[[updated_at]]`
	}
	if _, err := event.App.DB().NewQuery(query).Bind(params).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageFavoriteFailed, "Couldn't change the favorite.", err)
	}
	return event.NoContent(http.StatusNoContent)
}
