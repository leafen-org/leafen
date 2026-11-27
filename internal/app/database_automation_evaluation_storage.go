package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Evaluation storage owns candidate selection and durable outcomes; action
// execution calls it inside the existing room transaction.
func loadDatabaseAutomationEvaluationCandidates(
	app core.App, jobID string,
) ([]databaseAutomationEvaluationCandidate, error) {
	rows := []databaseAutomationEvaluationCandidate{}
	err := app.DB().NewQuery(`
		SELECT [[event]].[[id]] AS [[event_id]], [[event]].[[owner]], [[event]].[[actor_id]], [[event]].[[workspace_id]],
			[[definition]].[[updated_by]] AS [[runner_id]],
			[[event]].[[page_id]], [[event]].[[database_id]], [[event]].[[row_id]],
			[[event]].[[event_type]], [[event]].[[changed_property_ids]], [[event]].[[row_snapshot_json]],
			[[event]].[[created]] AS [[event_created]], [[event]].[[captured_frontier]],
			[[definition]].[[id]] AS [[definition_id]], [[definition]].[[name]] AS [[definition_name]],
			[[definition]].[[revision]] AS [[definition_revision]], [[definition]].[[updated_frontier]],
			[[definition]].[[status]] AS [[definition_status]], [[definition]].[[trigger_type]], [[definition]].[[scope_type]],
			[[definition]].[[view_id]], [[definition]].[[trigger_json]], [[definition]].[[action_json]]
		FROM {{leafen_database_automation_events}} [[event]]
			INDEXED BY [[idx_leafen_database_automation_events_job_id]]
		JOIN {{leafen_database_automation_definitions}} [[definition]]
			ON [[definition]].[[owner]] = [[event]].[[owner]]
			AND [[definition]].[[page_id]] = [[event]].[[page_id]]
			AND [[definition]].[[database_id]] = [[event]].[[database_id]]
			AND (([[event]].[[event_type]] = 'schedule'
				AND [[definition]].[[id]] = [[event]].[[definition_id]]
				AND [[definition]].[[revision]] = [[event]].[[definition_revision]])
				OR ([[event]].[[event_type]] <> 'schedule' AND [[event]].[[definition_id]] = ''
					AND [[definition]].[[trigger_type]] = 'events'))
			AND [[definition]].[[created_frontier]] <= [[event]].[[captured_frontier]]
			AND ([[definition]].[[status]] = 'active'
				OR [[definition]].[[updated_frontier]] > [[event]].[[captured_frontier]])
		LEFT JOIN {{leafen_database_automation_outcomes}} [[outcome]]
			ON [[outcome]].[[event_id]] = [[event]].[[id]]
			AND [[outcome]].[[definition_id]] = [[definition]].[[id]]
		WHERE [[event]].[[job_id]] = {:jobId} AND [[outcome]].[[id]] IS NULL
		ORDER BY [[event]].[[id]], [[definition]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"jobId": jobID, "limit": databaseAutomationEvaluationChunk}).All(&rows)
	return rows, err
}

func hasDatabaseAutomationEvaluationCandidates(app core.App, jobID string) (bool, error) {
	var remaining int
	err := app.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{leafen_database_automation_events}} [[event]]
				INDEXED BY [[idx_leafen_database_automation_events_job_id]]
			JOIN {{leafen_database_automation_definitions}} [[definition]]
				ON [[definition]].[[owner]] = [[event]].[[owner]]
				AND [[definition]].[[page_id]] = [[event]].[[page_id]]
				AND [[definition]].[[database_id]] = [[event]].[[database_id]]
				AND (([[event]].[[event_type]] = 'schedule'
					AND [[definition]].[[id]] = [[event]].[[definition_id]]
					AND [[definition]].[[revision]] = [[event]].[[definition_revision]])
					OR ([[event]].[[event_type]] <> 'schedule' AND [[event]].[[definition_id]] = ''
						AND [[definition]].[[trigger_type]] = 'events'))
				AND [[definition]].[[created_frontier]] <= [[event]].[[captured_frontier]]
				AND ([[definition]].[[status]] = 'active'
					OR [[definition]].[[updated_frontier]] > [[event]].[[captured_frontier]])
			LEFT JOIN {{leafen_database_automation_outcomes}} [[outcome]]
				ON [[outcome]].[[event_id]] = [[event]].[[id]]
				AND [[outcome]].[[definition_id]] = [[definition]].[[id]]
			WHERE [[event]].[[job_id]] = {:jobId} AND [[outcome]].[[id]] IS NULL
			LIMIT 1
		)
	`).Bind(dbx.Params{"jobId": jobID}).Row(&remaining)
	return remaining == 1, err
}

func insertDatabaseAutomationOutcome(
	app core.App, candidate databaseAutomationEvaluationCandidate, status, errorCode, resultJSON string,
) error {
	now := time.Now().UTC().UnixMilli()
	_, err := app.DB().Insert("leafen_database_automation_outcomes", dbx.Params{
		"id":    automationStableID("hao", candidate.EventID, candidate.DefinitionID),
		"owner": candidate.OwnerID, "workspace_id": candidate.WorkspaceID,
		"page_id": candidate.PageID, "database_id": candidate.DatabaseID,
		"event_id": candidate.EventID, "definition_id": candidate.DefinitionID,
		"definition_revision": candidate.DefinitionRevision, "status": status,
		"error_code": errorCode, "result_json": resultJSON, "created": now, "updated": now,
	}).Execute()
	return err
}

// databaseAutomationFailureReason is why an automation's actions failed:
// the organization's data rules, a bot its runner may no longer send to, or
// the actions themselves.
func databaseAutomationFailureReason(err error) string {
	if errors.Is(err, errDLPPolicyBlocked) || errors.Is(err, errDLPPolicyUnavailable) {
		return databaseAutomationPausedReasonDlp
	}
	return databaseAutomationPausedReasonAction
}

func pauseDatabaseAutomationDefinition(
	app core.App, candidate databaseAutomationEvaluationCandidate, reason string,
) (bool, error) {
	activeDelta := 0
	if candidate.TriggerType == databaseAutomationTriggerEvents {
		activeDelta = -1
	}
	var frontier int
	err := app.DB().NewQuery(`
		UPDATE {{leafen_database_automation_frontiers}}
		SET [[frontier]] = [[frontier]] + 1, [[active_count]] = [[active_count]] + {:activeDelta}
		WHERE [[owner]] = {:owner}
			AND [[page_id]] = {:pageId} AND [[database_id]] = {:databaseId}
			AND EXISTS (
				SELECT 1 FROM {{leafen_database_automation_definitions}}
				WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[page_id]] = {:pageId}
					AND [[database_id]] = {:databaseId} AND [[revision]] = {:revision}
					AND [[status]] = 'active'
			)
		RETURNING [[frontier]]
	`).Bind(dbx.Params{
		"id": candidate.DefinitionID, "owner": candidate.OwnerID,
		"pageId": candidate.PageID, "databaseId": candidate.DatabaseID,
		"revision": candidate.DefinitionRevision, "activeDelta": activeDelta,
	}).Row(&frontier)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_automation_definitions}}
		SET [[status]] = 'paused', [[revision]] = [[revision]] + 1,
			[[updated_frontier]] = {:frontier}, [[next_run_at]] = 0,
			[[paused_reason]] = {:reason}, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[database_id]] = {:databaseId} AND [[revision]] = {:revision}
			AND [[status]] = 'active'
	`).Bind(dbx.Params{
		"id": candidate.DefinitionID, "owner": candidate.OwnerID, "frontier": frontier,
		"pageId": candidate.PageID, "revision": candidate.DefinitionRevision,
		"databaseId": candidate.DatabaseID, "reason": reason, "updated": time.Now().UTC().UnixMilli(),
	}).Execute()
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if changed != 1 {
		return false, errDatabaseAutomationUnavailable
	}
	// An open list shows it paused at once, as a person's change does.
	announceDatabaseAutomationsAfterCommit(app, candidate.PageID)
	return true, nil
}
