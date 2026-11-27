package app

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// databaseTurn is the work of turning a database between a full page and
// inline (notion_database_turn.go) for whoever asks it: the person it is done
// as, how each page it changes is let to them, and the name its changes go
// by. A connection asks it through the API's is_inline
// (notionDatabaseTurnOf), an import for its importer when a page it brought
// held the database inline (workspace_import_inline_databases.go).
type databaseTurn struct {
	actorID   string
	authorize func(pageID string) func(core.App) (pageAuthorization, error)
	name      string
}

// setKind makes a page a page or a database's page.
func (turn databaseTurn) setKind(ctx context.Context, ygo *pocketBaseYgoPersistence, pageID, kind string) error {
	_, err := ygo.applyContentGraphMutationContext(
		ctx, turn.actorID, pageID,
		graphMutationInput{MutationID: turn.name + "-kind:" + newDiagnosticReference(), PageKind: &kind},
		contentGraphMutationHooks{Authorize: turn.authorize(pageID)},
	)
	return err
}

// placeInline stands a database just moved to the end of the page above its
// page where its page's block is, and puts the emptied page in the trash, its
// block leaving the body with it. Asked again, it finds the database in place.
func (turn databaseTurn) placeInline(
	ctx context.Context, app core.App, ygo *pocketBaseYgoPersistence, parentID, pageID, blockID string,
) error {
	_, err := ygo.applyContentGraphMutationContext(
		ctx, turn.actorID, parentID,
		graphMutationInput{MutationID: turn.name + "-inline:" + newDiagnosticReference()},
		contentGraphMutationHooks{
			Authorize: turn.authorize(parentID),
			Prepare: func(_ core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				metadata, err := structuralMetadata(doc)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				pageBlock, exists := metadata[pageID]
				if _, moved := metadata[blockID]; !exists || !moved {
					return contentGraphMutationPreparation{}, nil
				}
				afterID := pageID
				move := graphBlockMove{BlockID: blockID, AfterID: &afterID}
				if pageBlock.ParentID != "" {
					parent := pageBlock.ParentID
					move.ParentID = &parent
				}
				return contentGraphMutationPreparation{Operations: []graphOperation{{Kind: graphOperationKindMoveBlock, Move: &move}}}, nil
			},
		},
	)
	if err != nil {
		return err
	}
	return turn.trash(app, pageID)
}

// trash puts the page a database left in the trash, as a page's trash does.
func (turn databaseTurn) trash(app core.App, pageID string) error {
	status := pageMetadataApplied
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		input := pageMetadataInput{
			Title: current.GetString("title"), ParentID: current.GetString("parent_id"), InTrash: true,
			MutationID: turn.name + "-turn-trash:" + newDiagnosticReference(), BaseCursor: current.GetInt("cursor"),
		}
		_, status, err = applyPageMetadataTransaction(txApp, turn.actorID, pageID, input, pageMetadataRequestHash(pageID, input))
		return err
	})
	switch {
	case err != nil:
		return err
	case status == pageMetadataPageMissing:
		return errDatabaseRowPageNotFound
	case status != pageMetadataApplied:
		return graphConflictError(errorCodeGraphRevisionConflict, "The database page could not be changed in its current state.")
	}
	return nil
}

// notionDatabaseTurnOf is the turn a connection asks for: the pages it may
// reach, none in the trash.
func notionDatabaseTurnOf(identity notionOAuthBearerIdentity) databaseTurn {
	return databaseTurn{
		actorID: identity.PrincipalID, name: "notion-public-database",
		authorize: func(pageID string) func(core.App) (pageAuthorization, error) {
			return authorizeNotionPageMutation(identity, pageID)
		},
	}
}

// workspaceImportDatabaseTurnOf is the turn an import asks for: its
// importer's pages of its workspace they may change, none in the trash.
func workspaceImportDatabaseTurnOf(job storedJob) databaseTurn {
	return databaseTurn{
		actorID: job.Owner, name: "workspace-import-database",
		authorize: func(pageID string) func(core.App) (pageAuthorization, error) {
			return func(app core.App) (pageAuthorization, error) {
				current, err := authorizePageBody(app, job.Owner, pageID, permissionWorkspaceWrite)
				if err == nil && current.Workspace.WorkspaceID != job.WorkspaceID {
					err = permissionDenied()
				}
				if err != nil {
					if isPermissionDenied(err) {
						return pageAuthorization{}, graphNotFoundError()
					}
					return pageAuthorization{}, err
				}
				trashed, err := pageInTrashPath(app, pageID)
				if err != nil {
					return pageAuthorization{}, err
				}
				if trashed {
					return pageAuthorization{}, graphNotFoundError()
				}
				return current, nil
			}
		},
	}
}
