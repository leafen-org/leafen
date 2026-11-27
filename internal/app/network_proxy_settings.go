package app

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"leafen.org/leafen/internal/netproxy"
)

// The server's network proxy (decided 2026-10-21 18:00): its requests out —
// link previews, webhooks, imports, the services it connects — go by the
// proxy an administrator sets, else the environment's or the system's
// (netproxy), with nothing to set on a network whose system says so. The proxy's
// password is sealed with the administrator settings key. On the desktop
// app's own server the computer's setting is the app's (its This computer
// settings), which the server leaves as it is.

const maxNetworkProxyTextLength = 2048

type storedNetworkProxy struct {
	Mode           string `db:"mode"`
	URL            string `db:"url"`
	NoProxy        string `db:"no_proxy"`
	Username       string `db:"username"`
	PasswordCipher string `db:"password_cipher"`
	Revision       int    `db:"revision"`
}

func loadNetworkProxy(app core.App) (storedNetworkProxy, error) {
	stored := storedNetworkProxy{}
	err := app.DB().NewQuery(`SELECT [[mode]], [[url]], [[no_proxy]], [[username]], [[password_cipher]], [[revision]]
		FROM {{leafen_network_proxy_settings}} WHERE [[id]] = 'default'`).One(&stored)
	return stored, err
}

// networkProxyManagedByApp is the desktop app's own server, whose computer
// the app sets the proxy for.
func networkProxyManagedByApp(app core.App) bool { return app.Store().Has(localOwnerStoreKey) }

// networkProxyConfig is the administrator's proxy, its password opened, or
// nil to follow the environment and the system.
func networkProxyConfig(app core.App, stored storedNetworkProxy) (*netproxy.Config, error) {
	if stored.Mode != networkProxyManual || stored.URL == "" {
		return nil, nil
	}
	proxy, err := url.Parse(stored.URL)
	if err != nil {
		return nil, err
	}
	if stored.Username != "" {
		password, err := decryptAdministratorSetting(app, stored.PasswordCipher)
		if err != nil {
			return nil, err
		}
		proxy.User = url.UserPassword(stored.Username, password)
	}
	return &netproxy.Config{URL: proxy.String(), NoProxy: stored.NoProxy}, nil
}

// applyNetworkProxy makes the administrator's proxy the server's.
func applyNetworkProxy(app core.App) error {
	if networkProxyManagedByApp(app) {
		return nil
	}
	stored, err := loadNetworkProxy(app)
	if err != nil {
		return err
	}
	config, err := networkProxyConfig(app, stored)
	if err != nil {
		return err
	}
	return netproxy.Set(config)
}

func registerNetworkProxy(event *core.ServeEvent) {
	if err := applyNetworkProxy(event.App); err != nil {
		event.App.Logger().Warn("network proxy: couldn't apply the administrator's proxy", "error", err)
	}
	event.Router.GET(apiRoot+"/admin/network-proxy", readNetworkProxy)
	event.Router.PUT(apiRoot+"/admin/network-proxy", saveNetworkProxy)
}

func projectNetworkProxy(app core.App, stored storedNetworkProxy) administratorNetworkProxy {
	state := netproxy.Current()
	return administratorNetworkProxy{Mode: stored.Mode, URL: stored.URL, NoProxy: stored.NoProxy, Username: stored.Username,
		PasswordSet: stored.PasswordCipher != "", Revision: stored.Revision, Source: state.Source, HTTP: state.HTTP, HTTPS: state.HTTPS,
		AutoConfig: state.AutoConfig, AutoDiscover: state.AutoDiscover, Unfollowed: state.Unfollowed, ManagedByApp: networkProxyManagedByApp(app)}
}

func readNetworkProxy(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	stored, err := loadNetworkProxy(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the network proxy.", err)
	}
	return event.JSON(http.StatusOK, projectNetworkProxy(event.App, stored))
}

// saveNetworkProxy sets the server's proxy, the change kept in the server
// audit log (the password only as set or cleared), and goes by it at once.
func saveNetworkProxy(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	if networkProxyManagedByApp(event.App) {
		return apiError(event, http.StatusConflict, errorCodeInvalidInput, "This computer's proxy is set in the app's This computer settings.")
	}
	input := administratorNetworkProxyInput{}
	if err := event.BindBody(&input); err != nil || (input.Mode != networkProxySystem && input.Mode != networkProxyManual) {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the network proxy.")
	}
	next := storedNetworkProxy{Mode: input.Mode}
	if input.Mode == networkProxyManual {
		proxy, err := netproxy.Check(netproxy.Config{URL: input.URL})
		if err != nil || proxy.User != nil || len(input.NoProxy) > maxNetworkProxyTextLength || len(input.Username) > 256 ||
			len(input.Password) > maxAdministratorSecretLength || strings.ContainsAny(input.NoProxy+input.Username, "\r\n") {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the proxy: an http:// or https:// address with its port.")
		}
		next.URL, next.NoProxy, next.Username = proxy.String(), strings.TrimSpace(input.NoProxy), strings.TrimSpace(input.Username)
	}
	current, err := loadNetworkProxy(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the network proxy.", err)
	}
	if input.Revision != current.Revision {
		return apiError(event, http.StatusConflict, errorCodeInvalidInput, "The network proxy changed meanwhile. Reload it and try again.")
	}
	// The password stays unless a new one is given or it is cleared; a
	// proxy with no user name keeps none.
	next.PasswordCipher = current.PasswordCipher
	switch {
	case next.Username == "" || input.ClearPassword:
		next.PasswordCipher = ""
	case input.Password != "":
		if next.PasswordCipher, err = encryptAdministratorSetting(event.App, cipherNetworkProxy, input.Password); err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the network proxy.", err)
		}
	}
	next.Revision = current.Revision + 1
	actor := ""
	if event.Auth != nil {
		actor = event.Auth.Id
	}
	passwordState := func(cipher string) string {
		if cipher == "" {
			return ""
		}
		return "set"
	}
	err = event.App.RunInTransaction(func(tx core.App) error {
		if _, err := tx.DB().NewQuery(`UPDATE {{leafen_network_proxy_settings}} SET [[mode]] = {:mode}, [[url]] = {:url},
			[[no_proxy]] = {:noProxy}, [[username]] = {:username}, [[password_cipher]] = {:password}, [[revision]] = {:revision}
			WHERE [[id]] = 'default'`).Bind(dbx.Params{"mode": next.Mode, "url": next.URL, "noProxy": next.NoProxy, "username": next.Username,
			"password": next.PasswordCipher, "revision": next.Revision}).Execute(); err != nil {
			return err
		}
		at := time.Now().UnixMilli()
		for _, change := range [][3]string{
			{"network_proxy_mode", current.Mode, next.Mode},
			{"network_proxy_url", current.URL, next.URL},
			{"network_proxy_no_proxy", current.NoProxy, next.NoProxy},
			{"network_proxy_username", current.Username, next.Username},
			{"network_proxy_password", passwordState(current.PasswordCipher), passwordState(next.PasswordCipher)},
		} {
			if change[1] == change[2] && !(change[0] == "network_proxy_password" && input.Password != "") {
				continue
			}
			if _, err := tx.DB().Insert("leafen_server_settings_audit_events", dbx.Params{
				"id": core.GenerateDefaultRandomId(), "actor_id": actor, "revision": next.Revision, "field": change[0],
				"from_value": change[1], "to_value": change[2], "created_at": at,
			}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the network proxy.", err)
	}
	if err := applyNetworkProxy(event.App); err != nil {
		event.App.Logger().Warn("network proxy: couldn't apply the administrator's proxy", "error", err)
	}
	return event.JSON(http.StatusOK, projectNetworkProxy(event.App, next))
}
