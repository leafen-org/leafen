package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/reearth/ygo/crdt"
)

// A file block's file is saved from the app that opened it (document apps
// decision of 2026-10-10, 2, 3 and 11). Nobody holds a file: whoever may
// change the page edits it, and Leafen tells each of them who else has it
// open, which holds nothing up. A save puts the new file in the block only
// while the block still holds the version that person opened; over a newer
// one the person chooses, outside the app, to save over it, to keep theirs
// as a copy beside it, or to open the latest. The file a save replaces stays
// in the page's history (native_page_history_file_references), from where
// the page's history brings it back.

const (
	pageFileOpenDuration          = 90 * time.Second
	errorCodePageFileVersionStale = "LEAFEN_FILE_VERSION_CONFLICT"
	errorCodePageFileSaveInvalid  = "LEAFEN_FILE_SAVE_INVALID"
)

// pageFileOpeners are the people who have a file open, by block, each until
// their screen stops saying so.
var pageFileOpeners = struct {
	sync.Mutex
	byBlock map[string]map[string]pageFileOpener
}{byBlock: map[string]map[string]pageFileOpener{}}

type pageFileOpener struct {
	Name      string
	ExpiresAt int64
}

type pageFileSaveInput struct {
	FileID      string `json:"fileId"`
	BaseVersion string `json:"baseVersion"`
}

type pageFileCopyInput struct {
	FileID string `json:"fileId"`
	// The copy's name in the page; the file's own when empty.
	Name string `json:"name"`
	// The copy's name when that one is in use on the page, "{n}" for its
	// number from 2 (2026-11-07: a second conflict copy took the first's
	// name, and the page held two of one name). Words are the caller's.
	NumberedName string `json:"numberedName"`
}

type pageFileVersion struct {
	BlockID     string `json:"blockId"`
	FileID      string `json:"fileId"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	Version     string `json:"version"`
	// Who saved this version, when it is newer than the one a save named.
	SavedBy string `json:"savedBy,omitempty"`
}

func registerPageFileSaveRoutes(event *core.ServeEvent) {
	limit := apis.BodyLimit(4096)
	event.Router.PUT(apiRoot+"/pages/{pageId}/blocks/{blockId}/file", savePageBlockFileRoute).Bind(limit)
	event.Router.PUT(apiRoot+"/pages/{pageId}/blocks/{blockId}/file-attach", attachPageBlockFileRoute).Bind(limit)
	event.Router.POST(apiRoot+"/pages/{pageId}/blocks/{blockId}/file-copy", copyPageBlockFileRoute).Bind(limit)
	event.Router.PUT(apiRoot+"/pages/{pageId}/blocks/{blockId}/file-open", openPageBlockFileRoute).Bind(limit)
	event.Router.DELETE(apiRoot+"/pages/{pageId}/blocks/{blockId}/file-open", closePageBlockFileRoute).Bind(limit)
	event.Router.POST(apiRoot+"/pages/{pageId}/blocks/{blockId}/add-to-page", addPageBlockFileToPageRoute).Bind(limit)
}

// pageFileBlock is a block of the page that holds a stored file, for a
// person who may do what requirement asks of the page; ok false when it
// answered instead.
func pageFileBlock(event *core.RequestEvent, requirement permissionRequirement) (string, storedNotionBlock, graphFileBlockContent, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return "", storedNotionBlock{}, graphFileBlockContent{}, false, apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID, blockID := strings.TrimSpace(event.Request.PathValue("pageId")), strings.TrimSpace(event.Request.PathValue("blockId"))
	if _, err := authorizePageBody(event.App, user.Id, pageID, requirement); err != nil {
		return "", storedNotionBlock{}, graphFileBlockContent{}, false, apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	// A block by its own id, or by the id the Notion-compatible API shows.
	if resolved, ok := resolveNotionBlockID(event.App, blockID); ok {
		blockID = resolved
	}
	block, found, err := loadStoredNotionBlock(event.App, blockID)
	if err != nil {
		return "", storedNotionBlock{}, graphFileBlockContent{}, false, apiErrorFrom(event, http.StatusInternalServerError, errorCodePageFileSaveInvalid, "The file couldn't be read.", err)
	}
	_, managed := graphManagedFileBlockTypes[block.Type]
	content, parseErr := parseGraphFileBlockContent(json.RawMessage(block.Content))
	if !found || block.PageID != pageID || !managed || parseErr != nil || content.FileID == "" {
		return "", storedNotionBlock{}, graphFileBlockContent{}, false, apiError(event, http.StatusNotFound, errorCodePageFileSaveInvalid, "No stored file in that block.")
	}
	return user.Id, block, content, true, nil
}

// openPageBlockFileRoute says a person has a file open, again while they
// do, and answers who else has: names the page's readers see anyway, as
// they see each other there.
func openPageBlockFileRoute(event *core.RequestEvent) error {
	personID, block, content, ok, err := pageFileBlock(event, permissionWorkspaceRead)
	if !ok {
		return err
	}
	name := ""
	_ = event.App.DB().NewQuery(`SELECT [[name]] FROM {{users}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": personID}).Row(&name)
	now := time.Now()
	pageFileOpeners.Lock()
	defer pageFileOpeners.Unlock()
	openers := pageFileOpeners.byBlock[block.ID]
	if openers == nil {
		openers = map[string]pageFileOpener{}
		pageFileOpeners.byBlock[block.ID] = openers
	}
	openers[personID] = pageFileOpener{Name: name, ExpiresAt: now.Add(pageFileOpenDuration).UnixMilli()}
	others := []map[string]string{}
	for id, opener := range openers {
		if opener.ExpiresAt <= now.UnixMilli() {
			delete(openers, id)
		} else if id != personID {
			others = append(others, map[string]string{"name": opener.Name})
		}
	}
	return event.JSON(http.StatusOK, map[string]any{"others": others, "current": pageFileVersionOf(block.ID, content)})
}

// closePageBlockFileRoute says a person closed a file. A screen that goes
// without saying so drops out when its last word runs out.
func closePageBlockFileRoute(event *core.RequestEvent) error {
	personID, block, _, ok, err := pageFileBlock(event, permissionWorkspaceRead)
	if !ok {
		return err
	}
	pageFileOpeners.Lock()
	defer pageFileOpeners.Unlock()
	if openers := pageFileOpeners.byBlock[block.ID]; openers != nil {
		delete(openers, personID)
		if len(openers) == 0 {
			delete(pageFileOpeners.byBlock, block.ID)
		}
	}
	return event.NoContent(http.StatusNoContent)
}

// pageFileUploaded is a file this person uploaded to the block's workspace;
// ok false when it answered instead.
func pageFileUploaded(event *core.RequestEvent, personID string, block storedNotionBlock, fileID string) (*core.Record, bool, error) {
	record, err := event.App.FindRecordById(filesCollection, strings.TrimSpace(fileID))
	if err != nil || record.GetString("workspace") != block.WorkspaceID || record.GetString("created_by") != personID ||
		record.GetString("sha256") == productFileUploadPendingDigest {
		return nil, false, apiError(event, http.StatusBadRequest, errorCodePageFileSaveInvalid, "Upload the file to this workspace first.")
	}
	return record, true, nil
}

// copyPageBlockFileRoute keeps an uploaded file as a new file block right
// after the block, as a person's copy when someone saved first.
func copyPageBlockFileRoute(event *core.RequestEvent) error {
	personID, block, _, ok, err := pageFileBlock(event, permissionWorkspaceWrite)
	if !ok {
		return err
	}
	input := pageFileCopyInput{}
	if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.FileID) == "" || len(input.Name) > 1024 || len(input.NumberedName) > 1024 {
		return apiError(event, http.StatusBadRequest, errorCodePageFileSaveInvalid, "Give the uploaded file to keep.")
	}
	record, ok, err := pageFileUploaded(event, personID, block, input.FileID)
	if !ok {
		return err
	}
	copied, blockID, err := insertPageFileCopy(event.Request.Context(), event.App, personID, block, record, strings.TrimSpace(input.Name), strings.TrimSpace(input.NumberedName))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePageFileSaveInvalid, "The copy couldn't be kept here.")
	}
	// An opened-file app keeps saving to the copy it kept.
	if authority, ok := userKeyOf(event); ok {
		if err := appOpenedFileKeep(event.App, authority, blockID); err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageFileSaveInvalid, "The copy was kept but can't be saved to yet.", err)
		}
	}
	return event.JSON(http.StatusOK, pageFileVersionOf(blockID, copied))
}

// insertPageFileCopy puts a file in a new block of the same kind right after
// a file block, named as given or as the file — numbered when another file
// on the page has that name and a numbered name is given.
func insertPageFileCopy(
	ctx context.Context, app core.App, actorID string, block storedNotionBlock, record *core.Record, name, numbered string,
) (graphFileBlockContent, string, error) {
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return graphFileBlockContent{}, "", errors.New(errorCodePageFileSaveInvalid)
	}
	content := canonicalGraphFileContent(record)
	if name != "" {
		content.Name = name
	}
	blockID := uuid.NewString()
	_, err := persistence.applyContentGraphMutationContext(ctx, actorID, block.PageID,
		graphMutationInput{MutationID: "file-copy:" + security.RandomString(20)},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				return authorizePageBody(txApp, actorID, block.PageID, permissionWorkspaceWrite)
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, found, err := loadStoredNotionBlock(txApp, block.ID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				if !found || current.PageID != block.PageID {
					return contentGraphMutationPreparation{}, errors.New(errorCodePageFileSaveInvalid)
				}
				if !notionManagedFileContentCompatible(current.Type, content) {
					return contentGraphMutationPreparation{}, fmt.Errorf("%s: a %s block can't hold a %s file", errorCodePageFileSaveInvalid, current.Type, content.ContentType)
				}
				position, err := pageBlockPositionAfter(txApp, current)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				if content.Name, err = pageFileFreeName(txApp, block.PageID, content.Name, numbered); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				encoded, err := json.Marshal(content)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				var parent *string
				if current.ParentID != "" {
					parent = &current.ParentID
				}
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind:  graphOperationKindUpsertBlock,
					Block: &graphBlock{ID: blockID, ParentID: parent, Position: position, Type: current.Type, Content: encoded},
				}}}, nil
			},
		},
	)
	return content, blockID, err
}

// pageFileFreeName is a name no other file on the page has: the name itself,
// else the numbered one with the first free number from 2 ("{n}" in it).
// Without a numbered name the name stands.
func pageFileFreeName(app core.App, pageID, name, numbered string) (string, error) {
	if name == "" || !strings.Contains(numbered, "{n}") {
		return name, nil
	}
	rows := []struct {
		Name string `db:"name"`
	}{}
	err := app.DB().NewQuery(`
		SELECT COALESCE(json_extract([[content]], '$.name'), '') AS [[name]] FROM {{leafen_blocks}}
		WHERE [[page_id]] = {:page} AND json_extract([[content]], '$.fileId') IS NOT NULL
	`).Bind(dbx.Params{"page": pageID}).All(&rows)
	if err != nil {
		return "", err
	}
	taken := make(map[string]bool, len(rows))
	for _, row := range rows {
		taken[strings.ToLower(row.Name)] = true
	}
	if !taken[strings.ToLower(name)] {
		return name, nil
	}
	for n := 2; n < 10000; n++ {
		next := strings.ReplaceAll(numbered, "{n}", strconv.Itoa(n))
		if !taken[strings.ToLower(next)] {
			return next, nil
		}
	}
	return name, nil
}

// pageBlockPositionAfter is an order key between a block and the sibling
// after it, or after it when it is the last.
func pageBlockPositionAfter(app core.App, block storedNotionBlock) (string, error) {
	next := ""
	err := app.DB().NewQuery(`
		SELECT COALESCE(MIN([[position]]), '') FROM {{leafen_blocks}}
		WHERE [[page_id]] = {:page} AND COALESCE([[parent_id]], '') = {:parent} AND [[position]] > {:position}
	`).Bind(dbx.Params{"page": block.PageID, "parent": block.ParentID, "position": block.Position}).Row(&next)
	if err != nil {
		return "", err
	}
	return databasePositionBetween(block.Position, next)
}

// savePageBlockFileRoute puts an uploaded file in a block in place of the
// version named, unless someone saved since.
func savePageBlockFileRoute(event *core.RequestEvent) error {
	personID, block, _, ok, err := pageFileBlock(event, permissionWorkspaceWrite)
	if !ok {
		return err
	}
	input := pageFileSaveInput{}
	if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.FileID) == "" || strings.TrimSpace(input.BaseVersion) == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageFileSaveInvalid, "Give the new file and the version you opened.")
	}
	record, ok, err := pageFileUploaded(event, personID, block, input.FileID)
	if !ok {
		return err
	}
	persistence, ok := event.App.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return apiError(event, http.StatusInternalServerError, errorCodePageFileSaveInvalid, "The file couldn't be saved.")
	}
	var saved graphFileBlockContent
	var stale *graphFileBlockContent
	_, err = persistence.applyContentGraphMutationContext(
		event.Request.Context(), personID, block.PageID,
		graphMutationInput{MutationID: "file-save:" + security.RandomString(20)},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				return authorizePageBody(txApp, personID, block.PageID, permissionWorkspaceWrite)
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, found, err := loadStoredNotionBlock(txApp, block.ID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				if !found || current.PageID != block.PageID || current.Type != block.Type {
					return contentGraphMutationPreparation{}, errors.New(errorCodePageFileSaveInvalid)
				}
				content, err := parseGraphFileBlockContent(json.RawMessage(current.Content))
				if err != nil || content.FileID == "" {
					return contentGraphMutationPreparation{}, errors.New(errorCodePageFileSaveInvalid)
				}
				if mcpFileBaseVersion(content) != strings.TrimSpace(input.BaseVersion) {
					stale = &content
					return contentGraphMutationPreparation{}, errors.New(errorCodePageFileVersionStale)
				}
				next := canonicalGraphFileContent(record)
				content.FileID, content.Name, content.Size = next.FileID, next.Name, next.Size
				content.ContentType, content.SHA256 = next.ContentType, next.SHA256
				if !notionManagedFileContentCompatible(current.Type, content) {
					return contentGraphMutationPreparation{}, fmt.Errorf("%s: a %s block can't hold a %s file", errorCodePageFileSaveInvalid, current.Type, content.ContentType)
				}
				encoded, err := json.Marshal(content)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				var parent *string
				if current.ParentID != "" {
					parent = &current.ParentID
				}
				saved = content
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind:  graphOperationKindUpsertBlock,
					Block: &graphBlock{ID: current.ID, ParentID: parent, Position: current.Position, Type: current.Type, Content: encoded},
				}}}, nil
			},
		},
	)
	if stale != nil {
		current := pageFileVersionOf(block.ID, *stale)
		current.SavedBy = pageFileSavedBy(event.App, stale.FileID)
		return event.JSON(http.StatusConflict, map[string]any{
			"error":   map[string]string{"code": errorCodePageFileVersionStale, "message": "Someone saved the file since you opened it."},
			"current": current,
		})
	}
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePageFileSaveInvalid, "The file couldn't be saved here.")
	}
	return event.JSON(http.StatusOK, pageFileVersionOf(block.ID, saved))
}

func pageFileVersionOf(blockID string, content graphFileBlockContent) pageFileVersion {
	return pageFileVersion{BlockID: blockID, FileID: content.FileID, Name: content.Name, ContentType: content.ContentType,
		Size: content.Size, Version: mcpFileBaseVersion(content)}
}

// pageFileSavedBy names who uploaded a file, as the page's people see them.
func pageFileSavedBy(app core.App, fileID string) string {
	name := ""
	_ = app.DB().NewQuery(`
		SELECT COALESCE([[users]].[[name]], '') FROM {{leafen_files}} AS [[file]]
		JOIN {{users}} AS [[users]] ON [[users]].[[id]] = [[file]].[[created_by]]
		WHERE [[file]].[[id]] = {:id}
	`).Bind(dbx.Params{"id": fileID}).Row(&name)
	return name
}
