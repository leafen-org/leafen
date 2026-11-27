package app

import (
	"context"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

func advanceAuthorizedProductFileUsageReport(
	ctx context.Context,
	app core.App,
	request authorizedProductFileUsageReportRequest,
) (productFileUsageReportTurn, error) {
	turn := productFileUsageReportTurn{}
	if err := normalizeAuthorizedProductFileUsageReportRequest(&request); err != nil {
		return turn, err
	}
	job, err := loadProductFileUsageReportJob(ctx, app, request)
	if err != nil && request.JobID == "" && errors.Is(err, errFileUsageReportContinuationInvalid) {
		job, err = createProductFileUsageReportJob(ctx, app, request)
	}
	if err != nil {
		return turn, err
	}
	if err := validateProductFileUsageReportJob(job, request); err != nil {
		return turn, err
	}
	if job.Status == fileUsageReportStatusCompleted {
		response, err := readAuthorizedProductFileUsageReportWorkspaceResults(
			ctx, app, request.ActorID, job.ID, request.ByWorkspaceAfter,
		)
		turn.Response = response
		return turn, err
	}
	if err := pruneProductFileUsageReportJobs(ctx, app, job); err != nil {
		return turn, err
	}
	summary, err := decodeProductFileUsageReportAccumulator(job.SummaryJSON)
	if err != nil {
		return turn, err
	}
	current, err := decodeProductFileUsageReportAccumulator(job.CurrentWorkspaceSummary)
	if err != nil {
		return turn, err
	}
	switch job.Phase {
	case fileUsageReportPhaseWorkspaceSelect:
		turn, err = advanceProductFileUsageReportWorkspaceSelection(
			ctx, app, request, job, summary,
		)
	case fileUsageReportPhaseUploads:
		turn, err = advanceProductFileUsageReportUploads(
			ctx, app, request, job, summary, current,
		)
	case fileUsageReportPhaseMaintenance:
		turn, err = advanceProductFileUsageReportMaintenance(
			ctx, app, request, job, summary, current,
		)
	default:
		err = errFileUsageReportStateInvalid
	}
	return turn, err
}

func advanceProductFileUsageReportWorkspaceSelection(
	ctx context.Context,
	app core.App,
	request authorizedProductFileUsageReportRequest,
	job storedProductFileUsageReportJob,
	summary productFileUsageReportAccumulator,
) (productFileUsageReportTurn, error) {
	turn := productFileUsageReportTurn{}
	if job.ReportType != fileUsageReportTypeOrganization || job.OrganizationID == "" ||
		job.CurrentWorkspaceID != "" || job.UploadCursor != "" {
		return turn, errFileUsageReportStateInvalid
	}
	workspaces, hasMoreWorkspaces, readRows, err := loadProductFileUsageReportWorkspaces(
		ctx, app, job.OrganizationID, job.WorkspaceCursor,
	)
	if err != nil {
		return turn, err
	}
	if readRows > fileUsageReportBatchWorkspaceWindow+1 {
		return turn, errFileUsageReportStateInvalid
	}
	turn.Workspaces = len(workspaces)
	if len(workspaces) == 0 {
		after := job
		after.Phase = fileUsageReportPhaseMaintenance
		after.UpdatedAt = request.NowMillis
		return persistProductFileUsageReportAdvance(ctx, app, job, after, summary, nil, turn)
	}

	windows := make([]productFileUsageReportSourceWindow, len(workspaces))
	firstHeavy := -1
	for index, workspace := range workspaces {
		window, err := loadProductFileUsageReportSourceWindow(
			ctx, app, workspace.ID, "", fileUsageReportBatchUploadWindow, request.NowMillis,
		)
		if err != nil {
			return turn, err
		}
		if window.ReadRows > fileUsageReportBatchUploadWindow+1 {
			return turn, errFileUsageReportStateInvalid
		}
		windows[index] = window
		if firstHeavy < 0 && window.HasMore {
			firstHeavy = index
		}
	}
	compatible := len(workspaces)
	if firstHeavy >= 0 {
		compatible = firstHeavy
	}
	after := job
	after.UpdatedAt = request.NowMillis
	results := make([]storedProductFileUsageReportWorkspaceResult, 0, compatible)
	for index := 0; index < compatible; index++ {
		workspaceSummary, err := addProductFileUsageReportSources(
			emptyProductFileUsageReportAccumulator(), windows[index].Items,
		)
		if err != nil {
			return turn, err
		}
		summary, err = addProductFileUsageReportSources(summary, windows[index].Items)
		if err != nil {
			return turn, err
		}
		stored, err := productFileUsageReportWorkspaceResultStorage(
			job.ID, workspaces[index], workspaceSummary,
		)
		if err != nil {
			return turn, err
		}
		results = append(results, stored)
		after.WorkspaceCount++
		turn.SourceRows += len(windows[index].Items)
	}
	if firstHeavy >= 0 {
		workspace := workspaces[firstHeavy]
		window := windows[firstHeavy]
		current, err := addProductFileUsageReportSources(
			emptyProductFileUsageReportAccumulator(), window.Items,
		)
		if err != nil {
			return turn, err
		}
		summary, err = addProductFileUsageReportSources(summary, window.Items)
		if err != nil {
			return turn, err
		}
		after.Phase = fileUsageReportPhaseUploads
		after.WorkspaceCursor = workspace.ID
		after.CurrentWorkspaceID = workspace.ID
		after.CurrentWorkspaceName = workspace.Name
		after.CurrentWorkspaceDomain = workspace.Domain
		after.UploadCursor = window.Next
		after.CurrentWorkspaceSummary, err = encodeProductFileUsageReportAccumulator(current)
		if err != nil {
			return turn, err
		}
		turn.SourceRows += len(window.Items)
	} else {
		after.WorkspaceCursor = workspaces[len(workspaces)-1].ID
		if !hasMoreWorkspaces {
			after.Phase = fileUsageReportPhaseMaintenance
		}
	}
	return persistProductFileUsageReportAdvance(ctx, app, job, after, summary, results, turn)
}

func advanceProductFileUsageReportUploads(
	ctx context.Context,
	app core.App,
	request authorizedProductFileUsageReportRequest,
	job storedProductFileUsageReportJob,
	summary productFileUsageReportAccumulator,
	current productFileUsageReportAccumulator,
) (productFileUsageReportTurn, error) {
	turn := productFileUsageReportTurn{Workspaces: 1}
	if job.CurrentWorkspaceID == "" {
		return turn, errFileUsageReportStateInvalid
	}
	window, err := loadProductFileUsageReportSourceWindow(
		ctx, app, job.CurrentWorkspaceID, job.UploadCursor,
		fileUsageReportUploadWindow, request.NowMillis,
	)
	if err != nil {
		return turn, err
	}
	if window.ReadRows > fileUsageReportUploadWindow+1 {
		return turn, errFileUsageReportStateInvalid
	}
	summary, err = addProductFileUsageReportSources(summary, window.Items)
	if err != nil {
		return turn, err
	}
	current, err = addProductFileUsageReportSources(current, window.Items)
	if err != nil {
		return turn, err
	}
	after := job
	after.UpdatedAt = request.NowMillis
	after.UploadCursor = window.Next
	results := []storedProductFileUsageReportWorkspaceResult{}
	if !window.HasMore {
		after.UploadCursor = ""
		if job.ReportType == fileUsageReportTypeOrganization {
			stored, err := productFileUsageReportWorkspaceResultStorage(
				job.ID,
				productFileUsageReportWorkspace{
					ID: job.CurrentWorkspaceID, Name: job.CurrentWorkspaceName,
					Domain: job.CurrentWorkspaceDomain,
				},
				current,
			)
			if err != nil {
				return turn, err
			}
			results = append(results, stored)
			after.WorkspaceCount++
			after.Phase = fileUsageReportPhaseWorkspaceSelect
			after.CurrentWorkspaceID = ""
			after.CurrentWorkspaceName = ""
			after.CurrentWorkspaceDomain = ""
			current = emptyProductFileUsageReportAccumulator()
		} else if job.ReportType == fileUsageReportTypeWorkspace {
			after.Phase = fileUsageReportPhaseMaintenance
		} else {
			return turn, errFileUsageReportStateInvalid
		}
	}
	after.CurrentWorkspaceSummary, err = encodeProductFileUsageReportAccumulator(current)
	if err != nil {
		return turn, err
	}
	turn.SourceRows = len(window.Items)
	return persistProductFileUsageReportAdvance(ctx, app, job, after, summary, results, turn)
}

func advanceProductFileUsageReportMaintenance(
	ctx context.Context,
	app core.App,
	request authorizedProductFileUsageReportRequest,
	job storedProductFileUsageReportJob,
	summary productFileUsageReportAccumulator,
	current productFileUsageReportAccumulator,
) (productFileUsageReportTurn, error) {
	turn := productFileUsageReportTurn{}
	if job.UploadCursor != "" {
		return turn, errFileUsageReportStateInvalid
	}
	workspaceID := ""
	organizationID := ""
	if job.ReportType == fileUsageReportTypeWorkspace && job.CurrentWorkspaceID != "" {
		workspaceID = job.CurrentWorkspaceID
		turn.Workspaces = 1
	} else if job.ReportType == fileUsageReportTypeOrganization &&
		job.CurrentWorkspaceID == "" && job.OrganizationID != "" {
		organizationID = job.OrganizationID
	} else {
		return turn, errFileUsageReportStateInvalid
	}
	runs, err := loadRecentProductFileMaintenanceRuns(
		ctx, app, workspaceID, organizationID, job.MaintenanceLimit,
	)
	if err != nil {
		return turn, err
	}
	maintenanceJSON, err := encodeProductFileUsageReportMaintenanceRuns(
		runs, job.MaintenanceLimit,
	)
	if err != nil {
		return turn, err
	}
	after := job
	after.UpdatedAt = request.NowMillis
	after.MaintenanceRunsJSON = maintenanceJSON
	after.Status = fileUsageReportStatusCompleted
	after.Phase = fileUsageReportPhaseCompleted
	after.CompletedAt = request.NowMillis
	after.CurrentWorkspaceID = ""
	after.CurrentWorkspaceName = ""
	after.CurrentWorkspaceDomain = ""
	after.CurrentWorkspaceSummary, _ = encodeProductFileUsageReportAccumulator(
		emptyProductFileUsageReportAccumulator(),
	)
	return persistProductFileUsageReportAdvance(ctx, app, job, after, summary, nil, turn)
}

func persistProductFileUsageReportAdvance(
	ctx context.Context,
	app core.App,
	before storedProductFileUsageReportJob,
	after storedProductFileUsageReportJob,
	summary productFileUsageReportAccumulator,
	results []storedProductFileUsageReportWorkspaceResult,
	turn productFileUsageReportTurn,
) (productFileUsageReportTurn, error) {
	encoded, err := encodeProductFileUsageReportAccumulator(summary)
	if err != nil {
		return turn, err
	}
	after.SummaryJSON = encoded
	after.Revision = before.Revision + 1
	if after.UpdatedAt < before.UpdatedAt {
		after.UpdatedAt = before.UpdatedAt
	}
	if after.CompletedAt > after.UpdatedAt {
		after.UpdatedAt = after.CompletedAt
	}
	if err := persistProductFileUsageReportJob(ctx, app, before, after, results); err != nil {
		return turn, err
	}
	turn.TransactionOperations = len(results) + 1
	turn.Response, err = productFileUsageReportResponseFrom(after, summary)
	if err != nil {
		return turn, err
	}
	if after.Status == fileUsageReportStatusCompleted {
		response, err := readAuthorizedProductFileUsageReportWorkspaceResults(
			ctx, app, after.ActorID, after.ID, "",
		)
		if err != nil {
			return turn, err
		}
		turn.Response = response
	}
	return turn, nil
}
