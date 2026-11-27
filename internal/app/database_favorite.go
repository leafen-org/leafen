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

// An inline database among a person's favorites: changed as a page favorite
// is, by request id and expected revision, for whoever can read its page.

func registerDatabaseFavoriteRoutes(event *core.ServeEvent) {
	event.Router.PUT(apiRoot+databaseFavoriteUpdatePath, updateDatabaseFavoriteRoute).
		Bind(apis.BodyLimit(pageFavoriteMaxBodyBytes))
}

func updateDatabaseFavoriteRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	containerID := strings.TrimSpace(event.Request.PathValue("containerId"))
	input := pageFavoriteMutationInput{}
	if containerID == "" || event.BindBody(&input) != nil || !validPageFavoriteInput(input) {
		return apiError(event, http.StatusBadRequest, errorCodePageFavoriteInvalid, "Check the favorite change details.")
	}
	response, err := updateDatabaseFavorite(event.App, user.Id, containerID, input)
	switch {
	case isPermissionDenied(err) || errors.Is(err, sql.ErrNoRows):
		return apiError(event, http.StatusNotFound, errorCodeDatabaseNotFound, "Database not found.")
	case errors.Is(err, errPageFavoriteConflict):
		return apiError(event, http.StatusConflict, errorCodePageFavoriteConflict, "The favorite state changed. Try again.")
	case errors.Is(err, errPageFavoriteIdentityReused):
		return apiError(event, http.StatusConflict, errorCodePageFavoriteIdentityReused, "The favorite change request ID can't be reused.")
	case err != nil:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodePageFavoriteFailed,
			"Couldn't change the favorite.", "database_favorite", "update",
			errorCodePageFavoriteFailed, err,
		)
	default:
		return event.JSON(http.StatusOK, response)
	}
}

func updateDatabaseFavorite(
	app core.App,
	principalID, containerID string,
	input pageFavoriteMutationInput,
) (databaseFavoriteMutationResponse, error) {
	stripe := pageFavoriteStripe(principalID, "database\x00"+containerID)
	stripe.Lock()
	defer stripe.Unlock()
	response := databaseFavoriteMutationResponse{}
	workspaceID := ""
	requestHash := pageFavoriteRequestHash(input)
	err := app.RunInTransaction(func(txApp core.App) error {
		var container struct {
			PageID string `db:"page_id"`
		}
		if err := txApp.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_database_containers}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": containerID}).One(&container); err != nil {
			return err
		}
		authorization, err := authorizePage(txApp, principalID, container.PageID, permissionWorkspaceRead)
		if err != nil {
			return err
		}
		state := storedPageFavoriteState{}
		err = txApp.DB().NewQuery(`
			SELECT [[favorite]],[[revision]],[[last_request_id]],[[last_request_hash]]
			FROM {{leafen_database_favorites}}
			WHERE [[container_id]]={:containerId} AND [[principal_id]]={:principalId}
		`).Bind(dbx.Params{"containerId": containerID, "principalId": principalID}).One(&state)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if state.LastRequestID == input.RequestID {
			if state.LastRequestHash != requestHash {
				return errPageFavoriteIdentityReused
			}
			response = databaseFavoriteMutationResponse{
				ContainerID: containerID, Favorite: state.Favorite, Revision: state.Revision,
				RequestID: input.RequestID, Duplicate: true,
			}
			return nil
		}
		if state.Revision != input.ExpectedRevision {
			return errPageFavoriteConflict
		}
		nextRevision := state.Revision + 1
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_database_favorites}}
				([[container_id]],[[page_id]],[[workspace_id]],[[principal_id]],[[favorite]],[[revision]],
				 [[last_request_id]],[[last_request_hash]],[[updated]])
			VALUES ({:containerId},{:pageId},{:workspaceId},{:principalId},{:favorite},{:revision},
				{:requestId},{:requestHash},{:updated})
			ON CONFLICT ([[container_id]],[[principal_id]]) DO UPDATE SET
				[[page_id]]=excluded.[[page_id]],[[workspace_id]]=excluded.[[workspace_id]],
				[[favorite]]=excluded.[[favorite]],[[revision]]=excluded.[[revision]],
				[[last_request_id]]=excluded.[[last_request_id]],[[last_request_hash]]=excluded.[[last_request_hash]],
				[[updated]]=excluded.[[updated]]
		`).Bind(dbx.Params{
			"containerId": containerID, "pageId": container.PageID, "workspaceId": authorization.Workspace.WorkspaceID,
			"principalId": principalID, "favorite": input.Favorite, "revision": nextRevision,
			"requestId": input.RequestID, "requestHash": requestHash, "updated": time.Now().UTC().UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		response = databaseFavoriteMutationResponse{
			ContainerID: containerID, Favorite: input.Favorite, Revision: nextRevision, RequestID: input.RequestID,
		}
		workspaceID = authorization.Workspace.WorkspaceID
		return nil
	})
	// The person's other screens take it (workspace_page_list_wait.go).
	if err == nil && workspaceID != "" {
		noteFavoriteChanges(workspaceID, principalID)
	}
	return response, err
}

// bootstrapDatabaseFavorites lists a person's favorite inline databases on
// the pages this bootstrap batch carries, named and drawn by their first data
// source's title and icon — one of several sources named by its own name —
// in the order they were made favorites — and the
// ones taken out, whose revision a later change starts from — each said to
// be a view block or not (database_view_block.go).
func bootstrapDatabaseFavorites(app core.App, principalID string, pageIDs map[string]bool) ([]databaseFavoriteProjection, error) {
	rows := []databaseFavoriteProjection{}
	first := func(column string) string {
		return `COALESCE((SELECT [[source]].[[` + column + `]] FROM {{leafen_database_container_sources}} [[place]]
				JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]]=[[place]].[[data_source_id]]
				WHERE [[place]].[[container_id]]=[[favorite_state]].[[container_id]]
				ORDER BY [[place]].[[position]], [[place]].[[data_source_id]] LIMIT 1), '') AS [[` + column + `]]`
	}
	if err := app.DB().NewQuery(`
		SELECT [[favorite_state]].[[container_id]], [[favorite_state]].[[page_id]], [[favorite_state]].[[workspace_id]],
			` + first("title") + `, ` + first("icon") + `, ` + first("icon_type") + `,
			[[favorite_state]].[[favorite]], [[favorite_state]].[[revision]],
			EXISTS (SELECT 1 FROM {{leafen_database_containers}} [[favorite_container]]
				WHERE [[favorite_container]].[[id]]=[[favorite_state]].[[container_id]]
					AND ` + databaseViewBlockSQL("[[favorite_container]]") + `) AS [[view_block]]
		FROM {{leafen_database_favorites}} [[favorite_state]]
		WHERE [[favorite_state]].[[principal_id]]={:principalId}
		ORDER BY [[favorite_state]].[[updated]], [[favorite_state]].[[container_id]]
	`).Bind(dbx.Params{"principalId": principalID}).All(&rows); err != nil {
		return nil, err
	}
	shown := rows[:0]
	for _, row := range rows {
		if pageIDs[row.PageID] {
			shown = append(shown, row)
		}
	}
	// Each named as its reader reads it (page_name_mentions.go).
	containers := make([]string, 0, len(shown))
	for _, row := range shown {
		containers = append(containers, row.ContainerID)
	}
	names := databaseNamesAsRead(app, principalID, containers)
	// One of several sources by its own name (inline_database_own_name.go).
	several, err := inlineDatabaseNamesOfSeveral(app, readerCloser(app, principalID), containers)
	if err != nil {
		return nil, err
	}
	for containerID, name := range several {
		names[containerID] = name
	}
	for index := range shown {
		if name, read := names[shown[index].ContainerID]; read {
			shown[index].Title, shown[index].TitleSpans = name.Title, name.Spans
		}
	}
	return shown, nil
}
