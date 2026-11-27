package app

import (
	"context"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's file cleanup deletes the uploads that were never finished.
// A file no page uses waits before it goes — a day after it was uploaded
// when no page ever took it, five minutes after a page let it go — and the
// server deletes it on its own when its wait is over; the cleanup leaves it
// (the data is kept on purpose). A cleanup done says so: how many such
// files the workspace still keeps, when the last of them goes and which
// they are, the way to delete one now named in the operation's note (seen
// 2026-11-26: a cleanup asked for a file just uploaded and on no page
// answered "completed", 0 scanned, and the agent told its person the
// workspace held no unused file).

// fileManualCleanupKeptShown bounds the kept files a cleanup names.
const fileManualCleanupKeptShown = 25

// productFileManualCleanupAnswer is a cleanup as answered: done, with the
// files no page uses its workspace still keeps.
func productFileManualCleanupAnswer(
	ctx context.Context,
	app core.App,
	job storedProductFileManualCleanupJob,
	duplicate bool,
) (fileManualCleanupResponse, error) {
	response, err := productFileManualCleanupResponse(job, duplicate)
	if err != nil {
		return response, err
	}
	response.Job.KeptFileIDs = []string{}
	if job.Status == fileManualCleanupStatusRunning {
		return response, nil
	}
	kept := struct {
		Files int   `db:"files"`
		Until int64 `db:"until"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) AS [[files]], COALESCE(MAX([[cleanup_due_at]]), 0) AS [[until]]
		FROM {{leafen_file_reference_cleanups}} WHERE [[workspace_id]]={:workspaceId}
	`).WithContext(ctx).Bind(dbx.Params{"workspaceId": job.WorkspaceID}).One(&kept); err != nil {
		return response, err
	}
	if kept.Files == 0 {
		return response, nil
	}
	ids := []struct {
		FileID string `db:"file_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[file_id]] FROM {{leafen_file_reference_cleanups}} WHERE [[workspace_id]]={:workspaceId}
		ORDER BY [[cleanup_due_at]], [[file_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"workspaceId": job.WorkspaceID, "limit": fileManualCleanupKeptShown}).All(&ids); err != nil {
		return response, err
	}
	response.Job.KeptFiles, response.Job.KeptUntil = kept.Files, kept.Until
	for _, id := range ids {
		response.Job.KeptFileIDs = append(response.Job.KeptFileIDs, id.FileID)
	}
	return response, nil
}

func init() {
	const kept = "A file no page uses isn't deleted here: it waits a day after it was uploaded when no page took it, five minutes " +
		"after a page let it go, and the server deletes it on its own then. A cleanup done says how many the workspace still keeps " +
		"(job.keptFiles), when the last goes (job.keptUntil, milliseconds) and up to 25 of them (job.keptFileIds); " +
		"DELETE /files/{id} with {\"requestId\"} deletes one of them now."
	for id, note := range map[string]string{
		"POST /workspaces/{workspaceId}/file-cleanup":        "Deletes the workspace's uploads that were never finished (job.expired). " + kept,
		"GET /workspaces/{workspaceId}/file-cleanup/{jobId}": kept,
	} {
		shape := nativeOperationShape(id)
		if held := nativeOperationNotes[shape]; held != "" {
			note = held + " " + note
		}
		nativeOperationNotes[shape] = note
	}
}
