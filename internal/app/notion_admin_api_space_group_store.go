package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionAdminSpaceGroupAuthority struct {
	OwnerID  string `db:"owner_id"`
	Revision int64  `db:"revision"`
}

type storedNotionAdminSpaceGroup struct {
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
	Name        string `db:"name"`
	Revision    int64  `db:"revision"`
}

type storedNotionAdminSpaceGroupMember struct {
	GroupID  string `db:"group_id"`
	UserID   string `db:"user_id"`
	Role     string `db:"role"`
	Revision int64  `db:"revision"`
}

func retrieveNotionAdminSpaceGroup(
	ctx context.Context,
	app core.App,
	identity notionAdminIdentity,
	workspaceID, groupID string,
) (notionAdminSpaceGroupResponse, error) {
	response := notionAdminSpaceGroupResponse{}
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		if _, err := loadNotionAdminSpaceGroupAuthority(ctx, txApp, identity.OrganizationID, workspaceID); err != nil {
			return err
		}
		group, err := loadStoredNotionAdminSpaceGroup(ctx, txApp, workspaceID, groupID)
		if err != nil {
			return err
		}
		response, err = projectNotionAdminSpaceGroup(group)
		return err
	})
	return response, err
}

func listNotionAdminSpaceGroups(
	ctx context.Context,
	app core.App,
	secret string,
	identity notionAdminIdentity,
	workspaceID string,
	cursor notionAdminSpaceGroupCursor,
	pageSize int,
) (notionAdminSpaceGroupListResponse, error) {
	response := notionAdminSpaceGroupListResponse{
		Object: "list", Type: "group", Results: []notionAdminSpaceGroupResponse{},
	}
	authorityRevision := int64(0)
	lastID := ""
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		authority, err := loadNotionAdminSpaceGroupAuthority(ctx, txApp, identity.OrganizationID, workspaceID)
		if err != nil {
			return err
		}
		authorityRevision = authority.Revision
		if cursor.Version != 0 && cursor.Revision != authority.Revision {
			return errInvalidNotionAdminSpaceGroupCursor
		}
		rows := []storedNotionAdminSpaceGroup{}
		if err := txApp.DB().NewQuery(`
			SELECT [[id]], [[workspace_id]], [[name]], [[revision]]
			FROM {{leafen_workspace_permission_groups}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[deleted_at]] IS NULL AND [[id]] > {:after}
			ORDER BY [[id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"workspaceId": workspaceID, "after": cursor.AfterID, "limit": pageSize + 1,
		}).All(&rows); err != nil {
			return err
		}
		response.HasMore = len(rows) > pageSize
		if response.HasMore {
			rows = rows[:pageSize]
		}
		for _, row := range rows {
			projected, err := projectNotionAdminSpaceGroup(row)
			if err != nil {
				return err
			}
			response.Results = append(response.Results, projected)
			lastID = row.ID
		}
		return nil
	})
	if err != nil {
		return notionAdminSpaceGroupListResponse{}, err
	}
	if response.HasMore {
		next, err := encodeNotionAdminSpaceGroupCursor(
			secret, identity, workspaceID, "", notionAdminSpaceGroupCursorGroups,
			authorityRevision, lastID,
		)
		if err != nil {
			return notionAdminSpaceGroupListResponse{}, err
		}
		response.NextCursor = &next
	}
	return response, nil
}

func listNotionAdminSpaceGroupMembers(
	ctx context.Context,
	app core.App,
	secret string,
	identity notionAdminIdentity,
	workspaceID, groupID string,
	cursor notionAdminSpaceGroupCursor,
	pageSize int,
) (notionAdminSpaceGroupMembershipListResponse, error) {
	response := notionAdminSpaceGroupMembershipListResponse{
		Object: "list", Type: notionAdminSpaceGroupCursorMembers,
		Results: []notionAdminSpaceGroupMembershipResponse{},
	}
	authorityRevision := int64(0)
	lastID := ""
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		authority, err := loadNotionAdminSpaceGroupAuthority(ctx, txApp, identity.OrganizationID, workspaceID)
		if err != nil {
			return err
		}
		if _, err := loadStoredNotionAdminSpaceGroup(ctx, txApp, workspaceID, groupID); err != nil {
			return err
		}
		authorityRevision = authority.Revision
		if cursor.Version != 0 && cursor.Revision != authority.Revision {
			return errInvalidNotionAdminSpaceGroupCursor
		}
		rows := []storedNotionAdminSpaceGroupMember{}
		if err := txApp.DB().NewQuery(`
			SELECT [[group_id]], [[user_id]], [[role]], [[revision]]
			FROM {{leafen_workspace_permission_group_members}}
			WHERE [[group_id]] = {:groupId} AND [[user_id]] > {:after}
			ORDER BY [[user_id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"groupId": groupID, "after": cursor.AfterID, "limit": pageSize + 1,
		}).All(&rows); err != nil {
			return err
		}
		response.HasMore = len(rows) > pageSize
		if response.HasMore {
			rows = rows[:pageSize]
		}
		for _, row := range rows {
			projected, err := projectNotionAdminSpaceGroupMember(row)
			if err != nil {
				return err
			}
			response.Results = append(response.Results, projected)
			lastID = row.UserID
		}
		return nil
	})
	if err != nil {
		return notionAdminSpaceGroupMembershipListResponse{}, err
	}
	if response.HasMore {
		next, err := encodeNotionAdminSpaceGroupCursor(
			secret, identity, workspaceID, groupID, notionAdminSpaceGroupCursorMembers,
			authorityRevision, lastID,
		)
		if err != nil {
			return notionAdminSpaceGroupMembershipListResponse{}, err
		}
		response.NextCursor = &next
	}
	return response, nil
}

func loadNotionAdminSpaceGroupAuthority(
	ctx context.Context,
	app core.App,
	organizationID, workspaceID string,
) (notionAdminSpaceGroupAuthority, error) {
	authority := notionAdminSpaceGroupAuthority{}
	err := app.DB().NewQuery(`
		SELECT [[workspace]].[[workspace_owner]] AS [[owner_id]], [[revision]].[[revision]]
		FROM {{leafen_workspaces}} [[workspace]]
		JOIN {{leafen_workspace_permission_group_revisions}} [[revision]]
			ON [[revision]].[[workspace_id]] = [[workspace]].[[id]]
		WHERE [[workspace]].[[id]] = {:workspaceId} AND [[workspace]].[[organization]] = {:organizationId}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "organizationId": organizationID,
	}).One(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return notionAdminSpaceGroupAuthority{}, errNotionAdminSpaceGroupNotFound
	}
	if err != nil {
		return notionAdminSpaceGroupAuthority{}, err
	}
	if authority.OwnerID == "" || authority.Revision < 1 {
		return notionAdminSpaceGroupAuthority{}, errors.New("invalid Notion Admin space-group authority")
	}
	return authority, nil
}

func loadStoredNotionAdminSpaceGroup(
	ctx context.Context,
	app core.App,
	workspaceID, groupID string,
) (storedNotionAdminSpaceGroup, error) {
	group := storedNotionAdminSpaceGroup{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[workspace_id]], [[name]], [[revision]]
		FROM {{leafen_workspace_permission_groups}}
		WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[deleted_at]] IS NULL
	`).WithContext(ctx).Bind(dbx.Params{"id": groupID, "workspaceId": workspaceID}).One(&group)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotionAdminSpaceGroup{}, errNotionAdminSpaceGroupNotFound
	}
	return group, err
}

func loadStoredNotionAdminSpaceGroupMember(
	ctx context.Context,
	app core.App,
	groupID, userID string,
) (storedNotionAdminSpaceGroupMember, error) {
	member := storedNotionAdminSpaceGroupMember{}
	err := app.DB().NewQuery(`
		SELECT [[group_id]], [[user_id]], [[role]], [[revision]]
		FROM {{leafen_workspace_permission_group_members}}
		WHERE [[group_id]] = {:groupId} AND [[user_id]] = {:userId}
	`).WithContext(ctx).Bind(dbx.Params{"groupId": groupID, "userId": userID}).One(&member)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotionAdminSpaceGroupMember{}, errNotionAdminSpaceGroupNotFound
	}
	return member, err
}

func projectNotionAdminSpaceGroup(group storedNotionAdminSpaceGroup) (notionAdminSpaceGroupResponse, error) {
	publicID, err := notionCompatibleGroupID(group.ID)
	if err != nil || group.Name == "" || group.Revision < 1 {
		return notionAdminSpaceGroupResponse{}, errors.Join(errNotionAdminSpaceGroupInvalid, err)
	}
	return notionAdminSpaceGroupResponse{Object: "group", ID: publicID, Name: group.Name}, nil
}

func projectNotionAdminSpaceGroupMember(member storedNotionAdminSpaceGroupMember) (notionAdminSpaceGroupMembershipResponse, error) {
	groupID, err := notionCompatibleGroupID(member.GroupID)
	if err != nil {
		return notionAdminSpaceGroupMembershipResponse{}, err
	}
	userID, err := notionCompatibleUserID(member.UserID)
	if err != nil || member.Revision < 1 ||
		member.Role != notionAdminSpaceGroupRoleOwner && member.Role != notionAdminSpaceGroupRoleMember {
		return notionAdminSpaceGroupMembershipResponse{}, errors.Join(errNotionAdminSpaceGroupInvalid, err)
	}
	return notionAdminSpaceGroupMembershipResponse{
		Object: "group_membership", GroupID: groupID,
		Member: notionAdminSpaceGroupMemberReference{Type: "user", UserID: userID}, Role: member.Role,
	}, nil
}
