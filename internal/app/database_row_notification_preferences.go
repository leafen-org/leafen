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

// A database row page is followed, as in Notion (seen 2026-10-10), for all
// updates (new comments and property changes), all comments, or replies and
// @mentions, which is everyone's default and keeps no row. The level is the
// row's page record's notification preference.

const (
	databaseRowNotificationAllUpdates         = "all_updates"
	databaseRowNotificationRepliesAndMentions = "replies_and_mentions"
)

type databaseRowNotificationPreferenceResponse struct {
	RowID string `json:"rowId"`
	Level string `json:"level"`
}

// followCreatedRowComments has the person who made a database's row or
// template follow every comment on it, as a page's maker does (seen
// 2026-10-30: a row made in its table shows "알림 받기 › 댓글"; one made by
// an integration, or only edited by the person, "멘션").
func followCreatedRowComments(app core.App, workspaceID, rowID, creatorID string, now time.Time) error {
	return saveDatabaseRowNotificationLevel(app, workspaceID, rowID, creatorID, pageNotificationAllComments, now)
}

// followNewRowsComments has a person follow the rows a change of theirs made:
// the ones the change names that weren't there before it (newRowIDs).
func followNewRowsComments(app core.App, userID string, rowIDs []string) error {
	for _, rowID := range rowIDs {
		workspaceID := ""
		err := app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": rowID}).Row(&workspaceID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if err := followCreatedRowComments(app, workspaceID, rowID, userID, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

// newRowIDs lists the rows a change upserts that don't exist yet.
func newRowIDs(app core.App, operations []graphOperation) ([]string, error) {
	fresh := []string{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		existing := 0
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": operation.Row.ID}).Row(&existing); err != nil {
			return nil, err
		}
		if existing == 0 {
			fresh = append(fresh, operation.Row.ID)
		}
	}
	return fresh, nil
}

func registerDatabaseRowNotificationRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/database-rows/{rowId}/notification-preference", readDatabaseRowNotificationPreference)
	event.Router.PUT(apiRoot+"/database-rows/{rowId}/notification-preference", saveDatabaseRowNotificationPreference)
}

func loadDatabaseRowNotificationLevel(app core.App, rowID, userID string) (string, error) {
	level := ""
	err := app.DB().NewQuery(`
		SELECT [[level]] FROM {{leafen_page_notification_preferences}}
		WHERE [[page_id]] = {:rowId} AND [[user_id]] = {:userId}
	`).Bind(dbx.Params{"rowId": rowID, "userId": userID}).Row(&level)
	if errors.Is(err, sql.ErrNoRows) {
		return databaseRowNotificationRepliesAndMentions, nil
	}
	return level, err
}

// saveDatabaseRowNotificationLevel records how a person follows a row page.
// Replies and @mentions removes the row; a row kept keeps the time it began
// following (everyCommentFollowedSince).
func saveDatabaseRowNotificationLevel(app core.App, workspaceID, rowID, userID, level string, now time.Time) error {
	if level == databaseRowNotificationRepliesAndMentions {
		_, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_page_notification_preferences}}
			WHERE [[page_id]] = {:rowId} AND [[user_id]] = {:userId}
		`).Bind(dbx.Params{"rowId": rowID, "userId": userID}).Execute()
		return err
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_notification_preferences}}
			([[page_id]], [[user_id]], [[workspace_id]], [[level]], [[updated_at]])
		VALUES ({:rowId}, {:userId}, {:workspaceId}, {:level}, {:now})
		ON CONFLICT ([[page_id]], [[user_id]]) DO UPDATE SET [[level]] = excluded.[[level]]
	`).Bind(dbx.Params{
		"rowId": rowID, "userId": userID, "workspaceId": workspaceID, "level": level, "now": now.UnixMilli(),
	}).Execute()
	return err
}

func databaseRowNotificationPreferenceTarget(event *core.RequestEvent) (*core.Record, databaseRowPageAuthority, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return nil, databaseRowPageAuthority{}, false, apiError(
			event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.",
		)
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	// A template's page takes them as a database page's (database_template_row_page.go).
	authority, err := authorizeDatabaseRowOrTemplatePage(event.App, user.Id, rowID, permissionWorkspaceRead)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return nil, databaseRowPageAuthority{}, false, apiError(
			event, http.StatusNotFound, errorCodeDatabaseRowPageNotFound, "Database row not found.",
		)
	}
	if err != nil {
		return nil, databaseRowPageAuthority{}, false, apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't load the notification settings.", "database_row_notification_preference.authorize", "authorize",
			errorCodeNotificationSettingsFailed, err,
		)
	}
	return user, authority, true, nil
}

func readDatabaseRowNotificationPreference(event *core.RequestEvent) error {
	user, authority, ok, err := databaseRowNotificationPreferenceTarget(event)
	if !ok {
		return err
	}
	level, err := loadDatabaseRowNotificationLevel(event.App, authority.RowID, user.Id)
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't load the notification settings.", "database_row_notification_preference.read", "storage-read",
			errorCodeNotificationSettingsFailed, err,
		)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, databaseRowNotificationPreferenceResponse{RowID: authority.RowID, Level: level})
}

func saveDatabaseRowNotificationPreference(event *core.RequestEvent) error {
	user, authority, ok, err := databaseRowNotificationPreferenceTarget(event)
	if !ok {
		return err
	}
	input := databaseRowNotificationPreferenceResponse{}
	if err := event.BindBody(&input); err != nil || (input.Level != databaseRowNotificationAllUpdates &&
		input.Level != pageNotificationAllComments &&
		input.Level != databaseRowNotificationRepliesAndMentions) {
		return apiError(event, http.StatusBadRequest, errorCodeNotificationSettingsInvalid, "Check the notification settings.")
	}
	if err := saveDatabaseRowNotificationLevel(
		event.App, authority.WorkspaceID, authority.RowID, user.Id, input.Level, time.Now().UTC(),
	); err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't save the notification settings.", "database_row_notification_preference.save", "storage-write",
			errorCodeNotificationSettingsFailed, err,
		)
	}
	return event.JSON(http.StatusOK, databaseRowNotificationPreferenceResponse{
		RowID: authority.RowID, Level: input.Level,
	})
}
