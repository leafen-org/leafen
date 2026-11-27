package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Undoing a move of a data source alone takes it back to the database it
// left, as the move notice's "실행 취소" and ⌘Z do in Notion (seen
// 2026-11-11, to a page and to a database): the source returns to its place
// among that database's own sources, the views it kept there as linked views
// are its own again, the table view the move made goes, and so does a
// database the move made for it. Its other databases' links follow it back.
// A move that took the views along is not undone, as Notion's answers "이
// 이동은 허용되지 않습니다." to it; the original database no longer links the
// source then, which refuses it here too.

// dataSourceMoveBack is what an undone move restores: the source's place
// among its original database's own sources and the view the move made.
type dataSourceMoveBack struct {
	OriginIndex int    `json:"originIndex"`
	ViewID      string `json:"viewId,omitempty"`
}

// dataSourceMoveBackHolderOperations lets the database the source moved to
// give it up. The table view the move made goes; views made of the source
// there since stay as linked views. A database left with nothing goes too,
// which it reports.
func dataSourceMoveBackHolderOperations(
	app core.App, doc *crdt.Doc, pageID, ownerID, holderID, originID, sourceID string, back dataSourceMoveBack,
) ([]graphOperation, bool, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return nil, false, err
	}
	entry, holder, err := notionDataSourceMoveContainer(metadata, holderID)
	if err != nil {
		return nil, false, err
	}
	own := holder.ownDataSourceIDs()
	if !slices.Contains(own, sourceID) {
		return nil, false, errDatabaseRowPageConflict
	}
	remaining := slices.DeleteFunc(slices.Clone(own), func(id string) bool { return id == sourceID })
	linked := holder.linkedMemberList()
	pageViews, err := loadDatabaseViewRecords(app, pageID)
	if err != nil {
		return nil, false, err
	}
	deletions, kept := []graphOperation{}, []graphOperation{}
	shown := map[string]string{}
	for id, stored := range pageViews {
		switch {
		case stored.DataSourceID == sourceID && stored.ContainerID == "":
			if id == back.ViewID {
				deletions = append(deletions, graphOperation{
					Kind: graphOperationKindDeleteView, ViewDeletion: &graphDatabaseDelete{ID: id},
				})
				continue
			}
			config, err := json.Marshal(stored.Config)
			if err != nil {
				return nil, false, err
			}
			kept = append(kept, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
				ID: id, DataSourceID: sourceID, ContainerID: holderID,
				Type: stored.Type, Name: stored.Name, Position: stored.Position, Config: config,
			}})
			shown[stored.Position+"\x00"+id] = id
		case stored.ContainerID == holderID || stored.ContainerID == "" && slices.Contains(remaining, stored.DataSourceID):
			shown[stored.Position+"\x00"+id] = id
		}
	}
	if len(remaining)+len(linked)+len(kept) == 0 {
		return append(deletions, graphOperation{
			Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: holderID},
		}), true, nil
	}
	if len(kept) > 0 {
		linked = append(linked, databaseContainerLinkedMember{DataSourceID: sourceID, SourceContainerID: originID})
	}
	content := withDatabaseContainerMembers(entry.Content, remaining, linked)
	// A database whose selected view goes selects its first remaining view.
	if selected, _ := content["viewId"].(string); !slices.Contains(mapsValues(shown), selected) {
		keys := make([]string, 0, len(shown))
		for key := range shown {
			keys = append(keys, key)
		}
		if len(keys) == 0 {
			return nil, false, errDatabaseRowPageConflict
		}
		slices.Sort(keys)
		content["viewId"] = shown[keys[0]]
	}
	block, err := notionDataSourceMoveBlock(app, entry, holderID, pageID, ownerID, content)
	if err != nil {
		return nil, false, err
	}
	slices.SortFunc(kept, func(left, right graphOperation) int { return strings.Compare(left.View.ID, right.View.ID) })
	operations := append(deletions, graphOperation{Kind: graphOperationKindUpsertBlock, Block: block})
	return append(operations, kept...), false, nil
}

// dataSourceMoveBackOriginOperations takes the source back into the database
// it left, at its place among the own sources, and makes the linked views it
// kept of the source its own views again. The database must still link the
// source where it went.
func dataSourceMoveBackOriginOperations(
	app core.App, doc *crdt.Doc, pageID, ownerID, originID, holderID, sourceID string, index int,
) ([]graphOperation, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return nil, err
	}
	entry, origin, err := notionDataSourceMoveContainer(metadata, originID)
	if err != nil {
		return nil, err
	}
	linked := origin.linkedMemberList()
	at := slices.Index(linked, databaseContainerLinkedMember{DataSourceID: sourceID, SourceContainerID: holderID})
	own := origin.ownDataSourceIDs()
	if at < 0 || slices.Contains(own, sourceID) {
		return nil, errDatabaseRowPageConflict
	}
	linked = slices.Delete(linked, at, at+1)
	own = slices.Insert(own, min(max(index, 0), len(own)), sourceID)
	block, err := notionDataSourceMoveBlock(app, entry, originID, pageID, ownerID,
		withDatabaseContainerMembers(entry.Content, own, linked))
	if err != nil {
		return nil, err
	}
	pageViews, err := loadDatabaseViewRecords(app, pageID)
	if err != nil {
		return nil, err
	}
	views := []graphOperation{}
	for id, stored := range pageViews {
		if stored.DataSourceID != sourceID || stored.ContainerID != originID {
			continue
		}
		config, err := json.Marshal(stored.Config)
		if err != nil {
			return nil, err
		}
		views = append(views, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: id, DataSourceID: sourceID, Type: stored.Type, Name: stored.Name, Position: stored.Position, Config: config,
		}})
	}
	slices.SortFunc(views, func(left, right graphOperation) int { return strings.Compare(left.View.ID, right.View.ID) })
	return append([]graphOperation{{Kind: graphOperationKindUpsertBlock, Block: block}}, views...), nil
}

// planDataSourceMoveBack takes the source back within one page: the database
// holding it lets it go first, then the original database takes it, then the
// databases here that linked it where it went follow it back.
func planDataSourceMoveBack(
	app core.App, doc *crdt.Doc, current notionDatabaseRowPageAuthorization,
	origin notionDataSourceMoveTarget, back dataSourceMoveBack,
) ([]graphOperation, bool, error) {
	sourceID, holderID, pageID := current.Container.DataSource, current.Container.ContainerID, current.Container.PageID
	if origin.PageID != pageID {
		return nil, false, errDatabaseRowPageConflict
	}
	linkers, err := dataSourceMoveLinkers(app, holderID, sourceID, origin.ContainerID)
	if err != nil {
		return nil, false, err
	}
	holder, removed, err := dataSourceMoveBackHolderOperations(
		app, doc, pageID, current.Container.OwnerID, holderID, origin.ContainerID, sourceID, back,
	)
	if err != nil {
		return nil, false, err
	}
	taken, err := dataSourceMoveBackOriginOperations(
		app, doc, pageID, origin.OwnerID, origin.ContainerID, holderID, sourceID, back.OriginIndex,
	)
	if err != nil {
		return nil, false, err
	}
	followers, err := dataSourceMoveLinkerOperations(app, doc, pageID, origin.OwnerID, linkers, sourceID, origin.ContainerID)
	if err != nil {
		return nil, false, err
	}
	return append(append(holder, taken...), followers...), removed, nil
}

// forgetDataSourceMoveDatabase leaves no deleted database behind for the
// database a move made and its undoing took away, nor the public ID the
// trash kept for it (keepDeletedDatabasePublicID).
func forgetDataSourceMoveDatabase(app core.App, blockID string) error {
	workspaceID := ""
	err := app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_deleted_databases}} WHERE [[block_id]]={:id}`).
		Bind(dbx.Params{"id": blockID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_deleted_databases}} WHERE [[block_id]]={:id}`).
		Bind(dbx.Params{"id": blockID}).Execute(); err != nil {
		return err
	}
	return dropDeletedDatabasePublicID(app, workspaceID, blockID)
}

// applyDataSourceMoveBackTurn is the last turn of taking a source back to a
// database on another page, once its data is there: the database holding it
// lets it go, then the original database takes it. It reports whether the
// databases changed so the move rolls back instead.
func applyDataSourceMoveBackTurn(
	app core.App, job storedBlockMoveJob, payload dataSourceMovePayload, batches map[string]*structuralMutationBatch,
) (bool, error) {
	holder, err := loadStructuralMutationBatch(app, job.SourcePageID, batches)
	if err != nil {
		return false, err
	}
	origin, err := loadStructuralMutationBatch(app, job.DestinationPageID, batches)
	if err != nil {
		return false, err
	}
	holderOperations, removed, err := dataSourceMoveBackHolderOperations(
		app, holder.doc, job.SourcePageID, job.OwnerID, job.RootBlockID, job.TargetRootBlockID,
		payload.DataSourceID, *payload.Back,
	)
	if errors.Is(err, errDatabaseRowPageConflict) {
		return false, beginBlockMoveRollback(app, job, errorCodeBlockMoveConflict)
	}
	if err != nil {
		return false, err
	}
	originOperations, err := dataSourceMoveBackOriginOperations(
		app, origin.doc, job.DestinationPageID, job.OwnerID, job.TargetRootBlockID, job.RootBlockID,
		payload.DataSourceID, payload.Back.OriginIndex,
	)
	if errors.Is(err, errDatabaseRowPageConflict) {
		return false, beginBlockMoveRollback(app, job, errorCodeBlockMoveConflict)
	}
	if err != nil {
		return false, err
	}
	noteDataSourceMoving(app, job.WorkspaceID, payload.DataSourceID)
	if err := applyDataSourceMoveStep(app, job, job.SourcePageID, holder, holderOperations); err != nil {
		return false, err
	}
	if removed {
		if err := forgetDataSourceMoveDatabase(app, job.RootBlockID); err != nil {
			return false, err
		}
	}
	return true, applyDataSourceMoveStep(app, job, job.DestinationPageID, origin, originOperations)
}

// moveDatabaseDataSourceBack undoes a move of the source alone from a
// database on the requested page, where it is checked to be now.
func moveDatabaseDataSourceBack(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	dataSourceID := strings.TrimSpace(event.Request.PathValue("dataSourceId"))
	page, err := authorizePageBody(event.App, user.Id, pageID, permissionWorkspaceWrite)
	if err != nil || dataSourceID == "" {
		return databaseDataSourceNotFound(event)
	}
	input := databaseDataSourceMoveBackRequest{}
	if event.BindBody(&input) != nil || strings.TrimSpace(input.DatabaseID) == "" || strings.TrimSpace(input.FromDatabaseID) == "" {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseDataSourceInvalid, "Choose where to move it back.")
	}
	identity := notionOAuthBearerIdentity{PrincipalID: user.Id, WorkspaceID: page.Workspace.WorkspaceID}
	current, err := authorizeNotionDataSourceParentCurrent(event.App, identity, dataSourceID, permissionWorkspaceWrite)
	if err != nil {
		return databaseDataSourceError(event, err, "move")
	}
	origin, err := authorizeDataSourceMoveContainer(event.App, identity, strings.TrimSpace(input.DatabaseID))
	if err != nil || origin.PageID != pageID {
		return databaseDataSourceNotFound(event)
	}
	if current.Container.ContainerID != strings.TrimSpace(input.FromDatabaseID) {
		return databaseDataSourceError(event, errDatabaseRowPageConflict, "move")
	}
	back := dataSourceMoveBack{OriginIndex: input.Index, ViewID: strings.TrimSpace(input.ViewID)}
	response := databaseDataSourceResponse{DataSourceID: dataSourceID, Status: "moved", DatabaseID: origin.ContainerID}
	if origin.PageID == current.Container.PageID {
		removed := false
		err := applyNotionDataSourceMutation(
			event.Request.Context(), ygo, current, "database-data-source-move-back:"+newDiagnosticReference(),
			func(txApp core.App, now notionDatabaseRowPageAuthorization, doc *crdt.Doc) ([]graphOperation, error) {
				if again, err := authorizeDataSourceMoveContainer(txApp, now.Identity, origin.ContainerID); err != nil || again != origin {
					return nil, refusedUnlessStorage(errDatabaseRowPageConflict, err)
				}
				operations, gone, err := planDataSourceMoveBack(txApp, doc, now, origin, back)
				removed = gone
				return operations, err
			},
			// Forgetting the database the move left in the trash changes
			// nothing of the page's graph.
			func(txApp core.App) (bool, error) {
				if !removed {
					return false, nil
				}
				return false, forgetDataSourceMoveDatabase(txApp, current.Container.ContainerID)
			},
		)
		if err == nil {
			err = followDataSourceMoveLinkersElsewhere(event.App, current, current.Container.ContainerID, origin.ContainerID)
		}
		if err != nil {
			return databaseDataSourceError(event, err, "move")
		}
		return event.JSON(http.StatusOK, response)
	}
	jobID := ""
	err = event.App.RunInTransaction(func(txApp core.App) error {
		now, err := authorizeNotionDataSourceParentCurrent(txApp, identity, dataSourceID, permissionWorkspaceWrite)
		if err != nil || now.Container.ContainerID != current.Container.ContainerID {
			return refusedUnlessStorage(errDatabaseRowPageConflict, err)
		}
		if again, err := authorizeDataSourceMoveContainer(txApp, identity, origin.ContainerID); err != nil || again != origin {
			return refusedUnlessStorage(errDatabaseRowPageConflict, err)
		}
		jobID, err = admitDataSourceMove(txApp, user.Id, now, origin, dataSourceMoveSpec{TargetID: origin.ContainerID, Back: &back})
		return err
	})
	if err != nil {
		return databaseDataSourceError(event, err, "move")
	}
	phase, err := driveDataSourceMove(event.Request.Context(), event.App, jobID, databaseDataSourceMoveWait)
	switch {
	case err != nil:
		return databaseDataSourceError(event, err, "move")
	case phase == blockMovePhaseFailed:
		return apiError(event, http.StatusConflict, errorCodeDatabaseDataSourceConflict,
			"The database changed, so the data source couldn't be moved back.")
	case phase != blockMovePhaseCompleted:
		// It goes on behind, followed at GET /jobs/{id} by its job.
		response.Status, response.JobID = "moving", jobID
		return event.JSON(http.StatusAccepted, response)
	}
	return event.JSON(http.StatusOK, response)
}
