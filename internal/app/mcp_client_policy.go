package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An MCP client reaches a workspace only where its organization lets it in.
// The organization module decides that (MCP client approval, a member's MCP
// access); without it every client is let in, as in an organization with
// approval off.

var (
	errMCPGovernanceInvalid     = errors.New(errorCodeMCPGovernanceInvalid)
	errMCPClientNotApproved     = errors.New(errorCodeMCPClientNotApproved)
	errMCPGovernanceUnavailable = errors.New(errorCodeMCPGovernanceUnavailable)
)

type mcpGovernanceWorkspace struct {
	ID             string
	OrganizationID string
	Name           string
}

type mcpGovernanceContext struct {
	ActorID      string
	ClientID     string
	ClientName   string
	ConnectionID string
	Stage        string
}

type storedMCPGovernanceWorkspace struct {
	ID             string `db:"id"`
	OrganizationID string `db:"organization_id"`
	Name           string `db:"name"`
}

// filterMCPClientApprovedWorkspaces keeps the workspaces the client may reach
// for the actor.
func filterMCPClientApprovedWorkspaces(
	app core.App,
	workspaces []mcpGovernanceWorkspace,
	clientID string,
	actorID string,
) ([]mcpGovernanceWorkspace, error) {
	if check := organizationPolicy.MCPClientWorkspaces; check != nil {
		return check(app, workspaces, clientID, actorID)
	}
	return append([]mcpGovernanceWorkspace{}, workspaces...), nil
}

// assertMCPClientApprovedForWorkspaces fails with errMCPClientNotApproved
// when the client may not reach one of the workspaces.
func assertMCPClientApprovedForWorkspaces(
	app core.App,
	workspaces []mcpGovernanceWorkspace,
	context mcpGovernanceContext,
) error {
	if check := organizationPolicy.MCPClientApproved; check != nil {
		return check(app, workspaces, context)
	}
	if len(workspaces) == 0 {
		return errMCPGovernanceInvalid
	}
	_, err := normalizeMCPGovernanceClientID(context.ClientID)
	return err
}

func normalizeMCPGovernanceClientID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > mcpGovernanceMaxClientIDRunes ||
		strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return "", errMCPGovernanceInvalid
	}
	return value, nil
}

func loadMCPGovernanceWorkspaces(
	app core.App,
	workspaceIDs []string,
) ([]mcpGovernanceWorkspace, error) {
	if len(workspaceIDs) == 0 || slices.Contains(workspaceIDs, "") {
		return nil, errMCPGovernanceInvalid
	}
	encoded, err := json.Marshal(workspaceIDs)
	if err != nil {
		return nil, err
	}
	rows := []storedMCPGovernanceWorkspace{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[organization]] AS [[organization_id]], [[name]]
		FROM {{leafen_workspaces}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:workspaceIds}))
		ORDER BY [[id]]
	`).Bind(dbx.Params{"workspaceIds": string(encoded)}).All(&rows); err != nil {
		return nil, errors.Join(errMCPGovernanceUnavailable, err)
	}
	if len(rows) != len(workspaceIDs) {
		return nil, errMCPGovernanceUnavailable
	}
	workspaces := make([]mcpGovernanceWorkspace, 0, len(rows))
	for _, row := range rows {
		if row.ID == "" || row.OrganizationID == "" {
			return nil, errMCPGovernanceUnavailable
		}
		workspaces = append(workspaces, mcpGovernanceWorkspace{
			ID: row.ID, OrganizationID: row.OrganizationID, Name: row.Name,
		})
	}
	return workspaces, nil
}

func mcpGovernanceManagementError(event *core.RequestEvent, err error, stage string) error {
	status := http.StatusServiceUnavailable
	code := errorCodeMCPGovernanceUnavailable
	message := "Couldn't process the MCP approval policy."
	if errors.Is(err, errMCPGovernanceInvalid) {
		status, code, message = http.StatusBadRequest, errorCodeMCPGovernanceInvalid, "Check the MCP approval policy."
	} else if errors.Is(err, errMCPClientNotApproved) {
		status, code, message = http.StatusForbidden, errorCodeMCPClientNotApproved, "This MCP client isn't approved for this workspace."
	} else if isPermissionDenied(err) {
		status, code, message = http.StatusNotFound, errorCodeOrganizationNotFound, "Organization not found."
	} else if isSQLiteBusyError(err) {
		// The server's failure, which the error writer tells busy (api_error_busy.go).
		status = http.StatusInternalServerError
	}
	return apiErrorWithCause(
		event, status, code, message, "enterprise_mcp_governance", stage, code, err,
	)
}
