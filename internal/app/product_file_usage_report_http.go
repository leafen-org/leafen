package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const (
	fileUsageReportMaxBodyBytes         = 8 * 1024
	productFileUsageReportLockNamespace = "file-usage-report"
)

func registerProductFileUsageReportRoutes(event *core.ServeEvent) {
	event.Router.Route(
		workspaceFileUsageReportMethod,
		apiRoot+workspaceFileUsageReportPath,
		workspaceProductFileUsageReport,
	).Bind(apis.BodyLimit(fileUsageReportMaxBodyBytes))
	event.Router.Route(
		organizationFileUsageReportMethod,
		apiRoot+organizationFileUsageReportPath,
		organizationProductFileUsageReport,
	).Bind(apis.BodyLimit(fileUsageReportMaxBodyBytes))
}

func workspaceProductFileUsageReport(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := fileUsageReportInput{}
	if err := event.BindBody(&input); err != nil || workspaceID == "" {
		return productFileUsageReportAPIError(event, errFileUsageReportInvalid, "validate")
	}
	if strings.TrimSpace(input.RequestID) == "" {
		return refuseMissingInput(event, errorCodeFileUsageReportInvalid, "requestId", "a new id of yours, so the same request sent again isn't done twice.")
	}
	authorization, err := authorizeWorkspace(
		event.App, user.Id, workspaceID, permissionWorkspaceOwner,
	)
	if err != nil {
		return productFileUsageReportAPIError(event, err, "authorize-workspace")
	}
	storageLimit, err := productFileUsageReportStorageLimit(
		event.App, authorization.Organization.OrganizationID,
	)
	if err != nil {
		return productFileUsageReportAPIError(event, err, "storage-policy")
	}
	return executeProductFileUsageReport(event, authorizedProductFileUsageReportRequest{
		ActorID: user.Id, RequestID: input.RequestID,
		ReportType: fileUsageReportTypeWorkspace, WorkspaceID: workspaceID,
		StorageLimitBytes: storageLimit, JobID: input.FileUsageReportJobID,
		ByWorkspaceAfter: input.ByWorkspaceAfter, MaintenanceLimit: input.MaintenanceLimit,
		NowMillis: time.Now().UTC().UnixMilli(),
	})
}

func organizationProductFileUsageReport(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	input := fileUsageReportInput{}
	if err := event.BindBody(&input); err != nil || organizationID == "" {
		return productFileUsageReportAPIError(event, errFileUsageReportInvalid, "validate")
	}
	if strings.TrimSpace(input.RequestID) == "" {
		return refuseMissingInput(event, errorCodeFileUsageReportInvalid, "requestId", "a new id of yours, so the same request sent again isn't done twice.")
	}
	if _, err := authorizeOrganization(
		event.App, user.Id, organizationID, permissionOrganizationBillingAdmin,
	); err != nil {
		return productFileUsageReportAPIError(event, err, "authorize-organization")
	}
	storageLimit, err := productFileUsageReportStorageLimit(event.App, organizationID)
	if err != nil {
		return productFileUsageReportAPIError(event, err, "storage-policy")
	}
	return executeProductFileUsageReport(event, authorizedProductFileUsageReportRequest{
		ActorID: user.Id, RequestID: input.RequestID,
		ReportType: fileUsageReportTypeOrganization, OrganizationID: organizationID,
		StorageLimitBytes: storageLimit, JobID: input.FileUsageReportJobID,
		ByWorkspaceAfter: input.ByWorkspaceAfter, MaintenanceLimit: input.MaintenanceLimit,
		NowMillis: time.Now().UTC().UnixMilli(),
	})
}

func productFileUsageReportStorageLimit(app core.App, organizationID string) (*int64, error) {
	organization, err := app.FindRecordById(organizationsCollection, organizationID)
	if err != nil {
		return nil, err
	}
	return decodeOrganizationStorageLimit(organization.GetString("storage_limit_bytes"))
}

func executeProductFileUsageReport(
	event *core.RequestEvent,
	request authorizedProductFileUsageReportRequest,
) error {
	lockID := request.ActorID + "\x1f" + request.RequestID
	if request.JobID != "" {
		lockID = request.ActorID + "\x1f" + request.JobID
	}
	release := acquireProductFileOperationLock(productFileUsageReportLockNamespace, lockID)
	defer release()
	turn, err := advanceAuthorizedProductFileUsageReport(
		event.Request.Context(), event.App, request,
	)
	if err != nil {
		return productFileUsageReportAPIError(event, err, "advance")
	}
	return event.JSON(http.StatusOK, turn.Response)
}

func productFileUsageReportAPIError(event *core.RequestEvent, err error, stage string) error {
	status := http.StatusInternalServerError
	code := errorCodeFileUsageReportStateInvalid
	causeCode := code
	message := "Couldn't create the file usage report."
	switch {
	case isPermissionDenied(err):
		status, code = http.StatusForbidden, errorCodePermissionDenied
		message = "The file usage report can't be accessed."
	case errors.Is(err, errFileUsageReportInvalid):
		status, code = http.StatusBadRequest, errorCodeFileUsageReportInvalid
		message = "Check the file usage report request."
	case errors.Is(err, errFileUsageReportContinuationInvalid):
		status, code = http.StatusConflict, errorCodeFileUsageReportContinuationInvalid
		message = "The file usage report continuation is invalid."
	case errors.Is(err, errFileUsageReportUnavailable):
		status, code = http.StatusServiceUnavailable, errorCodeFileUsageReportUnavailable
		message = "Try again after the other report job finishes."
	case isSQLiteBusyError(err):
		status, code = http.StatusServiceUnavailable, errorCodeFileUsageReportDatabaseBusy
		message = "Retry the same report after the other server operation finishes."
	case isSQLiteStorageUnavailableError(err):
		status, code = http.StatusServiceUnavailable, errorCodeFileUsageReportUnavailable
		message = "Retry the same report after server storage recovers."
	case errors.Is(err, errFileUsageReportStateInvalid):
		status, code = http.StatusInternalServerError, errorCodeFileUsageReportStateInvalid
	case errors.Is(err, errFileMaintenanceStateInvalid):
		status, code = http.StatusInternalServerError, errorCodeFileUsageReportStateInvalid
		causeCode = errorCodeFileMaintenanceStateInvalid
	}
	if causeCode == errorCodeFileUsageReportStateInvalid {
		causeCode = code
	}
	return apiErrorWithCause(
		event, status, code, message, "file_usage_report", stage, causeCode, err,
	)
}
