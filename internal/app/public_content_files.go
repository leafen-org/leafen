package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Published pages show their images, covers, icons and files to visitors,
// as Notion's public pages do. A file is served without a session only
// while a page the share link or site shows displays it (a block, a
// database value, a page, row or data source icon, a cover), only while the
// link is on and unexpired, and only while the workspace's organization
// allows public sharing and file downloads. A site's own preview image and
// favicon are served by the same owner.

// publicContentShownPageSQL names the page that shows a referenced file: the
// page itself, or for a database page the page holding its database.
const publicContentShownPageSQL = `CASE WHEN [[page]].[[data_source_id]] = '' THEN [[reference]].[[page_id]]
	ELSE COALESCE((SELECT [[source]].[[page_id]] FROM {{leafen_data_sources}} [[source]]
		WHERE [[source]].[[id]] = [[page]].[[data_source_id]]), '') END`

var publicContentFileOwnerKinds = []string{
	productFileReferenceOwnerBlock, productFileReferenceOwnerRow,
	productFileReferenceOwnerPageIcon, productFileReferenceOwnerPageCover,
	productFileReferenceOwnerDatabaseRowPageIcon, productFileReferenceOwnerDataSourceIcon,
	productFileReferenceOwnerDataSourceCover,
}

func registerPublicContentFileRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/public-content/{source}/{key}/files/{fileId}", servePublicContentFile)
}

func publicContentFileNotFound(event *core.RequestEvent) error {
	return apiError(event, http.StatusNotFound, errorCodePublicContentNotFound, "Public page not found.")
}

func servePublicContentFile(event *core.RequestEvent) error {
	authorization, fileID, dlpActions, ok, failure := authorizePublicContentFile(event)
	if !ok {
		return failure
	}
	if err := servePublicProductFile(event, authorization.Workspace, fileID, nil, dlpActions, "private, max-age=300"); err != nil ||
		event.Status() >= http.StatusBadRequest {
		return err
	}
	recordContentReads(event.App, authorization.WorkspaceID, contentReader{Via: contentReadViaShare, Client: event.Request.PathValue("source") + ":" + authorization.AuthorityID},
		[]contentRead{{Kind: "file", ID: fileID, How: contentReadOpen}})
	return nil
}

// authorizePublicContentFile reads a public page's file request: which
// page's authorization shows the file, and which outbound rules decide its
// reading. When ok is false the answer was written and failure is returned.
func authorizePublicContentFile(event *core.RequestEvent) (authorization publicVisibilityAuthorization, fileID string,
	dlpActions []string, ok bool, failure error) {
	key := strings.TrimSpace(event.Request.PathValue("key"))
	fileID = strings.TrimSpace(event.Request.PathValue("fileId"))
	if key == "" || len(key) > maxPublicContentTokenCharacters || !workspaceExportFileRecordIDPattern.MatchString(fileID) {
		return authorization, fileID, nil, false, publicContentFileNotFound(event)
	}
	now := time.Now().UTC()
	authorization, err := authorizePublicContentLink(event.App, event.Request.PathValue("source"), key, now)
	if errors.Is(err, errPublicVisibilityNotFound) {
		return authorization, fileID, nil, false, publicContentFileNotFound(event)
	}
	if err != nil {
		return authorization, fileID, nil, false, apiErrorWithCause(event, http.StatusInternalServerError, errorCodePublicContentFailed,
			"Couldn't load the public page.", "public_content.file", "authorize", errorCodePublicContentFailed, err)
	}
	visible, err := publicContentFileVisible(event.App, authorization, fileID, now)
	if err != nil {
		return authorization, fileID, nil, false, apiErrorWithCause(event, http.StatusInternalServerError, errorCodePublicContentFailed,
			"Couldn't load the public page.", "public_content.file", "reference", errorCodePublicContentFailed, err)
	}
	if !visible {
		return authorization, fileID, nil, false, publicContentFileNotFound(event)
	}
	// A view link isn't publishing to the web, so only file downloads decide.
	dlpActions = []string{dlpOutboundPublicSharing, dlpOutboundFileDownloads}
	if event.Request.PathValue("source") == publicContentSourceView {
		dlpActions = []string{dlpOutboundFileDownloads}
	}
	return authorization, fileID, dlpActions, true, nil
}

// authorizePublicContentLink reads the share link, site or view link a
// public request comes through; a view link's page names its files and
// pictures with the key its open gave it.
func authorizePublicContentLink(app core.App, source, key string, now time.Time) (publicVisibilityAuthorization, error) {
	switch source {
	case publicContentSourceShare:
		return authorizePublicSharePages(app, key, nil, now)
	case publicContentSourceSite:
		return authorizePublicSitePages(app, key, nil, now)
	case publicContentSourceView:
		return authorizeViewLinkFiles(app, key, now)
	}
	return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
}

// publicContentFileVisible reports whether a page the authority shows (its
// root, a page of its complete graph, or a published page its page links
// reach) displays the file.
func publicContentFileVisible(
	app core.App, authorization publicVisibilityAuthorization, fileID string, now time.Time,
) (bool, error) {
	params := dbx.Params{
		"fileId": fileID, "workspaceId": authorization.WorkspaceID,
		"ownerId": authorization.Workspace.GetString("owner"), "linkId": authorization.AuthorityID,
		"rootPageId": authorization.RootPageID, "sourceRevision": authorization.SourceRevision,
	}
	kinds := make([]string, 0, len(publicContentFileOwnerKinds))
	for index, kind := range publicContentFileOwnerKinds {
		name := "kind" + string(rune('a'+index))
		params[name] = kind
		kinds = append(kinds, "{:"+name+"}")
	}
	found := 0
	// The file's references are found by the file and each one's page by its
	// id; workspace, owner and flags are read after (+), or SQLite reads the
	// workspace's every page and reference.
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM (
			SELECT 1 FROM {{leafen_file_references}} [[reference]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[reference]].[[page_id]]
			WHERE [[reference]].[[file_id]] = {:fileId} AND +[[reference]].[[workspace_id]] = {:workspaceId}
				AND [[reference]].[[owner_kind]] IN (` + joinSQL(kinds) + `)
				AND +[[page]].[[workspace]] = {:workspaceId} AND +[[page]].[[owner]] = {:ownerId}
				AND +[[page]].[[in_trash]] = 0 AND +[[page]].[[notion_import_staging]] = 0
				AND ((` + publicContentShownPageSQL + `) = {:rootPageId} OR EXISTS (
					SELECT 1 FROM {{leafen_public_share_graph_memberships}} [[membership]]
					WHERE [[membership]].[[share_link_id]] = {:linkId}
						AND [[membership]].[[page_id]] = (` + publicContentShownPageSQL + `)
						AND [[membership]].[[root_page_id]] = {:rootPageId}
						AND [[membership]].[[source_revision]] = {:sourceRevision}
						AND [[membership]].[[state]] = 'complete'
				))
			LIMIT 1
		)
	`).Bind(params).Row(&found)
	if err != nil || found > 0 {
		return found > 0, err
	}
	return publicContentLinkedFileVisible(app, authorization, fileID, publicContentFileOwnerKinds, now)
}

// servePublicProductFile serves a workspace file to a visitor: never for a
// workspace being deleted, a file being uploaded or deleted, a disallowed
// content type, or an organization whose policy blocks the actions. Types a
// browser would run are sent as attachments in a sandbox.
func servePublicProductFile(
	event *core.RequestEvent,
	workspace *core.Record,
	fileID string,
	contentTypes map[string]struct{},
	dlpActions []string,
	cacheControl string,
) error {
	if workspace == nil || workspace.GetString("deletion_job_id") != "" || workspace.GetString("organization") == "" {
		return publicContentFileNotFound(event)
	}
	for _, action := range dlpActions {
		if err := enforceOrganizationDLPOutbound(event.App, workspace.GetString("organization"), action); err != nil {
			return publicContentFileNotFound(event)
		}
	}
	files, err := loadGraphFileAuthorityRecords(event.App, workspace.Id, []string{fileID})
	if err != nil {
		return publicContentFileNotFound(event)
	}
	if contentTypes != nil {
		if _, allowed := contentTypes[strings.ToLower(files[fileID].ContentType)]; !allowed {
			return publicContentFileNotFound(event)
		}
	}
	record, err := event.App.FindRecordById(filesCollection, fileID)
	if err != nil {
		return publicContentFileNotFound(event)
	}
	fsys, err := event.App.NewFilesystem()
	if err != nil {
		return err
	}
	defer fsys.Close()
	if err := verifyProductFileStorageIdentity(event.App, fsys, record); err != nil {
		return publicContentFileNotFound(event)
	}
	event.Response.Header().Set("Content-Type", record.GetString("content_type"))
	event.Response.Header().Set("X-Content-Type-Options", "nosniff")
	sandboxStoredFileDownload(event.Response.Header(), record.GetString("content_type"))
	event.Response.Header().Set("Cache-Control", cacheControl)
	event.Response.Header().Set("Referrer-Policy", "no-referrer")
	fileKey := record.BaseFilesPath() + "/" + record.GetString("blob")
	if productFilePosterAsked(event.Request.URL.Query()) {
		return serveProductFilePoster(event, fsys, record)
	}
	preview, outcome := productFilePreviewKey(event.Request.Context(), fsys, record, event.Request.URL.Query().Get("w"))
	switch {
	case preview != "":
		fileKey = preview
	case outcome == productFilePreviewBusy:
		event.Response.Header().Set("Cache-Control", "no-store")
	case outcome == productFilePreviewRefused:
		return refuseProductFilePreview(event)
	}
	return serveNamedFile(fsys, event.Response, event.Request, fileKey, productFileResponseName(event.Request, record.GetString("original_name")))
}
