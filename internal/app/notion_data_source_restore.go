package app

import (
	"context"
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// restoreNotionDataSource takes a source its database keeps in the trash out
// of it where it stands, and returns a detached source to the database it
// came from when that database is still live; otherwise its recoverable
// database block is restored where it left — anywhere, last in the parent
// it left (restoreAtParentEndAnywhere).
func restoreNotionDataSource(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	authorization notionDatabaseRowPageAuthorization,
	state notionDataSourceTrashState,
	anywhere bool,
) error {
	identity := authorization.Identity
	if state.InPlace {
		return restoreNotionDataSourceInPlace(event, ygo, identity, state)
	}
	origin, err := notionDataSourceTrashOrigin(event.App, state)
	if err != nil {
		return err
	}
	live := 0
	if origin != "" {
		if err := event.App.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_database_containers}} AS [[container]]
			JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]]=[[container]].[[block_id]]
			WHERE [[container]].[[id]]={:id} AND [[container]].[[kind]]='source'
				AND [[container]].[[page_id]]={:pageId}
				AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
					WHERE [[trash]].[[container_id]]=[[container]].[[id]])
		`).Bind(dbx.Params{"id": origin, "pageId": state.PageID}).Row(&live); err != nil {
			return err
		}
	}
	// One its database, in the trash, keeps in the trash where it stands
	// comes out of it there; its database stays in the trash.
	if origin == "" && !anywhere {
		if found, err := restoreNotionDataSourceInTrashedDatabase(event.App, state); err != nil || found {
			return err
		}
	}
	tombstone, found, err := loadRecoverableBlockTrash(event.App, state.RootBlockID.String)
	if err != nil {
		return err
	}
	if !found || tombstone.PageID != state.PageID {
		return errDatabaseRowPageNotFound
	}
	if live != 1 && origin != "" {
		return restoreDetachedNotionDataSource(event, ygo, identity, state, tombstone)
	}
	if live != 1 {
		place := restoreInPlace
		if anywhere {
			place = restoreAtParentEndAnywhere
		}
		return restoreRecoverableBlockTrashTree(
			event.Request.Context(), ygo, identity, tombstone,
			"notion-public-data-source-restore:"+newDiagnosticReference(), place,
		)
	}
	_, err = ygo.applyContentGraphMutationContext(
		event.Request.Context(), identity.PrincipalID, state.PageID,
		graphMutationInput{MutationID: "notion-public-data-source-restore:" + newDiagnosticReference()},
		contentGraphMutationHooks{
			Authorize: authorizeNotionPageMutation(identity, state.PageID),
			Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				entry, content, err := notionDataSourceContainerBlock(doc, origin)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				members := append(content.dataSourceIDs(), state.DataSource)
				block, err := notionDataSourceMembershipBlock(txApp, entry, origin, state.PageID, state.OwnerID, members)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind: graphOperationKindUpsertBlock, Block: block,
				}}}, nil
			},
			BeforeApplyOperations: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc, _ []graphOperation) error {
				if err := deleteRecoverableBlockTrash(txApp, tombstone.RootBlockID, state.PageID); err != nil {
					return err
				}
				noteDataSourceLeavesTrash(txApp, state.WorkspaceID, state.ContainerID, state.PageID, state.DataSource)
				return deleteDatabaseContainerProjection(txApp, state.ContainerID)
			},
		},
	)
	return err
}

// restoreDetachedNotionDataSource brings a detached source back as its own
// database block when the database it came from no longer exists.
func restoreDetachedNotionDataSource(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	identity notionOAuthBearerIdentity,
	state notionDataSourceTrashState,
	tombstone storedRecoverableBlockTrash,
) error {
	snapshot, err := parseRecoverableBlockTrashSnapshot(tombstone)
	if err != nil || len(snapshot.Blocks) != 1 || snapshot.Blocks[0].ID != tombstone.RootBlockID {
		return errDatabaseRowPageConflict
	}
	stored := snapshot.Blocks[0]
	content := map[string]any{}
	if json.Unmarshal(stored.Content, &content) != nil {
		return errDatabaseRowPageConflict
	}
	delete(content, dataSourceTrashOriginField)
	encoded, err := json.Marshal(content)
	if err != nil {
		return err
	}
	_, err = ygo.applyContentGraphMutationContext(
		event.Request.Context(), identity.PrincipalID, state.PageID,
		graphMutationInput{MutationID: "notion-public-data-source-restore:" + newDiagnosticReference()},
		contentGraphMutationHooks{
			Authorize: authorizeNotionPageMutation(identity, state.PageID),
			Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				metadata, err := structuralMetadata(doc)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				block := &graphBlock{
					ID: stored.ID, Position: stored.Position, Type: graphBlockTypeInlineDatabase, Content: encoded,
				}
				if _, exists := metadata[stored.ParentID]; exists && stored.ParentID != "" {
					parentID := stored.ParentID
					block.ParentID = &parentID
				}
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind: graphOperationKindUpsertBlock, Block: block,
				}}}, nil
			},
			BeforeApplyOperations: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc, _ []graphOperation) error {
				return deleteRecoverableBlockTrash(txApp, tombstone.RootBlockID, state.PageID)
			},
		},
	)
	return err
}

// recoverableBlockTrashPlace is where a restored block tree goes.
type recoverableBlockTrashPlace int

const (
	// restoreInPlace puts it back exactly where it left, refused once that
	// place changed.
	restoreInPlace recoverableBlockTrashPlace = iota
	// restoreAtParentEnd puts it last in the parent it left, as Notion's
	// public API restore does whether its place stands or not; one whose
	// parent is gone is refused as under an archived ancestor (seen
	// 2026-11-13: a database and a paragraph, on a page and in a toggle).
	restoreAtParentEnd
	// restoreAtParentEndAnywhere is the trash window's: last in the parent
	// it left, as Notion's trash restores a database (seen 2026-11-13), or
	// at its page's end when that parent is gone.
	restoreAtParentEndAnywhere
)

// restoreRecoverableBlockTrashTree puts a recoverable block tree back, as a
// public API request of identity, where place says.
func restoreRecoverableBlockTrashTree(
	ctx context.Context,
	ygo *pocketBaseYgoPersistence,
	identity notionOAuthBearerIdentity,
	tombstone storedRecoverableBlockTrash,
	mutationID string,
	place recoverableBlockTrashPlace,
) error {
	snapshot, err := parseRecoverableBlockTrashSnapshot(tombstone)
	if err != nil {
		return err
	}
	operations, explicitStarts, err := prepareRecoverableBlockTrashRestore(ygo.app, tombstone, snapshot)
	if err != nil {
		return err
	}
	_, err = ygo.applyContentGraphMutationContext(
		ctx, identity.PrincipalID, tombstone.PageID,
		graphMutationInput{MutationID: mutationID, Operations: operations},
		contentGraphMutationHooks{
			Authorize: authorizeNotionPageMutation(identity, tombstone.PageID),
			PrepareInput: func(_ core.App, _ pageAuthorization, doc *crdt.Doc, input graphMutationInput) (graphMutationInput, error) {
				if place == restoreInPlace {
					return input, nil
				}
				return input, placeRecoverableBlockTrashRestoreAtParentEnd(
					doc, tombstone, input.Operations, explicitStarts, place == restoreAtParentEndAnywhere,
				)
			},
			BeforeApplyOperations: validateRecoverableBlockTrashRestore(tombstone, snapshot, place != restoreInPlace),
			ExplicitStartBlockIDs: explicitStarts,
			ApplyAdditionalText: func(app core.App, _ pageAuthorization) (string, error) {
				return applyRecoverableBlockTrashText(app, tombstone.PageID, identity.PrincipalID, snapshot)
			},
			AfterApply: func(app core.App, _ pageAuthorization, _ *crdt.Doc) error {
				return deleteRecoverableBlockTrash(app, tombstone.RootBlockID, tombstone.PageID)
			},
		},
	)
	return err
}

// placeRecoverableBlockTrashRestoreAtParentEnd puts a restored tree after
// the last block of the parent it left, or first in it when it is empty. A
// parent that is gone refuses it, or, anywhere, sends it to its page's end.
func placeRecoverableBlockTrashRestoreAtParentEnd(
	doc *crdt.Doc, row storedRecoverableBlockTrash, operations []graphOperation, explicitStarts map[string]struct{},
	anywhere bool,
) error {
	parentID := row.ParentID
	if parentID != "" {
		metadata, err := structuralMetadata(doc)
		if err != nil {
			return err
		}
		if _, exists := metadata[parentID]; !exists {
			if !anywhere {
				return graphInputError(errorCodeGraphReferenceInvalid, notionArchivedAncestorMessage)
			}
			parentID = ""
		}
	}
	order, err := structuralOrder(doc, parentID)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if operation.Block == nil || operation.Block.ID != row.RootBlockID {
			continue
		}
		if parentID == "" {
			operation.Block.ParentID = nil
		}
		delete(explicitStarts, row.RootBlockID)
		if len(order) == 0 {
			operation.Block.AfterID = nil
			explicitStarts[row.RootBlockID] = struct{}{}
			continue
		}
		tail := order[len(order)-1]
		operation.Block.AfterID = &tail
	}
	return nil
}
