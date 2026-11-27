package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A "페이지 추가" action naming a template adds its row, then fills it from
// the template (Notion's, seen 2026-11-04): at once for a button, which says
// what it made; as a job for a database automation, retried as other jobs are.
const interactiveAutomationTemplateRowJobKind = "automation-template-row"

type interactiveAutomationTemplateRow struct {
	RowID      string `json:"rowId"`
	TemplateID string `json:"templateId"`
}

type interactiveAutomationTemplateRowJobPayload struct {
	// SystemAuthored is a database automation's row, which reads as Leafen's.
	SystemAuthored bool                             `json:"systemAuthored,omitzero"`
	PrincipalID    string                           `json:"principalId"`
	Row            interactiveAutomationTemplateRow `json:"row"`
}

func queueInteractiveAutomationTemplateRows(app core.App, authorization pageAuthorization, plan interactiveAutomationPlan) error {
	now := time.Now().UTC()
	for _, row := range plan.TemplateRows {
		payload, err := json.Marshal(interactiveAutomationTemplateRowJobPayload{PrincipalID: authorization.Workspace.PrincipalID, Row: row, SystemAuthored: plan.SystemAuthored})
		if err != nil {
			return err
		}
		id := automationStableID("hat", row.RowID, row.TemplateID)
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
				 [[status]], [[error_code]], [[not_before]], [[created]], [[updated]], [[payload_json]], [[cursor]],
				 [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]], [[chunk_count]])
			VALUES ({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind}, {:pageId},
				'queued', '', {:notBefore}, {:now}, {:now}, {:payload}, '', 0, 1, 0, {:maxAttempts}, 0)
			ON CONFLICT ([[owner]], [[request_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": id, "owner": authorization.Workspace.OwnerID, "workspaceId": authorization.Workspace.WorkspaceID,
			"requestId": "automation-template-row:" + id, "requestHash": automationDigest(id), "kind": interactiveAutomationTemplateRowJobKind,
			"pageId": authorization.Page.Id, "notBefore": now.Add(durableJobReadyDelay).UnixMilli(),
			"now": now.UnixMilli(), "payload": string(payload), "maxAttempts": defaultDurableJobMaxAttempts,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func (r *durableJobRunner) applyInteractiveAutomationTemplateRowJob(ctx context.Context, job storedJob) error {
	if r.ygo == nil {
		return errInteractiveAutomationUnavailable
	}
	payload := interactiveAutomationTemplateRowJobPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return err
	}
	if err := r.ygo.fillDatabaseRowFromTemplate(ctx, r.app, payload.PrincipalID, payload.Row.RowID, payload.Row.TemplateID, "", false); err != nil {
		return err
	}
	if payload.SystemAuthored {
		if err := markLeafenSystemAuthored(r.app, payload.Row.RowID); err != nil {
			return err
		}
	}
	// Done: a job left queued would fill the row again, and again.
	_, err := r.app.DB().NewQuery(`UPDATE {{leafen_jobs}}
		SET [[status]] = {:status}, [[processed_items]] = [[total_items]], [[chunk_count]] = [[chunk_count]] + 1,
			[[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'`).Bind(dbx.Params{
		"status": durableJobStatusCompleted, "updated": time.Now().UTC().UnixMilli(), "id": job.ID,
	}).Execute()
	return err
}
