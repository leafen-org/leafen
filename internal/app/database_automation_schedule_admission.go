package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type databaseAutomationScheduleAdmissionResult struct {
	Admitted int
	HasMore  bool
}

func registerDatabaseAutomationScheduleCron(app core.App, runner *durableJobRunner) {
	app.Cron().MustAdd("leafen-database-automation-schedules", "* * * * *", runner.notify)
}

func shouldScanDatabaseAutomationSchedules(now, nextScanAt time.Time, pending bool, activeLocalJobs int) bool {
	return nextScanAt.IsZero() || !now.Before(nextScanAt) || pending && activeLocalJobs == 0
}

func databaseAutomationScheduleIdentity(prefix, definitionID string, revision int, scheduledFor int64) string {
	return automationStableID(prefix, definitionID, strconv.Itoa(revision), strconv.FormatInt(scheduledFor, 10))
}

func databaseAutomationScheduleRequestHash(definition storedDatabaseAutomationDefinition, scheduledFor int64) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		definition.ID,
		strconv.Itoa(definition.Revision),
		strconv.FormatInt(scheduledFor, 10),
		definition.TriggerJSON,
		definition.ActionJSON,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func admitDueDatabaseAutomationSchedules(
	app core.App,
	dueAt time.Time,
) (databaseAutomationScheduleAdmissionResult, error) {
	result := databaseAutomationScheduleAdmissionResult{}
	dueMillis := dueAt.UTC().UnixMilli()
	err := app.RunInTransaction(func(txApp core.App) error {
		definitions := []storedDatabaseAutomationDefinition{}
		if err := txApp.DB().NewQuery(`
			SELECT [[id]], [[owner]], [[workspace_id]], [[page_id]], [[database_id]], [[name]],
				[[scope_type]], [[view_id]], [[trigger_type]], [[trigger_json]], [[action_json]],
				[[status]], [[revision]], [[created_frontier]], [[updated_frontier]], [[next_run_at]],
				[[paused_reason]], [[created_by]], [[updated_by]], [[created]], [[updated]]
			FROM {{leafen_database_automation_definitions}} [[definition]]
			WHERE [[trigger_type]] = 'schedule' AND [[status]] = 'active'
				AND [[next_run_at]] > 0 AND [[next_run_at]] <= {:dueAt}
				AND ` + databaseSourceNotTrashedSQL("[[definition]].[[database_id]]") + `
				AND ` + blockMovePageUnfencedSQL(`[[definition]].[[page_id]]`) + `
			ORDER BY [[next_run_at]], [[id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"dueAt": dueMillis, "limit": databaseAutomationScheduleAdmissionChunk + 1,
		}).All(&definitions); err != nil {
			return err
		}
		selected := definitions
		if len(selected) > databaseAutomationScheduleAdmissionChunk {
			selected = selected[:databaseAutomationScheduleAdmissionChunk]
		}
		for _, definition := range selected {
			trigger := databaseAutomationTrigger{}
			if json.Unmarshal([]byte(definition.TriggerJSON), &trigger) != nil ||
				validateDatabaseAutomationScheduleTrigger(trigger) != nil {
				paused, err := pauseDatabaseAutomationDefinition(txApp, databaseAutomationEvaluationCandidate{
					DefinitionID: definition.ID, OwnerID: definition.OwnerID,
					PageID: definition.PageID, DatabaseID: definition.DatabaseID,
					DefinitionRevision: definition.Revision, TriggerType: definition.TriggerType,
				}, databaseAutomationPausedReasonSchedule)
				if err != nil {
					return err
				}
				if !paused {
					continue
				}
				continue
			}

			scheduledFor := definition.NextRunAt
			next, exists, err := nextDatabaseAutomationScheduleRun(trigger, time.UnixMilli(scheduledFor).UTC())
			if err != nil {
				return err
			}
			// This run is its last when it ends after so many runs.
			left, err := databaseAutomationScheduleRunsLeft(txApp, definition.ID, trigger, 1)
			if err != nil {
				return err
			}
			nextRunAt := int64(0)
			if exists && left {
				nextRunAt = next.UnixMilli()
			}
			updated, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_database_automation_definitions}}
				SET [[next_run_at]] = {:nextRunAt}, [[updated]] = {:updated}
				WHERE [[id]] = {:id} AND [[revision]] = {:revision}
					AND [[status]] = 'active' AND [[trigger_type]] = 'schedule'
					AND [[next_run_at]] = {:scheduledFor}
			`).Bind(dbx.Params{
				"nextRunAt": nextRunAt, "updated": dueMillis, "id": definition.ID,
				"revision": definition.Revision, "scheduledFor": scheduledFor,
			}).Execute()
			if err != nil {
				return err
			}
			changed, _ := updated.RowsAffected()
			if changed != 1 {
				continue
			}

			occurrenceID := databaseAutomationScheduleIdentity("has", definition.ID, definition.Revision, scheduledFor)
			eventID := databaseAutomationScheduleIdentity("hae", definition.ID, definition.Revision, scheduledFor)
			jobID := databaseAutomationScheduleIdentity("haj", definition.ID, definition.Revision, scheduledFor)
			requestHash := databaseAutomationScheduleRequestHash(definition, scheduledFor)
			inserted, err := txApp.DB().NewQuery(`
				INSERT OR IGNORE INTO {{leafen_database_automation_schedule_occurrences}}
					([[id]], [[owner]], [[workspace_id]], [[page_id]], [[database_id]], [[definition_id]],
					 [[definition_revision]], [[scheduled_for]], [[event_id]], [[job_id]], [[status]],
					 [[created]], [[updated]])
				VALUES ({:id}, {:owner}, {:workspaceId}, {:pageId}, {:databaseId}, {:definitionId},
					{:definitionRevision}, {:scheduledFor}, {:eventId}, {:jobId}, 'queued', {:now}, {:now})
			`).Bind(dbx.Params{
				"id": occurrenceID, "owner": definition.OwnerID,
				"workspaceId": definition.WorkspaceID, "pageId": definition.PageID,
				"databaseId": definition.DatabaseID, "definitionId": definition.ID,
				"definitionRevision": definition.Revision, "scheduledFor": scheduledFor,
				"eventId": eventID, "jobId": jobID, "now": dueMillis,
			}).Execute()
			if err != nil {
				return err
			}
			created, _ := inserted.RowsAffected()
			if created == 0 {
				continue
			}
			if _, err := txApp.DB().Insert("leafen_database_automation_events", dbx.Params{
				"id": eventID, "owner": definition.OwnerID, "workspace_id": definition.WorkspaceID,
				"page_id": definition.PageID, "database_id": definition.DatabaseID, "row_id": "",
				"actor_id": definition.UpdatedBy, "mutation_id": occurrenceID,
				"request_hash": requestHash, "event_type": databaseAutomationScheduleEvent,
				"changed_property_ids": "[]", "row_snapshot_json": "{}",
				"captured_frontier": definition.UpdatedFrontier,
				"definition_id":     definition.ID, "definition_revision": definition.Revision,
				"job_id": jobID, "status": "queued", "created": scheduledFor, "updated": dueMillis,
			}).Execute(); err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string]any{
				"automationId": definition.ID, "revision": definition.Revision,
				"scheduledFor": scheduledFor,
			})
			requestID := "database-automation-schedule:" + occurrenceID
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_jobs}}
					([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]],
					 [[page_id]], [[status]], [[error_code]], [[not_before]], [[created]], [[updated]],
					 [[payload_json]], [[cursor]], [[processed_items]], [[total_items]], [[attempt]],
					 [[max_attempts]], [[chunk_count]])
				VALUES ({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind},
					{:pageId}, 'queued', '', {:notBefore}, {:now}, {:now}, {:payload}, '', 0, 1, 0,
					{:maxAttempts}, 0)
			`).Bind(dbx.Params{
				"id": jobID, "owner": definition.OwnerID, "workspaceId": definition.WorkspaceID,
				"requestId": requestID, "requestHash": requestHash,
				"kind": databaseAutomationEventJobKind, "pageId": definition.PageID,
				"notBefore": dueMillis, "now": dueMillis, "payload": string(payload),
				"maxAttempts": defaultDurableJobMaxAttempts,
			}).Execute(); err != nil {
				return err
			}
			result.Admitted++
		}
		return txApp.DB().NewQuery(`
			SELECT EXISTS (
				SELECT 1 FROM {{leafen_database_automation_definitions}} [[definition]]
				WHERE [[trigger_type]] = 'schedule' AND [[status]] = 'active'
					AND [[next_run_at]] > 0 AND [[next_run_at]] <= {:dueAt}
					AND ` + databaseSourceNotTrashedSQL("[[definition]].[[database_id]]") + `
					AND ` + blockMovePageUnfencedSQL(`[[definition]].[[page_id]]`) + `
				LIMIT 1
			)
		`).Bind(dbx.Params{"dueAt": dueMillis}).Row(&result.HasMore)
	})
	if err != nil {
		return databaseAutomationScheduleAdmissionResult{}, err
	}
	return result, nil
}
