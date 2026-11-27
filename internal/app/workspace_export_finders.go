package app

import (
	"encoding/json"
	"reflect"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person's workspace exports are looked through a window at a time to the
// last, so an export made before many others is still found.
const workspaceExportFindWindow = 20

// keptWorkspaceExports are the newest finished export of each format whose
// file is still kept, besides the latest when it is that format's, newest
// first: a file made before stays to download under a later export of
// another format, or one cancelled or failed (2026-11-07: an HTML export's
// download went once Markdown was exported; Notion's links stay in the inbox
// until they end).
func (r *durableJobRunner) keptWorkspaceExports(app core.App, owner, workspaceID string, latest workspaceExportJob) []workspaceExportJob {
	formats := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[format]] FROM {{leafen_workspace_export_artifacts}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[owner]] = {:owner}
	`).Bind(dbx.Params{"owner": owner, "workspaceId": workspaceID}).Column(&formats); err != nil {
		return []workspaceExportJob{}
	}
	type keptExport struct {
		updated int64
		job     workspaceExportJob
	}
	found := []keptExport{}
	for _, format := range formats {
		if latest.Status == workspaceExportStatusCompleted && latest.Format == format {
			continue
		}
		if job, updated, ok := r.newestKeptWorkspaceExport(app, owner, workspaceID, format, latest.ID); ok {
			found = append(found, keptExport{updated: updated, job: job})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].updated > found[j].updated })
	kept := make([]workspaceExportJob, 0, len(found))
	for _, item := range found {
		kept = append(kept, item.job)
	}
	return kept
}

// newestKeptWorkspaceExport is the newest finished workspace export of a
// format, other than the latest, whose file opens, read newest first a
// window at a time.
func (r *durableJobRunner) newestKeptWorkspaceExport(app core.App, owner, workspaceID, format, latestID string) (workspaceExportJob, int64, bool) {
	after := timeIDCursor{}
	for {
		rows := []struct {
			ID      string `db:"id"`
			Updated int64  `db:"updated"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[job]].[[id]], [[job]].[[updated]] FROM {{leafen_workspace_export_artifacts}} [[artifact]]
			JOIN {{leafen_jobs}} [[job]] ON [[job]].[[id]] = [[artifact]].[[job_id]]
			WHERE [[artifact]].[[workspace_id]] = {:workspaceId} AND [[artifact]].[[owner]] = {:owner} AND [[artifact]].[[format]] = {:format}
				AND [[job]].[[owner]] = {:owner} AND [[job]].[[workspace_id]] = {:workspaceId} AND [[job]].[[kind]] = {:kind}
				AND [[job]].[[page_id]] = '' AND [[job]].[[status]] = 'completed'
				AND ({:afterId} = '' OR [[job]].[[updated]] < {:afterAt} OR ([[job]].[[updated]] = {:afterAt} AND [[job]].[[id]] < {:afterId}))
			ORDER BY [[job]].[[updated]] DESC, [[job]].[[id]] DESC LIMIT {:limit}
		`).Bind(dbx.Params{
			"owner": owner, "workspaceId": workspaceID, "format": format, "kind": workspaceExportJobKind,
			"afterAt": after.At, "afterId": after.ID, "limit": workspaceExportFindWindow,
		}).All(&rows); err != nil {
			return workspaceExportJob{}, 0, false
		}
		for _, row := range rows {
			job, err := r.workspaceExportResponse(row.ID, owner, workspaceID)
			if err != nil || job.ID == latestID || job.Status != workspaceExportStatusCompleted || job.Format != format {
				continue
			}
			_, file, err := openWorkspaceExportArtifact(app, row.ID, owner, workspaceID)
			if err != nil {
				continue
			}
			_ = file.Close()
			return job, row.Updated, true
		}
		if len(rows) < workspaceExportFindWindow {
			return workspaceExportJob{}, 0, false
		}
		last := rows[len(rows)-1]
		after = timeIDCursor{At: last.Updated, ID: last.ID}
	}
}

// runningWorkspaceExport is the actor's export of the same scope, format,
// options and content that is still being made (a job stays queued until
// it ends), looked for through all their queued exports.
func runningWorkspaceExport(
	app core.App, input workspaceExportAdmissionInput, scope workspaceExportScope, linkedView *workspaceExportLinkedView,
) (string, bool, error) {
	after := timeIDCursor{}
	for {
		rows := []struct {
			ID      string `db:"id"`
			Created int64  `db:"created"`
			Payload string `db:"payload_json"`
		}{}
		err := app.DB().NewQuery(`
			SELECT [[id]], [[created]], [[payload_json]] FROM {{leafen_jobs}}
			WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspace} AND [[kind]] = {:kind}
				AND [[status]] = 'queued'
				AND ({:afterId} = '' OR [[created]] < {:afterAt} OR ([[created]] = {:afterAt} AND [[id]] < {:afterId}))
			ORDER BY [[created]] DESC, [[id]] DESC LIMIT {:limit}
		`).Bind(dbx.Params{
			"owner": input.ActorID, "workspace": input.WorkspaceID, "kind": workspaceExportJobKind,
			"afterAt": after.At, "afterId": after.ID, "limit": workspaceExportFindWindow,
		}).All(&rows)
		if err != nil {
			return "", false, err
		}
		for _, row := range rows {
			payload := workspaceExportJobPayload{}
			if json.Unmarshal([]byte(row.Payload), &payload) != nil || payload.NotionAdmin != nil {
				continue
			}
			if payload.Format == input.Format &&
				reflect.DeepEqual(payload.Scope, scope) && reflect.DeepEqual(payload.Options, input.Options) &&
				reflect.DeepEqual(payload.LinkedView, linkedView) {
				return row.ID, true, nil
			}
		}
		if len(rows) < workspaceExportFindWindow {
			return "", false, nil
		}
		last := rows[len(rows)-1]
		after = timeIDCursor{At: last.Created, ID: last.ID}
	}
}
