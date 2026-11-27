package app

import (
	"context"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var errWorkspacePrincipalDirectoryInvalid = errors.New("invalid workspace principal directory cursor")

type storedWorkspacePrincipalOption struct {
	PrincipalType  string `db:"principal_type"`
	PrincipalID    string `db:"principal_id"`
	Label          string `db:"label"`
	Email          string `db:"email"`
	WorkspaceOwner bool   `db:"workspace_owner"` // shown as such beside their name
	Guest          bool   `db:"guest"`
}

type workspacePrincipalDirectoryAuthority struct {
	PermissionRevision int   `db:"permission_revision"`
	PersonRevision     int64 `db:"person_revision"`
	GroupRevision      int   `db:"group_revision"`
}

func splitWorkspacePrincipalDirectoryAfterID(id string) (
	includeGroups int, groupAfter, userAfter string, err error,
) {
	if id == "" {
		return 1, "", "", nil
	}
	typeAndID := strings.SplitN(id, ":", 2)
	if len(typeAndID) != 2 || typeAndID[1] == "" {
		return 0, "", "", errWorkspacePrincipalDirectoryInvalid
	}
	switch typeAndID[0] {
	case "group":
		return 1, typeAndID[1], "", nil
	case "user":
		return 0, "", typeAndID[1], nil
	default:
		return 0, "", "", errWorkspacePrincipalDirectoryInvalid
	}
}

func readWorkspacePrincipalDirectoryAuthority(
	ctx context.Context, app core.App, workspaceID string,
) (workspacePrincipalDirectoryAuthority, error) {
	authority := workspacePrincipalDirectoryAuthority{}
	err := app.DB().NewQuery(`
		SELECT COALESCE([[permission]].[[cursor]],0) AS [[permission_revision]],
			[[frontier]].[[person_revision]],[[groups]].[[revision]] AS [[group_revision]]
		FROM {{leafen_workspaces}} [[workspace]]
		JOIN {{leafen_form_option_frontiers}} [[frontier]]
			ON [[frontier]].[[workspace_id]]=[[workspace]].[[id]]
		JOIN {{leafen_workspace_permission_group_revisions}} [[groups]]
			ON [[groups]].[[workspace_id]]=[[workspace]].[[id]]
		LEFT JOIN {{leafen_workspace_permission_state}} [[permission]]
			ON [[permission]].[[workspace_id]]=[[workspace]].[[id]]
		WHERE [[workspace]].[[id]]={:workspaceId}
	`).WithContext(ctx).Bind(dbx.Params{"workspaceId": workspaceID}).One(&authority)
	return authority, err
}

// A restricted reader sees only the people they share a place with and no
// groups (restricted_member_people.go).
func loadWorkspacePrincipalOptions(
	ctx context.Context, app core.App, workspaceID, readerID, afterID string, limit int,
) ([]storedWorkspacePrincipalOption, error) {
	return loadWorkspacePrincipalDirectory(ctx, app, workspaceID, readerID, afterID, limit, false)
}

// workspaceGuestsSQL is the guests a share window offers beside the workspace's
// people, as Notion's suggestions do (seen 2026-11-05): whoever a page here is
// shared with and isn't a member, by their account or an address they proved.
// Someone with no share here any more isn't offered. An app's account isn't a
// person; a page connects to it from its own menu.
func workspaceGuestsSQL(peopleFilter string) string {
	columns := `SELECT 'user', [[user]].[[id]],
			COALESCE(NULLIF(trim([[user]].[[name]]),''), NULLIF(trim([[user]].[[email]]),''), [[user]].[[id]]),
			COALESCE(trim([[user]].[[email]]),''), 0, 1
		FROM {{leafen_page_permissions}} [[share]]
		`
	people := `
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]=[[share]].[[workspace_id]]
		JOIN {{leafen_organization_memberships}} [[organization_member]] ON [[organization_member]].[[user]]=[[user]].[[id]]
			AND [[organization_member]].[[organization]]=[[workspace]].[[organization]] AND [[organization_member]].[[status]]='active'
		WHERE {:includeGuests}=1 AND [[share]].[[workspace_id]]={:workspaceId} AND [[user]].[[id]]>{:userAfter}
			AND [[user]].[[id]]!=[[workspace]].[[workspace_owner]]
			AND NOT EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[member]]
				WHERE [[member]].[[workspace_id]]=[[share]].[[workspace_id]] AND [[member]].[[user_id]]=[[user]].[[id]])
			AND NOT EXISTS (SELECT 1 FROM {{leafen_integration_accounts}} [[app_account]] WHERE [[app_account]].[[user_id]]=[[user]].[[id]])` + peopleFilter
	return columns + `JOIN {{users}} [[user]] ON [[share]].[[principal_type]]='user' AND [[user]].[[id]]=[[share]].[[principal_id]]` + people + `
		UNION
		` + columns + `JOIN {{leafen_account_emails}} [[identity]] ON [[share]].[[principal_type]]='email'
			AND [[identity]].[[email]]=lower(trim([[share]].[[principal_id]])) AND ` + provenAccountEmailSQL("[[identity]].") + `
		JOIN {{users}} [[user]] ON [[user]].[[id]]=[[identity]].[[user_id]]` + people
}

func loadWorkspacePrincipalDirectory(
	ctx context.Context, app core.App, workspaceID, readerID, afterID string, limit int, includeGuests bool,
) ([]storedWorkspacePrincipalOption, error) {
	includeGroups, groupAfter, userAfter, err := splitWorkspacePrincipalDirectoryAfterID(afterID)
	if err != nil {
		return nil, err
	}
	restricted, err := workspaceMemberRestricted(app, workspaceID, readerID)
	if err != nil {
		return nil, err
	}
	// No one being deleted is offered a place (account_new_places.go).
	peopleFilter := ` AND ` + accountTakesNewPlacesSQL("[[user]].[[id]]")
	if restricted {
		includeGroups = 0
		peopleFilter += ` AND [[user]].[[id]] IN (` + restrictedVisiblePeopleSQL() + `)`
	}
	rows := []storedWorkspacePrincipalOption{}
	err = app.DB().NewQuery(`
		SELECT 'group' AS [[principal_type]], [[group]].[[id]] AS [[principal_id]],
			[[group]].[[name]] AS [[label]], '' AS [[email]], 0 AS [[workspace_owner]], 0 AS [[guest]]
		FROM {{leafen_workspace_permission_groups}} [[group]]
			INDEXED BY [[idx_leafen_permission_groups_workspace_list]]
		WHERE {:includeGroups}=1 AND [[group]].[[workspace_id]]={:workspaceId}
			AND [[group]].[[deleted_at]] IS NULL AND [[group]].[[id]]>{:groupAfter}
		UNION ALL
		SELECT 'user', [[user]].[[id]],
			COALESCE(NULLIF(trim([[user]].[[name]]),''), NULLIF(trim([[user]].[[email]]),''), [[user]].[[id]]),
			COALESCE(trim([[user]].[[email]]),''), 1, 0
		FROM {{leafen_workspaces}} [[workspace]]
		JOIN {{users}} [[user]] ON [[user]].[[id]]=[[workspace]].[[workspace_owner]]
		WHERE [[workspace]].[[id]]={:workspaceId} AND [[user]].[[id]]>{:userAfter}
			AND ` + accountTakesNewPlacesSQL("[[user]].[[id]]") + `
		UNION ALL
		SELECT 'user', [[user]].[[id]],
			COALESCE(NULLIF(trim([[user]].[[name]]),''), NULLIF(trim([[user]].[[email]]),''), [[user]].[[id]]),
			COALESCE(trim([[user]].[[email]]),''), 0, 0
		FROM {{leafen_workspace_memberships}} [[membership]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]=[[membership]].[[workspace_id]]
		JOIN {{users}} [[user]] ON [[user]].[[id]]=[[membership]].[[user_id]]
		WHERE [[membership]].[[workspace_id]]={:workspaceId}
			AND [[membership]].[[user_id]]!=[[workspace]].[[workspace_owner]]
			AND [[user]].[[id]]>{:userAfter}` + peopleFilter + `
		UNION
		` + workspaceGuestsSQL(peopleFilter) + `
		ORDER BY [[principal_type]],[[principal_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "includeGroups": includeGroups, "includeGuests": includeGuests, "readerId": readerID,
		"groupAfter": groupAfter, "userAfter": userAfter, "limit": limit,
	}).All(&rows)
	return rows, err
}
