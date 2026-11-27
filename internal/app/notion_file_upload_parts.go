package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/filesystem/blob"
	"github.com/pocketbase/pocketbase/tools/security"
)

// notionFileUploadPartMetadata is the name and content type a sent part
// leaves its upload with: a multi_part upload keeps the ones it was made
// with, as Notion's does, and a single_part one settles them from the file
// (notionFileUploadSentMetadata).
func notionFileUploadPartMetadata(
	upload storedProductFileUpload,
	file *filesystem.File,
	contentType string,
) (string, string, error) {
	if upload.Kind == fileUploadKindNotionMulti {
		return upload.OriginalName, upload.ContentType, nil
	}
	name, settled, err := notionFileUploadSentMetadata(upload, file.OriginalName, contentType)
	if err == nil && len(name) > notionFileUploadMaxNameBytes {
		err = notionInvalid(errFileUploadInvalid, fmt.Sprintf("filename must be at most %d bytes.", notionFileUploadMaxNameBytes))
	}
	return name, settled, err
}

func persistNotionProductFileUploadPart(
	app core.App,
	upload storedProductFileUpload,
	partNumber int,
	file *filesystem.File,
	name string,
	contentType string,
) (storedProductFileUpload, bool, error) {
	if file == nil || file.Size <= 0 || file.Size > notionFileUploadMaxPartBytes {
		return upload, false, notionInvalid(errFileUploadInvalid, "Each part must be non-empty and at most 20 MiB.")
	}
	reader, err := file.Reader.Open()
	if err != nil {
		return upload, false, err
	}
	defer reader.Close()
	fSys, err := app.NewFilesystem()
	if err != nil {
		return upload, false, err
	}
	defer fSys.Close()
	storageKey := productFileUploadPrefix(upload.UploadID) + "part-" +
		formatProductFileUploadPartNumber(partNumber) + "-" + security.RandomString(12) + ".bin"
	writer, err := fSys.NewWriter(storageKey, &blob.WriterOptions{ContentType: "application/octet-stream"})
	if err != nil {
		return upload, false, err
	}
	hasher := sha256.New()
	written, copyErr := io.CopyBuffer(
		io.MultiWriter(writer, hasher),
		io.LimitReader(reader, notionFileUploadMaxPartBytes+1),
		make([]byte, productFileDigestBufferSize),
	)
	closeErr := writer.Close()
	if copyErr != nil || closeErr != nil || written != file.Size || written <= 0 ||
		written > notionFileUploadMaxPartBytes {
		_ = fSys.Delete(storageKey)
		return upload, false, errors.Join(errFileUploadInvalid, copyErr, closeErr)
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	now := time.Now().UTC().UnixMilli()
	updated := upload
	duplicate := false
	quotaRelease, err := prepareProductOrganizationStorageAdmission(
		context.Background(), app, upload.WorkspaceID, now,
	)
	if err != nil {
		_ = fSys.Delete(storageKey)
		return upload, false, err
	}
	defer quotaRelease()
	err = app.RunInTransaction(func(txApp core.App) error {
		current, err := loadProductFileUploadByID(txApp, upload.ActorID, upload.UploadID)
		if err != nil {
			return err
		}
		if current.NotionMode == "" || current.Status != fileUploadStatusPending ||
			notionFileUploadPublicStatus(current, now) != "pending" {
			return errFileUploadExpired
		}
		if current.Kind == fileUploadKindNotionSingle {
			if partNumber != 0 || current.NotionPartsTotal != 0 {
				return errFileUploadPartConflict
			}
		} else if current.Kind == fileUploadKindNotionMulti {
			if partNumber < 0 || partNumber >= current.NotionPartsTotal ||
				partNumber < current.NotionPartsTotal-1 && written < notionFileUploadMinPartBytes {
				return notionInvalid(errFileUploadInvalid, "Each part but the last must be at least 5 MiB, and part_number within number_of_parts.")
			}
		} else {
			return notionInvalid(errFileUploadInvalid, "Only a single_part or multi_part upload takes file contents.")
		}
		if current.OriginalName != "" && current.OriginalName != name ||
			current.ContentType != "" && !notionFileUploadSameType(current.ContentType, contentType) {
			return notionInvalid(errFileUploadInvalid, "Every part must have the same filename and content type.")
		}
		authorization, err := authorizeProductFileUpload(
			txApp, current.ActorID, current.WorkspaceID, current.uploadScope(),
		)
		if err != nil {
			return err
		}
		stored, _ := notionFileUploadStoredType(name, contentType)
		if err := authorizeProductFileUploadMetadata(
			txApp, authorization, name, stored,
		); err != nil {
			return err
		}
		existing, err := loadProductFileUploadPart(txApp, current.UploadID, partNumber)
		if err == nil {
			if existing.Size != written || existing.SHA256 != digest {
				return errFileUploadPartConflict
			}
			updated = current
			duplicate = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		total := current.ReceivedBytes + written
		if total <= 0 || total > fileTransferMaxFileBytes ||
			current.NextPart >= max(1, current.NotionPartsTotal) {
			return notionInvalid(errFileUploadInvalid, "The upload is at most 5 GiB and number_of_parts parts.")
		}
		if _, err := growProductOrganizationStorageReservation(
			context.Background(), txApp, current.WorkspaceID, current.UploadID, total, now,
		); err != nil {
			return err
		}
		if _, err := txApp.DB().Insert(productFileUploadPartsTable, dbx.Params{
			"upload_id": current.UploadID, "part_number": partNumber,
			"size": written, "sha256": digest, "storage_key": storageKey,
		}).Execute(); err != nil {
			return err
		}
		result, err := txApp.DB().Update(productFileUploadsTable, dbx.Params{
			"original_name": name, "content_type": contentType,
			"total_size": total, "received_bytes": total,
			"next_part": current.NextPart + 1, "updated_at": now,
		}, dbx.HashExp{
			"upload_id": current.UploadID, "status": fileUploadStatusPending,
			"received_bytes": current.ReceivedBytes, "next_part": current.NextPart,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errFileUploadPartConflict, err)
		}
		current.OriginalName = name
		current.ContentType = contentType
		current.TotalSize = total
		current.ReceivedBytes = total
		current.NextPart++
		current.UpdatedAt = now
		updated = current
		return nil
	})
	if err != nil {
		_ = fSys.Delete(storageKey)
		return upload, false, err
	}
	if duplicate {
		_ = fSys.Delete(storageKey)
	} else {
		notifyProductFileUploadCleanupAfterCommit(app)
	}
	return updated, duplicate, nil
}
