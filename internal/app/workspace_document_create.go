package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/reearth/ygo/crdt"
)

// The library's "새 문서" (document apps decision of 2026-10-11, 12): a blank
// file of a type a document app starts from — the template its package
// carries (manifest newFiles) — copied as the person into a page they may
// change, at the end of it, by default their own private "문서" page, made
// when they have none. The person then opens it with the app. One request
// makes one document however often it is sent.

var workspaceDocumentContentTypes = map[string]string{
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".pdf":  "application/pdf",
	".hwp":  "application/x-hwp",
	".hwpx": "application/hwp+zip",
}

var errWorkspaceDocumentPageConflict = errors.New(errorCodeWorkspaceDocumentPageConflict)

func registerWorkspaceDocumentCreateRoute(event *core.ServeEvent) {
	event.Router.POST(apiRoot+workspaceDocumentCreatePath, createWorkspaceDocumentRoute).
		Bind(apis.BodyLimit(workspaceDocumentCreateMaxBodyBytes))
}

func createWorkspaceDocumentRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := workspaceDocumentCreateInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceDocumentCreateInvalid, "Check the new document.")
	}
	input.RequestID, input.InstallID, input.Opener = strings.TrimSpace(input.RequestID), strings.TrimSpace(input.InstallID), strings.TrimSpace(input.Opener)
	input.Type, input.PageID = strings.ToLower(strings.TrimSpace(input.Type)), strings.TrimSpace(input.PageID)
	input.Name, input.HomeTitle = strings.TrimSpace(input.Name), strings.TrimSpace(input.HomeTitle)
	if workspaceID == "" || input.RequestID == "" || len(input.RequestID) > 128 || input.InstallID == "" || input.Opener == "" ||
		workspaceDocumentContentTypes[input.Type] == "" || input.Name == "" || utf8.RuneCountInString(input.Name) > workspaceDocumentNameRunes ||
		len(input.PageID) > maxGraphIdentifierLength || (input.PageID == "" && input.HomeTitle == "") ||
		utf8.RuneCountInString(input.HomeTitle) > workspaceDocumentHomeTitleRunes || strings.ContainsAny(input.Name, "/\\\r\n") {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceDocumentCreateInvalid, "Check the new document.")
	}
	if _, err := authorizeWorkspaceReader(event.App, user.Id, workspaceID); err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return workspaceDocumentsFailure(event, err)
	}
	template, found, err := workspaceDocumentTemplate(event.App, user.Id, input)
	if err != nil {
		return workspaceDocumentsFailure(event, err)
	}
	if !found {
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "No app starts that kind of document.")
	}
	pageID := input.PageID
	if pageID == "" {
		if pageID, err = workspaceDocumentsHome(event.App, user.Id, workspaceID, input); err != nil {
			if errors.Is(err, errWorkspaceDocumentPageConflict) || destinationRefused(err) {
				return apiErrorFrom(event, http.StatusConflict, errorCodeWorkspaceDocumentPageConflict, workspaceDocumentRequestReused, err)
			}
			return workspaceDocumentsFailure(event, err)
		}
	}
	page, err := authorizePageBody(event.App, user.Id, pageID, permissionWorkspaceWrite)
	if refusal := workspaceDocumentPageRefused(event.App, user.Id, workspaceID, pageID, page, err); refusal != nil {
		return apiErrorFrom(event, http.StatusConflict, errorCodeWorkspaceDocumentPageConflict, workspaceDocumentRequestReused, refusal)
	}
	if err != nil {
		return workspaceDocumentsFailure(event, err)
	}
	name := input.Name
	if !strings.HasSuffix(strings.ToLower(name), input.Type) {
		name += input.Type
	}
	record, err := storeWorkspaceDocumentFile(event.App, user.Id, workspaceID, input.RequestID, name, input.Type, template)
	if err != nil {
		return workspaceDocumentsFailure(event, err)
	}
	blockID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("leafen:new-document-block:v1\x00"+user.Id+"\x00"+input.RequestID)).String()
	content, err := appendWorkspaceDocumentBlock(event.Request.Context(), event.App, user.Id, pageID, blockID, record)
	if errors.Is(err, errWorkspaceDocumentPageConflict) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceDocumentPageConflict, workspaceDocumentRequestReused)
	}
	if err != nil {
		return workspaceDocumentsFailure(event, err)
	}
	about, err := readWorkspaceDocumentPage(event.App, user.Id, workspaceID, pageID)
	if err != nil {
		return workspaceDocumentsFailure(event, err)
	}
	return event.JSON(http.StatusCreated, workspaceDocumentCreatedWire{Document: workspaceDocumentWire{
		BlockID: blockID, PageID: pageID, FileID: content.FileID, Name: content.Name, ContentType: content.ContentType,
		Size: content.Size, Version: mcpFileBaseVersion(content), Kind: strings.TrimPrefix(input.Type, "."), CanEdit: about.Editable,
		CreatedBy: user.Id, UpdatedAt: record.GetDateTime("created").Time().UTC().Format(time.RFC3339Nano),
		PageTitle: about.Title, PageIcon: about.Icon, PageIconType: about.IconType, ContainerPageID: about.ContainerPageID,
	}})
}

// workspaceDocumentTemplate reads the blank file the app the person may use
// starts documents of the type from.
func workspaceDocumentTemplate(app core.App, userID string, input workspaceDocumentCreateInput) ([]byte, bool, error) {
	apps, err := usableApps(app, userID)
	if err != nil {
		return nil, false, err
	}
	for _, usable := range apps {
		if usable.Install.ID != input.InstallID {
			continue
		}
		for _, opener := range usable.Manifest.Surfaces.FileOpeners {
			if opener.Key != input.Opener || !opener.Edit {
				continue
			}
			for _, made := range opener.NewFiles {
				if made.Type != input.Type {
					continue
				}
				data, err := loadAppPackageFile(app, usable.Install.PackageHash, path.Clean(made.Template))
				if err != nil || len(data) == 0 || len(data) > workspaceDocumentTemplateMaxBytes {
					return nil, false, errors.Join(errors.New("the app's blank file can't be read"), err)
				}
				return data, true, nil
			}
		}
	}
	return nil, false, nil
}

// workspaceDocumentsHome is the person's own "문서" page: their private page
// at the top called so, or one made now.
func workspaceDocumentsHome(app core.App, userID, workspaceID string, input workspaceDocumentCreateInput) (string, error) {
	pageID := ""
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_pages}}
		WHERE [[workspace]] = {:workspaceId} AND [[private_owner_id]] = {:userId} AND [[parent_id]] = '' AND [[teamspace_id]] = ''
			AND [[title]] = {:title} AND [[kind]] = {:kind} AND [[data_source_id]] = '' AND [[in_trash]] = FALSE
			AND [[deletion_job_id]] = '' AND [[notion_import_staging]] = FALSE AND [[database_template]] = FALSE
		ORDER BY [[created]], [[id]] LIMIT 1
	`).Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID, "title": input.HomeTitle, "kind": pageKindPage}).Row(&pageID)
	if err == nil {
		return pageID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	digest := sha256.Sum256([]byte("leafen:documents-home:v1\x00" + userID + "\x00" + input.RequestID))
	admission := normalizePageCreationAdmissionInput(pageCreationAdmissionInput{
		RequestID: "documents-home-" + hex.EncodeToString(digest[:16]), WorkspaceID: workspaceID, Title: input.HomeTitle,
	})
	created, status, err := admitPageCreation(app, userID, admission)
	if err != nil {
		return "", err
	}
	if status == pageCreationParentDenied {
		return "", refusedFor(http.StatusForbidden, errorCodePermissionDenied, "Check pageId: the organization keeps its restricted "+
			"members from making private pages, so no documents page of yours can be made; name a page you edit (pageId).",
			apiErrorField{Name: "pageId", Problem: "required here"})
	}
	if status != pageCreationApplied {
		return "", errWorkspaceDocumentPageConflict
	}
	return created.Page.ID, nil
}

// workspaceDocumentRequestReused is the conflict left once a page's refusal
// says its reason (workspaceDocumentPageRefused): the request ID made a
// document elsewhere already.
const workspaceDocumentRequestReused = "This request ID was used for a document on another page; send a new requestId."

// workspaceDocumentPageRefused is why the page a new document goes on refuses
// it, or nil: one the person can't add to, one of another workspace as such
// (other_workspace_refusal.go) — never as a page changed meanwhile
// (2026-11-26: both were 409 "Choose a page you can change.").
func workspaceDocumentPageRefused(app core.App, userID, workspaceID, pageID string, page pageAuthorization, err error) error {
	switch {
	case isPermissionDenied(err):
		return refusedFor(http.StatusForbidden, errorCodePermissionDenied, "Check pageId: it isn't a page you can add to; "+
			"name one you edit, or leave it out for your documents page.", apiErrorField{Name: "pageId", Problem: "not one you can add to"})
	case err == nil && page.Workspace.WorkspaceID != workspaceID:
		return otherWorkspaceRefused(app, "pageId", workspaceCrossing{
			subject: "the new document", from: workspaceID, place: "the page " + pageID, to: page.Workspace.WorkspaceID, made: true,
		})
	}
	return nil
}

// storeWorkspaceDocumentFile keeps the blank file as the person's upload,
// once per request.
func storeWorkspaceDocumentFile(app core.App, userID, workspaceID, requestID, name, kind string, data []byte) (*core.Record, error) {
	file, err := filesystem.NewFileFromBytes(data, name)
	if err != nil {
		return nil, err
	}
	digest, err := fileDigest(file.Reader)
	if err != nil {
		return nil, err
	}
	seed := sha256.Sum256([]byte("leafen:new-document-file:v1\x00" + userID + "\x00" + requestID))
	input := fileUploadPrepareInput{
		RequestID: "new-document-" + hex.EncodeToString(seed[:16]), WorkspaceID: workspaceID,
		Name: name, Size: int64(len(data)), ContentType: workspaceDocumentContentTypes[kind],
	}
	if err := normalizeProductFileUploadInput(&input); err != nil {
		return nil, err
	}
	release := acquireProductFileOperationLock(productFileOperationScopedNamespace(productFileOperationUploadRequest, userID), input.RequestID)
	defer release()
	upload, _, err := admitDirectProductFileUploadWithID(app, userID, input, digest, hex.EncodeToString(seed[16:])[:15])
	if err != nil {
		return nil, err
	}
	uploadRelease := acquireProductFileOperationLock(productFileOperationResumable, upload.UploadID)
	defer uploadRelease()
	if upload.Status == fileUploadStatusCompleted {
		return completedDirectProductFileUpload(app, upload)
	}
	return publishDirectProductFileUpload(app, upload, file, digest)
}

// appendWorkspaceDocumentBlock puts the file in a new file block at the end
// of the page, unless the request already did.
func appendWorkspaceDocumentBlock(ctx context.Context, app core.App, userID, pageID, blockID string, record *core.Record) (graphFileBlockContent, error) {
	content := canonicalGraphFileContent(record)
	if existing, found, err := loadStoredNotionBlock(app, blockID); err != nil {
		return graphFileBlockContent{}, err
	} else if found {
		if existing.PageID != pageID {
			return graphFileBlockContent{}, errWorkspaceDocumentPageConflict
		}
		return parseGraphFileBlockContent(json.RawMessage(existing.Content))
	}
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return graphFileBlockContent{}, errors.New("the page's content can't be changed now")
	}
	_, err := persistence.applyContentGraphMutationContext(ctx, userID, pageID,
		graphMutationInput{MutationID: "new-document:" + security.RandomString(20)},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				return authorizePageBody(txApp, userID, pageID, permissionWorkspaceWrite)
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				last := ""
				if err := txApp.DB().NewQuery(`
					SELECT COALESCE(MAX([[position]]), '') FROM {{leafen_blocks}}
					WHERE [[page_id]] = {:page} AND COALESCE([[parent_id]], '') = ''
				`).Bind(dbx.Params{"page": pageID}).Row(&last); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				position, err := databasePositionBetween(last, "")
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				encoded, err := json.Marshal(content)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind:  graphOperationKindUpsertBlock,
					Block: &graphBlock{ID: blockID, Position: position, Type: graphBlockTypeFile, Content: encoded},
				}}}, nil
			},
		},
	)
	return content, err
}
