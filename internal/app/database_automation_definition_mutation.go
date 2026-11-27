package app

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// databaseAutomationSentFields is whether an automation change sent its name:
// one left out, or sent null, keeps the name the automation has, as a page
// metadata change keeps a title it leaves out (page_metadata_sent_fields.go);
// the rest of the definition is sent whole. The screen sends the name, ""
// for an unnamed one.
type databaseAutomationSentFields struct {
	Name *string `json:"name"`
}

func updateDatabaseAutomationDefinition(
	app core.App, actorID, pageID, databaseID, automationID string, input databaseAutomationDefinitionInput,
	sent databaseAutomationSentFields,
) (databaseAutomationMutationResponse, error) {
	input.MutationID = strings.TrimSpace(input.MutationID)
	if !boundedAutomationString(input.MutationID, databaseAutomationMaxIdentifierRunes) {
		return databaseAutomationMutationResponse{}, errDatabaseAutomationMutationIDRefused
	}
	if input.ExpectedRevision < 0 {
		return databaseAutomationMutationResponse{}, databaseAutomationRefused("expectedRevision is the automation's revision as it was read, 0 or more")
	}
	// One that sent its name hashes as it always did.
	requestHash, err := databaseAutomationRequestHash("update", pageID, databaseID, automationID, struct {
		databaseAutomationDefinitionInput
		NameLeftOut bool `json:"nameLeftOut,omitzero"`
	}{input, sent.Name == nil})
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
		row, err := loadDatabaseAutomationDefinition(txApp, authorization.Workspace.OwnerID, pageID, databaseID, automationID)
		if err != nil {
			return err
		}
		if row.Status == databaseAutomationStatusDeleted {
			return errDatabaseAutomationDeleted
		}
		if !databaseAutomationRevisionMatches(row.Revision, input.ExpectedRevision) {
			return errDatabaseAutomationConflict
		}
		if sent.Name == nil {
			input.Name = row.Name
		}
		normalized, err := normalizeDatabaseAutomationDefinition(txApp, authorization, databaseID, input)
		if err != nil {
			return err
		}
		activeDelta := 0
		if row.Status == databaseAutomationStatusActive {
			if row.TriggerType == databaseAutomationTriggerEvents && normalized.TriggerType != databaseAutomationTriggerEvents {
				activeDelta = -1
			} else if row.TriggerType != databaseAutomationTriggerEvents && normalized.TriggerType == databaseAutomationTriggerEvents {
				activeDelta = 1
			}
		}
		frontier, err := advanceDatabaseAutomationFrontier(txApp, authorization, databaseID, activeDelta)
		if err != nil {
			return err
		}
		row.Name, row.ScopeType, row.ViewID = normalized.Name, normalized.Scope.Type, normalized.Scope.ViewID
		row.TriggerType = normalized.TriggerType
		row.TriggerJSON, row.ActionJSON = normalized.TriggerJSON, normalized.ActionJSON
		row.NextRunAt = normalized.NextRunAt
		// One that has run as many times as it ends after stays done through an edit.
		if row.NextRunAt > 0 {
			trigger := databaseAutomationTrigger{}
			if json.Unmarshal([]byte(row.TriggerJSON), &trigger) != nil {
				return errDatabaseAutomationUnavailable
			}
			left, err := databaseAutomationScheduleRunsLeft(txApp, row.ID, trigger, 0)
			if err != nil {
				return err
			}
			if !left {
				row.NextRunAt = 0
			}
		}
		row.Revision++
		row.UpdatedFrontier = frontier
		row.UpdatedAt = time.Now().UTC().UnixMilli()
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_database_automation_definitions}}
			SET [[name]] = {:name}, [[scope_type]] = {:scopeType}, [[view_id]] = {:viewId},
				[[trigger_type]] = {:triggerType}, [[trigger_json]] = {:trigger},
				[[action_json]] = {:action}, [[next_run_at]] = {:nextRunAt}, [[revision]] = {:revision},
				[[updated_frontier]] = {:frontier}, [[updated_by]] = {:actorId}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[revision]] = {:expectedRevision}
		`).Bind(dbx.Params{
			"name": row.Name, "scopeType": row.ScopeType, "viewId": row.ViewID,
			"triggerType": row.TriggerType, "trigger": row.TriggerJSON, "action": row.ActionJSON,
			"nextRunAt": row.NextRunAt, "revision": row.Revision,
			"frontier": frontier, "actorId": actorID, "updated": row.UpdatedAt,
			"id": row.ID, "expectedRevision": input.ExpectedRevision,
		}).Execute()
		if err != nil {
			return err
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return errDatabaseAutomationConflict
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

func setDatabaseAutomationDefinitionState(
	app core.App,
	actorID, pageID, databaseID, automationID, operation string,
	input databaseAutomationMutationInput,
) (databaseAutomationMutationResponse, error) {
	input.MutationID = strings.TrimSpace(input.MutationID)
	if !boundedAutomationString(input.MutationID, databaseAutomationMaxIdentifierRunes) {
		return databaseAutomationMutationResponse{}, errDatabaseAutomationMutationIDRefused
	}
	if input.ExpectedRevision < 0 {
		return databaseAutomationMutationResponse{}, databaseAutomationRefused("expectedRevision is the automation's revision as it was read, 0 or more")
	}
	requestHash, err := databaseAutomationRequestHash(operation, pageID, databaseID, automationID, input)
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
		row, err := loadDatabaseAutomationDefinition(txApp, authorization.Workspace.OwnerID, pageID, databaseID, automationID)
		if err != nil {
			return err
		}
		if row.Status == databaseAutomationStatusDeleted {
			return errDatabaseAutomationDeleted
		}
		if !databaseAutomationRevisionMatches(row.Revision, input.ExpectedRevision) {
			return errDatabaseAutomationConflict
		}
		nextStatus, activeDelta := row.Status, 0
		switch operation {
		case "enabled":
			if input.Enabled {
				if row.Status == databaseAutomationStatusPaused {
					return refuse(errDatabaseAutomationConflict, "the automation is paused: resume it to turn it on")
				}
				nextStatus = databaseAutomationStatusActive
			} else {
				nextStatus = databaseAutomationStatusDisabled
			}
		case "resume":
			if row.Status != databaseAutomationStatusPaused {
				return refuse(errDatabaseAutomationConflict, "the automation isn't paused")
			}
			nextStatus = databaseAutomationStatusActive
		case "delete":
			nextStatus = databaseAutomationStatusDeleted
		default:
			return databaseAutomationRefused("the change is enabled, resume or delete, not %q", operation)
		}
		if row.TriggerType == databaseAutomationTriggerEvents {
			if row.Status == databaseAutomationStatusActive && nextStatus != databaseAutomationStatusActive {
				activeDelta = -1
			} else if row.Status != databaseAutomationStatusActive && nextStatus == databaseAutomationStatusActive {
				activeDelta = 1
			}
		}
		nextRunAt := row.NextRunAt
		if nextStatus != databaseAutomationStatusActive || row.TriggerType != databaseAutomationTriggerSchedule {
			nextRunAt = 0
		} else if row.Status != databaseAutomationStatusActive {
			trigger := databaseAutomationTrigger{}
			if json.Unmarshal([]byte(row.TriggerJSON), &trigger) != nil {
				return errDatabaseAutomationUnavailable
			}
			next, exists, err := nextDatabaseAutomationScheduleRun(trigger, time.Now().UTC())
			if err != nil {
				return failedBecause(errDatabaseAutomationUnavailable, err)
			}
			left, err := databaseAutomationScheduleRunsLeft(txApp, row.ID, trigger, 0)
			if err != nil {
				return err
			}
			nextRunAt = 0
			if exists && left {
				nextRunAt = next.UnixMilli()
			}
		}
		frontier, err := advanceDatabaseAutomationFrontier(txApp, authorization, databaseID, activeDelta)
		if err != nil {
			return err
		}
		row.Status, row.PausedReason, row.NextRunAt = nextStatus, "", nextRunAt
		row.Revision++
		row.UpdatedFrontier = frontier
		row.UpdatedAt = time.Now().UTC().UnixMilli()
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_database_automation_definitions}}
			SET [[status]] = {:status}, [[revision]] = {:revision}, [[updated_frontier]] = {:frontier},
				[[next_run_at]] = {:nextRunAt}, [[paused_reason]] = '',
				[[updated_by]] = {:actorId}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[revision]] = {:expectedRevision}
		`).Bind(dbx.Params{
			"status": row.Status, "revision": row.Revision, "frontier": frontier,
			"nextRunAt": row.NextRunAt,
			"actorId":   actorID, "updated": row.UpdatedAt, "id": row.ID,
			"expectedRevision": input.ExpectedRevision,
		}).Execute()
		if err != nil {
			return err
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return errDatabaseAutomationConflict
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
