package app

import (
	"log/slog"

	"github.com/pocketbase/dbx"
)

// An export whose page — the page it exports, or the one holding the
// database it exports — an operation holds for a while (page_access_hold.go)
// waits and goes on once that is done, rather than failing as a permission
// its person lacks (2026-11-23: a page export was asked "Workspace not
// found." while its page's blocks were being moved): its turn is tried again
// every durableJobStoreRetryDelay, its phase, attempts and error kept, as an
// import waits for its page (workspace_import_held.go). Asked for while the
// page is held, it is answered held (workspace_export.go), and a page it only
// names — a sub-page, a mention, a folder above — keeps its name.

// waitWorkspaceExportHeld has an export whose turn met its page held try that
// turn again a little later, and reports whether it did.
func (r *durableJobRunner) waitWorkspaceExportHeld(job storedJob, reference string, applyErr error) (bool, error) {
	busy := pageAccessHeld(applyErr)
	if job.Kind != workspaceExportJobKind || busy == "" {
		return false, nil
	}
	logDurableJobScheduledAgain(
		r.app, slog.LevelDebug, errorCodePageBusy, reference, job.ID,
		"workspace-export.apply", "held-wait", busy, applyErr,
	)
	now := r.clock.now().UnixMilli()
	notBefore := now + durableJobStoreRetryDelay.Milliseconds()
	_, err := r.app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[not_before]] = {:notBefore}, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued' AND [[not_before]] < {:notBefore}
	`).Bind(dbx.Params{"id": job.ID, "notBefore": notBefore, "updated": now}).Execute()
	return true, err
}
