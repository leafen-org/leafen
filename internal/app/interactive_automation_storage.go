package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedInteractiveAutomationExecution struct {
	ActorID          string `db:"actor_id"`
	ExecutionID      string `db:"execution_id"`
	OwnerID          string `db:"owner_id"`
	WorkspaceID      string `db:"workspace_id"`
	PageID           string `db:"page_id"`
	SourceKind       string `db:"source_kind"`
	SourceID         string `db:"source_id"`
	TargetHash       string `db:"target_hash"`
	DocumentHash     string `db:"document_hash"`
	ConfirmationHash string `db:"confirmation_hash"`
	ResultJSON       string `db:"result_json"`
	CreatedAt        int64  `db:"created_at"`
}

func loadInteractiveAutomationExecution(
	app core.App,
	actorID string,
	executionID string,
) (storedInteractiveAutomationExecution, bool, error) {
	row := storedInteractiveAutomationExecution{}
	err := app.DB().NewQuery(`
		SELECT [[actor_id]], [[execution_id]], [[owner_id]], [[workspace_id]], [[page_id]],
			[[source_kind]], [[source_id]], [[target_hash]], [[document_hash]],
			[[confirmation_hash]], [[result_json]], [[created_at]]
		FROM {{leafen_interactive_automation_executions}}
		WHERE [[actor_id]] = {:actorId} AND [[execution_id]] = {:executionId}
	`).Bind(dbx.Params{"actorId": actorID, "executionId": executionID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedInteractiveAutomationExecution{}, false, nil
	}
	if err != nil {
		return storedInteractiveAutomationExecution{}, false, err
	}
	return row, true, nil
}

func insertInteractiveAutomationExecution(
	app core.App,
	row storedInteractiveAutomationExecution,
) error {
	_, err := app.DB().Insert(interactiveAutomationExecutionsTable, dbx.Params{
		"actor_id": row.ActorID, "execution_id": row.ExecutionID,
		"owner_id": row.OwnerID, "workspace_id": row.WorkspaceID, "page_id": row.PageID,
		"source_kind": row.SourceKind, "source_id": row.SourceID,
		"target_hash": row.TargetHash, "document_hash": row.DocumentHash,
		"confirmation_hash": row.ConfirmationHash, "result_json": row.ResultJSON,
		"created_at": row.CreatedAt,
	}).Execute()
	return err
}

func interactiveAutomationResponseFromStored(
	row storedInteractiveAutomationExecution,
) (interactiveAutomationExecuteResponse, error) {
	response := interactiveAutomationExecuteResponse{}
	if row.ActorID == "" || row.ExecutionID == "" || row.OwnerID == "" ||
		row.WorkspaceID == "" || row.PageID == "" || row.SourceID == "" ||
		len(row.TargetHash) != 64 || len(row.DocumentHash) != 64 ||
		len(row.ConfirmationHash) != 64 || len([]byte(row.ResultJSON)) > interactiveAutomationMaxResultBytes ||
		json.Unmarshal([]byte(row.ResultJSON), &response) != nil || response.Receipt == nil ||
		response.Receipt.ExecutionID != row.ExecutionID || response.Receipt.Duplicate {
		return interactiveAutomationExecuteResponse{}, errInteractiveAutomationUnavailable
	}
	response.Receipt.Duplicate = true
	return response, nil
}
