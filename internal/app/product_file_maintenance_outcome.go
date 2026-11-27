package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	productFileMaintenanceSourceUpload    = "upload"
	productFileMaintenanceSourceReference = "reference"
)

type storedProductFileMaintenanceOutcome struct {
	ID                  string `db:"id"`
	WorkspaceID         string `db:"workspace_id"`
	SourceKind          string `db:"source_kind"`
	SourceID            string `db:"source_id"`
	Attempt             int    `db:"attempt"`
	ScheduledAtNanos    int64  `db:"scheduled_at_nanos"`
	FinishedAt          int64  `db:"finished_at"`
	Expired             int    `db:"expired"`
	DeletedObjects      int    `db:"deleted_objects"`
	FailedObjects       int    `db:"failed_objects"`
	DiagnosticReference string `db:"diagnostic_reference"`
}

type productFileMaintenanceOutcomeSettlement struct {
	Settled int
	HasMore bool
}

func productFileMaintenanceOutcomeID(sourceKind, sourceID string, attempt int) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		productFileMaintenanceRunKind,
		sourceKind,
		sourceID,
		formatInt64(int64(attempt)),
	}, "\x1f")))
	return "fmo_" + hex.EncodeToString(digest[:])
}

func recordProductFileMaintenanceOutcome(
	ctx context.Context,
	app core.App,
	scheduledAt time.Time,
	finishedAt time.Time,
	workspaceID string,
	sourceKind string,
	sourceID string,
	attempt int,
	outcome productFileMaintenanceObjectOutcome,
) error {
	workspaceID = strings.TrimSpace(workspaceID)
	sourceID = strings.TrimSpace(sourceID)
	scheduledAt = scheduledAt.UTC()
	finishedAt = finishedAt.UTC()
	diagnosticReference := strings.TrimSpace(outcome.DiagnosticReference)
	if err := ctx.Err(); err != nil {
		return errors.Join(errFileMaintenanceStateInvalid, err)
	}
	if scheduledAt.IsZero() || scheduledAt.UnixNano() <= 0 ||
		finishedAt.Before(scheduledAt) || finishedAt.UnixMilli() <= 0 ||
		workspaceID == "" || sourceID == "" || attempt <= 0 ||
		(sourceKind != productFileMaintenanceSourceUpload &&
			sourceKind != productFileMaintenanceSourceReference) ||
		!validProductFileMaintenanceStats(productFileMaintenanceWorkspaceStats{
			Scanned: 1, Expired: outcome.Expired,
			DeletedObjects:      outcome.DeletedObjects,
			FailedObjects:       outcome.FailedObjects,
			DiagnosticReference: outcome.DiagnosticReference,
		}) || diagnosticReference != outcome.DiagnosticReference ||
		(outcome.FailedObjects == 0 && diagnosticReference != "") ||
		(outcome.FailedObjects > 0 && diagnosticReference == "") {
		return errFileMaintenanceStateInvalid
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_file_maintenance_outcomes}}
			([[id]],[[workspace_id]],[[source_kind]],[[source_id]],[[attempt]],
			 [[scheduled_at_nanos]],[[finished_at]],[[expired]],[[deleted_objects]],
			 [[failed_objects]],[[diagnostic_reference]],[[created_at]],[[updated_at]])
		VALUES ({:id},{:workspaceId},{:sourceKind},{:sourceId},{:attempt},
			{:scheduledAtNanos},{:finishedAt},{:expired},{:deletedObjects},
			{:failedObjects},{:diagnosticReference},{:createdAt},{:updatedAt})
		ON CONFLICT ([[id]]) DO NOTHING
	`).WithContext(ctx).Bind(dbx.Params{
		"id":          productFileMaintenanceOutcomeID(sourceKind, sourceID, attempt),
		"workspaceId": workspaceID, "sourceKind": sourceKind,
		"sourceId": sourceID, "attempt": attempt,
		"scheduledAtNanos": scheduledAt.UnixNano(), "finishedAt": finishedAt.UnixMilli(),
		"expired": outcome.Expired, "deletedObjects": outcome.DeletedObjects,
		"failedObjects":       outcome.FailedObjects,
		"diagnosticReference": outcome.DiagnosticReference,
		"createdAt":           finishedAt.UnixMilli(), "updatedAt": finishedAt.UnixMilli(),
	}).Execute()
	return err
}

func runnerFinishedAt(clock durableJobClock, scheduledAt time.Time) time.Time {
	finishedAt := clock.now().UTC()
	if finishedAt.Before(scheduledAt) {
		return scheduledAt.UTC()
	}
	return finishedAt
}

func settleProductFileMaintenanceOutcomes(
	ctx context.Context,
	app core.App,
) (productFileMaintenanceOutcomeSettlement, error) {
	settlement := productFileMaintenanceOutcomeSettlement{}
	var pending int
	if err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_file_maintenance_outcomes}}
			INDEXED BY [[idx_leafen_file_maintenance_outcomes_due]] LIMIT 1
		)
	`).WithContext(ctx).Row(&pending); err != nil || pending == 0 {
		return settlement, err
	}
	err := app.RunInTransaction(func(txApp core.App) error {
		rows := []storedProductFileMaintenanceOutcome{}
		if err := txApp.DB().NewQuery(`
			SELECT [[id]],[[workspace_id]],[[source_kind]],[[source_id]],[[attempt]],
				[[scheduled_at_nanos]],[[finished_at]],[[expired]],[[deleted_objects]],
				[[failed_objects]],[[diagnostic_reference]]
			FROM {{leafen_file_maintenance_outcomes}}
			INDEXED BY [[idx_leafen_file_maintenance_outcomes_due]]
			ORDER BY [[scheduled_at_nanos]],[[id]]
			LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"limit": fileMaintenanceCombinedTurnLimit + 1,
		}).All(&rows); err != nil {
			return err
		}
		settlement.HasMore = len(rows) > fileMaintenanceCombinedTurnLimit
		if settlement.HasMore {
			rows = rows[:fileMaintenanceCombinedTurnLimit]
		}
		if len(rows) == 0 {
			return nil
		}
		type turn struct {
			scheduledAt time.Time
			finishedAt  time.Time
			workspaces  map[string]productFileMaintenanceWorkspaceStats
		}
		turns := map[int64]*turn{}
		ordered := []int64{}
		for _, row := range rows {
			if row.ScheduledAtNanos <= 0 || row.FinishedAt <= 0 || row.Attempt <= 0 ||
				(row.SourceKind != productFileMaintenanceSourceUpload &&
					row.SourceKind != productFileMaintenanceSourceReference) {
				return errFileMaintenanceStateInvalid
			}
			current := turns[row.ScheduledAtNanos]
			if current == nil {
				finishedAt := time.UnixMilli(row.FinishedAt).UTC()
				scheduledAt := time.Unix(0, row.ScheduledAtNanos).UTC()
				if finishedAt.Before(scheduledAt) {
					finishedAt = scheduledAt
				}
				current = &turn{
					scheduledAt: scheduledAt,
					finishedAt:  finishedAt,
					workspaces:  map[string]productFileMaintenanceWorkspaceStats{},
				}
				turns[row.ScheduledAtNanos] = current
				ordered = append(ordered, row.ScheduledAtNanos)
			}
			if candidate := time.UnixMilli(row.FinishedAt).UTC(); candidate.After(current.finishedAt) {
				current.finishedAt = candidate
			}
			stats := current.workspaces[row.WorkspaceID]
			stats.add(productFileMaintenanceWorkspaceStats{
				Scanned: 1, Expired: row.Expired,
				DeletedObjects:      row.DeletedObjects,
				FailedObjects:       row.FailedObjects,
				DiagnosticReference: row.DiagnosticReference,
			})
			current.workspaces[row.WorkspaceID] = stats
		}
		sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
		workspaceSet := map[string]struct{}{}
		for _, scheduledAtNanos := range ordered {
			workspaceIDs, err := productFileMaintenanceWorkspaceIDs(
				turns[scheduledAtNanos].workspaces,
			)
			if err != nil {
				return err
			}
			for _, workspaceID := range workspaceIDs {
				workspaceSet[workspaceID] = struct{}{}
			}
		}
		workspaceIDs := make([]string, 0, len(workspaceSet))
		for workspaceID := range workspaceSet {
			workspaceIDs = append(workspaceIDs, workspaceID)
		}
		sort.Strings(workspaceIDs)
		organizations, err := loadProductFileMaintenanceOrganizations(
			ctx, txApp, workspaceIDs,
		)
		if err != nil {
			return err
		}
		for _, scheduledAtNanos := range ordered {
			current := turns[scheduledAtNanos]
			turnWorkspaceIDs, err := productFileMaintenanceWorkspaceIDs(current.workspaces)
			if err != nil {
				return err
			}
			if err := insertProductFileMaintenanceRuns(
				ctx, txApp, current.scheduledAt, current.finishedAt, current.workspaces,
				turnWorkspaceIDs, organizations,
			); err != nil {
				return err
			}
		}
		if err := retainProductFileMaintenanceRuns(ctx, txApp, workspaceIDs); err != nil {
			return err
		}
		if err := applyProductFileManualCleanupOutcomes(ctx, txApp, rows); err != nil {
			return err
		}
		params := dbx.Params{}
		identities := make([]string, 0, len(rows))
		for index, row := range rows {
			key := "id" + formatInt64(int64(index))
			params[key] = row.ID
			identities = append(identities, "{:"+key+"}")
		}
		deleted, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_file_maintenance_outcomes}}
			WHERE [[id]] IN (` + strings.Join(identities, ",") + `)
		`).WithContext(ctx).Bind(params).Execute()
		if err != nil {
			return err
		}
		count, err := deleted.RowsAffected()
		if err != nil || count != int64(len(rows)) {
			return errors.Join(errFileMaintenanceStateInvalid, err)
		}
		settlement.Settled = len(rows)
		return nil
	})
	return settlement, err
}
