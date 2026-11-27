package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	fileUploadKindDirect           = "direct"
	fileUploadKindResumable        = "resumable"
	fileUploadKindWorkspaceArchive = "workspace_archive"
	fileUploadKindNotionSingle     = "notion_single"
	fileUploadKindNotionMulti      = "notion_multi"
	fileUploadKindNotionExternal   = "notion_external"
	fileUploadStatusFailed         = "failed"
	fileUploadStatusExpired        = "expired"
)

type storedProductFileUpload struct {
	ActorID                 string `db:"actor_id"`
	RequestID               string `db:"request_id"`
	RequestHash             string `db:"request_hash"`
	UploadID                string `db:"upload_id"`
	Kind                    string `db:"kind"`
	WorkspaceID             string `db:"workspace_id"`
	OriginalName            string `db:"original_name"`
	ContentType             string `db:"content_type"`
	TotalSize               int64  `db:"total_size"`
	Status                  string `db:"status"`
	ReceivedBytes           int64  `db:"received_bytes"`
	NextPart                int    `db:"next_part"`
	FileID                  string `db:"file_id"`
	SHA256                  string `db:"sha256"`
	CleanupDueAt            int64  `db:"cleanup_due_at"`
	CleanupTries            int    `db:"cleanup_attempts"`
	CleanupError            string `db:"cleanup_error_code"`
	CreatedAt               int64  `db:"created_at"`
	UpdatedAt               int64  `db:"updated_at"`
	CompletedAt             int64  `db:"completed_at"`
	NotionMode              string `db:"notion_mode"`
	NotionPartsTotal        int    `db:"notion_parts_total"`
	NotionExternalURLCipher string `db:"notion_external_url_cipher"`
	NotionImportAttempts    int    `db:"notion_import_attempts"`
	NotionImportNextAt      int64  `db:"notion_import_next_at"`
	NotionAttachedAt        int64  `db:"notion_attached_at"`
	NotionFailureType       string `db:"notion_failure_type"`
	NotionFailureCode       string `db:"notion_failure_code"`
	NotionFailureMessage    string `db:"notion_failure_message"`
	NotionFailureParameter  string `db:"notion_failure_parameter"`
	NotionFailureStatus     int    `db:"notion_failure_status"`
	PageID                  string `db:"page_id"`
	Purpose                 string `db:"purpose"`
}

type storedProductFileUploadPart struct {
	UploadID   string `db:"upload_id"`
	PartNumber int    `db:"part_number"`
	Size       int64  `db:"size"`
	SHA256     string `db:"sha256"`
	StorageKey string `db:"storage_key"`
}

func admitProductFileUpload(
	app core.App,
	actorID string,
	input fileUploadPrepareInput,
) (storedProductFileUpload, bool, error) {
	return admitProductFileUploadKind(
		app, actorID, input, fileUploadKindResumable, "",
	)
}

func admitProductFileUploadKind(
	app core.App,
	actorID string,
	input fileUploadPrepareInput,
	kind string,
	digest string,
) (storedProductFileUpload, bool, error) {
	result := storedProductFileUpload{}
	created := false
	if kind != fileUploadKindResumable && kind != fileUploadKindWorkspaceArchive {
		return result, false, errFileUploadInvalid
	}
	requestHash := productFileUploadRequestHash(kind, input, digest)
	quotaRelease, err := prepareProductOrganizationStorageAdmission(
		context.Background(), app, input.WorkspaceID, time.Now().UTC().UnixMilli(),
	)
	if err != nil {
		return result, false, err
	}
	defer quotaRelease()
	err = app.RunInTransaction(func(txApp core.App) error {
		existing, err := loadProductFileUploadByRequest(txApp, actorID, input.RequestID)
		if err == nil {
			if existing.Kind != kind || existing.RequestHash != requestHash {
				return errFileUploadRequestIDReused
			}
			if existing.Status == fileUploadStatusCleanupPending {
				return errFileUploadExpired
			}
			if _, err := authorizeProductFileUpload(txApp, actorID, existing.WorkspaceID, existing.uploadScope()); err != nil {
				return err
			}
			result = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		authorization, err := authorizeProductFileUpload(txApp, actorID, input.WorkspaceID, input.uploadScope())
		if err != nil {
			return err
		}
		if err := authorizeProductFileUploadMetadata(
			txApp, authorization, input.Name, input.ContentType,
		); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		cleanupDueAt := productFileUploadCleanupDueAt(now)
		result = storedProductFileUpload{
			ActorID: actorID, RequestID: input.RequestID, RequestHash: requestHash,
			UploadID: core.GenerateDefaultRandomId(), Kind: kind,
			WorkspaceID: input.WorkspaceID, OriginalName: input.Name,
			ContentType: input.ContentType, TotalSize: input.Size,
			Status: fileUploadStatusPending, SHA256: digest, CleanupDueAt: cleanupDueAt,
			PageID: input.PageID, Purpose: input.Purpose,
		}
		if _, err := reserveProductOrganizationStorage(
			context.Background(), txApp, result.WorkspaceID, result.UploadID,
			result.TotalSize, now,
		); err != nil {
			return err
		}
		_, err = txApp.DB().Insert(productFileUploadsTable, dbx.Params{
			"actor_id": result.ActorID, "request_id": result.RequestID,
			"request_hash": result.RequestHash, "upload_id": result.UploadID,
			"kind": result.Kind, "workspace_id": result.WorkspaceID,
			"original_name": result.OriginalName, "content_type": result.ContentType,
			"total_size": result.TotalSize, "status": result.Status,
			"received_bytes": 0, "next_part": 0, "file_id": "", "sha256": digest,
			"created_at": now, "updated_at": now, "completed_at": 0,
			"cleanup_due_at": cleanupDueAt, "cleanup_attempts": 0,
			"cleanup_error_code": "", "page_id": result.PageID, "purpose": result.Purpose,
		}).Execute()
		if err != nil {
			return err
		}
		if _, err := ensureNotionObjectIdentity(
			txApp, notionObjectIdentityFileUpload, result.WorkspaceID, result.UploadID,
		); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err == nil && created {
		notifyProductFileUploadCleanupAfterCommit(app)
	}
	return result, created, err
}

func loadProductFileUploadByRequest(
	app core.App,
	actorID, requestID string,
) (storedProductFileUpload, error) {
	result := storedProductFileUpload{}
	err := app.DB().NewQuery(productFileUploadSelect + `
		WHERE [[actor_id]] = {:actorId} AND [[request_id]] = {:requestId}
		LIMIT 1
	`).Bind(dbx.Params{"actorId": actorID, "requestId": requestID}).One(&result)
	return result, err
}

func loadProductFileUploadByID(
	app core.App,
	actorID, uploadID string,
) (storedProductFileUpload, error) {
	result := storedProductFileUpload{}
	err := app.DB().NewQuery(productFileUploadSelect + `
		WHERE [[actor_id]] = {:actorId} AND [[upload_id]] = {:uploadId}
		LIMIT 1
	`).Bind(dbx.Params{"actorId": actorID, "uploadId": uploadID}).One(&result)
	return result, err
}

const productFileUploadSelect = `
	SELECT [[actor_id]], [[request_id]], [[request_hash]], [[upload_id]], [[kind]],
		[[workspace_id]], [[original_name]], [[content_type]], [[total_size]],
		[[status]], [[received_bytes]], [[next_part]], [[file_id]], [[sha256]],
		[[cleanup_due_at]], [[cleanup_attempts]], [[cleanup_error_code]],
		[[created_at]], [[updated_at]], [[completed_at]], [[notion_mode]],
		[[notion_parts_total]], [[notion_external_url_cipher]],
		[[notion_import_attempts]], [[notion_import_next_at]], [[notion_attached_at]],
		[[notion_failure_type]], [[notion_failure_code]], [[notion_failure_message]],
		[[notion_failure_parameter]], [[notion_failure_status]], [[page_id]], [[purpose]]
	FROM {{leafen_file_uploads}}
`

func productFileUploadCleanupDueAt(nowMillis int64) int64 {
	return nowMillis + int64(fileTransferUploadInactivitySeconds)*int64(time.Second/time.Millisecond)
}

func loadProductFileUploadParts(
	app core.App,
	uploadID string,
) ([]storedProductFileUploadPart, error) {
	parts := []storedProductFileUploadPart{}
	err := app.DB().NewQuery(`
		SELECT [[upload_id]], [[part_number]], [[size]], [[sha256]], [[storage_key]]
		FROM {{leafen_file_upload_parts}}
		WHERE [[upload_id]] = {:uploadId}
		ORDER BY [[part_number]] ASC
	`).Bind(dbx.Params{"uploadId": uploadID}).All(&parts)
	return parts, err
}

func loadProductFileUploadPart(
	app core.App,
	uploadID string,
	partNumber int,
) (storedProductFileUploadPart, error) {
	part := storedProductFileUploadPart{}
	err := app.DB().NewQuery(`
		SELECT [[upload_id]], [[part_number]], [[size]], [[sha256]], [[storage_key]]
		FROM {{leafen_file_upload_parts}}
		WHERE [[upload_id]] = {:uploadId} AND [[part_number]] = {:partNumber}
		LIMIT 1
	`).Bind(dbx.Params{"uploadId": uploadID, "partNumber": partNumber}).One(&part)
	return part, err
}

func productFileUploadRequestHash(
	kind string,
	input fileUploadPrepareInput,
	digest string,
) string {
	fields := []string{
		kind, input.WorkspaceID, input.Name, input.ContentType,
		strconv.FormatInt(input.Size, 10), strings.ToLower(strings.TrimSpace(digest)),
	}
	if input.PageID != "" {
		fields = append(fields, input.PageID, input.Purpose)
	}
	value := strings.Join(fields, "\x00")
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func productFileUploadIncomplete(app core.App, fileID string) (bool, error) {
	if strings.TrimSpace(fileID) == "" {
		return false, nil
	}
	var found int
	err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_file_uploads}}
			WHERE [[file_id]] = {:fileId} AND [[status]] != {:completed}
			LIMIT 1
		)
	`).Bind(dbx.Params{"fileId": fileID, "completed": fileUploadStatusCompleted}).Row(&found)
	return found == 1, err
}

func incompleteProductFileUploadIDs(
	app core.App,
	fileIDs []string,
) (map[string]struct{}, error) {
	result := map[string]struct{}{}
	for start := 0; start < len(fileIDs); start += graphFileAuthorityBatchSize {
		end := min(start+graphFileAuthorityBatchSize, len(fileIDs))
		params := dbx.Params{"completed": fileUploadStatusCompleted}
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
			SELECT DISTINCT [[file_id]] FROM {{leafen_file_uploads}}
			WHERE [[status]] != {:completed} AND [[file_id]] IN (` + strings.Join(placeholders, ",") + `)
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
