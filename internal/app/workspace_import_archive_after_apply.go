package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func workspaceImportArchiveAfterApply(
	job storedJob,
	pageID string,
	items []workspaceImportStagedItem,
) func(core.App, pageAuthorization, *crdt.Doc) error {
	return func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
		hasText := false
		hasPresentation := false
		hasBodyOrder := false
		// A chunk holds one kind of item, so a property's deletion or a data
		// source's wiki comes without any of the others.
		hasItemState := false
		for _, item := range items {
			if item.Text != "" {
				hasText = true
			}
			if item.RowPresentation != nil {
				hasPresentation = true
			}
			if item.BodyPosition != nil && item.Operation != nil && item.Operation.Block != nil &&
				item.Operation.Block.ParentID == nil {
				hasBodyOrder = true
			}
			if item.PropertyDeletedAt != nil || item.WikiSource != nil || item.TaskSettings != nil {
				hasItemState = true
			}
		}
		if !hasText && !hasPresentation && !hasBodyOrder && !hasItemState {
			return nil
		}
		if hasBodyOrder {
			// An archive's body order predates sub-pages as blocks: its blocks
			// keep their order, and a negative place marks a bad archive.
			for _, item := range items {
				if item.BodyPosition != nil && *item.BodyPosition < 0 {
					return workspaceImportInputError()
				}
			}
		}
		if hasText {
			deltas, err := workspaceImportArchiveTextDeltas(items)
			if err != nil {
				return err
			}
			documents := map[string]*notionYgoDocument{}
			document, err := loadNotionYgoDocument(txApp, pageID, documents)
			if err != nil {
				return err
			}
			document.doc.Transact(func(txn *crdt.Transaction) {
				for _, item := range items {
					if item.Text == "" || item.Operation == nil || item.Operation.Block == nil {
						continue
					}
					text := txn.GetText(ygoBlockTextRootPrefix + item.Operation.Block.ID)
					if text.Len() != 0 {
						continue
					}
					if delta, rich := deltas[item.Operation.Block.ID]; rich {
						// Two runs that read alike, mentions or equations, stay two.
						text.ApplyDelta(txn, graphTextDeltaPieced(delta))
					} else {
						text.Insert(txn, 0, item.Text, nil)
					}
				}
			})
			if err := saveNotionYgoDocuments(txApp, job.Owner, documents); err != nil {
				return err
			}
		}
		for _, item := range items {
			if item.PropertyDeletedAt != nil && item.Operation != nil && item.Operation.Property != nil {
				if err := importDatabasePropertyTrashState(
					txApp, item.Operation.Property.ID, job.Owner, *item.PropertyDeletedAt,
				); err != nil {
					return err
				}
			}
			if item.WikiSource != nil && item.Operation != nil && item.Operation.DataSource != nil {
				if err := holdWorkspaceImportArchiveWikiSource(
					txApp, item.Operation.DataSource.ID, *item.WikiSource,
				); err != nil {
					return err
				}
			}
			if item.StarterIdentity != nil && item.Operation != nil && item.Operation.DataSource != nil {
				if err := recordDataSourceStarterIdentity(
					txApp, item.Operation.DataSource.ID, *item.StarterIdentity,
				); err != nil {
					return err
				}
			}
			if item.TaskSettings != nil && item.Operation != nil && item.Operation.DataSource != nil {
				if err := holdWorkspaceImportArchiveTaskSettings(
					txApp, job.ID, item.Operation.DataSource.ID, *item.TaskSettings,
				); err != nil {
					return err
				}
			}
			if item.RowPresentation == nil {
				continue
			}
			presentation := item.RowPresentation
			if presentation.RowID == "" ||
				!validWorkspaceImportArchivePageIcon(presentation.Icon, presentation.IconType) {
				return workspaceImportInputError()
			}
			if err := updateDatabaseRowPagePresentation(
				txApp, presentation.RowID, job.WorkspaceID, pageID, job.Owner,
				presentation.Icon, presentation.IconType,
			); err != nil {
				return err
			}
			if presentation.Cover != "" {
				position := presentation.CoverPosition
				if _, err := setRowPageRecordCover(
					txApp, presentation.RowID, job.WorkspaceID, job.Owner, presentation.Cover, &position,
				); err != nil {
					return err
				}
				if err := replaceProductFilePageReferences(
					txApp, job.WorkspaceID, presentation.RowID, "", "none", presentation.Cover,
				); err != nil {
					return err
				}
			}
			if presentation.Font != "" && presentation.Font != pageFontDefault || presentation.SmallText || presentation.FullWidth {
				font := databaseRowPageFont(presentation.Font)
				if _, err := setRowPageRecordStyle(
					txApp, presentation.RowID, job.WorkspaceID, job.Owner, &font, &presentation.SmallText, &presentation.FullWidth,
				); err != nil {
					return err
				}
			}
			if _, err := setRowPageRecordCustomization(txApp, presentation.RowID, job.WorkspaceID, job.Owner, &presentation.TableOfContentsDisplay, &presentation.InlineCommentsDisplay); err != nil {
				return err
			}
			if err := insertWorkspaceImportPageReactions(
				txApp, job.WorkspaceID, pageID, commentTargetDatabaseRow, presentation.RowID, presentation.Reactions,
			); err != nil {
				return err
			}
			if presentation.ArchivedAt != nil {
				if _, err := applyDatabaseRowPageArchiveState(
					txApp, presentation.RowID, job.WorkspaceID, pageID, job.Owner, true, *presentation.ArchivedAt,
				); err != nil {
					return err
				}
			}
			if presentation.TrashedAt != nil {
				if err := importDatabaseRowTrashState(
					txApp, presentation.RowID, presentation.TrashRootID, job.Owner, *presentation.TrashedAt,
				); err != nil {
					return err
				}
			}
			// The row page's sub-pages, made under the page holding its
			// database, move under it.
			for _, subPageID := range presentation.SubPageIDs {
				moved, err := txApp.DB().NewQuery(`UPDATE {{leafen_pages}} SET [[parent_id]] = {:rowId}
					WHERE [[id]] = {:pageId} AND [[workspace]] = {:workspaceId} AND [[notion_import_staging]] = TRUE`).
					Bind(dbx.Params{
						"rowId": presentation.RowID, "pageId": subPageID, "workspaceId": job.WorkspaceID,
					}).Execute()
				if err != nil {
					return err
				}
				if changed, _ := moved.RowsAffected(); changed != 1 {
					return workspaceImportInputError()
				}
			}
		}
		return nil
	}
}
