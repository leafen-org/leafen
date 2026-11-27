package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An MCP token's "콘텐츠 사용 권한" (PUT /mcp/connections/{id}/pages), chosen
// on its page by its person: what an MCP token reaches, as Notion's tokens
// are given their pages. An agent connection has none to choose.

// chooseMCPTokenPages turns its "모든 페이지" on, reaching every page its
// person reads, or narrows it to pages its person reads in its workspaces
// and the pages under them — none chosen, no page (MCP token decision of
// 2026-11-10, 3). Turned on, the pages chosen before are let go.
func chooseMCPTokenPages(event *core.RequestEvent, ownerID string, stored storedMCPToken) error {
	input := struct {
		AllPages bool     `json:"allPages"`
		PageIDs  []string `json:"pageIds"`
	}{}
	if event.BindBody(&input) != nil {
		return errMCPTokenInvalid
	}
	// An agent connection reaches every page its person reads: it has no
	// pages to choose (connection_person_rights.go).
	if stored.Agent {
		return errAgentConnectionNotNarrowed
	}
	if input.AllPages {
		_, err := event.App.DB().Update(mcpConnectionsTable, dbx.Params{"all_pages": 1, "page_ids": "[]"}, dbx.HashExp{"id": stored.ID}).Execute()
		return err
	}
	pageIDs := uniqueNonemptyStrings(input.PageIDs)
	if len(pageIDs) > mcpTokenMostPages {
		return errMCPTokenInvalid
	}
	workspaces := []string{}
	if err := event.App.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_mcp_connection_workspaces}} WHERE [[connection_id]] = {:id}`).
		Bind(dbx.Params{"id": stored.ID}).Column(&workspaces); err != nil {
		return err
	}
	for _, pageID := range pageIDs {
		page, err := authorizePage(event.App, ownerID, pageID, permissionWorkspaceRead)
		if isPermissionDenied(err) || err == nil && !slices.Contains(workspaces, page.Workspace.WorkspaceID) {
			return errMCPTokenInvalid
		}
		if err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return err
	}
	_, err = event.App.DB().Update(mcpConnectionsTable, dbx.Params{"all_pages": 0, "page_ids": string(encoded)}, dbx.HashExp{"id": stored.ID}).Execute()
	return err
}
