package organization

import (
	"context"
	"fmt"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type scimOperationStateInvalidError struct{}

func (*scimOperationStateInvalidError) Error() string {
	return leafen.ErrorCodeSCIMOperationStateInvalid
}

func beginSCIMOperationDiscard(
	ctx context.Context,
	app core.App,
	operationID string,
) (bool, error) {
	transitioned := false
	err := runSCIMOperationTransaction(ctx, app, func(txApp core.App) error {
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_scim_operations}}
			SET [[recovery_phase]] = 'discard', [[next_attempt_at]] = 0, [[updated_at]] = {:now}
			WHERE [[id]] = {:id} AND [[status]] = 'pending' AND [[recovery_phase]] = ''
		`).WithContext(ctx).Bind(dbx.Params{
			"id": operationID, "now": time.Now().UTC().UnixMilli(),
		}).Execute()
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		transitioned = rows == 1
		return err
	})
	return transitioned, err
}

func continueSCIMOperationDiscard(
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
			current.RecoveryPhase != leafen.SCIMOperationRecoveryDiscard {
			return nil
		}
		positions, err := loadSCIMDiscardStagingTurn(ctx, txApp, current.ID)
		if err != nil {
			return err
		}
		if len(positions) > 0 {
			return deleteSCIMDiscardStagingTurn(ctx, txApp, current.ID, positions)
		}
		activeGeneration, err := loadSCIMActiveGeneration(ctx, txApp, current)
		if err != nil {
			return err
		}
		if current.TargetGeneration != activeGeneration {
			members, err := loadSCIMDiscardGenerationTurn(ctx, txApp, current)
			if err != nil {
				return err
			}
			if len(members) > 0 {
				return deleteSCIMDiscardGenerationTurn(ctx, txApp, current, members)
			}
		}
		now := time.Now().UTC().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_scim_operations}}
			SET [[status]] = 'failed', [[terminal_at]] = {:now},
				[[next_attempt_at]] = 0, [[updated_at]] = {:now}
			WHERE [[id]] = {:id} AND [[status]] = 'pending'
				AND [[recovery_phase]] = 'discard'
		`).WithContext(ctx).Bind(dbx.Params{"id": current.ID, "now": now}).Execute()
		return err
	})
}

func loadSCIMActiveGeneration(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
) (int, error) {
	var generation int
	err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[active_generation]]), -1) FROM {{leafen_scim_groups}}
		WHERE [[id]] = {:groupId} AND [[organization_id]] = {:organizationId}
	`).WithContext(ctx).Bind(dbx.Params{
		"groupId": operation.ResourceID, "organizationId": operation.OrganizationID,
	}).Row(&generation)
	return generation, err
}

func loadSCIMDiscardStagingTurn(
	ctx context.Context,
	app core.App,
	operationID string,
) ([]int, error) {
	positions := []int{}
	err := app.DB().NewQuery(`
		SELECT [[position]] FROM {{leafen_scim_operation_members}}
		WHERE [[operation_id]] = {:operationId}
		ORDER BY [[position]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"operationId": operationID, "limit": leafen.SCIMOperationBatchSize,
	}).Column(&positions)
	return positions, err
}

func deleteSCIMDiscardStagingTurn(
	ctx context.Context,
	app core.App,
	operationID string,
	positions []int,
) error {
	params := dbx.Params{"operationId": operationID}
	values := make([]string, 0, len(positions))
	for index, position := range positions {
		key := fmt.Sprintf("position%d", index)
		params[key] = position
		values = append(values, "{:"+key+"}")
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_scim_operation_members}}
		WHERE [[operation_id]] = {:operationId}
			AND [[position]] IN (` + strings.Join(values, ", ") + `)
	`).WithContext(ctx).Bind(params).Execute()
	return err
}

func loadSCIMDiscardGenerationTurn(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
) ([]string, error) {
	members := []string{}
	err := app.DB().NewQuery(`
		SELECT [[organization_member_id]] FROM {{leafen_scim_group_members}}
		WHERE [[group_id]] = {:groupId} AND [[generation]] = {:generation}
		ORDER BY [[organization_member_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"groupId": operation.ResourceID, "generation": operation.TargetGeneration,
		"limit": leafen.SCIMOperationBatchSize,
	}).Column(&members)
	return members, err
}

func deleteSCIMDiscardGenerationTurn(
	ctx context.Context,
	app core.App,
	operation storedSCIMOperation,
	members []string,
) error {
	params := dbx.Params{
		"groupId": operation.ResourceID, "generation": operation.TargetGeneration,
	}
	values := make([]string, 0, len(members))
	for index, member := range members {
		key := fmt.Sprintf("member%d", index)
		params[key] = member
		values = append(values, "{:"+key+"}")
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_scim_group_members}}
		WHERE [[group_id]] = {:groupId} AND [[generation]] = {:generation}
			AND [[organization_member_id]] IN (` + strings.Join(values, ", ") + `)
	`).WithContext(ctx).Bind(params).Execute()
	return err
}
