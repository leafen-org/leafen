//go:build !leafen_noapps

package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// setAppOAuthReturnRoute is an administrator saying an app's outside
// sign-in's provider has the current return address, or taking it back.
func setAppOAuthReturnRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	provider, ok := appOAuthProvider(manifest, event.Request.PathValue("provider"))
	if !ok {
		return appPlatformFailure(event, appOAuthProviderUnknown(event.Request.PathValue("provider")))
	}
	input := appOAuthReturnInput{}
	if err := event.BindBody(&input); err != nil {
		return appPlatformFailure(event, bodyRefusal(errAppInputInvalid, "redirect URI choice", err))
	}
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		_, changed, err := setKeptReturnAdded(txApp, appOAuthReturnKey(install.ID, provider.Key), origin+apiRoot+appOAuthCallbackPath, input.Added)
		if err != nil || !changed {
			return err
		}
		return recordAppAudit(txApp, install, "oauth-return", user.Id, map[string]any{"provider": provider.Key, "added": input.Added})
	})
	if errors.Is(err, sql.ErrNoRows) {
		return appPlatformFailure(event, refuse(errAppInputInvalid, "no one has logged in with %s through the app yet, so it keeps no redirect URI", provider.Key))
	}
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return listAppSecretsRoute(event)
}
