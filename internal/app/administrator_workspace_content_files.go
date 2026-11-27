package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's files as a server administrator sees them (administrator_
// workspace_content.go): the largest first and by who put them there, each
// to download, and one no page uses to delete as the workspace's owner
// would. A file a page uses goes with the page.

const (
	administratorContentFileLimit = 100
	// administratorContentStoragePeople is how many people a window of the
	// storage list holds; the list reads on from its next cursor.
	administratorContentStoragePeople = 500
)

type storedAdministratorContentFile struct {
	ID          string `db:"id"`
	Name        string `db:"original_name"`
	ContentType string `db:"content_type"`
	Size        int64  `db:"size"`
	CreatedByID string `db:"created_by"`
	CreatedBy   string `db:"created_by_name"`
	PageID      string `db:"page_id"`
	PageTitle   string `db:"page_title"`
	CreatedAt   string `db:"created"`
}

// administratorContentFilesSQL are a workspace's own files that are not on
// their way out.
const administratorContentFilesSQL = `
	FROM {{leafen_files}} [[file]]
	WHERE [[file]].[[workspace]] = {:workspaceId} AND [[file]].[[authority_state]] = {:authority}
		AND [[file]].[[created_by]] != ''
		AND NOT EXISTS (SELECT 1 FROM {{leafen_file_delete_receipts}} [[deletion]]
			WHERE [[deletion]].[[file_id]] = [[file]].[[id]] AND [[deletion]].[[status]] = {:pending})`

// administratorFilePageSQL is the one page a file's line names, its ID and
// title from that same page: of the pages using it, the first by ID (two
// unordered picks named one page's ID beside another's title).
const administratorFilePageSQL = `SELECT [[reference]].[[page_id]] FROM {{leafen_file_references}} [[reference]]
	WHERE [[reference]].[[file_id]] = [[file]].[[id]] ORDER BY [[reference]].[[page_id]] LIMIT 1`

func listAdministratorWorkspaceFiles(event *core.RequestEvent) error {
	actorID, workspace, ok := administratorContentRequest(event)
	if !ok {
		return nil
	}
	offset, valid := decodeAdministratorContentOffset(event.Request.URL.Query().Get("cursor"))
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the file list position.")
	}
	rows := []storedAdministratorContentFile{}
	if err := event.App.DB().NewQuery(`
		SELECT [[file]].[[id]], [[file]].[[original_name]], [[file]].[[content_type]], [[file]].[[size]], [[file]].[[created_by]],
			COALESCE((SELECT ` + accountNameSQL("person") + ` FROM {{users}} [[person]]
				WHERE [[person]].[[id]] = [[file]].[[created_by]]), '') AS [[created_by_name]],
			COALESCE((` + administratorFilePageSQL + `), '') AS [[page_id]],
			COALESCE((SELECT [[page]].[[title]] FROM {{leafen_pages}} [[page]]
				WHERE [[page]].[[id]] = (` + administratorFilePageSQL + `)), '') AS [[page_title]],
			[[file]].[[created]]
		` + administratorContentFilesSQL + `
		ORDER BY [[file]].[[size]] DESC, [[file]].[[id]] LIMIT {:limit} OFFSET {:offset}
	`).Bind(dbx.Params{
		"workspaceId": workspace.ID, "authority": fileAuthorityWorkspace, "pending": fileDeleteStatusPending,
		"limit": administratorContentFileLimit + 1, "offset": offset,
	}).All(&rows); err != nil {
		return administratorContentError(event, err, "files")
	}
	list := administratorWorkspaceFileList{Items: []administratorWorkspaceFile{}}
	if len(rows) > administratorContentFileLimit {
		rows = rows[:administratorContentFileLimit]
		next := "o:" + strconv.Itoa(offset+administratorContentFileLimit)
		list.NextCursor = &next
	}
	for _, row := range rows {
		referenced, err := findLiveProductFileReference(event.App, row.ID)
		if err != nil {
			return administratorContentError(event, err, "file-reference")
		}
		// A file kept only by pages' version history can go: its space comes
		// back, and those versions show where it was.
		historyOnly := false
		if !referenced {
			if historyOnly, err = findActiveProductFileReference(event.App, row.ID); err != nil {
				return administratorContentError(event, err, "file-reference")
			}
		}
		held, err := administratorFileHeld(event.App, workspace, row.CreatedByID)
		if err != nil {
			return administratorContentError(event, err, "file-hold")
		}
		list.Items = append(list.Items, administratorWorkspaceFile{
			ID: row.ID, Name: row.Name, ContentType: row.ContentType, Size: row.Size,
			CreatedByID: row.CreatedByID, CreatedByName: row.CreatedBy, PageID: row.PageID,
			PageTitle: workspace.titleAsRead(event.App, row.PageID, row.PageTitle),
			CreatedAt: row.CreatedAt, Referenced: referenced, Held: held, HistoryOnly: historyOnly,
		})
	}
	// Each page's title with its spans, for a plain title to say its mentions (page_title_spans.go).
	titles := make(map[string]*string, len(list.Items))
	for index := range list.Items {
		titles[list.Items[index].PageID] = &list.Items[index].PageTitle
	}
	spans := pageTitlesSpansAsRead(event.App, workspace.OwnerID, titles)
	// And its kind, for an untitled page's name (page_name_kind.go): its
	// files said no page at all (2026-11-14).
	pageIDs := make([]string, 0, len(titles))
	for pageID := range titles {
		if pageID != "" {
			pageIDs = append(pageIDs, pageID)
		}
	}
	kinds, err := loadPageNameKinds(event.App, pageIDs)
	if err != nil {
		return administratorContentError(event, err, "files")
	}
	for index := range list.Items {
		item := &list.Items[index]
		item.PageTitleSpans = spans[item.PageID]
		item.PageKind, item.ViewBlock, item.SourceTitles = kinds[item.PageID].Kind, kinds[item.PageID].ViewBlock, kinds[item.PageID].SourceTitles
	}
	if offset == 0 {
		if err := recordAdministratorContentEvent(event.App, workspace.ID, actorID, administratorContentBrowseFiles,
			"workspace", workspace.ID, workspace.Name); err != nil {
			return administratorContentError(event, err, "record")
		}
	}
	return event.JSON(http.StatusOK, list)
}

// administratorFileHeld is whether a legal hold keeps a file its maker put
// in the workspace.
func administratorFileHeld(app core.App, workspace administratorContentWorkspace, createdBy string) (bool, error) {
	err := assertNoActiveLegalHoldForPermanentDelete(app, workspace.OrganizationID, legalHoldDeletionTarget{
		WorkspaceID: workspace.ID, CustodianUserIDs: []string{createdBy},
	})
	if legalHoldFailureCode(err) != "" {
		return true, nil
	}
	return false, err
}

// listAdministratorWorkspaceStorage is what each person's files in a
// workspace take, the most first, a window at a time to the last person.
func listAdministratorWorkspaceStorage(event *core.RequestEvent) error {
	_, workspace, ok := administratorContentRequest(event)
	if !ok {
		return nil
	}
	offset, valid := decodeAdministratorContentOffset(event.Request.URL.Query().Get("cursor"))
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the storage list position.")
	}
	rows := []struct {
		UserID string `db:"user_id"`
		Name   string `db:"name"`
		Email  string `db:"email"`
		Files  int64  `db:"file_count"`
		Bytes  int64  `db:"storage_bytes"`
	}{}
	if err := event.App.DB().NewQuery(`
		SELECT [[file]].[[created_by]] AS [[user_id]],
			COALESCE((SELECT [[person]].[[name]] FROM {{users}} [[person]] WHERE [[person]].[[id]] = [[file]].[[created_by]]), '') AS [[name]],
			COALESCE((SELECT ` + accountEmailSQL("person") + ` FROM {{users}} [[person]] WHERE [[person]].[[id]] = [[file]].[[created_by]]), '') AS [[email]],
			COUNT(*) AS [[file_count]], COALESCE(SUM([[file]].[[size]]), 0) AS [[storage_bytes]]
		` + administratorContentFilesSQL + `
		GROUP BY [[file]].[[created_by]] ORDER BY [[storage_bytes]] DESC, [[file]].[[created_by]] LIMIT {:limit} OFFSET {:offset}
	`).Bind(dbx.Params{
		"workspaceId": workspace.ID, "authority": fileAuthorityWorkspace, "pending": fileDeleteStatusPending,
		"limit": administratorContentStoragePeople + 1, "offset": offset,
	}).All(&rows); err != nil {
		return administratorContentError(event, err, "storage")
	}
	list := administratorWorkspaceStorageList{Items: []administratorWorkspaceStoragePerson{}}
	if len(rows) > administratorContentStoragePeople {
		rows = rows[:administratorContentStoragePeople]
		next := "o:" + strconv.Itoa(offset+administratorContentStoragePeople)
		list.NextCursor = &next
	}
	for _, row := range rows {
		list.Items = append(list.Items, administratorWorkspaceStoragePerson{
			UserID: row.UserID, Name: row.Name, Email: row.Email, FileCount: row.Files, StorageBytes: row.Bytes,
		})
	}
	return event.JSON(http.StatusOK, list)
}

// administratorContentFile is a workspace's own file, not on its way out.
func administratorContentFile(event *core.RequestEvent, workspace administratorContentWorkspace) (*core.Record, bool) {
	record, err := event.App.FindRecordById(filesCollection, strings.TrimSpace(event.Request.PathValue("fileId")))
	pending := false
	if err == nil {
		pending, err = productFileDeletionPending(event.App, record.Id)
	}
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (pending || !workspaceFileRecordBelongsTo(record, workspace.ID))) {
		_ = apiError(event, http.StatusNotFound, errorCodeFileNotFound, "File not found.")
		return nil, false
	}
	if err != nil {
		_ = administratorContentError(event, err, "file")
		return nil, false
	}
	return record, true
}

func downloadAdministratorWorkspaceFile(event *core.RequestEvent) error {
	actorID, workspace, ok := administratorContentRequest(event)
	if !ok {
		return nil
	}
	record, ok := administratorContentFile(event, workspace)
	if !ok {
		return nil
	}
	fsys, err := event.App.NewFilesystem()
	if err != nil {
		return administratorContentError(event, err, "filesystem")
	}
	defer fsys.Close()
	if err := verifyProductFileStorageIdentity(event.App, fsys, record); err != nil {
		return apiError(event, http.StatusNotFound, errorCodeFileNotFound, "File not found.")
	}
	if err := recordAdministratorContentEvent(event.App, workspace.ID, actorID, administratorContentDownloadFile,
		"file", record.Id, record.GetString("original_name")); err != nil {
		return administratorContentError(event, err, "record")
	}
	event.Response.Header().Set("Content-Type", record.GetString("content_type"))
	event.Response.Header().Set("X-Content-Type-Options", "nosniff")
	sandboxStoredFileDownload(event.Response.Header(), record.GetString("content_type"))
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Referrer-Policy", "no-referrer")
	return serveNamedFile(fsys, event.Response, event.Request, record.BaseFilesPath()+"/"+record.GetString("blob"),
		productFileResponseName(event.Request, record.GetString("original_name")))
}

// deleteAdministratorWorkspaceFile deletes a file no page uses, unless a
// legal hold keeps it.
func deleteAdministratorWorkspaceFile(event *core.RequestEvent) error {
	actorID, workspace, ok := administratorContentRequest(event)
	if !ok {
		return nil
	}
	input := administratorContentActionInput{}
	if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.RequestID) == "" ||
		len([]rune(input.RequestID)) > maxFileDeleteRequestIDRunes {
		return fileDeleteError(event, errFileDeleteInvalid, "administrator-validate")
	}
	record, ok := administratorContentFile(event, workspace)
	if !ok {
		return nil
	}
	// A file only pages' version history still keeps leaves that history
	// first, unless a legal hold keeps it, which the deletion then says.
	held, err := administratorFileHeld(event.App, workspace, record.GetString("created_by"))
	if err != nil {
		return administratorContentError(event, err, "file-hold")
	}
	// A key's call asks its person once the file is found (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	if !held {
		if err := event.App.RunInTransaction(func(txApp core.App) error {
			live, err := findLiveProductFileReference(txApp, record.Id)
			if err != nil || live {
				return err
			}
			_, err = txApp.DB().Delete("leafen_file_references", dbx.HashExp{
				"file_id": record.Id, "owner_kind": "page_history",
			}).Execute()
			return err
		}); err != nil {
			return administratorContentError(event, err, "file-history")
		}
	}
	if _, stage, err := deleteProductFileAs(event.Request.Context(), event.App, workspace.PersonID, record.Id,
		"server-admin:"+strings.TrimSpace(input.RequestID)); err != nil {
		return fileDeleteError(event, err, "administrator-"+stage)
	}
	if err := recordAdministratorContentEvent(event.App, workspace.ID, actorID, administratorContentDeleteFile,
		"file", record.Id, record.GetString("original_name")); err != nil {
		return administratorContentError(event, err, "record")
	}
	return event.JSON(http.StatusOK, administratorWorkspaceActionResult{Status: "deleted"})
}
