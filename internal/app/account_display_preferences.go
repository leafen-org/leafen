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

// The account's display preferences other than its language, kept apart
// from the language row because that row also says the language was chosen
// (account_language.go). Each follows the person to every device, as
// Notion's "기본 설정" does (seen 2026-10-02): "한 주의 시작을 월요일로
// 설정하기" makes calendars and date pickers start their weeks on Monday,
// right away, the setting for Enter to add a new line makes Enter break the
// line in comment boxes, which Cmd/Ctrl+Enter then sends, "숫자 형식" sets
// the separators numbers and currencies show (accountNumberFormats), the time
// zone is the device's while "위치를 사용하여 자동으로 시간대 설정" is on, or
// the one chosen, and reminders without a zone of their own fire in it
// (loadAccountTimeZone), "텍스트 방향 제어 항상 표시" offers the block menu's
// text direction in a left-to-right language too, "날짜 형식" is the
// format new @date mentions take (date_mention_format.go), and the Developer
// item's "개발자 기능 활성화" shows the developer bar and page menu
// (developer_ids.go). Beside them the account keeps that the person pressed
// the workspace menu's GitHub line, which then shows them no more
// (account_github_support.go).

const (
	accountDisplayPreferencesPath      = "/account/display-preferences"
	accountDisplayPreferencesBodyBytes = 4 << 10

	errorCodeAccountDisplayPreferencesInvalid = "LEAFEN_ACCOUNT_DISPLAY_PREFERENCES_INVALID"
	errorCodeAccountDisplayPreferencesFailed  = "LEAFEN_ACCOUNT_DISPLAY_PREFERENCES_FAILED"
)

type accountDisplayPreferences struct {
	WeekStartsOnMonday bool   `json:"weekStartsOnMonday"`
	EnterAddsNewLine   bool   `json:"enterAddsNewLine"`
	DeveloperMode      bool   `json:"developerMode"`
	DateMentionFormat  string `json:"dateMentionFormat"`
	NumberFormat       string `json:"numberFormat"`
	TimeZoneAuto       bool   `json:"timeZoneAuto"`
	TimeZone           string `json:"timeZone"`
	// The block menu offers text direction whatever the language.
	AlwaysShowTextDirection bool `json:"alwaysShowTextDirection"`
	// The person opened Leafen's GitHub page from the workspace menu's line.
	GitHubSupportOpened bool `json:"githubSupportOpened"`
}

// A change names only the preferences it sets.
type accountDisplayPreferencesInput struct {
	WeekStartsOnMonday      *bool   `json:"weekStartsOnMonday"`
	EnterAddsNewLine        *bool   `json:"enterAddsNewLine"`
	DeveloperMode           *bool   `json:"developerMode"`
	DateMentionFormat       *string `json:"dateMentionFormat"`
	NumberFormat            *string `json:"numberFormat"`
	TimeZoneAuto            *bool   `json:"timeZoneAuto"`
	TimeZone                *string `json:"timeZone"`
	AlwaysShowTextDirection *bool   `json:"alwaysShowTextDirection"`
	GitHubSupportOpened     *bool   `json:"githubSupportOpened"`
}

// accountNumberFormats are Notion's "숫자 형식" (seen 2026-10-02): the
// separators of the person's language, "1,000,000.00" or "1.000.000,00".
var accountNumberFormats = map[string]struct{}{"default": {}, "comma_period": {}, "period_comma": {}}

func validAccountNumberFormat(format string) bool {
	_, ok := accountNumberFormats[format]
	return ok
}

// validAccountTimeZone holds a time zone to the ones this server knows, or
// none yet.
func validAccountTimeZone(zone string) bool {
	if zone == "" {
		return true
	}
	if len(zone) > 64 || strings.TrimSpace(zone) != zone {
		return false
	}
	_, err := time.LoadLocation(zone)
	return err == nil
}

// loadAccountTimeZone is the time zone a person's reminders without a zone
// of their own fire in; empty when the person has none.
func loadAccountTimeZone(app core.App, userID string) string {
	if userID == "" {
		return ""
	}
	var zone string
	if err := app.DB().NewQuery(`
		SELECT [[time_zone]] FROM {{leafen_account_display_preferences}} WHERE [[owner_id]] = {:ownerId}
	`).Bind(dbx.Params{"ownerId": userID}).Row(&zone); err != nil || !validAccountTimeZone(zone) {
		return ""
	}
	return zone
}

type storedAccountDisplayPreferences struct {
	WeekStartsMonday        int    `db:"week_starts_monday"`
	EnterAddsNewLine        int    `db:"enter_adds_new_line"`
	DeveloperMode           int    `db:"developer_mode"`
	DateMentionFormat       string `db:"date_mention_format"`
	NumberFormat            string `db:"number_format"`
	TimeZoneAuto            int    `db:"time_zone_auto"`
	TimeZone                string `db:"time_zone"`
	AlwaysShowTextDirection int    `db:"always_show_text_direction"`
	GitHubSupportOpened     int    `db:"github_support_opened"`
}

func registerAccountDisplayPreferenceRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+accountDisplayPreferencesPath, readAccountDisplayPreferences)
	event.Router.PATCH(apiRoot+accountDisplayPreferencesPath, saveAccountDisplayPreferences).
		Bind(apis.BodyLimit(accountDisplayPreferencesBodyBytes))
	registerAccountWorkspaceOrderRoutes(event)
}

func loadAccountDisplayPreferences(app core.App, ownerID string) (accountDisplayPreferences, error) {
	stored := storedAccountDisplayPreferences{}
	err := app.DB().NewQuery(`
		SELECT [[week_starts_monday]], [[enter_adds_new_line]], [[developer_mode]], [[date_mention_format]],
			[[number_format]], [[time_zone_auto]], [[time_zone]], [[always_show_text_direction]], [[github_support_opened]]
		FROM {{leafen_account_display_preferences}}
		WHERE [[owner_id]] = {:ownerId}
	`).Bind(dbx.Params{"ownerId": ownerID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return accountDisplayPreferences{
			DateMentionFormat: dateMentionFormatRelative, NumberFormat: "default", TimeZoneAuto: true,
		}, nil
	}
	if !validDateMentionFormat(stored.DateMentionFormat) {
		stored.DateMentionFormat = dateMentionFormatRelative
	}
	if !validAccountNumberFormat(stored.NumberFormat) {
		stored.NumberFormat = "default"
	}
	if !validAccountTimeZone(stored.TimeZone) {
		stored.TimeZone = ""
	}
	return accountDisplayPreferences{
		WeekStartsOnMonday:      stored.WeekStartsMonday == 1,
		EnterAddsNewLine:        stored.EnterAddsNewLine == 1,
		DeveloperMode:           stored.DeveloperMode == 1,
		DateMentionFormat:       stored.DateMentionFormat,
		NumberFormat:            stored.NumberFormat,
		TimeZoneAuto:            stored.TimeZoneAuto == 1,
		TimeZone:                stored.TimeZone,
		AlwaysShowTextDirection: stored.AlwaysShowTextDirection == 1,
		GitHubSupportOpened:     stored.GitHubSupportOpened == 1,
	}, err
}

func readAccountDisplayPreferences(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	preferences, err := loadAccountDisplayPreferences(event.App, user.Id)
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAccountDisplayPreferencesFailed,
			"Couldn't load the display settings.", "account_display_preferences.read", "storage-read",
			errorCodeAccountDisplayPreferencesFailed, err,
		)
	}
	return event.JSON(http.StatusOK, preferences)
}

func saveAccountDisplayPreferences(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := accountDisplayPreferencesInput{}
	bound := event.BindBody(&input)
	if bound == nil && input.WeekStartsOnMonday == nil && input.EnterAddsNewLine == nil && input.DeveloperMode == nil &&
		input.DateMentionFormat == nil && input.NumberFormat == nil && input.TimeZoneAuto == nil &&
		input.TimeZone == nil && input.AlwaysShowTextDirection == nil && input.GitHubSupportOpened == nil {
		// Each is a change of its own; one at least is sent (api_missing_input.go).
		return apiError(event, http.StatusBadRequest, errorCodeAccountDisplayPreferencesInvalid, "Send at least one of weekStartsOnMonday, "+
			"enterAddsNewLine, developerMode, dateMentionFormat, numberFormat, timeZoneAuto, timeZone, alwaysShowTextDirection and githubSupportOpened to change.")
	}
	if bound != nil || input.TimeZone != nil && !validAccountTimeZone(*input.TimeZone) ||
		input.DateMentionFormat != nil && !validDateMentionFormat(*input.DateMentionFormat) ||
		input.NumberFormat != nil && !validAccountNumberFormat(*input.NumberFormat) {
		return apiError(event, http.StatusBadRequest, errorCodeAccountDisplayPreferencesInvalid, "Check the display settings.")
	}
	flag := func(on bool) int {
		if on {
			return 1
		}
		return 0
	}
	preferences := accountDisplayPreferences{}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		current, err := loadAccountDisplayPreferences(txApp, user.Id)
		if err != nil {
			return err
		}
		if input.WeekStartsOnMonday != nil {
			current.WeekStartsOnMonday = *input.WeekStartsOnMonday
		}
		if input.EnterAddsNewLine != nil {
			current.EnterAddsNewLine = *input.EnterAddsNewLine
		}
		if input.DeveloperMode != nil {
			current.DeveloperMode = *input.DeveloperMode
		}
		if input.DateMentionFormat != nil {
			current.DateMentionFormat = *input.DateMentionFormat
		}
		if input.NumberFormat != nil {
			current.NumberFormat = *input.NumberFormat
		}
		if input.TimeZoneAuto != nil {
			current.TimeZoneAuto = *input.TimeZoneAuto
		}
		if input.TimeZone != nil {
			current.TimeZone = *input.TimeZone
		}
		if input.AlwaysShowTextDirection != nil {
			current.AlwaysShowTextDirection = *input.AlwaysShowTextDirection
		}
		if input.GitHubSupportOpened != nil {
			current.GitHubSupportOpened = *input.GitHubSupportOpened
		}
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_account_display_preferences}}
				([[owner_id]], [[week_starts_monday]], [[enter_adds_new_line]], [[developer_mode]],
				 [[date_mention_format]], [[number_format]], [[time_zone_auto]], [[time_zone]],
				 [[always_show_text_direction]], [[github_support_opened]], [[updated_at]])
			VALUES ({:ownerId}, {:weekStartsMonday}, {:enterAddsNewLine}, {:developerMode}, {:dateMentionFormat},
				{:numberFormat}, {:timeZoneAuto}, {:timeZone}, {:alwaysShowTextDirection}, {:githubSupportOpened}, {:updatedAt})
			ON CONFLICT ([[owner_id]]) DO UPDATE SET
				[[week_starts_monday]] = excluded.[[week_starts_monday]],
				[[enter_adds_new_line]] = excluded.[[enter_adds_new_line]],
				[[developer_mode]] = excluded.[[developer_mode]],
				[[date_mention_format]] = excluded.[[date_mention_format]],
				[[number_format]] = excluded.[[number_format]],
				[[time_zone_auto]] = excluded.[[time_zone_auto]],
				[[time_zone]] = excluded.[[time_zone]],
				[[always_show_text_direction]] = excluded.[[always_show_text_direction]],
				[[github_support_opened]] = excluded.[[github_support_opened]],
				[[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{
			"ownerId": user.Id, "weekStartsMonday": flag(current.WeekStartsOnMonday),
			"enterAddsNewLine": flag(current.EnterAddsNewLine), "developerMode": flag(current.DeveloperMode),
			"dateMentionFormat": current.DateMentionFormat, "numberFormat": current.NumberFormat,
			"timeZoneAuto": flag(current.TimeZoneAuto), "timeZone": current.TimeZone,
			"alwaysShowTextDirection": flag(current.AlwaysShowTextDirection),
			"githubSupportOpened":     flag(current.GitHubSupportOpened),
			"updatedAt":               time.Now().UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		preferences, err = loadAccountDisplayPreferences(txApp, user.Id)
		return err
	})
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAccountDisplayPreferencesFailed,
			"Couldn't save the display settings.", "account_display_preferences.save", "storage-write",
			errorCodeAccountDisplayPreferencesFailed, err,
		)
	}
	return event.JSON(http.StatusOK, preferences)
}
