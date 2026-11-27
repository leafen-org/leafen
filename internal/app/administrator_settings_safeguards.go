package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The server's settings are saved in one request (PATCH /admin/settings),
// and some of them are an administrator's safeguards over who comes in: who
// may sign up, whether existing accounts may be added to workspaces, whether
// sign-up confirms the email address, and the sign-in providers. An agent
// changing one of these asks its person every time (user_key_admin_routes.go,
// decided 2026-11-13); a request that changes only the server's mail, map,
// updates, name and the rest goes ahead as before.

// administratorSettingsChangeSafeguards says the request changes one of
// these safeguards from what the server holds now, read as the request's
// handler reads it (fillAdministratorSettingsInput). Settings the server
// can't read, or a body that isn't the settings' JSON, ask.
func administratorSettingsChangeSafeguards(app core.App, _ userKeyAuthority, body []byte) bool {
	current, err := loadAdministratorSettings(app)
	if err != nil {
		return true
	}
	input := administratorSettingsInput{}
	fillAdministratorSettingsInput(&input, current)
	if json.Unmarshal(body, &input) != nil || normalizeAdministratorSettingsInput(&input) != nil {
		return true
	}
	google, oidc := input.Providers.Google, input.Providers.OIDC
	secretChanged := func(secret *string, clear bool, cipher string) bool {
		return secret != nil || clear && cipher != ""
	}
	return input.SignupPolicy != current.SignupPolicy || input.MemberAddPolicy != current.MemberAddPolicy ||
		input.SMTP.RequireEmailVerification != current.RequireEmailVerification ||
		google.Enabled != current.GoogleEnabled || google.ClientID != current.GoogleClientID ||
		secretChanged(google.ClientSecret, google.ClearSecret, current.GoogleSecretCipher) ||
		oidc.Enabled != current.OIDCEnabled || oidc.Name != current.OIDCName || oidc.Issuer != current.OIDCIssuer ||
		oidc.ClientID != current.OIDCClientID || strings.Join(oidc.Scopes, " ") != current.OIDCScopes ||
		secretChanged(oidc.ClientSecret, oidc.ClearSecret, current.OIDCSecretCipher)
}
