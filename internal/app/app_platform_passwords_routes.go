//go:build !leafen_noapps

package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerAppPasswordRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPlatformPersonPasswordsPath, listAppPasswordsRoute)
	event.Router.POST(apiRoot+appPlatformPersonPasswordCreatePath, createAppPasswordRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
	event.Router.DELETE(apiRoot+appPlatformPersonPasswordDeletePath, deleteAppPasswordRoute)
}

// appPasswordPerson is the person behind the request — signed in or through
// their key; an app's token can't make or read app passwords.
func appPasswordPerson(event *core.RequestEvent) (*core.Record, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return nil, false
	}
	if appTokenCall(event) {
		_ = apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "An app can't make or read app passwords.")
		return nil, false
	}
	return user, true
}

func appPasswordFailure(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errAppPasswordInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeAppPasswordInvalid, "Name the password in up to 100 characters on one line.")
	case errors.Is(err, errAppPasswordLimit):
		return apiError(event, http.StatusConflict, errorCodeAppPasswordLimit, "An app can have up to 20 passwords; revoke one you don't use.")
	case errors.Is(err, errAppNotUsable), errors.Is(err, sql.ErrNoRows):
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "Not found.")
	}
	return appPlatformFailure(event, err)
}

func appPasswordList(event *core.RequestEvent, userID string) (appPasswordListResponse, error) {
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return appPasswordListResponse{}, err
	}
	return listAppPasswords(event.App, userID, origin)
}

func listAppPasswordsRoute(event *core.RequestEvent) error {
	user, ok := appPasswordPerson(event)
	if !ok {
		return nil
	}
	response, err := appPasswordList(event, user.Id)
	if err != nil {
		return appPasswordFailure(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func createAppPasswordRoute(event *core.RequestEvent) error {
	user, ok := appPasswordPerson(event)
	if !ok {
		return nil
	}
	input := appPasswordCreateInput{}
	if event.BindBody(&input) != nil {
		return appPasswordFailure(event, errAppPasswordInvalid)
	}
	// The app a password signs in to is named: one left out is the request's
	// to fix, not an app missing (2026-11-26, an agent asked for a password
	// without one was told "Not found.").
	if strings.TrimSpace(input.InstallID) == "" || strings.TrimSpace(input.Tenancy) == "" {
		return apiError(event, http.StatusBadRequest, errorCodeAppPasswordInvalid,
			"Check the app password: installId and tenancy name the app it logs in to, as GET /account/app-passwords lists its apps.")
	}
	password, secret, err := createAppPassword(event.App, user.Id, input.InstallID, input.Tenancy, input.Name)
	if err != nil {
		return appPasswordFailure(event, err)
	}
	// The name an app signs in with: the person's email, or the password's ID
	// where they have none (appEndpointSignIn takes either).
	username := user.Email()
	if username == "" {
		username = password.ID
	}
	return event.JSON(http.StatusCreated, appPasswordCreateResponse{Password: password, Secret: secret, Username: username})
}

func deleteAppPasswordRoute(event *core.RequestEvent) error {
	user, ok := appPasswordPerson(event)
	if !ok {
		return nil
	}
	if err := deleteAppPassword(event.App, user.Id, event.Request.PathValue("passwordId")); err != nil {
		return appPasswordFailure(event, err)
	}
	response, err := appPasswordList(event, user.Id)
	if err != nil {
		return appPasswordFailure(event, err)
	}
	return event.JSON(http.StatusOK, response)
}
