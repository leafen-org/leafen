package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// "Opened file only" (personAccess "openedFile", script-eval decision of
// 2026-10-10): the narrowest reach an app may ask for. Its screen, opened on
// a file from the file's block, gets a token held to that file: it reads the
// block's file, uploads, and saves to that block or to a copy it keeps
// beside it — nothing else of the person's — and its own storage. Elsewhere
// the app reaches no data of the person's. An injected script in an editor
// with such a token stops at the document it opened.

const appOpenedFileAccess = "openedFile"

// appConnectionScope is the scope an app's connection keeps: an opened-file
// app's is content, and each of its tokens holds it to the opened file.
func appConnectionScope(manifest appManifest) string {
	if manifest.PersonAccess == appOpenedFileAccess {
		return userKeyScopeNames[userKeyScopeContent]
	}
	return manifest.PersonAccess
}

// appOpenedFile is the file block a screen opened on, as the shell names it.
type appOpenedFile struct {
	PageID  string
	BlockID string
}

// appOpenedFileBlock checks that the person may read the page and that the
// block on it holds a stored file, answering the block's own id.
func appOpenedFileBlock(app core.App, userID string, opened appOpenedFile) (string, bool) {
	if _, err := authorizePageBody(app, userID, opened.PageID, permissionWorkspaceRead); err != nil {
		return "", false
	}
	blockID := opened.BlockID
	if resolved, ok := resolveNotionBlockID(app, blockID); ok {
		blockID = resolved
	}
	block, found, err := loadStoredNotionBlock(app, blockID)
	if err != nil || !found || block.PageID != opened.PageID {
		return "", false
	}
	if _, managed := graphManagedFileBlockTypes[block.Type]; !managed {
		return "", false
	}
	content, err := parseGraphFileBlockContent(json.RawMessage(block.Content))
	if err != nil || content.FileID == "" {
		return "", false
	}
	return block.ID, true
}

// appOpenedFileKeep adds a block — a copy the screen kept — to what its
// token reaches.
func appOpenedFileKeep(app core.App, authority userKeyAuthority, blockID string) error {
	if authority.DelegationHash == "" || authority.OpenedPageID == "" {
		return nil
	}
	blocks := append(slices.Clone(authority.OpenedBlockIDs), blockID)
	encoded, err := json.Marshal(blocks)
	if err != nil {
		return err
	}
	_, err = app.DB().Update(appDelegationsTable, dbx.Params{"file_block_ids": string(encoded)},
		dbx.HashExp{"token_hash": authority.DelegationHash}).Execute()
	return err
}

var (
	appOpenedFileRead    = regexp.MustCompile(`^/files/([^/]+)$`)
	appOpenedFileUpload  = regexp.MustCompile(`^/files/uploads(/[^/]+(/parts/[^/]+|/complete)?)?$`)
	appOpenedFileSave    = regexp.MustCompile(`^/pages/([^/]+)/blocks/([^/]+)/(file|file-copy)$`)
	appOpenedFileStorage = regexp.MustCompile(`^/apps/([^/]+)/storage(/.*)?$`)
)

var errAppOpenedFileOutside = errors.New("an opened-file app reaches only the file it opened")

// appOpenedFileAllows says whether a call with an opened-file token stays
// within the file it opened.
func appOpenedFileAllows(app core.App, request *http.Request, authority userKeyAuthority, path string, body []byte) error {
	method := request.Method
	if match := appOpenedFileStorage.FindStringSubmatch(path); path == "/app-storage" || match != nil && match[1] == authority.AppInstallID {
		return nil
	}
	if match := appOpenedFileSave.FindStringSubmatch(path); match != nil {
		if (match[3] == "file" && method != http.MethodPut) || (match[3] == "file-copy" && method != http.MethodPost) {
			return errAppOpenedFileOutside
		}
		blockID := match[2]
		if resolved, ok := resolveNotionBlockID(app, blockID); ok {
			blockID = resolved
		}
		if match[1] != authority.OpenedPageID || !slices.Contains(authority.OpenedBlockIDs, blockID) {
			return errAppOpenedFileOutside
		}
		return nil
	}
	if appOpenedFileUpload.MatchString(path) && method != http.MethodGet {
		// An upload goes to the opened page's workspace.
		if path == "/files/uploads" {
			// Read as the handler reads it, every spelling of the field.
			fields := readUserKeyBodyFields(request.Header.Get("Content-Type"), body)
			workspaceID, single := fields.one("workspaceId")
			if !fields.readable || !single || workspaceID != appOpenedFileWorkspace(app, authority.OpenedPageID) {
				return errAppOpenedFileOutside
			}
		}
		return nil
	}
	if match := appOpenedFileRead.FindStringSubmatch(path); match != nil && method == http.MethodGet && match[1] != "uploads" {
		// The file a kept block holds now.
		blocks := make([]any, len(authority.OpenedBlockIDs))
		for index, blockID := range authority.OpenedBlockIDs {
			blocks[index] = blockID
		}
		var held int
		if err := app.DB().Select("COUNT(*)").From("leafen_blocks").
			Where(dbx.HashExp{"page_id": authority.OpenedPageID}).AndWhere(dbx.In("id", blocks...)).
			AndWhere(dbx.NewExp("json_extract([[content]], '$.fileId') = {:file}", dbx.Params{"file": match[1]})).
			Row(&held); err != nil || held == 0 {
			return refusedUnlessStorage(errAppOpenedFileOutside, err)
		}
		return nil
	}
	return errAppOpenedFileOutside
}

// appOpenedFileWorkspace is the workspace of the page a file was opened on.
func appOpenedFileWorkspace(app core.App, pageID string) string {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return ""
	}
	return page.GetString("workspace")
}
