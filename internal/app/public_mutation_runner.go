package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const publicMutationEffectLockNamespace = "public-mutation-effects"

func registerPublicMutationRunnerRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+publicMutationOperationPath, readPublicMutationOperation)
	event.Router.POST(apiRoot+publicMutationContinuePath, continuePublicMutationOperation)
}

func readPublicMutationOperation(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	authorization, err := authorizePublicMutationManager(event.App, user.Id, pageID)
	if err != nil {
		return publicMutationRouteError(event, err, "operation-authorization")
	}
	operation, err := loadPublicMutationOperation(
		event.App, authorization.Workspace.WorkspaceID, pageID,
		strings.TrimSpace(event.Request.PathValue("operationId")),
	)
	if err != nil {
		return publicMutationRouteError(event, err, "operation-read")
	}
	return event.JSON(http.StatusOK, publicMutationOperationResponse{
		Operation: projectPublicMutationOperation(operation),
	})
}

func continuePublicMutationOperation(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	authorization, err := authorizePublicMutationManager(event.App, user.Id, pageID)
	if err != nil {
		return publicMutationRouteError(event, err, "continue-authorization")
	}
	operationID := strings.TrimSpace(event.Request.PathValue("operationId"))
	operation, err := loadPublicMutationOperation(
		event.App, authorization.Workspace.WorkspaceID, pageID, operationID,
	)
	if err != nil {
		return publicMutationRouteError(event, err, "continue-read")
	}
	if operation.Status == publicMutationOperationPending {
		release := acquireProductOperationLock(
			publicMutationEffectLockNamespace, operation.WorkspaceID+"\x00"+operation.PageID,
		)
		defer release()
		if _, err := authorizePublicMutationManager(event.App, user.Id, pageID); err != nil {
			return publicMutationRouteError(event, err, "continue-reauthorization")
		}
		operation, err = loadPublicMutationOperation(event.App, operation.WorkspaceID, pageID, operationID)
		if err != nil {
			return publicMutationRouteError(event, err, "continue-refresh")
		}
		if operation.Status == publicMutationOperationPending {
			if err := runPublicMutationEffectWindow(event.App, operation); err != nil {
				return publicMutationRouteError(event, err, "continue-settlement")
			}
			operation, err = loadPublicMutationOperation(event.App, operation.WorkspaceID, pageID, operationID)
			if err != nil {
				return publicMutationRouteError(event, err, "continue-result")
			}
		}
	}
	httpStatus := http.StatusAccepted
	if operation.Status != publicMutationOperationPending {
		httpStatus = http.StatusOK
	}
	return event.JSON(httpStatus, publicMutationOperationResponse{
		Operation: projectPublicMutationOperation(operation),
	})
}

func loadPublicMutationOperation(
	app core.App, workspaceID, pageID, operationID string,
) (storedPublicMutationOperation, error) {
	if workspaceID == "" || pageID == "" || operationID == "" {
		return storedPublicMutationOperation{}, errPublicMutationNotFound
	}
	row := storedPublicMutationOperation{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[actor_id]],[[request_id]],[[request_hash]],[[action]],
			[[revision]],[[status]],[[error_code]],[[response_json]],[[created_at]],[[updated_at]]
		FROM {{leafen_public_mutation_operations}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{
		"id": operationID, "workspaceId": workspaceID, "pageId": pageID,
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedPublicMutationOperation{}, errPublicMutationNotFound
	}
	return row, err
}

func projectPublicMutationOperation(row storedPublicMutationOperation) publicMutationOperation {
	return publicMutationOperation{
		ID: row.ID, RequestID: row.RequestID, Action: row.Action, Status: row.Status,
		Revision: row.Revision, ErrorCode: row.ErrorCode,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func finalizePublicMutationOperation(app core.App, operationID string) error {
	return app.RunInTransaction(func(txApp core.App) error {
		var pending, failed int
		if err := txApp.DB().NewQuery(`
			SELECT
				COALESCE(SUM(CASE WHEN [[status]]='pending' THEN 1 ELSE 0 END),0),
				COALESCE(SUM(CASE WHEN [[status]]='failed' THEN 1 ELSE 0 END),0)
			FROM {{leafen_public_mutation_effects}} WHERE [[operation_id]]={:operationId}
		`).Bind(dbx.Params{"operationId": operationID}).Row(&pending, &failed); err != nil {
			return err
		}
		if pending > 0 {
			return nil
		}
		operationStatus, errorCode := publicMutationOperationCompleted, ""
		if failed > 0 {
			operationStatus, errorCode = publicMutationOperationFailed, errorCodePublicMutationFailed
		}
		_, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_public_mutation_operations}}
			SET [[status]]={:status},[[error_code]]={:errorCode},[[updated_at]]={:now}
			WHERE [[id]]={:id} AND [[status]]='pending'
		`).Bind(dbx.Params{
			"status": operationStatus, "errorCode": errorCode,
			"now": time.Now().UTC().UnixMilli(), "id": operationID,
		}).Execute()
		return err
	})
}
