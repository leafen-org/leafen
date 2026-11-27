package app

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	errorCodeMCPAuthorityMissing  = "LEAFEN_MCP_AUTHORITY_MISSING"
	errorCodeMCPWorkspaceRequired = "LEAFEN_MCP_WORKSPACE_REQUIRED"
	errorCodeMCPScopeDenied       = "LEAFEN_MCP_SCOPE_DENIED"
	errorCodeMCPToolInvalid       = "LEAFEN_MCP_TOOL_INVALID"
	errorCodeMCPToolFailed        = "LEAFEN_MCP_TOOL_FAILED"
)

var (
	errMCPAuthorityMissing  = errors.New(errorCodeMCPAuthorityMissing)
	errMCPWorkspaceRequired = errors.New(errorCodeMCPWorkspaceRequired)
	errMCPScopeDenied       = errors.New(errorCodeMCPScopeDenied)
	errMCPToolInvalid       = errors.New(errorCodeMCPToolInvalid)
	errMCPToolFailed        = errors.New(errorCodeMCPToolFailed)
)

// mcpCallSeed names one tool call for the IDs its change takes. Every call is
// a change of its own, as Notion's tools are: a progress token restarts with
// each client, and the server keeps no session to tell a resend from a new
// call.
func mcpCallSeed() string {
	return security.RandomString(32)
}

type mcpWorkspaceInput struct {
	WorkspaceID   string
	TeamspaceID   string
	AllowImplicit bool
}

type mcpToolSurface uint8

const (
	mcpToolSurfaceStrict mcpToolSurface = iota
	mcpToolSurfaceNative
)

type mcpWorkspaceListInput struct{}

type mcpWorkspaceListOutput struct {
	Workspaces []mcpWorkspaceListItem `json:"workspaces"`
	// How a call that needs a workspace finds one (mcpWorkspaceNeeded).
	Note string `json:"note"`
}

// mcpWorkspaceListNote is what leafen_list_workspaces says of the list.
const mcpWorkspaceListNote = "An agent connection reaches all of its person's workspaces, an MCP token its one, as Notion's. " +
	"Where the connection reaches several, a call that makes something with no page, database or block to put it in — or " +
	"otherwise names nothing in one workspace — takes workspace_id; a call on a page, database or block works in that one's " +
	"workspace, and a search covers them all."

func registerMCPTools(server *mcp.Server, app core.App, surface mcpToolSurface) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
	if surface == mcpToolSurfaceNative {
		mcp.AddTool(server, &mcp.Tool{
			Name: "leafen_list_workspaces", Title: "List Leafen workspaces",
			Description: "List the workspaces this MCP connection reaches, with when each was made (workspaces of the same name differ by it and by id); " +
				"note says when a call takes workspace_id.",
			Annotations: readOnly,
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpWorkspaceListInput) (*mcp.CallToolResult, mcpWorkspaceListOutput, error) {
			authority, err := mcpAuthorityFromContext(ctx)
			if err != nil {
				return nil, mcpWorkspaceListOutput{}, err
			}
			workspaces, err := listMCPWorkspaces(app, authority)
			if err != nil {
				return nil, mcpWorkspaceListOutput{}, failedBecause(errMCPToolFailed, err)
			}
			return nil, mcpWorkspaceListOutput{Workspaces: workspaces, Note: mcpWorkspaceListNote}, nil
		})
	}

	if surface == mcpToolSurfaceNative {
		registerMCPOperationTools(server, app, readOnly)
		registerMCPAppTools(server, app)
		registerMCPWorkdayTool(server, readOnly)
		registerMCPServiceTools(server, app, readOnly)
		registerMCPRemoteAccessTool(server, app)
		registerMCPServerMoveTool(server, app)
		registerMCPWorkspaceMoveTool(server, app)
		registerMCPStandbyTool(server, app)
		registerMCPServerVersionTool(server, app, readOnly)
		registerMCPBugReportTool(server, app)
		registerMCPCatalogTool(server, app, readOnly)
		registerMCPConnectionRequestsTool(server, app)
		registerMCPSignInRequestsTool(server, app)
	}
	registerMCPToolAccess(server, app, readOnly)
	registerMCPSearchTools(server, app, readOnly, surface)
	registerMCPPageMarkdownSpec(server)
	registerMCPPageTools(server, app, readOnly, surface)
	registerMCPPageCreateTool(server, app, surface)
	registerMCPPageMoveTool(server, app, surface)
	registerMCPDatabaseCreateTool(server, app)
	registerMCPDataSourceUpdateTool(server, app)
	registerMCPViewTools(server, app)
	if surface == mcpToolSurfaceNative {
		registerMCPViewDeleteTool(server, app)
		registerMCPDatabasePageLayoutTool(server, app)
	}
	registerMCPDataSourceQueryTool(server, app, readOnly)
	registerMCPDataSourceQueryMultipleTool(server, app, readOnly)
	registerMCPDirectoryTools(server, app, readOnly)
	registerMCPPeopleTools(server, app, readOnly)
	registerMCPPageListTools(server, app, readOnly)
	registerMCPSidebarListTools(server, app, readOnly)
	registerMCPSidebarTabTools(server, app, readOnly)
	registerMCPVersionTools(server, app, readOnly)
	registerMCPSuggestedEditTools(server, app, readOnly)
	registerMCPCommentTools(server, app, readOnly)
	registerMCPCommentEditTools(server, app)
	registerMCPFileTools(server, app, readOnly)
	registerMCPFileDownloadURLsTool(server, app, readOnly)
	if surface == mcpToolSurfaceNative {
		registerMCPFileVersionTools(server, app, readOnly)
		registerMCPNotionMigrationTools(server, app, readOnly)
		registerMCPNotionMigrationRunTools(server, app, readOnly)
		registerMCPNotionMigrationFormatsTool(server, app)
		registerMCPNotionMigrationVerifyTool(server, app)
		registerMCPNotionMigrationGuide(server, app, readOnly)
		registerMCPInboxTools(server, app, readOnly)
		registerMCPSendTool(server, app)
	}
	registerMCPAsyncTaskTool(server, app, readOnly)
	registerMCPSetupGuideTools(server, app, readOnly, surface)
	registerMCPNotionTrashTools(server, app)
	registerApprovedUnsupportedNotionMCPTools(server)
	registerUnsupportedNotionMeetingNotesMCPTool(server)
}

func mcpAuthorityFromContext(ctx context.Context) (mcpAuthority, error) {
	authority, ok := ctx.Value(mcpAuthorityContextKey{}).(mcpAuthority)
	if !ok || authority.ConnectionID == "" || authority.Resource == "" || authority.PrincipalID == "" || authority.ClientID == "" ||
		len(authority.ScopedWorkspaces) == 0 {
		return mcpAuthority{}, errMCPAuthorityMissing
	}
	return authority, nil
}

func selectMCPWorkspace(
	app core.App,
	authority mcpAuthority,
	input mcpWorkspaceInput,
	requirement permissionRequirement,
) (workspaceAuthorization, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	teamspaceID, err := mcpTeamspaceWorkspaceID(app, authority, strings.TrimSpace(input.TeamspaceID))
	if err != nil {
		return workspaceAuthorization{}, failedBecause(errMCPToolFailed, err)
	}
	if workspaceID != "" && teamspaceID != "" && workspaceID != teamspaceID {
		return workspaceAuthorization{}, mcpInvalid("workspace_id and teamspace_id name different workspaces: give one")
	}
	if workspaceID == "" {
		workspaceID = teamspaceID
	}
	// One named nowhere is the connection's one workspace; where it reaches
	// several, the call names one (connection_person_rights.go).
	if workspaceID == "" && input.AllowImplicit && len(authority.WorkspaceIDs) == 1 {
		workspaceID = authority.WorkspaceIDs[0]
	}
	if workspaceID == "" {
		return workspaceAuthorization{}, mcpWorkspaceNeeded(authority, "")
	}
	var scopedWorkspace *mcpGovernanceWorkspace
	for index := range authority.ScopedWorkspaces {
		if authority.ScopedWorkspaces[index].ID == workspaceID {
			scopedWorkspace = &authority.ScopedWorkspaces[index]
			break
		}
	}
	if scopedWorkspace == nil {
		// One the call's workspace_id held it from (mcp_answer_workspaces.go).
		if authority.whole != nil && slices.Contains(authority.whole.WorkspaceIDs, workspaceID) && len(authority.WorkspaceIDs) == 1 {
			authority.used.miss(workspaceID)
			return workspaceAuthorization{}, mcpInvalid("%s", strings.TrimPrefix(
				mcpHeldWorkspaceText(*authority.whole, authority.WorkspaceIDs[0], workspaceID), errorCodeMCPToolInvalid+": "))
		}
		return workspaceAuthorization{}, mcpDenied("workspace %s isn't one this connection was given: %s", workspaceID, mcpWorkspacesListedOn(authority))
	}
	err = assertMCPClientApprovedForWorkspaces(app, []mcpGovernanceWorkspace{*scopedWorkspace}, mcpGovernanceContext{
		ActorID: authority.PrincipalID, ClientID: authority.ClientID, ClientName: authority.ClientName,
		ConnectionID: authority.ConnectionID, Stage: "hosted-tool-call",
	})
	if errors.Is(err, errMCPClientNotApproved) {
		return workspaceAuthorization{}, errMCPClientNotApproved
	}
	if err != nil {
		return workspaceAuthorization{}, failedBecause(errMCPToolFailed, err)
	}
	if !slices.Contains(authority.WorkspaceIDs, workspaceID) {
		return workspaceAuthorization{}, mcpDenied("workspace %s isn't one this connection was given: %s", workspaceID, mcpWorkspacesListedOn(authority))
	}
	authorization, err := authorizeWorkspace(app, authority.PrincipalID, workspaceID, requirement)
	if err != nil {
		if isPermissionDenied(err) {
			return workspaceAuthorization{}, mcpDenied("this account doesn't have the access this needs in that workspace")
		}
		return workspaceAuthorization{}, failedBecause(errMCPToolFailed, err)
	}
	authority.used.note(*scopedWorkspace)
	return authorization, nil
}
