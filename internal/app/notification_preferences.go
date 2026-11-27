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

const (
	pageNotificationAllComments        = "all_comments"
	pageNotificationRepliesAndMentions = "replies_and_mentions"

	errorCodeNotificationSettingsInvalid = "LEAFEN_NOTIFICATION_SETTINGS_INVALID"
	errorCodeNotificationSettingsFailed  = "LEAFEN_NOTIFICATION_SETTINGS_FAILED"
)

type storedNotificationSettings struct {
	EmailActivity    bool `db:"email_activity"`
	EmailAlways      bool `db:"email_always"`
	EmailPageUpdates bool `db:"email_page_updates"`
	EmailDigest      bool `db:"email_digest"`
	Revision         int  `db:"revision"`
}

type notificationSettingsResponse struct {
	EmailActivity    bool `json:"emailActivity"`
	EmailAlways      bool `json:"emailAlways"`
	EmailPageUpdates bool `json:"emailPageUpdates"`
	EmailDigest      bool `json:"emailDigest"`
	// EmailAvailable reports whether this server can send email at all.
	EmailAvailable bool `json:"emailAvailable"`
	// EmailLinks reports whether emails can link back to the pages.
	EmailLinks bool `json:"emailLinks"`
	Revision   int  `json:"revision"`
}

type notificationSettingsInput struct {
	EmailActivity    *bool `json:"emailActivity"`
	EmailAlways      *bool `json:"emailAlways"`
	EmailPageUpdates *bool `json:"emailPageUpdates"`
	EmailDigest      *bool `json:"emailDigest"`
}

type pageNotificationPreferenceResponse struct {
	PageID string `json:"pageId"`
	Level  string `json:"level"`
}

type pageNotificationPreferenceInput struct {
	Level string `json:"level"`
}

func registerNotificationPreferenceRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/account/notifications", readNotificationSettings)
	event.Router.PATCH(apiRoot+"/account/notifications", updateNotificationSettings)
	event.Router.GET(apiRoot+"/pages/{id}/notification-preference", readPageNotificationPreference)
	event.Router.PUT(apiRoot+"/pages/{id}/notification-preference", savePageNotificationPreference)
	registerDatabaseNotificationPreferenceRoutes(event)
	registerDatabaseRowNotificationRoutes(event)
	registerWebPushRoutes(event)
	registerDesktopNoticeRoutes(event)
	registerWorkspaceUnreadRoutes(event)
	registerSlackNotificationRoutes(event)
	event.Router.GET(apiRoot+"/notification-email/unsubscribe", confirmNotificationEmailUnsubscribe)
	event.Router.POST(apiRoot+"/notification-email/unsubscribe", unsubscribeNotificationEmail)
}

// loadNotificationSettings reads a person's settings; someone who never
// changed them gets activity emails only when they are away.
func loadNotificationSettings(app core.App, userID string) (storedNotificationSettings, error) {
	settings := storedNotificationSettings{}
	err := app.DB().NewQuery(`
		SELECT [[email_activity]], [[email_always]], [[email_page_updates]], [[email_digest]], [[revision]]
		FROM {{leafen_notification_settings}} WHERE [[user_id]] = {:userId}
	`).Bind(dbx.Params{"userId": userID}).One(&settings)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotificationSettings{EmailActivity: true, EmailPageUpdates: true, EmailDigest: true}, nil
	}
	return settings, err
}

func saveNotificationSettings(
	app core.App,
	userID string,
	input notificationSettingsInput,
	now time.Time,
) (storedNotificationSettings, error) {
	settings := storedNotificationSettings{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := loadNotificationSettings(txApp, userID)
		if err != nil {
			return err
		}
		if input.EmailActivity != nil {
			current.EmailActivity = *input.EmailActivity
		}
		if input.EmailAlways != nil {
			current.EmailAlways = *input.EmailAlways
		}
		if input.EmailPageUpdates != nil {
			current.EmailPageUpdates = *input.EmailPageUpdates
		}
		if input.EmailDigest != nil {
			current.EmailDigest = *input.EmailDigest
		}
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_notification_settings}}
				([[user_id]], [[email_activity]], [[email_always]], [[email_page_updates]], [[email_digest]],
				 [[revision]], [[updated_at]])
			VALUES ({:userId}, {:activity}, {:always}, {:pageUpdates}, {:digest}, 1, {:now})
			ON CONFLICT ([[user_id]]) DO UPDATE SET
				[[email_activity]] = excluded.[[email_activity]],
				[[email_always]] = excluded.[[email_always]],
				[[email_page_updates]] = excluded.[[email_page_updates]],
				[[email_digest]] = excluded.[[email_digest]],
				[[revision]] = [[leafen_notification_settings]].[[revision]] + 1,
				[[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{
			"userId": userID, "activity": current.EmailActivity, "always": current.EmailAlways,
			"pageUpdates": current.EmailPageUpdates, "digest": current.EmailDigest, "now": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		if !current.EmailActivity {
			// Turning activity emails off also stops the ones still waiting.
			if err := skipPendingNotificationEmails(txApp, userID, "email_activity_off", now); err != nil {
				return err
			}
		}
		settings, err = loadNotificationSettings(txApp, userID)
		return err
	})
	return settings, err
}

func notificationSettingsState(app core.App, settings storedNotificationSettings) (notificationSettingsResponse, error) {
	administrator, err := loadAdministratorSettings(app)
	if err != nil {
		return notificationSettingsResponse{}, err
	}
	return notificationSettingsResponse{
		EmailActivity: settings.EmailActivity, EmailAlways: settings.EmailAlways,
		EmailPageUpdates: settings.EmailPageUpdates, EmailDigest: settings.EmailDigest,
		EmailAvailable: notificationEmailConfigured(administrator),
		EmailLinks:     othersLinkOrigin() != "", Revision: settings.Revision,
	}, nil
}

func readNotificationSettings(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	settings, err := loadNotificationSettings(event.App, user.Id)
	if err == nil {
		var response notificationSettingsResponse
		if response, err = notificationSettingsState(event.App, settings); err == nil {
			event.Response.Header().Set("Cache-Control", "no-store")
			return event.JSON(http.StatusOK, response)
		}
	}
	return apiErrorWithCause(
		event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
		"Couldn't load the notification settings.", "notification_settings.read", "storage-read",
		errorCodeNotificationSettingsFailed, err,
	)
}

func updateNotificationSettings(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := notificationSettingsInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeNotificationSettingsInvalid, "Check the notification settings.")
	}
	// Each is a change of its own; one at least is sent (api_missing_input.go).
	if input.EmailActivity == nil && input.EmailAlways == nil && input.EmailPageUpdates == nil && input.EmailDigest == nil {
		return apiError(event, http.StatusBadRequest, errorCodeNotificationSettingsInvalid,
			"Send at least one of emailActivity, emailAlways, emailPageUpdates and emailDigest to change.")
	}
	settings, err := saveNotificationSettings(event.App, user.Id, input, time.Now().UTC())
	if err == nil {
		var response notificationSettingsResponse
		if response, err = notificationSettingsState(event.App, settings); err == nil {
			return event.JSON(http.StatusOK, response)
		}
	}
	return apiErrorWithCause(
		event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
		"Couldn't save the notification settings.", "notification_settings.save", "storage-write",
		errorCodeNotificationSettingsFailed, err,
	)
}

func loadPageNotificationLevel(app core.App, pageID, userID string) (string, error) {
	level := ""
	err := app.DB().NewQuery(`
		SELECT [[level]] FROM {{leafen_page_notification_preferences}}
		WHERE [[page_id]] = {:pageId} AND [[user_id]] = {:userId}
	`).Bind(dbx.Params{"pageId": pageID, "userId": userID}).Row(&level)
	if errors.Is(err, sql.ErrNoRows) {
		return pageNotificationRepliesAndMentions, nil
	}
	return level, err
}

// followCreatedPageComments has the person who made a page follow every
// comment on it, as in Notion (seen 2026-10-16: a page just made shows
// "알림 받기 › 댓글"; someone it is shared with sees "멘션"). A page made by
// an import follows nobody: whoever made it worked elsewhere.
func followCreatedPageComments(app core.App, workspaceID, pageID, creatorID string, createdAt int64) error {
	return savePageNotificationLevel(app, workspaceID, pageID, creatorID, pageNotificationAllComments, time.UnixMilli(createdAt))
}

// savePageNotificationLevel records who follows every comment on a page.
// Replies and @mentions is everyone's default, so choosing it removes the row;
// a row kept keeps the time it began following (everyCommentFollowedSince).
func savePageNotificationLevel(app core.App, workspaceID, pageID, userID, level string, now time.Time) error {
	if level == pageNotificationRepliesAndMentions {
		_, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_page_notification_preferences}}
			WHERE [[page_id]] = {:pageId} AND [[user_id]] = {:userId}
		`).Bind(dbx.Params{"pageId": pageID, "userId": userID}).Execute()
		return err
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_notification_preferences}}
			([[page_id]], [[user_id]], [[workspace_id]], [[level]], [[updated_at]])
		VALUES ({:pageId}, {:userId}, {:workspaceId}, {:level}, {:now})
		ON CONFLICT ([[page_id]], [[user_id]]) DO UPDATE SET [[level]] = excluded.[[level]]
	`).Bind(dbx.Params{
		"pageId": pageID, "userId": userID, "workspaceId": workspaceID, "level": level,
		"now": now.UnixMilli(),
	}).Execute()
	return err
}

// pageNotificationPreferenceTarget is the signed-in person and the page whose
// notification setting they read or save; a page they can't read, or one in
// the trash, is not found.
func pageNotificationPreferenceTarget(event *core.RequestEvent) (*core.Record, pageAuthorization, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return nil, pageAuthorization{}, false, apiError(
			event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.",
		)
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	authorization, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) || (err == nil && authorization.Page.GetBool("in_trash")) {
		return nil, pageAuthorization{}, false, apiError(
			event, http.StatusNotFound, errorCodePageNotFound, "Page not found.",
		)
	}
	if err != nil {
		return nil, pageAuthorization{}, false, apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't load the notification settings.", "page_notification_preference.authorize", "authorize",
			errorCodeNotificationSettingsFailed, err,
		)
	}
	return user, authorization, true, nil
}

func readPageNotificationPreference(event *core.RequestEvent) error {
	user, authorization, ok, err := pageNotificationPreferenceTarget(event)
	if !ok {
		return err
	}
	level, err := loadPageNotificationLevel(event.App, authorization.Page.Id, user.Id)
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't load the notification settings.", "page_notification_preference.read", "storage-read",
			errorCodeNotificationSettingsFailed, err,
		)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, pageNotificationPreferenceResponse{PageID: authorization.Page.Id, Level: level})
}

func savePageNotificationPreference(event *core.RequestEvent) error {
	user, authorization, ok, err := pageNotificationPreferenceTarget(event)
	if !ok {
		return err
	}
	input := pageNotificationPreferenceInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeNotificationSettingsInvalid, "Check the notification settings.")
	}
	if input.Level == "" {
		return refuseMissingInput(event, errorCodeNotificationSettingsInvalid, "level", "replies_and_mentions, all_comments, or all_updates for a database's page.")
	}
	if !pageNotificationLevelFits(authorization.Page, input.Level) {
		return apiError(event, http.StatusBadRequest, errorCodeNotificationSettingsInvalid, "Check the notification settings.")
	}
	if err := savePageNotificationLevel(
		event.App, authorization.Workspace.WorkspaceID, authorization.Page.Id, user.Id, input.Level,
		time.Now().UTC(),
	); err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed,
			"Couldn't save the notification settings.", "page_notification_preference.save", "storage-write",
			errorCodeNotificationSettingsFailed, err,
		)
	}
	return event.JSON(http.StatusOK, pageNotificationPreferenceResponse{
		PageID: authorization.Page.Id, Level: input.Level,
	})
}

// pageNotificationLevelFits reports whether a page may be followed at a
// level: a database page for all updates, any page for all comments, and
// either for replies and @mentions.
func pageNotificationLevelFits(page *core.Record, level string) bool {
	if level == pageNotificationRepliesAndMentions {
		return true
	}
	return level == pageNotificationAllComments || level == databaseRowNotificationAllUpdates && isDatabaseRowPageRecord(page)
}
