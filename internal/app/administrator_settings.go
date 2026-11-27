package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var errAdministratorSettingsConflict = errors.New(errorCodeAdministratorSettingsConflict)

type storedAdministratorSettings struct {
	PlaceSearchEnabled       bool   `db:"place_search_enabled"`
	PlaceSearchConfigCipher  string `db:"place_search_config_cipher"`
	FormatVersion            int    `db:"format_version"`
	Revision                 int    `db:"revision"`
	SignupPolicy             string `db:"signup_policy"`
	SignupContact            string `db:"signup_contact"`
	MemberAddPolicy          string `db:"member_add_policy"`
	SMTPEnabled              bool   `db:"smtp_enabled"`
	SMTPHost                 string `db:"smtp_host"`
	SMTPPort                 int    `db:"smtp_port"`
	SMTPUsername             string `db:"smtp_username"`
	SMTPPasswordCipher       string `db:"smtp_password_cipher"`
	SMTPTLSMode              string `db:"smtp_tls_mode"`
	SenderName               string `db:"sender_name"`
	SenderAddress            string `db:"sender_address"`
	RequireEmailVerification bool   `db:"require_email_verification"`
	GoogleEnabled            bool   `db:"google_enabled"`
	GoogleClientID           string `db:"google_client_id"`
	GoogleSecretCipher       string `db:"google_secret_cipher"`
	OIDCEnabled              bool   `db:"oidc_enabled"`
	OIDCName                 string `db:"oidc_name"`
	OIDCIssuer               string `db:"oidc_issuer"`
	OIDCClientID             string `db:"oidc_client_id"`
	OIDCSecretCipher         string `db:"oidc_secret_cipher"`
	OIDCScopes               string `db:"oidc_scopes"`
	MapTilesEnabled          bool   `db:"map_tiles_enabled"`
	MapTileURL               string `db:"map_tile_url"`
	MapAttribution           string `db:"map_attribution"`
	MapAttributionURL        string `db:"map_attribution_url"`
	UpdateCheckEnabled       bool   `db:"update_check_enabled"`
	SwitcherListsEnabled     bool   `db:"switcher_lists_enabled"`
	ReadLogEnabled           bool   `db:"read_log_enabled"`
	ServerName               string `db:"server_name"`
	ServerAlias              string `db:"server_alias"`
}

func loadAdministratorSettings(app core.App) (storedAdministratorSettings, error) {
	settings := storedAdministratorSettings{}
	err := app.DB().NewQuery(`
		SELECT [[place_search_enabled]], [[place_search_config_cipher]], [[format_version]], [[revision]], [[signup_policy]], [[signup_contact]], [[member_add_policy]], [[smtp_enabled]], [[smtp_host]], [[smtp_port]],
			[[smtp_username]], [[smtp_password_cipher]], [[smtp_tls_mode]], [[sender_name]],
			[[sender_address]], [[require_email_verification]], [[google_enabled]],
			[[google_client_id]], [[google_secret_cipher]], [[oidc_enabled]], [[oidc_name]],
			[[oidc_issuer]], [[oidc_client_id]], [[oidc_secret_cipher]], [[oidc_scopes]],
			[[map_tiles_enabled]], [[map_tile_url]], [[map_attribution]], [[map_attribution_url]],
			[[update_check_enabled]], [[switcher_lists_enabled]], [[read_log_enabled]],
			[[server_name]], (SELECT [[alias]] FROM {{leafen_server_identity}} WHERE [[id]] = 'current') AS [[server_alias]]
		FROM {{leafen_server_settings}} WHERE [[id]] = 'default'
	`).One(&settings)
	return settings, err
}

// projectAdministratorSettings is what the administrators read; origin is
// the server's address the request sees, which the sign-in providers' return
// addresses are made from (none when it can't be read).
func projectAdministratorSettings(app core.App, settings storedAdministratorSettings, origin string) administratorSettingsResponse {
	// The server's own number shows to its administrators; one unread shows empty.
	serverID, _ := loadServerID(app)
	// Policies the data keeps that this server, without the organization
	// module, doesn't enforce; none read shows none.
	unenforced, _ := unenforcedOrganizationPolicies(app)
	googleReturn := authProviderRedirectURI(origin, authProviderGoogle)
	oidcReturn := authProviderRedirectURI(origin, authProviderOIDC)
	googleRegistered, googleAdded := projectAuthProviderReturn(app, authProviderGoogle, settings.GoogleClientID, googleReturn)
	oidcRegistered, oidcAdded := projectAuthProviderReturn(app, authProviderOIDC, settings.OIDCClientID, oidcReturn)
	return administratorSettingsResponse{
		ServerID:                       serverID,
		ServerName:                     settings.ServerName,
		ServerAlias:                    settings.ServerAlias,
		UnenforcedOrganizationPolicies: unenforced,
		Version:                        settings.FormatVersion,
		Revision:                       settings.Revision,
		SignupPolicy:                   settings.SignupPolicy,
		SignupContact:                  settings.SignupContact,
		MemberAddPolicy:                settings.MemberAddPolicy,
		SMTP: administratorSMTPSettings{
			Enabled: settings.SMTPEnabled, Host: settings.SMTPHost, Port: settings.SMTPPort,
			Username: settings.SMTPUsername, PasswordConfigured: settings.SMTPPasswordCipher != "",
			TLSMode: settings.SMTPTLSMode, SenderName: settings.SenderName,
			SenderAddress:            settings.SenderAddress,
			RequireEmailVerification: settings.RequireEmailVerification,
		},
		Providers: administratorProviders{
			Google: administratorGoogleSettings{
				Enabled: settings.GoogleEnabled, ClientID: settings.GoogleClientID,
				SecretConfigured: settings.GoogleSecretCipher != "",
				RedirectURI:      googleReturn, RegisteredRedirectURI: googleRegistered, RedirectAdded: googleAdded,
			},
			OIDC: administratorOIDCSettings{
				Enabled: settings.OIDCEnabled, Name: settings.OIDCName, Issuer: settings.OIDCIssuer,
				ClientID: settings.OIDCClientID, SecretConfigured: settings.OIDCSecretCipher != "",
				Scopes:      strings.Fields(settings.OIDCScopes),
				RedirectURI: oidcReturn, RegisteredRedirectURI: oidcRegistered, RedirectAdded: oidcAdded,
			},
		},
		Map:           projectAdministratorMapSettings(settings),
		PlaceSearch:   projectAdministratorPlaceSearch(app, settings),
		Updates:       projectAdministratorUpdateSettings(app, settings),
		SwitcherLists: administratorSwitcherListSettings{Enabled: settings.SwitcherListsEnabled},
		ReadLog:       administratorReadLogSettings{Enabled: settings.ReadLogEnabled},
	}
}

func updateAdministratorSettings(event *core.RequestEvent) error {
	requestContext := event.Request.Context()
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	administrator, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	current, err := loadAdministratorSettings(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the administrator settings.", err)
	}
	// What the request sends goes over the settings as they are, so a
	// setting it leaves out stays (fillAdministratorSettingsInput).
	input := administratorSettingsInput{}
	fillAdministratorSettingsInput(&input, current)
	if err := event.BindBody(&input); err != nil {
		return administratorSettingsInputError(event)
	}
	if err := normalizeAdministratorSettingsInput(&input); err != nil {
		return administratorSettingsFieldError(event, err)
	}
	if input.Version != current.FormatVersion || input.Revision != current.Revision {
		return apiError(event, http.StatusConflict, errorCodeAdministratorSettingsConflict, "The settings changed elsewhere.")
	}

	ciphers, err := resolveAdministratorSecrets(event.App, []administratorSecretUpdate{
		{Current: current.SMTPPasswordCipher, Incoming: input.SMTP.Password, Clear: input.SMTP.ClearPassword},
		{Current: current.GoogleSecretCipher, Incoming: input.Providers.Google.ClientSecret, Clear: input.Providers.Google.ClearSecret},
		{Current: current.OIDCSecretCipher, Incoming: input.Providers.OIDC.ClientSecret, Clear: input.Providers.OIDC.ClearSecret},
	})
	if err != nil {
		return administratorSettingsSecretError(event, err)
	}
	smtpCipher, googleCipher, oidcCipher := ciphers[0], ciphers[1], ciphers[2]
	// A mail account signs in with its password; a server that needs no
	// sign-in (a relay on this machine or network) takes none, its user name
	// left empty.
	if input.SMTP.Enabled && input.SMTP.Username != "" && smtpCipher == "" {
		return administratorSettingsFieldError(event, errors.New(errorCodeAdministratorSMTPPasswordRequired))
	}
	if input.Providers.Google.Enabled && googleCipher == "" {
		return administratorSettingsFieldError(event, errors.New(errorCodeAdministratorGoogleSecretRequired))
	}
	if input.Providers.OIDC.Enabled && oidcCipher == "" {
		return administratorSettingsFieldError(event, errors.New(errorCodeAdministratorOIDCSecretRequired))
	}
	// An OpenID provider is saved only once its issuer answers with usable
	// settings (2026-10-16: an unreachable issuer was saved, and its sign-in
	// button led to an error).
	if input.Providers.OIDC.Enabled && (!current.OIDCEnabled || current.OIDCIssuer != input.Providers.OIDC.Issuer) {
		discoveryContext, cancel := context.WithTimeout(requestContext, 10*time.Second)
		_, discoveryErr := discoverAuthProvider(discoveryContext, authProviderConfig{Issuer: input.Providers.OIDC.Issuer})
		cancel()
		if discoveryErr != nil {
			code := errorCodeAuthProviderUnavailable
			if discoveryErr.Error() == errorCodeAuthProviderResponseInvalid {
				code = errorCodeAuthProviderResponseInvalid
			}
			return apiError(event, http.StatusUnprocessableEntity, code, "Couldn't find usable OpenID settings at this issuer.")
		}
	}

	next := storedSettingsFromInput(input, smtpCipher, googleCipher, oidcCipher)
	applyAdministratorMapInput(&next, input.Map, current)
	if err := applyAdministratorPlaceSearchInput(event.App, &next, input.PlaceSearch, current); err != nil {
		if errors.Is(err, errInvalidPlaceSearchSettings) {
			return administratorSettingsInputError(event)
		}
		return administratorSettingsSecretError(event, err)
	}
	next.UpdateCheckEnabled = current.UpdateCheckEnabled
	if input.Updates != nil {
		next.UpdateCheckEnabled = input.Updates.Enabled
	}
	checkNow := next.UpdateCheckEnabled && !current.UpdateCheckEnabled
	next.SignupContact = current.SignupContact
	if input.SignupContact != nil {
		next.SignupContact = *input.SignupContact
	}
	next.SwitcherListsEnabled = current.SwitcherListsEnabled
	if input.SwitcherLists != nil {
		next.SwitcherListsEnabled = input.SwitcherLists.Enabled
	}
	next.ReadLogEnabled = current.ReadLogEnabled
	if input.ReadLog != nil {
		next.ReadLogEnabled = input.ReadLog.Enabled
	}
	next.ServerName, next.ServerAlias = current.ServerName, current.ServerAlias
	if input.ServerName != nil {
		next.ServerName = *input.ServerName
	}
	// A key's call that changes a safeguard asks its person once the
	// settings check out (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	updatedAt := time.Now().UnixMilli()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_server_settings}} SET
 [[place_search_enabled]]={:placeSearchEnabled},[[place_search_config_cipher]]={:placeSearchConfigCipher},
				[[revision]] = [[revision]] + 1,
				[[signup_policy]] = {:signupPolicy}, [[signup_contact]] = {:signupContact},
				[[member_add_policy]] = {:memberAddPolicy},
				[[smtp_enabled]] = {:smtpEnabled}, [[smtp_host]] = {:smtpHost}, [[smtp_port]] = {:smtpPort},
				[[smtp_username]] = {:smtpUsername}, [[smtp_password_cipher]] = {:smtpPasswordCipher},
				[[smtp_tls_mode]] = {:smtpTLSMode}, [[sender_name]] = {:senderName},
				[[sender_address]] = {:senderAddress}, [[require_email_verification]] = {:requireEmailVerification},
				[[google_enabled]] = {:googleEnabled}, [[google_client_id]] = {:googleClientId},
				[[google_secret_cipher]] = {:googleSecretCipher}, [[oidc_enabled]] = {:oidcEnabled},
				[[oidc_name]] = {:oidcName}, [[oidc_issuer]] = {:oidcIssuer}, [[oidc_client_id]] = {:oidcClientId},
				[[oidc_secret_cipher]] = {:oidcSecretCipher}, [[oidc_scopes]] = {:oidcScopes},
				[[map_tiles_enabled]] = {:mapTilesEnabled}, [[map_tile_url]] = {:mapTileUrl},
				[[map_attribution]] = {:mapAttribution}, [[map_attribution_url]] = {:mapAttributionUrl},
				[[update_check_enabled]] = {:updateCheckEnabled},
				[[switcher_lists_enabled]] = {:switcherListsEnabled}, [[read_log_enabled]] = {:readLogEnabled}, [[server_name]] = {:serverName},
				[[updated_at]] = {:updatedAt}
			WHERE [[id]] = 'default' AND [[format_version]] = {:version} AND [[revision]] = {:revision}
		`).WithContext(requestContext).
			Bind(administratorSettingsParams(input, next, updatedAt)).
			Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return errAdministratorSettingsConflict
		}
		if checkNow {
			// Turning the check on looks for a newer release right away.
			if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_release_manifest_state}} SET [[next_check_at]] = 0
				WHERE [[id]] = 'default'`).WithContext(requestContext).Execute(); err != nil {
				return err
			}
		}
		if err := recordServerSettingsAudit(txApp, administrator.Id, input.Revision+1, current, next, updatedAt); err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, errAdministratorSettingsConflict) {
		return apiError(event, http.StatusConflict, errorCodeAdministratorSettingsConflict, "The settings changed elsewhere.")
	}
	if requestContext.Err() != nil || errors.Is(err, context.Canceled) {
		return apiErrorAndFlush(event, http.StatusServiceUnavailable, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the administrator settings.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the administrator settings.", err)
	}
	next.Revision = input.Revision + 1
	rememberReadLogRecording(event.App, next.ReadLogEnabled)
	if checkNow {
		notifyReleaseManifestRunner(event.App)
	}
	return event.JSON(http.StatusOK, projectAdministratorSettings(event.App, next, authProviderReturnOrigin(event)))
}

func administratorSettingsParams(input administratorSettingsInput, next storedAdministratorSettings, updatedAt int64) dbx.Params {
	return dbx.Params{
		"placeSearchEnabled": next.PlaceSearchEnabled, "placeSearchConfigCipher": next.PlaceSearchConfigCipher,
		"signupPolicy": next.SignupPolicy, "signupContact": next.SignupContact,
		"memberAddPolicy": next.MemberAddPolicy,
		"smtpEnabled":     next.SMTPEnabled, "smtpHost": next.SMTPHost, "smtpPort": next.SMTPPort,
		"smtpUsername": next.SMTPUsername, "smtpPasswordCipher": next.SMTPPasswordCipher,
		"smtpTLSMode": next.SMTPTLSMode, "senderName": next.SenderName, "senderAddress": next.SenderAddress,
		"requireEmailVerification": next.RequireEmailVerification, "googleEnabled": next.GoogleEnabled,
		"googleClientId": next.GoogleClientID, "googleSecretCipher": next.GoogleSecretCipher,
		"oidcEnabled": next.OIDCEnabled, "oidcName": next.OIDCName, "oidcIssuer": next.OIDCIssuer,
		"oidcClientId": next.OIDCClientID, "oidcSecretCipher": next.OIDCSecretCipher,
		"oidcScopes": next.OIDCScopes, "mapTilesEnabled": next.MapTilesEnabled, "mapTileUrl": next.MapTileURL,
		"mapAttribution": next.MapAttribution, "mapAttributionUrl": next.MapAttributionURL,
		"updateCheckEnabled":   next.UpdateCheckEnabled,
		"switcherListsEnabled": next.SwitcherListsEnabled, "readLogEnabled": next.ReadLogEnabled,
		"serverName": next.ServerName,
		"updatedAt":  updatedAt, "version": input.Version, "revision": input.Revision,
	}
}
