package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedDatabaseAutomationDefinition struct {
	ID              string `db:"id"`
	OwnerID         string `db:"owner"`
	WorkspaceID     string `db:"workspace_id"`
	PageID          string `db:"page_id"`
	DatabaseID      string `db:"database_id"`
	Name            string `db:"name"`
	ScopeType       string `db:"scope_type"`
	ViewID          string `db:"view_id"`
	TriggerType     string `db:"trigger_type"`
	TriggerJSON     string `db:"trigger_json"`
	ActionJSON      string `db:"action_json"`
	Status          string `db:"status"`
	Revision        int    `db:"revision"`
	CreatedFrontier int    `db:"created_frontier"`
	UpdatedFrontier int    `db:"updated_frontier"`
	NextRunAt       int64  `db:"next_run_at"`
	PausedReason    string `db:"paused_reason"`
	CreatedBy       string `db:"created_by"`
	UpdatedBy       string `db:"updated_by"`
	CreatedAt       int64  `db:"created"`
	UpdatedAt       int64  `db:"updated"`
}

type storedDatabaseAutomationMutation struct {
	RequestHash  string `db:"request_hash"`
	ResponseJSON string `db:"response_json"`
}

func databaseAutomationRevisionMatches(current, expectedRevision int) bool {
	return current == expectedRevision
}

func databaseAutomationRequestHash(operation, pageID, databaseID, automationID string, input any) (string, error) {
	canonical, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{operation, pageID, databaseID, automationID, string(canonical)}, "\x00")))
	return hex.EncodeToString(digest[:]), nil
}

func loadDatabaseAutomationMutationReplay(
	app core.App, actorID, mutationID, requestHash string,
) (databaseAutomationMutationResponse, bool, error) {
	row := storedDatabaseAutomationMutation{}
	err := app.DB().NewQuery(`
		SELECT [[request_hash]], [[response_json]] FROM {{leafen_database_automation_mutations}}
		WHERE [[actor_id]] = {:actorId} AND [[mutation_id]] = {:mutationId}
	`).Bind(dbx.Params{"actorId": actorID, "mutationId": mutationID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return databaseAutomationMutationResponse{}, false, nil
	}
	if err != nil {
		return databaseAutomationMutationResponse{}, false, err
	}
	if row.RequestHash != requestHash {
		return databaseAutomationMutationResponse{}, false, errDatabaseAutomationMutationIDReused
	}
	response := databaseAutomationMutationResponse{}
	if len(row.ResponseJSON) > databaseAutomationMaxResultBytes || json.Unmarshal([]byte(row.ResponseJSON), &response) != nil {
		return databaseAutomationMutationResponse{}, false, errDatabaseAutomationUnavailable
	}
	response.Duplicate = true
	return response, true, nil
}

func insertDatabaseAutomationMutation(
	app core.App,
	actorID, mutationID, requestHash string,
	definition storedDatabaseAutomationDefinition,
	response databaseAutomationMutationResponse,
) error {
	payload, err := json.Marshal(response)
	if err != nil || len(payload) > databaseAutomationMaxResultBytes {
		return failedBecause(errDatabaseAutomationUnavailable, err)
	}
	_, err = app.DB().Insert("leafen_database_automation_mutations", dbx.Params{
		"actor_id": actorID, "mutation_id": mutationID, "request_hash": requestHash,
		"workspace_id": definition.WorkspaceID, "page_id": definition.PageID,
		"database_id": definition.DatabaseID, "definition_id": definition.ID,
		"response_json": string(payload), "created": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

func loadDatabaseAutomationDefinition(
	app core.App, ownerID, pageID, databaseID, automationID string,
) (storedDatabaseAutomationDefinition, error) {
	row := storedDatabaseAutomationDefinition{}
	err := app.DB().NewQuery(`
		SELECT [[definition]].[[id]], [[definition]].[[owner]], [[definition]].[[workspace_id]],
			[[definition]].[[page_id]], [[definition]].[[database_id]], [[definition]].[[name]],
			[[definition]].[[scope_type]], [[definition]].[[view_id]], [[definition]].[[trigger_type]],
			[[definition]].[[trigger_json]], [[definition]].[[action_json]], [[definition]].[[status]],
			[[definition]].[[revision]], [[definition]].[[created_frontier]], [[definition]].[[updated_frontier]],
			[[definition]].[[next_run_at]], [[definition]].[[paused_reason]],
			[[definition]].[[created_by]], [[definition]].[[updated_by]],
			[[definition]].[[created]], [[definition]].[[updated]]
		FROM {{leafen_database_automation_definitions}} AS [[definition]]
		WHERE [[definition]].[[owner]] = {:owner} AND [[definition]].[[page_id]] = {:pageId}
			AND [[definition]].[[database_id]] = {:databaseId} AND [[definition]].[[id]] = {:id}
			AND ` + databaseSourceNotTrashedSQL("[[definition]].[[database_id]]") + `
	`).Bind(dbx.Params{
		"owner": ownerID, "pageId": pageID, "databaseId": databaseID, "id": automationID,
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedDatabaseAutomationDefinition{}, errDatabaseAutomationNotFound
	}
	return row, err
}

func renderDatabaseAutomationDefinition(row storedDatabaseAutomationDefinition) (databaseAutomationDefinitionResponse, error) {
	response := databaseAutomationDefinitionResponse{
		ID: row.ID, WorkspaceID: row.WorkspaceID, PageID: row.PageID, DatabaseID: row.DatabaseID,
		Name: row.Name, Scope: databaseAutomationScope{Type: row.ScopeType, ViewID: row.ViewID},
		TriggerType: row.TriggerType, ActionDocument: json.RawMessage(row.ActionJSON),
		Status: row.Status, Revision: row.Revision,
		PausedReason: row.PausedReason, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.NextRunAt > 0 {
		response.NextRunAt = time.UnixMilli(row.NextRunAt).UTC().Format(time.RFC3339Nano)
	}
	if json.Unmarshal([]byte(row.TriggerJSON), &response.Trigger) != nil || !json.Valid(response.ActionDocument) {
		return databaseAutomationDefinitionResponse{}, errDatabaseAutomationUnavailable
	}
	return response, nil
}

func advanceDatabaseAutomationFrontier(
	app core.App, authorization pageAuthorization, databaseID string, activeDelta int,
) (int, error) {
	return advanceDatabaseAutomationFrontierOf(
		app, authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID,
		authorization.Page.Id, databaseID, activeDelta,
	)
}

// advanceDatabaseAutomationFrontierOf moves on a database's automation
// frontier, counting the automations that wait on its rows' changes; every
// change to an automation does, so the page's open room is told of it here
// once it is committed (announceDatabaseAutomationsAfterCommit).
func advanceDatabaseAutomationFrontierOf(
	app core.App, ownerID, workspaceID, pageID, databaseID string, activeDelta int,
) (int, error) {
	initialActiveCount := activeDelta
	if initialActiveCount < 0 {
		initialActiveCount = 0
	}
	var frontier int
	err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_automation_frontiers}}
			([[owner]], [[workspace_id]], [[page_id]], [[database_id]], [[frontier]], [[active_count]])
		VALUES ({:owner}, {:workspaceId}, {:pageId}, {:databaseId}, 1, {:initialActiveCount})
		ON CONFLICT ([[owner]], [[page_id]], [[database_id]]) DO UPDATE SET
			[[frontier]] = [[frontier]] + 1,
			[[active_count]] = [[active_count]] + {:activeDelta}
		RETURNING [[frontier]]
	`).Bind(dbx.Params{
		"owner": ownerID, "workspaceId": workspaceID, "pageId": pageID, "databaseId": databaseID,
		"initialActiveCount": initialActiveCount, "activeDelta": activeDelta,
	}).Row(&frontier)
	if err == nil {
		announceDatabaseAutomationsAfterCommit(app, pageID)
	}
	return frontier, err
}

func createDatabaseAutomationDefinition(
	app core.App, actorID, pageID, databaseID string, input databaseAutomationDefinitionInput,
) (databaseAutomationMutationResponse, error) {
	input.MutationID = strings.TrimSpace(input.MutationID)
	if !boundedAutomationString(input.MutationID, databaseAutomationMaxIdentifierRunes) {
		return databaseAutomationMutationResponse{}, errDatabaseAutomationMutationIDRefused
	}
	requestHash, err := databaseAutomationRequestHash("create", pageID, databaseID, "", input)
	if err != nil {
		return databaseAutomationMutationResponse{}, err
	}
	response := databaseAutomationMutationResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeDatabaseAutomationSource(txApp, actorID, pageID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		if replay, ok, err := loadDatabaseAutomationMutationReplay(txApp, actorID, input.MutationID, requestHash); err != nil || ok {
			response = replay
			return err
		}
		normalized, err := normalizeDatabaseAutomationDefinition(txApp, authorization, databaseID, input)
		if err != nil {
			return err
		}
		activeDelta := 0
		if normalized.TriggerType == databaseAutomationTriggerEvents {
			activeDelta = 1
		}
		frontier, err := advanceDatabaseAutomationFrontier(txApp, authorization, databaseID, activeDelta)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		row := storedDatabaseAutomationDefinition{
			ID: automationStableID("had", actorID, input.MutationID), OwnerID: authorization.Workspace.OwnerID,
			WorkspaceID: authorization.Workspace.WorkspaceID, PageID: pageID, DatabaseID: databaseID,
			Name: normalized.Name, ScopeType: normalized.Scope.Type, ViewID: normalized.Scope.ViewID,
			TriggerType: normalized.TriggerType,
			TriggerJSON: normalized.TriggerJSON, ActionJSON: normalized.ActionJSON,
			Status: databaseAutomationStatusActive, Revision: 1,
			CreatedFrontier: frontier, UpdatedFrontier: frontier, NextRunAt: normalized.NextRunAt,
			CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now,
		}
		_, err = txApp.DB().Insert("leafen_database_automation_definitions", dbx.Params{
			"id": row.ID, "owner": row.OwnerID, "workspace_id": row.WorkspaceID, "page_id": row.PageID,
			"database_id": row.DatabaseID, "name": row.Name, "scope_type": row.ScopeType, "view_id": row.ViewID,
			"trigger_type": row.TriggerType,
			"trigger_json": row.TriggerJSON, "action_json": row.ActionJSON, "status": row.Status,
			"revision": row.Revision, "created_frontier": frontier, "updated_frontier": frontier,
			"next_run_at": row.NextRunAt, "paused_reason": "", "created_by": actorID,
			"updated_by": actorID, "created": now, "updated": now,
		}).Execute()
		if err != nil {
			return err
		}
		definition, err := renderDatabaseAutomationDefinition(row)
		if err != nil {
			return err
		}
		response = databaseAutomationMutationResponse{Definition: definition}
		return insertDatabaseAutomationMutation(txApp, actorID, input.MutationID, requestHash, row, response)
	})
	return response, err
}
