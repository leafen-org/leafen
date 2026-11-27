package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func recordProductFileMaintenanceOutcomeForManualJob(
	ctx context.Context,
	app core.App,
	scheduledAt time.Time,
	finishedAt time.Time,
	workspaceID string,
	sourceKind string,
	sourceID string,
	attempt int,
	outcome productFileMaintenanceObjectOutcome,
	jobID string,
) error {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return errFileMaintenanceStateInvalid
	}
	if err := recordProductFileMaintenanceOutcome(
		ctx, app, scheduledAt, finishedAt, workspaceID,
		sourceKind, sourceID, attempt, outcome,
	); err != nil {
		return err
	}
	outcomeID := productFileMaintenanceOutcomeID(sourceKind, sourceID, attempt)
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_file_manual_cleanup_outcomes}} ([[outcome_id]],[[job_id]])
		VALUES ({:outcomeId},{:jobId})
		ON CONFLICT ([[outcome_id]]) DO NOTHING
	`).WithContext(ctx).Bind(dbx.Params{
		"outcomeId": outcomeID, "jobId": jobID,
	}).Execute()
	if err != nil {
		return err
	}
	var mappedJobID string
	if err := app.DB().NewQuery(`SELECT [[job_id]] FROM {{leafen_file_manual_cleanup_outcomes}}
		WHERE [[outcome_id]]={:outcomeId}`,
	).WithContext(ctx).Bind(dbx.Params{"outcomeId": outcomeID}).Row(&mappedJobID); err != nil {
		return err
	}
	if mappedJobID != jobID {
		return errFileMaintenanceStateInvalid
	}
	return nil
}

func applyProductFileManualCleanupOutcomes(
	ctx context.Context,
	app core.App,
	rows []storedProductFileMaintenanceOutcome,
) error {
	if len(rows) == 0 || !tableKnown(app, productFileManualCleanupOutcomesTable) {
		return nil
	}
	params := dbx.Params{}
	identities := make([]string, 0, len(rows))
	byID := make(map[string]storedProductFileMaintenanceOutcome, len(rows))
	for index, row := range rows {
		key := "outcome" + formatInt64(int64(index))
		params[key] = row.ID
		identities = append(identities, "{:"+key+"}")
		byID[row.ID] = row
	}
	mappings := []struct {
		OutcomeID string `db:"outcome_id"`
		JobID     string `db:"job_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[outcome_id]],[[job_id]] FROM {{leafen_file_manual_cleanup_outcomes}}
		WHERE [[outcome_id]] IN (` + strings.Join(identities, ",") + `)
		ORDER BY [[job_id]],[[outcome_id]]
	`).WithContext(ctx).Bind(params).All(&mappings); err != nil {
		return err
	}
	type aggregate struct {
		scanned             int
		expired             int
		deletedObjects      int
		failedObjects       int
		diagnosticReference string
		updatedAt           int64
	}
	byJob := map[string]aggregate{}
	for _, mapping := range mappings {
		row, ok := byID[mapping.OutcomeID]
		if !ok {
			return errFileMaintenanceStateInvalid
		}
		current := byJob[mapping.JobID]
		current.scanned++
		current.expired += row.Expired
		current.deletedObjects += row.DeletedObjects
		current.failedObjects += row.FailedObjects
		if current.diagnosticReference == "" {
			current.diagnosticReference = row.DiagnosticReference
		}
		if row.FinishedAt > current.updatedAt {
			current.updatedAt = row.FinishedAt
		}
		byJob[mapping.JobID] = current
	}
	for jobID, current := range byJob {
		updated, err := app.DB().NewQuery(`
			UPDATE {{leafen_file_manual_cleanup_jobs}} SET
				[[scanned]]=[[scanned]]+{:scanned},
				[[expired]]=[[expired]]+{:expired},
				[[deleted_objects]]=[[deleted_objects]]+{:deletedObjects},
				[[failed_objects]]=[[failed_objects]]+{:failedObjects},
				[[diagnostic_reference]]=CASE
					WHEN [[diagnostic_reference]]='' THEN {:diagnosticReference}
					ELSE [[diagnostic_reference]] END,
				[[revision]]=[[revision]]+1,[[updated_at]]={:updatedAt}
			WHERE [[id]]={:jobId} AND [[status]]='running'
		`).WithContext(ctx).Bind(dbx.Params{
			"scanned": current.scanned, "expired": current.expired,
			"deletedObjects":      current.deletedObjects,
			"failedObjects":       current.failedObjects,
			"diagnosticReference": current.diagnosticReference,
			"updatedAt":           current.updatedAt, "jobId": jobID,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := updated.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errFileMaintenanceStateInvalid, err)
		}
	}
	return nil
}
