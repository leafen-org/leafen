package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/reearth/ygo/crdt"
)

// A file an imported page, database or row owns, an icon, a cover or a file
// in a files value, is recovered by reading its owner again from Notion for a
// fresh address, copying it, and putting it where the import would have: on
// the page, on the database's data source (and on the page a root database
// became; a full-page database's cover only there), on the row page, or at
// the end of the row's files value. When the owner is gone the file can't be
// recovered.

var errNotionImportOwnedFileOwnerGone = errors.New(errorCodeNotionImportFilePermanent)

type notionImportOwnedFileTarget struct {
	// PageID is the page that takes the icon or cover.
	PageID string
	// RowID is the row that takes the icon, the cover or the file.
	RowID string
	// DataSourceID is the data source that takes a database icon, or an
	// inline database's cover.
	DataSourceID string
	// PropertyID is the files property that takes the file.
	PropertyID string
	// BlockID is the callout that takes the icon.
	BlockID string
	Cover   bool
}

// refreshNotionImportOwnedFileURL reads a file's owner again for a fresh
// address of the file.
func refreshNotionImportOwnedFileURL(ctx context.Context, client notionProviderClient, sourceID string) (string, error) {
	if suffix, owner := notionImportPresentationFileTarget(sourceID); suffix != "" {
		rawURL, _, err := refreshNotionImportPresentationFileURL(ctx, client, suffix, owner)
		return rawURL, err
	}
	rawURL, _, err := refreshNotionImportPropertyFileURL(ctx, client, sourceID)
	return rawURL, err
}

// lookupNotionImportMapping returns what an imported source became, while
// it still exists.
func lookupNotionImportMapping(app core.App, jobID, sourceID, kind string) (string, error) {
	table := map[string]string{
		"page": "leafen_pages", "row": "leafen_database_rows", "data_source": "leafen_data_sources",
		"property": "leafen_database_properties", "block": "leafen_blocks",
	}[kind]
	nativeID := ""
	err := app.DB().NewQuery(`
		SELECT [[mapping]].[[native_id]] FROM {{leafen_notion_import_mappings}} [[mapping]]
		WHERE [[mapping]].[[job_id]] = {:jobId} AND [[mapping]].[[source_id]] = {:sourceId}
			AND [[mapping]].[[kind]] = {:kind}
			AND EXISTS (SELECT 1 FROM ` + app.DB().QuoteSimpleTableName(table) + ` [[native]]
				WHERE [[native]].[[id]] = [[mapping]].[[native_id]])
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": sourceID, "kind": kind}).Row(&nativeID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return nativeID, err
}

// resolveNotionImportOwnedFileTarget finds what now holds the place of a
// file's owner.
func resolveNotionImportOwnedFileTarget(app core.App, jobID, sourceID string) (notionImportOwnedFileTarget, error) {
	target := notionImportOwnedFileTarget{}
	if suffix, owner := notionImportPresentationFileTarget(sourceID); suffix != "" {
		target.Cover = suffix == notionPresentationPageCover || suffix == notionPresentationDatabaseCover
		var err error
		if suffix == notionPresentationBlockIcon {
			if target.BlockID, err = lookupNotionImportMapping(app, jobID, owner, "block"); err == nil && target.BlockID == "" {
				err = errNotionImportOwnedFileOwnerGone
			}
			return target, err
		}
		if suffix == notionPresentationDatabaseIcon || suffix == notionPresentationDatabaseCover {
			target.PageID, err = lookupNotionImportMapping(app, jobID, notionDatabaseRootPageSourceID(owner), "page")
			// A database without a page of its own, an inline one, keeps its
			// cover on its data source.
			if err == nil && (!target.Cover || target.PageID == "") {
				target.DataSourceID, err = lookupNotionImportMapping(app, jobID, owner, "data_source")
			}
		} else if target.PageID, err = lookupNotionImportMapping(app, jobID, owner, "page"); err == nil && target.PageID == "" {
			target.RowID, err = lookupNotionImportMapping(app, jobID, owner, "row")
		}
		if err == nil && target.PageID == "" && target.RowID == "" && target.DataSourceID == "" {
			err = errNotionImportOwnedFileOwnerGone
		}
		return target, err
	}
	rowSourceID, notionPropertyID, _, ok := notionImportPropertyFileTarget(sourceID)
	if !ok {
		return target, errNotionImportOwnedFileOwnerGone
	}
	databaseID := ""
	if err := app.DB().NewQuery(`
		SELECT [[parent_source_id]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[kind]] = 'row'
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": rowSourceID}).Row(&databaseID); errors.Is(err, sql.ErrNoRows) {
		return target, errNotionImportOwnedFileOwnerGone
	} else if err != nil {
		return target, err
	}
	var err error
	if target.RowID, err = lookupNotionImportMapping(app, jobID, rowSourceID, "row"); err != nil {
		return target, err
	}
	if target.PropertyID, err = lookupNotionImportMapping(
		app, jobID, notionPropertySourceID(databaseID, notionPropertyID), "property",
	); err != nil {
		return target, err
	}
	if target.RowID == "" || target.PropertyID == "" {
		return target, errNotionImportOwnedFileOwnerGone
	}
	return target, nil
}

// retryNotionImportOwnedFile recovers a file a page, database or row owns.
func (r *durableJobRunner) retryNotionImportOwnedFile(
	event *core.RequestEvent,
	actorID string,
	job storedJob,
	payload notionImportJobPayload,
	task storedNotionFileTask,
	client notionProviderClient,
) error {
	ctx := event.Request.Context()
	rawURL, refreshErr := refreshNotionImportOwnedFileURL(ctx, client, task.SourceID)
	if err := authorizeNotionImportPublication(event.App, actorID, job.WorkspaceID); isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	} else if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportFileCopyRequired, "Couldn't check the file recovery permission.", err)
	}
	if refreshErr != nil {
		return apiError(event, http.StatusConflict, errorCodeNotionImportFileRefreshFailed, "Couldn't read the file from Notion again.")
	}
	fetched, err := fetchNotionFile(ctx, event.App, rawURL)
	if err != nil {
		return retryNotionFileFetchError(event, job, payload, task, err)
	}
	defer os.Remove(fetched.Path)
	suffix, _ := notionImportPresentationFileTarget(task.SourceID)
	if !notionManagedFileContentCompatible(task.Kind, graphFileBlockContent{ContentType: fetched.ContentType}) ||
		suffix != "" && !strings.HasPrefix(strings.ToLower(fetched.ContentType), "image/") {
		return apiError(event, http.StatusConflict, errorCodeNotionImportFilePermanent, "The Notion source file isn't valid yet.")
	}
	outcome, err := r.commitRecoveredNotionOwnedFile(ctx, event.App, actorID, job, payload, task, fetched)
	switch {
	case errors.Is(err, errNotionImportOwnedFileOwnerGone):
		return apiError(event, http.StatusConflict, errorCodeNotionImportFilePermanent,
			"The page, database or row this file belonged to is gone, so it can't be recovered.")
	case isPermissionDenied(err):
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied,
			"You can't change the page this file belongs to.")
	case err != nil:
		if quotaError, ok := organizationStorageAPIError(event, err, "notion_import.file", "recover-quota"); ok {
			return quotaError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportFileCopyRequired, "Couldn't recover the file.", err)
	}
	return event.JSON(http.StatusOK, outcome)
}

// commitRecoveredNotionOwnedFile keeps a recovered owned file and puts it on
// its owner, as the person retrying it, through app, the request's
// (request_write_stop.go).
func (r *durableJobRunner) commitRecoveredNotionOwnedFile(
	ctx context.Context,
	app core.App,
	actorID string,
	job storedJob,
	payload notionImportJobPayload,
	task storedNotionFileTask,
	fetched notionFetchedFile,
) (notionImportFileOutcome, error) {
	target, err := resolveNotionImportOwnedFileTarget(r.app, job.ID, task.SourceID)
	if err != nil {
		return notionImportFileOutcome{}, err
	}
	upload, err := filesystem.NewFileFromPath(fetched.Path)
	if err != nil {
		return notionImportFileOutcome{}, err
	}
	nowMillis := time.Now().UTC().UnixMilli()
	quotaRelease, err := prepareProductOrganizationStorageAdmission(ctx, r.app, job.WorkspaceID, nowMillis)
	if err != nil {
		return notionImportFileOutcome{}, err
	}
	defer quotaRelease()

	// store keeps the file and puts it on a page or row page; the data source
	// and files value take it through a content change, after which a root
	// database's page takes it in a change of its own.
	graph := target.DataSourceID != "" || target.PropertyID != "" || target.BlockID != ""
	store := func(txApp core.App) (*core.Record, error) {
		record, err := storeRecoveredNotionFile(txApp, job, payload, task, upload, fetched, nowMillis)
		if err != nil || record == nil {
			return record, err
		}
		if target.PageID != "" && !graph {
			if err := attachNotionImportRecoveredPageFile(txApp, actorID, target, record.Id); err != nil {
				return nil, err
			}
		}
		if target.RowID != "" && target.PropertyID == "" {
			row, err := authorizeDatabaseRowPage(txApp, actorID, target.RowID, permissionWorkspaceWrite)
			if err != nil {
				return nil, err
			}
			if target.Cover {
				if err := applyNotionImportRowCover(txApp, target.RowID, row.WorkspaceID, actorID, record.Id); err != nil {
					return nil, err
				}
			} else if err := updateDatabaseRowPagePresentation(
				txApp, target.RowID, row.WorkspaceID, row.ContainerPageID, actorID, record.Id, "image",
			); err != nil {
				return nil, err
			}
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_import_reports}}
			SET [[files_copied]] = [[files_copied]] + 1,
				[[files_omitted]] = CASE WHEN [[files_omitted]] > 0 THEN [[files_omitted]] - 1 ELSE 0 END,
				[[updated]] = {:updated}
			WHERE [[job_id]] = {:jobId}
		`).Bind(dbx.Params{"jobId": job.ID, "updated": time.Now().UTC().UnixMilli()}).Execute()
		return record, err
	}
	if !graph {
		err = app.RunInTransaction(func(txApp core.App) error {
			_, err := store(txApp)
			return err
		})
	} else {
		err = r.commitRecoveredNotionGraphFile(ctx, actorID, job.ID+":"+task.SourceID, target, store)
		if err == nil && target.PageID != "" {
			err = app.RunInTransaction(func(txApp core.App) error {
				stored, err := loadNotionFileTask(txApp, job.ID, task.SourceID)
				if err != nil || stored.FileID == "" {
					return err
				}
				return attachNotionImportRecoveredPageFile(txApp, actorID, target, stored.FileID)
			})
		}
	}
	if err != nil {
		return notionImportFileOutcome{}, err
	}
	task, err = loadNotionFileTask(r.app, job.ID, task.SourceID)
	if err != nil {
		return notionImportFileOutcome{}, err
	}
	return notionImportFileOutcomeFromStored(r.app, job.ID, task), nil
}

// commitRecoveredNotionGraphFile puts a recovered file on a data source's
// icon or cover or at the end of a row's files value through the content
// change of the page that holds them.
func (r *durableJobRunner) commitRecoveredNotionGraphFile(
	ctx context.Context,
	actorID, fileKey string,
	target notionImportOwnedFileTarget,
	store func(core.App) (*core.Record, error),
) error {
	pageID := ""
	var err error
	if target.BlockID != "" {
		err = r.app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_blocks}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": target.BlockID}).Row(&pageID)
	} else if target.DataSourceID != "" {
		err = r.app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": target.DataSourceID}).Row(&pageID)
	} else {
		err = r.app.DB().NewQuery(`
			SELECT [[source]].[[page_id]] FROM {{leafen_database_rows}} [[row]]
			JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[row]].[[data_source_id]]
			WHERE [[row]].[[id]] = {:id}
		`).Bind(dbx.Params{"id": target.RowID}).Row(&pageID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return errNotionImportOwnedFileOwnerGone
	}
	if err != nil {
		return err
	}
	_, err = r.ygo.applyContentGraphMutationContext(ctx, actorID, pageID, graphMutationInput{
		MutationID: "notion-import-file:" + notionImportOwnedFileMutationKey(fileKey),
	}, contentGraphMutationHooks{
		Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
			record, err := store(txApp)
			if err != nil || record == nil {
				return contentGraphMutationPreparation{Deferred: record == nil}, err
			}
			operation, err := recoveredNotionFileOperation(txApp, target, record)
			if err != nil {
				return contentGraphMutationPreparation{}, err
			}
			return contentGraphMutationPreparation{Operations: []graphOperation{operation}}, nil
		},
	})
	return err
}

// notionImportOwnedFileMutationKey names one file's recovery change within
// the identifier length a content change takes.
func notionImportOwnedFileMutationKey(fileKey string) string {
	digest := sha256.Sum256([]byte(fileKey))
	return hex.EncodeToString(digest[:16])
}

// recoveredNotionFileOperation is the content change that puts a recovered
// file on a data source's icon or cover or at the end of a row's files value.
func recoveredNotionFileOperation(app core.App, target notionImportOwnedFileTarget, record *core.Record) (graphOperation, error) {
	if target.BlockID != "" {
		return recoveredNotionBlockIconOperation(app, target.BlockID, record)
	}
	if target.DataSourceID != "" {
		source := struct {
			Title string `db:"title"`
		}{}
		if err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": target.DataSourceID}).One(&source); err != nil {
			return graphOperation{}, err
		}
		dataSource := &graphDataSource{ID: target.DataSourceID, Title: source.Title}
		image, iconType := record.Id, graphDataSourceIconImage
		if target.Cover {
			dataSource.Cover = &image
		} else {
			dataSource.Icon, dataSource.IconType = &image, &iconType
		}
		return graphOperation{Kind: graphOperationKindUpsertDataSource, DataSource: dataSource}, nil
	}
	row := struct {
		DataSourceID    string `db:"data_source_id"`
		Position        string `db:"position"`
		SubitemParentID string `db:"subitem_parent_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[data_source_id]], [[position]], [[subitem_parent_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": target.RowID}).One(&row); err != nil {
		return graphOperation{}, err
	}
	current := ""
	if err := app.DB().NewQuery(`
		SELECT [[value]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}
	`).Bind(dbx.Params{"rowId": target.RowID, "propertyId": target.PropertyID}).Row(&current); err != nil &&
		!errors.Is(err, sql.ErrNoRows) {
		return graphOperation{}, err
	}
	items := []any{}
	if current != "" && json.Unmarshal([]byte(current), &items) != nil {
		items = []any{}
	}
	items = append(items, graphFileValueItemFromManaged(canonicalGraphFileContent(record)))
	encoded, err := json.Marshal(items)
	if err != nil {
		return graphOperation{}, err
	}
	var parent *string
	if row.SubitemParentID != "" {
		value := row.SubitemParentID
		parent = &value
	}
	return graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
		ID: target.RowID, DataSourceID: row.DataSourceID, Position: row.Position, SubitemParentID: parent,
		Values: map[string]graphRowValue{target.PropertyID: {Type: graphValueTypeFiles, Value: encoded}},
	}}, nil
}

// attachNotionImportRecoveredPageFile sets a page's icon or cover to a
// recovered image, as a page presentation change by the person retrying it.
func attachNotionImportRecoveredPageFile(app core.App, actorID string, target notionImportOwnedFileTarget, fileID string) error {
	authorization, err := authorizePage(app, actorID, target.PageID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	committed, err := commitPageMetadataRecord(app, authorization, actorID, func(page *core.Record, _ int64) {
		if target.Cover {
			applyPageCover(page, &fileID, nil)
			return
		}
		page.Set("icon", fileID)
		page.Set("icon_type", "image")
	})
	if err != nil {
		return err
	}
	page := committed.Page
	return replaceProductFilePageReferences(
		app, authorization.Workspace.WorkspaceID, page.Id,
		page.GetString("icon"), page.GetString("icon_type"), page.GetString("cover"),
	)
}
