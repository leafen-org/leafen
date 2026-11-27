package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A replacement's blocks are named as an append's are
// (workspaceMarkdownOwnedBlocks).
const (
	workspaceMarkdownReplaceBlockPrefix       = "workspace-import-replace-"
	workspaceMarkdownReplaceLegacyBlockPrefix = "workspace-import-replace:"
)

func workspaceMarkdownReplaceBlocks(jobID string) workspaceMarkdownOwnedBlocks {
	return workspaceMarkdownOwnedBlocks{
		prefix: workspaceMarkdownReplacePrefix(jobID),
		legacy: workspaceMarkdownReplaceLegacyBlockPrefix + jobID + ":",
	}
}

type workspaceMarkdownReplaceLease struct {
	JobID        string `db:"job_id"`
	PageID       string `db:"page_id"`
	Owner        string `db:"owner"`
	WorkspaceID  string `db:"workspace_id"`
	PageRevision int    `db:"page_revision"`
	PageCursor   int    `db:"page_cursor"`
}

type workspaceMarkdownReplaceSnapshot struct {
	BlockID  string `db:"block_id"`
	ParentID string `db:"parent_id"`
	Position string `db:"position"`
	Type     string `db:"type"`
	Content  string `db:"content"`
	Text     string `db:"text"`
	State    string `db:"state"`
}

func (r *durableJobRunner) createWorkspaceMarkdownReplace(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	authorization, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) || (err == nil && authorization.Workspace.WorkspaceID != workspaceID) {
		return workspaceImportPageRefused(event, user.Id, pageID, err, "authorize-replace")
	}
	if err == nil {
		err = workspaceImportTargetGone(event.App, pageID)
	}
	if err != nil {
		return workspaceImportAPIError(event, err, "authorize-replace")
	}
	input := workspaceMarkdownReplaceInput{}
	if err := event.BindBody(&input); err != nil {
		return workspaceImportBodyRefused(event, "Markdown replace request", err)
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	if problem := firstNonempty(durableRequestIDProblem(input.RequestID), workspaceImportTextProblem("markdown", input.Markdown)); problem != "" {
		return workspaceImportRefused(event, problem)
	}
	return r.admitWorkspaceImport(event, user.Id, workspaceID, workspaceImportAdmission{
		Input: workspaceImportInput{
			RequestID: input.RequestID, Format: workspaceImportFormatMarkdown,
			Title: authorization.Page.GetString("title"), Markdown: input.Markdown,
		},
		TargetPageID:   pageID,
		Replace:        true,
		TargetRevision: authorization.Page.GetInt("revision"),
		TargetCursor:   authorization.Page.GetInt("cursor"),
	})
}

func workspaceMarkdownReplaceRequestHash(workspaceID, pageID, markdown string) string {
	digest := sha256.Sum256([]byte(strings.Join(
		[]string{"markdown-replace-v1", workspaceID, pageID, markdown}, "\x00",
	)))
	return hex.EncodeToString(digest[:])
}

func workspaceMarkdownReplacePrefix(jobID string) string {
	return workspaceMarkdownReplaceBlockPrefix + jobID + "-"
}

func workspaceMarkdownReplacePageLeased(app core.App, pageID, allowedJobID string) (bool, error) {
	var jobID string
	err := app.DB().NewQuery(`
		SELECT [[job_id]] FROM {{leafen_workspace_import_replace_leases}}
		WHERE [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).Row(&jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return jobID != allowedJobID, nil
}

func insertWorkspaceMarkdownReplaceLease(
	app core.App,
	jobID string,
	authorization pageAuthorization,
	created int64,
) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_import_replace_leases}}
			([[job_id]], [[page_id]], [[owner]], [[workspace_id]], [[page_revision]], [[page_cursor]], [[created]])
		VALUES
			({:jobId}, {:pageId}, {:owner}, {:workspaceId}, {:revision}, {:cursor}, {:created})
	`).Bind(dbx.Params{
		"jobId": jobID, "pageId": authorization.Page.Id,
		"owner": authorization.Workspace.OwnerID, "workspaceId": authorization.Workspace.WorkspaceID,
		"revision": authorization.Page.GetInt("revision"), "cursor": authorization.Page.GetInt("cursor"),
		"created": created,
	}).Execute()
	return err
}

// authorizeWorkspaceMarkdownReplace admits a replacement's turn while its
// page takes changes; its cleanup puts the page back even in the trash
// (workspace_import_target_gone.go).
func authorizeWorkspaceMarkdownReplace(
	app core.App,
	job storedJob,
) (pageAuthorization, workspaceMarkdownReplaceLease, error) {
	authorization, lease, err := authorizeWorkspaceMarkdownReplaceAs(app, job, job.Owner)
	if lease.JobID != "" {
		if gone := workspaceImportTargetGone(app, job.PageID); gone != nil {
			return pageAuthorization{}, lease, gone
		}
	}
	return authorization, lease, err
}

func authorizeWorkspaceMarkdownReplaceAs(
	app core.App,
	job storedJob,
	principalID string,
) (pageAuthorization, workspaceMarkdownReplaceLease, error) {
	lease := workspaceMarkdownReplaceLease{}
	if err := app.DB().NewQuery(`
		SELECT [[job_id]], [[page_id]], [[owner]], [[workspace_id]], [[page_revision]], [[page_cursor]]
		FROM {{leafen_workspace_import_replace_leases}} WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": job.ID}).One(&lease); err != nil {
		return pageAuthorization{}, lease, err
	}
	if lease.PageID != job.PageID || lease.WorkspaceID != job.WorkspaceID {
		return pageAuthorization{}, lease, workspaceImportStateErrorAt("replace-lease-identity")
	}
	authorization, err := authorizePage(app, principalID, job.PageID, permissionWorkspaceWrite)
	if err != nil || authorization.Workspace.WorkspaceID != job.WorkspaceID ||
		authorization.Workspace.OwnerID != lease.Owner ||
		authorization.Page.GetInt("revision") != lease.PageRevision ||
		authorization.Page.GetInt("cursor") != lease.PageCursor {
		if err != nil {
			return pageAuthorization{}, lease, err
		}
		return pageAuthorization{}, lease, workspaceImportTargetNotFreshError(nil, "replace-target-fence")
	}
	return authorization, lease, nil
}

func authorizeWorkspaceMarkdownReplaceCleanup(
	app core.App,
	job storedJob,
) (pageAuthorization, workspaceMarkdownReplaceLease, error) {
	lease := workspaceMarkdownReplaceLease{}
	if err := app.DB().NewQuery(`
		SELECT [[job_id]], [[page_id]], [[owner]], [[workspace_id]], [[page_revision]], [[page_cursor]]
		FROM {{leafen_workspace_import_replace_leases}} WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": job.ID}).One(&lease); err != nil {
		return pageAuthorization{}, lease, err
	}
	return authorizeWorkspaceMarkdownReplaceAs(app, job, lease.Owner)
}

func workspaceMarkdownReplaceAuthorization(job storedJob) func(core.App) (pageAuthorization, error) {
	return func(txApp core.App) (pageAuthorization, error) {
		authorization, _, err := authorizeWorkspaceMarkdownReplace(txApp, job)
		return authorization, err
	}
}

func workspaceMarkdownReplaceCleanupAuthorization(job storedJob) func(core.App) (pageAuthorization, error) {
	return func(txApp core.App) (pageAuthorization, error) {
		authorization, _, err := authorizeWorkspaceMarkdownReplaceCleanup(txApp, job)
		return authorization, err
	}
}

func workspaceMarkdownReplaceMutationHooks(
	job storedJob,
	operations []graphOperation,
) (contentGraphMutationHooks, error) {
	canonical, err := json.Marshal(operations)
	if err != nil {
		return contentGraphMutationHooks{}, err
	}
	digest := sha256.Sum256(canonical)
	return contentGraphMutationHooks{
		Authorize:                   workspaceMarkdownReplaceAuthorization(job),
		WorkspaceImportReplaceJobID: job.ID,
		AllowTrashedPage:            true,
		ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
			return hex.EncodeToString(digest[:]), nil
		},
	}, nil
}

func (r *durableJobRunner) snapshotWorkspaceMarkdownReplace(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	if !payload.SourceVerified {
		source, err := loadStoredWorkspaceImportSource(r.app, job.ID)
		if err != nil {
			return workspaceImportStateErrorAt("replace-source")
		}
		if err := verifyWorkspaceImportSource(r.app.DataDir(), source); err != nil {
			return err
		}
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || !currentPayload.ReplaceExistingPage || currentPayload.Phase != workspaceImportPhaseStaged ||
			currentPayload.ReplaceSnapshotCursor != payload.ReplaceSnapshotCursor {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("replace-snapshot-frontier")
		}
		if currentPayload.CancelRequested {
			currentPayload.Phase = workspaceImportPhaseCleanup
			return r.updateWorkspaceImportPayload(
				txApp, current, currentPayload, current.Cursor, current.Processed, false,
			)
		}
		authorization, _, err := authorizeWorkspaceMarkdownReplace(txApp, current)
		if err != nil {
			return err
		}
		rows := []workspaceMarkdownReplaceSnapshot{}
		err = txApp.DB().NewQuery(`
			SELECT [[id]] AS [[block_id]], [[parent_id]], [[position]], [[type]], [[content]], '' AS [[text]],
				'snapshot' AS [[state]]
			FROM {{leafen_blocks}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] > {:cursor}
			ORDER BY [[id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"owner": authorization.Workspace.OwnerID, "pageId": current.PageID,
			"cursor": currentPayload.ReplaceSnapshotCursor, "limit": workspaceImportMutationTurnItems,
		}).All(&rows)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			currentPayload.SourceVerified = true
			currentPayload.Phase = workspaceImportPhaseApply
			return r.updateWorkspaceImportPayload(
				txApp, current, currentPayload, current.Cursor, current.Processed, true,
			)
		}
		documents := map[string]*notionYgoDocument{}
		document, err := loadNotionYgoDocument(txApp, current.PageID, documents)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		for _, row := range rows {
			row.Text = document.doc.GetText(ygoBlockTextRootPrefix + row.BlockID).ToString()
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_workspace_import_replace_snapshots}}
					([[job_id]], [[block_id]], [[owner]], [[workspace_id]], [[page_id]], [[parent_id]],
					 [[position]], [[type]], [[content]], [[text]], [[state]], [[created]])
				VALUES
					({:jobId}, {:blockId}, {:owner}, {:workspaceId}, {:pageId}, {:parentId},
					 {:position}, {:type}, {:content}, {:text}, 'snapshot', {:created})
				ON CONFLICT ([[job_id]], [[block_id]]) DO NOTHING
			`).Bind(dbx.Params{
				"jobId": current.ID, "blockId": row.BlockID, "owner": authorization.Workspace.OwnerID,
				"workspaceId": current.WorkspaceID, "pageId": current.PageID, "parentId": row.ParentID,
				"position": row.Position, "type": row.Type, "content": row.Content,
				"text": row.Text, "created": now,
			}).Execute(); err != nil {
				return err
			}
		}
		currentPayload.SourceVerified = true
		currentPayload.ReplaceSnapshotCursor = rows[len(rows)-1].BlockID
		currentPayload.ReplaceSnapshotItems += len(rows)
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, current.Cursor, current.Processed, true,
		)
	})
}
