package app

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Personal notifications can also reach a person in Slack, as Notion's Slack
// notifications do: the server operator names one of the Slack bot
// connections database automations use (LEAFEN_NOTIFICATION_SLACK_CONNECTION
// naming a key of LEAFEN_AUTOMATION_SLACK_CONNECTIONS), each person turns
// Slack notifications on for themselves, and a notification still unread ten
// seconds later is sent to them as a direct message from that bot. The bot
// finds the person in Slack by their Leafen email, and only once they turn it on.

const (
	slackNotificationConnectionEnvironment = "LEAFEN_NOTIFICATION_SLACK_CONNECTION"
	slackNotificationUserIDRunes           = 64
)

// slackNotificationConnection is the operator's bot connection for personal
// notifications and its token, or empty when Slack notifications are off.
func slackNotificationConnection(getenv func(string) string) (string, string) {
	id := strings.TrimSpace(getenv(slackNotificationConnectionEnvironment))
	if id == "" {
		return "", ""
	}
	connections, valid := databaseAutomationSlackConnections(getenv(databaseAutomationSlackConnectionsEnvironment))
	if !valid || connections[id] == "" {
		return "", ""
	}
	return id, connections[id]
}

type slackNotificationState struct {
	Available bool `json:"available"`
	Enabled   bool `json:"enabled"`
}

type slackNotificationInput struct {
	Enabled bool `json:"enabled"`
}

func registerSlackNotificationRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/account/slack-notifications", readSlackNotificationState)
	event.Router.PUT(apiRoot+"/account/slack-notifications", saveSlackNotificationState)
	event.Router.GET(apiRoot+automationSlackChannelsPath, readAutomationSlackChannels)
}

func readSlackNotificationState(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	state, err := loadSlackNotificationState(event.App, user.Id)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed, "Couldn't read the Slack notification settings.", err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, state)
}

func saveSlackNotificationState(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := slackNotificationInput{}
	if event.BindBody(&input) != nil {
		return apiError(event, http.StatusBadRequest, errorCodeNotificationSettingsInvalid, "Check the Slack notification settings.")
	}
	if connection, _ := slackNotificationConnection(os.Getenv); input.Enabled && connection == "" {
		return apiError(event, http.StatusConflict, errorCodeNotificationSettingsInvalid, "Slack notifications aren't configured on this server.")
	}
	now := time.Now().UnixMilli()
	if _, err := event.App.DB().NewQuery(`
		INSERT INTO {{leafen_slack_notification_settings}} ([[user_id]], [[enabled]], [[created_at]], [[updated_at]])
		VALUES ({:userId}, {:enabled}, {:now}, {:now})
		ON CONFLICT ([[user_id]]) DO UPDATE SET [[enabled]] = excluded.[[enabled]], [[updated_at]] = excluded.[[updated_at]]
	`).Bind(dbx.Params{"userId": user.Id, "enabled": input.Enabled, "now": now}).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed, "Couldn't save the Slack notification settings.", err)
	}
	state, err := loadSlackNotificationState(event.App, user.Id)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotificationSettingsFailed, "Couldn't read the Slack notification settings.", err)
	}
	return event.JSON(http.StatusOK, state)
}

func loadSlackNotificationState(app core.App, userID string) (slackNotificationState, error) {
	connection, _ := slackNotificationConnection(os.Getenv)
	state := slackNotificationState{Available: connection != ""}
	var enabled int
	err := app.DB().NewQuery(`SELECT [[enabled]] FROM {{leafen_slack_notification_settings}} WHERE [[user_id]] = {:userId}`).
		Bind(dbx.Params{"userId": userID}).Row(&enabled)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return state, err
	}
	state.Enabled = state.Available && enabled == 1
	return state, nil
}

// enqueueSlackNotificationDeliveries gives each new personal notification
// the Slack message it becomes, for people who turned Slack notifications on.
func enqueueSlackNotificationDeliveries(app core.App, projections []productNotificationProjection, now int64) error {
	if connection, _ := slackNotificationConnection(os.Getenv); connection == "" {
		return nil
	}
	enabled := map[string]bool{}
	queued := false
	for _, projection := range projections {
		if !projection.Personal {
			continue
		}
		on, known := enabled[projection.UserID]
		if !known {
			var count int
			if err := app.DB().NewQuery(`
				SELECT COUNT(*) FROM {{leafen_slack_notification_settings}} WHERE [[user_id]] = {:userId} AND [[enabled]] = 1
			`).Bind(dbx.Params{"userId": projection.UserID}).Row(&count); err != nil {
				return err
			}
			on = count > 0
			enabled[projection.UserID] = on
		}
		if !on {
			continue
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_slack_notification_deliveries}} (
				[[notification_id]], [[workspace_id]], [[user_id]], [[status]], [[attempts]],
				[[next_attempt_at]], [[created_at]], [[updated_at]]
			) VALUES ({:id}, {:workspaceId}, {:userId}, 'pending', 0, {:due}, {:now}, {:now})
			ON CONFLICT ([[notification_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": projection.ID, "workspaceId": projection.WorkspaceID, "userId": projection.UserID,
			"due": now + slackNotificationDelay.Milliseconds(), "now": now,
		}).Execute(); err != nil {
			return err
		}
		queued = true
	}
	if queued {
		notifySlackNotificationRunnerAfterCommit(app)
	}
	return nil
}
