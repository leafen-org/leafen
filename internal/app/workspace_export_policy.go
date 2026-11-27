package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// workspaceExportPolicyRefusal is what keeps an export in: the
// organization's DLP policy, its sharing security, and a teamspace with
// export turned off for a page of it. The organization's own export through
// its admin API answers to its DLP policy alone.
func workspaceExportPolicyRefusal(
	app core.App, scope workspaceExportScopeAuthorization, organizationExport bool,
) error {
	organizationID := scope.Workspace.Organization.OrganizationID
	if err := enforceOrganizationDLPOutbound(app, organizationID, dlpOutboundExports); err != nil {
		return err
	}
	if organizationExport {
		return nil
	}
	if err := enforceSharingSecurityExports(app, organizationID); err != nil {
		return err
	}
	if scope.PageID != "" {
		return enforceTeamspaceExport(app, scope.PageID)
	}
	return nil
}

// getWorkspaceExportPolicy says whether the person may export a page or the
// workspace, so the export dialog says why not when it opens rather than
// after the choice is made.
func (r *durableJobRunner) getWorkspaceExportPolicy(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	query := event.Request.URL.Query()
	scope := workspaceExportScope{Kind: strings.TrimSpace(query.Get("kind")), TargetID: strings.TrimSpace(query.Get("targetId"))}
	switch scope.Kind {
	case workspaceExportScopeWorkspace:
		if scope.TargetID != "" {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the export details.")
		}
	case workspaceExportScopePage, workspaceExportScopeDatabase:
		if scope.TargetID == "" {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the export details.")
		}
	default:
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the export details.")
	}
	authorization, err := authorizeWorkspaceExportScope(event.App, user.Id, workspaceID, scope)
	if pageID, busy := pageAccessHeldOn(err); busy != "" {
		return answerPageHeld(event, pageID, busy)
	}
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportReadFailed, "Couldn't read the export.", err)
	}
	err = workspaceExportPolicyRefusal(event.App, authorization, false)
	if err != nil && !isDLPPolicyDecisionError(err) && !errors.Is(err, errOrganizationSharingSecurityBlocked) {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportReadFailed, "Couldn't read the export.", err)
	}
	return event.JSON(http.StatusOK, workspaceExportPolicyResponse{Allowed: err == nil})
}
