package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// The editor's Manage data sources menu moves an own source to another
// database or into trash through the same owners as the public API, with the
// signed-in member's page permissions.

const databaseDataSourceMoveWait = 10 * time.Second

func registerDatabaseDataSourceRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.POST(apiRoot+databaseDataSourceMovePath, func(event *core.RequestEvent) error {
		return moveDatabaseDataSource(event, ygo)
	}).Bind(apis.BodyLimit(databaseDataSourceMaxBodyBytes))
	event.Router.POST(apiRoot+databaseDataSourceMoveBackPath, func(event *core.RequestEvent) error {
		return moveDatabaseDataSourceBack(event, ygo)
	}).Bind(apis.BodyLimit(databaseDataSourceMaxBodyBytes))
	event.Router.POST(apiRoot+databaseDataSourceTrashPath, func(event *core.RequestEvent) error {
		return trashDatabaseDataSource(event, ygo)
	}).Bind(apis.BodyLimit(databaseDataSourceMaxBodyBytes))
	event.Router.POST(apiRoot+databaseDataSourceRestorePath, func(event *core.RequestEvent) error {
		return restoreDatabaseDataSource(event, ygo)
	}).Bind(apis.BodyLimit(databaseDataSourceMaxBodyBytes))
}

// authorizeDatabaseDataSourceSession finds the own source on the requested
// page for a member who can edit that page; ok false when it answered the
// refusal instead, err then being that answer's write.
func authorizeDatabaseDataSourceSession(event *core.RequestEvent) (authorization notionDatabaseRowPageAuthorization, ok bool, err error) {
	user, signedIn := authenticatedUser(event)
	if !signedIn {
		return authorization, false, apiError(
			event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.",
		)
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	dataSourceID := strings.TrimSpace(event.Request.PathValue("dataSourceId"))
	page, err := authorizePageBody(event.App, user.Id, pageID, permissionWorkspaceWrite)
	if err != nil || dataSourceID == "" {
		return authorization, false, databaseDataSourceNotFound(event)
	}
	identity := notionOAuthBearerIdentity{PrincipalID: user.Id, WorkspaceID: page.Workspace.WorkspaceID}
	authorization, err = authorizeNotionDataSourceParentCurrent(
		event.App, identity, dataSourceID, permissionWorkspaceWrite,
	)
	if err != nil || authorization.Container.PageID != pageID {
		return notionDatabaseRowPageAuthorization{}, false, databaseDataSourceNotFound(event)
	}
	return authorization, true, nil
}

func moveDatabaseDataSource(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	authorization, ok, err := authorizeDatabaseDataSourceSession(event)
	if !ok {
		return err
	}
	input := databaseDataSourceMoveRequest{}
	if event.BindBody(&input) != nil {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseDataSourceInvalid, "Choose where to move it.")
	}
	request := dataSourceMoveRequest{
		DatabaseID: strings.TrimSpace(input.DatabaseID), PageID: strings.TrimSpace(input.PageID), MoveViews: input.MoveViews,
		NamedAfterSource: true,
	}
	target, spec, err := resolveDataSourceMoveTarget(event.App, authorization.Identity, request)
	if err != nil {
		return databaseDataSourceError(event, err, "move")
	}
	response := databaseDataSourceResponse{DataSourceID: authorization.Container.DataSource, Status: "moved"}
	if !spec.NewDatabase && target.ContainerID == authorization.Container.ContainerID {
		return event.JSON(http.StatusOK, response)
	}
	// Where it went and the table view made there, which its undoing takes away.
	response.DatabaseID = spec.TargetID
	if !spec.MoveViews {
		response.ViewID = spec.ViewID
	}
	if target.PageID == authorization.Container.PageID {
		err := applyNotionDataSourceMutation(
			event.Request.Context(), ygo, authorization, "database-data-source-move:"+newDiagnosticReference(),
			func(txApp core.App, current notionDatabaseRowPageAuthorization, doc *crdt.Doc) ([]graphOperation, error) {
				if !spec.NewDatabase {
					currentTarget, err := authorizeDataSourceMoveContainer(txApp, current.Identity, target.ContainerID)
					if err != nil || currentTarget != target {
						return nil, refusedUnlessStorage(errDatabaseRowPageConflict, err)
					}
				}
				named := spec
				named.ViewName = dataSourceMoveViewName(current.Container.Title)
				return planNotionDataSourceMove(txApp, doc, current, target, named)
			},
			nil,
		)
		if err == nil {
			err = followDataSourceMoveLinkersElsewhere(event.App, authorization, authorization.Container.ContainerID, spec.TargetID)
		}
		if err != nil {
			return databaseDataSourceError(event, err, "move")
		}
		return event.JSON(http.StatusOK, response)
	}
	jobID, moved, err := startDataSourceCrossPageMove(event.App, authorization, request)
	if err != nil {
		return databaseDataSourceError(event, err, "move")
	}
	response.DatabaseID, response.ViewID = moved.TargetID, ""
	if !moved.MoveViews {
		response.ViewID = moved.ViewID
	}
	phase, err := driveDataSourceMove(event.Request.Context(), event.App, jobID, databaseDataSourceMoveWait)
	switch {
	case err != nil:
		return databaseDataSourceError(event, err, "move")
	case phase == blockMovePhaseFailed:
		return apiError(event, http.StatusConflict, errorCodeDatabaseDataSourceConflict,
			"The database changed during the move, so the data source couldn't be moved.")
	case phase != blockMovePhaseCompleted:
		// It goes on behind, followed at GET /jobs/{id} by its job.
		response.Status, response.JobID = "moving", jobID
		return event.JSON(http.StatusAccepted, response)
	}
	return event.JSON(http.StatusOK, response)
}

func trashDatabaseDataSource(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	authorization, ok, err := authorizeDatabaseDataSourceSession(event)
	if !ok {
		return err
	}
	if err := trashNotionDataSource(event, ygo, authorization, true); err != nil {
		return databaseDataSourceError(event, err, "trash")
	}
	return event.JSON(http.StatusOK, databaseDataSourceResponse{
		DataSourceID: authorization.Container.DataSource, Status: "trashed",
	})
}

// restoreDatabaseDataSource takes a source this page's database sent to the
// trash back into it, as the trash notice's "실행 취소" asks (Notion's,
// seen 2026-11-02).
func restoreDatabaseDataSource(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
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
	identity := notionOAuthBearerIdentity{PrincipalID: user.Id, WorkspaceID: page.Workspace.WorkspaceID}
	authorization, state, err := authorizeNotionDataSourceAnyState(event.App, identity, dataSourceID, permissionWorkspaceWrite)
	// One of a deleted database comes back with its database (deleted_databases.go).
	if err != nil || state.PageID != pageID || state.Deleted {
		return databaseDataSourceNotFound(event)
	}
	if state.trashed() {
		if err := restoreNotionDataSource(event, ygo, authorization, state, false); err != nil {
			return databaseDataSourceError(event, err, "restore")
		}
	}
	return event.JSON(http.StatusOK, databaseDataSourceResponse{DataSourceID: dataSourceID, Status: "restored"})
}

func databaseDataSourceNotFound(event *core.RequestEvent) error {
	return apiError(event, http.StatusNotFound, errorCodeDatabaseDataSourceNotFound, "Data source not found.")
}

func databaseDataSourceError(event *core.RequestEvent, err error, stage string) error {
	graphError := &graphRequestError{}
	switch {
	case errors.As(err, &graphError) && graphError.status == http.StatusBadRequest:
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseDataSourceInvalid, graphError.message)
	case errors.Is(err, errDatabaseRowPageNotFound) || errors.As(err, &graphError) && graphError.status == http.StatusNotFound:
		return databaseDataSourceNotFound(event)
	case errors.Is(err, errDatabaseRowPageConflict) || errors.As(err, &graphError) && graphError.status == http.StatusConflict:
		return apiError(event, http.StatusConflict, errorCodeDatabaseDataSourceConflict,
			"The database changed. Try again.")
	}
	return apiErrorWithCause(event, http.StatusServiceUnavailable, errorCodeDatabaseDataSourceInvalid,
		"Couldn't change the data source.", "database_data_source", stage, errorCodeDatabaseDataSourceInvalid, err)
}
