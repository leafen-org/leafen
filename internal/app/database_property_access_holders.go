package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Who has full access to a database, as its property access window's fixed
// first exception — "전체 허용 권한이 있는 모든 사용자" — names them in the
// dark tip over its name, as Notion's (seen 2026-11-19 in Chrome at 1440,
// dpr 2: a private database named its owner; one at a teamspace's top named
// "모든 워크스페이스 멤버", "팀스페이스 멤버" and "팀스페이스 소유자", and the
// first was gone once its general access gave less than full access; one
// shared with a second person at full access named that person, then the one
// a page above gave it). It names how full access comes to people, not
// everyone it reaches, by the same sources the access decision reads
// (page_access_policy.go): a private page's owner; each share at full access
// on the database's page and on the pages above whose sharing it follows — a
// person, an address, a group, an app, everyone in the workspace — its own
// first, then the nearest page's; then its teamspace's roles at full access:
// the rest of the workspace, its members, its owners. At most
// databasePropertyAccessHoldersMax are named; More counts the rest.

const (
	databasePropertyAccessHoldersMax = 20

	propertyAccessHolderPerson           = "person"
	propertyAccessHolderGroup            = "group"
	propertyAccessHolderApp              = "app"
	propertyAccessHolderWorkspace        = "workspace"
	propertyAccessHolderTeamspaceMembers = "teamspace_members"
	propertyAccessHolderTeamspaceOwners  = "teamspace_owners"
)

// DatabasePropertyAccessHolder is one way full access to a database comes:
// Kind is one of the above. ID is the person's or app's account ("" for an
// address no account has proved yet) or the group's; Name is
// theirs, as the share window names them.
type DatabasePropertyAccessHolder struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// DatabasePropertyAccessHolders are those named, and how many more there are.
type DatabasePropertyAccessHolders struct {
	Holders []DatabasePropertyAccessHolder `json:"holders"`
	More    int                            `json:"more"`
}

type propertyAccessHolderList struct {
	DatabasePropertyAccessHolders
	seen map[string]bool
}

func (list *propertyAccessHolderList) add(holder DatabasePropertyAccessHolder) {
	key := holder.Kind + "\x00" + holder.ID
	if holder.ID == "" {
		key += "\x00" + holder.Name
	}
	if list.seen[key] {
		return
	}
	list.seen[key] = true
	if len(list.Holders) >= databasePropertyAccessHoldersMax {
		list.More++
		return
	}
	list.Holders = append(list.Holders, holder)
}

// loadDatabasePropertyAccessHolders names who has full access to the
// database page, for someone with full access to it.
func loadDatabasePropertyAccessHolders(app core.App, actorID, pageID string) (*DatabasePropertyAccessHolders, error) {
	authorization, err := authorizePageAccessRole(app, actorID, pageID, pageAccessRoleFullAccess)
	if err != nil {
		return nil, err
	}
	ctx, page := context.Background(), authorization.Page
	workspaceID := page.GetString("workspace")
	list := &propertyAccessHolderList{DatabasePropertyAccessHolders: DatabasePropertyAccessHolders{
		Holders: []DatabasePropertyAccessHolder{}}, seen: map[string]bool{}}
	chain, err := loadPageAccessAncestorIDs(app, workspaceID, page.Id)
	if err != nil {
		return nil, err
	}
	// A private page's owner, as its share window names them first.
	holderID, private, err := publicMutationFullAccessHolder(ctx, app, authorization, chain)
	if err != nil {
		return nil, err
	}
	if private {
		owner, err := loadPublicMutationAccessOwner(ctx, app, holderID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			list.add(DatabasePropertyAccessHolder{Kind: propertyAccessHolderPerson, ID: owner.PrincipalID, Name: owner.Label})
		}
	}
	if err := addPropertyAccessShareHolders(ctx, app, workspaceID, chain, list); err != nil {
		return nil, err
	}
	if teamspaceID := page.GetString("teamspace_id"); teamspaceID != "" {
		if err := addPropertyAccessTeamspaceHolders(app, workspaceID, teamspaceID, page.Id, list); err != nil {
			return nil, err
		}
	}
	return &list.DatabasePropertyAccessHolders, nil
}

// addPropertyAccessShareHolders adds the shares at full access on the pages
// of the chain, each principal once where it first comes: the page's own,
// then the nearest page's above, each page's in the order they were made.
func addPropertyAccessShareHolders(ctx context.Context, app core.App, workspaceID string, chain []string, list *propertyAccessHolderList) error {
	if len(chain) == 0 {
		return nil
	}
	encoded, err := json.Marshal(chain)
	if err != nil {
		return err
	}
	rows := []struct {
		storedInheritedShare
		AppName string `db:"app_name"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[permission]].[[principal_type]],[[permission]].[[principal_id]],
			` + publicMutationPrincipalColumnsSQL + `,
			COALESCE((SELECT NULLIF(trim([[app]].[[name]]),'') FROM {{users}} [[app]]
				WHERE [[permission]].[[principal_type]]='integration' AND [[app]].[[id]]=[[permission]].[[principal_id]]), '') AS [[app_name]],
			COUNT(*) OVER () AS [[total]]
		FROM {{leafen_page_permissions}} [[permission]]
		JOIN json_each({:chain}) [[step]] ON [[step]].[[value]]=[[permission]].[[page_id]]
		` + publicMutationPrincipalJoinsSQL + `
		WHERE [[permission]].[[workspace_id]]={:workspaceId} AND [[permission]].[[role]]='full_access'
		GROUP BY [[permission]].[[principal_type]],[[permission]].[[principal_id]]
		ORDER BY MIN([[step]].[[key]]),MIN([[permission]].[[created_at]]),[[permission]].[[principal_id]]
		LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"workspaceId": workspaceID, "chain": string(encoded),
		"limit": databasePropertyAccessHoldersMax + 1}).All(&rows); err != nil {
		return err
	}
	for _, row := range rows {
		holder := DatabasePropertyAccessHolder{Kind: propertyAccessHolderPerson, ID: row.PrincipalUser, Name: row.PrincipalLabel}
		switch row.PrincipalType {
		case publicMutationPrincipalWorkspace:
			holder = DatabasePropertyAccessHolder{Kind: propertyAccessHolderWorkspace}
		case publicMutationPrincipalGroup:
			holder = DatabasePropertyAccessHolder{Kind: propertyAccessHolderGroup, ID: row.PrincipalID, Name: row.PrincipalLabel}
		case publicMutationPrincipalIntegration:
			holder = DatabasePropertyAccessHolder{Kind: propertyAccessHolderApp, ID: row.PrincipalID, Name: row.AppName}
		}
		list.add(holder)
	}
	if len(rows) > 0 {
		list.More += max(rows[0].Total-len(rows), 0)
	}
	return nil
}

// addPropertyAccessTeamspaceHolders adds the teamspace's groups that have
// full access on the page: the rest of the workspace, its members and its
// owners, by the page's own roles where its sharing sets them.
func addPropertyAccessTeamspaceHolders(app core.App, workspaceID, teamspaceID, pageID string, list *propertyAccessHolderList) error {
	teamspace := storedTeamspace{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[access]],[[member_page_role]],[[open_page_role]],[[outside_access]],[[archived_at]]
		FROM {{leafen_teamspaces}} WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[deleted_at]]=0
	`).Bind(dbx.Params{"id": teamspaceID, "workspaceId": workspaceID}).One(&teamspace)
	if errors.Is(err, sql.ErrNoRows) || err == nil && teamspace.ArchivedAt != 0 {
		return nil
	}
	if err != nil {
		return err
	}
	if teamspaceOutsideRole(teamspace) == teamspacePageRoleFullAccess {
		list.add(DatabasePropertyAccessHolder{Kind: propertyAccessHolderWorkspace})
	}
	ownerRole, memberRole, err := pageTeamspaceGroupRoles(app, workspaceID, teamspace, pageID)
	if err != nil {
		return err
	}
	if memberRole == teamspacePageRoleFullAccess {
		list.add(DatabasePropertyAccessHolder{Kind: propertyAccessHolderTeamspaceMembers, ID: teamspaceID})
	}
	if ownerRole == teamspacePageRoleFullAccess {
		list.add(DatabasePropertyAccessHolder{Kind: propertyAccessHolderTeamspaceOwners, ID: teamspaceID})
	}
	return nil
}
