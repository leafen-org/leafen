package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// An app's schedules and data events (docs/app-platform.md 11) wait in the
// same queue as its hooks' calls, each marked with its kind: a change in the
// app's workspace that an `events` entry names, or a schedule's turn. The
// app's own changes don't call it back, and a page changed again while its
// event still waits stays one event.

const (
	appQueueHook     = "hook"
	appQueueEvent    = "event"
	appQueueSchedule = "schedule"
)

// appDataWorkspace is which app a workspace is and whose, as a content change
// asks it once (write_lookup_memo.go).
type appDataWorkspace struct {
	InstallID string `db:"app_install_id"`
	Owner     string `db:"workspace_owner"`
}

type appWorkspaceKey string

// recordAppDataEvent queues a change in an app's workspace for each of the
// app's `events` entries that names its kind.
func recordAppDataEvent(app core.App, input eventWebhookEventInput) {
	workspace, err := writeLookup(app, appWorkspaceKey(input.WorkspaceID), func() (appDataWorkspace, error) {
		workspace := appDataWorkspace{}
		err := app.DB().NewQuery(`SELECT [[app_install_id]], [[workspace_owner]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": input.WorkspaceID}).One(&workspace)
		return workspace, err
	})
	if err != nil || workspace.InstallID == "" || input.ActorID == workspace.Owner {
		return
	}
	install, err := loadAppInstall(app, workspace.InstallID)
	if err != nil || install.Status != "active" || install.RemovedAt != 0 {
		return
	}
	_, manifest, err := loadAppPackage(app, install.PackageHash)
	if err != nil {
		return
	}
	tenancy := appWorkspaceTenancy(app, install.ID, input.WorkspaceID)
	for _, event := range manifest.Events {
		if !appOneOf(input.Type, event.Types) || !appEventRowMatches(app, install.ID, tenancy, event, input) {
			continue
		}
		topic := input.Type + ":" + input.EntityID
		var waiting int
		if app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_hook_events}} WHERE [[install_id]] = {:install} AND [[kind]] = 'event'
			AND [[hook]] = {:key} AND [[topic]] = {:topic} AND [[status]] = 'waiting'`).
			Bind(dbx.Params{"install": install.ID, "key": event.Key, "topic": topic}).Row(&waiting) != nil || waiting > 0 {
			continue
		}
		body, _ := json.Marshal(map[string]any{"type": input.Type, "entityId": input.EntityID, "entityType": input.EntityType,
			"pageId": input.PageID, "actor": input.ActorID, "workspaceId": input.WorkspaceID, "data": input.Data})
		if _, err := app.DB().Insert(appHookEventsTable, dbx.Params{
			"id": security.RandomString(20), "install_id": install.ID, "hook": event.Key, "delivery": "", "content_type": "application/json",
			"body": body, "status": "waiting", "received_at": time.Now().UnixMilli(), "kind": appQueueEvent, "topic": topic,
		}).Execute(); err != nil {
			app.Logger().Warn("leafen app event not queued", "install", install.ID, "error", err)
			continue
		}
		select {
		case appHookWake <- struct{}{}:
		default:
		}
	}
}
