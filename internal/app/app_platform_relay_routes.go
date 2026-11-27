//go:build !leafen_noapps

package app

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// The relay route and its administration: strict mode for the server, and
// each app's secrets (docs/app-platform.md 7·8).

func registerAppRelayRoutes(event *core.ServeEvent) {
	if event.Server != nil {
		if _, port, err := net.SplitHostPort(event.Server.Addr); err == nil {
			if number, err := strconv.Atoi(port); err == nil {
				appRelayOwnPort.Store(int32(number))
			}
		}
	}
	event.Router.PUT(apiRoot+appPlatformSettingsUpdatePath, updateAppPlatformSettingsRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
	event.Router.GET(apiRoot+appPlatformSecretsListPath, listAppSecretsRoute)
	registerAppSettingRoutes(event)
	registerAppCredentialRoutes(event)
	registerAppPasswordRoutes(event)
	registerAppEndpointRoutes(event)
	registerAppEndpointLimitRoutes(event)
	event.Router.PUT(apiRoot+appPlatformSecretSetPath, setAppSecretRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
	event.Router.DELETE(apiRoot+appPlatformSecretClearPath, clearAppSecretRoute)
	// A base64 body of the largest request, with room for the rest.
	event.Router.POST(apiRoot+appPlatformRelayPath, relayAppRequestRoute).Bind(apis.BodyLimit(appRelayMaxRequestBytes*4/3 + appPlatformInputMaxBytes))
	event.Router.POST(apiRoot+appPlatformOAuthStartPath, startAppOAuthRoute)
	event.Router.GET(apiRoot+appPlatformOAuthCallbackPath, appOAuthCallbackRoute)
	event.Router.POST(apiRoot+appPlatformOAuthReturnPath, setAppOAuthReturnRoute)
	event.Router.GET(apiRoot+appPlatformOAuthAccountsPath, listAppOAuthRoute)
	event.Router.DELETE(apiRoot+appPlatformOAuthDisconnectPath, disconnectAppOAuthRoute)
	event.Router.POST(apiRoot+appPlatformHookReceivePath, receiveAppHookRoute).Bind(apis.BodyLimit(appHookMaxBodyBytes))
	event.Router.GET(apiRoot+appPlatformHooksListPath, listAppHooksRoute)
	event.Router.POST(apiRoot+appPlatformHookSecretPath, makeAppHookSecretRoute)
	event.Router.DELETE(apiRoot+appPlatformHookOrgRemovePath, removeAppHookOrgRoute)
	event.Router.GET(apiRoot+appPlatformJobsListPath, listAppJobsRoute)
	event.Router.PUT(apiRoot+appPlatformScheduleSetPath, setAppScheduleRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
}

func updateAppPlatformSettingsRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	input := appPlatformSettingsWire{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return appPlatformFailure(event, bodyRefusal(errAppInputInvalid, "app platform settings", err))
	}
	// A key's call asks its person only for settings that would be saved
	// (user_key_acting_ask.go).
	if err := rehearseBeforeAsking(event, func(txApp core.App) error {
		return saveAppPlatformSettings(txApp, input, user.Id)
	}); err != nil {
		return appPlatformFailure(event, err)
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	if err := saveAppPlatformSettings(event.App, input, user.Id); err != nil {
		return appPlatformFailure(event, err)
	}
	saved, err := loadAppPlatformSettings(event.App)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, saved)
}

func adminAppInstall(event *core.RequestEvent) (storedAppInstall, appManifest, error) {
	install, err := loadAppInstall(event.App, event.Request.PathValue("installId"))
	if err != nil {
		return install, appManifest{}, err
	}
	_, manifest, err := loadAppPackage(event.App, install.PackageHash)
	return install, manifest, err
}

func listAppSecretsRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	secrets, err := listAppSecrets(event.App, install, manifest)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	response := appSecretListResponse{Secrets: secrets}
	// The address to register with each outside sign-in's provider.
	if len(manifest.Network.OAuth) > 0 {
		if origin, err := outwardServerOrigin(event.Request); err == nil {
			response.OAuthCallback = origin + apiRoot + appOAuthCallbackPath
			response.OAuthReturns = appOAuthReturns(event.App, install, manifest, response.OAuthCallback)
		}
	}
	return event.JSON(http.StatusOK, response)
}

func setAppSecretRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	input := appSecretInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return appPlatformFailure(event, bodyRefusal(errAppInputInvalid, "secret", err))
	}
	if input.Value == "" {
		return appPlatformFailure(event, refuse(errAppInputInvalid, "value is empty: clear the secret instead of setting it empty"))
	}
	if err := setAppSecret(event.App, install, manifest, event.Request.PathValue("name"), input.Value, user.Id); err != nil {
		return appPlatformFailure(event, err)
	}
	return listAppSecretsRoute(event)
}

func clearAppSecretRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	// A key's call asks its person once the app is found (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	if err := setAppSecret(event.App, install, manifest, event.Request.PathValue("name"), "", user.Id); err != nil {
		return appPlatformFailure(event, err)
	}
	return listAppSecretsRoute(event)
}

// appTokenInstall is the running app whose own token calls, with the
// person the token acts for; anything else is answered and ok is false.
func appTokenInstall(event *core.RequestEvent) (storedAppInstall, appManifest, string, bool, error) {
	installID := event.Request.PathValue("installId")
	authority, ok := event.Get(userKeyAuthorityKey).(userKeyAuthority)
	// A route without the install in its path is the calling app's own.
	if installID == "" {
		installID = authority.AppInstallID
	}
	if !ok || authority.AppInstallID == "" || authority.AppInstallID != installID {
		return storedAppInstall{}, appManifest{}, "", false,
			apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "Only the app itself can do this.")
	}
	install, err := loadAppInstall(event.App, installID)
	if err != nil || install.Status != "active" || install.RemovedAt != 0 {
		return install, appManifest{}, "", false, apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "App not found.")
	}
	_, manifest, err := loadAppPackage(event.App, install.PackageHash)
	if err != nil {
		return install, manifest, "", false, appPlatformFailure(event, err)
	}
	return install, manifest, authority.OwnerID, true, nil
}

// relayAppRequestRoute relays for the app whose token calls it, as the
// person the token acts for — or, for an app that reaches none of the
// person's data and so holds no token, for Leafen's own window around its
// frame, with the person's sign-in (appRelayCaller).
func relayAppRequestRoute(event *core.RequestEvent) error {
	install, manifest, personID, tenancy, ok, failure := appRelayCaller(event)
	if !ok {
		return failure
	}
	settings, err := loadAppPlatformSettings(event.App)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	raw, err := io.ReadAll(event.Request.Body)
	input := appRelayInput{}
	if err == nil {
		err = json.Unmarshal(raw, &input)
	}
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppRelayDenied,
			refusalMessage("Send a method, an address, headers and a body.", bodyRefusalReason("relay request", err)))
	}
	answer, err := runAppRelay(event.Request.Context(), appRelayCall{
		App: event.App, Install: install, Manifest: manifest, PersonID: personID, Tenancy: tenancy,
		Strict: settings.StrictNetwork || appTenancyStrict(event.App, tenancy),
		// The app's screen relays for the person using it.
		Credentials: newAppCredentialFiller(event.App, install, manifest, personID),
	}, input)
	if err != nil {
		message := err.Error()
		if index := strings.Index(message, ": "); index >= 0 {
			message = message[index+2:]
		}
		switch {
		case errors.Is(err, errAppRelayDenied):
			return apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, message)
		case errors.Is(err, errAppRelayLimited):
			return apiError(event, http.StatusTooManyRequests, errorCodeAppRelayLimited, "Too many relayed requests; try again in a minute.")
		case errors.Is(err, errAppRelayFailed):
			return apiError(event, http.StatusBadGateway, errorCodeAppRelayFailed, message)
		case errors.Is(err, errDLPPolicyBlocked):
			return apiError(event, http.StatusForbidden, errorCodeDLPPolicyBlocked, "The organization's data rules keep this from leaving.")
		}
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, answer)
}

// appRelayCaller is whom a relay call or an outside sign-in is made for: the
// app's token, or the person's own sign-in for an app without data access
// that they may use in the tenancy its frame opened in (?tenancy=). Such an
// app's frame relays and connects accounts only through Leafen's window,
// which is what it reaches; a person could do no more through it than through
// the frame itself. One rule for both, as the relay fills {{oauth:KEY}} with
// the token of the person it is made for (2026-11-14: an app without
// personAccess could declare network.oauth, but its connectAccount always
// ended in noDataAccess).
func appRelayCaller(event *core.RequestEvent) (storedAppInstall, appManifest, string, string, bool, error) {
	if authority, key := userKeyOf(event); key {
		install, manifest, personID, ok, failure := appTokenInstall(event)
		return install, manifest, personID, authority.TenancyKey, ok, failure
	}
	user, ok := authenticatedUser(event)
	if !ok {
		return storedAppInstall{}, appManifest{}, "", "", false, apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	usable, err := usableApps(event.App, user.Id)
	if err != nil {
		return storedAppInstall{}, appManifest{}, "", "", false, appPlatformFailure(event, err)
	}
	installID, tenancy := event.Request.PathValue("installId"), event.Request.URL.Query().Get("tenancy")
	for _, candidate := range usable {
		if candidate.Install.ID != installID || candidate.TenancyKey != tenancy {
			continue
		}
		if candidate.Manifest.PersonAccess != "" {
			break
		}
		return candidate.Install, candidate.Manifest, user.Id, tenancy, true, nil
	}
	return storedAppInstall{}, appManifest{}, "", "", false,
		apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "Only the app itself can do this.")
}

func startAppOAuthRoute(event *core.RequestEvent) error {
	install, manifest, personID, _, ok, failure := appRelayCaller(event)
	if !ok {
		return failure
	}
	origin, err := publicServerOrigin(event.Request)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	address, err := startAppOAuth(event.App, install, manifest, event.Request.PathValue("provider"), personID, origin)
	if errors.Is(err, errAppOAuthNotReady) {
		return apiError(event, http.StatusConflict, errorCodeAppOAuthNotReady, "The administrator hasn't set this login up yet.")
	}
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, appOAuthStartResponse{AuthorizeURL: address})
}

func listAppOAuthRoute(event *core.RequestEvent) error {
	install, manifest, personID, _, ok, failure := appRelayCaller(event)
	if !ok {
		return failure
	}
	accounts, err := listAppOAuthAccounts(event.App, install, manifest, personID)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, appOAuthAccountList{Accounts: accounts})
}

func disconnectAppOAuthRoute(event *core.RequestEvent) error {
	install, manifest, personID, _, ok, failure := appRelayCaller(event)
	if !ok {
		return failure
	}
	if err := disconnectAppOAuth(event.App, install, manifest, event.Request.PathValue("provider"), personID); err != nil {
		return appPlatformFailure(event, err)
	}
	return listAppOAuthRoute(event)
}

// appOAuthCallbackCallWords are what a call to the address an app's
// sign-in comes back to hears.
const appOAuthCallbackCallWords = "Check state: no login for an app Leafen started is waiting with it. " +
	"This is where the provider sends the person's browser back after they log in; the app starts one " +
	"(POST /apps/{installId}/oauth/{provider}/start, with its own token) and gives the authorizeUrl they open."

// appOAuthCallbackRoute is where a provider sends the person back: the
// sign-in completes and the page says so (signInCallbackPage).
func appOAuthCallbackRoute(event *core.RequestEvent) error {
	query := event.Request.URL.Query()
	install, provider, err := completeAppOAuth(event.Request.Context(), event.App, query.Get("state"), query.Get("code"))
	if err != nil {
		// The person reads a plain sentence; the administrator finds the cause in the log.
		event.App.Logger().Warn("leafen app sign-in failed", "install", install.ID, "provider", provider, "error", err)
	}
	return signInCallbackPage{
		Done: [2]string{"Connected.", "연결했습니다."},
		Failed: [2]string{"Couldn’t connect: the login expired, was refused or failed. Try again from the app.",
			"연결하지 못했습니다. 로그인이 만료됐거나 거부됐거나 실패했습니다. 앱에서 다시 시도하세요."},
		Result:    map[string]any{"type": "leafen:oauth", "install": install.ID, "provider": provider, "ok": err == nil},
		AnyOrigin: true,
		Back:      "/",
		Code:      errorCodeAppOAuthFailed, Call: appOAuthCallbackCallWords,
	}.write(event, err == nil)
}

func listAppHooksRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	// The address an outside sender is given (outwardServerOrigin).
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	hooks, err := listAppHooks(event.App, install, manifest, origin)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, appHookList{Hooks: hooks})
}

func makeAppHookSecretRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	input := appHookSecretInput{}
	if event.Request.ContentLength != 0 {
		if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
			return appPlatformFailure(event, bodyRefusal(errAppInputInvalid, "hook secret request", err))
		}
	}
	made, err := makeAppHookSecret(event.App, install, manifest, event.Request.PathValue("hook"), input.TenancyKey, user.Id, origin)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, made)
}

// receiveAppHookRoute is an app's hook as outside services call it.
func receiveAppHookRoute(event *core.RequestEvent) error {
	body, err := io.ReadAll(io.LimitReader(event.Request.Body, appHookMaxBodyBytes+1))
	if err != nil || len(body) > appHookMaxBodyBytes {
		return apiError(event, http.StatusRequestEntityTooLarge, errorCodeAppHookRefused, "The body is larger than the hook takes.")
	}
	receipt, err := receiveAppHook(event.App, event.Request.PathValue("slug"), event.Request.PathValue("hook"), event.Request, body, time.Now())
	switch {
	case err == nil:
		return event.JSON(http.StatusAccepted, receipt)
	case errors.Is(err, errAppInstallNotFound):
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "No such hook.")
	case errors.Is(err, errAppHookRefused):
		return apiError(event, http.StatusUnauthorized, errorCodeAppHookRefused, "The hook's secret or signature doesn't match.")
	case errors.Is(err, errAppHookStopped):
		return apiError(event, http.StatusServiceUnavailable, errorCodeAppHookStopped, "The app is stopped; try again later.")
	case errors.Is(err, errAppHookLimited):
		return apiError(event, http.StatusTooManyRequests, errorCodeAppHookLimited, "Too many calls to this hook; try again in a minute.")
	}
	return appPlatformFailure(event, err)
}

func removeAppHookOrgRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if err := removeAppHookOrg(event.App, install, manifest, event.Request.PathValue("hook"), event.Request.PathValue("organizationId"), user.Id); err != nil {
		return appPlatformFailure(event, err)
	}
	return listAppHooksRoute(event)
}

func listAppJobsRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	jobs, err := loadAppJobs(event.App, install, manifest)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if jobs.Approvals, jobs.ApprovalsNextCursor, err = loadAppApprovals(event.App, install, manifest, timeIDCursor{}); err != nil {
		return appPlatformFailure(event, err)
	}
	if jobs.Allowances, err = loadAppAllowances(event.App, install, manifest); err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, jobs)
}

func setAppScheduleRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	input := appScheduleInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return appPlatformFailure(event, bodyRefusal(errAppInputInvalid, "schedule", err))
	}
	if err := setAppSchedule(event.App, install, manifest, event.Request.PathValue("schedule"), input, user.Id); err != nil {
		return appPlatformFailure(event, err)
	}
	return listAppJobsRoute(event)
}
