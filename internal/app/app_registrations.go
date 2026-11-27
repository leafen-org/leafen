package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Apps a person registers to use Leafen from outside: a name and the
// addresses it returns to. An app sends people to /oauth/authorize with
// its id as client_id, the native API (<origin>/api/leafen/v1) as resource
// and a key's scope; each person who allows it chooses the workspaces it
// reaches and gets a connection acting as them within that scope. The
// registration stays while its person keeps it; removing it ends every
// connection made with it.

type storedAppRegistration struct {
	ID               string `db:"id"`
	OwnerID          string `db:"owner_id"`
	RequestID        string `db:"request_id"`
	ClientName       string `db:"client_name"`
	RedirectURIsJSON string `db:"redirect_uris_json"`
	Created          int64  `db:"created"`
	Updated          int64  `db:"updated"`
	ActsAs           string `db:"acts_as"`
}

// findRegisteredAppClient reads a registered app as an OAuth client.
func findRegisteredAppClient(app core.App, clientID string) (storedMCPOAuthClient, error) {
	var client storedMCPOAuthClient
	err := app.DB().NewQuery(`
		SELECT [[id]], [[client_name]], [[redirect_uris_json]], '' AS [[metadata_hash]], [[created]]
		FROM {{leafen_app_registrations}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": clientID}).One(&client)
	return client, err
}

func registerAppRegistrationRoutes(event *core.ServeEvent) {
	limit := apis.BodyLimit(16 << 10)
	event.Router.GET(apiRoot+appRegistrationListPath, listAppRegistrations)
	event.Router.POST(apiRoot+appRegistrationCreatePath, createAppRegistration).Bind(limit)
	event.Router.PATCH(apiRoot+appRegistrationUpdatePath, updateAppRegistration).Bind(limit)
	event.Router.DELETE(apiRoot+appRegistrationDeletePath, deleteAppRegistration)
}

func appRegistrationWireFor(app core.App, row storedAppRegistration) appRegistrationWire {
	redirects := []string{}
	_ = json.Unmarshal([]byte(row.RedirectURIsJSON), &redirects)
	wire := appRegistrationWire{ID: row.ID, Name: row.ClientName, RedirectURIs: redirects, CreatedAt: row.Created, ActsAs: row.ActsAs}
	if wire.ActsAs == "" {
		wire.ActsAs = appActsAsPerson
	}
	_ = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_mcp_connections}}
		WHERE [[oauth_client_id]] = {:id} AND [[revoked_at]] = 0`).Bind(dbx.Params{"id": row.ID}).Row(&wire.Installs)
	return wire
}

func normalizeAppRegistrationInput(input appRegistrationInput) (string, []string, bool) {
	name := strings.TrimSpace(input.Name)
	redirects := uniqueNonemptyStrings(input.RedirectURIs)
	if name == "" || utf8.RuneCountInString(name) > appRegistrationMaxNameRunes ||
		len(redirects) == 0 || len(redirects) > appRegistrationMaxRedirects {
		return "", nil, false
	}
	for _, redirect := range redirects {
		if !validMCPOAuthRedirectURI(redirect) {
			return "", nil, false
		}
	}
	return name, redirects, true
}

func listAppRegistrations(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	rows := []storedAppRegistration{}
	if err := event.App.DB().NewQuery(`SELECT * FROM {{leafen_app_registrations}} WHERE [[owner_id]] = {:owner}
		ORDER BY [[created]], [[id]]`).Bind(dbx.Params{"owner": user.Id}).All(&rows); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAppRegistrationFailed,
			"Couldn't list the apps.", "app_registration", "list", errorCodeAppRegistrationFailed, err)
	}
	response := appRegistrationListResponse{Apps: []appRegistrationWire{}}
	for _, row := range rows {
		response.Apps = append(response.Apps, appRegistrationWireFor(event.App, row))
	}
	return event.JSON(http.StatusOK, response)
}

func createAppRegistration(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := appRegistrationInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppRegistrationInvalid, "Check the app.")
	}
	name, redirects, valid := normalizeAppRegistrationInput(input)
	requestID := strings.TrimSpace(input.RequestID)
	if !valid || requestID == "" || len(requestID) > 255 {
		return apiError(event, http.StatusBadRequest, errorCodeAppRegistrationInvalid,
			"Give the app a name and https addresses to return to (http only for this computer).")
	}
	var row storedAppRegistration
	limited := false
	err := event.App.RunInTransaction(func(txApp core.App) error {
		err := txApp.DB().NewQuery(`SELECT * FROM {{leafen_app_registrations}}
			WHERE [[owner_id]] = {:owner} AND [[request_id]] = {:request}`).
			Bind(dbx.Params{"owner": user.Id, "request": requestID}).One(&row)
		if err == nil || !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var count int
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_registrations}} WHERE [[owner_id]] = {:owner}`).
			Bind(dbx.Params{"owner": user.Id}).Row(&count); err != nil {
			return err
		}
		if count >= appRegistrationMaxPerOwner {
			limited = true
			return nil
		}
		encoded, _ := json.Marshal(redirects)
		now := time.Now().UTC().UnixMilli()
		actsAs := appActsAsPerson
		if input.ActsAs == appActsAsIntegration {
			actsAs = appActsAsIntegration
		}
		row = storedAppRegistration{
			ID: "lapp_" + security.RandomString(24), OwnerID: user.Id, RequestID: requestID, ClientName: name,
			RedirectURIsJSON: string(encoded), Created: now, Updated: now, ActsAs: actsAs,
		}
		_, err = txApp.DB().Insert("leafen_app_registrations", dbx.Params{
			"id": row.ID, "owner_id": row.OwnerID, "request_id": row.RequestID, "client_name": row.ClientName,
			"redirect_uris_json": row.RedirectURIsJSON, "created": now, "updated": now, "acts_as": actsAs,
		}).Execute()
		return err
	})
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAppRegistrationFailed,
			"Couldn't register the app.", "app_registration", "create", errorCodeAppRegistrationFailed, err)
	}
	if limited {
		return apiError(event, http.StatusConflict, errorCodeAppRegistrationLimit, "You have registered as many apps as you can.")
	}
	return event.JSON(http.StatusCreated, appRegistrationWireFor(event.App, row))
}

func loadOwnedAppRegistration(event *core.RequestEvent, ownerID string) (storedAppRegistration, error) {
	row := storedAppRegistration{}
	err := event.App.DB().NewQuery(`SELECT * FROM {{leafen_app_registrations}} WHERE [[id]] = {:id} AND [[owner_id]] = {:owner}`).
		Bind(dbx.Params{"id": strings.TrimSpace(event.Request.PathValue("appId")), "owner": ownerID}).One(&row)
	return row, err
}

func updateAppRegistration(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	row, err := loadOwnedAppRegistration(event, user.Id)
	if err != nil {
		return apiError(event, http.StatusNotFound, errorCodeAppRegistrationNotFound, "App not found.")
	}
	input := appRegistrationInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppRegistrationInvalid, "Check the app.")
	}
	name, redirects, valid := normalizeAppRegistrationInput(input)
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodeAppRegistrationInvalid,
			"Give the app a name and https addresses to return to (http only for this computer).")
	}
	encoded, _ := json.Marshal(redirects)
	row.ClientName, row.RedirectURIsJSON, row.Updated = name, string(encoded), time.Now().UTC().UnixMilli()
	if _, err := event.App.DB().NewQuery(`UPDATE {{leafen_app_registrations}} SET [[client_name]] = {:name},
		[[redirect_uris_json]] = {:redirects}, [[updated]] = {:updated} WHERE [[id]] = {:id}`).Bind(dbx.Params{
		"name": row.ClientName, "redirects": row.RedirectURIsJSON, "updated": row.Updated, "id": row.ID,
	}).Execute(); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAppRegistrationFailed,
			"Couldn't change the app.", "app_registration", "update", errorCodeAppRegistrationFailed, err)
	}
	return event.JSON(http.StatusOK, appRegistrationWireFor(event.App, row))
}

// deleteAppRegistration removes an app and ends every connection made with it.
func deleteAppRegistration(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	row, err := loadOwnedAppRegistration(event, user.Id)
	if err != nil {
		return apiError(event, http.StatusNotFound, errorCodeAppRegistrationNotFound, "App not found.")
	}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		now := time.Now().UnixMilli()
		if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_mcp_connections}}
			SET [[revoked_at]] = {:now}, [[retire_after]] = {:retireAfter}
			WHERE [[oauth_client_id]] = {:id} AND [[revoked_at]] = 0`).Bind(dbx.Params{
			"now": now, "retireAfter": now + int64(mcpCredentialRetentionMilliseconds), "id": row.ID,
		}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_mcp_oauth_authorization_workspaces}} WHERE [[authorization_id]] IN (
				SELECT [[id]] FROM {{leafen_mcp_oauth_authorizations}} WHERE [[client_id]] = {:id});
			DELETE FROM {{leafen_mcp_oauth_authorizations}} WHERE [[client_id]] = {:id};
			DELETE FROM {{leafen_app_registrations}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": row.ID}).Execute(); err != nil {
			return err
		}
		notifyMCPCredentialRegistryAfterCommit(txApp)
		return nil
	})
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAppRegistrationFailed,
			"Couldn't remove the app.", "app_registration", "delete", errorCodeAppRegistrationFailed, err)
	}
	return event.NoContent(http.StatusNoContent)
}
