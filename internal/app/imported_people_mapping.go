package app

import (
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const importedPeopleMappingMaxBodyBytes = 32 * 1024

func registerImportedPeopleMappingRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.POST(
		apiRoot+"/workspaces/{workspaceId}/imported-people/scan",
		importedPeopleMappingScanHandler,
	).Bind(apis.BodyLimit(importedPeopleMappingMaxBodyBytes))
	event.Router.POST(
		apiRoot+"/workspaces/{workspaceId}/imported-people/apply",
		importedPeopleMappingApplyHandler(ygo),
	).Bind(apis.BodyLimit(importedPeopleMappingMaxBodyBytes))
}

func importedPeopleMappingScanHandler(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := notionImportedPeopleScanInput{}
	if workspaceID == "" || event.BindBody(&input) != nil {
		return importedPeopleMappingAPIError(event, errImportedPeopleCursorInvalid, "scan-validate")
	}
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, importedPeopleMappingPermission); err != nil {
		return importedPeopleMappingAPIError(event, err, "scan-authorize")
	}
	result, err := scanImportedPeople(event.App, user.Id, workspaceID, input.Cursor, input.PeopleOnly)
	if err != nil {
		return importedPeopleMappingAPIError(event, err, "scan")
	}
	return event.JSON(http.StatusOK, result)
}

func importedPeopleMappingApplyHandler(ygo *pocketBaseYgoPersistence) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		user, ok := authenticatedUser(event)
		if !ok {
			return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
		input := notionImportedPeopleApplyInput{}
		if workspaceID == "" || event.BindBody(&input) != nil || slices.Contains(slices.Collect(maps.Values(input.Mappings)), importedPeopleCleared) {
			// Leaving a person out is a migration plan's choice, not this screen's.
			return importedPeopleMappingAPIError(event, errImportedPeopleMappingFailed, "apply-validate")
		}
		if strings.TrimSpace(input.RequestID) == "" {
			return refuseMissingInput(event, errorCodeImportedPeopleMappingFailed, "requestId", "a new id of yours, so the same request sent again isn't done twice.")
		}
		if len(input.Mappings) == 0 {
			return refuseMissingInput(event, errorCodeImportedPeopleMappingFailed, "mappings", "each imported person's ID with the person here it is.")
		}
		if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, importedPeopleMappingPermission); err != nil {
			return importedPeopleMappingAPIError(event, err, "apply-authorize")
		}
		result, err := applyImportedPeopleMappingTurn(event.App, ygo, user.Id, workspaceID, input)
		if err != nil {
			return importedPeopleMappingAPIError(event, err, "apply")
		}
		return event.JSON(http.StatusOK, result)
	}
}

func importedPeopleMappingAPIError(event *core.RequestEvent, err error, stage string) error {
	status := http.StatusInternalServerError
	code := errorCodeImportedPeopleMappingFailed
	message := "Couldn't map the imported user."
	switch {
	case isPermissionDenied(err):
		status, code = http.StatusForbidden, errorCodePermissionDenied
		message = "You don't have permission to manage imported user mappings."
	case errors.Is(err, errImportedPeopleCursorInvalid):
		status, code = http.StatusConflict, errorCodeImportedPeopleCursorInvalid
		message = "The imported user query position expired. Query again."
	case errors.Is(err, errImportedPeopleMappingBusy):
		status = http.StatusConflict
		message = "Another imported user mapping is running. Try again after it finishes."
	case errors.Is(err, errImportedPeopleMappingFailed):
		status = http.StatusBadRequest
		message = "Check the imported user mapping."
	case isSQLiteBusyError(err):
		status = http.StatusServiceUnavailable
		message = "Retry the same request after the other operation finishes."
	}
	return apiErrorWithCause(event, status, code, message, "imported_people_mapping", stage, code, err)
}
