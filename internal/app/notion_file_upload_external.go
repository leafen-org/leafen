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

// validateNotionExternalFileUploadSource checks that the file at an
// external_url answers. Its own type says nothing: the upload keeps the
// name and content type it was made with, as Notion's does.
func validateNotionExternalFileUploadSource(ctx context.Context, rawURL string) error {
	parsed, err := validateNotionFileURL(rawURL)
	if err != nil {
		return notionInvalid(errFileUploadInvalid, "external_url must be a public https URL.")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, parsed.String(), nil)
	if err != nil {
		return notionInvalid(errFileUploadInvalid, "external_url must be a public https URL.")
	}
	client := newNotionFileHTTPClient()
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return notionInvalid(errFileUploadInvalid, "The file at external_url couldn't be reached.")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 ||
		strings.TrimSpace(response.Header.Get("Content-Type")) == "" ||
		response.ContentLength > fileTransferMaxFileBytes {
		return notionInvalid(errFileUploadInvalid, "The file at external_url must answer with a Content-Type and be at most 5 GiB.")
	}
	return nil
}

func loadProductFileUploadByUploadID(
	app core.App,
	uploadID string,
) (storedProductFileUpload, error) {
	upload := storedProductFileUpload{}
	err := app.DB().NewQuery(productFileUploadSelect + `
		WHERE [[upload_id]]={:uploadId}
		LIMIT 1
	`).Bind(dbx.Params{"uploadId": uploadID}).One(&upload)
	return upload, err
}

func prepareNotionExternalFileUploadPublication(
	app core.App,
	upload storedProductFileUpload,
	size int64,
	digest string,
	contentType string,
) (storedProductFileUpload, error) {
	if upload.Kind != fileUploadKindNotionExternal ||
		upload.NotionMode != notionFileUploadModeExternal ||
		size <= 0 || size > fileTransferMaxFileBytes || !sha256Pattern.MatchString(digest) {
		return storedProductFileUpload{}, errFileUploadInvalid
	}
	now := time.Now().UTC().UnixMilli()
	quotaRelease, err := prepareProductOrganizationStorageAdmission(
		context.Background(), app, upload.WorkspaceID, now,
	)
	if err != nil {
		return storedProductFileUpload{}, err
	}
	defer quotaRelease()

	prepared := storedProductFileUpload{}
	err = app.RunInTransaction(func(txApp core.App) error {
		current, err := loadProductFileUploadByID(txApp, upload.ActorID, upload.UploadID)
		if err != nil {
			return err
		}
		if current.Kind != fileUploadKindNotionExternal ||
			current.NotionMode != notionFileUploadModeExternal ||
			notionFileUploadPublicStatus(current, now) != "pending" {
			return errFileUploadExpired
		}
		if current.Status != fileUploadStatusPending && current.Status != fileUploadStatusSettling {
			return errFileUploadIncomplete
		}
		if current.Status == fileUploadStatusSettling {
			if current.TotalSize != size || current.ReceivedBytes != size ||
				current.ContentType != contentType || current.SHA256 != digest ||
				current.FileID != current.UploadID {
				return errFileUploadUnavailable
			}
			prepared = current
			return nil
		}
		authorization, err := authorizeProductFileUpload(
			txApp, current.ActorID, current.WorkspaceID, current.uploadScope(),
		)
		if err != nil {
			return err
		}
		stored, _ := notionFileUploadStoredType(current.OriginalName, contentType)
		if err := authorizeProductFileUploadMetadata(
			txApp, authorization, current.OriginalName, stored,
		); err != nil {
			return err
		}
		if _, err := growProductOrganizationStorageReservation(
			context.Background(), txApp, current.WorkspaceID, current.UploadID, size, now,
		); err != nil {
			return err
		}
		updated, err := txApp.DB().Update(productFileUploadsTable, dbx.Params{
			"content_type": contentType, "total_size": size,
			"received_bytes": size, "next_part": 1, "sha256": digest,
			"updated_at": now,
		}, dbx.HashExp{
			"upload_id": current.UploadID, "status": fileUploadStatusPending,
			"received_bytes": current.ReceivedBytes,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := updated.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errFileUploadUnavailable, err)
		}
		current.ContentType = contentType
		current.TotalSize = size
		current.ReceivedBytes = size
		current.NextPart = 1
		current.SHA256 = digest
		current.UpdatedAt = now
		prepared = current
		return nil
	})
	return prepared, err
}

func completeNotionExternalFileUploadImport(app core.App, uploadID string) error {
	now := time.Now().UTC().UnixMilli()
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_file_uploads}}
		SET [[notion_external_url_cipher]]='', [[notion_import_next_at]]=0,
			[[notion_import_attempts]]=[[notion_import_attempts]]+1,
			[[updated_at]]={:now}
		WHERE [[upload_id]]={:uploadId} AND [[kind]]={:kind}
			AND [[notion_mode]]={:mode} AND [[status]]={:completed}
	`).Bind(dbx.Params{
		"now": now, "uploadId": uploadID, "kind": fileUploadKindNotionExternal,
		"mode": notionFileUploadModeExternal, "completed": fileUploadStatusCompleted,
	}).Execute()
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return errors.Join(errFileUploadUnavailable, err)
	}
	if upload, err := loadProductFileUploadByUploadID(app, uploadID); err == nil {
		recordFileUploadWebhookEvent(app, eventWebhookFileUploadCompleted, upload, pageHistoryConnection{}, nil)
	}
	return nil
}

func failNotionExternalFileUpload(
	app core.App,
	upload storedProductFileUpload,
	failureType string,
	code string,
	message string,
) error {
	now := time.Now().UTC().UnixMilli()
	quotaRelease, err := prepareProductOrganizationStorageAdmission(
		context.Background(), app, upload.WorkspaceID, now,
	)
	if err != nil {
		return err
	}
	defer quotaRelease()

	return app.RunInTransaction(func(txApp core.App) error {
		current, err := loadProductFileUploadByID(txApp, upload.ActorID, upload.UploadID)
		if errors.Is(err, sql.ErrNoRows) {
			return errFileUploadNotFound
		}
		if err != nil {
			return err
		}
		if current.Kind != fileUploadKindNotionExternal ||
			current.NotionMode != notionFileUploadModeExternal {
			return errFileUploadInvalid
		}
		if current.Status == fileUploadStatusFailed {
			return nil
		}
		if current.Status == fileUploadStatusCompleted {
			return nil
		}
		if current.Status != fileUploadStatusPending && current.Status != fileUploadStatusSettling {
			return errFileUploadIncomplete
		}
		if err := releaseProductOrganizationStorageIfPresent(
			context.Background(), txApp, current.UploadID, now,
		); err != nil {
			return err
		}
		if _, err := txApp.DB().Delete(productFileUploadPartsTable, dbx.HashExp{
			"upload_id": current.UploadID,
		}).Execute(); err != nil {
			return err
		}
		updated, err := txApp.DB().Update(productFileUploadsTable, dbx.Params{
			"status": fileUploadStatusFailed, "total_size": 0,
			"received_bytes": 0, "next_part": 0, "file_id": "", "sha256": "",
			"cleanup_due_at": 0, "cleanup_error_code": "",
			"notion_external_url_cipher": "", "notion_import_next_at": 0,
			"notion_import_attempts": current.NotionImportAttempts + 1,
			"notion_failure_type":    failureType, "notion_failure_code": code,
			"notion_failure_message": message, "notion_failure_parameter": "",
			"notion_failure_status": 0, "updated_at": now,
		}, dbx.HashExp{
			"upload_id": current.UploadID, "status": current.Status,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := updated.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errFileUploadUnavailable, err)
		}
		current.NotionFailureType, current.NotionFailureCode, current.NotionFailureMessage = failureType, code, message
		current.NotionFailureParameter, current.NotionFailureStatus, current.UpdatedAt = "", 0, now
		recordFileUploadFailedWebhookEvent(txApp, current)
		return nil
	})
}
