package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An unattended run — a database automation — writes other pages'
// databases after its own page's change as jobs, one for each page, which
// run as the person the automation runs as and are retried as other jobs
// are. A button run writes them at once (executeInteractiveAutomationButton).
const interactiveAutomationOtherPageJobKind = "automation-other-page-write"

type interactiveAutomationOtherPageJobPayload struct {
	PrincipalID string                              `json:"principalId"`
	Write       interactiveAutomationOtherPageWrite `json:"write"`
	// SystemAuthored is a database automation's write, which reads as Leafen's.
	SystemAuthored bool `json:"systemAuthored,omitzero"`
}

func queueInteractiveAutomationOtherPageWrites(
	app core.App, authorization pageAuthorization, plan interactiveAutomationPlan, phase string,
) error {
	inputs, err := interactiveAutomationOtherPageInputs(app, authorization, plan)
	if err != nil || len(inputs) == 0 {
		return err
	}
	now := time.Now().UTC()
	for _, input := range inputs {
		write := interactiveAutomationOtherPageWrite{Phase: phase, PageID: input.PageID, Operations: input.Operations}
		payload, err := json.Marshal(interactiveAutomationOtherPageJobPayload{
			PrincipalID: authorization.Workspace.PrincipalID, Write: write, SystemAuthored: plan.SystemAuthored,
		})
		if err != nil {
			return err
		}
		_, err = app.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
				 [[status]], [[error_code]], [[not_before]], [[created]], [[updated]], [[payload_json]], [[cursor]],
				 [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]], [[chunk_count]])
			VALUES ({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind}, {:pageId},
				'queued', '', {:notBefore}, {:now}, {:now}, {:payload}, '', 0, 1, 0, {:maxAttempts}, 0)
			ON CONFLICT ([[owner]], [[request_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": automationStableID("job", write.mutationID()), "owner": authorization.Workspace.OwnerID,
			"workspaceId": authorization.Workspace.WorkspaceID, "requestId": "automation-other-page:" + write.mutationID(),
			"requestHash": automationDigest(write.mutationID()), "kind": interactiveAutomationOtherPageJobKind,
			"pageId": write.PageID, "notBefore": now.Add(durableJobReadyDelay).UnixMilli(),
			"now": now.UnixMilli(), "payload": string(payload), "maxAttempts": defaultDurableJobMaxAttempts,
		}).Execute()
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *durableJobRunner) applyInteractiveAutomationOtherPageJob(_ context.Context, job storedJob) error {
	if r.ygo == nil {
		return errors.New(errorCodeJobRunnerFailed)
	}
	payload := interactiveAutomationOtherPageJobPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return err
	}
	if payload.Write.PageID != job.PageID {
		return errInteractiveAutomationInvalid
	}
	// Only database automations queue these (a button writes at once), and
	// what they write sets off no automation.
	if err := applyInteractiveAutomationOtherPages(r.ygo, payload.PrincipalID, []interactiveAutomationOtherPageWrite{payload.Write}, true); err != nil {
		return err
	}
	if payload.SystemAuthored {
		for _, operation := range payload.Write.Operations {
			if operation.Row == nil {
				continue
			}
			if err := markLeafenSystemEdited(r.app, operation.Row.ID, payload.PrincipalID); err != nil {
				return err
			}
		}
	}
	_, err := r.app.DB().NewQuery(`UPDATE {{leafen_jobs}}
		SET [[status]] = {:status}, [[processed_items]] = [[total_items]], [[chunk_count]] = [[chunk_count]] + 1,
			[[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'`).Bind(dbx.Params{
		"status": durableJobStatusCompleted, "updated": time.Now().UTC().UnixMilli(), "id": job.ID,
	}).Execute()
	return err
}
