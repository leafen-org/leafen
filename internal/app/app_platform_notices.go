//go:build !leafen_noapps

package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Notices about apps (notification_app_notices.go): the person an agent's
// call waits on, and the server's administrators when a catalog revokes a
// version or an app is stopped for overrunning its budget. A notice lands
// in the person's first workspace in the app's organization (any of theirs
// for a server-wide one), never in an app's own workspace.

const appNoticeAdministratorTarget = "/admin/settings?section=apps"

func init() {
	appNoticeVisible = appInstallNoticeVisible
}

// appInstallNoticeVisible shows notices about an app while it is installed
// and not removed, and notices about a request for an app to the person who
// asked and the administrators while the request is kept.
func appInstallNoticeVisible(app core.App, userID, subject string) (bool, error) {
	var count int
	if requestID, isRequest := strings.CutPrefix(subject, appNoticeRequestPrefix); isRequest {
		err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_requests}} WHERE [[id]] = {:id}
			AND ([[person_id]] = {:user} OR EXISTS (SELECT 1 FROM {{leafen_administrators}} WHERE [[user_id]] = {:user}))`).
			Bind(dbx.Params{"id": requestID, "user": userID}).Row(&count)
		return count == 1, err
	}
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_installs}} WHERE [[id]] = {:id} AND [[removed_at]] = 0`).
		Bind(dbx.Params{"id": subject}).Row(&count)
	return count == 1, err
}

// appNoticeRequestPrefix marks a notice about a request for an app, which
// may not be installed yet.
const appNoticeRequestPrefix = "request:"

// notifyAppPeople puts a notice about an app in each person's inbox, and so
// in their mail and push as their settings say, naming the app (and what
// the preview names) in the person's language where the app has it.
func notifyAppPeople(app core.App, install storedAppInstall, people []string, organizationID, activityKey string, preview map[string]string, target string) error {
	_, manifest, err := loadAppPackage(app, install.PackageHash)
	if err != nil {
		return err
	}
	return notifyAppPeopleAbout(app, install.ID, manifest.Name, people, organizationID, activityKey, preview, target)
}

// notifyAppPeopleAbout puts the notice under subject — an install, or
// "request:<id>" for a request — naming the app by names.
func notifyAppPeopleAbout(app core.App, subject string, names map[string]string, people []string, organizationID, activityKey string,
	preview map[string]string, target string) error {
	return notifyAppPeopleFrom(app, "", subject, names, people, organizationID, activityKey, preview, target)
}

// notifyAppPeopleFrom is notifyAppPeopleAbout for a notice someone's own act
// sent, such as a request to install an app: it carries them as its actor.
func notifyAppPeopleFrom(app core.App, actorID, subject string, names map[string]string, people []string, organizationID, activityKey string,
	preview map[string]string, target string) error {
	now := time.Now().UnixMilli()
	projections := make([]productNotificationProjection, 0, len(people))
	for _, person := range people {
		workspaceID, err := personNoticeWorkspace(app, person, organizationID)
		if err != nil {
			return err
		}
		if workspaceID == "" {
			continue
		}
		language, err := serverTextLanguage(app, person, nil)
		if err != nil {
			return err
		}
		projections = append(projections, productNotificationProjection{
			ID: appNoticeID("app-notice", activityKey, person), EventID: appNoticeID("app-notice-event", activityKey),
			WorkspaceID: workspaceID, UserID: person, ActivityKey: activityKey, Kind: "system",
			PageID: appNoticePagePrefix + subject, ActorID: actorID, Title: appOneLine(appLocalText(names, language), 120),
			Preview: appOneLine(appLocalText(preview, language), 300),
			Target:  target, OccurredAt: now, Personal: true,
		})
	}
	_, err := upsertProductNotifications(context.Background(), app, projections, now)
	return err
}

// notifyAppAdministrators tells every administrator of the server, who
// manage apps in Settings → Apps.
func notifyAppAdministrators(app core.App, install storedAppInstall, activityKey string, preview map[string]string) error {
	administrators, err := serverAdministrators(app)
	if err != nil {
		return err
	}
	return notifyAppPeople(app, install, administrators, "", activityKey, preview, appNoticeAdministratorTarget)
}

// appLocalText is an app's text in the language given, else English, else
// the first language it has.
func appLocalText(text map[string]string, language string) string {
	if value := text[language]; value != "" {
		return value
	}
	return appDisplayName(appManifest{Name: text})
}

// appNoticeRequest is what a worker's leafen_notify sends: a line of text for
// a person who may use the app, and the place in the app it opens.
type appNoticeRequest struct {
	Person  string `json:"person"`
	Text    string `json:"text"`
	Path    string `json:"path,omitempty"`
	Tenancy string `json:"tenancy,omitempty"`
}

const (
	appNoticeMaxRunes    = 200
	appNoticeMaxPathLen  = 300
	appNoticesPerHour    = 10
	appNoticeHourMillis  = int64(time.Hour / time.Millisecond)
	appSentNoticePrefix  = "app-notice:"
	appNoticeLimitReason = "at most 10 notices an hour to one person"
)

// sendAppNotice delivers an app's notice to a person who may use it in the
// run's organization — plain text on one line, opening the app's place —
// at most ten an hour to one person.
func sendAppNotice(app core.App, install storedAppInstall, manifest appManifest, tenancy string, request appNoticeRequest, now time.Time) error {
	if !manifest.Notifications {
		return errors.New("the app's manifest doesn't declare notifications")
	}
	if tenancy == "" {
		return errors.New("leafen_notify needs the organization when the run has none")
	}
	text := appOneLine(strings.Join(strings.Fields(request.Text), " "), appNoticeMaxRunes)
	if text == "" {
		return errors.New("a notice needs its text")
	}
	place := strings.TrimLeft(request.Path, "/")
	if len(place) > appNoticeMaxPathLen || strings.Contains(place, "..") || strings.ContainsAny(place, "\\\x00\r\n\t") || strings.HasPrefix(request.Path, "//") {
		return errors.New("a notice's path is a place within the app, up to 300 characters")
	}
	granted, err := appGranted(app, install.ID, tenancy, request.Person)
	if err != nil {
		return err
	}
	workspaces, err := appPersonWorkspaces(app, request.Person, tenancy)
	if err != nil {
		return err
	}
	if request.Person == "" || !granted || len(workspaces) == 0 {
		return errors.New("that person can't use the app here")
	}
	// The notices sent land in a workspace the person owns or belongs to
	// (personNoticeWorkspace); counting there reads only their last hour.
	var recent int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_notifications}}
		WHERE [[workspace_id]] IN (SELECT [[id]] FROM {{leafen_workspaces}} WHERE [[workspace_owner]] = {:person}
			UNION SELECT [[workspace_id]] FROM {{leafen_workspace_memberships}} WHERE [[user_id]] = {:person})
		AND [[user_id]] = {:person} AND [[activity_key]] LIKE {:prefix} AND [[occurred_at]] > {:since}`).Bind(dbx.Params{
		"person": request.Person, "prefix": appSentNoticePrefix + install.ID + ":%", "since": now.UnixMilli() - appNoticeHourMillis,
	}).Row(&recent); err != nil {
		return err
	}
	if recent >= appNoticesPerHour {
		return errors.New(appNoticeLimitReason)
	}
	activity := appSentNoticePrefix + install.ID + ":" + security.RandomString(16)
	return notifyAppPeople(app, install, []string{request.Person}, strings.TrimPrefix(tenancy, "org:"), activity,
		map[string]string{"en": text}, "/apps/"+install.Slug+"/"+place)
}

// warnAppNotice keeps a notice that couldn't be sent from failing what it
// tells of.
func warnAppNotice(app core.App, install storedAppInstall, activityKey string, err error) {
	if err != nil {
		app.Logger().Warn("leafen app notice failed", "install", install.ID, "notice", activityKey, "error", err)
	}
}
