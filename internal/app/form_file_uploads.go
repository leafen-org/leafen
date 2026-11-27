package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A form's file question takes files as Notion's does (seen 2026-11-02:
// "업로드", with a size limit of 100MB and at most 20 files): each goes up on
// its own,
// for the form's database page, sent by whom the form's answer is written
// as — a member respondent themselves, or the form's owner for the web — and
// is noted against the form and its question. An answer names the files by
// their id, and only files sent through that form for that question, not
// yet in another answer, join the new page: no other file of the workspace
// can be slipped into a response. A file over one request's size goes up in
// parts through the same resumable upload as the workspace's, behind the
// form's checks; one no answer has named a day later is let go.

const (
	formFileUploadsTable   = "leafen_form_file_uploads"
	maxFormFilesPerAnswer  = 20
	formFileUploadWindow   = 10 * time.Minute
	formFileUploadsPerWait = 60
	formFileMaxBytes       = 100 << 20
	formFileUnusedAfter    = 24 * time.Hour
	formFileCleanupBatch   = 200
)

// formFileUploadPace keeps one respondent (or one address on the web) to a
// bounded number of files per form within a window.
type formFileUploadPace struct {
	mu    sync.Mutex
	sends map[string][]time.Time
}

var formFileUploadPaces = &formFileUploadPace{sends: map[string][]time.Time{}}

func (pace *formFileUploadPace) admit(key string, now time.Time) bool {
	pace.mu.Lock()
	defer pace.mu.Unlock()
	kept := pace.sends[key][:0]
	for _, at := range pace.sends[key] {
		if now.Sub(at) < formFileUploadWindow {
			kept = append(kept, at)
		}
	}
	if len(kept) >= formFileUploadsPerWait {
		pace.sends[key] = kept
		return false
	}
	pace.sends[key] = append(kept, now)
	if len(pace.sends) > 4096 {
		for other, times := range pace.sends {
			if len(times) == 0 || now.Sub(times[len(times)-1]) >= formFileUploadWindow {
				delete(pace.sends, other)
			}
		}
	}
	return true
}

func registerFormFileUploadRoute(event *core.ServeEvent) {
	event.Router.POST(apiRoot+formPath+"/files", uploadFormFile)
	event.Router.POST(apiRoot+formPath+"/files/uploads", prepareFormFileUpload).
		Bind(apis.BodyLimit(fileUploadJSONBodyBytes))
	event.Router.PUT(apiRoot+formPath+"/files/uploads/{uploadId}/parts/{partNumber}", putFormFileUploadPart).
		Bind(apis.BodyLimit(fileTransferPartBytes + fileUploadJSONBodyBytes))
	event.Router.POST(apiRoot+formPath+"/files/uploads/{uploadId}/complete", completeFormFileUpload).
		Bind(apis.BodyLimit(fileUploadJSONBodyBytes))
	// Files no answer named are let go a day on, looked at the start and every
	// hour, behind both and stopped with the server (server_background.go).
	clean := func() {
		serverBackgroundOf(event.App).run("unused form files", func(context.Context) {
			if err := cleanUnusedFormFiles(event.App, time.Now().UTC()); err != nil {
				event.App.Logger().Warn("forms: couldn't let go of unused form files", "error", err)
			}
		})
	}
	clean()
	event.App.Cron().MustAdd("leafenFormUnusedFiles", "23 * * * *", clean)
}

// formFileGrant is who a form's file goes up as and for which form.
type formFileGrant struct {
	link        storedFormLink
	actorID     string
	principalID string
}

// paceKey is the respondent a form's sends are counted for: who is signed
// in, or the address on the web.
func (grant formFileGrant) paceKey(event *core.RequestEvent) string {
	if grant.principalID == "" {
		return grant.link.ID + "\x00" + event.RealIP()
	}
	return grant.link.ID + "\x00" + grant.principalID
}

// authorizeFormFile checks a form's token lets a file go up for the visible
// file question propertyID, and says whom it goes up as.
func authorizeFormFile(event *core.RequestEvent, token, propertyID string) (formFileGrant, error) {
	return authorizeFormQuestion(event, token, propertyID, graphValueTypeFiles)
}

// authorizeFormQuestion checks a form's token lets its respondent act for its
// visible question propertyID of the kind propertyType — send a file, search
// a place — and says whom the act goes as.
func authorizeFormQuestion(event *core.RequestEvent, token, propertyID, propertyType string) (formFileGrant, error) {
	if !validFormToken(token) {
		return formFileGrant{}, formInvalid("token is the one in the form's link, /forms/{token}")
	}
	if propertyID == "" {
		return formFileGrant{}, formInvalid("propertyId is empty: the question's propertyId")
	}
	principalID := ""
	if user, ok := authenticatedUser(event); ok {
		principalID = user.Id
	}
	link, err := loadStoredFormLinkByToken(event.App, token)
	if err != nil {
		return formFileGrant{}, err
	}
	actorID := principalID
	switch link.Audience {
	case formAudienceWorkspace:
		if principalID == "" {
			return formFileGrant{}, errFormAuthenticationRequired
		}
		// A member's files go up with the form's owner's access, as its answer.
		member, err := formWorkspaceMember(event.App, principalID, link)
		if err != nil {
			return formFileGrant{}, err
		}
		if member {
			if err := formPageOpen(event.App, link); err != nil {
				return formFileGrant{}, err
			}
			actorID = link.Owner
		}
	case formAudienceWeb:
		if err := authorizeFormDefinition(event.App, principalID, link); err != nil {
			return formFileGrant{}, err
		}
		actorID = link.Owner
	default:
		return formFileGrant{}, errFormNotFound
	}
	authorization, err := authorizePageBody(event.App, actorID, link.PageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) || err == nil && (authorization.Workspace.OwnerID != link.Owner ||
		authorization.Workspace.WorkspaceID != link.WorkspaceID) {
		return formFileGrant{}, errFormNotFound
	}
	if err != nil {
		return formFileGrant{}, err
	}
	source, err := loadFormSource(event.App, link.Owner, link.PageID, link.DataSourceID, link.ViewID, link.Audience, nil)
	if err != nil || source.ViewConfig["type"] != graphViewTypeForm || source.ViewConfig["leafenFormAudience"] != link.Audience {
		return formFileGrant{}, refusedUnlessStorage(errFormNotFound, err)
	}
	// An anonymous answer's files go up as the form's owner, as a web form's do.
	if link.Audience == formAudienceWorkspace && formSourceAnonymous(source) {
		actorID = link.Owner
	}
	for index, question := range source.Form.Questions {
		if question.PropertyID == propertyID && !question.Hidden && source.Properties[index].Type == propertyType {
			return formFileGrant{link: link, actorID: actorID, principalID: principalID}, nil
		}
	}
	return formFileGrant{}, formInvalid("propertyId %q names no shown %s question of this form", propertyID, propertyType)
}

// authorizeFormFileUpload checks a form's token for a resumable upload begun
// through that form and not yet named by an answer.
func authorizeFormFileUpload(event *core.RequestEvent, token, uploadID string) (formFileGrant, string, error) {
	var noted struct {
		LinkID     string `db:"form_link_id"`
		PropertyID string `db:"property_id"`
		UsedRowID  string `db:"used_row_id"`
	}
	if err := event.App.DB().NewQuery(`
		SELECT [[form_link_id]], [[property_id]], [[used_row_id]] FROM {{` + formFileUploadsTable + `}}
		WHERE [[file_id]] = {:fileId}
	`).Bind(dbx.Params{"fileId": uploadID}).One(&noted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return formFileGrant{}, "", errFormNotFound
		}
		return formFileGrant{}, "", err
	}
	grant, err := authorizeFormFile(event, token, noted.PropertyID)
	if err != nil {
		return formFileGrant{}, "", err
	}
	if grant.link.ID != noted.LinkID || noted.UsedRowID != "" {
		return formFileGrant{}, "", errFormNotFound
	}
	return grant, noted.PropertyID, nil
}

func noteFormFileUpload(app core.App, fileID, linkID, propertyID string) error {
	_, err := app.DB().NewQuery(`
		INSERT OR IGNORE INTO {{` + formFileUploadsTable + `}} ([[file_id]], [[form_link_id]], [[property_id]], [[uploaded_at]], [[used_row_id]])
		VALUES ({:fileId}, {:linkId}, {:propertyId}, {:now}, '')
	`).Bind(dbx.Params{"fileId": fileID, "linkId": linkID, "propertyId": propertyID, "now": time.Now().UTC().UnixMilli()}).Execute()
	return err
}

func uploadFormFile(event *core.RequestEvent) error {
	event.Request.Body = http.MaxBytesReader(event.Response, event.Request.Body, fileTransferDirectBytes+(1<<16))
	uploads, err := event.FindUploadedFiles("file")
	if event.Request.MultipartForm != nil {
		defer event.Request.MultipartForm.RemoveAll()
	}
	if err != nil || len(uploads) != 1 {
		return writeFormError(event, formInvalid("the body is a multipart form with one file in its \"file\" part"), "file")
	}
	upload := uploads[0]
	token := strings.TrimSpace(event.Request.FormValue("token"))
	propertyID := strings.TrimSpace(event.Request.FormValue("propertyId"))
	requestID := strings.TrimSpace(event.Request.FormValue("requestId"))
	if requestID == "" || len(requestID) > 128 {
		return writeFormError(event, formInvalid("requestId is a new id of yours, 1–128 bytes, so the same request sent again isn't done twice"), "file")
	}
	grant, err := authorizeFormFile(event, token, propertyID)
	if err != nil {
		return writeFormError(event, err, "file")
	}
	if !formFileUploadPaces.admit(grant.paceKey(event), time.Now()) {
		return writeFormError(event, errFormRateLimit, "file")
	}
	link, actorID := grant.link, grant.actorID
	input := fileUploadPrepareInput{
		// Many web respondents send as the form's owner: each request is the form's own.
		RequestID: "form:" + link.ID + ":" + requestID, WorkspaceID: link.WorkspaceID,
		Name: strings.TrimSpace(upload.OriginalName), Size: upload.Size,
		ContentType: directProductFileContentType(event), PageID: link.PageID, Purpose: fileUploadPurposePage,
	}
	if err := normalizeProductFileUploadInput(&input); err != nil {
		return directFileUploadError(event, err, "form-file-validate")
	}
	if input.Size > fileTransferDirectBytes {
		return directFileUploadError(event, errFileUploadResumableRequired, "form-file-validate")
	}
	if err := requireDataDiskRoom(event.App, input.Size); err != nil {
		return writeServerDiskLow(event, "form-file", err)
	}
	digest, err := fileDigest(upload.Reader)
	if err != nil {
		return directFileUploadError(event, err, "form-file-digest")
	}
	release := acquireProductFileOperationLock(productFileOperationScopedNamespace(productFileOperationUploadRequest, actorID), input.RequestID)
	defer release()
	receipt, _, err := admitDirectProductFileUpload(event.App, actorID, input, digest)
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "form-file-metadata")
	}
	if err != nil {
		return directFileUploadError(event, err, "form-file-admit")
	}
	uploadRelease := acquireProductFileOperationLock(productFileOperationResumable, receipt.UploadID)
	defer uploadRelease()
	var record *core.Record
	if receipt.Status == fileUploadStatusCompleted {
		record, err = completedDirectProductFileUpload(event.App, receipt)
	} else {
		record, err = publishDirectProductFileUpload(event.App, receipt, upload, digest)
	}
	if err != nil {
		return directFileUploadError(event, err, "form-file-publish")
	}
	if err := noteFormFileUpload(event.App, record.Id, link.ID, propertyID); err != nil {
		return writeFormError(event, err, "file")
	}
	return event.JSON(http.StatusCreated, formFileResponse(record))
}

func formFileResponse(record *core.Record) map[string]any {
	file := fileRecordResponse(record)
	return map[string]any{"fileId": file.ID, "name": file.Name, "size": file.Size, "contentType": file.ContentType}
}

// formFileAnswerIDs reads a file question's answer: the ids of files sent
// for it, at most twenty.
func formFileAnswerIDs(value any) ([]string, error) {
	items, ok := value.([]any)
	if !ok || len(items) > maxFormFilesPerAnswer {
		return nil, formInvalid("a file question's answer is a list of up to %d file ids", maxFormFilesPerAnswer)
	}
	ids := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		id, ok := item.(string)
		id = strings.TrimSpace(id)
		if !ok || id == "" || len(id) > maxFormIdentifierCharacters || seen[id] {
			return nil, formInvalid("a file question's answer names each file once, by the id its upload answered with")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// resolveFormFileAnswers turns each file question's ids into the files the
// new page holds, from what was sent through this form for that question and
// not taken by another answer.
func resolveFormFileAnswers(app core.App, link storedFormLink, source formSource, rowID string, values map[string]graphRowValue) error {
	for _, property := range source.Properties {
		value, present := values[property.ID]
		if property.Type != graphValueTypeFiles || !present {
			continue
		}
		ids := []string{}
		if err := json.Unmarshal(value.Value, &ids); err != nil {
			return errFormInvalid
		}
		items := make([]graphFileValueItem, 0, len(ids))
		for _, id := range ids {
			var usedRowID string
			if err := app.DB().NewQuery(`
				SELECT [[used_row_id]] FROM {{` + formFileUploadsTable + `}}
				WHERE [[file_id]] = {:fileId} AND [[form_link_id]] = {:linkId} AND [[property_id]] = {:propertyId}
			`).Bind(dbx.Params{"fileId": id, "linkId": link.ID, "propertyId": property.ID}).Row(&usedRowID); err != nil {
				return formInvalid("answers.%s names file %q, which wasn't sent through this form for that question", property.ID, id)
			}
			if usedRowID != "" && usedRowID != rowID {
				return formInvalid("answers.%s names file %q, which another answer holds", property.ID, id)
			}
			record, err := app.FindRecordById(filesCollection, id)
			if err != nil || !workspaceFileRecordBelongsTo(record, link.WorkspaceID) {
				return formInvalid("answers.%s names file %q, which isn't the form's workspace's", property.ID, id)
			}
			file := fileRecordResponse(record)
			items = append(items, graphFileValueItem{FileID: file.ID, Name: file.Name, Size: file.Size, ContentType: file.ContentType, SHA256: file.SHA256})
		}
		encoded, err := json.Marshal(items)
		if err != nil {
			return errFormInvalid
		}
		values[property.ID] = graphRowValue{Type: property.Type, Value: encoded}
	}
	return nil
}

// markFormFilesUsed keeps an answer's files from joining any other.
func markFormFilesUsed(app core.App, link storedFormLink, source formSource, rowID string, values map[string]graphRowValue) error {
	for _, property := range source.Properties {
		value, present := values[property.ID]
		if property.Type != graphValueTypeFiles || !present {
			continue
		}
		items := []graphFileValueItem{}
		if err := json.Unmarshal(value.Value, &items); err != nil {
			return errFormInvalid
		}
		for _, item := range items {
			result, err := app.DB().NewQuery(`
				UPDATE {{` + formFileUploadsTable + `}} SET [[used_row_id]] = {:rowId}
				WHERE [[file_id]] = {:fileId} AND [[form_link_id]] = {:linkId} AND [[property_id]] = {:propertyId}
					AND [[used_row_id]] IN ('', {:rowId})
			`).Bind(dbx.Params{"rowId": rowID, "fileId": item.FileID, "linkId": link.ID, "propertyId": property.ID}).Execute()
			if err != nil {
				return err
			}
			if changed, _ := result.RowsAffected(); changed != 1 {
				return errFormConflict
			}
		}
	}
	return nil
}
