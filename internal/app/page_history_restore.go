package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type storedPageHistoryRestoreReceipt struct {
	ActorID           string `db:"actor_id"`
	PageID            string `db:"page_id"`
	RequestID         string `db:"request_id"`
	RequestHash       string `db:"request_hash"`
	RestoredVersionID string `db:"restored_version_id"`
	Revision          int    `db:"revision"`
}

// A restore refused for something other than the page itself, each still a
// restore conflict to callers that only ask that.
var (
	errPageHistoryRequestReused = refuse(errPageHistoryRestoreConflict, "requestId was used for another restore: a new restore takes a new id")
	errPageHistoryPageLocked    = refuse(errPageHistoryRestoreConflict, "the page is locked: unlock it before restoring a version")
)

func restorePageHistoryVersion(
	app core.App,
	actorID, pageID, versionID string,
	input pageHistoryRestoreRequest,
) (pageHistoryRestoreResponse, error) {
	response := pageHistoryRestoreResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizePageHistoryRestore(txApp, actorID, pageID)
		if err != nil {
			return err
		}
		requestHash := pageHistoryRestoreRequestHash(pageID, versionID, input.BaseRevision, input)
		existing, err := loadPageHistoryRestoreReceipt(txApp, actorID, input.RequestID)
		if err == nil {
			if existing.PageID != pageID || existing.RequestHash != requestHash ||
				existing.RestoredVersionID != versionID {
				return errPageHistoryRequestReused
			}
			page, err := pageRecordResponseWithWiki(txApp, authorization.Page)
			if err != nil {
				return err
			}
			response = pageHistoryRestoreResponse{
				Page:    page,
				Receipt: pageHistoryReceiptAt(input.RequestID, versionID, existing.Revision, true),
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if locked, err := pageLockedFor(txApp, authorization.Page, actorID); err != nil {
			return err
		} else if locked {
			return errPageHistoryPageLocked
		}
		targetVersion, err := loadPageHistoryVersion(txApp, pageID, versionID)
		if pageHistoryVersionMissing(err) {
			return errPageHistoryVersionMissing
		}
		if err != nil {
			return err
		}
		if start, err := pageHistoryStartCursor(txApp, pageID); err != nil {
			return err
		} else if targetVersion.VersionCursor < start {
			return errPageHistoryVersionMissing
		}
		frontier, err := pageHistorySnapshotFrontier(txApp, pageID)
		if err != nil {
			return err
		}
		if frontier != input.BaseRevision {
			return errPageHistoryRevisionChanged
		}
		// The page as it stands now is kept with the latest version, so a
		// restore can be undone even after changes its history missed —
		// its text too, as every checkpoint keeps it: the edits before the
		// restore are read at it (workspace_activity_changes.go), and typing
		// since the last checkpoint was in no text span (2026-11-14: their
		// lines read empty). The page's documents are read once for the
		// restore's steps (page_history_documents.go).
		documents := newPageHistoryDocuments()
		if err := capturePageHistoryCheckpointAndSettle(
			txApp, authorization.Workspace.WorkspaceID, pageID, frontier, documents,
		); err != nil {
			return err
		}
		target, err := materializePageHistoryVersion(txApp, pageID, targetVersion.VersionCursor)
		if err != nil {
			return err
		}
		laterSubPages, err := pageHistoryLaterBlocks(txApp, pageID, target, input.RestoreDatabases, documents)
		if err != nil {
			return err
		}
		if err := returnMovedPageHistorySubPages(
			txApp, actorID, authorization.Workspace.WorkspaceID, pageID, input.RequestID, target,
			input.RestoreDatabases,
		); err != nil {
			return err
		}
		databaseReturns, err := bringBackPageHistoryBlocks(txApp, pageID, target)
		if err != nil {
			return err
		}
		structuralUpdate, stateVector, err := restorePageHistoryDocuments(
			txApp, authorization, actorID, pageID, target, documents,
		)
		if err != nil {
			return err
		}
		// A block the version brings back is out of the trash it went to, and
		// a linked database it brings back has its views again.
		if restored, _, _, err := documents.read(txApp, ygoStructuralStream, pageID); err != nil {
			return err
		} else if err := settleReturnedBlockTrash(txApp, authorization, actorID, restored); err != nil {
			return err
		} else if err := returnPageHistoryLinkedViews(txApp, authorization, actorID, restored, target); err != nil {
			return err
		}
		if isDatabaseRowPageRecord(authorization.Page) {
			err = restoreDatabaseRowPageHistoryState(txApp, pageID, actorID, target.Page)
		} else {
			err = restorePageHistoryPageState(txApp, authorization, actorID, target.Page)
		}
		if err != nil {
			return err
		}
		if err := bringBackPageHistorySubPages(txApp, actorID, pageID, input.RequestID, target); err != nil {
			return err
		}
		if err := trashPageHistoryLaterSubPages(txApp, actorID, input.RequestID, laterSubPages); err != nil {
			return err
		}
		createdAt := time.Now().UTC().UnixMilli()
		mutationID := "page-history-restore:" + input.RequestID
		if err := recordPageHistoryGraphRestore(
			txApp, authorization.Workspace.OwnerID, pageID, mutationID,
			requestHash, structuralUpdate, stateVector, createdAt,
		); err != nil {
			return err
		}
		if err := recordNotionAdminLegalHoldPageInteraction(txApp, pageEditedInteraction(
			"page-history-restore", pageID+"\x00"+actorID+"\x00"+input.RequestID,
			authorization.Workspace.Organization.OrganizationID,
			authorization.Workspace.WorkspaceID, pageID, actorID, createdAt,
		)); err != nil {
			return err
		}
		restorePayload, err := json.Marshal(map[string]string{"versionId": versionID})
		if err != nil {
			return err
		}
		restoredVersion, err := recordPageHistoryEvent(
			txApp, pageID, actorID, pageHistoryEventRestore, restorePayload,
		)
		if err != nil {
			return err
		}
		if err := capturePageHistoryCheckpointAndProject(
			txApp, pageID, restoredVersion.VersionCursor, documents,
		); err != nil {
			return err
		}
		if _, err := txApp.DB().Insert("leafen_page_history_restore_receipts", dbx.Params{
			"actor_id": actorID, "workspace_id": authorization.Workspace.WorkspaceID,
			"page_id": pageID, "request_id": input.RequestID,
			"request_hash": requestHash, "restored_version_id": versionID,
			"revision": restoredVersion.VersionCursor, "created_at": createdAt,
		}).Execute(); err != nil {
			return err
		}
		// The inline databases moved to other pages come back, one move at a
		// time, each to where the version had it.
		if input.RestoreDatabases {
			if _, err := admitBlockMoveReturns(
				txApp, actorID, pageID, "page-history-restore:"+requestHash[:32], databaseReturns,
			); err != nil {
				return err
			}
		}
		page, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		pageResponse, err := pageRecordResponseWithWiki(txApp, page)
		if err != nil {
			return err
		}
		response = pageHistoryRestoreResponse{
			Page:    pageResponse,
			Receipt: pageHistoryReceiptAt(input.RequestID, versionID, restoredVersion.VersionCursor, false),
		}
		return nil
	})
	return response, err
}

func loadPageHistoryRestoreReceipt(
	app core.App, actorID, requestID string,
) (storedPageHistoryRestoreReceipt, error) {
	receipt := storedPageHistoryRestoreReceipt{}
	err := app.DB().NewQuery(`
		SELECT [[actor_id]],[[page_id]],[[request_id]],[[request_hash]],
			[[restored_version_id]],[[revision]]
		FROM {{leafen_page_history_restore_receipts}}
		WHERE [[actor_id]]={:actorId} AND [[request_id]]={:requestId}
	`).Bind(dbx.Params{"actorId": actorID, "requestId": requestID}).One(&receipt)
	return receipt, err
}

func restorePageHistoryDocuments(
	app core.App,
	authorization pageAuthorization,
	actorID, pageID string,
	target pageHistoryMaterializedState,
	documents *pageHistoryDocuments,
) ([]byte, string, error) {
	currentStructural, found, err := documents.take(app, ygoStructuralStream, pageID)
	if err != nil {
		return nil, "", err
	}
	if !found {
		if err := seedStructuralGraphDocument(app, pageID, currentStructural); err != nil {
			return nil, "", err
		}
	}
	mark := crdt.MarkChanges(currentStructural)
	if err := replacePageHistoryBlockRoots(currentStructural, target.Structural); err != nil {
		return nil, "", err
	}
	// A full-page database takes back a version whose body is its database.
	if err := keepFullPageDatabaseBody(app, currentStructural, pageID); errors.Is(err, errFullPageDatabaseBody) {
		return nil, "", refuse(errPageHistoryRestoreConflict, "the version's body isn't the database this full-page database holds")
	} else if err != nil {
		return nil, "", err
	}
	structuralUpdate := crdt.EncodeChangesSinceV1(currentStructural, mark)
	if len(structuralUpdate) == 0 {
		return nil, "", refuse(errPageHistoryRestoreConflict, "restoring the version makes no change to the page's blocks")
	}
	if len(structuralUpdate) > maxYgoUpdateBytes {
		return nil, "", refuse(errPageHistoryRestoreConflict, "the change back to the version is over %d bytes", maxYgoUpdateBytes)
	}
	structuralState := crdt.EncodeStateAsUpdateV1(currentStructural, nil)
	if err := replaceYgoStructuralSnapshot(app, pageID, structuralState); err != nil {
		return nil, "", err
	}
	if err := documents.stored(app, ygoStructuralStream, pageID, currentStructural, structuralState); err != nil {
		return nil, "", err
	}
	if err := replacePageHistoryBlockProjections(
		app, authorization, actorID, target,
	); err != nil {
		return nil, "", err
	}
	// The page's databases stand where the version puts their blocks.
	if err := refreshDatabaseContainerPlacements(app, authorization.Workspace.OwnerID, pageID); err != nil {
		return nil, "", err
	}
	if err := replacePageHistoryCollaborationState(app, pageID, target, documents); err != nil {
		return nil, "", err
	}
	stateVector, err := graphChangeStateVector(structuralUpdate)
	if err != nil {
		return nil, "", err
	}
	return structuralUpdate, stateVector, nil
}

func replacePageHistoryBlockProjections(
	app core.App,
	authorization pageAuthorization,
	actorID string,
	target pageHistoryMaterializedState,
) error {
	pageID := authorization.Page.Id
	currentIDs := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId} ORDER BY [[id]]
	`).Bind(dbx.Params{"pageId": pageID}).Column(&currentIDs); err != nil {
		return err
	}
	kept, err := pageHistoryKeptFileTimes(app, pageID)
	if err != nil {
		return err
	}
	blocks, err := pageHistoryBlocks(target)
	if err != nil {
		return err
	}
	// The databases the version takes away leave as their blocks do, read
	// before their blocks' rows go (page_history_linked_views_back.go).
	databases, err := takeAwayPageHistoryDatabaseBlocks(app, actorID, pageID, blocks)
	if err != nil {
		return err
	}
	if err := deleteProductFileBlockReferences(
		app, authorization.Workspace.WorkspaceID, pageID, currentIDs,
	); err != nil {
		return err
	}
	if err := deleteStructuralBlockProjections(
		app, authorization.Workspace.OwnerID, pageID, currentIDs,
	); err != nil {
		return err
	}
	for _, block := range blocks {
		projection := &graphBlock{
			ID: block.ID, Type: block.Type, Content: block.Content, Position: block.Position,
		}
		if block.ParentID != "" {
			projection.ParentID = &block.ParentID
		}
		if err := upsertStructuralBlockProjectionAt(
			app, authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID,
			pageID, actorID, projection, block.ParentID, block.Position,
		); err != nil {
			return err
		}
		if err := replaceProductFileBlockReference(
			app, authorization.Workspace.WorkspaceID, pageID, projection,
		); err != nil {
			return err
		}
		if err := keepPageHistoryFileTimes(app, kept, block); err != nil {
			return err
		}
	}
	// The databases it brings back come back as their blocks do.
	return bringBackPageHistoryDatabaseBlocks(app, authorization, databases, blocks)
}

func replacePageHistoryCollaborationState(
	app core.App, pageID string, target pageHistoryMaterializedState, documents *pageHistoryDocuments,
) error {
	current, _, err := documents.take(app, ygoCollaborationStream, pageID)
	if err != nil {
		return err
	}
	currentMetadata, err := structuralMetadata(target.Structural)
	if err != nil {
		return err
	}
	// Texts are read before the change, as a read takes its document's lock,
	// which Transact holds (YText.ToDelta).
	restored := make(map[string][]crdt.Delta, len(currentMetadata))
	for id := range currentMetadata {
		restored[id] = target.Collaboration.GetText(ygoBlockTextRootPrefix + id).ToDelta()
	}
	current.Transact(func(transaction *crdt.Transaction) {
		for id := range currentMetadata {
			text := transaction.GetText(ygoBlockTextRootPrefix + id)
			if text.Len() > 0 {
				text.Delete(transaction, 0, text.Len())
			}
			text.ApplyDelta(transaction, restored[id])
		}
	})
	state := crdt.EncodeStateAsUpdateV1(current, nil)
	if err := replaceYgoSnapshot(app, pageID, state); err != nil {
		return err
	}
	return documents.stored(app, ygoCollaborationStream, pageID, current, state)
}

func restorePageHistoryPageState(
	app core.App, authorization pageAuthorization, actorID string, state pageHistoryPageState,
) error {
	state.BacklinksDisplay = normalizedPageCustomizationDisplay(&state.BacklinksDisplay)
	state.PageCommentsDisplay = normalizedPageCustomizationDisplay(&state.PageCommentsDisplay)
	page := authorization.Page
	workspaceState, err := app.FindFirstRecordByData(
		userStateCollection, "owner", authorization.Workspace.OwnerID,
	)
	if err != nil {
		return err
	}
	nextCursor := workspaceState.GetInt("cursor") + 1
	workspaceState.Set("cursor", nextCursor)
	if err := app.Save(workspaceState); err != nil {
		return err
	}
	setPageTitleAsKept(page, state.Title, state.TitleSpans)
	page.Set("content", state.Content)
	page.Set("icon", state.Icon)
	page.Set("icon_type", state.IconType)
	page.Set("cover", state.Cover)
	page.Set("cover_position", state.CoverPosition)
	page.Set("font", state.Font)
	page.Set("small_text", state.SmallText)
	page.Set("full_width", state.FullWidth)
	page.Set("backlinks_display", state.BacklinksDisplay)
	page.Set("page_comments_display", state.PageCommentsDisplay)
	page.Set("table_of_contents_display", normalizedPageTableOfContentsDisplay(&state.TableOfContentsDisplay))
	page.Set("inline_comments_display", normalizedPageInlineCommentsDisplay(&state.InlineCommentsDisplay))
	page.Set("revision", page.GetInt("revision")+1)
	page.Set("cursor", nextCursor)
	setPageLastEditor(page, actorID)
	if err := app.Save(page); err != nil {
		return err
	}
	if err := replaceProductFilePageReferences(
		app, authorization.Workspace.WorkspaceID, page.Id,
		state.Icon, state.IconType, state.Cover,
	); err != nil {
		return err
	}
	return markSearchDirty(app, page)
}

func recordPageHistoryGraphRestore(
	app core.App,
	ownerID, pageID, mutationID, requestHash string,
	update []byte, stateVector string, createdAt int64,
) error {
	var cursor int
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[cursor]]),0)+1 FROM {{leafen_graph_mutations}}
		WHERE [[owner]]={:ownerId} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{"ownerId": ownerID, "pageId": pageID}).Row(&cursor); err != nil {
		return err
	}
	counts, _ := json.Marshal(graphMutationCounts{})
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_graph_mutations}}
			([[owner]],[[page_id]],[[mutation_id]],[[request_hash]],[[cursor]],[[counts_json]],[[state_vector]],[[created]])
		VALUES ({:owner},{:pageId},{:mutationId},{:requestHash},{:cursor},{:counts},{:stateVector},{:created})
	`).Bind(dbx.Params{
		"owner": ownerID, "pageId": pageID, "mutationId": mutationID,
		"requestHash": requestHash, "cursor": cursor, "counts": string(counts),
		"stateVector": stateVector, "created": createdAt,
	}).Execute(); err != nil {
		return err
	}
	return insertStructuralGraphChange(
		app, ownerID, pageID, mutationID, requestHash, cursor, update, createdAt,
	)
}
