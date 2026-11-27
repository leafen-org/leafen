package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Blocks an append or a replacement makes are named after its job and their
// place, "workspace-import-append-<job>-<n>": the form a block link names a
// block by (#block-<id>, workspaceImportReferenceIDPattern), so that links,
// mentions and exports name them. Jobs admitted before named them with
// colons ("workspace-import-append:<job>:<n>"), which their cleanup still
// reads and their blocks keep.
const (
	workspaceMarkdownAppendBlockPrefix       = "workspace-import-append-"
	workspaceMarkdownAppendLegacyBlockPrefix = "workspace-import-append:"
)

// workspaceMarkdownOwnedBlocks is the blocks one job makes on its page: those
// named by its prefix, or by the colon prefix of a job admitted before.
type workspaceMarkdownOwnedBlocks struct{ prefix, legacy string }

func workspaceMarkdownAppendBlocks(jobID string) workspaceMarkdownOwnedBlocks {
	return workspaceMarkdownOwnedBlocks{
		prefix: workspaceMarkdownAppendPrefix(jobID),
		legacy: workspaceMarkdownAppendLegacyBlockPrefix + jobID + ":",
	}
}

// sql is true for the column naming one of the blocks; its params come from
// bind. A prefix's names sort before the colon prefix's.
func (owned workspaceMarkdownOwnedBlocks) sql(column string) string {
	return `((` + column + ` >= {:ownedLower} AND ` + column + ` < {:ownedUpper})
		OR (` + column + ` >= {:legacyLower} AND ` + column + ` < {:legacyUpper}))`
}

func (owned workspaceMarkdownOwnedBlocks) bind(params dbx.Params) dbx.Params {
	params["ownedLower"], params["ownedUpper"] = owned.prefix, owned.prefix+"~"
	params["legacyLower"], params["legacyUpper"] = owned.legacy, owned.legacy+"~"
	return params
}

// place is a block's place after its job's prefix; "" for a block not one
// of them.
func (owned workspaceMarkdownOwnedBlocks) place(id string) string {
	if place, found := strings.CutPrefix(id, owned.prefix); found {
		return place
	}
	place, _ := strings.CutPrefix(id, owned.legacy)
	return place
}

func (owned workspaceMarkdownOwnedBlocks) owns(id string) bool {
	return strings.HasPrefix(id, owned.prefix) || strings.HasPrefix(id, owned.legacy)
}

func (r *durableJobRunner) createWorkspaceMarkdownAppend(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	// Markdown adds to a page's body, as a database page's body takes it,
	// while the page takes changes (workspace_import_target_gone.go).
	authorization, err := admitWorkspaceImportTarget(event.App, user.Id, workspaceID, pageID)
	if isPermissionDenied(err) {
		return workspaceImportPageRefused(event, user.Id, pageID, err, "authorize-append")
	}
	if err != nil {
		return workspaceImportAPIError(event, err, "authorize-append")
	}
	input := workspaceMarkdownAppendInput{}
	if err := event.BindBody(&input); err != nil {
		return workspaceImportBodyRefused(event, "Markdown append request", err)
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
		TargetPageID: pageID,
		Append:       true,
	})
}

func workspaceMarkdownAppendRequestHash(workspaceID, pageID, markdown string) string {
	digest := sha256.Sum256([]byte(strings.Join(
		[]string{"markdown-append-v1", workspaceID, pageID, markdown}, "\x00",
	)))
	return hex.EncodeToString(digest[:])
}

func workspaceMarkdownAppendPrefix(jobID string) string {
	return workspaceMarkdownAppendBlockPrefix + jobID + "-"
}

func (r *durableJobRunner) prepareWorkspaceMarkdownAppend(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	if !payload.SourceVerified {
		source, err := loadStoredWorkspaceImportSource(r.app, job.ID)
		if err != nil {
			return workspaceImportStateErrorAt("append-source")
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
		payload, err = decodeWorkspaceImportPayload(current)
		if err != nil || !payload.AppendToExistingPage {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("append-prepare")
		}
		if payload.CancelRequested {
			payload.Phase = workspaceImportPhaseCleanup
			return r.updateWorkspaceImportPayload(
				txApp, current, payload, current.Cursor, current.Processed, false,
			)
		}
		if _, err := authorizeWorkspaceImportTarget(
			txApp, current.Owner, current.WorkspaceID, workspaceImportPrimaryRootPageID(payload),
		); err != nil {
			return err
		}
		payload.SourceVerified = true
		payload.Phase = workspaceImportPhaseApply
		return r.updateWorkspaceImportPayload(
			txApp, current, payload, current.Cursor, current.Processed, true,
		)
	})
}

type workspaceMarkdownAppendBlock struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
}

func (r *durableJobRunner) cleanupWorkspaceMarkdownAppend(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	if payload.MergeDataSourceID != "" {
		return r.cleanupWorkspaceCSVMerge(job, payload)
	}
	owned := workspaceMarkdownAppendBlocks(job.ID)
	page, err := r.app.FindRecordById(pagesCollection, job.PageID)
	if errors.Is(err, sql.ErrNoRows) {
		return r.finishWorkspaceMarkdownAppendCleanup(job, payload)
	}
	if err != nil {
		return err
	}
	rows := []workspaceMarkdownAppendBlock{}
	err = r.app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]] FROM {{leafen_blocks}}
		WHERE [[id]] > {:cursor} AND ` + owned.sql("[[id]]") + `
			AND [[owner]] = {:owner} AND [[page_id]] = {:pageId}
		ORDER BY [[id]] LIMIT {:limit}
	`).Bind(owned.bind(dbx.Params{
		"cursor": payload.AppendCleanupCursor,
		"owner":  page.GetString("owner"), "pageId": job.PageID, "limit": workspaceImportCleanupTurnItems,
	})).All(&rows)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return r.finishWorkspaceMarkdownAppendCleanup(job, payload)
	}
	operations := make([]graphOperation, 0, len(rows))
	for _, row := range rows {
		if row.ParentID == "" || !owned.owns(row.ParentID) {
			operations = append(operations, graphOperation{
				Kind:     graphOperationKindDeleteBlock,
				Deletion: &graphBlockDelete{BlockID: row.ID},
			})
		}
	}
	if len(operations) > 0 {
		canonical, err := json.Marshal(operations)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(canonical)
		_, err = r.ygo.applyContentGraphMutation(job.Owner, job.PageID, graphMutationInput{
			MutationID: fmt.Sprintf("workspace-import-cleanup-%s-%s", job.ID, owned.place(rows[0].ID)),
			Operations: operations,
		}, contentGraphMutationHooks{
			Authorize:        workspaceMarkdownAppendCleanupAuthorization(job),
			AllowTrashedPage: true,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
		})
		if err != nil {
			return err
		}
	}
	nextCursor := rows[len(rows)-1].ID
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || !currentPayload.AppendToExistingPage ||
			currentPayload.AppendCleanupCursor != payload.AppendCleanupCursor {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("append-cleanup-cursor")
		}
		currentPayload.AppendCleanupCursor = nextCursor
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, current.Cursor, current.Processed, true,
		)
	})
}

func workspaceMarkdownAppendCleanupAuthorization(job storedJob) func(core.App) (pageAuthorization, error) {
	return func(txApp core.App) (pageAuthorization, error) {
		page, err := txApp.FindRecordById(pagesCollection, job.PageID)
		if err != nil {
			return pageAuthorization{}, err
		}
		authorization, err := authorizePageBody(
			txApp, page.GetString("owner"), job.PageID, permissionWorkspaceWrite,
		)
		if err != nil || authorization.Workspace.WorkspaceID != job.WorkspaceID ||
			authorization.Workspace.OwnerID != page.GetString("owner") {
			if err == nil {
				err = permissionDenied()
			}
			return pageAuthorization{}, err
		}
		return authorization, nil
	}
}

func (r *durableJobRunner) finishWorkspaceMarkdownAppendCleanup(
	job storedJob,
	payload workspaceImportJobPayload,
) error {
	source, err := loadStoredWorkspaceImportSource(r.app, job.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if err := removeWorkspaceImportSourceFile(r.app.DataDir(), source.StorageKey); err != nil {
			return err
		}
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		payload, err = decodeWorkspaceImportPayload(current)
		if err != nil || !payload.AppendToExistingPage {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("append-cleanup-finish")
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_workspace_import_sources}} WHERE [[job_id]] = {:jobId}
		`).Bind(dbx.Params{"jobId": current.ID}).Execute(); err != nil {
			return err
		}
		status := durableJobStatusCanceled
		payload.Phase = workspaceImportPhaseCancelled
		if payload.TerminalErrorCode != "" {
			status = durableJobStatusFailed
			payload.Phase = workspaceImportPhaseFailed
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = {:status}, [[payload_json]] = {:payload},
				[[error_code]] = {:errorCode}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "owner": current.Owner, "status": status,
			"payload": string(encoded), "errorCode": payload.TerminalErrorCode, "updated": now,
		}).Execute()
		if err != nil {
			return err
		}
		return cleanupWorkspaceImportTerminalHistory(txApp, current.WorkspaceID)
	})
}
