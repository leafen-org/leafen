package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type organizationAuditEvent struct {
	ID             string
	OrganizationID string
	WorkspaceID    string
	ActorID        string
	Action         string
	TargetType     string
	TargetID       string
	Metadata       map[string]any
	OccurredAt     int64
	// KeepsLatestName is a line written again as its group goes on (a name
	// typed in turn: workspaceActivityGroupID), which keeps the name it was
	// last given; any other line written again is left as it was first.
	KeepsLatestName bool
}

func insertOrganizationAuditEvent(app core.App, event organizationAuditEvent) error {
	return insertOrganizationAuditEventContext(context.Background(), app, event)
}

func insertOrganizationAuditEventContext(
	ctx context.Context,
	app core.App,
	event organizationAuditEvent,
) error {
	event.ID = strings.TrimSpace(event.ID)
	event.OrganizationID = strings.TrimSpace(event.OrganizationID)
	event.WorkspaceID = strings.TrimSpace(event.WorkspaceID)
	event.ActorID = strings.TrimSpace(event.ActorID)
	event.Action = strings.TrimSpace(event.Action)
	event.TargetType = strings.TrimSpace(event.TargetType)
	event.TargetID = strings.TrimSpace(event.TargetID)
	if event.OrganizationID == "" || event.ActorID == "" || event.Action == "" ||
		event.TargetType == "" || event.TargetID == "" ||
		len([]rune(event.ID)) > 255 ||
		len([]rune(event.WorkspaceID)) > 255 ||
		len([]rune(event.Action)) > organizationAuditMaxFilterRunes ||
		len([]rune(event.TargetType)) > organizationAuditMaxFilterRunes {
		return errors.New(errorCodeOrganizationAuditInvalid)
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	metadata, err := json.Marshal(event.Metadata)
	if err != nil || len(metadata) > organizationAuditMaxMetadataBytes {
		return errors.New(errorCodeOrganizationAuditInvalid)
	}
	if event.OccurredAt == 0 {
		event.OccurredAt = time.Now().UTC().UnixMilli()
	}
	// Each change is its own event: without an identity of its own an event
	// gets a fresh one, so a repeated change to the same subject keeps its own
	// row even within one millisecond.
	if event.ID == "" {
		event.ID = organizationAuditEventID(
			event.OrganizationID, event.Action, event.TargetID, event.OccurredAt,
		)
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_organization_audit_events}}
			([[id]], [[organization_id]], [[actor_id]], [[action]], [[subject_type]],
			 [[subject_id]], [[created]], [[workspace_id]], [[metadata_json]])
		VALUES ({:id}, {:organizationId}, {:actorId}, {:action}, {:targetType},
			{:targetId}, {:occurredAt}, {:workspaceId}, {:metadata})
		ON CONFLICT ([[id]]) DO ` + organizationAuditEventConflict(event) + `
	`).WithContext(ctx).Bind(dbx.Params{
		"id":             event.ID,
		"organizationId": event.OrganizationID, "workspaceId": event.WorkspaceID,
		"actorId": event.ActorID, "action": event.Action, "targetType": event.TargetType,
		"targetId": event.TargetID, "occurredAt": event.OccurredAt, "metadata": string(metadata),
	}).Execute()
	return err
}

// organizationAuditEventConflict is what writing a line again does: nothing,
// or for a line that keeps its latest name, that name over the one it had.
func organizationAuditEventConflict(event organizationAuditEvent) string {
	if !event.KeepsLatestName {
		return "NOTHING"
	}
	return "UPDATE SET [[metadata_json]] = json_set([[metadata_json]], '$.name', json_extract(excluded.[[metadata_json]], '$.name'))"
}
