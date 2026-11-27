package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type authorizedProductFileUsageReportRequest struct {
	ActorID           string
	RequestID         string
	ReportType        string
	WorkspaceID       string
	OrganizationID    string
	StorageLimitBytes *int64
	JobID             string
	ByWorkspaceAfter  string
	MaintenanceLimit  int
	NowMillis         int64
}

type productFileUsageReportTurn struct {
	Response              fileUsageReportResponse
	SourceRows            int
	Workspaces            int
	TransactionOperations int
}

type storedProductFileUsageReportJob struct {
	ID                      string `db:"id"`
	LifecycleKey            string `db:"lifecycle_key"`
	ReportType              string `db:"report_type"`
	ActorID                 string `db:"actor_id"`
	RequestID               string `db:"request_id"`
	WorkspaceID             string `db:"workspace_id"`
	OrganizationID          string `db:"organization_id"`
	StorageLimitBytes       string `db:"storage_limit_bytes"`
	MaintenanceLimit        int    `db:"maintenance_limit"`
	Status                  string `db:"status"`
	Phase                   string `db:"phase"`
	WorkspaceCursor         string `db:"workspace_cursor"`
	CurrentWorkspaceID      string `db:"current_workspace_id"`
	CurrentWorkspaceName    string `db:"current_workspace_name"`
	CurrentWorkspaceDomain  string `db:"current_workspace_domain"`
	UploadCursor            string `db:"upload_cursor"`
	Revision                int    `db:"revision"`
	SummaryJSON             string `db:"summary_json"`
	CurrentWorkspaceSummary string `db:"current_workspace_summary_json"`
	MaintenanceRunsJSON     string `db:"maintenance_runs_json"`
	WorkspaceCount          int    `db:"workspace_count"`
	StartedAt               int64  `db:"started_at"`
	CompletedAt             int64  `db:"completed_at"`
	CreatedAt               int64  `db:"created_at"`
	UpdatedAt               int64  `db:"updated_at"`
}

type productFileUsageReportWorkspace struct {
	ID     string `db:"id"`
	Name   string `db:"name"`
	Domain string `db:"domain"`
}

type storedProductFileUsageReportWorkspaceResult struct {
	WorkspaceID        string `db:"workspace_id"`
	Name               string `db:"name"`
	Domain             string `db:"domain"`
	ActiveStorageBytes int64  `db:"active_storage_bytes"`
	TotalsJSON         string `db:"totals_json"`
	PendingJSON        string `db:"pending_json"`
}

type productFileUsageReportSourceWindow struct {
	Items    []productFileUsageReportSource
	Next     string
	HasMore  bool
	ReadRows int
}

const productFileUsageReportSelect = `
	SELECT [[id]],[[lifecycle_key]],[[report_type]],[[actor_id]],[[request_id]],
		[[workspace_id]],[[organization_id]],[[storage_limit_bytes]],[[maintenance_limit]],[[status]],[[phase]],
		[[workspace_cursor]],[[current_workspace_id]],[[current_workspace_name]],
		[[current_workspace_domain]],[[upload_cursor]],[[revision]],[[summary_json]],
		[[current_workspace_summary_json]],[[maintenance_runs_json]],[[workspace_count]],[[started_at]],
		[[completed_at]],[[created_at]],[[updated_at]]
	FROM {{leafen_file_usage_report_jobs}}
`

func normalizeAuthorizedProductFileUsageReportRequest(
	request *authorizedProductFileUsageReportRequest,
) error {
	request.ActorID = strings.TrimSpace(request.ActorID)
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.WorkspaceID = strings.TrimSpace(request.WorkspaceID)
	request.OrganizationID = strings.TrimSpace(request.OrganizationID)
	request.JobID = strings.TrimSpace(request.JobID)
	request.ByWorkspaceAfter = strings.TrimSpace(request.ByWorkspaceAfter)
	if request.ActorID == "" || request.RequestID == "" ||
		utf8.RuneCountInString(request.RequestID) > maxWorkspacePermissionIdentifierLength {
		return errFileUsageReportInvalid
	}
	if request.MaintenanceLimit == 0 {
		request.MaintenanceLimit = 10
	}
	if request.MaintenanceLimit < 1 || request.MaintenanceLimit > 50 {
		return errFileUsageReportInvalid
	}
	if request.NowMillis <= 0 {
		request.NowMillis = time.Now().UTC().UnixMilli()
	}
	workspace := request.ReportType == fileUsageReportTypeWorkspace &&
		request.WorkspaceID != "" && request.OrganizationID == ""
	organization := request.ReportType == fileUsageReportTypeOrganization &&
		request.OrganizationID != "" && request.WorkspaceID == ""
	if !workspace && !organization {
		return errFileUsageReportInvalid
	}
	return nil
}

func productFileUsageReportLifecycleKey(request authorizedProductFileUsageReportRequest) string {
	target := request.WorkspaceID
	if request.ReportType == fileUsageReportTypeOrganization {
		target = request.OrganizationID
	}
	return strings.Join([]string{
		request.ReportType, target, request.ActorID, strconv.Itoa(request.MaintenanceLimit),
	}, "\x1f")
}

func loadProductFileUsageReportJob(
	ctx context.Context,
	app core.App,
	request authorizedProductFileUsageReportRequest,
) (storedProductFileUsageReportJob, error) {
	job := storedProductFileUsageReportJob{}
	params := dbx.Params{"actorId": request.ActorID}
	where := "[[actor_id]]={:actorId}"
	if request.JobID != "" {
		where += " AND [[id]]={:jobId}"
		params["jobId"] = request.JobID
	} else {
		where += " AND [[request_id]]={:requestId}"
		params["requestId"] = request.RequestID
	}
	err := app.DB().NewQuery(productFileUsageReportSelect + " WHERE " + where + " LIMIT 1").
		WithContext(ctx).Bind(params).One(&job)
	if errors.Is(err, sql.ErrNoRows) {
		return job, errFileUsageReportContinuationInvalid
	}
	return job, err
}

func createProductFileUsageReportJob(
	ctx context.Context,
	app core.App,
	request authorizedProductFileUsageReportRequest,
) (storedProductFileUsageReportJob, error) {
	empty := emptyProductFileUsageReportAccumulator()
	emptyJSON, err := json.Marshal(empty)
	if err != nil {
		return storedProductFileUsageReportJob{}, errors.Join(errFileUsageReportStateInvalid, err)
	}
	phase := fileUsageReportPhaseUploads
	currentWorkspaceID := request.WorkspaceID
	if request.ReportType == fileUsageReportTypeOrganization {
		phase = fileUsageReportPhaseWorkspaceSelect
	}
	job := storedProductFileUsageReportJob{
		ID: core.GenerateDefaultRandomId(), LifecycleKey: productFileUsageReportLifecycleKey(request),
		ReportType: request.ReportType, ActorID: request.ActorID, RequestID: request.RequestID,
		WorkspaceID: request.WorkspaceID, OrganizationID: request.OrganizationID,
		MaintenanceLimit: request.MaintenanceLimit, Status: fileUsageReportStatusRunning,
		Phase: phase, CurrentWorkspaceID: currentWorkspaceID,
		SummaryJSON: string(emptyJSON), CurrentWorkspaceSummary: string(emptyJSON),
		MaintenanceRunsJSON: "[]",
		StartedAt:           request.NowMillis, CreatedAt: request.NowMillis, UpdatedAt: request.NowMillis,
	}
	if request.StorageLimitBytes != nil {
		job.StorageLimitBytes = strconv.FormatInt(*request.StorageLimitBytes, 10)
	}
	insert := func() error {
		_, err := app.DB().Insert(productFileUsageReportJobsTable, dbx.Params{
			"id": job.ID, "lifecycle_key": job.LifecycleKey, "report_type": job.ReportType,
			"actor_id": job.ActorID, "request_id": job.RequestID,
			"workspace_id": job.WorkspaceID, "organization_id": job.OrganizationID,
			"storage_limit_bytes": job.StorageLimitBytes,
			"maintenance_limit":   job.MaintenanceLimit, "status": job.Status, "phase": job.Phase,
			"workspace_cursor": "", "current_workspace_id": job.CurrentWorkspaceID,
			"current_workspace_name": "", "current_workspace_domain": "", "upload_cursor": "",
			"revision": 0, "summary_json": job.SummaryJSON,
			"current_workspace_summary_json": job.CurrentWorkspaceSummary,
			"maintenance_runs_json":          job.MaintenanceRunsJSON,
			"workspace_count":                0, "started_at": job.StartedAt, "completed_at": 0,
			"created_at": job.CreatedAt, "updated_at": job.UpdatedAt,
		}).WithContext(ctx).Execute()
		return err
	}
	err = insert()
	if err != nil {
		existing, loadErr := loadProductFileUsageReportJob(ctx, app, request)
		if loadErr == nil {
			return existing, nil
		}
		// A report is moved on by the screen that asked for it. One no screen
		// has moved for a while — its tab closed, its connection or the
		// server gone — is let go, and this request makes the report anew
		// (2026-10-16: an abandoned report kept the usage screen from loading
		// for good).
		abandoned, abandonErr := app.DB().NewQuery(`DELETE FROM {{leafen_file_usage_report_jobs}}
			WHERE [[lifecycle_key]]={:lifecycleKey} AND [[status]]='running' AND [[updated_at]]<{:before}`).
			WithContext(ctx).Bind(dbx.Params{
			"lifecycleKey": job.LifecycleKey, "before": request.NowMillis - fileUsageReportAbandonedAfter.Milliseconds(),
		}).Execute()
		if abandonErr == nil {
			if count, _ := abandoned.RowsAffected(); count > 0 {
				err = insert()
			}
		}
	}
	if err != nil {
		var activeCount int
		activeErr := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_file_usage_report_jobs}}
			WHERE [[lifecycle_key]]={:lifecycleKey} AND [[status]]='running'`).
			WithContext(ctx).Bind(dbx.Params{"lifecycleKey": job.LifecycleKey}).Row(&activeCount)
		if activeErr == nil && activeCount == 1 {
			return job, refusedUnlessStorage(errFileUsageReportUnavailable, err)
		}
		return job, err
	}
	if err := pruneProductFileUsageReportJobs(ctx, app, job); err != nil {
		return job, err
	}
	return job, nil
}

func pruneProductFileUsageReportJobs(
	ctx context.Context,
	app core.App,
	active storedProductFileUsageReportJob,
) error {
	rows := []struct {
		ID string `db:"id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_file_usage_report_jobs}}
		WHERE [[lifecycle_key]]={:lifecycleKey} AND [[status]]='completed' AND [[id]]!={:activeId}
		ORDER BY [[updated_at]] DESC,[[id]] DESC LIMIT 5
	`).WithContext(ctx).Bind(dbx.Params{
		"lifecycleKey": active.LifecycleKey, "activeId": active.ID,
	}).All(&rows)
	if err != nil {
		return err
	}
	if len(rows) <= fileUsageReportRetainedCompletedPerLifecycle {
		return nil
	}
	for _, row := range rows[fileUsageReportRetainedCompletedPerLifecycle:] {
		if _, err := app.DB().Delete(productFileUsageReportJobsTable, dbx.HashExp{
			"id": row.ID, "status": fileUsageReportStatusCompleted,
		}).WithContext(ctx).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func validateProductFileUsageReportJob(
	job storedProductFileUsageReportJob,
	request authorizedProductFileUsageReportRequest,
) error {
	if job.ID == "" || job.ActorID != request.ActorID || job.RequestID != request.RequestID ||
		job.ReportType != request.ReportType || job.WorkspaceID != request.WorkspaceID ||
		job.OrganizationID != request.OrganizationID ||
		job.MaintenanceLimit != request.MaintenanceLimit ||
		job.LifecycleKey != productFileUsageReportLifecycleKey(request) || job.Revision < 0 ||
		job.WorkspaceCount < 0 || job.StartedAt <= 0 || job.UpdatedAt <= 0 {
		return errFileUsageReportContinuationInvalid
	}
	if _, err := decodeProductFileUsageReportMaintenanceRuns(
		job.MaintenanceRunsJSON, job.MaintenanceLimit,
	); err != nil {
		return err
	}
	validStatus := job.Status == fileUsageReportStatusRunning || job.Status == fileUsageReportStatusCompleted
	validPhase := job.Phase == fileUsageReportPhaseWorkspaceSelect ||
		job.Phase == fileUsageReportPhaseUploads || job.Phase == fileUsageReportPhaseMaintenance ||
		job.Phase == fileUsageReportPhaseCompleted
	if !validStatus || !validPhase ||
		(job.Status == fileUsageReportStatusCompleted) != (job.Phase == fileUsageReportPhaseCompleted) ||
		(job.Status == fileUsageReportStatusCompleted) != (job.CompletedAt > 0) {
		return errFileUsageReportStateInvalid
	}
	return nil
}

func decodeProductFileUsageReportAccumulator(value string) (productFileUsageReportAccumulator, error) {
	accumulator := productFileUsageReportAccumulator{}
	if err := json.Unmarshal([]byte(value), &accumulator); err != nil {
		return accumulator, errors.Join(errFileUsageReportStateInvalid, err)
	}
	if err := normalizeProductFileUsageReportAccumulator(&accumulator); err != nil {
		return accumulator, err
	}
	return accumulator, nil
}

func encodeProductFileUsageReportAccumulator(
	accumulator productFileUsageReportAccumulator,
) (string, error) {
	if err := normalizeProductFileUsageReportAccumulator(&accumulator); err != nil {
		return "", err
	}
	value, err := json.Marshal(accumulator)
	if err != nil {
		return "", errors.Join(errFileUsageReportStateInvalid, err)
	}
	return string(value), nil
}

func persistProductFileUsageReportJob(
	ctx context.Context,
	app core.App,
	before storedProductFileUsageReportJob,
	after storedProductFileUsageReportJob,
	results []storedProductFileUsageReportWorkspaceResult,
) error {
	if len(results)+1 > fileUsageReportTransactionOperationLimit {
		return errFileUsageReportStateInvalid
	}
	return app.RunInTransaction(func(txApp core.App) error {
		for _, result := range results {
			_, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_file_usage_report_workspace_results}}
					([[job_id]],[[workspace_id]],[[name]],[[domain]],[[active_storage_bytes]],[[totals_json]],[[pending_json]])
				VALUES ({:jobId},{:workspaceId},{:name},{:domain},{:activeStorageBytes},{:totalsJson},{:pendingJson})
				ON CONFLICT ([[job_id]],[[workspace_id]]) DO UPDATE SET
					[[name]]=excluded.[[name]],[[domain]]=excluded.[[domain]],
					[[active_storage_bytes]]=excluded.[[active_storage_bytes]],
					[[totals_json]]=excluded.[[totals_json]],[[pending_json]]=excluded.[[pending_json]]
			`).WithContext(ctx).Bind(dbx.Params{
				"jobId": after.ID, "workspaceId": result.WorkspaceID, "name": result.Name,
				"domain": result.Domain, "activeStorageBytes": result.ActiveStorageBytes,
				"totalsJson": result.TotalsJSON, "pendingJson": result.PendingJSON,
			}).Execute()
			if err != nil {
				return err
			}
		}
		query := txApp.DB().NewQuery(`
			UPDATE {{leafen_file_usage_report_jobs}} SET
				[[status]]={:status},[[phase]]={:phase},[[workspace_cursor]]={:workspaceCursor},
				[[current_workspace_id]]={:currentWorkspaceId},
				[[current_workspace_name]]={:currentWorkspaceName},
				[[current_workspace_domain]]={:currentWorkspaceDomain},
				[[upload_cursor]]={:uploadCursor},[[revision]]={:nextRevision},
				[[summary_json]]={:summaryJson},
				[[current_workspace_summary_json]]={:currentSummaryJson},
				[[maintenance_runs_json]]={:maintenanceRunsJson},
				[[workspace_count]]={:workspaceCount},[[completed_at]]={:completedAt},
				[[updated_at]]={:updatedAt}
			WHERE [[id]]={:id} AND [[actor_id]]={:actorId} AND [[status]]={:beforeStatus}
				AND [[phase]]={:beforePhase} AND [[revision]]={:beforeRevision}
		`).WithContext(ctx).Bind(dbx.Params{
			"status": after.Status, "phase": after.Phase,
			"workspaceCursor":        after.WorkspaceCursor,
			"currentWorkspaceId":     after.CurrentWorkspaceID,
			"currentWorkspaceName":   after.CurrentWorkspaceName,
			"currentWorkspaceDomain": after.CurrentWorkspaceDomain,
			"uploadCursor":           after.UploadCursor, "nextRevision": after.Revision,
			"summaryJson": after.SummaryJSON, "currentSummaryJson": after.CurrentWorkspaceSummary,
			"maintenanceRunsJson": after.MaintenanceRunsJSON,
			"workspaceCount":      after.WorkspaceCount, "completedAt": after.CompletedAt,
			"updatedAt": after.UpdatedAt, "id": before.ID, "actorId": before.ActorID,
			"beforeStatus": before.Status, "beforePhase": before.Phase,
			"beforeRevision": before.Revision,
		})
		result, err := query.Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return errors.Join(errFileUsageReportUnavailable, err)
		}
		return nil
	})
}

func encodeProductFileUsageReportResultCursor(bytes int64, workspaceID string) string {
	value := strconv.FormatInt(bytes, 10) + "\n" + workspaceID
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeProductFileUsageReportResultCursor(value string) (int64, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, "", errFileUsageReportContinuationInvalid
	}
	parts := strings.SplitN(string(decoded), "\n", 2)
	if len(parts) != 2 || parts[1] == "" {
		return 0, "", errFileUsageReportContinuationInvalid
	}
	bytes, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || bytes < 0 {
		return 0, "", errFileUsageReportContinuationInvalid
	}
	return bytes, parts[1], nil
}

func readAuthorizedProductFileUsageReportWorkspaceResults(
	ctx context.Context,
	app core.App,
	actorID string,
	jobID string,
	after string,
) (fileUsageReportResponse, error) {
	job := storedProductFileUsageReportJob{}
	err := app.DB().NewQuery(productFileUsageReportSelect + `
		WHERE [[id]]={:jobId} AND [[actor_id]]={:actorId} LIMIT 1
	`).WithContext(ctx).Bind(dbx.Params{"jobId": jobID, "actorId": actorID}).One(&job)
	if errors.Is(err, sql.ErrNoRows) {
		return fileUsageReportResponse{}, permissionDenied()
	}
	if err != nil {
		return fileUsageReportResponse{}, err
	}
	if job.Status != fileUsageReportStatusCompleted || job.Phase != fileUsageReportPhaseCompleted {
		return fileUsageReportResponse{}, errFileUsageReportContinuationInvalid
	}
	accumulator, err := decodeProductFileUsageReportAccumulator(job.SummaryJSON)
	if err != nil {
		return fileUsageReportResponse{}, err
	}
	params := dbx.Params{"jobId": job.ID, "limit": fileUsageReportWorkspaceResultWindow + 1}
	where := "[[job_id]]={:jobId}"
	if after != "" {
		bytes, workspaceID, err := decodeProductFileUsageReportResultCursor(after)
		if err != nil {
			return fileUsageReportResponse{}, err
		}
		params["afterBytes"] = bytes
		params["afterWorkspaceId"] = workspaceID
		where += ` AND ([[active_storage_bytes]]<{:afterBytes} OR
			([[active_storage_bytes]]={:afterBytes} AND [[workspace_id]]<{:afterWorkspaceId}))`
	}
	rows := []storedProductFileUsageReportWorkspaceResult{}
	err = app.DB().NewQuery(`
		SELECT [[workspace_id]],[[name]],[[domain]],[[active_storage_bytes]],[[totals_json]],[[pending_json]]
		FROM {{leafen_file_usage_report_workspace_results}}
		WHERE ` + where + `
		ORDER BY [[active_storage_bytes]] DESC,[[workspace_id]] DESC LIMIT {:limit}
	`).WithContext(ctx).Bind(params).All(&rows)
	if err != nil {
		return fileUsageReportResponse{}, err
	}
	hasMore := len(rows) > fileUsageReportWorkspaceResultWindow
	if hasMore {
		rows = rows[:fileUsageReportWorkspaceResultWindow]
	}
	response, err := productFileUsageReportResponseFrom(job, accumulator)
	if err != nil {
		return fileUsageReportResponse{}, err
	}
	for _, row := range rows {
		result := fileUsageReportWorkspaceResult{
			WorkspaceID: row.WorkspaceID, Name: row.Name, Domain: row.Domain,
			ActiveStorageBytes: row.ActiveStorageBytes,
		}
		if err := json.Unmarshal([]byte(row.TotalsJSON), &result.Totals); err != nil {
			return fileUsageReportResponse{}, errors.Join(errFileUsageReportStateInvalid, err)
		}
		if err := json.Unmarshal([]byte(row.PendingJSON), &result.Pending); err != nil {
			return fileUsageReportResponse{}, errors.Join(errFileUsageReportStateInvalid, err)
		}
		response.ByWorkspace = append(response.ByWorkspace, result)
	}
	response.ByWorkspaceHasMore = hasMore
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		response.ByWorkspaceNextCursor = encodeProductFileUsageReportResultCursor(
			last.ActiveStorageBytes, last.WorkspaceID,
		)
	}
	return response, nil
}

func productFileUsageReportWorkspaceResultStorage(
	jobID string,
	workspace productFileUsageReportWorkspace,
	accumulator productFileUsageReportAccumulator,
) (storedProductFileUsageReportWorkspaceResult, error) {
	totals, err := json.Marshal(accumulator.Totals)
	if err != nil {
		return storedProductFileUsageReportWorkspaceResult{}, err
	}
	pending, err := json.Marshal(accumulator.Pending)
	if err != nil {
		return storedProductFileUsageReportWorkspaceResult{}, err
	}
	return storedProductFileUsageReportWorkspaceResult{
		WorkspaceID: workspace.ID, Name: workspace.Name, Domain: workspace.Domain,
		ActiveStorageBytes: accumulator.Totals.ActiveStorageBytes,
		TotalsJSON:         string(totals), PendingJSON: string(pending),
	}, nil
}
