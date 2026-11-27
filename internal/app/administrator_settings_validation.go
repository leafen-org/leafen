package app

import (
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

func normalizeAdministratorSettingsInput(input *administratorSettingsInput) error {
	input.SignupPolicy = strings.ToLower(strings.TrimSpace(input.SignupPolicy))
	input.MemberAddPolicy = strings.ToLower(strings.TrimSpace(input.MemberAddPolicy))
	// Whom people ask about an invitation where sign-up is closed: one line,
	// shown on the sign-up page as written (signup_policy.go).
	if input.SignupContact != nil {
		contact := strings.Join(strings.Fields(*input.SignupContact), " ")
		if utf8.RuneCountInString(contact) > administratorSignupContactRunes {
			return errors.New(errorCodeAdministratorSettingsInvalid)
		}
		input.SignupContact = &contact
	}
	// The server's name, as its people and their agents tell servers apart (server_name.go).
	if input.ServerName != nil {
		name, err := normalizeServerName(*input.ServerName)
		if err != nil {
			return err
		}
		input.ServerName = &name
	}
	input.SMTP.Host = strings.TrimSpace(input.SMTP.Host)
	input.SMTP.Username = strings.TrimSpace(input.SMTP.Username)
	input.SMTP.TLSMode = strings.ToLower(strings.TrimSpace(input.SMTP.TLSMode))
	input.SMTP.SenderName = strings.TrimSpace(input.SMTP.SenderName)
	input.SMTP.SenderAddress = strings.ToLower(strings.TrimSpace(input.SMTP.SenderAddress))
	input.Providers.Google.ClientID = strings.TrimSpace(input.Providers.Google.ClientID)
	input.Providers.OIDC.Name = strings.TrimSpace(input.Providers.OIDC.Name)
	input.Providers.OIDC.Issuer = strings.TrimRight(strings.TrimSpace(input.Providers.OIDC.Issuer), "/")
	input.Providers.OIDC.ClientID = strings.TrimSpace(input.Providers.OIDC.ClientID)
	if input.Version != 3 || input.Revision < 1 || !validAdministratorSignupPolicy(input.SignupPolicy) ||
		!validAdministratorMemberAddPolicy(input.MemberAddPolicy) || !validAdministratorSettingsLengths(*input) {
		return errors.New(errorCodeAdministratorSettingsInvalid)
	}
	if input.SMTP.Port < 1 || input.SMTP.Port > 65535 || !validAdministratorTLSMode(input.SMTP.TLSMode) {
		return errors.New(errorCodeAdministratorSettingsInvalid)
	}
	if (input.SMTP.ClearPassword && input.SMTP.Password != nil) ||
		(input.Providers.Google.ClearSecret && input.Providers.Google.ClientSecret != nil) ||
		(input.Providers.OIDC.ClearSecret && input.Providers.OIDC.ClientSecret != nil) {
		return errors.New(errorCodeAdministratorSettingsInvalid)
	}
	// Mail on names which field it is missing, as the screen shows it there.
	if input.SMTP.Enabled && input.SMTP.Host == "" {
		return errors.New(errorCodeAdministratorSMTPHostRequired)
	}
	if input.SMTP.Enabled {
		address, err := mail.ParseAddress(input.SMTP.SenderAddress)
		if err != nil || address.Address != input.SMTP.SenderAddress {
			return errors.New(errorCodeAdministratorSMTPSenderInvalid)
		}
	}
	if input.SMTP.RequireEmailVerification && !input.SMTP.Enabled {
		return errors.New(errorCodeAdministratorEmailVerificationNeedsSMTP)
	}
	if input.Providers.Google.Enabled && input.Providers.Google.ClientID == "" {
		return errors.New(errorCodeAdministratorGoogleClientIDRequired)
	}
	scopes, err := normalizeAdministratorScopes(input.Providers.OIDC.Scopes)
	if err != nil {
		return err
	}
	input.Providers.OIDC.Scopes = scopes
	if input.Providers.OIDC.Enabled {
		if err := administratorOIDCInputError(input.Providers.OIDC); err != nil {
			return err
		}
	}
	return normalizeAdministratorMapInput(input.Map)
}

func validAdministratorSettingsLengths(input administratorSettingsInput) bool {
	values := []string{
		input.SMTP.Host, input.SMTP.Username, input.SMTP.SenderName, input.SMTP.SenderAddress,
		input.Providers.Google.ClientID, input.Providers.OIDC.Name, input.Providers.OIDC.Issuer,
		input.Providers.OIDC.ClientID,
	}
	for _, value := range values {
		if len(value) > maxAdministratorSettingsTextLength {
			return false
		}
	}
	for _, secret := range []*string{input.SMTP.Password, input.Providers.Google.ClientSecret, input.Providers.OIDC.ClientSecret} {
		if secret != nil && (len(*secret) == 0 || len(*secret) > maxAdministratorSecretLength) {
			return false
		}
	}
	return true
}

func validAdministratorTLSMode(mode string) bool {
	return mode == administratorSMTPTLSNone || mode == administratorSMTPTLSStartTLS || mode == administratorSMTPTLSTLS
}

func validAdministratorSignupPolicy(policy string) bool {
	return policy == administratorSignupPolicyPublic || policy == administratorSignupPolicyClosed
}

func validAdministratorMemberAddPolicy(policy string) bool {
	return policy == administratorMemberAddPolicyEnabled || policy == administratorMemberAddPolicyDisabled
}

func normalizeAdministratorScopes(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > maxAdministratorOIDCScopes {
		return nil, errors.New(errorCodeAdministratorOIDCScopesInvalid)
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > maxAdministratorOIDCScopeLength || strings.ContainsAny(value, " \t\r\n") {
			return nil, errors.New(errorCodeAdministratorOIDCScopesInvalid)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

// administratorOIDCInputError names the first field an enabled OIDC sign-in misses.
func administratorOIDCInputError(input administratorOIDCSettingsInput) error {
	switch {
	case input.Name == "":
		return errors.New(errorCodeAdministratorOIDCNameRequired)
	case !validOIDCIssuer(input.Issuer):
		return errors.New(errorCodeAdministratorOIDCIssuerInvalid)
	case input.ClientID == "":
		return errors.New(errorCodeAdministratorOIDCClientIDRequired)
	case !containsAdministratorScope(input.Scopes, "openid"):
		return errors.New(errorCodeAdministratorOIDCScopesInvalid)
	}
	return nil
}

func containsAdministratorScope(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

type administratorSecretUpdate struct {
	Current  string
	Incoming *string
	Clear    bool
}

func resolveAdministratorSecrets(app core.App, updates []administratorSecretUpdate) ([]string, error) {
	resolved := make([]string, len(updates))
	plainValues := make([]string, 0, len(updates))
	plainIndexes := make([]int, 0, len(updates))
	for index, update := range updates {
		switch {
		case update.Clear:
			resolved[index] = ""
		case update.Incoming == nil:
			resolved[index] = update.Current
		default:
			plainValues = append(plainValues, *update.Incoming)
			plainIndexes = append(plainIndexes, index)
		}
	}
	if len(plainValues) == 0 {
		return resolved, nil
	}
	encrypted, err := encryptAdministratorSettings(app, cipherServerSettings, plainValues)
	if err != nil {
		return nil, err
	}
	for index, cipher := range encrypted {
		resolved[plainIndexes[index]] = cipher
	}
	return resolved, nil
}

func storedSettingsFromInput(input administratorSettingsInput, smtpCipher, googleCipher, oidcCipher string) storedAdministratorSettings {
	return storedAdministratorSettings{
		FormatVersion: input.Version, Revision: input.Revision, SignupPolicy: input.SignupPolicy,
		MemberAddPolicy: input.MemberAddPolicy,
		SMTPEnabled:     input.SMTP.Enabled, SMTPHost: input.SMTP.Host, SMTPPort: input.SMTP.Port,
		SMTPUsername: input.SMTP.Username, SMTPPasswordCipher: smtpCipher, SMTPTLSMode: input.SMTP.TLSMode,
		SenderName: input.SMTP.SenderName, SenderAddress: input.SMTP.SenderAddress,
		RequireEmailVerification: input.SMTP.RequireEmailVerification,
		GoogleEnabled:            input.Providers.Google.Enabled, GoogleClientID: input.Providers.Google.ClientID,
		GoogleSecretCipher: googleCipher, OIDCEnabled: input.Providers.OIDC.Enabled,
		OIDCName: input.Providers.OIDC.Name, OIDCIssuer: input.Providers.OIDC.Issuer,
		OIDCClientID: input.Providers.OIDC.ClientID, OIDCSecretCipher: oidcCipher,
		OIDCScopes: strings.Join(input.Providers.OIDC.Scopes, " "),
	}
}

// fillAdministratorSettingsInput writes the settings as a change writes
// them, which a change's request is then read over: a policy, a mail setting
// or a sign-in provider's it leaves out stays as it is, as Notion's PATCH
// keeps what isn't sent (2026-11-13: a change sending only some mail settings
// emptied the rest, and one without Google turned its sign-in off). Secrets
// are kept unless sent or cleared; the version and revision are the
// request's own. The screen sends every setting.
func fillAdministratorSettingsInput(input *administratorSettingsInput, current storedAdministratorSettings) {
	*input = administratorSettingsInput{
		SignupPolicy: current.SignupPolicy, MemberAddPolicy: current.MemberAddPolicy,
		SMTP: administratorSMTPSettingsInput{
			Enabled: current.SMTPEnabled, Host: current.SMTPHost, Port: current.SMTPPort,
			Username: current.SMTPUsername, TLSMode: current.SMTPTLSMode, SenderName: current.SenderName,
			SenderAddress: current.SenderAddress, RequireEmailVerification: current.RequireEmailVerification,
		},
		Providers: administratorProvidersInput{
			Google: administratorGoogleSettingsInput{Enabled: current.GoogleEnabled, ClientID: current.GoogleClientID},
			OIDC: administratorOIDCSettingsInput{
				Enabled: current.OIDCEnabled, Name: current.OIDCName, Issuer: current.OIDCIssuer,
				ClientID: current.OIDCClientID, Scopes: strings.Fields(current.OIDCScopes),
			},
		},
	}
}

func administratorSettingsInputError(event *core.RequestEvent) error {
	return apiError(event, 400, errorCodeAdministratorSettingsInvalid, "Check the administrator settings.")
}

// administratorSettingsFieldError says which field the settings miss, where
// a check knows it; anything else is the general refusal.
func administratorSettingsFieldError(event *core.RequestEvent, err error) error {
	switch err.Error() {
	case errorCodeAdministratorSMTPHostRequired:
		return apiError(event, 400, errorCodeAdministratorSMTPHostRequired, "Give the mail server's address.")
	case errorCodeAdministratorSMTPSenderInvalid:
		return apiError(event, 400, errorCodeAdministratorSMTPSenderInvalid, "Give the address mail is sent from.")
	case errorCodeAdministratorSMTPPasswordRequired:
		return apiError(event, 400, errorCodeAdministratorSMTPPasswordRequired, "Give the password for the mail account, or leave the user name empty for a server that needs no login.")
	case errorCodeAdministratorEmailVerificationNeedsSMTP:
		return apiError(event, 400, errorCodeAdministratorEmailVerificationNeedsSMTP, "Turn on SMTP to require email verification.")
	case errorCodeAdministratorGoogleClientIDRequired:
		return apiError(event, 400, errorCodeAdministratorGoogleClientIDRequired, "Give the Google client ID.")
	case errorCodeAdministratorGoogleSecretRequired:
		return apiError(event, 400, errorCodeAdministratorGoogleSecretRequired, "Give the Google client secret.")
	case errorCodeAdministratorOIDCNameRequired:
		return apiError(event, 400, errorCodeAdministratorOIDCNameRequired, "Give the OIDC login a display name.")
	case errorCodeAdministratorOIDCIssuerInvalid:
		return apiError(event, 400, errorCodeAdministratorOIDCIssuerInvalid, "Give the OIDC provider's https issuer address.")
	case errorCodeAdministratorOIDCClientIDRequired:
		return apiError(event, 400, errorCodeAdministratorOIDCClientIDRequired, "Give the OIDC client ID.")
	case errorCodeAdministratorOIDCSecretRequired:
		return apiError(event, 400, errorCodeAdministratorOIDCSecretRequired, "Give the OIDC client secret.")
	case errorCodeAdministratorOIDCScopesInvalid:
		return apiError(event, 400, errorCodeAdministratorOIDCScopesInvalid, "Give the OIDC scopes, with openid among them.")
	}
	return administratorSettingsInputError(event)
}

func administratorSettingsSecretError(event *core.RequestEvent, err error) error {
	return apiErrorFrom(event, 500, errorCodeAdministratorSecretFailed, "Couldn't save the administrator secret settings.", err)
}
