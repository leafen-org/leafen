package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An app's data that couldn't be made in a workspace, or brought up to the
// installed version after an update, or made as a person's own, is kept for
// its administrators in one place: the reason goes into the app's audit, and
// the app's details show it while a workspace is still failed or behind, or
// a person's own data still failed. The next opening of the app there, or the
// next time the app is reached for that person, tries again
// (docs/app-platform.md 6).

// appDataFailureKind is what an app's data failed at.
type appDataFailureKind int

const (
	// An organization's data made the first time.
	appDataFailureSetup appDataFailureKind = iota
	// An organization's data caught up with an update.
	appDataFailureUpdate
	// A person's own data (app_platform_person_data.go).
	appDataFailurePerson
)

// noteAppDataFailure keeps why an app's data couldn't be made, caught up
// with an update or made for a person, once for each reason however often
// people open the app, and says when it couldn't keep it. A person's is
// kept with the first person it failed for.
func noteAppDataFailure(app core.App, install storedAppInstall, failure error, kind appDataFailureKind, actorID string) error {
	if last, _ := appDataFailureReason(app, install.ID, kind); last == failure.Error() {
		return nil
	}
	detail := map[string]any{"error": failure.Error()}
	switch kind {
	case appDataFailureUpdate:
		detail["update"] = true
	case appDataFailurePerson:
		detail["person"] = true
	}
	return recordAppAudit(app, install, appAuditDataSetupFailed, actorID, detail)
}

// appDataFailureReason is why the app's data of a kind couldn't be made
// last, if ever.
func appDataFailureReason(app core.App, installID string, kind appDataFailureKind) (string, error) {
	detail, update, person := "", 0, 0
	switch kind {
	case appDataFailureUpdate:
		update = 1
	case appDataFailurePerson:
		person = 1
	}
	err := app.DB().NewQuery(`SELECT [[detail_json]] FROM {{leafen_app_audit_events}}
		WHERE [[install_id]] = {:install} AND [[action]] = {:action}
			AND COALESCE(json_extract([[detail_json]], '$.update'), 0) = {:update}
			AND COALESCE(json_extract([[detail_json]], '$.person'), 0) = {:person}
		ORDER BY [[id]] DESC LIMIT 1`).
		Bind(dbx.Params{"install": installID, "action": appAuditDataSetupFailed, "update": update, "person": person}).Row(&detail)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	reason := struct {
		Error string `json:"error"`
	}{}
	return reason.Error, json.Unmarshal([]byte(detail), &reason)
}

// noteAppPersonDataOutcome keeps whether a person's own data could be made:
// why not on their place with the app in the organization (its connection)
// while it couldn't, cleared once it could, and the reason in the app's
// audit as an organization's is.
func noteAppPersonDataOutcome(app core.App, install storedAppInstall, tenancyKey, userID string, failure error) error {
	reason := ""
	if failure != nil {
		reason = failure.Error()
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_app_connections}} SET [[person_data_failure]] = {:reason}
		WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy} AND [[user_id]] = {:user}
			AND [[person_data_failure]] != {:reason}`).
		Bind(dbx.Params{"reason": reason, "install": install.ID, "tenancy": tenancyKey, "user": userID}).Execute(); err != nil {
		return err
	}
	if failure == nil {
		return nil
	}
	return noteAppDataFailure(app, install, failure, appDataFailurePerson, userID)
}

// showAppDataFailures puts what the app's details say of its data in them:
// an organization's (appDataFailureShown) and how many people's own data
// still failed, with the reason most of them failed for.
func showAppDataFailures(app core.App, install storedAppInstall, wire *appInstallWire) {
	wire.DataFailure, wire.DataUpdateFailed = appDataFailureShown(app, install)
	// Only people whose way into the app is still open count.
	failures := []struct {
		Reason string `db:"reason"`
		People int    `db:"people"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[link]].[[person_data_failure]] AS [[reason]], COUNT(*) AS [[people]]
		FROM {{leafen_app_connections}} [[link]]
		JOIN {{leafen_mcp_connections}} [[connection]] ON [[connection]].[[id]] = [[link]].[[connection_id]] AND [[connection]].[[revoked_at]] = 0
		WHERE [[link]].[[install_id]] = {:install} AND [[link]].[[person_data_failure]] != ''
		GROUP BY [[link]].[[person_data_failure]] ORDER BY [[people]] DESC, [[reason]]`).
		Bind(dbx.Params{"install": install.ID}).All(&failures); err != nil || len(failures) == 0 {
		return
	}
	wire.PersonDataFailure = failures[0].Reason
	for _, failure := range failures {
		wire.PersonDataFailures += failure.People
	}
}

// appDataFailureShown is what the app's details say of its organizations'
// data: why a workspace's first making failed, else why one made from an
// earlier version couldn't catch up with the installed one (update) — only
// while such a workspace is left.
func appDataFailureShown(app core.App, install storedAppInstall) (reason string, update bool) {
	left := struct {
		Failed int `db:"failed"`
		Behind int `db:"behind"`
	}{}
	if err := app.DB().NewQuery(`SELECT
			COALESCE(SUM([[setup_state]] = {:failed}), 0) AS [[failed]],
			COALESCE(SUM([[setup_state]] = {:done} AND [[data_package]] != {:package}), 0) AS [[behind]]
		FROM {{leafen_app_workspaces}} WHERE [[install_id]] = {:install}`).
		Bind(dbx.Params{"install": install.ID, "failed": appDataSetupFailed, "done": appDataSetupDone, "package": install.PackageHash}).
		One(&left); err != nil {
		return "", false
	}
	if left.Failed > 0 {
		reason, _ = appDataFailureReason(app, install.ID, appDataFailureSetup)
		return reason, false
	}
	if left.Behind > 0 {
		reason, _ = appDataFailureReason(app, install.ID, appDataFailureUpdate)
		return reason, reason != ""
	}
	return "", false
}
