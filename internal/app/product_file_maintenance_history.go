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

const productFileMaintenanceRunKind = "file-cleanup"

var errFileMaintenanceStateInvalid = errors.New(errorCodeFileMaintenanceStateInvalid)

type productFileMaintenanceWorkspaceStats struct {
	Scanned             int
	Expired             int
	DeletedObjects      int
	FailedObjects       int
	DiagnosticReference string
}

type productFileMaintenanceObjectOutcome struct {
	Expired             int
	DeletedObjects      int
	FailedObjects       int
	DiagnosticReference string
}

func (stats *productFileMaintenanceWorkspaceStats) add(
	other productFileMaintenanceWorkspaceStats,
) {
	stats.Scanned += other.Scanned
	stats.Expired += other.Expired
	stats.DeletedObjects += other.DeletedObjects
	stats.FailedObjects += other.FailedObjects
	if stats.DiagnosticReference == "" {
		stats.DiagnosticReference = other.DiagnosticReference
	}
}

type productFileMaintenanceSliceResult struct {
	Processed  int
	HasMore    bool
	NextDue    int64
	Workspaces map[string]productFileMaintenanceWorkspaceStats
}

func (result *productFileMaintenanceSliceResult) addWorkspaceOutcome(
	workspaceID string,
	outcome productFileMaintenanceObjectOutcome,
) {
	if result.Workspaces == nil {
		result.Workspaces = map[string]productFileMaintenanceWorkspaceStats{}
	}
	stats := result.Workspaces[workspaceID]
	stats.Scanned++
	stats.Expired += outcome.Expired
	stats.DeletedObjects += outcome.DeletedObjects
	stats.FailedObjects += outcome.FailedObjects
	if stats.DiagnosticReference == "" {
		stats.DiagnosticReference = outcome.DiagnosticReference
	}
	result.Workspaces[workspaceID] = stats
}

type productFileMaintenanceLane func(
	context.Context,
	time.Time,
) (productFileMaintenanceSliceResult, error)

type productFileWorkspaceMaintenanceLane func(
	context.Context,
	time.Time,
	string,
	string,
) (productFileMaintenanceSliceResult, error)

type storedProductFileMaintenanceRun struct {
	ID                  string `db:"id"`
	WorkspaceID         string `db:"workspace_id"`
	Kind                string `db:"kind"`
	Status              string `db:"status"`
	ScheduledAt         int64  `db:"scheduled_at"`
	StartedAt           int64  `db:"started_at"`
	FinishedAt          int64  `db:"finished_at"`
	Scanned             int    `db:"scanned"`
	Expired             int    `db:"expired"`
	DeletedObjects      int    `db:"deleted_objects"`
	FailedObjects       int    `db:"failed_objects"`
	DiagnosticReference string `db:"diagnostic_reference"`
}

func productFileMaintenanceRunID(workspaceID string, scheduledAt time.Time) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		productFileMaintenanceRunKind,
		workspaceID,
		formatInt64(scheduledAt.UTC().UnixNano()),
	}, "\x1f")))
	return "fm_" + hex.EncodeToString(digest[:])
}

func formatInt64(value int64) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	buffer := [20]byte{}
	position := len(buffer)
	for value > 0 {
		position--
		buffer[position] = digits[value%10]
		value /= 10
	}
	return string(buffer[position:])
}

func validProductFileMaintenanceStats(stats productFileMaintenanceWorkspaceStats) bool {
	return stats.Scanned >= 0 && stats.Expired >= 0 &&
		stats.DeletedObjects >= 0 && stats.FailedObjects >= 0 &&
		stats.Expired <= stats.Scanned && stats.FailedObjects <= stats.Scanned &&
		stats.DeletedObjects+stats.FailedObjects <= stats.Scanned
}

func productFileMaintenanceRunStatus(stats productFileMaintenanceWorkspaceStats) string {
	if stats.FailedObjects == 0 {
		return fileMaintenanceRunStatusSuccess
	}
	if stats.FailedObjects >= stats.Scanned {
		return fileMaintenanceRunStatusFailed
	}
	return fileMaintenanceRunStatusPartialFailure
}

func loadProductFileMaintenanceOrganizations(
	ctx context.Context,
	app core.App,
	workspaceIDs []string,
) (map[string]string, error) {
	params := dbx.Params{}
	identities := make([]string, 0, len(workspaceIDs))
	for index, workspaceID := range workspaceIDs {
		key := "workspace" + formatInt64(int64(index))
		params[key] = workspaceID
		identities = append(identities, "{:"+key+"}")
	}
	rows := []struct {
		ID             string `db:"id"`
		OrganizationID string `db:"organization_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[organization]] AS [[organization_id]]
		FROM {{leafen_workspaces}} WHERE [[id]] IN (` + strings.Join(identities, ",") + `)
		ORDER BY [[id]]
	`).WithContext(ctx).Bind(params).All(&rows)
	if err != nil {
		return nil, err
	}
	organizations := make(map[string]string, len(rows))
	for _, row := range rows {
		organizations[row.ID] = row.OrganizationID
	}
	if len(organizations) != len(workspaceIDs) {
		return nil, errFileMaintenanceStateInvalid
	}
	return organizations, nil
}

func productFileMaintenanceWorkspaceIDs(
	statsByWorkspace map[string]productFileMaintenanceWorkspaceStats,
) ([]string, error) {
	if len(statsByWorkspace) > fileMaintenanceCombinedTurnLimit {
		return nil, errFileMaintenanceStateInvalid
	}
	workspaceIDs := make([]string, 0, len(statsByWorkspace))
	for workspaceID, stats := range statsByWorkspace {
		normalizedWorkspaceID := strings.TrimSpace(workspaceID)
		if normalizedWorkspaceID == "" || normalizedWorkspaceID != workspaceID ||
			!validProductFileMaintenanceStats(stats) ||
			(stats.FailedObjects == 0 && strings.TrimSpace(stats.DiagnosticReference) != "") {
			return nil, errFileMaintenanceStateInvalid
		}
		if stats.Scanned == 0 {
			continue
		}
		workspaceIDs = append(workspaceIDs, workspaceID)
	}
	sort.Strings(workspaceIDs)
	return workspaceIDs, nil
}

func insertProductFileMaintenanceRuns(
	ctx context.Context,
	app core.App,
	scheduledAt time.Time,
	finishedAt time.Time,
	statsByWorkspace map[string]productFileMaintenanceWorkspaceStats,
	workspaceIDs []string,
	organizations map[string]string,
) error {
	if scheduledAt.IsZero() || finishedAt.Before(scheduledAt) ||
		len(workspaceIDs) > fileMaintenanceCombinedTurnLimit {
		return errFileMaintenanceStateInvalid
	}
	scheduledMillis := scheduledAt.UTC().UnixMilli()
	finishedMillis := finishedAt.UTC().UnixMilli()
	if scheduledMillis <= 0 || finishedMillis < scheduledMillis {
		return errFileMaintenanceStateInvalid
	}
	for _, workspaceID := range workspaceIDs {
		if organizations[workspaceID] == "" {
			return errFileMaintenanceStateInvalid
		}
		stats := statsByWorkspace[workspaceID]
		reference := stats.DiagnosticReference
		if stats.FailedObjects > 0 && reference == "" {
			reference = newDiagnosticReference()
		}
		_, err := app.DB().NewQuery(`
				INSERT INTO {{leafen_file_maintenance_runs}}
					([[id]],[[workspace_id]],[[organization_id]],[[kind]],[[status]],
					 [[scheduled_at]],[[started_at]],[[finished_at]],[[scanned]],[[expired]],
					 [[deleted_objects]],[[failed_objects]],[[diagnostic_reference]],
					 [[created_at]],[[updated_at]])
				VALUES ({:id},{:workspaceId},{:organizationId},{:kind},{:status},
					{:scheduledAt},{:startedAt},{:finishedAt},{:scanned},{:expired},
					{:deletedObjects},{:failedObjects},{:diagnosticReference},
					{:createdAt},{:updatedAt})
				ON CONFLICT ([[id]]) DO NOTHING
		`).WithContext(ctx).Bind(dbx.Params{
			"id":          productFileMaintenanceRunID(workspaceID, scheduledAt),
			"workspaceId": workspaceID, "organizationId": organizations[workspaceID],
			"kind":        productFileMaintenanceRunKind,
			"status":      productFileMaintenanceRunStatus(stats),
			"scheduledAt": scheduledMillis, "startedAt": scheduledMillis,
			"finishedAt": finishedMillis, "scanned": stats.Scanned,
			"expired": stats.Expired, "deletedObjects": stats.DeletedObjects,
			"failedObjects":       stats.FailedObjects,
			"diagnosticReference": reference,
			"createdAt":           finishedMillis, "updatedAt": finishedMillis,
		}).Execute()
		if err != nil {
			return err
		}
	}
	return nil
}

func retainProductFileMaintenanceRuns(
	ctx context.Context,
	app core.App,
	workspaceIDs []string,
) error {
	if len(workspaceIDs) == 0 || len(workspaceIDs) > fileMaintenanceCombinedTurnLimit {
		return errFileMaintenanceStateInvalid
	}
	params := dbx.Params{"retained": fileMaintenanceRunRetentionPerWorkspace}
	identities := make([]string, 0, len(workspaceIDs))
	for index, workspaceID := range workspaceIDs {
		key := "workspace" + formatInt64(int64(index))
		params[key] = workspaceID
		identities = append(identities, "{:"+key+"}")
	}
	_, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_file_maintenance_runs}} AS [[candidate]]
			WHERE [[candidate]].[[workspace_id]] IN (` + strings.Join(identities, ",") + `)
				AND [[candidate]].[[id]] NOT IN (
					SELECT [[retained]].[[id]] FROM {{leafen_file_maintenance_runs}} AS [[retained]]
					WHERE [[retained]].[[workspace_id]]=[[candidate]].[[workspace_id]]
					ORDER BY [[retained]].[[started_at]] DESC,[[retained]].[[id]] DESC
					LIMIT {:retained}
				)
	`).WithContext(ctx).Bind(params).Execute()
	return err
}

func loadRecentProductFileMaintenanceRuns(
	ctx context.Context,
	app core.App,
	workspaceID string,
	organizationID string,
	limit int,
) ([]fileUsageReportMaintenanceRun, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	organizationID = strings.TrimSpace(organizationID)
	if (workspaceID == "") == (organizationID == "") ||
		limit < 1 || limit > fileMaintenanceRunRetentionPerWorkspace {
		return nil, errFileMaintenanceStateInvalid
	}
	rows := []storedProductFileMaintenanceRun{}
	params := dbx.Params{"limit": limit + 1}
	query := `SELECT [[run]].[[id]],[[run]].[[workspace_id]],[[run]].[[kind]],
		[[run]].[[status]],[[run]].[[scheduled_at]],[[run]].[[started_at]],
		[[run]].[[finished_at]],[[run]].[[scanned]],[[run]].[[expired]],
		[[run]].[[deleted_objects]],[[run]].[[failed_objects]],
		[[run]].[[diagnostic_reference]] FROM {{leafen_file_maintenance_runs}} AS [[run]]`
	if workspaceID != "" {
		query += ` WHERE [[run]].[[workspace_id]]={:workspaceId}`
		params["workspaceId"] = workspaceID
	} else {
		query += ` INNER JOIN {{leafen_workspaces}} AS [[workspace]]
			ON [[workspace]].[[id]]=[[run]].[[workspace_id]]
			AND [[workspace]].[[organization]]=[[run]].[[organization_id]]
			WHERE [[run]].[[organization_id]]={:organizationId}`
		params["organizationId"] = organizationID
	}
	query += ` ORDER BY [[run]].[[started_at]] DESC,[[run]].[[id]] DESC LIMIT {:limit}`
	if err := app.DB().NewQuery(query).WithContext(ctx).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	manual, err := loadRecentProductFileManualCleanups(ctx, app, workspaceID, organizationID, limit+1)
	if err != nil {
		return nil, err
	}
	rows = append(rows, manual...)
	sort.Slice(rows, func(left, right int) bool {
		if rows[left].StartedAt != rows[right].StartedAt {
			return rows[left].StartedAt > rows[right].StartedAt
		}
		return rows[left].ID > rows[right].ID
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	runs := make([]fileUsageReportMaintenanceRun, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, fileUsageReportMaintenanceRun{
			ID: row.ID, WorkspaceID: row.WorkspaceID, Kind: row.Kind,
			Status: row.Status, ScheduledAt: row.ScheduledAt,
			StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
			Scanned: row.Scanned, Expired: row.Expired,
			DeletedObjects: row.DeletedObjects, FailedObjects: row.FailedObjects,
			DiagnosticReference: row.DiagnosticReference,
		})
	}
	return runs, nil
}

// loadRecentProductFileManualCleanups reads the finished "정리" of the usage
// screen as runs, which is the last cleanup there even when it found nothing.
func loadRecentProductFileManualCleanups(
	ctx context.Context,
	app core.App,
	workspaceID string,
	organizationID string,
	limit int,
) ([]storedProductFileMaintenanceRun, error) {
	rows := []storedProductFileMaintenanceRun{}
	params := dbx.Params{"limit": limit, "running": fileManualCleanupStatusRunning}
	query := `SELECT [[job]].[[id]],[[job]].[[workspace_id]],[[job]].[[scheduled_at]],
		[[job]].[[scheduled_at]] AS [[started_at]],[[job]].[[completed_at]] AS [[finished_at]],
		[[job]].[[scanned]],[[job]].[[expired]],[[job]].[[deleted_objects]],[[job]].[[failed_objects]],
		[[job]].[[diagnostic_reference]] FROM {{leafen_file_manual_cleanup_jobs}} AS [[job]]`
	if workspaceID != "" {
		query += ` WHERE [[job]].[[workspace_id]]={:workspaceId}`
		params["workspaceId"] = workspaceID
	} else {
		query += ` INNER JOIN {{leafen_workspaces}} AS [[workspace]] ON [[workspace]].[[id]]=[[job]].[[workspace_id]]
			WHERE [[workspace]].[[organization]]={:organizationId}`
		params["organizationId"] = organizationID
	}
	query += ` AND [[job]].[[status]]!={:running} ORDER BY [[job]].[[scheduled_at]] DESC,[[job]].[[id]] DESC LIMIT {:limit}`
	if err := app.DB().NewQuery(query).WithContext(ctx).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	for index := range rows {
		row := &rows[index]
		row.Kind = productFileMaintenanceRunKind
		row.Status = productFileMaintenanceRunStatus(productFileMaintenanceWorkspaceStats{
			Scanned: row.Scanned, FailedObjects: row.FailedObjects,
		})
		row.FinishedAt = max(row.FinishedAt, row.StartedAt)
		if row.FailedObjects == 0 {
			row.DiagnosticReference = ""
		} else if strings.TrimSpace(row.DiagnosticReference) == "" {
			row.DiagnosticReference = newDiagnosticReference()
		}
	}
	return rows, nil
}
