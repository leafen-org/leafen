package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	errorCodeMCPConnectionInvalid = "LEAFEN_MCP_CONNECTION_INVALID"
	errorCodeMCPConnectionFailed  = "LEAFEN_MCP_CONNECTION_FAILED"
	errorCodeMCPConnectionMissing = "LEAFEN_MCP_CONNECTION_NOT_FOUND"
)

type storedMCPConnection struct {
	ID             string `db:"id"`
	OwnerID        string `db:"owner_id"`
	ClientName     string `db:"client_name"`
	Created        int64  `db:"created"`
	TokenHash      string `db:"token_hash"`
	TokenExpiresAt int64  `db:"token_expires_at"`
	OAuthClientID  string `db:"oauth_client_id"`
	OAuthResource  string `db:"oauth_resource"`
	OAuthScope     string `db:"oauth_scope"`
	AccessScope    string `db:"access_scope"`
	RevokedAt      int64  `db:"revoked_at"`
	LastUsedAt     int64  `db:"last_used_at"`
	// The tunnel address it last came through, "" for none (agent_reach_schema.go).
	ReachedVia string `db:"reached_via"`
	CreatedBy  string `db:"created_by_connection"`
	// What its page turned off and narrowed it to (mcp_token_tools.go):
	// AllPages 0 holds it to PageIDs, none of them reaching no page.
	DisabledTools string `db:"disabled_tools"`
	PageIDs       string `db:"page_ids"`
	AllPages      int64  `db:"all_pages"`
	// Made for all of its person's workspaces (user_key_reach_gained.go).
	AllWorkspaces int64 `db:"all_workspaces"`
	// An agent connection rather than an MCP token (connection_person_rights.go).
	Agent bool `db:"agent_connection"`
	// Its page lists Notion's tools Leafen doesn't have (mcp_notion_only_tools.go).
	NotionOnlyTools int64 `db:"notion_only_tools"`
	// Its agent turned Notion's tools on for its list (mcp_token_notion_tools.go).
	NotionTools int64 `db:"notion_tools"`
}

type storedMCPWorkspace struct {
	WorkspaceID    string `db:"workspace_id"`
	OrganizationID string `db:"organization_id"`
	Name           string `db:"name"`
}

type mcpConnectionRevocationFilter struct {
	ConnectionID string
	ClientKey    string
	OwnerID      string
	WorkspaceID  string
}

type mcpAuthority struct {
	ConnectionID        string
	CredentialHash      string
	CredentialExpiresAt int64
	Resource            string
	// PrincipalID is the connection's person, whose rights and name its work
	// has.
	PrincipalID string
	ClientID    string
	ClientName  string
	// Scope is what it may do: all its person may for an agent connection,
	// what the tools its page leaves on need for an MCP token
	// (connection_person_rights.go) — held to what its workspaces'
	// organizations allow keys.
	Scope              userKeyScope
	Created            int64
	ScopedWorkspaces   []mcpGovernanceWorkspace
	ApprovedWorkspaces []mcpGovernanceWorkspace
	WorkspaceIDs       []string
	// AllWorkspaces marks one made for all of its person's workspaces, those
	// they come to be in later too (user_key_reach_gained.go).
	AllWorkspaces bool
	// Agent marks an agent connection rather than an MCP token
	// (connection_person_rights.go).
	Agent bool
	// The tools its page turned off, and whether its page's "모든 페이지" is
	// off, holding it to the pages chosen there (none chosen: no page) —
	// mcp_token_tools.go: an MCP token's alone, as nothing narrows an agent
	// connection (connection_person_rights.go).
	connectionNarrowing
	// NotionOnlyTools lists Notion's tools Leafen doesn't have (mcp_notion_only_tools.go).
	NotionOnlyTools bool
	// NotionTools lists every notion-* tool, beyond those Notion's MCP
	// lists (mcp_core_tools.go).
	NotionTools bool
	// Clock is its person's, which its tools write times on (person_clock.go).
	Clock *time.Location
	// used keeps the workspaces a call worked in, for its answer
	// (mcp_answer_workspaces.go); nil keeps none.
	used *mcpWorkspacesUsed
	// whole is the connection's authority before a call held it to one of
	// its workspaces (within), whose uploads the call still brings over
	// (mcp_file_attach.go); nil when it wasn't held.
	whole *mcpAuthority
}

func createMCPConnection(event *core.RequestEvent, runtime *mcpCredentialCompletionRuntime) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	var input mcpConnectionInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionInvalid, "Check the MCP connection details.")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.ClientName = strings.TrimSpace(input.ClientName)
	workspaceIDs := uniqueNonemptyStrings(input.WorkspaceIDs)
	// A token, as Notion's, is for one workspace and does what its person
	// may there with the tools its page leaves on (connection_person_rights.go).
	if input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > maxMCPConnectionRequestIDRunes ||
		input.ClientName == "" || utf8.RuneCountInString(input.ClientName) > 255 ||
		len(workspaceIDs) == 0 || !mcpTokenLifetimeKnown(input.ExpiresIn) || agentConnectionRequest(input.RequestID) {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionInvalid, "Check the MCP connection details.")
	}
	if len(workspaceIDs) > 1 {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionInvalid, errMCPTokenOneWorkspace.Error())
	}
	// The address the person gives an agent, which may be elsewhere.
	origin, err := outwardServerOrigin(event.Request)
	endpoint := origin + "/mcp"
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPConnectionFailed, "Couldn't create the MCP connection.", err)
	}
	result := runtime.completeManual(event.App, user.Id, mcpConnectionInput{
		RequestID: input.RequestID, ClientName: input.ClientName, WorkspaceIDs: workspaceIDs, ExpiresIn: input.ExpiresIn,
	}, endpoint)
	if result.err != nil {
		return mcpManualCompletionError(event, result.err)
	}
	// A key a connection's key made ends with that connection.
	key, keyed := userKeyOf(event)
	if err := noteConnectionMaker(event.App, mcpConnectionsTable, result.response.ID, key, keyed); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPConnectionFailed, "Couldn't create the MCP connection.", err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	return event.JSON(result.status, result.response)
}

// mcpManualCompletionError answers why a key wasn't made, for a connection
// made in settings and for one traded from a connection code alike.
func mcpManualCompletionError(event *core.RequestEvent, err error) error {
	var policyDenial userKeyPolicyError
	if errors.As(err, &policyDenial) {
		return apiError(event, http.StatusForbidden, errorCodeUserKeyPolicy, policyDenial.reason)
	}
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if errors.Is(err, errMCPClientNotApproved) {
		return mcpGovernanceManagementError(event, errMCPClientNotApproved, "manual-admission")
	}
	if errors.Is(err, errMCPConnectionRequestIDReused) {
		return apiError(event, http.StatusConflict, errorCodeMCPConnectionRequestIDReused, "This request ID was already used for a different connection request.")
	}
	if errors.Is(err, errMCPConnectionCapacity) {
		return apiError(event, http.StatusConflict, errorCodeMCPConnectionCapacity, "This account reached its MCP connection limit. Disconnect one you no longer use, then try again.")
	}
	if errors.Is(err, errMCPPublicDestinationHTTPSRequired) {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionHTTPSRequired, "Agents connect to a public server address only over HTTPS.")
	}
	return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPConnectionFailed, "Couldn't create the MCP connection.", err)
}

func revokeMCPConnection(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	connectionID := strings.TrimSpace(event.Request.PathValue("id"))
	if connectionID == "" {
		return apiError(event, http.StatusNotFound, errorCodeMCPConnectionMissing, "MCP connection not found.")
	}
	missing := false
	err := event.App.RunInTransaction(func(txApp core.App) error {
		_, found, err := revokeMCPConnectionsInTransaction(
			txApp,
			mcpConnectionRevocationFilter{ConnectionID: connectionID, OwnerID: user.Id},
			time.Now().UnixMilli(),
		)
		if err != nil {
			return err
		}
		if !found {
			missing = true
		}
		return nil
	})
	if missing {
		return apiError(event, http.StatusNotFound, errorCodeMCPConnectionMissing, "MCP connection not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPConnectionFailed, "Couldn't disconnect the MCP connection.", err)
	}
	event.Response.WriteHeader(http.StatusNoContent)
	return nil
}

func revokeMCPConnectionsInTransaction(
	app core.App,
	filter mcpConnectionRevocationFilter,
	now int64,
) (int, bool, error) {
	exact := filter.ConnectionID != "" && filter.ClientKey == "" && filter.WorkspaceID == ""
	governed := filter.ConnectionID == "" && filter.WorkspaceID != ""
	if !app.IsTransactional() || filter.OwnerID == "" || now <= 0 || exact == governed {
		return 0, false, errMCPGovernanceInvalid
	}
	params := dbx.Params{
		"connectionId": filter.ConnectionID,
		"clientKey":    filter.ClientKey,
		"ownerId":      filter.OwnerID,
		"workspaceId":  filter.WorkspaceID,
		"exact":        exact,
	}
	predicate := `
		[[connection]].[[owner_id]] = {:ownerId} AND (
			({:exact} = TRUE AND [[connection]].[[id]] = {:connectionId}) OR
			({:exact} = FALSE AND
				({:clientKey} = '' OR [[connection]].[[id]] = {:clientKey} OR [[connection]].[[oauth_client_id]] = {:clientKey}) AND
				EXISTS (
					SELECT 1 FROM {{leafen_mcp_connection_workspaces}} [[scope]]
					WHERE [[scope]].[[connection_id]] = [[connection]].[[id]]
						AND [[scope]].[[workspace_id]] = {:workspaceId}
				)
			)
		)`
	var matched int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_mcp_connections}} [[connection]]
		WHERE ` + predicate).Bind(params).Row(&matched); err != nil {
		return 0, false, err
	}
	if matched == 0 {
		return 0, false, nil
	}
	params["revokedAt"] = now
	params["retireAfter"] = now + int64(mcpCredentialRetentionMilliseconds)
	revoking := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[connection]].[[id]] FROM {{leafen_mcp_connections}} [[connection]]
		WHERE [[connection]].[[revoked_at]] = 0 AND ` + predicate).Bind(params).Column(&revoking); err != nil {
		return 0, false, err
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_mcp_connections}} AS [[connection]]
		SET [[revoked_at]] = {:revokedAt}, [[retire_after]] = {:retireAfter}
		WHERE [[connection]].[[revoked_at]] = 0 AND ` + predicate).Bind(params).Execute()
	if err != nil {
		return 0, false, err
	}
	// What these connections made ends with them.
	if err := revokeConnectionChildren(app, revoking, now); err != nil {
		return 0, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected < 0 || affected > int64(matched) {
		return 0, false, failedBecause(errMCPGovernanceUnavailable, err)
	}
	if affected > 0 {
		notifyMCPCredentialRegistryAfterCommit(app)
	}
	return int(affected), true, nil
}

func authenticateMCPRequest(app core.App, request *http.Request) (mcpAuthority, bool, error) {
	token := bearerToken(request)
	if token == "" {
		return mcpAuthority{}, false, nil
	}
	connections := []storedMCPConnection{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[client_name]], [[token_hash]], [[token_expires_at]],
			[[oauth_client_id]], [[oauth_resource]], [[oauth_scope]], [[access_scope]], [[revoked_at]], [[created]],
			[[last_used_at]], [[reached_via]], [[created_by_connection]],
			[[disabled_tools]], [[page_ids]], [[all_pages]], [[all_workspaces]], [[notion_only_tools]], [[notion_tools]],
			[[agent_connection]]
		FROM {{leafen_mcp_connections}}
		WHERE [[token_hash]] = {:tokenHash}
			OR ` + partialIndexKey(`[[previous_token_hash]]`, ` = {:tokenHash}`) + ` AND [[previous_token_until]] > {:now}
		LIMIT 2
	`).Bind(dbx.Params{"tokenHash": mcpTokenHash(token), "now": time.Now().UnixMilli()}).All(&connections); err != nil {
		return mcpAuthority{}, false, err
	}
	if len(connections) != 1 || connections[0].RevokedAt != 0 ||
		(connections[0].TokenExpiresAt != 0 && connections[0].TokenExpiresAt <= time.Now().UnixMilli()) {
		return mcpAuthority{}, false, nil
	}
	// An AI connection acts as its owner, so it stops while they can't act.
	if active, err := accountMayAct(app, connections[0].OwnerID); err != nil || !active {
		return mcpAuthority{}, false, err
	}
	// One another connection made holds only while that one does.
	if holds, err := connectionLineageHolds(app, connections[0].CreatedBy); err != nil || !holds {
		return mcpAuthority{}, false, err
	}
	// An OAuth token holds for the endpoint it was asked for.
	resource, err := mcpEndpointResource(request)
	if err != nil {
		return mcpAuthority{}, false, nil
	}
	if connections[0].OAuthResource != "" {
		// One granted at an earlier address holds where it forwards to, and the other way round.
		if !sameServerResource(connections[0].OAuthResource, resource) ||
			connections[0].OAuthScope != mcpOAuthScope {
			return mcpAuthority{}, false, nil
		}
	}
	if connections[0].AllWorkspaces == 1 {
		keepUserKeyReachingAll(app, connections[0].ID, connections[0].OwnerID)
	}
	workspaces := []storedMCPWorkspace{}
	if err := app.DB().NewQuery(`
		SELECT [[scope]].[[workspace_id]], [[workspace]].[[organization]] AS [[organization_id]],
			[[workspace]].[[name]]
		FROM {{leafen_mcp_connection_workspaces}} [[scope]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[scope]].[[workspace_id]]
		WHERE [[scope]].[[connection_id]] = {:connectionId}
		ORDER BY [[scope]].[[workspace_id]]
	`).Bind(dbx.Params{"connectionId": connections[0].ID}).All(&workspaces); err != nil {
		return mcpAuthority{}, false, err
	}
	if len(workspaces) == 0 {
		return mcpAuthority{}, false, nil
	}
	workspaceIDs := make([]string, 0, len(workspaces))
	scopedWorkspaces := make([]mcpGovernanceWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		workspaceIDs = append(workspaceIDs, workspace.WorkspaceID)
		scopedWorkspaces = append(scopedWorkspaces, mcpGovernanceWorkspace{
			ID: workspace.WorkspaceID, OrganizationID: workspace.OrganizationID, Name: workspace.Name,
		})
	}
	if err := authorizeWorkspaceSet(app, connections[0].OwnerID, workspaceIDs, permissionWorkspaceRead); err != nil {
		if isPermissionDenied(err) {
			return mcpAuthority{}, false, nil
		}
		return mcpAuthority{}, false, err
	}
	clientID := connections[0].OAuthClientID
	if clientID == "" {
		clientID = mcpGovernanceManualClientID
	}
	approvedWorkspaces, err := filterMCPClientApprovedWorkspaces(
		app, scopedWorkspaces, clientID, connections[0].OwnerID,
	)
	if err != nil {
		return mcpAuthority{}, false, err
	}
	approvedIDs := make([]string, 0, len(approvedWorkspaces))
	for _, workspace := range approvedWorkspaces {
		approvedIDs = append(approvedIDs, workspace.ID)
	}
	scope, known := parseUserKeyScope(connections[0].AccessScope)
	if !known {
		return mcpAuthority{}, false, nil
	}
	// What the organizations of its workspaces allow keys holds it too.
	capped, err := userKeyPolicyCap(app, approvedIDs)
	if err != nil {
		return mcpAuthority{}, false, err
	}
	scope = min(scope, capped)
	noteMCPConnectionUse(app, connections[0].ID, connections[0].LastUsedAt)
	noteMCPConnectionReach(app, connections[0].ID, connections[0].ReachedVia, mcpReachedVia(request))
	return mcpAuthority{
		Scope: scope, Created: connections[0].Created,
		ConnectionID: connections[0].ID, CredentialHash: connections[0].TokenHash,
		CredentialExpiresAt: connections[0].TokenExpiresAt, Resource: resource,
		// An agent's connection works as its person.
		PrincipalID: connections[0].OwnerID,
		ClientID:    clientID, ClientName: connections[0].ClientName,
		ScopedWorkspaces: scopedWorkspaces, ApprovedWorkspaces: approvedWorkspaces,
		WorkspaceIDs: approvedIDs, AllWorkspaces: connections[0].AllWorkspaces == 1, Agent: connections[0].Agent,
		connectionNarrowing: connectionNarrowingOf(connections[0].Agent, connections[0].DisabledTools, connections[0].PageIDs,
			connections[0].AllPages),
		NotionOnlyTools: connections[0].NotionOnlyTools == 1, NotionTools: connections[0].NotionTools == 1,
		Clock: personLocation(app, connections[0].OwnerID),
	}, true, nil
}

func uniqueNonemptyStrings(values []string) []string {
	result := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func mcpTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// owner is the connection's person.
func (authority mcpAuthority) owner() string {
	return authority.PrincipalID
}
