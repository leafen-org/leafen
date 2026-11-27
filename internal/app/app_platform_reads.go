//go:build !leafen_noapps

package app

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What an app read of a person's data (docs/app-platform.md 15): each read
// an app makes as the person — its screen's token, an action or schedule run
// as them, an agent using its tools for them — is counted by day and by what
// it read, and the person sees the list from the app's own bar. The app's
// own account reading its app workspace isn't the person's data. Rows go
// with the app or the person, and after the organization's audit retention
// when it chose one (kept where it chose none).

const (
	appDataReadsShownDays  = 30
	appDataReadsShownItems = 200
)

type appDataReadKey struct {
	installID, personID, tenancy string
	day                          int64
	kind, target                 string
}

// appDataReadCount is a day's reads of one thing and when the last was.
type appDataReadCount struct{ reads, last int64 }

var appDataReads = struct {
	sync.Mutex
	pending map[appDataReadKey]appDataReadCount
}{pending: map[appDataReadKey]appDataReadCount{}}

func init() {
	appReadRecorder = recordAppDataRead
}

func appDataReadKind(place userKeyPlace) string {
	switch place {
	case userKeyPlaceWorkspaceParam, userKeyPlaceBodyWorkspace:
		return "workspace"
	case userKeyPlacePageParam, userKeyPlaceBodyPage:
		return "page"
	case userKeyPlaceRowParam:
		return "row"
	case userKeyPlaceContainerParam:
		return "database"
	case userKeyPlaceFileParam:
		return "file"
	case userKeyPlaceJobParam:
		return "job"
	case userKeyPlaceOrganizationParam:
		return "organization"
	}
	return "account"
}

func recordAppDataRead(authority userKeyAuthority, place userKeyPlace, target string, now time.Time) {
	kind := appDataReadKind(place)
	if kind == "account" || place == userKeyPlaceBodyWorkspace || place == userKeyPlaceBodyPage {
		target = ""
	}
	key := appDataReadKey{installID: authority.AppInstallID, personID: authority.OwnerID, tenancy: authority.TenancyKey,
		day: now.UTC().Truncate(24 * time.Hour).UnixMilli(), kind: kind, target: target}
	appDataReads.Lock()
	count := appDataReads.pending[key]
	appDataReads.pending[key] = appDataReadCount{reads: count.reads + 1, last: max(count.last, now.UnixMilli())}
	appDataReads.Unlock()
}

// saveAppDataReads writes the reads counted since the last save.
func saveAppDataReads(app core.App) error {
	appDataReads.Lock()
	pending := appDataReads.pending
	appDataReads.pending = map[appDataReadKey]appDataReadCount{}
	appDataReads.Unlock()
	for key, count := range pending {
		if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_app_data_reads}}
			([[install_id]], [[person_id]], [[tenancy_key]], [[day]], [[kind]], [[target_id]], [[reads]], [[last_at]])
			SELECT {:install}, {:person}, {:tenancy}, {:day}, {:kind}, {:target}, {:reads}, {:last}
			WHERE EXISTS (SELECT 1 FROM {{leafen_app_installs}} WHERE [[id]] = {:install})
				AND EXISTS (SELECT 1 FROM {{users}} WHERE [[id]] = {:person})
			ON CONFLICT ([[install_id]], [[person_id]], [[day]], [[kind]], [[target_id]]) DO UPDATE SET [[reads]] = [[reads]] + excluded.[[reads]],
				[[last_at]] = MAX([[last_at]], excluded.[[last_at]])`).
			Bind(dbx.Params{"install": key.installID, "person": key.personID, "tenancy": key.tenancy, "day": key.day, "kind": key.kind,
				"target": key.target, "reads": count.reads, "last": count.last}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// pruneAppDataReads keeps reads as long as each organization keeps its
// audit log: all of them where it chose no period, so with no period chosen
// anywhere the reads aren't looked at. Each read finds its organization's
// period by the organization's key, read outside the write lock
// (retire_rows_window.go).
func pruneAppDataReads(app core.App, now time.Time) error {
	var periods bool
	if err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_organization_audit_retention_policies}})`).Row(&periods); err != nil || !periods {
		return err
	}
	return retireAllBoundRows(app, appDataReadsTable, appDataReadPastPeriodSQL, dbx.Params{"now": now.UnixMilli()})
}

// appDataReadPastPeriodSQL holds for the aliased read when its organization
// chose a period and the read's day is past it.
func appDataReadPastPeriodSQL(alias string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_organization_audit_retention_policies}} AS [[policy]]
		WHERE [[policy]].[[organization_id]] = substr(` + alias + `.[[tenancy_key]], 5) AND substr(` + alias + `.[[tenancy_key]], 1, 4) = 'org:'
			AND ` + alias + `.[[day]] < {:now} - 86400000 * [[policy]].[[retention_days]])`
}

func registerAppDataReadsRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPlatformDataReadsPath, appDataReadsRoute)
}

// appDataReadsRoute shows the person — signed in or through their key,
// never an app's token — what an app read of theirs in the last 30 days,
// newest first, naming what they can still read.
func appDataReadsRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	if appTokenCall(event) {
		return apiError(event, http.StatusForbidden, errorCodeUserKeyClosed, "An app can't see what apps read.")
	}
	if err := saveAppDataReads(event.App); err != nil {
		return appPlatformFailure(event, err)
	}
	since := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -appDataReadsShownDays+1)
	rows := []struct {
		Kind    string `db:"kind"`
		Target  string `db:"target_id"`
		Reads   int64  `db:"reads"`
		Days    int    `db:"days"`
		LastDay int64  `db:"last_day"`
		LastAt  int64  `db:"last_at"`
	}{}
	if err := event.App.DB().NewQuery(`SELECT [[kind]], [[target_id]], SUM([[reads]]) AS [[reads]], COUNT(*) AS [[days]], MAX([[day]]) AS [[last_day]],
			MAX([[last_at]]) AS [[last_at]]
		FROM {{leafen_app_data_reads}} WHERE [[install_id]] = {:install} AND [[person_id]] = {:person} AND [[day]] >= {:since}
		GROUP BY [[kind]], [[target_id]] ORDER BY [[last_day]] DESC, [[reads]] DESC LIMIT {:limit}`).
		Bind(dbx.Params{"install": event.Request.PathValue("installId"), "person": user.Id, "since": since.UnixMilli(),
			"limit": appDataReadsShownItems}).All(&rows); err != nil {
		return appPlatformFailure(event, err)
	}
	answer := appDataReadsWire{Since: since.UnixMilli(), Items: []appDataReadWire{}}
	for _, row := range rows {
		answer.Total += row.Reads
		answer.Items = append(answer.Items, appDataReadWire{Kind: row.Kind, TargetID: row.Target, Title: appDataReadTitle(event.App, user.Id, row.Kind, row.Target),
			Reads: row.Reads, Days: row.Days, LastDay: row.LastDay, LastAt: row.LastAt})
	}
	return event.JSON(http.StatusOK, answer)
}

// appDataReadTitle names a page or workspace the person can still read;
// anything else goes by its kind alone.
func appDataReadTitle(app core.App, personID, kind, target string) string {
	switch kind {
	case "page":
		if read, err := authorizePage(app, personID, target, permissionWorkspaceRead); err == nil && read.Page != nil {
			return strings.TrimSpace(read.Page.GetString("title"))
		}
	case "workspace":
		// Its owner reads it as its members do: the workspace's own reading
		// rule says who may, not its membership rows, which hold members only.
		if read, err := authorizeWorkspace(app, personID, target, permissionWorkspaceRead); err == nil && read.Workspace != nil {
			return strings.TrimSpace(read.Workspace.GetString("name"))
		}
	}
	return ""
}
