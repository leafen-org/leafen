package organization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedSCIMOperationMember struct {
	Position             int    `db:"position"`
	OrganizationMemberID string `db:"organization_member_id"`
}

type storedSCIMOldGenerationMember struct {
	Generation           int    `db:"generation"`
	OrganizationMemberID string `db:"organization_member_id"`
}

// continueEnterpriseSCIMOperation advances exactly one idempotent/resumable
// group-membership turn. Apply, activation, and old-generation cleanup each
// commit their own bounded durable frontier before yielding.
func continueEnterpriseSCIMOperation(app core.App, operationID string) (scimOperationTurn, error) {
	return continueEnterpriseSCIMOperationContext(context.Background(), app, operationID)
}

func continueEnterpriseSCIMOperationContext(
	ctx context.Context,
	app core.App,
	operationID string,
) (scimOperationTurn, error) {
	if err := ctx.Err(); err != nil {
		return scimOperationTurn{}, err
	}
	if operationID == "" {
		return scimOperationTurn{Status: leafen.SCIMOperationStatusCompleted}, nil
	}
	operation, err := loadSCIMOperationContext(ctx, app, operationID)
	if err != nil {
		return scimOperationTurn{}, err
	}
	if operation.Status != leafen.SCIMOperationStatusPending || operation.TerminalAt != 0 {
		return scimOperationTurnFromStored(operation), nil
	}
	if operation.RecoveryPhase == leafen.SCIMOperationRecoveryDiscard {
		err = continueSCIMOperationDiscard(ctx, app, operation)
	} else if operation.Phase == leafen.SCIMOperationPhaseCleanup {
		err = continueSCIMOperationCleanup(ctx, app, operation)
	} else {
		err = continueSCIMOperationApply(ctx, app, operation)
	}
	if err != nil {
		var invalidState *scimOperationStateInvalidError
		if !errors.As(err, &invalidState) {
			return scimOperationTurn{}, err
		}
		transitioned, transitionErr := beginSCIMOperationDiscard(ctx, app, operation.ID)
		if transitionErr != nil {
			return scimOperationTurn{}, transitionErr
		}
		if transitioned {
			leafen.LogDurableJobDiagnostic(
				app, leafen.ErrorCodeSCIMOperationStateInvalid, leafen.NewDiagnosticReference(), operation.ID,
				"scim.group.continue", "operation-state", nil,
			)
		}
	}
	operation, err = loadSCIMOperationContext(ctx, app, operationID)
	if err != nil {
		return scimOperationTurn{}, err
	}
	return scimOperationTurnFromStored(operation), nil
}

func continueSCIMOperationApply(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
) error {
	return runSCIMOperationTransaction(ctx, app, func(txApp core.App) error {
		current, err := loadSCIMOperationContext(ctx, txApp, operation.ID)
		if err != nil {
			return err
		}
		if current.Status != leafen.SCIMOperationStatusPending ||
			current.Phase != leafen.SCIMOperationPhaseApply || current.Cursor != operation.Cursor {
			return nil
		}
		members, err := loadSCIMOperationMemberTurn(ctx, txApp, current)
		if err != nil {
			return err
		}
		if err := validateSCIMOperationMemberTurn(current, members); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if err := insertSCIMGenerationMembers(ctx, txApp, current, members, now); err != nil {
			return err
		}
		if err := deleteAppliedSCIMOperationMembers(ctx, txApp, current, len(members)); err != nil {
			return err
		}
		cursor := current.Cursor + len(members)
		if cursor < current.Total {
			_, err = txApp.DB().NewQuery(`
				UPDATE {{leafen_scim_operations}}
				SET [[cursor]] = {:cursor}, [[next_attempt_at]] = 0, [[updated_at]] = {:now}
				WHERE [[id]] = {:id} AND [[status]] = 'pending' AND [[phase]] = 'apply'
					AND [[cursor]] = {:previous}
			`).Bind(dbx.Params{
				"cursor": cursor, "now": now, "id": current.ID, "previous": current.Cursor,
			}).WithContext(ctx).Execute()
			return err
		}
		return activateSCIMOperationGeneration(ctx, txApp, current, cursor, now)
	})
}

func loadSCIMOperationMemberTurn(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
) ([]storedSCIMOperationMember, error) {
	rows := []storedSCIMOperationMember{}
	err := app.DB().NewQuery(`
		SELECT [[position]], [[organization_member_id]]
		FROM {{leafen_scim_operation_members}}
		WHERE [[operation_id]] = {:operationId} AND [[position]] >= {:cursor}
		ORDER BY [[position]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"operationId": operation.ID, "cursor": operation.Cursor, "limit": leafen.SCIMOperationBatchSize,
	}).WithContext(ctx).All(&rows)
	return rows, err
}

func validateSCIMOperationMemberTurn(
	operation storedSCIMOperation,
	members []storedSCIMOperationMember,
) error {
	remaining := operation.Total - operation.Cursor
	if remaining < 0 || len(members) != min(remaining, leafen.SCIMOperationBatchSize) {
		return &scimOperationStateInvalidError{}
	}
	for offset, member := range members {
		if member.Position != operation.Cursor+offset {
			return &scimOperationStateInvalidError{}
		}
	}
	return nil
}

func insertSCIMGenerationMembers(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
	members []storedSCIMOperationMember,
	now int64,
) error {
	if len(members) == 0 {
		return nil
	}
	params := dbx.Params{
		"groupId": operation.ResourceID, "generation": operation.TargetGeneration, "now": now,
	}
	values := make([]string, 0, len(members))
	for index, member := range members {
		key := fmt.Sprintf("member%d", index)
		params[key] = member.OrganizationMemberID
		values = append(values, "({:groupId}, {:generation}, {:"+key+"}, {:now})")
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_scim_group_members}}
			([[group_id]], [[generation]], [[organization_member_id]], [[created_at]])
		VALUES ` + strings.Join(values, ", ") + `
		ON CONFLICT ([[group_id]], [[generation]], [[organization_member_id]]) DO NOTHING
	`).WithContext(ctx).Bind(params).Execute()
	return err
}

func deleteAppliedSCIMOperationMembers(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
	count int,
) error {
	if count == 0 {
		return nil
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_scim_operation_members}}
		WHERE [[operation_id]] = {:operationId} AND [[position]] >= {:start}
			AND [[position]] < {:end}
	`).Bind(dbx.Params{
		"operationId": operation.ID, "start": operation.Cursor, "end": operation.Cursor + count,
	}).WithContext(ctx).Execute()
	return err
}

func activateSCIMOperationGeneration(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
	cursor int,
	now int64,
) error {
	payload := leafen.SCIMGroupOperationMetadata{}
	if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
		return err
	}
	var priorExternalID string
	if err := app.DB().NewQuery(`
		SELECT [[external_id]] FROM {{leafen_scim_groups}}
		WHERE [[id]] = {:groupId} AND [[organization_id]] = {:organizationId}
	`).Bind(dbx.Params{
		"groupId": operation.ResourceID, "organizationId": operation.OrganizationID,
	}).WithContext(ctx).Row(&priorExternalID); err != nil {
		return err
	}
	deletedAt := int64(0)
	if payload.Deleted {
		deletedAt = now
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_scim_groups}}
		SET [[display_name]] = {:displayName}, [[normalized_name]] = {:normalizedName},
			[[external_id]] = {:externalId}, [[active_generation]] = {:generation},
			[[current_payload_hash]] = {:payloadHash},
			[[deleted_at]] = {:deletedAt}, [[updated_at]] = {:now}
		WHERE [[id]] = {:groupId} AND [[organization_id]] = {:organizationId}
	`).Bind(dbx.Params{
		"displayName": payload.DisplayName, "normalizedName": normalizeSCIMGroupName(payload.DisplayName),
		"externalId": payload.ExternalID, "generation": operation.TargetGeneration,
		"payloadHash": operation.PayloadHash, "deletedAt": deletedAt, "now": now,
		"groupId": operation.ResourceID, "organizationId": operation.OrganizationID,
	}).WithContext(ctx).Execute(); err != nil {
		return err
	}
	if err := leafen.SyncSCIMGroupListAccessInTransaction(
		ctx, app, operation.OrganizationID, operation.ResourceID,
		priorExternalID, payload.ExternalID, payload.Deleted,
	); err != nil {
		return err
	}
	if err := leafen.BumpOrganizationPolicyContext(ctx, app, operation.OrganizationID, now); err != nil {
		return err
	}
	// The line names the group by the name it took; a deleted group's line
	// keeps it (organizationAuditTargetNameSQL).
	if err := leafen.RecordOrganizationAuditEventContext(ctx, app, leafen.OrganizationAuditEvent{
		OrganizationID: operation.OrganizationID, ActorID: "scim", Action: "organization.scim.group.updated",
		TargetType: "scim_group", TargetID: operation.ResourceID, OccurredAt: now,
		Metadata: map[string]any{"name": payload.DisplayName, "deleted": payload.Deleted},
	}); err != nil {
		return err
	}
	status, terminalAt, phase, err := nextSCIMOperationCleanupState(ctx, app, operation, now)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_scim_operations}}
		SET [[cursor]] = {:cursor}, [[phase]] = {:phase}, [[status]] = {:status},
			[[terminal_at]] = {:terminalAt}, [[next_attempt_at]] = 0, [[updated_at]] = {:now}
		WHERE [[id]] = {:id} AND [[status]] = 'pending' AND [[phase]] = 'apply'
	`).Bind(dbx.Params{
		"cursor": cursor, "phase": phase, "status": status,
		"terminalAt": terminalAt, "now": now, "id": operation.ID,
	}).WithContext(ctx).Execute()
	return err
}

func nextSCIMOperationCleanupState(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
	now int64,
) (string, int64, string, error) {
	hasOld, err := hasSCIMOldGenerationMembers(ctx, app, operation)
	if err != nil {
		return "", 0, "", err
	}
	if hasOld {
		return leafen.SCIMOperationStatusPending, 0, leafen.SCIMOperationPhaseCleanup, nil
	}
	return leafen.SCIMOperationStatusCompleted, now, leafen.SCIMOperationPhaseCleanup, nil
}

func continueSCIMOperationCleanup(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
) error {
	return runSCIMOperationTransaction(ctx, app, func(txApp core.App) error {
		current, err := loadSCIMOperationContext(ctx, txApp, operation.ID)
		if err != nil {
			return err
		}
		if current.Status != leafen.SCIMOperationStatusPending || current.Phase != leafen.SCIMOperationPhaseCleanup {
			return nil
		}
		rows, err := loadSCIMOldGenerationTurn(ctx, txApp, current)
		if err != nil {
			return err
		}
		if err := deleteSCIMOldGenerationTurn(ctx, txApp, current, rows); err != nil {
			return err
		}
		hasMore, err := hasSCIMOldGenerationMembers(ctx, txApp, current)
		if err != nil || hasMore {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_scim_operations}}
			SET [[status]] = 'completed', [[terminal_at]] = {:now},
				[[next_attempt_at]] = 0, [[updated_at]] = {:now}
			WHERE [[id]] = {:id} AND [[status]] = 'pending' AND [[phase]] = 'cleanup'
		`).WithContext(ctx).Bind(dbx.Params{"id": current.ID, "now": now}).Execute()
		return err
	})
}

func loadSCIMOldGenerationTurn(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
) ([]storedSCIMOldGenerationMember, error) {
	rows := []storedSCIMOldGenerationMember{}
	err := app.DB().NewQuery(`
		SELECT [[generation]], [[organization_member_id]]
		FROM {{leafen_scim_group_members}}
		WHERE [[group_id]] = {:groupId} AND [[generation]] != {:generation}
		ORDER BY [[generation]], [[organization_member_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"groupId":    operation.ResourceID,
		"generation": operation.TargetGeneration,
		"limit":      leafen.SCIMOperationBatchSize,
	}).WithContext(ctx).All(&rows)
	return rows, err
}

func deleteSCIMOldGenerationTurn(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
	rows []storedSCIMOldGenerationMember,
) error {
	if len(rows) == 0 {
		return nil
	}
	params := dbx.Params{"groupId": operation.ResourceID}
	keys := make([]string, 0, len(rows))
	for index, row := range rows {
		generationKey := fmt.Sprintf("generation%d", index)
		memberKey := fmt.Sprintf("member%d", index)
		params[generationKey] = row.Generation
		params[memberKey] = row.OrganizationMemberID
		keys = append(keys, "({:"+generationKey+"}, {:"+memberKey+"})")
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_scim_group_members}}
		WHERE [[group_id]] = {:groupId}
			AND ([[generation]], [[organization_member_id]]) IN (` + strings.Join(keys, ", ") + `)
	`).WithContext(ctx).Bind(params).Execute()
	return err
}

func hasSCIMOldGenerationMembers(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
) (bool, error) {
	var found int
	err := app.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{leafen_scim_group_members}}
			WHERE [[group_id]] = {:groupId} AND [[generation]] != {:generation}
			LIMIT 1
		)
	`).Bind(dbx.Params{
		"groupId": operation.ResourceID, "generation": operation.TargetGeneration,
	}).WithContext(ctx).Row(&found)
	return found == 1, err
}

func runSCIMOperationTransaction(
	ctx context.Context,
	app core.App,
	operation func(core.App) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return app.RunInTransaction(operation)
}

func scimOperationTurnFromStored(operation storedSCIMOperation) scimOperationTurn {
	return scimOperationTurn{
		Cursor: operation.Cursor, HasMore: operation.Status == leafen.SCIMOperationStatusPending,
		OperationID: operation.ID, Status: operation.Status, Total: operation.Total,
	}
}
