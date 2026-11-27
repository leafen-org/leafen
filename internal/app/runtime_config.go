package app

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const upstreamRepositoryURL = "https://github.com/leafen-org/leafen"

func registerRuntimeConfigRoute(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/runtime-config", readPublicRuntimeConfig)
}

func readPublicRuntimeConfig(event *core.RequestEvent) error {
	providers := []string{}
	emailRecovery := false
	settings, err := loadAdministratorSettings(event.App)
	// Where sign-up is closed the sign-up page says so as soon as it is
	// asked, with whom to ask if the administrator named someone; sign-up
	// the server's mail can't finish names them too (2026-11-07).
	signupClosed := err != nil || settings.SignupPolicy != administratorSignupPolicyPublic
	signupContact := ""
	if err == nil {
		signupContact = settings.SignupContact
	}
	if err == nil {
		if settings.GoogleEnabled && settings.GoogleClientID != "" && settings.GoogleSecretCipher != "" {
			providers = append(providers, authProviderGoogle)
		}
		if settings.OIDCEnabled && settings.OIDCClientID != "" && settings.OIDCSecretCipher != "" {
			providers = append(providers, authProviderOIDC)
		}
		emailRecovery = settings.SMTPEnabled && settings.SMTPHost != "" && settings.SenderAddress != ""
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	// A server whose data moved elsewhere says so, and where (replica_runner.go).
	moved := readReplicaFence(event.App.DataDir())
	movedTo := ""
	if moved != nil {
		movedTo = moved.MovedTo
	}
	// A standby says whose; it takes the primary's data when it takes over.
	standbyOf := ""
	backups, backupsErr := loadScheduledBackup(event.App)
	if backupsErr == nil && backups.StandbyEnabled {
		standbyOf = firstNonEmpty(backups.StandbyPrimaryOrigin, "standby")
	}
	// The server's code names Leafen's entry in an agent's settings with its
	// name (agent_entry_names.go); it is on the sign-in screens already.
	serverCode, _ := loadServerCode(event.App)
	return event.JSON(http.StatusOK, publicRuntimeConfig{
		AllowAnonymousBootstrap: false,
		OAuthProviders:          providers,
		NotionOAuthConfigured:   false,
		SignupClosed:            signupClosed,
		SignupContact:           signupContact,
		ServerName:              settings.ServerName,
		ServerAlias:             settings.ServerAlias,
		ServerCode:              strings.ToLower(serverCode),
		EmailRecoveryAvailable:  emailRecovery,
		AppHostname:             configuredPublicAppHostname(),
		PublicAddress:           configuredPublicOrigin(),
		TailscaleAddress:        tailscaleAddressServed(event.App),
		EarlierAddresses:        earlierServerAddresses(),
		CompanySignIn:           CompanySignInAvailable != nil && CompanySignInAvailable(event.App),
		ServerMoved:             moved != nil,
		MovedTo:                 movedTo,
		ServerPaused:            replicationPaused(event.App),
		StandbyOf:               standbyOf,
		PairAddress:             pairAddress(event.App, backups, backupsErr == nil),
		AppPlatform:             appPlatformAvailable,
		AppWorkers:              appWorkerMode,
		AppPlatformMissing:      appPlatformMissingReason(),
		UnsplashCoverEnabled:    unsplashCoverConfigured(),
		Map:                     publicRuntimeMap(settings, err == nil),
		PlaceSearchEnabled:      placeSearchAvailable(event.App, settings, err == nil),
		SponsorCredits:          publicRuntimeSponsorCredits(event.App),
		Legal: publicRuntimeLegalLinks{
			SourceURL: upstreamRepositoryURL,
		},
	})
}

func configuredPublicAppHostname() string {
	raw := strings.TrimSpace(os.Getenv("LEAFEN_PUBLIC_URL"))
	parsed, err := url.Parse(raw)
	if err != nil || raw == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// CompanySignInAvailable says whether an organization signs in with SAML by
// a verified domain, for the sign-in screen's company sign-in; the
// organization module sets it, and without it there is none.
var CompanySignInAvailable func(app core.App) bool
