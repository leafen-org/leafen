package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func administratorAccountCleanupResponse(app core.App, job storedJob, cursor string) (administratorAccountCleanup, error) {
	response := administratorAccountCleanup{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil {
			return err
		}
		response, err = administratorAccountCleanupSnapshot(txApp, current, cursor)
		return err
	})
	return response, err
}

func administratorAccountCleanupSnapshot(app core.App, job storedJob, cursor string) (administratorAccountCleanup, error) {
	payload, err := decodeAdministratorAccountCleanupPayload(job)
	if err != nil {
		return administratorAccountCleanup{}, err
	}
	rows, next, err := listAdministratorAccountCleanupAssignments(app, job.ID, cursor)
	if err != nil {
		return administratorAccountCleanup{}, err
	}
	unassigned := 0
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_administrator_account_cleanup_assignments}}
		WHERE [[job_id]]={:jobId} AND [[status]]='unassigned'`).Bind(dbx.Params{"jobId": job.ID}).Row(&unassigned); err != nil {
		return administratorAccountCleanup{}, err
	}
	status := administratorAccountCleanupPending
	if job.Status == administratorAccountCleanupWaitingStatus {
		status = administratorAccountCleanupWaiting
	} else if job.Status == durableJobStatusCompleted {
		status = administratorAccountCleanupCompleted
	} else if job.Status == durableJobStatusFailed {
		status = administratorAccountCleanupFailed
	}
	return administratorAccountCleanup{
		JobID: job.ID, TargetUserID: payload.TargetUserID, Status: status, Phase: payload.Phase,
		Revision: payload.Revision, ProcessedItems: job.Processed, TotalItems: job.Total,
		UnassignedCount: unassigned, Assignments: rows, NextCursor: next, ErrorCode: job.ErrorCode,
	}, nil
}

func listAdministratorAccountCleanupAssignments(app core.App, jobID, cursor string) ([]administratorAccountTransferAssignment, *string, error) {
	afterType, afterID, err := decodeAdministratorAccountCleanupCursor(cursor)
	if err != nil {
		return nil, nil, err
	}
	rows := []storedAdministratorAccountTransferAssignment{}
	err = app.DB().NewQuery(`SELECT [[resource_type]],[[resource_id]],[[resource_name]],[[successor_user_id]],[[status]]
		FROM {{leafen_administrator_account_cleanup_assignments}} WHERE [[job_id]]={:jobId}
			AND ([[resource_type]]>{:afterType} OR ([[resource_type]]={:afterType} AND [[resource_id]]>{:afterId}))
		ORDER BY [[resource_type]],[[resource_id]] LIMIT {:limit}`).Bind(dbx.Params{
		"jobId": jobID, "afterType": afterType, "afterId": afterID, "limit": administratorAccountCleanupTurnLimit + 1,
	}).All(&rows)
	if err != nil {
		return nil, nil, err
	}
	var next *string
	if len(rows) > administratorAccountCleanupTurnLimit {
		last := rows[administratorAccountCleanupTurnLimit-1]
		value := base64.RawURLEncoding.EncodeToString([]byte(last.ResourceType + "\x00" + last.ResourceID))
		next, rows = &value, rows[:administratorAccountCleanupTurnLimit]
	}
	result := make([]administratorAccountTransferAssignment, 0, len(rows))
	for _, row := range rows {
		var successor *string
		if row.SuccessorUserID.Valid {
			value := row.SuccessorUserID.String
			successor = &value
		}
		result = append(result, administratorAccountTransferAssignment{
			ResourceType: row.ResourceType, ResourceID: row.ResourceID, ResourceName: row.ResourceName,
			SuccessorUserID: successor, Status: row.Status,
		})
	}
	return result, next, nil
}

func decodeAdministratorAccountCleanupCursor(value string) (string, string, error) {
	if value == "" {
		return "", "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	parts := strings.Split(string(decoded), "\x00")
	if err != nil || len(parts) != 2 {
		return "", "", fmt.Errorf("%s: invalid cleanup cursor", errorCodeInvalidInput)
	}
	return parts[0], parts[1], nil
}
