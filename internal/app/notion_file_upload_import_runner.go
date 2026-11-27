package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

const notionFileUploadImportRunnerStoreKey = "leafenNotionFileUploadImportRunner"

type notionFileUploadImportRunner struct {
	app  core.App
	wake chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	runMu  sync.Mutex
}

func newNotionFileUploadImportRunner(app core.App) *notionFileUploadImportRunner {
	return &notionFileUploadImportRunner{app: app, wake: make(chan struct{}, 1)}
}

func (runner *notionFileUploadImportRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel = cancel
	runner.done = make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *notionFileUploadImportRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	cancel, done := runner.cancel, runner.done
	runner.cancel, runner.done = nil, nil
	runner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("notion file upload import: %w", ctx.Err())
	}
}

func (runner *notionFileUploadImportRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *notionFileUploadImportRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait, err := runner.runTurn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeFileUploadFailed, "notion_file_upload.import", err)
			wait = 5 * time.Second
		}
		if wait == 0 {
			continue
		}
		var timer *time.Timer
		var timerChannel <-chan time.Time
		if wait > 0 {
			timer = time.NewTimer(wait)
			timerChannel = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-runner.wake:
		case <-timerChannel:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func (runner *notionFileUploadImportRunner) runTurn(ctx context.Context) (time.Duration, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	now := time.Now().UTC().UnixMilli()
	upload, found, next, err := loadReadyNotionFileUploadImport(ctx, runner.app, now)
	if err != nil {
		return -1, err
	}
	if !found {
		if next == 0 {
			return -1, nil
		}
		wait := time.Duration(next-now) * time.Millisecond
		if wait <= 0 {
			return 0, nil
		}
		return wait, nil
	}
	return 0, runner.process(ctx, upload)
}

func loadReadyNotionFileUploadImport(
	ctx context.Context,
	app core.App,
	now int64,
) (storedProductFileUpload, bool, int64, error) {
	rows := []storedProductFileUpload{}
	err := notionFileUploadImportReadyQuery(app, now).WithContext(ctx).All(&rows)
	if err != nil || len(rows) > 0 {
		if len(rows) > 0 {
			return rows[0], true, 0, err
		}
		return storedProductFileUpload{}, false, 0, err
	}
	var next int64
	err = notionFileUploadImportNextQuery(app).WithContext(ctx).Row(&next)
	return storedProductFileUpload{}, false, next, err
}

func (runner *notionFileUploadImportRunner) process(
	ctx context.Context,
	upload storedProductFileUpload,
) error {
	release := acquireProductFileOperationLock(productFileOperationResumable, upload.UploadID)
	defer release()
	current, err := loadProductFileUploadByID(runner.app, upload.ActorID, upload.UploadID)
	if err != nil || current.Kind != fileUploadKindNotionExternal {
		return err
	}
	if current.Status == fileUploadStatusSettling {
		if _, exists, findErr := findProductFilePublicationRecord(runner.app, current); findErr != nil {
			return findErr
		} else if exists {
			if _, err := settleProductFileUploadCompletion(runner.app, current); err != nil {
				return err
			}
			return completeNotionExternalFileUploadImport(runner.app, current.UploadID)
		}
	}
	plainURL, err := decryptNotionFileURL(runner.app, current.NotionExternalURLCipher)
	if err != nil {
		return failNotionExternalFileUpload(runner.app, current, "internal_error", "file_upload_fetch_failed", "The external file could not be imported.")
	}
	fetched, fetchErr := fetchNotionFile(ctx, runner.app, plainURL)
	if fetchErr != nil {
		return runner.recordFetchFailure(current, fetchErr)
	}
	defer os.Remove(fetched.Path)
	// The upload keeps the content type it was made with, whatever the file
	// says of itself, as Notion's does (seen 2026-11-11).
	contentType := current.ContentType
	if contentType == "" {
		return failNotionExternalFileUpload(runner.app, current, "validation_error", "file_upload_invalid_type", "The external file type is not supported.")
	}
	prepared, err := prepareNotionExternalFileUploadPublication(
		runner.app, current, fetched.Size, fetched.SHA256, contentType,
	)
	if err != nil {
		return err
	}
	file, err := filesystem.NewFileFromPath(fetched.Path)
	if err != nil {
		return err
	}
	file.OriginalName = prepared.OriginalName
	file.Size = prepared.TotalSize
	if _, err := publishDirectProductFileUpload(
		runner.app, prepared, file, fetched.SHA256,
	); err != nil {
		return err
	}
	return completeNotionExternalFileUploadImport(runner.app, prepared.UploadID)
}

func (runner *notionFileUploadImportRunner) recordFetchFailure(
	upload storedProductFileUpload,
	fetchErr error,
) error {
	var failure *notionFileFetchFailure
	if errors.As(fetchErr, &failure) && failure.kind == "transient" &&
		upload.NotionImportAttempts < 3 {
		next := time.Now().UTC().Add(time.Duration(5<<upload.NotionImportAttempts) * time.Second).UnixMilli()
		if next < notionFileUploadExpiresAt(upload) {
			_, err := runner.app.DB().NewQuery(`
				UPDATE {{leafen_file_uploads}}
				SET [[notion_import_attempts]]=[[notion_import_attempts]]+1,
					[[notion_import_next_at]]={:next}, [[updated_at]]={:now}
				WHERE [[upload_id]]={:uploadId} AND [[status]]={:pending}
			`).Bind(dbx.Params{
				"next": next, "now": time.Now().UTC().UnixMilli(),
				"uploadId": upload.UploadID, "pending": fileUploadStatusPending,
			}).Execute()
			return err
		}
	}
	code := "file_upload_fetch_failed"
	message := "The external file could not be imported."
	if errors.As(fetchErr, &failure) && failure.kind == "permanent" {
		code = "file_upload_invalid_url"
		message = "The external file URL or file is invalid."
	}
	return failNotionExternalFileUpload(
		runner.app, upload, "validation_error", code, message,
	)
}

func notifyNotionFileUploadImportAfterCommit(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(notionFileUploadImportRunnerStoreKey).(*notionFileUploadImportRunner); ok {
			runner.notify()
		}
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				notify()
			}
			return nil
		})
		return
	}
	notify()
}
