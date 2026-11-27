package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	customEmojiCollectionPath    = "/workspaces/{workspaceId}/custom-emojis"
	customEmojiItemPath          = customEmojiCollectionPath + "/{emojiId}"
	customEmojiByIDPath          = "/custom-emojis/{emojiId}"
	customEmojiDefaultPageSize   = 50
	customEmojiMaximumPageSize   = 100
	customEmojiMaximumNameRunes  = 100
	customEmojiMutationBodyBytes = 16 << 10

	errorCodeCustomEmojiInvalid        = "LEAFEN_CUSTOM_EMOJI_INVALID"
	errorCodeCustomEmojiNotFound       = "LEAFEN_CUSTOM_EMOJI_NOT_FOUND"
	errorCodeCustomEmojiConflict       = "LEAFEN_CUSTOM_EMOJI_CONFLICT"
	errorCodeCustomEmojiNameConflict   = "LEAFEN_CUSTOM_EMOJI_NAME_CONFLICT"
	errorCodeCustomEmojiReadFailed     = "LEAFEN_CUSTOM_EMOJI_READ_FAILED"
	errorCodeCustomEmojiMutationFailed = "LEAFEN_CUSTOM_EMOJI_MUTATION_FAILED"
)

var (
	errCustomEmojiInvalid      = errors.New(errorCodeCustomEmojiInvalid)
	errCustomEmojiNotFound     = errors.New(errorCodeCustomEmojiNotFound)
	errCustomEmojiConflict     = errors.New(errorCodeCustomEmojiConflict)
	errCustomEmojiNameConflict = errors.New(errorCodeCustomEmojiNameConflict)
)

type storedCustomEmoji struct {
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
	Name        string `db:"name"`
	NameKey     string `db:"name_key"`
	FileID      string `db:"file_id"`
	CreatedBy   string `db:"created_by"`
	Revision    int    `db:"revision"`
	CreatedAt   int64  `db:"created_at"`
	UpdatedAt   int64  `db:"updated_at"`
	ContentType string `db:"content_type"`
	CreatorName string `db:"creator_name"`
}

type customEmojiResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	FileID      string `json:"fileId"`
	URL         string `json:"url"`
	Revision    int    `json:"revision"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	CreatedBy   string `json:"createdBy"`
	// In the workspace's list only: who added it, and whether the reader may change it.
	CreatorName string `json:"creatorName,omitempty"`
	CanChange   bool   `json:"canChange,omitempty"`
	Duplicate   bool   `json:"duplicate,omitempty"`
	// Deleted, read with ?include=deleted: its last name (custom_emoji_tombstone.go).
	Deleted bool `json:"deleted,omitempty"`
}

type customEmojiCreateInput struct {
	Name      string `json:"name"`
	FileID    string `json:"fileId"`
	RequestID string `json:"requestId"`
}

type customEmojiRenameInput struct {
	Name         string `json:"name"`
	BaseRevision int    `json:"baseRevision"`
	// A new picture, as Notion's "편집" replaces it; empty keeps the one it has.
	FileID string `json:"fileId,omitempty"`
}

type customEmojiDeleteInput struct {
	BaseRevision int `json:"baseRevision"`
}

type customEmojiDeleteResponse struct {
	ID      string `json:"id"`
	FileID  string `json:"fileId"`
	Deleted bool   `json:"deleted"`
}

func registerCustomEmojiRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+customEmojiCollectionPath, listCustomEmojis)
	event.Router.GET(apiRoot+customEmojiItemPath, getCustomEmoji)
	event.Router.GET(apiRoot+customEmojiByIDPath, getCustomEmojiByID)
	event.Router.POST(apiRoot+customEmojiCollectionPath, createCustomEmoji).
		Bind(apis.BodyLimit(customEmojiMutationBodyBytes))
	event.Router.PATCH(apiRoot+customEmojiItemPath, renameCustomEmoji).
		Bind(apis.BodyLimit(customEmojiMutationBodyBytes))
	event.Router.DELETE(apiRoot+customEmojiItemPath, deleteCustomEmoji).
		Bind(apis.BodyLimit(customEmojiMutationBodyBytes))
}

func createCustomEmoji(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := customEmojiCreateInput{}
	if err := event.BindBody(&input); err != nil {
		return customEmojiError(event, bodyRefusal(errCustomEmojiInvalid, "custom emoji", err), "create-bind")
	}
	name, nameKey, err := normalizeCustomEmojiName(input.Name)
	input.FileID = strings.TrimSpace(input.FileID)
	input.RequestID = strings.TrimSpace(input.RequestID)
	switch {
	case err != nil:
	case workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength:
		err = customEmojiRefused("the workspace ID in the path is 1–%d characters", maxWorkspacePermissionIdentifierLength)
	case !workspaceExportFileRecordIDPattern.MatchString(input.FileID):
		err = errCustomEmojiFileID
	case input.RequestID == "" || len([]rune(input.RequestID)) > fileTransferRequestIDChars:
		err = customEmojiRefused("requestId is a new id of yours, 1–%d characters, so the same emoji sent again isn't made twice", fileTransferRequestIDChars)
	}
	if err != nil {
		return customEmojiError(event, err, "create-validate")
	}

	emojiID := customEmojiID(workspaceID, user.Id, input.RequestID)
	result := storedCustomEmoji{}
	duplicate := false
	err = event.App.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		// The workspace may keep new emoji to its owners.
		if err := requireWorkspacePolicy(txApp, workspaceID, user.Id, authorization.Role, customEmojiLimit, errCustomEmojiForbidden); err != nil {
			return err
		}
		file, _, err := authorizeWorkspaceFile(
			txApp, user.Id, input.FileID, permissionWorkspaceWrite,
		)
		if err != nil || file.GetString("workspace") != workspaceID {
			if err != nil {
				return err
			}
			return permissionDenied()
		}
		if !strings.HasPrefix(strings.ToLower(file.GetString("content_type")), "image/") {
			return errCustomEmojiNotImage
		}
		current, err := loadCustomEmojiByID(txApp, workspaceID, emojiID)
		if err == nil {
			if current.Name != name || current.FileID != input.FileID || current.CreatedBy != user.Id {
				return errCustomEmojiConflict
			}
			result, duplicate = current, true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		available, err := customEmojiNameAvailable(txApp, workspaceID, nameKey, "")
		if err != nil {
			return err
		}
		if !available {
			return errCustomEmojiNameConflict
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().Insert(customEmojisTable, dbx.Params{
			"id": emojiID, "workspace_id": workspaceID, "name": name, "name_key": nameKey,
			"file_id": input.FileID, "created_by": user.Id, "revision": 1,
			"created_at": now, "updated_at": now,
		}).Execute(); err != nil {
			return err
		}
		if err := reconcileProductFileReferenceLifecycle(txApp, workspaceID, []string{input.FileID}); err != nil {
			return err
		}
		result, err = loadCustomEmojiByID(txApp, workspaceID, emojiID)
		return err
	})
	if err != nil {
		return customEmojiError(event, err, "create")
	}
	response := nativeCustomEmojiResponse(result, duplicate)
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func renameCustomEmoji(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	emojiID := strings.TrimSpace(event.Request.PathValue("emojiId"))
	input := customEmojiRenameInput{}
	if err := event.BindBody(&input); err != nil {
		return customEmojiError(event, bodyRefusal(errCustomEmojiInvalid, "custom emoji change", err), "rename-bind")
	}
	name, nameKey, err := normalizeCustomEmojiName(input.Name)
	input.FileID = strings.TrimSpace(input.FileID)
	switch {
	case err != nil:
	case !validCustomEmojiID(emojiID):
		err = errCustomEmojiIDRefused
	case input.BaseRevision < 1:
		err = errCustomEmojiRevision
	case input.FileID != "" && !workspaceExportFileRecordIDPattern.MatchString(input.FileID):
		err = errCustomEmojiFileID
	}
	if err != nil {
		return customEmojiError(event, err, "rename-validate")
	}
	result := storedCustomEmoji{}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		current, err := loadCustomEmojiByID(txApp, workspaceID, emojiID)
		if errors.Is(err, sql.ErrNoRows) {
			return errCustomEmojiNotFound
		}
		if err != nil {
			return err
		}
		if err := authorizeCustomEmojiChange(authorization, current); err != nil {
			return err
		}
		fileID := current.FileID
		if input.FileID != "" {
			fileID = input.FileID
		}
		if current.Revision != input.BaseRevision {
			if current.Name == name && current.FileID == fileID {
				result = current
				return nil
			}
			return errCustomEmojiConflict
		}
		// A new picture is checked as a new emoji's is: an image the writer may
		// use in this workspace.
		if fileID != current.FileID {
			file, _, err := authorizeWorkspaceFile(txApp, user.Id, fileID, permissionWorkspaceWrite)
			if err != nil {
				return err
			}
			if file.GetString("workspace") != workspaceID {
				return permissionDenied()
			}
			if !strings.HasPrefix(strings.ToLower(file.GetString("content_type")), "image/") {
				return errCustomEmojiNotImage
			}
		}
		available, err := customEmojiNameAvailable(txApp, workspaceID, nameKey, emojiID)
		if err != nil {
			return err
		}
		if !available {
			return errCustomEmojiNameConflict
		}
		now := time.Now().UTC().UnixMilli()
		updated, err := txApp.DB().Update(customEmojisTable, dbx.Params{
			"name": name, "name_key": nameKey, "file_id": fileID, "revision": current.Revision + 1,
			"updated_at": now,
		}, dbx.HashExp{
			"workspace_id": workspaceID, "id": emojiID, "revision": current.Revision,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := updated.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errCustomEmojiConflict, err)
		}
		if fileID != current.FileID {
			if err := reconcileProductFileReferenceLifecycle(txApp, workspaceID, []string{current.FileID, fileID}); err != nil {
				return err
			}
		}
		result, err = loadCustomEmojiByID(txApp, workspaceID, emojiID)
		return err
	})
	if err != nil {
		return customEmojiError(event, err, "rename")
	}
	return event.JSON(http.StatusOK, nativeCustomEmojiResponse(result, false))
}

func deleteCustomEmoji(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	emojiID := strings.TrimSpace(event.Request.PathValue("emojiId"))
	input := customEmojiDeleteInput{}
	if err := event.BindBody(&input); err != nil {
		return customEmojiError(event, bodyRefusal(errCustomEmojiInvalid, "custom emoji removal", err), "delete-validate")
	}
	if !validCustomEmojiID(emojiID) {
		return customEmojiError(event, errCustomEmojiIDRefused, "delete-validate")
	}
	if input.BaseRevision < 1 {
		return customEmojiError(event, errCustomEmojiRevision, "delete-validate")
	}
	deleted := storedCustomEmoji{}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		current, err := loadCustomEmojiByID(txApp, workspaceID, emojiID)
		if errors.Is(err, sql.ErrNoRows) {
			return errCustomEmojiNotFound
		}
		if err != nil {
			return err
		}
		if err := authorizeCustomEmojiChange(authorization, current); err != nil {
			return err
		}
		if current.Revision != input.BaseRevision {
			return errCustomEmojiConflict
		}
		result, err := txApp.DB().Delete(customEmojisTable, dbx.HashExp{
			"workspace_id": workspaceID, "id": emojiID, "revision": current.Revision,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errCustomEmojiConflict, err)
		}
		if err := reconcileProductFileReferenceLifecycle(txApp, workspaceID, []string{current.FileID}); err != nil {
			return err
		}
		if err := recordCustomEmojiTombstone(txApp, current); err != nil {
			return err
		}
		deleted = current
		return nil
	})
	if err != nil {
		return customEmojiError(event, err, "delete")
	}
	return event.JSON(http.StatusOK, customEmojiDeleteResponse{
		ID: deleted.ID, FileID: deleted.FileID, Deleted: true,
	})
}

// loadCustomEmojiPage reads a page of a workspace's emoji in a read
// snapshot of its own (read_snapshot.go): it writes nothing.
func loadCustomEmojiPage(
	ctx context.Context,
	app core.App,
	principalID, workspaceID, afterID, name string,
	including bool, limit int,
) ([]storedCustomEmoji, error) {
	rows := []storedCustomEmoji{}
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		if _, err := authorizeWorkspace(txApp, principalID, workspaceID, permissionWorkspaceRead); err != nil {
			return err
		}
		return txApp.DB().NewQuery(`
			SELECT [[emoji]].[[id]],[[emoji]].[[workspace_id]],[[emoji]].[[name]],
				[[emoji]].[[name_key]],[[emoji]].[[file_id]],[[emoji]].[[created_by]],
				[[emoji]].[[revision]],[[emoji]].[[created_at]],[[emoji]].[[updated_at]],
				[[file]].[[content_type]], COALESCE([[creator]].[[name]], '') AS [[creator_name]]
			FROM {{leafen_custom_emojis}} AS [[emoji]]
			INDEXED BY [[idx_leafen_custom_emojis_workspace_id]]
			LEFT JOIN {{users}} AS [[creator]] ON [[creator]].[[id]]=[[emoji]].[[created_by]]
			JOIN {{leafen_files}} AS [[file]] ON [[file]].[[id]]=[[emoji]].[[file_id]]
				AND [[file]].[[workspace]]=[[emoji]].[[workspace_id]]
				AND [[file]].[[authority_state]]={:authority} AND [[file]].[[created_by]]!=''
			WHERE [[emoji]].[[workspace_id]]={:workspaceId} AND [[emoji]].[[id]]` + notionCursorComparison(including) + `{:afterId}
				AND [[emoji]].[[workspace_import_job_id]]='' AND [[emoji]].[[carried]]=0
				AND ({:name}='' OR [[emoji]].[[name]]={:name})
				AND NOT EXISTS (SELECT 1 FROM {{leafen_file_delete_receipts}} AS [[deletion]]
					WHERE [[deletion]].[[file_id]]=[[emoji]].[[file_id]]
						AND [[deletion]].[[status]]={:deletePending})
				AND NOT EXISTS (SELECT 1 FROM {{leafen_file_uploads}} AS [[upload]]
					WHERE [[upload]].[[file_id]]=[[emoji]].[[file_id]]
						AND [[upload]].[[status]]!={:uploadCompleted})
			ORDER BY [[emoji]].[[id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"workspaceId": workspaceID, "afterId": afterID, "name": name, "limit": limit,
			"authority": fileAuthorityWorkspace, "deletePending": fileDeleteStatusPending,
			"uploadCompleted": fileUploadStatusCompleted,
		}).All(&rows)
	})
	return rows, err
}

func customEmojiNameAvailable(app core.App, workspaceID, nameKey, exceptID string) (bool, error) {
	var found int
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_custom_emojis}}
		WHERE [[workspace_id]]={:workspaceId} AND [[name_key]]={:nameKey}
			AND [[id]]!={:exceptId} LIMIT 1)
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "nameKey": nameKey, "exceptId": exceptID,
	}).Row(&found)
	return found == 0, err
}

// customEmojiRefused refuses a custom emoji request for the reason given
// (request_refusal.go).
func customEmojiRefused(format string, args ...any) error {
	return refuse(errCustomEmojiInvalid, format, args...)
}

// Refusals more than one custom emoji route says.
var (
	errCustomEmojiIDRefused = errors.Join(errCustomEmojiNotFound) // an ID no custom emoji can have names none
	errCustomEmojiRevision  = customEmojiRefused("baseRevision is the emoji's revision as it was read, 1 or more")
	errCustomEmojiFileID    = customEmojiRefused("fileId names an uploaded image file of the workspace")
	errCustomEmojiNotImage  = customEmojiRefused("fileId names a file that isn't an image")
)

func normalizeCustomEmojiName(value string) (string, string, error) {
	name := norm.NFC.String(strings.TrimSpace(value))
	if name == "" || len([]rune(name)) > customEmojiMaximumNameRunes {
		return "", "", customEmojiRefused("name is 1–%d characters", customEmojiMaximumNameRunes)
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return "", "", customEmojiRefused("name holds a control character")
		}
	}
	return name, cases.Fold().String(name), nil
}

func customEmojiID(workspaceID, actorID, requestID string) string {
	return uuid.NewSHA1(
		uuid.NameSpaceURL,
		[]byte("leafen-custom-emoji-v1\x00"+workspaceID+"\x00"+actorID+"\x00"+requestID),
	).String()
}

func validCustomEmojiID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == strings.ToLower(value)
}

func customEmojiPageSize(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return customEmojiDefaultPageSize, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > customEmojiMaximumPageSize {
		return 0, customEmojiRefused("limit is a number from 1 to %d, not %q", customEmojiMaximumPageSize, raw)
	}
	return value, nil
}

func nativeCustomEmojiResponse(row storedCustomEmoji, duplicate bool) customEmojiResponse {
	path := strings.Replace(fileTransferDownloadPath, "{id}", row.FileID, 1)
	return customEmojiResponse{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Name: row.Name,
		FileID: row.FileID, URL: apiRoot + path, Revision: row.Revision,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, CreatedBy: row.CreatedBy,
		CreatorName: row.CreatorName, Duplicate: duplicate,
	}
}

func customEmojiError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusInternalServerError,
		errorCodeCustomEmojiMutationFailed, "Couldn't save the custom emoji."
	switch {
	case errors.Is(err, errCustomEmojiInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodeCustomEmojiInvalid, refusalMessage("Check the custom emoji.", refusalReason(err))
	case errors.Is(err, errCustomEmojiNotFound), isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodeCustomEmojiNotFound, "Custom emoji not found."
	case errors.Is(err, errCustomEmojiNameConflict):
		status, code, message = http.StatusConflict,
			errorCodeCustomEmojiNameConflict, "A custom emoji with the same name already exists."
	case errors.Is(err, errCustomEmojiForbidden):
		status, code, message = http.StatusForbidden,
			errorCodeCustomEmojiForbidden, "Only whoever added the emoji or the workspace's owner can change it."
	case errors.Is(err, errCustomEmojiConflict):
		status, code, message = http.StatusConflict,
			errorCodeCustomEmojiConflict, "Load the latest custom emoji again."
	}
	if stage == "list" && status == http.StatusInternalServerError {
		code, message = errorCodeCustomEmojiReadFailed, "Couldn't load the custom emoji."
	}
	return writeAPIError(
		event, status, code, message, "custom_emoji", stage, code, err,
		diagnosticSourcePosition(1),
	)
}
