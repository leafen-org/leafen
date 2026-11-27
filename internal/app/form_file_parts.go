package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A form's file over one request's size goes up in parts through the
// workspace's own resumable upload, each step behind the form's checks
// (form_file_uploads.go); the files no answer named a day on are let go.

// prepareFormFileUpload begins a file too large for one request, as the
// workspace's own resumable upload does, up to the form's 100MB.
func prepareFormFileUpload(event *core.RequestEvent) error {
	body := struct {
		Token       string `json:"token"`
		PropertyID  string `json:"propertyId"`
		RequestID   string `json:"requestId"`
		Name        string `json:"name"`
		Size        int64  `json:"size"`
		ContentType string `json:"contentType"`
	}{}
	if err := event.BindBody(&body); err != nil {
		return writeFormError(event, bodyRefusal(errFormInvalid, "form file request", err), "file")
	}
	requestID := strings.TrimSpace(body.RequestID)
	if requestID == "" || len(requestID) > 128 {
		return writeFormError(event, formInvalid("requestId is a new id of yours, 1–128 bytes, so the same request sent again isn't done twice"), "file")
	}
	if body.Size <= 0 || body.Size > formFileMaxBytes {
		return writeFormError(event, formInvalid("size is the file's bytes, 1–%d", formFileMaxBytes), "file")
	}
	grant, err := authorizeFormFile(event, strings.TrimSpace(body.Token), strings.TrimSpace(body.PropertyID))
	if err != nil {
		return writeFormError(event, err, "file")
	}
	if !formFileUploadPaces.admit(grant.paceKey(event), time.Now()) {
		return writeFormError(event, errFormRateLimit, "file")
	}
	input := fileUploadPrepareInput{
		RequestID: "form:" + grant.link.ID + ":" + requestID, WorkspaceID: grant.link.WorkspaceID,
		Name: body.Name, Size: body.Size, ContentType: body.ContentType,
		PageID: grant.link.PageID, Purpose: fileUploadPurposePage,
	}
	if err := normalizeProductFileUploadInput(&input); err != nil {
		return fileUploadError(event, err, "form-prepare-validate")
	}
	if err := requireDataDiskRoom(event.App, input.Size); err != nil {
		return writeServerDiskLow(event, "form-file", err)
	}
	release := acquireProductFileOperationLock(productFileOperationScopedNamespace(productFileOperationUploadRequest, grant.actorID), input.RequestID)
	defer release()
	upload, created, err := admitProductFileUpload(event.App, grant.actorID, input)
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "form-file-metadata")
	}
	if err != nil {
		return fileUploadError(event, err, "form-prepare-admit")
	}
	if err := noteFormFileUpload(event.App, upload.UploadID, grant.link.ID, strings.TrimSpace(body.PropertyID)); err != nil {
		return writeFormError(event, err, "file")
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	return event.JSON(status, productFileUploadStateResponse(upload, !created))
}

// putFormFileUploadPart keeps one part of a form's resumable upload; the
// form's token comes in a header, the body being the part.
func putFormFileUploadPart(event *core.RequestEvent) error {
	uploadID, partNumber, digest, err := productFileUploadPartRequest(event)
	if err != nil {
		return fileUploadError(event, err, "form-part-validate")
	}
	grant, _, err := authorizeFormFileUpload(event, strings.TrimSpace(event.Request.Header.Get(formFileTokenHeader)), uploadID)
	if err != nil {
		return writeFormError(event, err, "file")
	}
	updated, duplicate, stage, err := storeProductFileUploadPart(event.App, grant.actorID, uploadID, partNumber, digest, event.Request.Body)
	if err != nil {
		return fileUploadError(event, err, "form-"+stage)
	}
	return event.JSON(http.StatusOK, productFileUploadPartResponse(updated, duplicate))
}

// completeFormFileUpload publishes a form's resumable upload whose parts are
// all in, answering as the direct upload does.
func completeFormFileUpload(event *core.RequestEvent) error {
	uploadID := strings.TrimSpace(event.Request.PathValue("uploadId"))
	body := struct {
		Token     string `json:"token"`
		RequestID string `json:"requestId"`
	}{}
	if err := event.BindBody(&body); err != nil {
		return writeFormError(event, bodyRefusal(errFormInvalid, "form file request", err), "file")
	}
	requestID := strings.TrimSpace(body.RequestID)
	if !fileUploadIDPattern.MatchString(uploadID) {
		return writeFormError(event, formInvalid("the path's uploadId is the id the upload's start answered with"), "file")
	}
	if requestID == "" || len(requestID) > 128 {
		return writeFormError(event, formInvalid("requestId is a new id of yours, 1–128 bytes, so the same request sent again isn't done twice"), "file")
	}
	grant, propertyID, err := authorizeFormFileUpload(event, strings.TrimSpace(body.Token), uploadID)
	if err != nil {
		return writeFormError(event, err, "file")
	}
	_, record, duplicate, stage, err := finishProductFileUpload(event.App, grant.actorID, uploadID, "form:"+grant.link.ID+":"+requestID)
	if err != nil {
		return fileUploadError(event, err, "form-"+stage)
	}
	if record.Id != uploadID {
		if err := noteFormFileUpload(event.App, record.Id, grant.link.ID, propertyID); err != nil {
			return writeFormError(event, err, "file")
		}
	}
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, formFileResponse(record))
}

// cleanUnusedFormFiles lets go of the files sent to forms that no answer
// named within a day, as whom each went up as; an upload never finished
// leaves only its note, the upload itself expiring on its own.
func cleanUnusedFormFiles(app core.App, now time.Time) error {
	unused := []struct {
		FileID string `db:"file_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[file_id]] FROM {{` + formFileUploadsTable + `}}
		WHERE [[used_row_id]] = '' AND [[uploaded_at]] < {:before}
		ORDER BY [[uploaded_at]] LIMIT {:limit}
	`).Bind(dbx.Params{"before": now.Add(-formFileUnusedAfter).UnixMilli(), "limit": formFileCleanupBatch}).All(&unused); err != nil {
		return err
	}
	var failures []error
	for _, item := range unused {
		record, err := app.FindRecordById(filesCollection, item.FileID)
		if err == nil {
			if _, _, err := deleteProductFileAs(context.Background(), app, record.GetString("created_by"), item.FileID, "form-unused:"+item.FileID); err != nil {
				failures = append(failures, err)
				continue
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			failures = append(failures, err)
			continue
		}
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{` + formFileUploadsTable + `}} WHERE [[file_id]] = {:fileId} AND [[used_row_id]] = ''
		`).Bind(dbx.Params{"fileId": item.FileID}).Execute(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
