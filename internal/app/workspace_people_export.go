package app

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's members and guests as a CSV file, as Notion's 설정 → 일반 →
// 내보내기 offers to its owner ("Export members as CSV": email, name, role,
// permission groups and user ID; Notion's help, read 2026-10-12). Leafen
// keeps one name, so the file has one name column. Its owner, then its
// members, then its guests — the people outside it whom its pages are
// shared with, an address no account has verified having no name or ID.
// A value a spreadsheet would read as a formula is written as text.

type storedWorkspacePerson struct {
	UserID string `db:"user_id"`
	Email  string `db:"email"`
	Name   string `db:"name"`
	Role   string `db:"role"`
	Groups string `db:"groups"`
}

var workspacePeopleExportRoles = map[string]string{
	"owner": "Workspace owner", "member": "Member", "guest": "Guest",
}

func exportWorkspacePeople(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength || len(event.Request.URL.Query()) > 0 {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMemberListInvalid, "Check the member query.")
	}
	workspace, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner)
	if err != nil {
		if isPermissionDenied(err) {
			return workspaceOwnersOnly(event, user.Id, workspaceID, "Only the workspace's owners export its members.", workspaceNotFound(event))
		}
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceMemberListFailed,
			"Couldn't load the workspace members.", "workspace_people_export", "authorize", errorCodeWorkspaceMemberListFailed, err)
	}
	people, err := loadWorkspacePeopleForExport(event.App, workspaceID)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceMemberListFailed,
			"Couldn't load the workspace members.", "workspace_people_export", "read", errorCodeWorkspaceMemberListFailed, err)
	}
	name := strings.TrimSpace(workspace.Workspace.GetString("name"))
	if name == "" {
		name = "Workspace"
	}
	header := event.Response.Header()
	header.Set("Content-Type", "text/csv; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("Pragma", "no-cache")
	header.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="members.csv"; filename*=UTF-8''%s`,
		url.PathEscape(name+" members.csv")))
	event.Response.WriteHeader(http.StatusOK)
	// A byte order mark, so a spreadsheet reads the names as UTF-8.
	if _, err := event.Response.Write([]byte{0xef, 0xbb, 0xbf}); err != nil {
		return err
	}
	writer := csv.NewWriter(event.Response)
	if err := writer.Write([]string{"Email", "Name", "Role", "Permission groups", "User ID"}); err != nil {
		return err
	}
	for _, person := range people {
		if err := writer.Write([]string{
			csvText(person.Email), csvText(person.Name), workspacePeopleExportRoles[person.Role],
			csvText(person.Groups), person.UserID,
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func loadWorkspacePeopleForExport(app core.App, workspaceID string) ([]storedWorkspacePerson, error) {
	people := []storedWorkspacePerson{}
	err := app.DB().NewQuery(workspaceGuestSharesSQL + `,
		[[groups]] AS (
			SELECT [[member]].[[user_id]], group_concat([[member]].[[name]], ', ') AS [[names]] FROM (
				SELECT [[group_member]].[[user_id]], [[group]].[[name]]
				FROM {{leafen_workspace_permission_group_members}} [[group_member]]
				JOIN {{leafen_workspace_permission_groups}} [[group]] ON [[group]].[[id]]=[[group_member]].[[group_id]]
				WHERE [[group]].[[workspace_id]]={:workspaceId} AND [[group]].[[deleted_at]] IS NULL
				ORDER BY [[group_member]].[[user_id]], [[group]].[[normalized_name]], [[group]].[[id]]
			) AS [[member]] GROUP BY [[member]].[[user_id]]
		)
		SELECT [[user_id]], [[email]], [[name]], [[role]], [[groups]] FROM (
			SELECT [[user]].[[id]] AS [[user_id]], [[user]].[[email]], trim([[user]].[[name]]) AS [[name]], 'owner' AS [[role]],
				COALESCE([[groups]].[[names]], '') AS [[groups]], 0 AS [[rank]]
			FROM {{leafen_workspaces}} [[workspace]]
			JOIN {{users}} [[user]] ON [[user]].[[id]]=[[workspace]].[[workspace_owner]]
			LEFT JOIN [[groups]] ON [[groups]].[[user_id]]=[[user]].[[id]]
			WHERE [[workspace]].[[id]]={:workspaceId}
			UNION ALL
			SELECT [[user]].[[id]], [[user]].[[email]], trim([[user]].[[name]]), [[membership]].[[role]],
				COALESCE([[groups]].[[names]], ''), 1
			FROM {{leafen_workspace_memberships}} [[membership]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]=[[membership]].[[workspace_id]]
			JOIN {{users}} [[user]] ON [[user]].[[id]]=[[membership]].[[user_id]]
			LEFT JOIN [[groups]] ON [[groups]].[[user_id]]=[[user]].[[id]]
			WHERE [[membership]].[[workspace_id]]={:workspaceId}
				AND [[membership]].[[user_id]] != [[workspace]].[[workspace_owner]]
			UNION ALL
			SELECT COALESCE(MAX([[guest_share]].[[account_id]]), ''),
				COALESCE(MAX([[user]].[[email]]), MAX([[guest_share]].[[principal_id]])),
				COALESCE(trim(MAX([[user]].[[name]])), ''), 'guest', '', 2
			FROM [[guest_share]]
			LEFT JOIN {{users}} [[user]] ON [[user]].[[id]]=[[guest_share]].[[account_id]]
			GROUP BY [[guest_share]].[[guest_key]]
		) ORDER BY [[rank]], lower([[email]]), [[user_id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID}).All(&people)
	return people, err
}
