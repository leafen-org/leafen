//go:build !leafen_noapps

package app

import (
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Why a person can't use an app here, so its page and its blocks say the one
// reason that holds and what to do — an administrator turns it on or gives
// it to people, a member asks — rather than "not installed, stopped, or not
// usable here" for everyone (2026-10-16). Apps on the server are no secret
// to its people: their catalog lists them.

func registerAppAvailabilityRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appAvailabilityPath, appAvailabilityRoute)
}

// appAvailabilityRoute answers for ?slug= (an app's page) or ?app= (a
// block's app), in ?tenancy= when given.
func appAvailabilityRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	query := event.Request.URL.Query()
	slug, appID, tenancy := query.Get("slug"), query.Get("app"), query.Get("tenancy")
	if slug == "" && appID == "" {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, "Name the app by its slug or its id.")
	}
	answer, err := appAvailabilityFor(event.App, user.Id, slug, appID, tenancy)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, answer)
}

func appAvailabilityFor(app core.App, userID, slug, appID, tenancy string) (appAvailabilityWire, error) {
	answer := appAvailabilityWire{State: "notInstalled"}
	settings, err := loadAppPlatformSettings(app)
	if err != nil {
		return answer, err
	}
	answer.InstallPolicy = settings.InstallPolicy
	if answer.CanManage, err = isAdministrator(app, userID); err != nil {
		return answer, err
	}
	installs, err := loadAppInstalls(app, "[[removed_at]] = 0 AND ([[slug]] = {:slug} OR [[app_id]] = {:app})", dbx.Params{"slug": slug, "app": appID})
	if err != nil {
		return answer, err
	}
	if len(installs) == 0 {
		// An app in the trash is still the server's until it is purged: its
		// places say so and that it can be restored, not that it was never
		// installed (2026-11-15: the address said to find and install it).
		trashed, err := loadAppInstalls(app, "[[removed_at]] > 0 AND ([[slug]] = {:slug} OR [[app_id]] = {:app})", dbx.Params{"slug": slug, "app": appID})
		if err != nil || len(trashed) == 0 {
			return answer, err
		}
		latest := trashed[0]
		for _, candidate := range trashed[1:] {
			if candidate.RemovedAt > latest.RemovedAt {
				latest = candidate
			}
		}
		answer.State, answer.Slug, answer.PurgeAt = "trashed", latest.Slug, appPurgeAt(latest.RemovedAt)
		answer.Name = appInstallName(app, latest)
		return answer, nil
	}
	install := installs[0]
	answer.Slug = install.Slug
	answer.Name = appInstallName(app, install)
	if install.Status == "revoked" {
		answer.State = "revoked"
		return answer, nil
	}
	if install.Status != "active" {
		answer.State = "stopped"
		return answer, nil
	}
	tenancies, err := appTenancies(app)
	if err != nil {
		return answer, err
	}
	answer.State = "notGranted"
	for _, candidate := range tenancies {
		workspaces, err := appPersonOrdinaryWorkspaces(app, userID, candidate.Key)
		if err != nil {
			return answer, err
		}
		if len(workspaces) == 0 {
			continue
		}
		granted, err := appGranted(app, install.ID, candidate.Key, userID)
		if err != nil {
			return answer, err
		}
		if !granted {
			continue
		}
		if tenancy == "" || candidate.Key == tenancy {
			answer.State = "usable"
			return answer, nil
		}
		answer.State = "otherOrganization"
	}
	// Someone the app isn't given to may add it themselves where the policy
	// lets people add allowed apps and this is one (2026-11-06: the app's
	// address only said to ask an administrator, while the apps list offered
	// "추가").
	if answer.State == "notGranted" && !answer.CanManage && settings.InstallPolicy == "allowlist" {
		allowed, err := appAllowlisted(app)
		if err != nil {
			return answer, err
		}
		answer.CanAdd = allowed[install.AppID] != ""
	}
	return answer, nil
}

// appInstallName is the name an installed app's package gives it, if it can be read.
func appInstallName(app core.App, install storedAppInstall) map[string]string {
	if _, manifest, err := loadAppPackage(app, install.PackageHash); err == nil {
		return appTextOrEmpty(manifest.Name)
	}
	return nil
}
