//go:build !leafen_noapps

package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An app's undo of a run is offered for a run that ended done, and run for
// no other (docs/apps.md, "undo"): a run that never started — the app was
// busy, stopped, refused — changed nothing, and one that broke midway did
// no one knows what, while the undo action finds what to take back by the
// run's input, as another run's (2026-11-16: "취소 동작 실행" on a run that
// found the app busy took away the row a later run made with the same
// input). What a run changed in Leafen is still reverted page by page.

// errAppRunNotUndoable refuses an undo of a run that didn't end done, or
// isn't the person's run of an action with that undo.
var errAppRunNotUndoable = errors.New("the run didn't finish, so the app can't undo it")

// runAppWorkerFunction runs one function of one of the app's workers and
// records the run — an undo of one of the person's runs only for a run that
// ended done.
func runAppWorkerFunction(ctx context.Context, app core.App, install storedAppInstall, manifest appManifest, run appRun) ([]byte, string, error) {
	if !appUndoRunAllowed(app, install, manifest, run) {
		return nil, "", errAppRunNotUndoable
	}
	return runAppWorker(ctx, app, install, manifest, run, nil)
}

// appUndoRunAllowed is false for a call of an action that undoes another
// whose input names one of the runs ({run, input}, as what the app did
// sends it) when that run isn't the person's run of this app, of an action
// whose undo it is, that ended done.
func appUndoRunAllowed(app core.App, install storedAppInstall, manifest appManifest, run appRun) bool {
	if run.Trigger != "action" && run.Trigger != "tool" && run.Trigger != appQueueAutomation {
		return true
	}
	undoes := false
	for _, action := range manifest.Actions {
		undoes = undoes || action.Undo == run.Ref
	}
	call := struct {
		Input struct {
			Run   *string         `json:"run"`
			Input json.RawMessage `json:"input"`
		} `json:"input"`
	}{}
	if !undoes || json.Unmarshal(run.Input, &call) != nil || call.Input.Run == nil || call.Input.Input == nil {
		return true
	}
	undone := struct {
		Ref    string `db:"trigger_ref"`
		Status string `db:"status"`
	}{}
	err := app.DB().NewQuery(`SELECT [[trigger_ref]], [[status]] FROM {{leafen_app_runs}} WHERE [[id]] = {:run} AND [[install_id]] = {:install}
		AND [[subject]] = {:person} AND [[trigger]] IN ('action', 'tool', 'automation')`).
		Bind(dbx.Params{"run": *call.Input.Run, "install": install.ID, "person": run.Subject}).One(&undone)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return false
	}
	action, found := appManifestActionByKey(manifest, undone.Ref)
	return found && action.Undo == run.Ref && undone.Status == "ok"
}
