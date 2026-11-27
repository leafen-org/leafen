package organization

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	leafen "leafen.org/leafen/internal/app"

	scimErrors "github.com/elimity-com/scim/errors"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedSCIMOperation struct {
	ID               string `db:"id"`
	OrganizationID   string `db:"organization_id"`
	Kind             string `db:"kind"`
	ResourceID       string `db:"resource_id"`
	PayloadHash      string `db:"payload_hash"`
	PayloadJSON      string `db:"payload_json"`
	TargetGeneration int    `db:"target_generation"`
	Cursor           int    `db:"cursor"`
	Total            int    `db:"total"`
	Phase            string `db:"phase"`
	RecoveryPhase    string `db:"recovery_phase"`
	NextAttemptAt    int64  `db:"next_attempt_at"`
	Status           string `db:"status"`
	TerminalAt       int64  `db:"terminal_at"`
	CreatedAt        int64  `db:"created_at"`
	UpdatedAt        int64  `db:"updated_at"`
}

type scimPendingOperationConflict struct {
	Operation storedSCIMOperation
}

func (conflict scimPendingOperationConflict) Error() string {
	return leafen.ErrorCodeSCIMOperationConflict
}

type scimOperationTurn struct {
	Cursor      int
	HasMore     bool
	OperationID string
	Status      string
	Total       int
}

func beginEnterpriseSCIMGroupOperation(
	app core.App,
	organizationID string,
	groupID string,
	payload leafen.SCIMGroupMutationPayload,
	create bool,
) (string, error) {
	payload.MemberIDs = leafen.UniqueSortedSCIMMemberIDs(payload.MemberIDs)
	if err := validateSCIMGroupMembers(app, organizationID, payload.MemberIDs); err != nil {
		return "", err
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	metadataBytes, err := json.Marshal(leafen.SCIMOperationMetadata(payload))
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payloadBytes)
	payloadHash := hex.EncodeToString(digest[:])
	operationID := ""
	now := time.Now().UTC().UnixMilli()
	err = app.RunInTransaction(func(txApp core.App) error {
		existing, findErr := findPendingSCIMGroupOperation(txApp, organizationID, groupID)
		if findErr == nil {
			if existing.PayloadHash != payloadHash {
				return scimPendingOperationConflict{Operation: existing}
			}
			operationID = existing.ID
			return nil
		}
		if !errors.Is(findErr, sql.ErrNoRows) {
			return findErr
		}
		activeGeneration, currentPayloadHash, err := prepareSCIMGroupMutation(
			txApp, organizationID, groupID, payload, create, now,
		)
		if err != nil {
			return err
		}
		if currentPayloadHash == payloadHash {
			operationID, err = findCompletedSCIMGroupOperation(
				txApp, organizationID, groupID, payloadHash,
			)
			return err
		}
		operationID, err = newSCIMRecordID("lso_")
		if err != nil {
			return err
		}
		_, err = txApp.DB().Insert(leafen.SCIMOperationsTable, dbx.Params{
			"id": operationID, "organization_id": organizationID, "kind": "group_replace",
			"resource_id": groupID, "payload_hash": payloadHash, "payload_json": string(metadataBytes),
			"target_generation": activeGeneration + 1, "cursor": 0, "total": len(payload.MemberIDs),
			"phase": leafen.SCIMOperationPhaseApply, "status": leafen.SCIMOperationStatusPending,
			"terminal_at": 0, "created_at": now, "updated_at": now,
		}).Execute()
		if err != nil {
			return err
		}
		return leafen.InsertSCIMOperationMemberRange(txApp, operationID, payload.MemberIDs, 0, now)
	})
	return operationID, err
}

func findPendingSCIMGroupOperation(
	app core.App,
	organizationID string,
	groupID string,
) (storedSCIMOperation, error) {
	row := storedSCIMOperation{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[organization_id]], [[kind]], [[resource_id]], [[payload_hash]],
			[[payload_json]], [[target_generation]], [[cursor]], [[total]], [[phase]],
			[[recovery_phase]], [[next_attempt_at]], [[status]],
			[[terminal_at]], [[created_at]], [[updated_at]]
		FROM {{leafen_scim_operations}}
		WHERE [[organization_id]] = {:organizationId} AND [[kind]] = 'group_replace'
			AND [[resource_id]] = {:resourceId} AND [[status]] = 'pending'
		ORDER BY [[created_at]], [[id]] LIMIT 1
	`).Bind(dbx.Params{"organizationId": organizationID, "resourceId": groupID}).One(&row)
	return row, err
}

func prepareSCIMGroupMutation(
	app core.App,
	organizationID string,
	groupID string,
	payload leafen.SCIMGroupMutationPayload,
	create bool,
	now int64,
) (int, string, error) {
	if create {
		_, err := app.DB().Insert(leafen.SCIMGroupsTable, dbx.Params{
			"id": groupID, "organization_id": organizationID,
			"display_name": payload.DisplayName, "normalized_name": normalizeSCIMGroupName(payload.DisplayName),
			"external_id": payload.ExternalID, "active_generation": 0, "current_payload_hash": "",
			"deleted_at": 0, "created_at": now, "updated_at": now,
		}).Execute()
		if err != nil {
			return 0, "", err
		}
		err = leafen.SyncSCIMGroupListAccessInTransaction(
			context.Background(), app, organizationID, groupID,
			payload.ExternalID, payload.ExternalID, false,
		)
		return 0, "", err
	}
	var activeGeneration int
	var currentPayloadHash string
	err := app.DB().NewQuery(`
		SELECT [[active_generation]], [[current_payload_hash]] FROM {{leafen_scim_groups}}
		WHERE [[id]] = {:id} AND [[organization_id]] = {:organizationId} AND [[deleted_at]] = 0
	`).Bind(dbx.Params{"id": groupID, "organizationId": organizationID}).Row(
		&activeGeneration, &currentPayloadHash,
	)
	return activeGeneration, currentPayloadHash, err
}

func findCompletedSCIMGroupOperation(
	app core.App,
	organizationID string,
	groupID string,
	payloadHash string,
) (string, error) {
	row := storedSCIMOperation{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[organization_id]], [[kind]], [[resource_id]], [[payload_hash]],
			[[payload_json]], [[target_generation]], [[cursor]], [[total]], [[phase]],
			[[recovery_phase]], [[next_attempt_at]], [[status]],
			[[terminal_at]], [[created_at]], [[updated_at]]
		FROM {{leafen_scim_operations}}
		WHERE [[organization_id]] = {:organizationId} AND [[kind]] = 'group_replace'
			AND [[resource_id]] = {:resourceId} AND [[payload_hash]] = {:payloadHash}
			AND [[status]] = 'completed'
		ORDER BY [[terminal_at]] DESC, [[id]] DESC LIMIT 1
	`).Bind(dbx.Params{
		"organizationId": organizationID, "resourceId": groupID, "payloadHash": payloadHash,
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return row.ID, err
}

func loadSCIMOperation(app core.App, operationID string) (storedSCIMOperation, error) {
	return loadSCIMOperationContext(context.Background(), app, operationID)
}

func loadSCIMOperationContext(
	ctx context.Context,
	app core.App,
	operationID string,
) (storedSCIMOperation, error) {
	row := storedSCIMOperation{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[organization_id]], [[kind]], [[resource_id]], [[payload_hash]],
			[[payload_json]], [[target_generation]], [[cursor]], [[total]], [[phase]],
			[[recovery_phase]], [[next_attempt_at]], [[status]],
			[[terminal_at]], [[created_at]], [[updated_at]]
		FROM {{leafen_scim_operations}} WHERE [[id]] = {:id}
	`).WithContext(ctx).Bind(dbx.Params{"id": operationID}).One(&row)
	return row, err
}

func validateSCIMGroupMembers(app core.App, organizationID string, memberIDs []string) error {
	for start := 0; start < len(memberIDs); start += leafen.SCIMOperationBatchSize {
		end := min(start+leafen.SCIMOperationBatchSize, len(memberIDs))
		params := dbx.Params{"organizationId": organizationID}
		placeholders := make([]string, 0, end-start)
		for index, id := range memberIDs[start:end] {
			key := fmt.Sprintf("member%d", index)
			params[key] = id
			placeholders = append(placeholders, "{:"+key+"}")
		}
		var count int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_organization_memberships}}
			WHERE [[organization]] = {:organizationId} AND [[scim_managed]] = 1
				AND [[id]] IN (` + leafen.JoinSQL(placeholders) + `)
		`).Bind(params).Row(&count); err != nil {
			return err
		}
		if count != end-start {
			return scimRefusal(scimErrors.ScimErrorInvalidValue, "Each member must be a user this connection manages.")
		}
	}
	return nil
}
