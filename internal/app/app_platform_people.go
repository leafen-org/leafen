//go:build !leafen_noapps

package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The people an app may name (docs/app-platform.md 3): its screen and its
// workers choose whom a task goes to — an approver, an assignee — among
// the organization's people who may use the app there, by id and name, a
// window at a time by name with nextCursor while more follow: an answer
// that stopped at a hundred and said nothing of more left everyone past
// them out of an approvals app's approvers (2026-11-24). Leafen gives
// nothing else of them: no email, no picture, no one the app isn't granted
// to. A screen also learns which of them it acts for.

const appPeopleMax = 100

func readAppPeopleRoute(event *core.RequestEvent) error {
	authority, ok := event.Get(userKeyAuthorityKey).(userKeyAuthority)
	if !ok || authority.AppInstallID == "" || authority.TenancyKey == "" {
		return apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "Only an app reads the people it may name.")
	}
	install, err := loadAppInstall(event.App, authority.AppInstallID)
	if err != nil || install.Status != "active" || install.RemovedAt != 0 {
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "App not found.")
	}
	query := strings.ToLower(strings.TrimSpace(event.Request.URL.Query().Get("q")))
	limit := appPeopleMax
	if raw := event.Request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > appPeopleMax {
			return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, "Ask for 1 to 100 people.")
		}
		limit = parsed
	}
	// ?ids= asks after particular people, at most a page of them.
	var asked []string
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("ids")); raw != "" {
		asked = strings.Split(raw, ",")
		if len(asked) > appPeopleMax {
			return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, "Ask for 1 to 100 people.")
		}
	}
	// ?email= asks whose address this is, naming only them.
	if email := strings.ToLower(strings.TrimSpace(event.Request.URL.Query().Get("email"))); email != "" {
		if asked, err = appPeopleWithEmail(event.App, email); err != nil {
			return appPlatformFailure(event, err)
		}
	}
	// ?cursor= goes on past where an answer's nextCursor says it stopped.
	var after *storedOrganizationPerson
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("cursor")); raw != "" {
		if after = appPeopleCursorStart(raw); after == nil {
			return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, "Start the list again.")
		}
	}
	// ?groups= keeps those in any of the groups, for an app that reads them.
	var inGroups map[string]bool
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("groups")); raw != "" {
		groups := strings.Split(raw, ",")
		if len(groups) > appGrantsMaxGroups {
			return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, "Ask after up to 100 groups.")
		}
		if !appReadsGroups(event.App, install) {
			return apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "The app's manifest doesn't read groups.")
		}
		inGroups = map[string]bool{}
		for _, group := range groups {
			inGroups[appGrantGroupPrefix+strings.TrimSpace(group)] = true
		}
	}
	answer := appPeopleWire{People: []appPersonWire{}}
	// The person the app's token acts for; an app's own account is no one.
	if !authority.AppAccount {
		answer.You = authority.OwnerID
	}
	organizationID, inOrganization := strings.CutPrefix(authority.TenancyKey, "org:")
	if !inOrganization || utf8.RuneCountInString(query) > 100 {
		return event.JSON(http.StatusOK, answer)
	}
	// The organization's people by name, apps' own accounts left out in the
	// read (they are people to Leafen, not to the app), a window at a time
	// until the answer is full: four times as many read and then those not
	// granted left out, an app granted to a few named none past them
	// (fillWindows, 2026-11-23). One more found past a full answer says more
	// follow, from the last one in it.
	var lastNamed storedOrganizationPerson
	err = eachWindow(limit*4,
		func(size int, last *storedOrganizationPerson) ([]storedOrganizationPerson, error) {
			if last == nil {
				last = after
			}
			return loadOrganizationPeopleWindow(event.App, organizationID, query, asked, true, last, size)
		},
		func(people []storedOrganizationPerson) (bool, error) {
			for _, person := range people {
				granted, err := appGranted(event.App, install.ID, authority.TenancyKey, person.UserID)
				if err != nil {
					return false, err
				}
				// With ?groups= each comes with which of them they are in.
				var in []string
				if granted && inGroups != nil {
					subjects, err := appPersonGroupSubjects(event.App, authority.TenancyKey, person.UserID)
					if err != nil {
						return false, err
					}
					for _, subject := range subjects {
						if inGroups[subject] {
							in = append(in, strings.TrimPrefix(subject, appGrantGroupPrefix))
						}
					}
					granted = len(in) > 0
				}
				if !granted {
					continue
				}
				if len(answer.People) == limit {
					answer.NextCursor = appPeopleCursor(lastNamed)
					return true, nil
				}
				answer.People = append(answer.People, appPersonWire{ID: person.UserID, Name: person.DisplayName, Groups: in})
				lastNamed = person
			}
			return false, nil
		})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, answer)
}

// appPeopleCursor is where a window of an app's people goes on from: the
// last one's sort keys and id (storedOrganizationPerson), as JSON so no
// name can break them apart.
func appPeopleCursor(last storedOrganizationPerson) string {
	encoded, _ := json.Marshal([]string{last.SortName, last.SortEmail, last.UserID})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// appPeopleCursorStart reads appPeopleCursor back, nil for one it didn't make.
func appPeopleCursorStart(cursor string) *storedOrganizationPerson {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	keys := []string{}
	if err != nil || json.Unmarshal(decoded, &keys) != nil || len(keys) != 3 || keys[2] == "" {
		return nil
	}
	return &storedOrganizationPerson{SortName: keys[0], SortEmail: keys[1], UserID: keys[2]}
}

// appReadsGroups says whether the app's installed manifest reads groups.
func appReadsGroups(app core.App, install storedAppInstall) bool {
	_, manifest, err := loadAppPackage(app, install.PackageHash)
	return err == nil && manifest.Groups
}

// readAppGroupsRoute lists the organization's groups — made by hand or kept
// by SCIM — for an app whose manifest reads them: by id and name, never their
// people (GET /app-people?groups= says who of the app's people is in them),
// a window at a time by name from ?cursor=, with nextCursor while more follow.
func readAppGroupsRoute(event *core.RequestEvent) error {
	authority, ok := event.Get(userKeyAuthorityKey).(userKeyAuthority)
	if !ok || authority.AppInstallID == "" || authority.TenancyKey == "" {
		return apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "Only an app reads its organization's groups.")
	}
	install, err := loadAppInstall(event.App, authority.AppInstallID)
	if err != nil || install.Status != "active" || install.RemovedAt != 0 {
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "App not found.")
	}
	if !appReadsGroups(event.App, install) {
		return apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "The app's manifest doesn't read groups.")
	}
	groups, next, err := appGrantGroupWindow(event.App, authority.TenancyKey, event.Request.URL.Query().Get("cursor"))
	if errors.Is(err, errAppGroupCursorInvalid) {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, "Start the list again.")
	}
	if err != nil {
		return appPlatformFailure(event, err)
	}
	answer := appGroupsWire{Groups: []appGroupNameWire{}, NextCursor: next}
	for _, group := range groups {
		answer.Groups = append(answer.Groups, appGroupNameWire{ID: group.ID, Name: group.Name, ManagedBy: group.ManagedBy})
	}
	return event.JSON(http.StatusOK, answer)
}

// readAppWorkspacesRoute lists the workspaces the call reaches in its
// organization — a person's, for their app token or a run for them; the
// app's own for its account — naming the app's own workspace and other
// apps' as such, so an app can read a person's tasks or inbox in each.
func readAppWorkspacesRoute(event *core.RequestEvent) error {
	authority, ok := event.Get(userKeyAuthorityKey).(userKeyAuthority)
	if !ok || authority.AppInstallID == "" || authority.TenancyKey == "" {
		return apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "Only an app reads the workspaces it reaches.")
	}
	var appWorkspace string
	_ = event.App.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_app_workspaces}} WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy}`).
		Bind(dbx.Params{"install": authority.AppInstallID, "tenancy": authority.TenancyKey}).Row(&appWorkspace)
	answer := appReachWire{Workspaces: []appReachWorkspaceWire{}}
	for _, id := range authority.WorkspaceIDs {
		var name string
		if err := event.App.DB().NewQuery(`SELECT [[name]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id} AND [[deletion_job_id]] = ''`).
			Bind(dbx.Params{"id": id}).Row(&name); err != nil {
			continue
		}
		// Other apps' workspaces hold their data, not the person's work.
		var apps int
		_ = event.App.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_workspaces}} WHERE [[workspace_id]] = {:id}`).
			Bind(dbx.Params{"id": id}).Row(&apps)
		answer.Workspaces = append(answer.Workspaces, appReachWorkspaceWire{ID: id, Name: name, App: id == appWorkspace,
			OtherApp: id != appWorkspace && apps > 0})
	}
	return event.JSON(http.StatusOK, answer)
}

// appPeopleWithEmail is who signs in with the address, proven or not, and an
// id nobody has when nobody does.
func appPeopleWithEmail(app core.App, email string) ([]string, error) {
	ids := []string{}
	err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_account_emails}} WHERE lower([[email]]) = {:email}
		UNION SELECT [[id]] FROM {{users}} WHERE lower([[email]]) = {:email}`).Bind(dbx.Params{"email": email}).Column(&ids)
	if err == nil && len(ids) == 0 {
		ids = []string{""}
	}
	return ids, err
}
