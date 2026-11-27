package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

const (
	fileDeleteMaxBodyBytes = 4096
)

var (
	errFileDeleteInvalid         = errors.New(errorCodeFileDeleteInvalid)
	errFileReferenced            = errors.New(errorCodeFileReferenced)
	errFileDeleteRequestIDReused = errors.New(errorCodeFileDeleteRequestIDReused)
	errFileDeleteUnavailable     = errors.New(errorCodeFileDeleteUnavailable)
)

type storedProductFileDeleteReceipt struct {
	ActorID     string `db:"actor_id"`
	RequestID   string `db:"request_id"`
	RequestHash string `db:"request_hash"`
	WorkspaceID string `db:"workspace_id"`
	FileID      string `db:"file_id"`
	Status      string `db:"status"`
}

type productFileDeleteAdmission struct {
	Receipt storedProductFileDeleteReceipt
	Record  *core.Record
}

func deleteFile(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	fileID := strings.TrimSpace(event.Request.PathValue("id"))
	input := fileDeleteInput{}
	if err := event.BindBody(&input); err != nil {
		return fileDeleteError(event, errFileDeleteInvalid, "bind")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	if fileID == "" || len(fileID) > maxWorkspacePermissionIdentifierLength ||
		input.RequestID == "" || len([]rune(input.RequestID)) > maxFileDeleteRequestIDRunes {
		return fileDeleteError(event, errFileDeleteInvalid, "validate")
	}
	receipt, stage, err := deleteProductFileAs(event.Request.Context(), event.App, user.Id, fileID, input.RequestID)
	if err != nil {
		return fileDeleteError(event, err, stage)
	}
	return event.JSON(http.StatusOK, productFileDeleteResponse(receipt))
}

// deleteProductFileAs deletes a workspace's file as principalID asks, by
// their authority; a failure names the stage it stopped at.
func deleteProductFileAs(
	ctx context.Context, app core.App, principalID, fileID, requestID string,
) (storedProductFileDeleteReceipt, string, error) {
	fileRelease := acquireProductFileOperationLock(productFileOperationReferenceCleanup, fileID)
	defer fileRelease()

	release := acquireProductFileOperationLock(
		productFileOperationScopedNamespace(productFileOperationDeleteRequest, principalID),
		requestID,
	)
	defer release()

	admission, err := admitProductFileDelete(app, principalID, fileID, requestID)
	if err != nil {
		return storedProductFileDeleteReceipt{}, "admit", err
	}
	if admission.Receipt.Status == fileDeleteStatusCompleted {
		return admission.Receipt, "", nil
	}
	quotaRelease, err := prepareProductOrganizationStorageAdmission(
		ctx, app, admission.Receipt.WorkspaceID, time.Now().UTC().UnixMilli(),
	)
	if err != nil {
		return storedProductFileDeleteReceipt{}, "quota-ready", err
	}
	defer quotaRelease()
	if err := deleteProductFileStorage(app, admission.Record); err != nil {
		return storedProductFileDeleteReceipt{}, "delete-storage", errors.Join(errFileDeleteUnavailable, err)
	}
	receipt, err := completeProductFileDelete(app, admission.Receipt)
	if err != nil {
		return storedProductFileDeleteReceipt{}, "complete", err
	}
	return receipt, "", nil
}

func admitProductFileDelete(
	app core.App,
	actorID, fileID, requestID string,
) (productFileDeleteAdmission, error) {
	result := productFileDeleteAdmission{}
	requestHash := productFileDeleteRequestHash(fileID)
	err := app.RunInTransaction(func(txApp core.App) error {
		receipt, err := loadProductFileDeleteReceipt(txApp, actorID, requestID)
		if err == nil {
			if receipt.FileID != fileID || receipt.RequestHash != requestHash {
				return errFileDeleteRequestIDReused
			}
			authorization, err := authorizeWorkspace(
				txApp, actorID, receipt.WorkspaceID, permissionWorkspaceWrite,
			)
			if err != nil {
				return err
			}
			result.Receipt = receipt
			if receipt.Status == fileDeleteStatusCompleted {
				return nil
			}
			record, err := txApp.FindRecordById(filesCollection, fileID)
			if err != nil || !workspaceFileRecordBelongsTo(record, receipt.WorkspaceID) {
				return errors.Join(errFileDeleteUnavailable, err)
			}
			if err := authorizeProductFileDeletePolicy(txApp, authorization, record); err != nil {
				return err
			}
			result.Record = record
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		record, authorization, err := authorizeWorkspaceFile(
			txApp, actorID, fileID, permissionWorkspaceWrite,
		)
		if err != nil {
			return err
		}
		if err := authorizeProductFileDeletePolicy(txApp, authorization, record); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		_, err = txApp.DB().Insert(productFileDeleteReceiptsTable, dbx.Params{
			"actor_id": actorID, "request_id": requestID, "request_hash": requestHash,
			"workspace_id": authorization.WorkspaceID, "file_id": fileID,
			"status": fileDeleteStatusPending, "created_at": now, "updated_at": now,
			"completed_at": 0,
		}).Execute()
		if err != nil {
			return err
		}
		result = productFileDeleteAdmission{
			Receipt: storedProductFileDeleteReceipt{
				ActorID: actorID, RequestID: requestID, RequestHash: requestHash,
				WorkspaceID: authorization.WorkspaceID, FileID: fileID,
				Status: fileDeleteStatusPending,
			},
			Record: record,
		}
		return nil
	})
	return result, err
}

func authorizeProductFileDeletePolicy(
	app core.App,
	authorization workspaceAuthorization,
	record *core.Record,
) error {
	if err := assertNoActiveLegalHoldForPermanentDelete(
		app, authorization.Organization.OrganizationID,
		legalHoldDeletionTarget{
			WorkspaceID:      authorization.WorkspaceID,
			CustodianUserIDs: []string{record.GetString("created_by")},
		},
	); err != nil {
		return err
	}
	referenced, err := findActiveProductFileReference(app, record.Id)
	if err != nil {
		return err
	}
	if referenced {
		return errFileReferenced
	}
	return nil
}

func findActiveProductFileReference(app core.App, fileID string) (bool, error) {
	return findProductFileReference(app, fileID, true)
}

// findLiveProductFileReference is whether anything but a page's version
// history still uses a file: a page, a database item, an emoji, an icon or a
// cover (a teamspace's or a group's icon too), an upload in progress.
func findLiveProductFileReference(app core.App, fileID string) (bool, error) {
	return findProductFileReference(app, fileID, false)
}

func findProductFileReference(app core.App, fileID string, history bool) (bool, error) {
	historyReferences := ""
	if !history {
		historyReferences = ` AND [[owner_kind]] != 'page_history'`
	}
	notionReference := "0"
	var notionColumns int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM pragma_table_info('leafen_file_uploads')
		WHERE [[name]] IN ('notion_mode','notion_attached_at')
	`).Row(&notionColumns); err != nil {
		return false, err
	}
	if notionColumns == 2 {
		notionReference = `EXISTS(
			SELECT 1 FROM {{leafen_file_uploads}} AS [[upload]]
			WHERE [[upload]].[[file_id]]={:fileId}
				AND [[upload]].[[notion_mode]]!=''
				AND [[upload]].[[notion_attached_at]]>0
				AND [[upload]].[[status]]={:uploadCompleted} LIMIT 1
		)`
	}
	var found int
	err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_file_references}}
			WHERE [[file_id]] = {:fileId}` + historyReferences + `
			LIMIT 1
		) OR EXISTS(
			SELECT 1 FROM {{leafen_custom_emojis}} AS [[emoji]]
			INDEXED BY [[idx_leafen_custom_emojis_file]]
			WHERE [[emoji]].[[file_id]]={:fileId} LIMIT 1
		) OR EXISTS(
			SELECT 1 FROM {{leafen_pages}} AS [[page]] INDEXED BY [[idx_leafen_pages_image_icon]]
			JOIN {{leafen_files}} AS [[file]] ON [[file]].[[id]]={:fileId}
				AND [[file]].[[workspace]]=[[page]].[[workspace]]
				AND [[file]].[[authority_state]]={:authority} AND [[file]].[[created_by]]!=''
			WHERE [[page]].[[icon_type]]='image' AND [[page]].[[icon]]!=''
				AND [[page]].[[icon]] IN ({:fileId},{:fileAddress}) LIMIT 1
		) OR EXISTS(
			SELECT 1 FROM {{leafen_pages}} AS [[page]] INDEXED BY [[idx_leafen_pages_cover]]
			JOIN {{leafen_files}} AS [[file]] ON [[file]].[[id]]={:fileId}
				AND [[file]].[[workspace]]=[[page]].[[workspace]]
				AND [[file]].[[authority_state]]={:authority} AND [[file]].[[created_by]]!=''
			WHERE [[page]].[[cover]]!='' AND [[page]].[[cover]] IN ({:fileId},{:fileAddress}) LIMIT 1
		) OR EXISTS(
			SELECT 1 FROM {{leafen_page_duplication_files}} AS [[duplicate]]
			JOIN {{leafen_page_duplication_jobs}} AS [[detail]]
				ON [[detail]].[[job_id]]=[[duplicate]].[[job_id]]
			JOIN {{leafen_jobs}} AS [[base]] ON [[base]].[[id]]=[[detail]].[[job_id]]
			WHERE [[duplicate]].[[target_file_id]]={:fileId} AND [[duplicate]].[[status]]='copied'
				AND [[detail]].[[phase]] IN ('copying','publishing')
				AND [[base]].[[status]]='queued' LIMIT 1
		) OR EXISTS(
			SELECT 1 FROM {{leafen_block_copy_files}} AS [[carried]]
			JOIN {{leafen_jobs}} AS [[base]] ON [[base]].[[id]]=[[carried]].[[job_id]]
			WHERE [[carried]].[[target_file_id]]={:fileId} AND [[carried]].[[status]]='copied'
				AND [[base]].[[status]]='queued' LIMIT 1
		) OR ` + heldIconFileReferenceSQL + ` OR ` + notionReference).Bind(dbx.Params{
		"fileId": fileID, "fileAddress": productFilePresentationWithID(productFilePresentationPrefix, fileID),
		"authority": fileAuthorityWorkspace, "uploadCompleted": fileUploadStatusCompleted,
	}).Row(&found)
	return found == 1, err
}

func deleteProductFileStorage(app core.App, record *core.Record) error {
	if record == nil {
		return errFileDeleteUnavailable
	}
	fsys, err := app.NewFilesystem()
	if err != nil {
		return err
	}
	defer fsys.Close()
	if err := fsys.Delete(productFileStorageKey(record)); err != nil &&
		!errors.Is(err, filesystem.ErrNotFound) {
		return err
	}
	return deleteProductFilePreviews(fsys, record)
}

func completeProductFileDelete(
	app core.App,
	expected storedProductFileDeleteReceipt,
) (storedProductFileDeleteReceipt, error) {
	result := storedProductFileDeleteReceipt{}
	err := app.RunInTransaction(func(txApp core.App) error {
		receipt, err := loadProductFileDeleteReceipt(txApp, expected.ActorID, expected.RequestID)
		if err != nil {
			return err
		}
		if receipt.FileID != expected.FileID || receipt.RequestHash != expected.RequestHash ||
			receipt.WorkspaceID != expected.WorkspaceID {
			return errFileDeleteRequestIDReused
		}
		if receipt.Status == fileDeleteStatusCompleted {
			result = receipt
			return nil
		}
		referenced, err := findActiveProductFileReference(txApp, receipt.FileID)
		if err != nil {
			return err
		}
		if referenced {
			return errFileReferenced
		}
		now := time.Now().UTC().UnixMilli()
		if err := releaseProductOrganizationStorage(
			context.Background(), txApp, receipt.FileID, now,
		); err != nil {
			return err
		}
		if _, err := txApp.DB().Delete(filesCollection, dbx.HashExp{
			"id": receipt.FileID, "workspace": receipt.WorkspaceID,
		}).Execute(); err != nil {
			return err
		}
		updated, err := txApp.DB().Update(productFileDeleteReceiptsTable, dbx.Params{
			"status": fileDeleteStatusCompleted, "updated_at": now, "completed_at": now,
		}, dbx.HashExp{
			"actor_id": receipt.ActorID, "request_id": receipt.RequestID,
			"status": fileDeleteStatusPending,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := updated.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errFileDeleteUnavailable, err)
		}
		receipt.Status = fileDeleteStatusCompleted
		result = receipt
		return nil
	})
	if err == nil {
		notifyProductFileReferenceCleanupAfterCommit(app)
	}
	return result, err
}

func loadProductFileDeleteReceipt(
	app core.App,
	actorID, requestID string,
) (storedProductFileDeleteReceipt, error) {
	receipt := storedProductFileDeleteReceipt{}
	err := app.DB().NewQuery(`
		SELECT [[actor_id]], [[request_id]], [[request_hash]], [[workspace_id]],
			[[file_id]], [[status]]
		FROM {{leafen_file_delete_receipts}}
		WHERE [[actor_id]] = {:actorId} AND [[request_id]] = {:requestId}
		LIMIT 1
	`).Bind(dbx.Params{"actorId": actorID, "requestId": requestID}).One(&receipt)
	return receipt, err
}

func productFileDeletionPending(app core.App, fileID string) (bool, error) {
	var found int
	err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_file_delete_receipts}}
			WHERE [[file_id]] = {:fileId} AND [[status]] = {:status}
			LIMIT 1
		)
	`).Bind(dbx.Params{"fileId": fileID, "status": fileDeleteStatusPending}).Row(&found)
	return found == 1, err
}

func pendingProductFileDeletionIDs(
	app core.App,
	fileIDs []string,
) (map[string]struct{}, error) {
	result := map[string]struct{}{}
	for start := 0; start < len(fileIDs); start += graphFileAuthorityBatchSize {
		end := min(start+graphFileAuthorityBatchSize, len(fileIDs))
		params := dbx.Params{"status": fileDeleteStatusPending}
		placeholders := make([]string, 0, end-start)
		for index, fileID := range fileIDs[start:end] {
			key := "file" + strconv.Itoa(index)
			params[key] = fileID
			placeholders = append(placeholders, "{:"+key+"}")
		}
		rows := []struct {
			FileID string `db:"file_id"`
		}{}
		err := app.DB().NewQuery(`
			SELECT DISTINCT [[file_id]] FROM {{leafen_file_delete_receipts}}
			WHERE [[status]] = {:status} AND [[file_id]] IN (` + strings.Join(placeholders, ",") + `)
		`).Bind(params).All(&rows)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.FileID] = struct{}{}
		}
	}
	return result, nil
}

func productFileDeleteRequestHash(fileID string) string {
	digest := sha256.Sum256([]byte(fileID))
	return hex.EncodeToString(digest[:])
}

func productFileDeleteResponse(receipt storedProductFileDeleteReceipt) fileDeleteResponse {
	return fileDeleteResponse{FileID: receipt.FileID, Status: fileDeleteStatusCompleted}
}

func fileDeleteError(event *core.RequestEvent, err error, stage string) error {
	if quotaError, ok := organizationStorageAPIError(event, err, "file_delete", stage); ok {
		return quotaError
	}
	if storageError, ok := fileOperationalStorageAPIError(event, err, "file_delete", stage); ok {
		return storageError
	}
	status, code, message := http.StatusServiceUnavailable,
		errorCodeFileDeleteUnavailable, "Couldn't delete the file."
	switch {
	case errors.Is(err, errFileDeleteInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodeFileDeleteInvalid, "Check the file deletion request."
	case errors.Is(err, errFileReferenced):
		status, code, message = http.StatusConflict,
			errorCodeFileReferenced, "A file in use can't be deleted."
	case errors.Is(err, errFileDeleteRequestIDReused):
		status, code, message = http.StatusConflict,
			errorCodeFileDeleteRequestIDReused, "This request ID was used for a different file deletion."
	case errors.Is(err, errLegalHoldPreventsPermanentDelete):
		status, code, message = http.StatusPreconditionFailed,
			errorCodeLegalHoldPreventsPermanentDelete,
			"Release the active legal hold, then delete the file."
	case isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodeFileNotFound, "File not found."
	}
	return writeAPIError(
		event, status, code, message, "file_delete", stage, code, err,
		diagnosticSourcePosition(1),
	)
}
