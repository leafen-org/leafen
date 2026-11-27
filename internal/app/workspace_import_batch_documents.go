package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// The text of a batch's documents, kept on disk until they have gone in.

const workspaceImportBatchStorageInfix = ".batch-"

func validWorkspaceImportBatchDocument(document workspaceImportBatchDocumentInput) bool {
	if document.Position < 0 || utf8.RuneCountInString(document.Message) > workspaceImportBatchNoteRunes ||
		utf8.RuneCountInString(document.Details) > workspaceImportBatchNoteRunes {
		return false
	}
	if document.Failed {
		return document.Content == ""
	}
	return (document.Format == workspaceImportFormatMarkdown || document.Format == workspaceImportFormatCSV) &&
		len(document.Content) <= workspaceImportRawTextBytes
}

// stageWorkspaceImportBatchText keeps a window's text durably in one file of
// the batch's own, so a window of many small documents costs one sync.
func stageWorkspaceImportBatchText(appDir, jobID string, documents []workspaceImportBatchDocumentInput) (string, []int64, error) {
	directory := filepath.Join(appDir, workspaceImportDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", nil, workspaceImportStorageError(err)
	}
	key := jobID + workspaceImportBatchStorageInfix + security.RandomString(12)
	file, err := os.OpenFile(filepath.Join(directory, key), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", nil, workspaceImportStorageError(err)
	}
	buffered := bufio.NewWriter(file)
	offsets := make([]int64, len(documents))
	var at int64
	for index, document := range documents {
		offsets[index] = at
		count, err := buffered.WriteString(document.Content)
		if err != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
			return "", nil, workspaceImportStorageError(err)
		}
		at += int64(count)
	}
	if err := errors.Join(buffered.Flush(), file.Sync(), file.Close()); err != nil {
		_ = os.Remove(file.Name())
		return "", nil, workspaceImportStorageError(err)
	}
	if err := syncDirectory(directory); err != nil {
		_ = os.Remove(file.Name())
		return "", nil, workspaceImportStorageError(err)
	}
	return key, offsets, nil
}

// readWorkspaceImportBatchText reads one document's text back, as it came.
func readWorkspaceImportBatchText(appDir string, item storedWorkspaceImportBatchItem) (string, error) {
	if filepath.Base(item.StorageKey) != item.StorageKey || item.StorageKey == "" {
		return "", workspaceImportStateErrorAt("batch-text-key")
	}
	file, err := os.Open(filepath.Join(appDir, workspaceImportDirectory, item.StorageKey))
	if err != nil {
		return "", workspaceImportStateErrorAt("batch-text-open")
	}
	defer file.Close()
	text := make([]byte, item.StorageBytes)
	if _, err := file.ReadAt(text, item.StorageOffset); err != nil && item.StorageBytes > 0 {
		return "", workspaceImportStateErrorAt("batch-text-read")
	}
	digest := sha256.Sum256(text)
	if hex.EncodeToString(digest[:]) != item.SHA256 {
		return "", workspaceImportStateErrorAt("batch-text-digest")
	}
	return string(text), nil
}

// removeWorkspaceImportBatchText lets go of the text a batch was sent.
func removeWorkspaceImportBatchText(appDir, jobID string) error {
	directory := filepath.Join(appDir, workspaceImportDirectory)
	paths, err := filepath.Glob(filepath.Join(directory, jobID+workspaceImportBatchStorageInfix+"*"))
	if err != nil {
		return workspaceImportStorageError(err)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return workspaceImportStorageError(err)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return syncDirectory(directory)
}

// receiveWorkspaceImportBatchDocuments keeps a window of converted documents,
// or what kept one from being converted, and answers with the batch: one that
// has already gone on (from another tab) takes nothing more.
func (r *durableJobRunner) receiveWorkspaceImportBatchDocuments(event *core.RequestEvent) error {
	user, workspaceID, ok, err := authorizeWorkspaceArchiveUpload(event, "batch-documents-authorize")
	if !ok {
		return err
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	input := workspaceImportBatchDocumentsInput{}
	if err := event.BindBody(&input); err != nil {
		return workspaceImportBodyRefused(event, "batch documents request", err)
	}
	if problem := workspaceImportListProblem("documents", len(input.Documents), workspaceImportBatchItemsPerRequest); problem != "" {
		return workspaceImportRefused(event, problem)
	}
	var incoming int64
	for index, document := range input.Documents {
		if !validWorkspaceImportBatchDocument(document) {
			return workspaceImportRefused(event, fmt.Sprintf(
				"documents[%d] is a markdown or csv document of up to %d bytes at a position of 0 or more, or a failed one with no content", index, workspaceImportRawTextBytes))
		}
		incoming += int64(len(document.Content))
	}
	job, payload, err := loadWorkspaceImportBatch(r.app, jobID, user.Id, workspaceID)
	if err != nil {
		return workspaceImportBatchMissing(event, err, "batch-documents")
	}
	if job.Status != durableJobStatusQueued || payload.Phase != workspaceImportPhaseBatchUpload {
		return r.respondWorkspaceImportBatch(event, jobID, user.Id, workspaceID, true)
	}
	if err := requireDataDiskRoom(event.App, incoming); err != nil {
		return writeServerDiskLow(event, "workspace-import", err)
	}
	key, offsets, err := stageWorkspaceImportBatchText(r.app.DataDir(), jobID, input.Documents)
	if err != nil {
		return workspaceImportAPIError(event, err, "batch-documents-stage")
	}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		current, payload, err := loadWorkspaceImportBatch(txApp, jobID, user.Id, workspaceID)
		if err != nil {
			return err
		}
		if current.Status != durableJobStatusQueued || payload.Phase != workspaceImportPhaseBatchUpload {
			return nil
		}
		for index, document := range input.Documents {
			state, format, storageKey, digest := workspaceImportBatchItemReceived, document.Format, key, ""
			offset, bytes := offsets[index], int64(len(document.Content))
			if document.Failed {
				state, format, storageKey, offset, bytes = workspaceImportBatchItemFailed, "", "", 0, 0
			} else {
				sum := sha256.Sum256([]byte(document.Content))
				digest = hex.EncodeToString(sum[:])
			}
			if _, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_workspace_import_batch_items}}
				SET [[state]] = {:state}, [[format]] = {:format}, [[storage_key]] = {:key}, [[storage_offset]] = {:offset},
					[[storage_bytes]] = {:bytes}, [[sha256]] = {:digest}, [[message]] = {:message}, [[details]] = {:details}
				WHERE [[job_id]] = {:jobId} AND [[position]] = {:position} AND [[kind]] IN ('document', 'database', 'file')
					AND [[state]] IN ('pending', 'received', 'failed')
			`).Bind(dbx.Params{
				"jobId": jobID, "position": document.Position, "state": state, "format": format, "key": storageKey,
				"offset": offset, "bytes": bytes, "digest": digest, "message": document.Message, "details": document.Details,
			}).Execute(); err != nil {
				return err
			}
		}
		now := r.clock.now().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[processed_items]] = (SELECT COUNT(*) FROM {{leafen_workspace_import_batch_items}}
					WHERE [[job_id]] = {:id} AND [[state]] IN ('received', 'imported', 'failed') AND [[kind]] <> 'folder'),
				[[not_before]] = {:notBefore}, [[updated]] = {:now}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{"id": jobID, "now": now, "notBefore": now + workspaceImportBatchUploadIdle.Milliseconds()}).Execute(); err != nil {
			return err
		}
		// The last of the text has come: a reload right now loses nothing.
		_, err = r.beginWorkspaceImportBatch(txApp, current, payload)
		return err
	})
	if err != nil {
		_ = removeWorkspaceImportSourceFile(r.app.DataDir(), key)
		return workspaceImportBatchMissing(event, err, "batch-documents-keep")
	}
	r.notify()
	return r.respondWorkspaceImportBatch(event, jobID, user.Id, workspaceID, false)
}

// admitWorkspaceImportBatchDocument starts one document's import; the same
// request finds it if the batch stopped right after.
func (r *durableJobRunner) admitWorkspaceImportBatchDocument(
	job storedJob,
	payload workspaceImportJobPayload,
	item storedWorkspaceImportBatchItem,
) (string, error) {
	text, err := readWorkspaceImportBatchText(r.app.DataDir(), item)
	if err != nil {
		return "", err
	}
	input := workspaceImportInput{
		RequestID: fmt.Sprintf("workspace-import-batch-%s-%d-text", job.ID, item.Position),
		Format:    item.Format, Locale: payload.Locale, Title: item.Title,
	}
	if item.Format == workspaceImportFormatCSV {
		input.CSV = text
	} else {
		input.Markdown = text
	}
	admission := workspaceImportAdmission{Input: input, BatchJobID: job.ID}
	if item.Kind == workspaceImportBatchItemKindDocument {
		admission.TargetPageID, admission.Append = item.PageID, true
	} else if item.PageReady && item.PageID == payload.ParentPageID {
		// The empty page the batch went under, which its one file fills
		// (fillWorkspaceImportBatchPlace).
		admission.TargetPageID, admission.Append, admission.FillsPlace = item.PageID, true, true
		admission.Input.ParentPageID = payload.ParentPageID
	} else {
		admission.ReservedRootPageID = item.PageID
		if item.Kind == workspaceImportBatchItemKindDatabase {
			if err := r.app.DB().NewQuery(`
				SELECT [[title]], [[page_id]] FROM {{leafen_workspace_import_batch_items}}
				WHERE [[job_id]] = {:jobId} AND [[parent_position]] = {:position} AND [[kind]] = 'document'
				ORDER BY [[position]]
			`).Bind(dbx.Params{"jobId": job.ID, "position": item.Position}).All(&admission.ReservedRows); err != nil {
				return "", err
			}
		}
		var parent struct {
			PageID string `db:"page_id"`
			Ready  bool   `db:"page_ready"`
		}
		if item.ParentPosition >= 0 {
			if err := r.app.DB().NewQuery(`
				SELECT [[page_id]], [[page_ready]] FROM {{leafen_workspace_import_batch_items}}
				WHERE [[job_id]] = {:jobId} AND [[position]] = {:position}
			`).Bind(dbx.Params{"jobId": job.ID, "position": item.ParentPosition}).One(&parent); err != nil {
				return "", err
			}
		}
		switch {
		case parent.Ready:
			admission.Input.ParentPageID = parent.PageID
		case payload.ParentPageID != "":
			admission.Input.ParentPageID = payload.ParentPageID
		default:
			admission.Input.TeamspaceID, admission.Input.DataSourceID = payload.TeamspaceID, workspaceImportBatchRowSource(payload, item)
		}
	}
	childID, _, err := r.startWorkspaceImport(r.app, job.Owner, job.WorkspaceID, admission)
	return childID, err
}

// advanceWorkspaceImportBatchDocuments is one turn of the documents going in.
func (r *durableJobRunner) advanceWorkspaceImportBatchDocuments(ctx context.Context, job storedJob, payload workspaceImportJobPayload) error {
	going, err := r.settleWorkspaceImportBatchChildren(job)
	if err != nil {
		return err
	}
	next, err := loadWorkspaceImportBatchItemsInState(
		r.app, job.ID, workspaceImportBatchItemReceived, workspaceImportBatchDocumentsAtOnce-min(len(going), workspaceImportBatchDocumentsAtOnce),
	)
	if err != nil {
		return err
	}
	if len(going) == 0 && len(next) == 0 {
		left, err := loadWorkspaceImportBatchPagesLeft(r.app, job.ID)
		if err != nil {
			return err
		}
		if len(left) > 0 {
			return r.makeWorkspaceImportBatchPageRows(ctx, job, payload, left)
		}
		return r.completeWorkspaceImportBatch(job)
	}
	// A batch's databases go in before its documents and their pages: the
	// documents mention them, and an export's row pages are their rows,
	// which a database's import makes.
	databases := false
	for _, item := range slices.Concat(going, next) {
		databases = databases || item.Turn == 0
	}
	if databases {
		next = slices.DeleteFunc(next, func(item storedWorkspaceImportBatchItem) bool { return item.Turn > 0 })
	}
	positions := make([]int, 0, len(next))
	for _, item := range next {
		positions = append(positions, item.Position)
	}
	pages, err := loadWorkspaceImportBatchPagesFor(r.app, job.ID, positions)
	if err != nil {
		return err
	}
	if len(pages) > 0 {
		// The next documents' pages and folders this turn, the documents
		// the next.
		return r.makeWorkspaceImportBatchPageRows(ctx, job, payload, pages)
	}
	stuck, hold := payload.BatchBusyTurns, newWorkspaceImportBatchHold(payload, r.clock.now())
	keep := func(_ core.App, current *workspaceImportJobPayload) error {
		current.BatchBusyTurns = stuck
		hold.keep(current)
		return nil
	}
	for _, item := range next {
		var childID string
		var err error
		state, code, detail := workspaceImportBatchItemImporting, "", ""
		empty := false
		if item.Kind == workspaceImportBatchItemKindDocument && !item.PageReady {
			// A row page whose database didn't come in has no row to go in.
			state, code = workspaceImportBatchItemFailed, errorCodePageCreateParentConflict
		} else if empty, err = r.workspaceImportBatchEmptyRowPage(job.ID, item); err == nil && empty {
			state = workspaceImportBatchItemImported
		} else if err == nil {
			childID, err = r.admitWorkspaceImportBatchDocument(job, payload, item)
		}
		if err != nil {
			if workspaceImportBatchMayWait(err) && stuck < workspaceImportBatchBusyTurnsPerStuck {
				stuck++
				return r.scheduleWorkspaceImportBatch(job, workspaceImportBatchBusyDelay, keep)
			}
			if hold.waits(item.Position, workspaceImportHeldReason(r.app, job.Owner, err, item.PageID, payload.ParentPageID)) {
				return r.scheduleWorkspaceImportBatch(job, durableJobReadyDelay, keep)
			}
			state, code = workspaceImportBatchItemFailed, stableWorkspaceImportFailureCode(err)
			if isPermissionDenied(err) {
				code = errorCodePermissionDenied
			}
			detail = workspaceImportFailureDetail(err)
			if words := hold.words[item.Position]; words != "" {
				code, detail = errorCodePageCreateParentConflict, words
			}
		}
		stuck = 0
		message := item.Message
		if detail != "" {
			message = detail
		}
		if _, err := r.app.DB().NewQuery(`
			UPDATE {{leafen_workspace_import_batch_items}}
			SET [[state]] = {:state}, [[child_job_id]] = {:child}, [[error_code]] = {:code}, [[message]] = {:message}
			WHERE [[job_id]] = {:jobId} AND [[position]] = {:position}
		`).Bind(dbx.Params{
			"jobId": job.ID, "position": item.Position, "state": state, "child": childID, "code": code, "message": message,
		}).Execute(); err != nil {
			return err
		}
		if state == workspaceImportBatchItemImported {
			if _, err := r.app.DB().NewQuery(`
				UPDATE {{leafen_jobs}} SET [[processed_items]] = [[processed_items]] + 1 WHERE [[id]] = {:id}
			`).Bind(dbx.Params{"id": job.ID}).Execute(); err != nil {
				return err
			}
		}
	}
	return r.scheduleWorkspaceImportBatch(job, workspaceImportBatchTurnDelay, keep)
}
