package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

type storedDatabaseAutomationDelivery struct {
	ID                 string `db:"id"`
	OwnerID            string `db:"owner"`
	WorkspaceID        string `db:"workspace_id"`
	PageID             string `db:"page_id"`
	DatabaseID         string `db:"database_id"`
	EventID            string `db:"event_id"`
	DefinitionID       string `db:"definition_id"`
	DefinitionRevision int    `db:"definition_revision"`
	OutcomeID          string `db:"outcome_id"`
	ActionID           string `db:"action_id"`
	Channel            string `db:"channel"`
	PayloadJSON        string `db:"payload_json"`
	State              string `db:"state"`
	Attempts           int    `db:"attempts"`
	MaxAttempts        int    `db:"max_attempts"`
	NextAttemptAt      int64  `db:"next_attempt_at"`
	LeaseToken         string `db:"lease_token"`
	LeaseUntil         int64  `db:"lease_until"`
	TriggerType        string `db:"trigger_type"`
	DefinitionStatus   string `db:"definition_status"`
	CurrentRevision    int    `db:"current_revision"`
}

type databaseAutomationDeliverySettlement struct {
	Delivery storedDatabaseAutomationDelivery
	Result   databaseAutomationDeliveryProviderResult
	RetryAt  int64
	Terminal bool
}

const databaseAutomationDeliveryEffectiveDueSQL = "CASE WHEN [[state]]='claimed' THEN [[lease_until]] ELSE [[next_attempt_at]] END"

func claimDatabaseAutomationDeliveries(
	app core.App, now time.Time,
) ([]storedDatabaseAutomationDelivery, bool, error) {
	claimed := []storedDatabaseAutomationDelivery{}
	hasMore := false
	err := app.RunInTransaction(func(txApp core.App) error {
		candidates := []storedDatabaseAutomationDelivery{}
		err := txApp.DB().NewQuery(`
			SELECT [[delivery]].[[id]], [[delivery]].[[owner]], [[delivery]].[[workspace_id]],
				[[delivery]].[[page_id]], [[delivery]].[[database_id]], [[delivery]].[[event_id]],
				[[delivery]].[[definition_id]], [[delivery]].[[definition_revision]],
				[[delivery]].[[outcome_id]], [[delivery]].[[action_id]], [[delivery]].[[channel]],
				[[delivery]].[[payload_json]], [[delivery]].[[state]], [[delivery]].[[attempts]],
				[[delivery]].[[max_attempts]], [[delivery]].[[next_attempt_at]],
				[[delivery]].[[lease_token]], [[delivery]].[[lease_until]],
				COALESCE([[definition]].[[trigger_type]], '') AS [[trigger_type]],
				COALESCE([[definition]].[[status]], '') AS [[definition_status]],
				COALESCE([[definition]].[[revision]], 0) AS [[current_revision]]
			FROM {{leafen_database_automation_deliveries}} [[delivery]]
				INDEXED BY [[idx_leafen_database_automation_deliveries_ready]]
			LEFT JOIN {{leafen_database_automation_definitions}} [[definition]]
				ON [[definition]].[[id]] = [[delivery]].[[definition_id]]
				AND [[definition]].[[owner]] = [[delivery]].[[owner]]
				AND [[definition]].[[workspace_id]] = [[delivery]].[[workspace_id]]
				AND [[definition]].[[page_id]] = [[delivery]].[[page_id]]
				AND [[definition]].[[database_id]] = [[delivery]].[[database_id]]
			WHERE [[delivery]].[[state]] IN ('pending', 'retrying', 'claimed')
				AND ` + databaseAutomationDeliveryEffectiveDueSQL + ` <= {:now}
			ORDER BY ` + databaseAutomationDeliveryEffectiveDueSQL + `, [[delivery]].[[id]]
			LIMIT {:limit}
		`).Bind(dbx.Params{
			"now": now.UnixMilli(), "limit": databaseAutomationDeliveryClaimChunk + 1,
		}).All(&candidates)
		if err != nil {
			return err
		}
		hasMore = len(candidates) > databaseAutomationDeliveryClaimChunk
		if hasMore {
			candidates = candidates[:databaseAutomationDeliveryClaimChunk]
		}
		for _, candidate := range candidates {
			if !interactiveAutomationButtonDelivery(candidate.DefinitionID) &&
				(candidate.CurrentRevision != candidate.DefinitionRevision ||
					candidate.DefinitionStatus != databaseAutomationStatusActive) {
				result, err := txApp.DB().NewQuery(`
					UPDATE {{leafen_database_automation_deliveries}}
					SET [[state]]='stale', [[lease_token]]='', [[lease_until]]=0, [[updated]]={:updated}
					WHERE [[id]]={:id} AND [[state]]={:state} AND [[lease_token]]={:leaseToken}
				`).Bind(dbx.Params{
					"updated": now.UnixMilli(), "id": candidate.ID,
					"state": candidate.State, "leaseToken": candidate.LeaseToken,
				}).Execute()
				if err != nil {
					return err
				}
				changed, _ := result.RowsAffected()
				if changed == 1 {
					candidate.State = databaseAutomationDeliveryStale
					claimed = append(claimed, candidate)
				}
				continue
			}
			token := security.RandomString(24)
			result, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_database_automation_deliveries}}
				SET [[state]]='claimed', [[attempts]]=[[attempts]]+1,
					[[lease_token]]={:token}, [[lease_until]]={:leaseUntil}, [[updated]]={:updated}
				WHERE [[id]]={:id} AND [[state]]={:state} AND [[lease_token]]={:leaseToken}
			`).Bind(dbx.Params{
				"token":      token,
				"leaseUntil": now.Add(time.Duration(databaseAutomationDeliveryLeaseMilliseconds) * time.Millisecond).UnixMilli(),
				"updated":    now.UnixMilli(), "id": candidate.ID,
				"state": candidate.State, "leaseToken": candidate.LeaseToken,
			}).Execute()
			if err != nil {
				return err
			}
			changed, _ := result.RowsAffected()
			if changed == 1 {
				candidate.State = databaseAutomationDeliveryClaimed
				candidate.Attempts++
				candidate.LeaseToken = token
				claimed = append(claimed, candidate)
			}
		}
		return nil
	})
	return claimed, hasMore, err
}

func settleDatabaseAutomationDeliveries(
	app core.App, now time.Time, settlements []databaseAutomationDeliverySettlement,
) error {
	return app.RunInTransaction(func(txApp core.App) error {
		for _, settlement := range settlements {
			delivery := settlement.Delivery
			state, providerID, errorCode := databaseAutomationDeliverySucceeded, settlement.Result.ProviderID, ""
			deliveredAt, failedAt, retryAt := now.UnixMilli(), int64(0), delivery.NextAttemptAt
			if settlement.Result.State == databaseAutomationDeliveryResultRetryable && !settlement.Terminal {
				state, providerID, errorCode = databaseAutomationDeliveryRetrying, "", settlement.Result.ErrorCode
				deliveredAt, retryAt = 0, settlement.RetryAt
			} else if settlement.Result.State != databaseAutomationDeliveryResultSucceeded {
				state, providerID, errorCode = databaseAutomationDeliveryFailed, "", settlement.Result.ErrorCode
				deliveredAt, failedAt = 0, now.UnixMilli()
			}
			result, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_database_automation_deliveries}}
				SET [[state]]={:state}, [[next_attempt_at]]={:retryAt}, [[lease_token]]='', [[lease_until]]=0,
					[[provider_id]]={:providerId}, [[last_error]]={:errorCode},
					[[delivered_at]]={:deliveredAt}, [[failed_at]]={:failedAt}, [[updated]]={:updated}
				WHERE [[id]]={:id} AND [[state]]='claimed' AND [[lease_token]]={:leaseToken}
			`).Bind(dbx.Params{
				"state": state, "retryAt": retryAt, "providerId": providerID, "errorCode": errorCode,
				"deliveredAt": deliveredAt, "failedAt": failedAt, "updated": now.UnixMilli(),
				"id": delivery.ID, "leaseToken": delivery.LeaseToken,
			}).Execute()
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil || changed == 0 || state != databaseAutomationDeliveryFailed {
				if err != nil {
					return err
				}
				continue
			}
			candidate := databaseAutomationEvaluationCandidate{
				OwnerID: delivery.OwnerID, WorkspaceID: delivery.WorkspaceID,
				PageID: delivery.PageID, DatabaseID: delivery.DatabaseID,
				DefinitionID: delivery.DefinitionID, DefinitionRevision: delivery.DefinitionRevision,
				TriggerType: delivery.TriggerType,
			}
			// A webhook the workspace turned off is fixed in its settings.
			reason := databaseAutomationPausedReasonDelivery
			if errorCode == "webhook_disabled_by_workspace" {
				reason = databaseAutomationPausedReasonWebhooksOff
			}
			if _, err := pauseDatabaseAutomationDefinition(txApp, candidate, reason); err != nil {
				return err
			}
		}
		return nil
	})
}

func databaseAutomationDeliveryRetryAt(now time.Time, attempt int) int64 {
	delay := time.Duration(databaseAutomationDeliveryRetryBaseMilliseconds) * time.Millisecond
	for index := 1; index < attempt; index++ {
		if delay >= time.Duration(databaseAutomationDeliveryRetryMaxMilliseconds)*time.Millisecond/2 {
			delay = time.Duration(databaseAutomationDeliveryRetryMaxMilliseconds) * time.Millisecond
			break
		}
		delay *= 2
	}
	return now.Add(delay).UnixMilli()
}

func nextDatabaseAutomationDeliveryWait(app core.App, now time.Time) (time.Duration, error) {
	var due sql.NullInt64
	err := app.DB().NewQuery(`
		SELECT MIN(` + databaseAutomationDeliveryEffectiveDueSQL + `)
		FROM {{leafen_database_automation_deliveries}}
			INDEXED BY [[idx_leafen_database_automation_deliveries_ready]]
		WHERE [[state]] IN ('pending', 'retrying', 'claimed')
	`).Row(&due)
	if err != nil || !due.Valid {
		return -1, err
	}
	wait := time.UnixMilli(due.Int64).Sub(now)
	if wait < 0 {
		return 0, nil
	}
	return wait, nil
}

func databaseAutomationDeliveryRowsAffected(result sql.Result, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return rows, err
}
