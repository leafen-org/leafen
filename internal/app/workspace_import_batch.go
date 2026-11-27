package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A batch of files, or the documents inside a ZIP, is one import job. The
// browser reads and converts the files and sends the text here; once all of
// it is here the job goes on by itself (workspace_import_batch_runner.go), so
// a reload, a closed tab or a restart doesn't stop it (2026-11-04: a tab
// sending one request a file stopped at 328 of 600 when reloaded).
//
// Nothing is made while the text comes in: the batch's pages have their IDs
// from the start, so links between its documents can name them, and the same
// files chosen again carry on with what isn't here yet.

// workspaceImportBatchUploadIdle is how long a batch whose files stopped
// coming stays open before its text is let go. Chosen again later it takes
// the text again. A batch stays a queued job for this long, which holds off
// deleting its workspace, so it isn't longer.
const workspaceImportBatchUploadIdle = 24 * time.Hour

type storedWorkspaceImportBatchItem struct {
	Position       int    `db:"position"`
	ParentPosition int    `db:"parent_position"`
	Kind           string `db:"kind"`
	Turn           int    `db:"turn"`
	Name           string `db:"name"`
	Title          string `db:"title"`
	PageID         string `db:"page_id"`
	PageReady      bool   `db:"page_ready"`
	State          string `db:"state"`
	Format         string `db:"format"`
	StorageKey     string `db:"storage_key"`
	StorageOffset  int64  `db:"storage_offset"`
	StorageBytes   int64  `db:"storage_bytes"`
	SHA256         string `db:"sha256"`
	ChildJobID     string `db:"child_job_id"`
	ErrorCode      string `db:"error_code"`
	Message        string `db:"message"`
	Details        string `db:"details"`
}

func registerWorkspaceImportBatchRoutes(event *core.ServeEvent, jobs *durableJobRunner) {
	event.Router.POST(apiRoot+workspaceImportBatchCreatePath, jobs.createWorkspaceImportBatch)
	event.Router.POST(apiRoot+workspaceImportBatchItemsDeclarePath, jobs.declareWorkspaceImportBatchItems)
	event.Router.GET(apiRoot+workspaceImportBatchItemsListPath, jobs.listWorkspaceImportBatchItems)
	event.Router.POST(apiRoot+workspaceImportBatchDocumentsPath, jobs.receiveWorkspaceImportBatchDocuments).
		Bind(apis.BodyLimit(workspaceImportRequestBytes))
	event.Router.POST(apiRoot+workspaceImportBatchStartPath, jobs.startWorkspaceImportBatch)
}

func workspaceImportBatchRequestHash(workspaceID string, input workspaceImportBatchInput) string {
	parts := []string{"workspace-import-batch-v1", workspaceID, strconv.Itoa(input.Total), input.ParentPageID, input.TeamspaceID}
	if input.DataSourceID != "" {
		parts = append(parts, input.DataSourceID)
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func workspaceImportBatchRefusal(event *core.RequestEvent, err error, stage string) error {
	if isPermissionDenied(err) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
	}
	return workspaceImportAPIError(event, err, stage)
}

func (r *durableJobRunner) createWorkspaceImportBatch(event *core.RequestEvent) error {
	user, workspaceID, ok, err := authorizeWorkspaceArchiveUpload(event, "batch-authorize")
	if !ok {
		return err
	}
	input := workspaceImportBatchInput{}
	if err := event.BindBody(&input); err != nil {
		return workspaceImportBodyRefused(event, "batch import request", err)
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Title = strings.TrimSpace(input.Title)
	input.ParentPageID = strings.TrimSpace(input.ParentPageID)
	input.TeamspaceID = strings.TrimSpace(input.TeamspaceID)
	input.DataSourceID = strings.TrimSpace(input.DataSourceID)
	totalProblem := ""
	if input.Total < 1 {
		totalProblem = "total is how many items the batch brings, 1 or more"
	}
	if problem := firstNonempty(durableRequestIDProblem(input.RequestID), totalProblem,
		workspaceImportInputProblem(workspaceImportInput{
			Format: workspaceImportFormatMarkdown, Title: input.Title, Locale: input.Locale,
			ParentPageID: input.ParentPageID, TeamspaceID: input.TeamspaceID, DataSourceID: input.DataSourceID,
		})); problem != "" {
		return workspaceImportRefused(event, problem)
	}
	requestHash := workspaceImportBatchRequestHash(workspaceID, input)
	jobID, duplicate, reused := "", false, false
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizeWorkspaceImportAdmission(txApp, user.Id, workspaceID, "", false); err != nil {
			return err
		}
		// A place held for a while takes the batch's files once that is done (workspace_import_held.go).
		if err := authorizeWorkspaceImportPlace(txApp, user.Id, workspaceID, workspaceImportInput{
			ParentPageID: input.ParentPageID, TeamspaceID: input.TeamspaceID, DataSourceID: input.DataSourceID,
		}); err != nil && !workspaceImportHeldWaits(err) {
			return err
		}
		existing, err := loadStoredJobByRequest(txApp, user.Id, input.RequestID)
		if err == nil {
			if existing.Kind != workspaceImportJobKind || existing.WorkspaceID != workspaceID || existing.RequestHash != requestHash {
				reused = true
				return nil
			}
			jobID, duplicate = existing.ID, true
			return r.reopenWorkspaceImportBatch(txApp, existing)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		jobID = security.RandomString(15)
		payload, err := json.Marshal(workspaceImportJobPayload{
			SchemaVersion: 1, Format: workspaceImportFormatFileBatch, Title: input.Title, Locale: input.Locale,
			ParentPageID: input.ParentPageID, TeamspaceID: input.TeamspaceID, DataSourceID: input.DataSourceID,
			FillEmptyPage: input.FillEmptyPage && input.ParentPageID != "",
			Phase:         workspaceImportPhaseBatchUpload,
			PageIDs:       true,
		})
		if err != nil {
			return err
		}
		now := r.clock.now().UTC()
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
				 [[status]], [[error_code]], [[not_before]], [[created]], [[updated]], [[payload_json]],
				 [[cursor]], [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]], [[chunk_count]])
			VALUES
				({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind}, '',
				 'queued', '', {:notBefore}, {:now}, {:now}, {:payload}, '0', 0, {:total}, 0, {:maxAttempts}, 0)
		`).Bind(dbx.Params{
			"id": jobID, "owner": user.Id, "workspaceId": workspaceID, "requestId": input.RequestID,
			"requestHash": requestHash, "kind": workspaceImportJobKind, "payload": string(payload),
			"total": input.Total, "maxAttempts": defaultDurableJobMaxAttempts,
			"notBefore": now.Add(workspaceImportBatchUploadIdle).UnixMilli(), "now": now.UnixMilli(),
		}).Execute()
		return err
	})
	if reused {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportRequestIDReused, "This request ID was used for a different import.")
	}
	if err != nil {
		return workspaceImportBatchRefusal(event, err, "batch-create")
	}
	r.notify()
	return r.respondWorkspaceImportBatch(event, jobID, user.Id, workspaceID, duplicate)
}

// reopenWorkspaceImportBatch takes a batch that ended before all its files
// were in for the same files chosen again: what came in stays, and the rest
// comes again and goes in as if for the first time. One that went on, or one
// with all its files in, stays as it is.
func (r *durableJobRunner) reopenWorkspaceImportBatch(txApp core.App, job storedJob) error {
	payload, err := decodeWorkspaceImportPayload(job)
	if err != nil {
		return err
	}
	if payload.Format != workspaceImportFormatFileBatch {
		return workspaceImportStateErrorAt("batch-request")
	}
	now := r.clock.now().UnixMilli()
	if job.Status == durableJobStatusQueued {
		if payload.Phase != workspaceImportPhaseBatchUpload {
			return nil
		}
		return r.keepWorkspaceImportBatchOpen(txApp, job.ID, now)
	}
	if job.Status == durableJobStatusCompleted && job.Processed >= job.Total {
		return nil
	}
	if _, err := txApp.DB().NewQuery(`
		UPDATE {{leafen_workspace_import_batch_items}}
		SET [[state]] = 'pending', [[format]] = '', [[storage_key]] = '', [[storage_offset]] = 0, [[storage_bytes]] = 0,
			[[sha256]] = '', [[child_job_id]] = '', [[error_code]] = '', [[message]] = '', [[details]] = ''
		WHERE [[job_id]] = {:jobId} AND [[state]] NOT IN ('imported', 'skipped')
	`).Bind(dbx.Params{"jobId": job.ID}).Execute(); err != nil {
		return err
	}
	payload.Phase = workspaceImportPhaseBatchUpload
	payload.CancelRequested = false
	payload.TerminalErrorCode, payload.TerminalErrorDetail = "", ""
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = txApp.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]] = 'queued', [[error_code]] = '', [[diagnostic_reference]] = '', [[payload_json]] = {:payload},
			[[processed_items]] = (SELECT COUNT(*) FROM {{leafen_workspace_import_batch_items}}
				WHERE [[job_id]] = {:id} AND [[state]] = 'imported' AND [[kind]] <> 'folder'),
			[[not_before]] = {:notBefore}, [[updated]] = {:now}
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{
		"id": job.ID, "payload": string(encoded), "now": now,
		"notBefore": now + workspaceImportBatchUploadIdle.Milliseconds(),
	}).Execute()
	return err
}

func (r *durableJobRunner) keepWorkspaceImportBatchOpen(txApp core.App, jobID string, now int64) error {
	_, err := txApp.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[not_before]] = {:notBefore}, [[updated]] = {:now}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'
	`).Bind(dbx.Params{"id": jobID, "now": now, "notBefore": now + workspaceImportBatchUploadIdle.Milliseconds()}).Execute()
	return err
}

func (r *durableJobRunner) respondWorkspaceImportBatch(event *core.RequestEvent, jobID, ownerID, workspaceID string, duplicate bool) error {
	response, err := r.workspaceImportResponse(jobID, ownerID, workspaceID, duplicate)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeJobNotFound, "Import not found.")
	}
	if err != nil {
		return workspaceImportAPIError(event, err, "batch-respond")
	}
	return event.JSON(http.StatusOK, response)
}

// loadWorkspaceImportBatch is a person's batch in a workspace.
func loadWorkspaceImportBatch(app core.App, jobID, ownerID, workspaceID string) (storedJob, workspaceImportJobPayload, error) {
	job, err := loadStoredJob(app, jobID, ownerID)
	if err != nil {
		return storedJob{}, workspaceImportJobPayload{}, err
	}
	if !ownedJobMatches(job, workspaceImportJobKind, workspaceID) {
		return storedJob{}, workspaceImportJobPayload{}, sql.ErrNoRows
	}
	payload, err := decodeWorkspaceImportPayload(job)
	if err != nil {
		return storedJob{}, workspaceImportJobPayload{}, err
	}
	if payload.Format != workspaceImportFormatFileBatch {
		return storedJob{}, workspaceImportJobPayload{}, sql.ErrNoRows
	}
	return job, payload, nil
}

func workspaceImportBatchMissing(event *core.RequestEvent, err error, stage string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeJobNotFound, "Import not found.")
	}
	return workspaceImportAPIError(event, err, stage)
}

func validWorkspaceImportBatchItemInput(item workspaceImportBatchItemInput) bool {
	switch item.Kind {
	case workspaceImportBatchItemKindFolder, workspaceImportBatchItemKindDocument, workspaceImportBatchItemKindDatabase,
		workspaceImportBatchItemKindFile, workspaceImportBatchItemKindSkipped:
	default:
		return false
	}
	// Only a ZIP's attachment no document uses, skipped as a page, brings a file.
	if item.FileID != "" && (item.Kind != workspaceImportBatchItemKindSkipped || !workspaceImportReferenceIDPattern.MatchString(item.FileID)) {
		return false
	}
	return item.Position >= 0 && item.ParentPosition >= -1 && item.ParentPosition < item.Position &&
		strings.TrimSpace(item.Title) != "" && utf8.RuneCountInString(item.Title) <= maxGraphTitleRunes &&
		utf8.RuneCountInString(item.Name) <= workspaceImportBatchNameRunes
}

// declareWorkspaceImportBatchItems says what the batch brings, and answers
// with each item's page ID and how far it got: an item already said stays as
// it is.
func (r *durableJobRunner) declareWorkspaceImportBatchItems(event *core.RequestEvent) error {
	user, workspaceID, ok, err := authorizeWorkspaceArchiveUpload(event, "batch-items-authorize")
	if !ok {
		return err
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	input := workspaceImportBatchItemsInput{}
	if err := event.BindBody(&input); err != nil {
		return workspaceImportBodyRefused(event, "batch items request", err)
	}
	if problem := workspaceImportListProblem("items", len(input.Items), workspaceImportBatchItemsPerRequest); problem != "" {
		return workspaceImportRefused(event, problem)
	}
	first, last := input.Items[0].Position, input.Items[0].Position
	for index, item := range input.Items {
		if !validWorkspaceImportBatchItemInput(item) {
			return workspaceImportRefused(event, fmt.Sprintf(
				"items[%d] is a folder, document, database, file or skipped item with a title, a position of 0 or more and a parentPosition before it (-1 for none)", index))
		}
		first, last = min(first, item.Position), max(last, item.Position)
	}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		job, payload, err := loadWorkspaceImportBatch(txApp, jobID, user.Id, workspaceID)
		if err != nil {
			return err
		}
		if payload.Phase != workspaceImportPhaseBatchUpload || job.Status != durableJobStatusQueued {
			return nil
		}
		for _, item := range input.Items {
			if item.FileID != "" {
				file, err := txApp.FindRecordById(filesCollection, item.FileID)
				if err != nil || !workspaceFileRecordBelongsTo(file, workspaceID) {
					return workspaceImportInputError()
				}
			}
			state, turn := workspaceImportBatchItemPending, 1
			switch item.Kind {
			case workspaceImportBatchItemKindSkipped:
				state = workspaceImportBatchItemSkipped
			case workspaceImportBatchItemKindDatabase:
				// Its page exists before the documents that mention it go in.
				turn = 0
			}
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_workspace_import_batch_items}}
					([[job_id]], [[position]], [[parent_position]], [[kind]], [[turn]], [[name]], [[title]], [[page_id]], [[state]], [[file_id]])
				VALUES ({:jobId}, {:position}, {:parent}, {:kind}, {:turn}, {:name}, {:title}, {:pageId}, {:state}, {:fileId})
				ON CONFLICT ([[job_id]], [[position]]) DO NOTHING
			`).Bind(dbx.Params{
				"jobId": job.ID, "position": item.Position, "parent": item.ParentPosition, "kind": item.Kind, "turn": turn,
				"name": item.Name, "title": strings.TrimSpace(item.Title), "state": state, "fileId": item.FileID,
				"pageId": newPageID(),
			}).Execute(); err != nil {
				return err
			}
		}
		return r.keepWorkspaceImportBatchOpen(txApp, job.ID, r.clock.now().UnixMilli())
	})
	if err != nil {
		return workspaceImportBatchMissing(event, err, "batch-items")
	}
	items, _, err := readWorkspaceImportBatchItems(r.app, jobID, first-1, last-first+1, true)
	if err != nil {
		return workspaceImportAPIError(event, err, "batch-items-read")
	}
	return event.JSON(http.StatusOK, workspaceImportBatchItemList{Items: items})
}

func readWorkspaceImportBatchItems(app core.App, jobID string, after, limit int, folders bool) ([]workspaceImportBatchItem, bool, error) {
	rows := []storedWorkspaceImportBatchItem{}
	folderFilter := ""
	if !folders {
		folderFilter = "AND [[kind]] <> 'folder'"
	}
	if err := app.DB().NewQuery(`
		SELECT [[position]], [[parent_position]], [[kind]], [[turn]], [[name]], [[title]], [[page_id]], [[page_ready]],
			[[state]], [[format]], [[storage_key]], [[storage_offset]], [[storage_bytes]], [[sha256]], [[child_job_id]],
			[[error_code]], [[message]], [[details]]
		FROM {{leafen_workspace_import_batch_items}}
		WHERE [[job_id]] = {:jobId} AND [[position]] > {:after} ` + folderFilter + `
		ORDER BY [[position]] LIMIT {:limit}
	`).Bind(dbx.Params{"jobId": jobID, "after": after, "limit": limit + 1}).All(&rows); err != nil {
		return nil, false, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	items := make([]workspaceImportBatchItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, workspaceImportBatchItem{
			Position: row.Position, Kind: row.Kind, Name: row.Name, PageID: row.PageID, State: row.State,
			ErrorCode: row.ErrorCode, Message: row.Message, Details: row.Details,
		})
	}
	return items, more, nil
}

// listWorkspaceImportBatchItems reads what the batch brought, a window at a
// time, past the position it is asked after.
func (r *durableJobRunner) listWorkspaceImportBatchItems(event *core.RequestEvent) error {
	user, workspaceID, ok, err := authorizeWorkspaceArchiveUpload(event, "batch-list-authorize")
	if !ok {
		return err
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	if _, _, err := loadWorkspaceImportBatch(r.app, jobID, user.Id, workspaceID); err != nil {
		return workspaceImportBatchMissing(event, err, "batch-list")
	}
	after, err := strconv.Atoi(event.Request.URL.Query().Get("after"))
	if err != nil || after < -1 {
		after = -1
	}
	limit, err := strconv.Atoi(event.Request.URL.Query().Get("limit"))
	if err != nil || limit < 1 || limit > workspaceImportBatchItemsPerRequest {
		limit = workspaceImportBatchItemsPerRequest
	}
	items, more, err := readWorkspaceImportBatchItems(r.app, jobID, after, limit, false)
	if err != nil {
		return workspaceImportAPIError(event, err, "batch-list-read")
	}
	return event.JSON(http.StatusOK, workspaceImportBatchItemList{Items: items, HasMore: more})
}

// beginWorkspaceImportBatch hands the batch to the server once all its files
// are here: from then on it goes on with no one there.
func (r *durableJobRunner) beginWorkspaceImportBatch(txApp core.App, job storedJob, payload workspaceImportJobPayload) (bool, error) {
	var counts struct {
		Documents int `db:"documents"`
		Waiting   int `db:"waiting"`
		Imported  int `db:"imported"`
	}
	if err := txApp.DB().NewQuery(`
		SELECT
			COUNT(*) AS [[documents]],
			COALESCE(SUM([[state]] = 'pending'), 0) AS [[waiting]],
			COALESCE(SUM([[state]] = 'imported'), 0) AS [[imported]]
		FROM {{leafen_workspace_import_batch_items}}
		WHERE [[job_id]] = {:jobId} AND [[kind]] IN ('document', 'database', 'file')
	`).Bind(dbx.Params{"jobId": job.ID}).One(&counts); err != nil {
		return false, err
	}
	if counts.Documents != job.Total || counts.Waiting != 0 {
		return false, nil
	}
	payload.Phase = workspaceImportPhaseBatchPages
	encoded, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	now := r.clock.now().UnixMilli()
	_, err = txApp.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[payload_json]] = {:payload}, [[processed_items]] = {:processed}, [[not_before]] = {:now}, [[updated]] = {:now}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'
	`).Bind(dbx.Params{"id": job.ID, "payload": string(encoded), "processed": counts.Imported, "now": now}).Execute()
	return err == nil, err
}

// startWorkspaceImportBatch begins a batch that has nothing more to send: the
// same files chosen again with all of them already here.
func (r *durableJobRunner) startWorkspaceImportBatch(event *core.RequestEvent) error {
	user, workspaceID, ok, err := authorizeWorkspaceArchiveUpload(event, "batch-start-authorize")
	if !ok {
		return err
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	incomplete := false
	err = event.App.RunInTransaction(func(txApp core.App) error {
		job, payload, err := loadWorkspaceImportBatch(txApp, jobID, user.Id, workspaceID)
		if err != nil {
			return err
		}
		if job.Status != durableJobStatusQueued || payload.Phase != workspaceImportPhaseBatchUpload {
			return nil
		}
		began, err := r.beginWorkspaceImportBatch(txApp, job, payload)
		incomplete = !began
		return err
	})
	if incomplete {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportStateInvalid, "Not all of the import's files have arrived.")
	}
	if err != nil {
		return workspaceImportBatchMissing(event, err, "batch-start")
	}
	r.notify()
	return r.respondWorkspaceImportBatch(event, jobID, user.Id, workspaceID, false)
}
