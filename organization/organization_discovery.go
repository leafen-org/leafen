package organization

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const organizationDiscoveryMaxBodyBytes = 256 * 1024

func registerOrganizationDiscoveryRoutes(event *core.ServeEvent, jobs *leafen.DurableJobRunner) {
	event.Router.GET(leafen.APIRoot+leafen.OrganizationDiscoveryExportListPath, withJobs(jobs, listOrganizationDiscoveryExports))
	event.Router.POST(leafen.APIRoot+leafen.OrganizationDiscoveryExportCreatePath, withJobs(jobs, createOrganizationDiscoveryExport)).
		Bind(apis.BodyLimit(organizationDiscoveryMaxBodyBytes))
	event.Router.GET(leafen.APIRoot+leafen.OrganizationDiscoveryExportGetPath, withJobs(jobs, getOrganizationDiscoveryExport))
	event.Router.GET(leafen.APIRoot+leafen.OrganizationDiscoveryExportDownloadPath, withJobs(jobs, downloadOrganizationDiscoveryExport))
}

// withJobs hands a route the core's job runner.
func withJobs(jobs *leafen.DurableJobRunner, route func(*leafen.DurableJobRunner, *core.RequestEvent) error) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error { return route(jobs, event) }
}

func authorizeOrganizationDiscovery(event *core.RequestEvent) (leafen.OrganizationAuthorization, bool) {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		_ = leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
		return leafen.OrganizationAuthorization{}, false
	}
	authorization, err := leafen.AuthorizeOrganization(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("organizationId")),
		leafen.PermissionOrganizationContentSearch,
	)
	if leafen.IsPermissionDenied(err) {
		_ = leafen.APIError(event, http.StatusNotFound, leafen.ErrorCodeOrganizationNotFound, "Organization not found.")
		return leafen.OrganizationAuthorization{}, false
	}
	if err != nil {
		_ = leafen.APIErrorWithCause(
			event, http.StatusInternalServerError, leafen.ErrorCodeOrganizationAuthorityInvalid,
			"Couldn't check organization permission.", "organization_discovery", "authorization",
			leafen.ErrorCodeOrganizationAuthorityInvalid, err,
		)
		return leafen.OrganizationAuthorization{}, false
	}
	return authorization, true
}

func organizationDiscoveryListLimit(event *core.RequestEvent) (int, error) {
	raw := strings.TrimSpace(event.Request.URL.Query().Get("limit"))
	if raw == "" {
		return leafen.OrganizationDiscoveryExportsPerPage, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > leafen.OrganizationDiscoveryExportsPerPage {
		return 0, errOrganizationDiscoveryInvalid
	}
	return value, nil
}

func organizationDiscoveryError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusServiceUnavailable,
		leafen.ErrorCodeOrganizationDiscoveryUnavailable, "Couldn't process the organization search export."
	switch {
	case errors.Is(err, errOrganizationDiscoveryConflict):
		status, code, message = http.StatusConflict,
			leafen.ErrorCodeOrganizationDiscoveryInvalid, "The same change ID can't be used for a different request."
	case errors.Is(err, errOrganizationDiscoveryInvalid):
		status, code, message = http.StatusBadRequest,
			leafen.ErrorCodeOrganizationDiscoveryInvalid, "Check the search export request."
	case errors.Is(err, errOrganizationDiscoveryNotFound):
		status, code, message = http.StatusNotFound,
			leafen.ErrorCodeOrganizationDiscoveryNotFound, "Search export not found."
	case errors.Is(err, errOrganizationDiscoveryTooLarge):
		status, code, message = http.StatusRequestEntityTooLarge,
			leafen.ErrorCodeOrganizationDiscoveryTooLarge, "Reduce the search export size."
	case errors.Is(err, errOrganizationDiscoverySourceChanged):
		status, code, message = http.StatusConflict,
			leafen.ErrorCodeOrganizationDiscoverySourceChanged, "The source changed during export. Try again."
	case leafen.IsPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			leafen.ErrorCodeOrganizationNotFound, "Organization not found."
	}
	return leafen.APIErrorWithCause(
		event, status, code, message, "organization_discovery", stage, code, err,
	)
}
