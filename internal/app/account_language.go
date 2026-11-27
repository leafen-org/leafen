package app

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	accountLanguageReadOperation = "account_language.read"
	accountLanguageSaveOperation = "account_language.save"
)

// The languages an account can choose: the contract's selectable ones, which
// are all of its languages now. A language contracts/auth-wire.json marked
// "hidden" (its catalog not yet translated) would not be taken, though an
// account that chose it before would keep it stored and be shown English.
var supportedAccountLanguagePreferences = func() map[string]struct{} {
	preferences := make(map[string]struct{}, len(selectableAccountLanguagePreferenceValues))
	for _, preference := range selectableAccountLanguagePreferenceValues {
		preferences[preference] = struct{}{}
	}
	return preferences
}()

type storedAccountLanguagePreference struct {
	LanguagePreference string `db:"language_preference"`
}

func registerAccountLanguageRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/account/language", readAccountLanguage)
	event.Router.PUT(apiRoot+"/account/language", saveAccountLanguage)
}

func accountLanguageState(preference string) accountLanguageStateResponse {
	return accountLanguageStateResponse{
		LanguagePreference:          &preference,
		LanguageOnboardingCompleted: true,
	}
}

func storedAccountLanguage(app core.App, ownerID string) (storedAccountLanguagePreference, error) {
	var stored storedAccountLanguagePreference
	err := app.DB().NewQuery(`
		SELECT [[language_preference]]
		FROM {{leafen_account_preferences}}
		WHERE [[owner_id]] = {:ownerId}
	`).Bind(dbx.Params{"ownerId": ownerID}).One(&stored)
	return stored, err
}

func readAccountLanguage(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	stored, err := storedAccountLanguage(event.App, user.Id)
	if errors.Is(err, sql.ErrNoRows) {
		return event.JSON(http.StatusOK, accountLanguageStateResponse{})
	}
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAccountLanguageReadFailed,
			"Couldn't load the language setting.", accountLanguageReadOperation, "storage-read",
			errorCodeAccountLanguageReadFailed, err,
		)
	}
	return event.JSON(http.StatusOK, accountLanguageState(stored.LanguagePreference))
}

func saveAccountLanguage(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	var input accountLanguagePreferenceInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAccountLanguageInvalid, "Check the language setting.")
	}
	if input.LanguagePreference == "" {
		return refuseMissingInput(event, errorCodeAccountLanguageInvalid, "languagePreference", "a language code such as ko or en, or system to follow the device.")
	}
	if _, allowed := supportedAccountLanguagePreferences[input.LanguagePreference]; !allowed {
		return apiError(event, http.StatusBadRequest, errorCodeAccountLanguageInvalid, "Check the language setting.")
	}

	err := event.App.RunInTransaction(func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_account_preferences}} (
				[[owner_id]], [[language_preference]], [[updated_at]]
			) VALUES ({:ownerId}, {:preference}, {:updatedAt})
			ON CONFLICT ([[owner_id]]) DO UPDATE SET
				[[language_preference]] = excluded.[[language_preference]],
				[[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{
			"ownerId": user.Id, "preference": input.LanguagePreference,
			"updatedAt": time.Now().UnixMilli(),
		}).Execute()
		return err
	})
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAccountLanguageSaveFailed,
			"Couldn't save the language setting.", accountLanguageSaveOperation, "storage-write",
			errorCodeAccountLanguageSaveFailed, err,
		)
	}
	// It may be the administrator's, which is the server's language.
	forgetServerLanguage(event.App)
	return event.JSON(http.StatusOK, accountLanguageState(input.LanguagePreference))
}
