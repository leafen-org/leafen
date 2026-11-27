package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notices about apps (docs/app-platform.md 5·9·15): an agent's call waiting
// for the person's approval, a version a catalog revoked, an app stopped for
// overrunning its budget. They travel as Leafen's own system notices —
// inbox, mail and push alike — with an activity key starting "app-" and
// "app:<install>" where a page would be named, and they show while the app
// is installed rather than while a page is readable.

const appNoticePagePrefix = "app:"

// appNoticeVisible reports whether a person still sees notices about an
// install; a build without the app platform shows none.
var appNoticeVisible = func(app core.App, userID, installID string) (bool, error) { return false, nil }

// systemNoticeSubjectVisible reports, for a notice about an app, an outside
// service or a workspace rather than a page (handled), whether the person
// still sees it: while the app is installed for them, while they keep the
// service, or while they are in the named workspace.
func systemNoticeSubjectVisible(app core.App, userID, pageID string) (handled, visible bool, err error) {
	if installID, isApp := appNoticeInstall(pageID); isApp {
		visible, err = appNoticeVisible(app, userID, installID)
		return true, visible, err
	}
	if serviceID, isService := strings.CutPrefix(pageID, mcpServiceNoticePagePrefix); isService {
		visible, err = mcpServiceNoticeVisible(app, userID, serviceID)
		return true, visible, err
	}
	// A teamspace's join request or its approval (teamspace_join_request_notices.go).
	if teamspaceID, isTeamspace := strings.CutPrefix(pageID, teamspaceNoticePagePrefix); isTeamspace {
		visible, err = teamspaceNoticeVisible(app, userID, teamspaceID)
		return true, visible, err
	}
	// Being added to a workspace (workspace_member_added_notices.go).
	if workspaceID, isWorkspace := strings.CutPrefix(pageID, workspaceNoticePagePrefix); isWorkspace {
		visible, err = workspaceNoticeVisible(app, userID, workspaceID)
		return true, visible, err
	}
	// A share of theirs lowered or taken away (page_share_permission_notices.go).
	if pagePermissionNoticeShown(pageID) {
		return true, true, nil
	}
	// A notice about the person's own agents (server_tunnel_first_run.go).
	if strings.HasPrefix(pageID, agentNoticePagePrefix) {
		return true, true, nil
	}
	return false, false, nil
}

// appNoticeInstall is the install a notice's page slot names, when it is
// about an app.
func appNoticeInstall(pageID string) (string, bool) {
	return strings.CutPrefix(pageID, appNoticePagePrefix)
}

// personNoticeWorkspace is where a person's notice about an app lands.
func personNoticeWorkspace(app core.App, userID, organizationID string) (string, error) {
	var workspaceID string
	err := app.DB().NewQuery(`SELECT [[workspace]].[[id]] FROM {{leafen_workspaces}} [[workspace]]
		WHERE [[workspace]].[[app_install_id]] = '' AND [[workspace]].[[deletion_job_id]] = ''
			AND ({:organization} = '' OR [[workspace]].[[organization]] = {:organization})
			AND ([[workspace]].[[workspace_owner]] = {:user} OR EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[membership]]
				WHERE [[membership]].[[workspace_id]] = [[workspace]].[[id]] AND [[membership]].[[user_id]] = {:user}))
		ORDER BY [[workspace]].[[created]], [[workspace]].[[id]] LIMIT 1`).
		Bind(dbx.Params{"user": userID, "organization": organizationID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return workspaceID, err
}

func appNoticeID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:16])
}
