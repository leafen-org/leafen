package organization

import (
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The managed users' workspace filter (organization_managed_users.go): the
// workspaces the organization's people are in or own, by name, a window at a
// time. The list's answer carries the first window and its next cursor; this
// route searches them by name and reads on from a cursor, so any of them can
// be chosen however many there are, as Notion's filters search theirs.

const (
	organizationManagedUserWorkspacesPath      = "/organizations/{organizationId}/managed-users/workspaces"
	organizationManagedUserWorkspacesPerWindow = 200
)

type organizationManagedUserWorkspaceChoices struct {
	Items []organizationManagedUserWorkspace `json:"items"`
	// Where the next window starts; "" once the last is read.
	NextCursor string `json:"nextCursor,omitempty"`
}

// The workspaces the organization's people are in or own, found from those
// people by index — not from every membership on the server — whose name
// holds the query, from the cursor on. The sets are materialized and joined
// in order (CROSS JOIN keeps SQLite's order), or the planner folds them back
// into a scan of memberships.
var organizationManagedUserWorkspaceChoicesSQL = `
	WITH [[managed]] ([[user_id]]) AS MATERIALIZED (
		SELECT [[user]].[[id]] FROM {{users}} [[user]]
		WHERE ` + organizationManagedUserSQL + ` OR ` + organizationSuspendedUserSQL + `
	), [[link]] ([[workspace_id]]) AS MATERIALIZED (
		SELECT [[membership]].[[workspace_id]] FROM [[managed]]
		CROSS JOIN {{leafen_workspace_memberships}} [[membership]] ON [[membership]].[[user_id]] = [[managed]].[[user_id]]
		UNION
		SELECT [[owned]].[[id]] FROM [[managed]]
		CROSS JOIN {{leafen_workspaces}} [[owned]] ON [[owned]].[[workspace_owner]] = [[managed]].[[user_id]]
	)
	SELECT [[workspace]].[[id]], [[workspace]].[[name]], lower([[workspace]].[[name]]) AS [[sort_name]],
		[[workspace]].[[organization]] <> {:organizationId} AS [[external]]
	FROM [[link]] CROSS JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[link]].[[workspace_id]]
	WHERE [[workspace]].[[deletion_job_id]] = ''
		AND ({:query} = '' OR instr(lower([[workspace]].[[name]]), {:query}) > 0)
		AND ({:afterId} = '' OR lower([[workspace]].[[name]]) > {:afterName}
			OR (lower([[workspace]].[[name]]) = {:afterName} AND [[workspace]].[[id]] > {:afterId}))
	ORDER BY lower([[workspace]].[[name]]), [[workspace]].[[id]] LIMIT {:limit}
`

// loadOrganizationManagedUserWorkspaceChoices is a window of the workspaces
// to filter by whose name holds the query, from the cursor on.
func loadOrganizationManagedUserWorkspaceChoices(app core.App, organizationID, query, cursor string) (organizationManagedUserWorkspaceChoices, error) {
	after, err := decodeManagedUsersCursor(cursor)
	if err != nil {
		return organizationManagedUserWorkspaceChoices{}, err
	}
	choices := organizationManagedUserWorkspaceChoices{Items: []organizationManagedUserWorkspace{}}
	if err := app.DB().NewQuery(organizationManagedUserWorkspaceChoicesSQL).Bind(dbx.Params{
		"organizationId": organizationID, "query": strings.ToLower(strings.TrimSpace(query)),
		"afterName": after.Name, "afterId": after.ID, "limit": organizationManagedUserWorkspacesPerWindow + 1,
	}).All(&choices.Items); err != nil {
		return choices, err
	}
	if len(choices.Items) > organizationManagedUserWorkspacesPerWindow {
		choices.Items = choices.Items[:organizationManagedUserWorkspacesPerWindow]
		last := choices.Items[len(choices.Items)-1]
		choices.NextCursor = encodeManagedUsersCursor(last.SortName, last.ID)
	}
	return choices, nil
}

func listOrganizationManagedUserWorkspaces(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	values := event.Request.URL.Query()
	choices, err := loadOrganizationManagedUserWorkspaceChoices(event.App, authorization.OrganizationID, values.Get("query"), values.Get("cursor"))
	if err == errManagedUsersCursor {
		return leafen.APIError(event, http.StatusBadRequest, leafen.ErrorCodeInvalidInput, "Give the nextCursor of the last list, or none to start.")
	}
	if err != nil {
		return managedUsersError(event, err, "list-workspace-choices")
	}
	return event.JSON(http.StatusOK, choices)
}
