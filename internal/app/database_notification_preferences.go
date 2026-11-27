package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An inline database opened alone has its own "알림 받기" (Notion's, seen
// 2026-11-11), kept by its container beside the page holding it
// (schema_baseline.json). Following every comment on
// it follows every comment on its rows; replies and @mentions is the
// default and keeps no row. The page's own setting is the page's alone.

type databaseNotificationPreferenceResponse struct {
	PageID      string `json:"pageId"`
	ContainerID string `json:"containerId"`
	Level       string `json:"level"`
}

func registerDatabaseNotificationPreferenceRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/pages/{id}/databases/{containerId}/notification-preference", readDatabaseNotificationPreference)
	event.Router.PUT(apiRoot+"/pages/{id}/databases/{containerId}/notification-preference", saveDatabaseNotificationPreference)
}

// databaseNotificationPreferenceTarget is the person, the page and the
// database on it the preference is asked of, read through the page.
func databaseNotificationPreferenceTarget(event *core.RequestEvent) (*core.Record, pageAuthorization, string, bool, error) {
	user, authorization, ok, err := pageNotificationPreferenceTarget(event)
	if !ok {
		return nil, pageAuthorization{}, "", false, err
	}
	containerID := strings.TrimSpace(event.Request.PathValue("containerId"))
	held := 0
	if err := event.App.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_containers}} WHERE [[id]] = {:containerId} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"containerId": containerID, "pageId": authorization.Page.Id}).Row(&held); err != nil {
		return nil, pageAuthorization{}, "", false, apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't load the notification settings.", "database_notification_preference.authorize", "storage-read",
			errorCodeNotificationSettingsFailed, err,
		)
	}
	if held == 0 {
		return nil, pageAuthorization{}, "", false, apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	return user, authorization, containerID, true, nil
}

func loadDatabaseNotificationLevel(app core.App, containerID, userID string) (string, error) {
	level := ""
	err := app.DB().NewQuery(`
		SELECT [[level]] FROM {{leafen_database_notification_preferences}}
		WHERE [[container_id]] = {:containerId} AND [[user_id]] = {:userId}
	`).Bind(dbx.Params{"containerId": containerID, "userId": userID}).Row(&level)
	if errors.Is(err, sql.ErrNoRows) {
		return pageNotificationRepliesAndMentions, nil
	}
	return level, err
}

func readDatabaseNotificationPreference(event *core.RequestEvent) error {
	user, authorization, containerID, ok, err := databaseNotificationPreferenceTarget(event)
	if !ok {
		return err
	}
	level, err := loadDatabaseNotificationLevel(event.App, containerID, user.Id)
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't load the notification settings.", "database_notification_preference.read", "storage-read",
			errorCodeNotificationSettingsFailed, err,
		)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, databaseNotificationPreferenceResponse{
		PageID: authorization.Page.Id, ContainerID: containerID, Level: level,
	})
}

func saveDatabaseNotificationPreference(event *core.RequestEvent) error {
	user, authorization, containerID, ok, err := databaseNotificationPreferenceTarget(event)
	if !ok {
		return err
	}
	input := pageNotificationPreferenceInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeNotificationSettingsInvalid, "Check the notification settings.")
	}
	if input.Level == "" {
		return refuseMissingInput(event, errorCodeNotificationSettingsInvalid, "level", "all_comments or replies_and_mentions.")
	}
	if input.Level != pageNotificationAllComments && input.Level != pageNotificationRepliesAndMentions {
		return apiError(event, http.StatusBadRequest, errorCodeNotificationSettingsInvalid, "Check the notification settings.")
	}
	params := dbx.Params{
		"containerId": containerID, "pageId": authorization.Page.Id, "userId": user.Id,
		"workspaceId": authorization.Workspace.WorkspaceID, "level": input.Level, "now": time.Now().UTC().UnixMilli(),
	}
	query := `
		INSERT INTO {{leafen_database_notification_preferences}}
			([[container_id]], [[page_id]], [[user_id]], [[workspace_id]], [[level]], [[updated_at]])
		VALUES ({:containerId}, {:pageId}, {:userId}, {:workspaceId}, {:level}, {:now})
		ON CONFLICT ([[container_id]], [[user_id]]) DO UPDATE SET
			[[page_id]] = excluded.[[page_id]], [[level]] = excluded.[[level]]`
	if input.Level == pageNotificationRepliesAndMentions {
		query = `DELETE FROM {{leafen_database_notification_preferences}}
			WHERE [[container_id]] = {:containerId} AND [[user_id]] = {:userId}`
	}
	if _, err := event.App.DB().NewQuery(query).Bind(params).Execute(); err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't save the notification settings.", "database_notification_preference.save", "storage-write",
			errorCodeNotificationSettingsFailed, err,
		)
	}
	return event.JSON(http.StatusOK, databaseNotificationPreferenceResponse{
		PageID: authorization.Page.Id, ContainerID: containerID, Level: input.Level,
	})
}

// databaseRowCommentFollowersSQL lists who follows every comment on a
// database showing a row's source, after a cursor: its row comments reach
// them.
const databaseRowCommentFollowersSQL = `
	SELECT DISTINCT [[follow]].[[user_id]] FROM {{leafen_database_rows}} AS [[row]]
	JOIN {{leafen_database_container_sources}} AS [[shown]] ON [[shown]].[[data_source_id]] = [[row]].[[data_source_id]]
	JOIN {{leafen_database_notification_preferences}} AS [[follow]] ON [[follow]].[[container_id]] = [[shown]].[[container_id]]
	WHERE [[row]].[[id]] = {:rowId} AND [[follow]].[[level]] = 'all_comments' AND [[follow]].[[user_id]] > {:cursor}
	ORDER BY [[follow]].[[user_id]] LIMIT {:limit}`

// databaseRowCommentsFollowedSince reports whether a person follows every
// comment on a database that shows a row, and since when (Unix milliseconds):
// the earliest such database's bell.
func databaseRowCommentsFollowedSince(ctx context.Context, app core.App, rowID, userID string) (bool, int64, error) {
	since := sql.NullInt64{}
	err := app.DB().NewQuery(`
		SELECT MIN([[follow]].[[updated_at]]) FROM {{leafen_database_rows}} AS [[row]]
		JOIN {{leafen_database_container_sources}} AS [[shown]] ON [[shown]].[[data_source_id]] = [[row]].[[data_source_id]]
		JOIN {{leafen_database_notification_preferences}} AS [[follow]] ON [[follow]].[[container_id]] = [[shown]].[[container_id]]
		WHERE [[row]].[[id]] = {:rowId} AND [[follow]].[[user_id]] = {:userId} AND [[follow]].[[level]] = 'all_comments'
	`).WithContext(ctx).Bind(dbx.Params{"rowId": rowID, "userId": userID}).Row(&since)
	return since.Valid, since.Int64, err
}
