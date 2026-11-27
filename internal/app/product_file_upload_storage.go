package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/filesystem/blob"
	"github.com/pocketbase/pocketbase/tools/security"
)

const productFileUploadStoragePrefix = "leafen-upload-parts/"

func persistProductFileUploadPart(
	app core.App,
	upload storedProductFileUpload,
	partNumber int,
	expectedBytes int64,
	expectedDigest string,
	body io.Reader,
) (storedProductFileUpload, bool, error) {
	existing, existingErr := loadProductFileUploadPart(app, upload.UploadID, partNumber)
	hasExisting := existingErr == nil
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		return upload, false, existingErr
	}

	fsys, err := app.NewFilesystem()
	if err != nil {
		return upload, false, err
	}
	defer fsys.Close()
	storageKey := productFileUploadPrefix(upload.UploadID) + "part-" +
		formatProductFileUploadPartNumber(partNumber) + "-" + security.RandomString(12) + ".bin"
	writer, err := fsys.NewWriter(storageKey, &blob.WriterOptions{ContentType: "application/octet-stream"})
	if err != nil {
		return upload, false, err
	}
	hasher := sha256.New()
	written, copyErr := io.CopyBuffer(
		io.MultiWriter(writer, hasher),
		io.LimitReader(body, fileTransferPartBytes+1),
		make([]byte, productFileDigestBufferSize),
	)
	closeErr := writer.Close()
	actualDigest := hex.EncodeToString(hasher.Sum(nil))
	if copyErr != nil || closeErr != nil || written != expectedBytes || actualDigest != expectedDigest {
		_ = fsys.Delete(storageKey)
		if hasExisting {
			return upload, false, errors.Join(errFileUploadPartConflict, copyErr, closeErr)
		}
		return upload, false, errors.Join(errFileUploadPartMismatch, copyErr, closeErr)
	}
	if hasExisting {
		if existing.Size != written || existing.SHA256 != actualDigest {
			_ = fsys.Delete(storageKey)
			return upload, false, errFileUploadPartConflict
		}
		current := upload
		err = app.RunInTransaction(func(txApp core.App) error {
			loaded, err := loadProductFileUploadByID(txApp, upload.ActorID, upload.UploadID)
			if err != nil {
				return err
			}
			if loaded.Status != fileUploadStatusPending {
				return errFileUploadIncomplete
			}
			result, err := txApp.DB().Update(productFileUploadPartsTable, dbx.Params{
				"storage_key": storageKey,
			}, dbx.HashExp{
				"upload_id": upload.UploadID, "part_number": partNumber,
				"size": existing.Size, "sha256": existing.SHA256,
				"storage_key": existing.StorageKey,
			}).Execute()
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err != nil || count != 1 {
				return errors.Join(errFileUploadPartConflict, err)
			}
			now := time.Now().UTC().UnixMilli()
			cleanupDueAt := productFileUploadCleanupDueAt(now)
			if _, err := txApp.DB().Update(productFileUploadsTable, dbx.Params{
				"updated_at": now, "cleanup_due_at": cleanupDueAt,
			}, dbx.HashExp{
				"upload_id": loaded.UploadID, "status": fileUploadStatusPending,
			}).Execute(); err != nil {
				return err
			}
			loaded.CleanupDueAt = cleanupDueAt
			current = loaded
			return nil
		})
		if err != nil {
			_ = fsys.Delete(storageKey)
			return upload, false, err
		}
		_ = fsys.Delete(existing.StorageKey)
		notifyProductFileUploadCleanupAfterCommit(app)
		return current, true, nil
	}

	updated := upload
	err = app.RunInTransaction(func(txApp core.App) error {
		current, err := loadProductFileUploadByID(txApp, upload.ActorID, upload.UploadID)
		if err != nil {
			return err
		}
		if current.Status != fileUploadStatusPending || current.NextPart != partNumber ||
			current.ReceivedBytes+written > current.TotalSize {
			return errFileUploadPartConflict
		}
		if _, err := loadProductFileUploadPart(txApp, upload.UploadID, partNumber); err == nil {
			return errFileUploadPartConflict
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := txApp.DB().Insert(productFileUploadPartsTable, dbx.Params{
			"upload_id": upload.UploadID, "part_number": partNumber,
			"size": written, "sha256": actualDigest, "storage_key": storageKey,
		}).Execute(); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		cleanupDueAt := productFileUploadCleanupDueAt(now)
		result, err := txApp.DB().Update(productFileUploadsTable, dbx.Params{
			"received_bytes": current.ReceivedBytes + written,
			"next_part":      partNumber + 1, "updated_at": now,
			"cleanup_due_at": cleanupDueAt,
		}, dbx.HashExp{
			"actor_id": current.ActorID, "request_id": current.RequestID,
			"status": fileUploadStatusPending, "received_bytes": current.ReceivedBytes,
			"next_part": partNumber,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errFileUploadPartConflict, err)
		}
		current.ReceivedBytes += written
		current.NextPart = partNumber + 1
		current.CleanupDueAt = cleanupDueAt
		updated = current
		return nil
	})
	if err != nil {
		_ = fsys.Delete(storageKey)
		return upload, false, err
	}
	notifyProductFileUploadCleanupAfterCommit(app)
	return updated, false, nil
}

func streamAndPublishProductFileUpload(
	app core.App,
	upload storedProductFileUpload,
	parts []storedProductFileUploadPart,
) (*core.Record, error) {
	// The joined file is written beside its parts: the room asked for when
	// the upload was prepared, unless other writes took it since
	// (dataDiskPartsBytes).
	if err := requireDataDiskRoom(app, upload.TotalSize); err != nil {
		return nil, err
	}
	reader := newProductFileUploadPartsFileReader(app, parts, upload.TotalSize)
	file := &filesystem.File{
		Reader: reader, Name: "payload.bin", OriginalName: upload.OriginalName,
		Size: upload.TotalSize,
	}
	return publishProductFileUpload(app, upload, file, reader)
}

func publishProductFileUpload(
	app core.App,
	upload storedProductFileUpload,
	file *filesystem.File,
	reader *productFileUploadPartsFileReader,
) (*core.Record, error) {
	current, resuming, err := claimProductFileUploadPublication(app, upload, "")
	if err != nil {
		return nil, err
	}
	record, exists, err := findProductFilePublicationRecord(app, current)
	if err != nil {
		return nil, err
	}
	if !exists {
		collection, err := app.FindCollectionByNameOrId(filesCollection)
		if err != nil {
			return nil, err
		}
		record = newProductFileRecord(
			collection, current, file, productFileUploadPendingDigest,
		)
		reader.setVerifiedObserver(func(digest string) { record.Set("sha256", digest) })
		if resuming {
			if err := reclaimUncommittedProductFileRecordStorage(app, record); err != nil {
				return nil, err
			}
		}
		if err := publishProductFileRecordOutsideTransaction(app, record); err != nil {
			return nil, err
		}
		digest, err := reader.VerifiedDigest()
		if err != nil || record.GetString("sha256") != digest {
			return nil, errors.Join(errProductFileIntegrity, err)
		}
	} else if !productFileRecordMatchesUploadMetadata(record, current) ||
		!sha256Pattern.MatchString(record.GetString("sha256")) ||
		record.GetString("sha256") == productFileUploadPendingDigest {
		return nil, errFileUploadUnavailable
	}
	return record, nil
}

func settleProductFileUploadCompletion(
	app core.App,
	upload storedProductFileUpload,
) (*core.Record, error) {
	current, err := loadProductFileUploadByID(app, upload.ActorID, upload.UploadID)
	if err != nil {
		return nil, err
	}
	if _, err := authorizeProductFileUpload(app, current.ActorID, current.WorkspaceID, current.uploadScope()); err != nil {
		return nil, err
	}
	if current.Status != fileUploadStatusSettling && current.Status != fileUploadStatusCompleted {
		return nil, errFileUploadIncomplete
	}
	record, err := app.FindRecordById(filesCollection, current.FileID)
	digest := ""
	if record != nil {
		digest = record.GetString("sha256")
	}
	if err != nil || !sha256Pattern.MatchString(digest) ||
		digest == productFileUploadPendingDigest ||
		!productFileRecordMatchesUploadMetadata(record, current) {
		return nil, errors.Join(errFileUploadUnavailable, err)
	}
	if current.Status == fileUploadStatusCompleted && current.SHA256 != digest {
		return nil, errFileUploadUnavailable
	}
	if current.Status == fileUploadStatusSettling {
		if err := settleProductFileStorageIdentity(app, record); err != nil {
			return nil, err
		}
	}
	if productFileUploadUsesPartStorage(current.Kind) {
		if err := cleanupProductFileUploadStorage(app, current.UploadID); err != nil {
			return nil, err
		}
	}
	if current.Status == fileUploadStatusCompleted {
		if _, err := ensureNotionObjectIdentity(
			app, notionObjectIdentityFileUpload, current.WorkspaceID, current.UploadID,
		); err != nil {
			return nil, err
		}
		return record, nil
	}
	scheduledCleanup := false
	err = app.RunInTransaction(func(txApp core.App) error {
		latest, err := loadProductFileUploadByID(txApp, current.ActorID, current.UploadID)
		if err != nil {
			return err
		}
		if latest.Status == fileUploadStatusCompleted {
			_, err := ensureNotionObjectIdentity(
				txApp, notionObjectIdentityFileUpload, latest.WorkspaceID, latest.UploadID,
			)
			return err
		}
		if latest.Status != fileUploadStatusSettling || latest.FileID != current.FileID ||
			latest.Kind != current.Kind {
			return errFileUploadUnavailable
		}
		storedRecord, err := txApp.FindRecordById(filesCollection, latest.FileID)
		if err != nil || storedRecord.GetString("sha256") != digest ||
			!productFileRecordMatchesUploadMetadata(storedRecord, latest) {
			return errors.Join(errFileUploadUnavailable, err)
		}
		if productFileUploadUsesPartStorage(latest.Kind) {
			if _, err := txApp.DB().Delete(productFileUploadPartsTable, dbx.HashExp{
				"upload_id": current.UploadID,
			}).Execute(); err != nil {
				return err
			}
		}
		now := time.Now().UTC().UnixMilli()
		updated, err := txApp.DB().Update(productFileUploadsTable, dbx.Params{
			"status": fileUploadStatusCompleted, "sha256": digest,
			"updated_at": now, "completed_at": now, "cleanup_due_at": 0,
		}, dbx.HashExp{
			"actor_id": current.ActorID, "request_id": current.RequestID,
			"status": fileUploadStatusSettling, "file_id": current.FileID,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := updated.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errFileUploadUnavailable, err)
		}
		if _, err := ensureNotionObjectIdentity(
			txApp, notionObjectIdentityFileUpload, latest.WorkspaceID, latest.UploadID,
		); err != nil {
			return err
		}
		scheduledCleanup, err = scheduleProductFileNeverAttachedCleanup(
			context.Background(), txApp, latest.FileID, latest.WorkspaceID, now,
		)
		if err == nil && scheduledCleanup && latest.NotionMode != "" {
			_, err = txApp.DB().NewQuery(`
				UPDATE {{leafen_file_reference_cleanups}}
				SET [[cleanup_due_at]]={:due}, [[updated_at]]={:now}
				WHERE [[file_id]]={:fileId} AND [[status]]={:pending}
			`).Bind(dbx.Params{
				"due": notionFileUploadExpiresAt(latest), "now": now,
				"fileId": latest.FileID, "pending": fileReferenceCleanupStatusPending,
			}).Execute()
		}
		return err
	})
	if err == nil && scheduledCleanup {
		notifyProductFileReferenceCleanupAfterCommit(app)
	}
	return record, err
}

func productFileUploadUsesPartStorage(kind string) bool {
	return kind == fileUploadKindResumable ||
		kind == fileUploadKindNotionSingle || kind == fileUploadKindNotionMulti
}

func cleanupProductFileUploadStorage(app core.App, uploadID string) error {
	return cleanupProductFileUploadStorageWithContext(context.Background(), app, uploadID)
}

func productFileUploadPrefix(uploadID string) string {
	return productFileUploadStoragePrefix + uploadID + "/"
}

func formatProductFileUploadPartNumber(partNumber int) string {
	value := strconv.Itoa(partNumber)
	return strings.Repeat("0", max(0, 3-len(value))) + value
}

func productFileRecordMatchesUpload(
	record *core.Record,
	upload storedProductFileUpload,
	digest string,
) bool {
	return productFileRecordMatchesUploadMetadata(record, upload) &&
		record.GetString("sha256") == digest
}

func productFileRecordMatchesUploadMetadata(
	record *core.Record,
	upload storedProductFileUpload,
) bool {
	return record != nil && record.Id == upload.UploadID &&
		workspaceFileRecordBelongsTo(record, upload.WorkspaceID) &&
		record.GetString("original_name") == upload.OriginalName &&
		record.GetString("content_type") == upload.storedContentType() &&
		int64(record.GetFloat("size")) == upload.TotalSize
}
