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

// What an app has outside with this server's address follows a change of it
// (the multi-server decision's 2026-10-20 change "서버 주소는 바뀔 수 있다").
// A webhook sender or a program calling an app's endpoint at the old address
// is answered there while it forwards (forwardServedInPlace), and a hook
// whose last call came there says so beside its new address until one comes
// to the new one. An app's outside sign-in goes back through the return
// address its provider has, as the server's own sign-in does
// (auth_provider_redirects.go): the app's settings show the new value to add
// and take the administrator's word that it is added.

// appHookVia is the earlier address a call came to; "" for the current one.
func appHookVia(request *http.Request) string {
	if forward, ok := forwardFor(request); ok {
		return strings.TrimRight(forward.From, "/")
	}
	return ""
}

// appHookEarlierAddress is the earlier address a hook's last call came to,
// while it forwards here still; "" once one came to the current address.
func appHookEarlierAddress(app core.App, installID, key, tenancy string) (string, error) {
	var via string
	err := app.DB().NewQuery(`SELECT [[via]] FROM {{leafen_app_hook_events}}
		WHERE [[install_id]] = {:install} AND [[hook]] = {:hook} AND [[kind]] = 'hook' AND [[topic]] = {:tenancy}
		ORDER BY [[received_at]] DESC LIMIT 1`).
		Bind(dbx.Params{"install": installID, "hook": key, "tenancy": tenancy}).Row(&via)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil || via == "" || forwardedOrigin(via) == "" {
		return "", err
	}
	return via, nil
}

// appOAuthReturnKey names an app's outside sign-in among the kept return
// addresses.
func appOAuthReturnKey(installID, provider string) string {
	return "app:" + installID + ":" + provider
}

// appOAuthClientID is the client an app's outside sign-in signs in with.
func appOAuthClientID(app core.App, installID, provider string) string {
	secrets, err := appSecretValues(app, installID)
	if err != nil {
		return ""
	}
	idName, _ := appOAuthClientSecretNames(provider)
	return secrets[idName]
}

// appOAuthReturns are an app's outside sign-ins whose provider has an
// earlier return address that forwards to current: that address, and
// whether the administrator said the current one is added.
func appOAuthReturns(app core.App, install storedAppInstall, manifest appManifest, current string) []appOAuthReturnWire {
	returns := []appOAuthReturnWire{}
	for _, provider := range manifest.Network.OAuth {
		earlier, added := projectAuthProviderReturn(app, appOAuthReturnKey(install.ID, provider.Key),
			appOAuthClientID(app, install.ID, provider.Key), current)
		if earlier != "" {
			returns = append(returns, appOAuthReturnWire{Provider: provider.Key, Earlier: earlier, Added: added})
		}
	}
	return returns
}

// keepAppOAuthReturnsBefore keeps, for each app's outside sign-in with a
// client and none kept, the return address it had at the address just left,
// and names those needing the new one.
func keepAppOAuthReturnsBefore(app core.App, previous, address string) []string {
	installs, err := loadAppInstalls(app, "[[status]] = 'active' AND [[removed_at]] = 0", nil)
	if err != nil {
		app.Logger().Warn("leafen app sign-in return addresses not kept", "error", err)
		return nil
	}
	needed := []string{}
	for _, install := range installs {
		_, manifest, err := loadAppPackage(app, install.PackageHash)
		if err != nil || len(manifest.Network.OAuth) == 0 {
			continue
		}
		name := manifest.Name["en"]
		for _, other := range manifest.Name {
			if name == "" {
				name = other
			}
		}
		if name == "" {
			name = install.Slug
		}
		for _, provider := range manifest.Network.OAuth {
			clientID := appOAuthClientID(app, install.ID, provider.Key)
			if clientID == "" {
				continue
			}
			if keepEarlierReturn(app, appOAuthReturnKey(install.ID, provider.Key), clientID,
				previous+apiRoot+appOAuthCallbackPath, address+apiRoot+appOAuthCallbackPath) {
				needed = append(needed, name+" · "+provider.Key)
			}
		}
	}
	return needed
}

// keepEarlierReturn keeps uri as the return address the outside client key
// has, when none is kept, and says whether it isn't current.
func keepEarlierReturn(app core.App, key, clientID, uri, current string) bool {
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_auth_provider_redirects}}
			([[provider]], [[client_id]], [[redirect_uri]], [[succeeded_at]], [[added_uri]], [[updated_at]])
		VALUES ({:provider}, {:client}, {:uri}, 0, '', {:now})
		ON CONFLICT ([[provider]]) DO NOTHING
	`).Bind(dbx.Params{"provider": key, "client": clientID, "uri": uri, "now": time.Now().UnixMilli()}).Execute(); err != nil {
		app.Logger().Warn("leafen sign-in return address not kept", "provider", key, "error", err)
		return false
	}
	kept, ok := loadAuthProviderRedirect(app, key)
	return ok && kept.ClientID == clientID && !strings.EqualFold(kept.RedirectURI, current)
}
