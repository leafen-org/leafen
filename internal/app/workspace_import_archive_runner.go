package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func (r *durableJobRunner) applyWorkspaceArchiveImportChunk(job storedJob, payload workspaceImportJobPayload) error {
	kind := "source"
	err := r.applyWorkspaceArchiveImportChunkKind(job, payload, &kind)
	// A row a chunk looked for and didn't find says which chunk did, which
	// a bare "no rows" never told (2026-11-07: a moved workspace's import
	// stopped on it with nothing in the log but the error's type).
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return workspaceImportFailureAt(err, "archive-"+kind)
	}
	return err
}

func (r *durableJobRunner) applyWorkspaceArchiveImportChunkKind(job storedJob, payload workspaceImportJobPayload, kind *string) error {
	if r.ygo == nil {
		return workspaceImportStateError()
	}
	source, err := loadStoredWorkspaceImportSource(r.app, job.ID)
	if err != nil || source.Format != workspaceImportFormatNativeArchiveV2 {
		return workspaceImportStateError()
	}
	if !payload.SourceVerified {
		if err := verifyWorkspaceImportSource(r.app.DataDir(), source); err != nil {
			return err
		}
	}
	if !payload.ArchiveFilesReady {
		*kind = "files"
		return r.publishWorkspaceArchiveImportFile(job, payload, source)
	}
	offset, err := strconv.ParseInt(job.Cursor, 10, 64)
	if err != nil {
		return workspaceImportStateError()
	}
	chunk, nextOffset, err := loadWorkspaceImportStagedChunk(r.app.DataDir(), source, offset, payload.ChunkIndex)
	if err != nil || payload.ItemOffset != 0 {
		if err != nil {
			return err
		}
		return workspaceImportStateError()
	}
	payload.SourceVerified = true
	*kind = "operations"
	if len(chunk.Items) == 0 {
		// The one empty chunk of a bundle with nothing in it.
		return r.applyWorkspaceArchiveOperations(job, payload, chunk, nextOffset)
	}
	pageChunk := chunk.Items[0].Page != nil
	commentChunk := chunk.Items[0].Comment != nil
	templateChunk := chunk.Items[0].Template != nil
	blockTrashChunk := chunk.Items[0].BlockTrash != nil
	customEmojiChunk := chunk.Items[0].CustomEmoji != nil
	rowPageBlockChunk := chunk.Items[0].RowPageBlock != nil
	rowBlockTrashChunk := chunk.Items[0].RowBlockTrash != nil
	automationChunk := chunk.Items[0].Automation != nil
	for _, item := range chunk.Items {
		if (item.Page != nil) != pageChunk || (item.Comment != nil) != commentChunk ||
			(item.Template != nil) != templateChunk || (item.BlockTrash != nil) != blockTrashChunk ||
			(item.CustomEmoji != nil) != customEmojiChunk ||
			(item.RowPageBlock != nil) != rowPageBlockChunk ||
			(item.RowBlockTrash != nil) != rowBlockTrashChunk ||
			(item.Automation != nil) != automationChunk ||
			boolCount(pageChunk, commentChunk, templateChunk, blockTrashChunk, customEmojiChunk,
				rowPageBlockChunk, rowBlockTrashChunk, automationChunk) > 1 {
			return workspaceImportStateError()
		}
	}
	if pageChunk {
		*kind = "pages"
		return r.createWorkspaceArchivePages(job, payload, chunk, nextOffset)
	}
	if commentChunk {
		*kind = "comments"
		return r.applyWorkspaceArchiveComments(job, payload, chunk, nextOffset)
	}
	if templateChunk {
		*kind = "templates"
		return r.applyWorkspaceArchiveTemplates(job, payload, chunk, nextOffset)
	}
	if blockTrashChunk {
		*kind = "block-trash"
		return r.applyWorkspaceArchiveBlockTrash(job, payload, chunk, nextOffset)
	}
	if customEmojiChunk {
		*kind = "emojis"
		return r.applyWorkspaceArchiveCustomEmojis(job, payload, chunk, nextOffset)
	}
	if rowPageBlockChunk {
		*kind = "row-page-blocks"
		return r.applyWorkspaceArchiveRowPageBlocks(job, payload, chunk, nextOffset)
	}
	if rowBlockTrashChunk {
		*kind = "row-block-trash"
		return r.applyWorkspaceArchiveRowBlockTrash(job, payload, chunk, nextOffset)
	}
	if automationChunk {
		*kind = "automations"
		return r.applyWorkspaceArchiveAutomations(job, payload, chunk, nextOffset)
	}
	return r.applyWorkspaceArchiveOperations(job, payload, chunk, nextOffset)
}

func boolCount(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func (r *durableJobRunner) createWorkspaceArchivePages(
	job storedJob,
	payload workspaceImportJobPayload,
	chunk workspaceImportStagedChunk,
	nextOffset int64,
) error {
	teamspaces, err := r.ensureWorkspaceArchiveTeamspaces(job, chunk)
	if err != nil {
		return err
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued || current.Cursor != job.Cursor {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.ChunkIndex != payload.ChunkIndex || currentPayload.ItemOffset != 0 {
			if err != nil {
				return err
			}
			return workspaceImportStateError()
		}
		currentPayload.SourceVerified = true
		authorization, err := authorizeWorkspace(txApp, current.Owner, current.WorkspaceID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		var tracked int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_workspace_archive_import_pages}} WHERE [[job_id]] = {:jobId}
		`).Bind(dbx.Params{"jobId": current.ID}).Row(&tracked); err != nil {
			return err
		}
		workspacePages, err := workspaceArchiveTargetPages(txApp, current.WorkspaceID, true)
		if err != nil {
			return err
		}
		if tracked != current.Processed || workspacePages != tracked {
			return &workspaceImportFailureError{code: errorCodeWorkspaceImportTargetNotFresh}
		}
		state, err := workspaceUserState(txApp, authorization.OwnerID)
		if err != nil {
			return err
		}
		pages, err := txApp.FindCollectionByNameOrId(pagesCollection)
		if err != nil {
			return err
		}
		nextCursor := state.GetInt("cursor")
		createdAt := time.Now().UTC().UnixMilli()
		createdInteractions := make([]pageInteraction, 0, len(chunk.Items))
		for index, item := range chunk.Items {
			definition := item.Page
			if definition == nil || definition.ID == "" || item.PageID != definition.ID {
				return workspaceImportStateError()
			}
			nextCursor++
			page := core.NewRecord(pages)
			page.Id = definition.ID
			page.Set("owner", authorization.OwnerID)
			page.Set("workspace", authorization.WorkspaceID)
			teamspaceID := ""
			if definition.ParentID != "" {
				parent, err := txApp.FindRecordById(pagesCollection, definition.ParentID)
				if err != nil || parent.GetString("workspace") != authorization.WorkspaceID {
					if err != nil {
						return err
					}
					return workspaceImportStateError()
				}
				teamspaceID = parent.GetString("teamspace_id")
			} else if definition.TeamspaceID != "" {
				if teamspaceID, err = workspaceImportArchivePageTeamspaceID(
					txApp, authorization.WorkspaceID, definition, teamspaces,
				); err != nil {
					return err
				}
			}
			page.Set("teamspace_id", teamspaceID)
			page.Set("title", definition.Title)
			if definition.Kind != pageKindDatabase {
				spans, _ := encodedPageTitleSpans(definition.Title, definition.TitleSpans)
				page.Set("title_spans", spans)
			}
			page.Set("content", definition.Content)
			page.Set("revision", 0)
			page.Set("cursor", nextCursor)
			page.Set("parent_id", definition.ParentID)
			page.Set("position", definition.Position)
			page.Set("is_locked", definition.IsLocked)
			trashTimestamp := ""
			if definition.TrashedAt != nil {
				trashTimestamp = *definition.TrashedAt
			}
			applyPageTrashState(page, definition.InTrash, trashTimestamp)
			if definition.ArchivedAt != nil {
				applyPageArchiveState(page, true, current.Owner, *definition.ArchivedAt)
			}
			page.Set("icon", definition.Icon)
			page.Set("icon_type", definition.IconType)
			page.Set("cover", definition.Cover)
			page.Set("cover_position", definition.CoverPosition)
			page.Set("font", definition.Font)
			page.Set("small_text", definition.SmallText)
			page.Set("full_width", definition.FullWidth)
			page.Set("backlinks_display", definition.BacklinksDisplay)
			page.Set("page_comments_display", definition.PageCommentsDisplay)
			page.Set("table_of_contents_display", definition.TableOfContentsDisplay)
			page.Set("inline_comments_display", definition.InlineCommentsDisplay)
			page.Set("kind", readPageKind(definition.Kind))
			if definition.Kind == "" {
				currentPayload.ArchiveKindsUnknown = true
			}
			page.Set("notion_import_staging", true)
			definition.workspaceImportPageAnchor.set(page)
			setNewPageCustodianProvenance(page, current.Owner)
			if definition.CreatedBy != "" {
				page.Set("created_by", definition.CreatedBy)
			}
			if err := txApp.Save(page); err != nil {
				return err
			}
			// Saving a page notes it as edited now; it keeps its archived
			// edit, which the import's writes to its body leave
			// (notePageContentEdited).
			if definition.EditedAt != "" {
				if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_pages}} SET [[updated]] = {:edited} WHERE [[id]] = {:pageId}`).
					Bind(dbx.Params{"edited": definition.EditedAt, "pageId": page.Id}).Execute(); err != nil {
					return err
				}
			}
			if definition.MovedDatabase != "" {
				if err := putDatabasePageSignpost(txApp, page.Id, authorization.WorkspaceID, definition.MovedDatabase); err != nil {
					return err
				}
			}
			if err := insertWorkspaceImportPageReactions(
				txApp, authorization.WorkspaceID, page.Id, commentTargetPage, page.Id, definition.Reactions,
			); err != nil {
				return err
			}
			// The page's block comes when its parent's body is filled.
			if definition.BodyPosition != nil && *definition.BodyPosition < 0 {
				return workspaceImportStateError()
			}
			createdInteractions = append(createdInteractions, pageCreatedInteraction(
				authorization.Organization.OrganizationID, authorization.WorkspaceID,
				page.Id, current.Owner, createdAt,
			))
			if err := replaceProductFilePageReferences(
				txApp, authorization.WorkspaceID, page.Id,
				definition.Icon, definition.IconType, definition.Cover,
			); err != nil {
				return err
			}
			if definition.HasYgo {
				documents := map[string]*notionYgoDocument{}
				if _, err := loadNotionYgoDocument(txApp, page.Id, documents); err != nil {
					return err
				}
				if err := saveNotionYgoDocuments(txApp, current.Owner, documents); err != nil {
					return err
				}
			}
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_workspace_archive_import_pages}}
					([[job_id]], [[sequence]], [[page_id]], [[is_root]])
				VALUES ({:jobId}, {:sequence}, {:pageId}, {:isRoot})
			`).Bind(dbx.Params{
				"jobId": current.ID, "sequence": current.Processed + index,
				"pageId": page.Id, "isRoot": definition.Root,
			}).Execute(); err != nil {
				return err
			}
			if definition.Root {
				currentPayload.RootPageIDs = append(currentPayload.RootPageIDs, page.Id)
				if currentPayload.RootPageID == "" {
					currentPayload.RootPageID = page.Id
				}
			}
		}
		if err := recordNotionAdminLegalHoldPageInteraction(
			txApp, createdInteractions...,
		); err != nil {
			return err
		}
		state.Set("cursor", nextCursor)
		if err := txApp.Save(state); err != nil {
			return err
		}
		if len(currentPayload.RootPageIDs) == 0 && chunk.Last {
			return workspaceImportStateError()
		}
		cursor := strconv.FormatInt(nextOffset, 10)
		currentPayload.ChunkIndex++
		currentPayload.Phase = workspaceImportPhaseApply
		currentPayload.ReadyToPublish = chunk.Last
		if err := r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, cursor, current.Processed+len(chunk.Items), true,
		); err != nil {
			return err
		}
		if current.PageID == "" && len(currentPayload.RootPageIDs) > 0 {
			_, err = txApp.DB().NewQuery(`
				UPDATE {{leafen_jobs}} SET [[page_id]] = {:pageId} WHERE [[id]] = {:id} AND [[page_id]] = ''
			`).Bind(dbx.Params{"id": current.ID, "pageId": currentPayload.RootPageIDs[0]}).Execute()
		}
		return err
	})
}

func (r *durableJobRunner) applyWorkspaceArchiveOperations(
	job storedJob,
	payload workspaceImportJobPayload,
	chunk workspaceImportStagedChunk,
	nextOffset int64,
) error {
	groups := map[string][]workspaceImportStagedItem{}
	order := []string{}
	for _, item := range chunk.Items {
		if item.Page != nil || item.Operation == nil || item.PageID == "" {
			return workspaceImportStateError()
		}
		if _, exists := groups[item.PageID]; !exists {
			order = append(order, item.PageID)
		}
		groups[item.PageID] = append(groups[item.PageID], item)
	}
	for _, pageID := range order {
		items := groups[pageID]
		operations, err := workspaceImportGraphOperations(workspaceImportFormatNativeArchiveV2, items)
		if err != nil {
			return err
		}
		mutationID := fmt.Sprintf("workspace-archive-import-%s-%d-%s", job.ID, payload.ChunkIndex, pageID)
		explicitStarts := map[string]struct{}{}
		for _, item := range items {
			if item.Start && item.Operation != nil && item.Operation.Block != nil {
				explicitStarts[item.Operation.Block.ID] = struct{}{}
			}
		}
		batches := workspaceImportGraphOperationBatches(operations, workspaceImportMutationTurnItems)
		for batchIndex, batch := range batches {
			batchMutationID := mutationID
			if len(batches) > 1 {
				batchMutationID = fmt.Sprintf("%s-%d", mutationID, batchIndex)
			}
			hooks := contentGraphMutationHooks{
				Authorize:             workspaceImportGraphAuthorization(job, pageID),
				AllowTrashedPage:      true,
				ExplicitStartBlockIDs: explicitStarts,
				SuppressNotifications: true,
				// The archive has both sides of its two-way relations.
				RelationPairsAsWritten: true,
			}
			if batchIndex == len(batches)-1 {
				hooks.AfterApply = workspaceImportArchiveAfterApply(job, pageID, items)
			}
			if _, err := r.ygo.applyStoredContentGraphMutation(job.Owner, pageID, graphMutationInput{
				MutationID: batchMutationID, Operations: batch,
			}, hooks); err != nil {
				return workspaceImportFailureAt(err, "archive-apply-graph")
			}
		}
	}
	payload.ItemOffset = 0
	payload.ChunkIndex++
	payload.ReadyToPublish = chunk.Last
	payload.Phase = workspaceImportPhaseApply
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued || current.Cursor != job.Cursor {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.ChunkIndex != payload.ChunkIndex-1 {
			if err != nil {
				return err
			}
			return workspaceImportStateError()
		}
		payload.CancelRequested = currentPayload.CancelRequested
		return r.updateWorkspaceImportPayload(
			txApp, current, payload, strconv.FormatInt(nextOffset, 10),
			current.Processed+len(chunk.Items), true,
		)
	})
}

func loadWorkspaceArchiveImportPageIDs(app core.App, jobID string, rootsOnly bool) ([]string, error) {
	condition := ""
	if rootsOnly {
		condition = " AND [[is_root]] = 1"
	}
	rows := []struct {
		PageID string `db:"page_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_workspace_archive_import_pages}}
		WHERE [[job_id]] = {:jobId}` + condition + ` ORDER BY [[sequence]]
	`).Bind(dbx.Params{"jobId": jobID}).All(&rows)
	result := make([]string, len(rows))
	for index, row := range rows {
		result[index] = row.PageID
	}
	return result, err
}

func encodeWorkspaceImportPayload(payload workspaceImportJobPayload) (string, error) {
	encoded, err := json.Marshal(payload)
	return string(encoded), err
}

func (r *durableJobRunner) publishWorkspaceArchiveImport(
	txApp core.App,
	current storedJob,
	payload *workspaceImportJobPayload,
) error {
	authorization, err := authorizeWorkspace(txApp, current.Owner, current.WorkspaceID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	var tracked, staged int
	if err := txApp.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_workspace_archive_import_pages}} WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": current.ID}).Row(&tracked); err != nil {
		return err
	}
	if err := txApp.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_pages}} AS [[page]]
		JOIN {{leafen_workspace_archive_import_pages}} AS [[owned]] ON [[owned]].[[page_id]] = [[page]].[[id]]
		WHERE [[owned]].[[job_id]] = {:jobId} AND [[page]].[[workspace]] = {:workspaceId}
			AND [[page]].[[owner]] = {:ownerId} AND [[page]].[[notion_import_staging]] = TRUE
	`).Bind(dbx.Params{
		"jobId": current.ID, "workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID,
	}).Row(&staged); err != nil {
		return err
	}
	workspacePages, err := workspaceArchiveTargetPages(txApp, current.WorkspaceID, true)
	if err != nil {
		return err
	}
	// A moved workspace without pages brings none in.
	empty := tracked == 0 && payload.KeepIDs && current.Total == 0
	if (tracked == 0 && !empty) || staged != tracked || workspacePages != tracked || current.Processed != current.Total {
		return workspaceImportStateErrorAt("archive-publish-counts")
	}
	roots, err := loadWorkspaceArchiveImportPageIDs(txApp, current.ID, true)
	if err != nil || (len(roots) == 0 && !empty) {
		return workspaceImportStateErrorAt("archive-publish-roots")
	}
	if len(roots) > 0 {
		payload.RootPageID = roots[0]
	}
	payload.RootPageIDs = roots
	if err := publishWorkspaceArchiveWikis(txApp, current.ID, authorization, r.clock.now()); err != nil {
		return err
	}
	if err := publishWorkspaceArchiveTaskSettings(txApp, current.ID, authorization, r.clock.now()); err != nil {
		return err
	}
	now := r.clock.now().UnixMilli()
	if _, err := txApp.DB().NewQuery(`
		INSERT INTO {{leafen_search_dirty}} ([[page_id]], [[workspace_id]], [[version]], [[enqueued_at]])
		SELECT [[page]].[[id]], [[page]].[[workspace]], 1, {:now}
		FROM {{leafen_pages}} AS [[page]]
		JOIN {{leafen_workspace_archive_import_pages}} AS [[owned]] ON [[owned]].[[page_id]] = [[page]].[[id]]
		WHERE [[owned]].[[job_id]] = {:jobId} AND TRUE
		ON CONFLICT ([[page_id]]) DO UPDATE SET
			[[workspace_id]] = excluded.[[workspace_id]],
			[[version]] = [[leafen_search_dirty]].[[version]] + 1,
			[[enqueued_at]] = excluded.[[enqueued_at]]
	`).Bind(dbx.Params{"now": now, "jobId": current.ID}).Execute(); err != nil {
		return err
	}
	if _, err := txApp.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[notion_import_staging]] = FALSE
		WHERE [[id]] IN (
			SELECT [[page_id]] FROM {{leafen_workspace_archive_import_pages}} WHERE [[job_id]] = {:jobId}
		)
		`).Bind(dbx.Params{"jobId": current.ID}).Execute(); err != nil {
		return err
	}
	if err := settleWorkspaceArchivePageKinds(txApp, current.ID, payload.ArchiveKindsUnknown); err != nil {
		return err
	}
	// The imported pages show in their parents' bodies, their sub-pages in
	// theirs, and a database an older archive brought with no block on its.
	if _, err := txApp.DB().NewQuery(`
		INSERT OR REPLACE INTO {{leafen_child_page_block_backfill}} ([[parent_page_id]], [[workspace_id]], [[requested_at]])
		SELECT [[page]].[[parent_id]], [[page]].[[workspace]], {:now} FROM {{leafen_pages}} AS [[page]]
		JOIN {{leafen_workspace_archive_import_pages}} AS [[owned]] ON [[owned]].[[page_id]] = [[page]].[[id]]
		WHERE [[owned]].[[job_id]] = {:jobId} AND [[page]].[[parent_type]] = 'page' AND [[page]].[[parent_id]] != ''
		UNION
		SELECT [[page]].[[id]], [[page]].[[workspace]], {:now} FROM {{leafen_pages}} AS [[page]]
		JOIN {{leafen_workspace_archive_import_pages}} AS [[owned]] ON [[owned]].[[page_id]] = [[page]].[[id]]
		WHERE [[owned]].[[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": current.ID, "now": now}).Execute(); err != nil {
		return err
	}
	notifyWorkspaceArchiveImportBackfills(txApp)
	if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_custom_emojis}} SET [[workspace_import_job_id]]=''
			WHERE [[workspace_import_job_id]]={:jobId} AND [[workspace_id]]={:workspaceId}
		`).Bind(dbx.Params{
		"jobId": current.ID, "workspaceId": current.WorkspaceID,
	}).Execute(); err != nil {
		return err
	}
	if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_workspace_archive_import_targets}} WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": current.ID}).Execute(); err != nil {
		return err
	}
	return nil
}

// settleWorkspaceArchivePageKinds keeps a full-page database only where its
// body came in as its database alone, its own or linked, as the editor keeps
// it (keepFullPageDatabaseBody), and gives the pages of an archive from
// before kinds were kept their kinds by the rule the kinds arrived with. The
// pages came in with their archived kinds, page for none: a page is never
// without a kind, even for a moment (2026-10-03: a page cleared of its kind
// to be settled became a page at once, and every imported full-page
// database came in as a page).
func settleWorkspaceArchivePageKinds(app core.App, jobID string, kindsUnknown bool) error {
	imported := `[[id]] IN (SELECT [[page_id]] FROM {{leafen_workspace_archive_import_pages}} WHERE [[job_id]] = {:jobId})`
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_pages}} SET [[kind]] = 'page' WHERE [[kind]] = 'database' AND ` +
		imported + ` AND NOT ` + fullPageDatabaseBodySQL).Bind(dbx.Params{"jobId": jobID}).Execute(); err != nil {
		return err
	}
	if !kindsUnknown {
		return nil
	}
	_, err := app.DB().NewQuery(`UPDATE {{leafen_pages}} SET [[kind]] = 'database' WHERE [[kind]] = 'page' AND ` +
		imported + ` AND ` + fullPageDatabaseShapedSQL).Bind(dbx.Params{"jobId": jobID}).Execute()
	return err
}
