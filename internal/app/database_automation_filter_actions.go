package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type databaseAutomationFilterProgress struct {
	DocumentHash string                        `json:"documentHash"`
	Progress     interactiveAutomationProgress `json:"progress"`
	ChangedRows  int                           `json:"changedRows"`
}

type databaseAutomationFilterMutationError struct{ cause error }

func (err *databaseAutomationFilterMutationError) Error() string { return err.cause.Error() }
func (err *databaseAutomationFilterMutationError) Unwrap() error { return err.cause }

func databaseAutomationHasStructuredFilter(document interactiveAutomationDocument) bool {
	for _, action := range document.Actions {
		if interactiveAutomationUsesFilter(action) {
			return true
		}
	}
	return false
}

// Event and schedule candidates share the same query/action phase as a button.
// The event job owns pending progress; its outcome is written only at completion.
func advanceDatabaseAutomationFilteredActions(app core.App, doc *crdt.Doc, state *databaseAutomationEvaluationState,
	auth pageAuthorization, candidate databaseAutomationEvaluationCandidate, source interactiveAutomationSource,
	normalized normalizedInteractiveAutomation, outcomeID string) (bool, bool, string, error) {
	var stored struct {
		ID      string `db:"id"`
		Payload string `db:"payload_json"`
	}
	if err := app.DB().NewQuery(`SELECT [[job]].[[id]],[[job]].[[payload_json]] FROM {{leafen_jobs}} [[job]] JOIN {{leafen_database_automation_events}} [[event]] ON [[event]].[[job_id]]=[[job]].[[id]] WHERE [[event]].[[id]]={:event}`).Bind(dbx.Params{"event": candidate.EventID}).One(&stored); err != nil {
		return false, false, "", err
	}
	payload := map[string]json.RawMessage{}
	if json.Unmarshal([]byte(stored.Payload), &payload) != nil {
		return false, false, "", errDatabaseAutomationUnavailable
	}
	progresses := map[string]databaseAutomationFilterProgress{}
	if raw := payload["filterActionProgress"]; len(raw) > 0 && json.Unmarshal(raw, &progresses) != nil {
		return false, false, "", errDatabaseAutomationUnavailable
	}
	held, found := progresses[outcomeID]
	if found && held.DocumentHash != normalized.DocumentHash {
		return false, false, "", errInteractiveAutomationConflict
	}
	held.DocumentHash = normalized.DocumentHash
	if held.Progress.NextAction < len(normalized.Document.Actions) {
		action := normalized.Document.Actions[held.Progress.NextAction]
		if action.SourceBlockID != "" || action.Type == interactiveAutomationActionShowConfirmation {
			return false, false, "", errInteractiveAutomationUnsupported
		}
	}
	reference := time.UnixMilli(candidate.EventCreated).UTC()
	plan, actions, err := planInteractiveAutomationActionTurn(app, auth, source, normalized.Document, outcomeID, reference, held.Progress)
	if err != nil {
		return false, false, "", err
	}
	scanner, err := state.dlpScanner(app, auth)
	if err != nil {
		return false, false, "", err
	}
	// What a database automation changes reads as Leafen's, here as anywhere it writes.
	plan.SystemAuthored = true
	changed, err := state.applyPlan(app, doc, auth, plan, scanner)
	if err == nil {
		// Each turn of the run is its own phase.
		turn, _ := json.Marshal(held.Progress)
		err = queueInteractiveAutomationOtherPageWrites(app, auth, plan, automationStableID("hfo", outcomeID, string(turn)))
	}
	if err == nil {
		err = queueInteractiveAutomationTemplateRows(app, auth, plan)
	}
	if err != nil {
		return false, false, "", &databaseAutomationFilterMutationError{err}
	}
	deliveries, err := admitDatabaseAutomationDeliveries(app, candidate, actions, outcomeID, reference)
	if err != nil {
		return false, false, "", &databaseAutomationFilterMutationError{err}
	}
	held.Progress = plan.progress
	held.ChangedRows += len(plan.Rows)
	if held.Progress.Done {
		delete(progresses, outcomeID)
	} else {
		progresses[outcomeID] = held
	}
	encoded, err := json.Marshal(progresses)
	if err != nil {
		return false, false, "", &databaseAutomationFilterMutationError{err}
	}
	payload["filterActionProgress"] = encoded
	encoded, err = json.Marshal(payload)
	if err != nil {
		return false, false, "", &databaseAutomationFilterMutationError{err}
	}
	if _, err = app.DB().Update("leafen_jobs", dbx.Params{"payload_json": string(encoded)}, dbx.HashExp{"id": stored.ID}).Execute(); err != nil {
		return false, false, "", &databaseAutomationFilterMutationError{err}
	}
	result, err := json.Marshal(map[string]any{"effects": plan.Effects, "deliveries": deliveries, "changedRows": held.ChangedRows})
	if err != nil {
		return false, false, "", &databaseAutomationFilterMutationError{err}
	}
	return changed, !held.Progress.Done, string(result), nil
}

func pauseDatabaseAutomationFilteredCandidate(app core.App, state *databaseAutomationEvaluationState,
	candidate databaseAutomationEvaluationCandidate, reason string) (bool, error) {
	paused, err := pauseDatabaseAutomationDefinition(app, candidate, reason)
	if err != nil {
		return false, err
	}
	status := databaseAutomationOutcomeStale
	if paused {
		status = databaseAutomationOutcomePaused
		state.inactive[candidate.DefinitionID] = struct{}{}
	}
	return false, insertDatabaseAutomationOutcome(app, candidate, status, errorCodeDatabaseAutomationUnavailable, "{}")
}
