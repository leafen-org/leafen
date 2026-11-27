package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

func claimProductFileUploadPublication(
	app core.App,
	upload storedProductFileUpload,
	digest string,
) (storedProductFileUpload, bool, error) {
	current := storedProductFileUpload{}
	resuming := false
	err := app.RunInTransaction(func(txApp core.App) error {
		loaded, err := loadProductFileUploadByID(txApp, upload.ActorID, upload.UploadID)
		if err != nil {
			return err
		}
		if loaded.Kind != upload.Kind || loaded.RequestID != upload.RequestID ||
			loaded.RequestHash != upload.RequestHash || loaded.WorkspaceID != upload.WorkspaceID {
			return errFileUploadRequestIDReused
		}
		authorization, err := authorizeProductFileUpload(
			txApp, loaded.ActorID, loaded.WorkspaceID, loaded.uploadScope(),
		)
		if err != nil {
			return err
		}
		if err := authorizeProductFileUploadMetadata(
			txApp, authorization, loaded.OriginalName, loaded.ContentType,
		); err != nil {
			return err
		}
		if loaded.Status == fileUploadStatusCompleted {
			current = loaded
			return nil
		}
		if loaded.Status == fileUploadStatusCleanupPending {
			return errFileUploadExpired
		}
		if loaded.Status == fileUploadStatusSettling {
			if loaded.FileID != loaded.UploadID ||
				(digest != "" && loaded.SHA256 != digest) {
				return errFileUploadUnavailable
			}
			current = loaded
			resuming = true
			return nil
		}
		if loaded.Status != fileUploadStatusPending ||
			loaded.ReceivedBytes != loaded.TotalSize {
			return errFileUploadIncomplete
		}
		values := dbx.Params{
			"status": fileUploadStatusSettling, "file_id": loaded.UploadID,
			"updated_at": time.Now().UTC().UnixMilli(), "cleanup_due_at": 0,
		}
		if digest != "" {
			values["sha256"] = digest
			loaded.SHA256 = digest
		}
		updated, err := txApp.DB().Update(productFileUploadsTable, values, dbx.HashExp{
			"actor_id": loaded.ActorID, "request_id": loaded.RequestID,
			"upload_id": loaded.UploadID, "request_hash": loaded.RequestHash,
			"status": fileUploadStatusPending,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := updated.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errFileUploadUnavailable, err)
		}
		loaded.Status = fileUploadStatusSettling
		loaded.FileID = loaded.UploadID
		current = loaded
		return nil
	})
	return current, resuming, err
}

func newProductFileRecord(
	collection *core.Collection,
	upload storedProductFileUpload,
	file *filesystem.File,
	digest string,
) *core.Record {
	record := core.NewRecord(collection)
	record.Id = upload.UploadID
	setWorkspaceFileRecordAuthority(record, upload.WorkspaceID, upload.ActorID)
	record.Set("original_name", upload.OriginalName)
	record.Set("content_type", upload.storedContentType())
	record.Set("size", upload.TotalSize)
	record.Set("sha256", digest)
	record.Set("blob", file)
	return record
}

func publishProductFileRecordOutsideTransaction(app core.App, record *core.Record) error {
	if app.IsTransactional() {
		return errFileUploadUnavailable
	}
	return app.Save(record)
}

func reclaimUncommittedProductFileRecordStorage(app core.App, record *core.Record) error {
	if record == nil || record.Id == "" {
		return errFileUploadUnavailable
	}
	fsys, err := app.NewFilesystem()
	if err != nil {
		return err
	}
	defer fsys.Close()
	return errors.Join(fsys.DeletePrefix(record.BaseFilesPath())...)
}

func findProductFilePublicationRecord(
	app core.App,
	upload storedProductFileUpload,
) (*core.Record, bool, error) {
	record, err := app.FindRecordById(filesCollection, upload.UploadID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return record, err == nil, err
}
