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

// The return address a sign-in provider has for this server, kept so a
// change of the server's address doesn't stop Google or OIDC sign-in (the
// multi-server decision's 2026-10-20 change "서버 주소는 바뀔 수 있다": the
// provider's console holds the old address, which the server can't change).
// While the old address forwards here, sign-ins keep going back through it
// and its forward carries them on; the administrator is shown the new value
// to add at the provider and says once it is added, and the first sign-in
// that comes back to it ends the notice.

// authProviderRedirect is a provider's last return address a sign-in came
// back to (or the one it had when the address changed), and the current one
// the administrator says they added. Provider is the server's own sign-in
// ("google", "oidc"); the table takes any outside client registered with a
// return address here.
type authProviderRedirect struct {
	Provider    string `db:"provider"`
	ClientID    string `db:"client_id"`
	RedirectURI string `db:"redirect_uri"`
	SucceededAt int64  `db:"succeeded_at"`
	AddedURI    string `db:"added_uri"`
}

func loadAuthProviderRedirect(app core.App, provider string) (authProviderRedirect, bool) {
	kept := authProviderRedirect{}
	err := app.DB().NewQuery(`SELECT [[provider]], [[client_id]], [[redirect_uri]], [[succeeded_at]], [[added_uri]]
		FROM {{leafen_auth_provider_redirects}} WHERE [[provider]] = {:provider}`).
		Bind(dbx.Params{"provider": provider}).One(&kept)
	return kept, err == nil
}

// registeredAuthProviderReturn is the earlier return address the provider
// still has, while it forwards to current and the administrator hasn't said
// the current one is added; "" when sign-ins go back to current.
func registeredAuthProviderReturn(kept authProviderRedirect, clientID, current string) string {
	if current == "" || kept.ClientID != clientID || strings.EqualFold(kept.RedirectURI, current) ||
		!strings.EqualFold(movedResource(kept.RedirectURI), current) {
		return ""
	}
	return kept.RedirectURI
}

// authProviderReturnFor is where a sign-in starting at current goes back to:
// the provider's earlier address while it forwards here, else current.
func authProviderReturnFor(app core.App, config authProviderConfig, current string) string {
	kept, ok := loadAuthProviderRedirect(app, config.ID)
	if !ok || strings.EqualFold(kept.AddedURI, current) {
		return current
	}
	if registered := registeredAuthProviderReturn(kept, config.ClientID, current); registered != "" {
		return registered
	}
	return current
}

// keepAuthProviderReturn notes the return address a sign-in just came back
// to; one the administrator added stops being only added.
func keepAuthProviderReturn(app core.App, config authProviderConfig, redirectURI string) {
	now := time.Now().UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_auth_provider_redirects}}
			([[provider]], [[client_id]], [[redirect_uri]], [[succeeded_at]], [[added_uri]], [[updated_at]])
		VALUES ({:provider}, {:client}, {:uri}, {:now}, '', {:now})
		ON CONFLICT ([[provider]]) DO UPDATE SET
			[[client_id]] = excluded.[[client_id]], [[redirect_uri]] = excluded.[[redirect_uri]],
			[[succeeded_at]] = excluded.[[succeeded_at]], [[updated_at]] = excluded.[[updated_at]],
			[[added_uri]] = CASE WHEN [[added_uri]] = excluded.[[redirect_uri]] THEN '' ELSE [[added_uri]] END
		WHERE [[redirect_uri]] <> excluded.[[redirect_uri]] OR [[client_id]] <> excluded.[[client_id]]
			OR [[succeeded_at]] = 0 OR [[added_uri]] = excluded.[[redirect_uri]]
	`).Bind(dbx.Params{"provider": config.ID, "client": config.ClientID, "uri": redirectURI, "now": now}).Execute(); err != nil {
		app.Logger().Warn("leafen sign-in return address not kept", "provider", config.ID, "error", err)
	}
}

// keepAuthProviderReturnsBefore keeps, for each provider in use that has
// none kept yet, the return address it had at the address just left, and
// tells the administrators which providers need the new one.
func keepAuthProviderReturnsBefore(app core.App, previous, address string) {
	settings, err := loadAdministratorSettings(app)
	if err != nil {
		app.Logger().Warn("leafen sign-in return addresses not kept", "error", err)
		return
	}
	needed := []string{}
	for _, provider := range []struct{ id, client, name string }{
		{authProviderGoogle, settings.GoogleClientID, "Google"},
		{authProviderOIDC, settings.OIDCClientID, settings.OIDCName},
	} {
		if provider.client == "" || (provider.id == authProviderGoogle && !settings.GoogleEnabled) ||
			(provider.id == authProviderOIDC && !settings.OIDCEnabled) {
			continue
		}
		if keepEarlierReturn(app, provider.id, provider.client, authProviderRedirectURI(previous, provider.id),
			authProviderRedirectURI(address, provider.id)) {
			name := provider.name
			if name == "" {
				name = "OIDC"
			}
			needed = append(needed, name)
		}
	}
	// And the apps' outside sign-ins (app_platform_address_change.go); the
	// note opens the sign-in settings, or the apps' when only apps need it.
	target := "/admin/settings?section=providers"
	if len(needed) == 0 {
		target = appNoticeAdministratorTarget
	}
	needed = append(needed, keepAppOAuthReturnsBefore(app, previous, address)...)
	if len(needed) > 0 {
		notifySignInReturnChange(app, address, strings.Join(needed, ", "), target)
	}
	// The people's own clients on services (mcp_service_address_change.go).
	keepMCPServiceReturnsBefore(app, previous, address)
	// And the organization module's (its SAML sign-in).
	for _, follow := range addressChangeFollowers {
		follow(app, previous, address)
	}
}

// notifySignInReturnChange tells the administrators in their inbox that the
// named providers need the new return address.
func notifySignInReturnChange(app core.App, address, providers, target string) {
	administrators, err := serverAdministrators(app)
	if err != nil {
		app.Logger().Warn("leafen sign-in return notice failed", "error", err)
		return
	}
	NotifyReturnChange(app, administrators, "", address, providers, target)
}

// NotifyReturnChange tells people in their inbox that what title names needs
// the server's new return address; an organization's people see it in one
// of its workspaces.
func NotifyReturnChange(app core.App, people []string, organizationID, address, title, target string) {
	now := time.Now().UnixMilli()
	key := "sign-in-return:" + appNoticeID("sign-in-return", address, organizationID)
	projections := []productNotificationProjection{}
	for _, person := range people {
		workspaceID, err := personNoticeWorkspace(app, person, organizationID)
		if err != nil || workspaceID == "" {
			continue
		}
		projections = append(projections, productNotificationProjection{
			ID: appNoticeID("sign-in-return", address, organizationID, person), EventID: appNoticeID("sign-in-return-event", address, organizationID),
			WorkspaceID: workspaceID, UserID: person, ActivityKey: key, Kind: "system",
			PageID: agentNoticePagePrefix + "sign-in-return", Title: title,
			Target: target, OccurredAt: now,
		})
	}
	if len(projections) == 0 {
		return
	}
	if _, err := upsertProductNotifications(context.Background(), app, projections, now); err != nil {
		app.Logger().Warn("leafen sign-in return notice failed", "error", err)
	}
}

// projectAuthProviderReturn fills in what the administrators see of a
// provider's return address after a change: the earlier one sign-ins still
// go back through, and whether they said the current one is added.
func projectAuthProviderReturn(app core.App, provider, clientID, current string) (string, bool) {
	kept, ok := loadAuthProviderRedirect(app, provider)
	if !ok {
		return "", false
	}
	registered := registeredAuthProviderReturn(kept, clientID, current)
	return registered, registered != "" && strings.EqualFold(kept.AddedURI, current)
}

type authProviderReturnInput struct {
	Added bool `json:"added"`
}

// setAuthProviderReturnAdded is an administrator saying the current return
// address is added at the provider, so sign-ins go back to it, or taking it
// back when a sign-in there was refused.
func setAuthProviderReturnAdded(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	provider := event.Request.PathValue("provider")
	if provider != authProviderGoogle && provider != authProviderOIDC {
		return apiError(event, http.StatusNotFound, errorCodeAuthProviderNotFound, "Login provider not found.")
	}
	input := authProviderReturnInput{}
	if err := event.BindBody(&input); err != nil {
		return administratorSettingsInputError(event)
	}
	administrator, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	current := authProviderRedirectURI(authProviderReturnOrigin(event), provider)
	err := event.App.RunInTransaction(func(txApp core.App) error {
		kept, changed, err := setKeptReturnAdded(txApp, provider, current, input.Added)
		if err != nil || !changed {
			return err
		}
		// The server audit's revisions are the main settings'; these count
		// apart, under their own names.
		field := provider + "_return_address"
		revision := 0
		if err := txApp.DB().NewQuery(`SELECT COALESCE(MAX([[revision]]), 0) + 1 FROM {{leafen_server_settings_audit_events}}
			WHERE [[field]] = {:field}`).Bind(dbx.Params{"field": field}).Row(&revision); err != nil {
			return err
		}
		from, to := kept.RedirectURI, current
		if !input.Added {
			from, to = current, kept.RedirectURI
		}
		_, err = txApp.DB().Insert("leafen_server_settings_audit_events", dbx.Params{
			"id": core.GenerateDefaultRandomId(), "actor_id": administrator.Id, "revision": revision,
			"field": field, "from_value": from, "to_value": to, "created_at": time.Now().UnixMilli(),
		}).Execute()
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeAuthProviderNotFound, "No earlier redirect URI is kept for this provider.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the administrator settings.", err)
	}
	settings, err := loadAdministratorSettings(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the administrator settings.", err)
	}
	return event.JSON(http.StatusOK, projectAdministratorSettings(event.App, settings, authProviderReturnOrigin(event)))
}

// setKeptReturnAdded notes that the current return address is added at the
// outside client named key, or takes it back; sql.ErrNoRows when nothing is
// kept for it, and whether anything changed.
func setKeptReturnAdded(txApp core.App, key, current string, added bool) (authProviderRedirect, bool, error) {
	kept, ok := loadAuthProviderRedirect(txApp, key)
	if !ok || current == "" {
		return kept, false, sql.ErrNoRows
	}
	value := ""
	if added {
		value = current
	}
	if strings.EqualFold(kept.AddedURI, value) {
		return kept, false, nil
	}
	_, err := txApp.DB().NewQuery(`UPDATE {{leafen_auth_provider_redirects}}
		SET [[added_uri]] = {:added}, [[updated_at]] = {:now} WHERE [[provider]] = {:provider}`).
		Bind(dbx.Params{"added": value, "now": time.Now().UnixMilli(), "provider": key}).Execute()
	return kept, err == nil, err
}

// For the organization module, whose SAML sign-in's identity provider has
// this server's address too: the same kept return addresses.
var (
	KeptReturnFor = func(app core.App, key, clientID, current string) string {
		return authProviderReturnFor(app, authProviderConfig{ID: key, ClientID: clientID}, current)
	}
	KeepReturn = func(app core.App, key, clientID, uri string) {
		keepAuthProviderReturn(app, authProviderConfig{ID: key, ClientID: clientID}, uri)
	}
	KeepEarlierReturn   = keepEarlierReturn
	ProjectKeptReturn   = projectAuthProviderReturn
	OutwardServerOrigin = outwardServerOrigin
)

// SetKeptReturnAdded is setKeptReturnAdded for the organization module.
func SetKeptReturnAdded(txApp core.App, key, current string, added bool) (bool, error) {
	_, changed, err := setKeptReturnAdded(txApp, key, current, added)
	return changed, err
}

var addressChangeFollowers []func(app core.App, previous, address string)

// FollowAddressChange has the organization module run when the server's
// address changes from previous to address, as the server's own sign-in
// providers keep theirs (keepAuthProviderReturnsBefore).
func FollowAddressChange(follow func(app core.App, previous, address string)) {
	addressChangeFollowers = append(addressChangeFollowers, follow)
}
