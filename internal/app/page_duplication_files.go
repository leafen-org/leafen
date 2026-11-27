package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const productFileOperationPageDuplication = "page-duplication"

type storedPageDuplicationFile struct {
	JobID            string `db:"job_id"`
	SourceFileID     string `db:"source_file_id"`
	TargetFileID     string `db:"target_file_id"`
	SourcePageID     string `db:"source_page_id"`
	TargetPageID     string `db:"target_page_id"`
	Status           string `db:"status"`
	Bytes            int64  `db:"bytes"`
	TargetStorageKey string `db:"target_storage_key"`
}

func advancePageDuplicationFiles(
	ctx context.Context,
	app core.App,
	expected storedPageDuplicationJob,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state, err := loadPageDuplicationFileState(app, expected.ID)
	if err != nil {
		return err
	}
	if !state.InventoryComplete {
		return app.RunInTransaction(func(txApp core.App) error {
			job, err := validatePageDuplicationFileJob(txApp, expected.ID, pageDuplicationPhaseCopying)
			if err != nil {
				return err
			}
			current, err := loadPageDuplicationFileState(txApp, job.ID)
			if err != nil || current.InventoryComplete {
				return err
			}
			// Windows and files go on in one turn while it stays short: a page
			// a turn spent most of a big tree's copy waiting between turns
			// (2026-11-05: 608 pages, at least 150 s).
			deadline := time.Now().Add(pageDuplicationTurnBudget)
			for !current.InventoryComplete && time.Now().Before(deadline) {
				if err := inventoryPageDuplicationFilesWindow(txApp, job, current); err != nil {
					return err
				}
				if current, err = loadPageDuplicationFileState(txApp, job.ID); err != nil {
					return err
				}
			}
			return reschedulePageDuplication(txApp, job)
		})
	}
	deadline := time.Now().Add(pageDuplicationTurnBudget)
	for {
		task, err := nextPageDuplicationFile(app, expected.ID)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return err
		}
		if err := copyPageDuplicationFile(ctx, app, expected, task, pageDuplicationPhaseCopying, func(app core.App) (storedPageDuplicationFile, error) {
			return nextPageDuplicationFile(app, expected.ID)
		}); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return nil
		}
	}
	return app.RunInTransaction(func(txApp core.App) error {
		job, err := validatePageDuplicationFileJob(txApp, expected.ID, pageDuplicationPhaseCopying)
		if err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_page_duplication_file_state}}
			SET [[files_complete]]=1,[[updated_at]]={:now}
			WHERE [[job_id]]={:jobId} AND [[inventory_complete]]=1 AND [[files_complete]]=0
				AND NOT EXISTS (SELECT 1 FROM {{leafen_page_duplication_files}}
					WHERE [[job_id]]={:jobId} AND [[status]]!='copied' LIMIT 1)
		`).Bind(dbx.Params{
			"jobId": job.ID, "now": time.Now().UTC().UnixMilli(),
		}).Execute()
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errPageDuplicationConflict, err)
		}
		return reschedulePageDuplication(txApp, job)
	})
}

func inventoryPageDuplicationFilesWindow(
	app core.App,
	job storedPageDuplicationJob,
	state storedPageDuplicationFileState,
) error {
	references := []pageDuplicationFileReference{}
	inventoryComplete := false
	var closedItem *pageDuplicationFileReference
	if state.InventoryPageOpen {
		if err := app.DB().NewQuery(`
			SELECT [[item]].[[source_page_id]],[[item]].[[target_page_id]],
				[[reference]].[[owner_kind]],[[reference]].[[owner_id]],[[reference]].[[property_id]],
				[[reference]].[[file_id]] AS [[source_file_id]]
			FROM {{leafen_page_duplication_items}} [[item]]
				INDEXED BY [[idx_leafen_page_duplication_file_inventory]]
			JOIN {{leafen_file_references}} [[reference]]
				INDEXED BY [[idx_leafen_file_references_owner]]
				ON [[reference]].[[workspace_id]]={:workspaceId}
				AND [[reference]].[[page_id]]=[[item]].[[source_page_id]]
			WHERE [[item]].[[job_id]]={:jobId}
				AND [[item]].[[source_page_id]]={:sourcePageCursor}
				AND ([[reference]].[[owner_kind]],[[reference]].[[owner_id]],
					[[reference]].[[property_id]],[[reference]].[[file_id]]) >
					({:ownerKindCursor},{:ownerIdCursor},{:propertyIdCursor},{:fileIdCursor})
			ORDER BY [[reference]].[[owner_kind]],[[reference]].[[owner_id]],
				[[reference]].[[property_id]],[[reference]].[[file_id]]
			LIMIT {:limit}
		`).Bind(dbx.Params{
			"jobId": job.ID, "workspaceId": job.sourceWorkspace(),
			"sourcePageCursor": state.SourcePageCursor,
			"ownerKindCursor":  state.OwnerKindCursor, "ownerIdCursor": state.OwnerIDCursor,
			"propertyIdCursor": state.PropertyIDCursor, "fileIdCursor": state.FileIDCursor,
			"limit": maxPageDuplicationItemsPerTurn + 1,
		}).All(&references); err != nil {
			return err
		}
	} else {
		items := []pageDuplicationFileReference{}
		if err := app.DB().NewQuery(`
			SELECT [[source_page_id]],[[target_page_id]]
			FROM {{leafen_page_duplication_items}}
				INDEXED BY [[idx_leafen_page_duplication_file_inventory]]
			WHERE [[job_id]]={:jobId} AND [[source_page_id]]>{:sourcePageCursor}
			ORDER BY [[source_page_id]] LIMIT 2
		`).Bind(dbx.Params{
			"jobId": job.ID, "sourcePageCursor": state.SourcePageCursor,
		}).All(&items); err != nil {
			return err
		}
		// The page a copy stops at brings no files of its body
		// (pageDuplicationStopsAt).
		if len(items) > 0 && pageDuplicationStopsAt(job, items[0].SourcePageID) {
			closedItem = &items[0]
		} else if len(items) > 0 {
			closedItem = &items[0]
			if err := app.DB().NewQuery(`
			SELECT {:sourcePageId} AS [[source_page_id]],{:targetPageId} AS [[target_page_id]],
				[[owner_kind]],[[owner_id]],[[property_id]],[[file_id]] AS [[source_file_id]]
			FROM {{leafen_file_references}}
				INDEXED BY [[idx_leafen_file_references_owner]]
			WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:sourcePageId}
			ORDER BY [[owner_kind]],[[owner_id]],[[property_id]],[[file_id]]
			LIMIT {:limit}
		`).Bind(dbx.Params{
				"workspaceId":  job.sourceWorkspace(),
				"sourcePageId": items[0].SourcePageID, "targetPageId": items[0].TargetPageID,
				"limit": maxPageDuplicationItemsPerTurn + 1,
			}).All(&references); err != nil {
				return err
			}
		}
		inventoryComplete = len(items) == 0 ||
			(len(items) == 1 && len(references) <= maxPageDuplicationItemsPerTurn)
	}
	hasMore := len(references) > maxPageDuplicationItemsPerTurn
	if hasMore {
		references = references[:maxPageDuplicationItemsPerTurn]
	}
	if err := storePageDuplicationReadableFiles(app, job, references); err != nil {
		return err
	}
	next := pageDuplicationFileReference{SourcePageID: state.SourcePageCursor}
	if len(references) > 0 {
		next = references[len(references)-1]
	} else if closedItem != nil {
		// A page without references still advances the durable page frontier.
		next = *closedItem
	}
	pageOpen := hasMore && next.SourceFileID != ""
	if !pageOpen {
		next.OwnerKind = ""
		next.OwnerID = ""
		next.PropertyID = ""
		next.SourceFileID = ""
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_file_state}}
		SET [[source_page_cursor]]={:sourcePageCursor},[[owner_kind_cursor]]={:ownerKindCursor},
			[[owner_id_cursor]]={:ownerIdCursor},[[property_id_cursor]]={:propertyIdCursor},
			[[file_id_cursor]]={:fileIdCursor},[[inventory_page_open]]={:pageOpen},
			[[inventory_complete]]={:complete},[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[inventory_complete]]=0
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourcePageCursor": next.SourcePageID,
		"ownerKindCursor": next.OwnerKind, "ownerIdCursor": next.OwnerID,
		"propertyIdCursor": next.PropertyID, "fileIdCursor": next.SourceFileID,
		"pageOpen": pageOpen, "complete": inventoryComplete,
		"now": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

func storePageDuplicationFileMappings(
	app core.App,
	jobID string,
	references []pageDuplicationFileReference,
) error {
	unique := map[string]pageDuplicationFileReference{}
	for _, reference := range references {
		if reference.SourceFileID == "" {
			continue
		}
		if !workspaceExportFileRecordIDPattern.MatchString(reference.SourceFileID) {
			return errPageDuplicationConflict
		}
		if _, exists := unique[reference.SourceFileID]; !exists {
			unique[reference.SourceFileID] = reference
		}
	}
	if len(unique) == 0 {
		return nil
	}
	params := dbx.Params{"jobId": jobID, "status": "pending", "now": time.Now().UTC().UnixMilli()}
	values := make([]string, 0, len(unique))
	sourceBindings := make([]string, 0, len(unique))
	index := 0
	for sourceID, reference := range unique {
		prefix := fmt.Sprintf("file%d", index)
		params[prefix+"Source"] = sourceID
		params[prefix+"Target"] = core.GenerateDefaultRandomId()
		params[prefix+"SourcePage"] = reference.SourcePageID
		params[prefix+"TargetPage"] = reference.TargetPageID
		values = append(values, fmt.Sprintf(
			"({:jobId},{:%sSource},{:%sTarget},{:%sSourcePage},{:%sTargetPage},{:status},0,'',{:now},{:now})",
			prefix, prefix, prefix, prefix,
		))
		sourceBindings = append(sourceBindings, "{:"+prefix+"Source}")
		index++
	}
	if _, err := app.DB().NewQuery(`
		INSERT OR IGNORE INTO {{leafen_page_duplication_files}}
			([[job_id]],[[source_file_id]],[[target_file_id]],[[source_page_id]],[[target_page_id]],
			 [[status]],[[bytes]],[[target_storage_key]],[[created_at]],[[updated_at]])
		VALUES ` + strings.Join(values, ","),
	).Bind(params).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		INSERT OR IGNORE INTO {{leafen_page_duplication_identity_map}}
			([[job_id]],[[kind]],[[source_id]],[[target_id]],[[source_page_id]],[[target_page_id]],[[created_at]])
		SELECT [[job_id]],'file',[[source_file_id]],[[target_file_id]],
			[[source_page_id]],[[target_page_id]],[[created_at]]
		FROM {{leafen_page_duplication_files}}
		WHERE [[job_id]]={:jobId} AND [[source_file_id]] IN (` + strings.Join(sourceBindings, ",") + `)
	`).Bind(params).Execute()
	return err
}

func nextPageDuplicationFile(app core.App, jobID string) (storedPageDuplicationFile, error) {
	task := storedPageDuplicationFile{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[source_file_id]],[[target_file_id]],[[source_page_id]],[[target_page_id]],
			[[status]],[[bytes]],[[target_storage_key]]
		FROM {{leafen_page_duplication_files}}
		WHERE [[job_id]]={:jobId} AND [[status]]='pending'
		ORDER BY [[source_file_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID}).One(&task)
	return task, err
}

// copyPageDuplicationFile copies the next file of a copy in a phase: its
// files' turn, or the top's going first (page_duplication_top_first.go).
func copyPageDuplicationFile(
	ctx context.Context,
	app core.App,
	expected storedPageDuplicationJob,
	task storedPageDuplicationFile,
	phase string,
	next func(core.App) (storedPageDuplicationFile, error),
) error {
	release := acquireProductFileOperationLock(productFileOperationPageDuplication, task.TargetFileID)
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := next(app)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil || current.SourceFileID != task.SourceFileID || current.TargetFileID != task.TargetFileID {
		return errors.Join(errPageDuplicationConflict, err)
	}
	// A visitor's copy reads the site's files as the system
	// (system_job_access.go), the site still open to it (sourceReadable).
	reader := pageAccessSubject(personAccessSubject(expected.ActorID))
	if expected.SourcePublicLinkID != "" {
		if err := expected.sourceReadable(app); err != nil {
			return err
		}
		workspace, err := app.FindRecordById(workspacesCollection, expected.sourceWorkspace())
		if err != nil {
			return errors.Join(errPageDuplicationConflict, err)
		}
		reader = systemJobSubjectFor(workspace)
	}
	target, size, err := copyFileIntoWorkspace(
		app, reader, expected.ActorID, expected.sourceWorkspace(), expected.WorkspaceID,
		"page-duplication-file-"+expected.ID+"-"+task.TargetFileID, task.SourceFileID, task.TargetFileID,
	)
	if err != nil {
		return err
	}
	return app.RunInTransaction(func(txApp core.App) error {
		job, err := validatePageDuplicationFileJob(txApp, expected.ID, phase)
		if err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_page_duplication_files}}
			SET [[status]]='copied',[[bytes]]={:bytes},[[target_storage_key]]={:storageKey},
				[[updated_at]]={:now}
			WHERE [[job_id]]={:jobId} AND [[source_file_id]]={:sourceFileId}
				AND [[target_file_id]]={:targetFileId} AND [[status]]='pending'
		`).Bind(dbx.Params{
			"jobId": job.ID, "sourceFileId": task.SourceFileID,
			"targetFileId": task.TargetFileID, "bytes": size,
			"storageKey": productFileStorageKey(target), "now": time.Now().UTC().UnixMilli(),
		}).Execute()
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errPageDuplicationConflict, err)
		}
		if err := cancelProductFileReferenceCleanup(ctx, txApp, target.Id); err != nil {
			return err
		}
		// What waits on this file comes now: a brought emoji, the top's icon or cover (page_duplication_copied_file.go).
		if err := settlePageDuplicationCopiedFile(txApp, job, task); err != nil {
			return err
		}
		return reschedulePageDuplication(txApp, job)
	})
}

// copyFileIntoWorkspace copies a file of sourceWorkspaceID, as reader may
// read it, into targetWorkspaceID under targetFileID, added by actorID, with
// its size: a copy into another workspace names its own copies of the files.
func copyFileIntoWorkspace(
	app core.App, reader pageAccessSubject, actorID, sourceWorkspaceID, targetWorkspaceID, requestID, sourceFileID, targetFileID string,
) (*core.Record, int64, error) {
	source, err := reader.readWorkspaceFile(app, sourceFileID)
	if err != nil || !workspaceFileRecordBelongsTo(source, sourceWorkspaceID) {
		return nil, 0, errors.Join(errPageDuplicationConflict, err)
	}
	fsys, err := app.NewFilesystem()
	if err != nil {
		return nil, 0, err
	}
	defer fsys.Close()
	if err := verifyProductFileStorageIdentity(app, fsys, source); err != nil {
		return nil, 0, err
	}
	file, err := fsys.GetReuploadableFile(productFileStorageKey(source), false)
	if err != nil {
		return nil, 0, err
	}
	input := fileUploadPrepareInput{
		RequestID: requestID, WorkspaceID: targetWorkspaceID, Name: source.GetString("original_name"),
		Size: int64(source.GetFloat("size")), ContentType: source.GetString("content_type"),
	}
	if err := normalizeProductFileUploadInput(&input); err != nil {
		return nil, 0, errPageDuplicationConflict
	}
	digest := source.GetString("sha256")
	upload, _, err := admitDirectProductFileUploadWithID(app, actorID, input, digest, targetFileID)
	if err != nil {
		return nil, 0, err
	}
	var target *core.Record
	if upload.Status == fileUploadStatusCompleted {
		target, err = completedDirectProductFileUpload(app, upload)
	} else {
		target, err = publishDirectProductFileUpload(app, upload, file, digest)
	}
	if err != nil || target == nil || target.Id != targetFileID ||
		!workspaceFileRecordBelongsTo(target, targetWorkspaceID) ||
		target.GetString("sha256") != digest || productFileStorageKey(target) == productFileStorageKey(source) {
		return nil, 0, errors.Join(errPageDuplicationConflict, err)
	}
	// A copied video keeps its still; one the copy misses is made when next shown.
	_ = copyProductFilePoster(fsys, source, target)
	return target, input.Size, nil
}

func pageDuplicationTargetPagesClean(app core.App, jobID string) (bool, error) {
	var pending int
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_page_duplication_items}}
			WHERE [[job_id]]={:jobId} AND [[page_created]]=1 AND [[cleaned]]=0 LIMIT 1)
	`).Bind(dbx.Params{"jobId": jobID}).Row(&pending)
	return pending == 0, err
}

func pageDuplicationFilesClean(app core.App, jobID string) (bool, error) {
	var pending int
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_page_duplication_files}}
			WHERE [[job_id]]={:jobId} AND [[status]]!='cleaned' LIMIT 1)
	`).Bind(dbx.Params{"jobId": jobID}).Row(&pending)
	return pending == 0, err
}

func cleanPageDuplicationFile(
	ctx context.Context,
	app core.App,
	expected storedPageDuplicationJob,
) error {
	task := storedPageDuplicationFile{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[source_file_id]],[[target_file_id]],[[source_page_id]],[[target_page_id]],
			[[status]],[[bytes]],[[target_storage_key]]
		FROM {{leafen_page_duplication_files}}
		WHERE [[job_id]]={:jobId} AND [[status]]!='cleaned'
		ORDER BY [[source_file_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": expected.ID}).One(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	release := acquireProductFileOperationLock(productFileOperationPageDuplication, task.TargetFileID)
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	// A brought emoji goes before its picture (page_duplication_custom_emoji.go).
	if err := dropPageDuplicationCarriedCustomEmojis(app, expected, task.TargetFileID); err != nil {
		return err
	}
	record, findErr := app.FindRecordById(filesCollection, task.TargetFileID)
	if findErr == nil {
		if !workspaceFileRecordBelongsTo(record, expected.WorkspaceID) {
			return errPageDuplicationConflict
		}
		referenced, err := findActiveProductFileReference(app, record.Id)
		if err != nil || referenced {
			return errors.Join(errPageDuplicationConflict, err)
		}
		if err := deleteProductFileStorage(app, record); err != nil {
			return err
		}
	} else if !errors.Is(findErr, sql.ErrNoRows) {
		return findErr
	} else {
		collection, err := app.FindCollectionByNameOrId(filesCollection)
		if err != nil {
			return err
		}
		placeholder := core.NewRecord(collection)
		placeholder.Id = task.TargetFileID
		if err := reclaimUncommittedProductFileRecordStorage(app, placeholder); err != nil {
			return err
		}
	}
	return app.RunInTransaction(func(txApp core.App) error {
		job, err := validatePageDuplicationFileJob(txApp, expected.ID, pageDuplicationPhaseCleaning)
		if err != nil {
			return err
		}
		referenced, err := findActiveProductFileReference(txApp, task.TargetFileID)
		if err != nil || referenced {
			return errors.Join(errPageDuplicationConflict, err)
		}
		if _, err := txApp.DB().Delete(filesCollection, dbx.HashExp{
			"id": task.TargetFileID, "workspace": job.WorkspaceID,
		}).Execute(); err != nil {
			return err
		}
		reservation, err := loadProductOrganizationStorageReservation(
			ctx, txApp, task.TargetFileID,
		)
		if err == nil {
			if reservation.WorkspaceID != job.WorkspaceID {
				return errPageDuplicationConflict
			}
			if err := releaseProductOrganizationStorage(
				ctx, txApp, task.TargetFileID, time.Now().UTC().UnixMilli(),
			); err != nil {
				return err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for _, table := range []string{productFileReferenceCleanupsTable, productFileUploadsTable} {
			column := "file_id"
			if table == productFileUploadsTable {
				column = "upload_id"
			}
			if _, err := txApp.DB().NewQuery(fmt.Sprintf(
				"DELETE FROM {{%s}} WHERE [[%s]]={:fileId}", table, column,
			)).Bind(dbx.Params{"fileId": task.TargetFileID}).Execute(); err != nil {
				return err
			}
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_page_duplication_files}}
			SET [[status]]='cleaned',[[updated_at]]={:now}
			WHERE [[job_id]]={:jobId} AND [[source_file_id]]={:sourceFileId}
				AND [[target_file_id]]={:targetFileId} AND [[status]]!='cleaned'
		`).Bind(dbx.Params{
			"jobId": job.ID, "sourceFileId": task.SourceFileID,
			"targetFileId": task.TargetFileID, "now": time.Now().UTC().UnixMilli(),
		}).Execute()
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errPageDuplicationConflict, err)
		}
		return reschedulePageDuplication(txApp, job)
	})
}
