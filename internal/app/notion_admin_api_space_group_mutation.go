package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const notionAdminSpaceGroupMutationLockNamespace = "notion-admin-space-group-mutation"

func createNotionAdminSpaceGroup(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID, name string,
) (notionAdminSpaceGroupResponse, error) {
	release := acquireProductOperationLock(notionAdminSpaceGroupMutationLockNamespace, workspaceID)
	defer release()
	response := notionAdminSpaceGroupResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, _, err := authorizeCurrentNotionAdminSpaceGroupMutation(ctx, txApp, secret, expected, workspaceID)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		group := storedNotionAdminSpaceGroup{
			ID: core.GenerateDefaultRandomId(), WorkspaceID: workspaceID, Name: name, Revision: 1,
		}
		var duplicate int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_workspace_permission_groups}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[normalized_name]] = {:normalizedName}
				AND [[deleted_at]] IS NULL
		`).WithContext(ctx).Bind(dbx.Params{
			"workspaceId": workspaceID, "normalizedName": strings.ToLower(name),
		}).Row(&duplicate); err != nil {
			return err
		}
		if duplicate != 0 {
			return notionAdminInvalid(errNotionAdminSpaceGroupConflict, "body.name is already the name of another group in this workspace.")
		}
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_permission_groups}} (
				[[id]], [[workspace_id]], [[name]], [[normalized_name]], [[revision]],
				[[created_at]], [[updated_at]], [[created_by]], [[updated_by]]
			) VALUES (
				{:id}, {:workspaceId}, {:name}, {:normalizedName}, 1,
				{:now}, {:now}, {:actor}, {:actor}
			)
		`).WithContext(ctx).Bind(dbx.Params{
			"id": group.ID, "workspaceId": workspaceID, "name": name,
			"normalizedName": strings.ToLower(name), "now": now, "actor": current.CreatedBy,
		}).Execute()
		if err != nil {
			return notionAdminSpaceGroupStorageError(err)
		}
		if err := finalizeNotionAdminSpaceGroupMutation(
			txApp, current, now, "notion_admin.permission_group.create", "permission_group", group.ID,
		); err != nil {
			return err
		}
		response, err = projectNotionAdminSpaceGroup(group)
		return err
	})
	return response, err
}

func updateNotionAdminSpaceGroup(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID, groupID, name string,
) (notionAdminSpaceGroupResponse, error) {
	release := acquireProductOperationLock(notionAdminSpaceGroupMutationLockNamespace, workspaceID)
	defer release()
	response := notionAdminSpaceGroupResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, _, err := authorizeCurrentNotionAdminSpaceGroupMutation(ctx, txApp, secret, expected, workspaceID)
		if err != nil {
			return err
		}
		group, err := loadStoredNotionAdminSpaceGroup(ctx, txApp, workspaceID, groupID)
		if err != nil {
			return err
		}
		if group.Name == name {
			response, err = projectNotionAdminSpaceGroup(group)
			return err
		}
		now := time.Now().UTC().UnixMilli()
		err = txApp.DB().NewQuery(`
			UPDATE {{leafen_workspace_permission_groups}}
			SET [[name]] = {:name}, [[normalized_name]] = {:normalizedName},
				[[revision]] = [[revision]] + 1, [[updated_at]] = {:now}, [[updated_by]] = {:actor}
			WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[deleted_at]] IS NULL
			RETURNING [[id]], [[workspace_id]], [[name]], [[revision]]
		`).WithContext(ctx).Bind(dbx.Params{
			"name": name, "normalizedName": strings.ToLower(name), "now": now,
			"actor": current.CreatedBy, "id": groupID, "workspaceId": workspaceID,
		}).One(&group)
		if errors.Is(err, sql.ErrNoRows) {
			return errNotionAdminSpaceGroupNotFound
		}
		if err != nil {
			return notionAdminSpaceGroupStorageError(err)
		}
		if err := finalizeNotionAdminSpaceGroupMutation(
			txApp, current, now, "notion_admin.permission_group.update", "permission_group", groupID,
		); err != nil {
			return err
		}
		response, err = projectNotionAdminSpaceGroup(group)
		return err
	})
	return response, err
}

func deleteNotionAdminSpaceGroup(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID, groupID, transferID string,
) (notionAdminSpaceGroupResponse, error) {
	release := acquireProductOperationLock(notionAdminSpaceGroupMutationLockNamespace, workspaceID)
	defer release()
	response := notionAdminSpaceGroupResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, authority, err := authorizeCurrentNotionAdminSpaceGroupMutation(ctx, txApp, secret, expected, workspaceID)
		if err != nil {
			return err
		}
		if transferID != "" && transferID != authority.OwnerID {
			return errNotionAdminSpaceGroupNotFound
		}
		group, err := loadStoredNotionAdminSpaceGroup(ctx, txApp, workspaceID, groupID)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_workspace_permission_groups}}
			SET [[deleted_at]] = {:now}, [[revision]] = [[revision]] + 1,
				[[updated_at]] = {:now}, [[updated_by]] = {:actor}
			WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[deleted_at]] IS NULL
		`).WithContext(ctx).Bind(dbx.Params{
			"now": now, "actor": current.CreatedBy, "id": groupID, "workspaceId": workspaceID,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errNotionAdminSpaceGroupNotFound, err)
		}
		// Its shares reach no one now (people_change_graphs.go).
		if err := announcePeopleChange(txApp, workspaceID, groupID); err != nil {
			return err
		}
		if err := finalizeNotionAdminSpaceGroupMutation(
			txApp, current, now, "notion_admin.permission_group.delete", "permission_group", groupID,
		); err != nil {
			return err
		}
		response, err = projectNotionAdminSpaceGroup(group)
		return err
	})
	return response, err
}

func upsertNotionAdminSpaceGroupMember(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID, groupID, userID, role string,
	allowCreate bool,
) (notionAdminSpaceGroupMembershipResponse, error) {
	release := acquireProductOperationLock(notionAdminSpaceGroupMutationLockNamespace, workspaceID)
	defer release()
	response := notionAdminSpaceGroupMembershipResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, _, err := authorizeCurrentNotionAdminSpaceGroupMutation(ctx, txApp, secret, expected, workspaceID)
		if err != nil {
			return err
		}
		if err := assertNotionAdminSpaceGroupMemberTarget(
			ctx, txApp, current.OrganizationID, workspaceID, groupID, userID,
		); err != nil {
			return err
		}
		member, err := loadStoredNotionAdminSpaceGroupMember(ctx, txApp, groupID, userID)
		missing := errors.Is(err, errNotionAdminSpaceGroupNotFound)
		if err != nil && !missing {
			return err
		}
		if missing && !allowCreate {
			return errNotionAdminSpaceGroupNotFound
		}
		if !missing && member.Role == role {
			response, err = projectNotionAdminSpaceGroupMember(member)
			return err
		}
		now := time.Now().UTC().UnixMilli()
		action := "notion_admin.permission_group.member.update"
		if missing {
			action = "notion_admin.permission_group.member.add"
			member = storedNotionAdminSpaceGroupMember{
				GroupID: groupID, UserID: userID, Role: role, Revision: 1,
			}
			err = insertNotionAdminSpaceGroupMember(ctx, txApp, current, member, now)
			// Who the group's shares reach changed (people_change_graphs.go).
			if err == nil {
				err = announcePeopleChange(txApp, workspaceID, groupID)
			}
		} else {
			err = updateNotionAdminSpaceGroupMember(ctx, txApp, current, &member, role, now)
		}
		if err != nil {
			return notionAdminSpaceGroupStorageError(err)
		}
		if err := finalizeNotionAdminSpaceGroupMutation(
			txApp, current, now, action, "permission_group_member", groupID+":"+userID,
		); err != nil {
			return err
		}
		response, err = projectNotionAdminSpaceGroupMember(member)
		return err
	})
	return response, err
}

func deleteNotionAdminSpaceGroupMember(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID, groupID, userID string,
) (notionAdminSpaceGroupMembershipResponse, error) {
	release := acquireProductOperationLock(notionAdminSpaceGroupMutationLockNamespace, workspaceID)
	defer release()
	response := notionAdminSpaceGroupMembershipResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, _, err := authorizeCurrentNotionAdminSpaceGroupMutation(ctx, txApp, secret, expected, workspaceID)
		if err != nil {
			return err
		}
		if _, err := loadStoredNotionAdminSpaceGroup(ctx, txApp, workspaceID, groupID); err != nil {
			return err
		}
		member, err := loadStoredNotionAdminSpaceGroupMember(ctx, txApp, groupID, userID)
		if err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_workspace_permission_group_members}}
			WHERE [[group_id]] = {:groupId} AND [[user_id]] = {:userId}
		`).WithContext(ctx).Bind(dbx.Params{"groupId": groupID, "userId": userID}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errNotionAdminSpaceGroupNotFound, err)
		}
		// Who the group's shares reach changed (people_change_graphs.go).
		if err := announcePeopleChange(txApp, workspaceID, groupID); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if err := finalizeNotionAdminSpaceGroupMutation(
			txApp, current, now, "notion_admin.permission_group.member.remove",
			"permission_group_member", groupID+":"+userID,
		); err != nil {
			return err
		}
		response, err = projectNotionAdminSpaceGroupMember(member)
		return err
	})
	return response, err
}

func insertNotionAdminSpaceGroupMember(
	ctx context.Context,
	app core.App,
	identity notionAdminIdentity,
	member storedNotionAdminSpaceGroupMember,
	now int64,
) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_permission_group_members}} (
			[[group_id]], [[user_id]], [[role]], [[revision]], [[created_at]], [[updated_at]], [[updated_by]]
		) VALUES ({:groupId}, {:userId}, {:role}, 1, {:now}, {:now}, {:actor})
	`).WithContext(ctx).Bind(dbx.Params{
		"groupId": member.GroupID, "userId": member.UserID, "role": member.Role,
		"now": now, "actor": identity.CreatedBy,
	}).Execute()
	return err
}

func updateNotionAdminSpaceGroupMember(
	ctx context.Context,
	app core.App,
	identity notionAdminIdentity,
	member *storedNotionAdminSpaceGroupMember,
	role string,
	now int64,
) error {
	err := app.DB().NewQuery(`
		UPDATE {{leafen_workspace_permission_group_members}}
		SET [[role]] = {:role}, [[revision]] = [[revision]] + 1,
			[[updated_at]] = {:now}, [[updated_by]] = {:actor}
		WHERE [[group_id]] = {:groupId} AND [[user_id]] = {:userId}
		RETURNING [[group_id]], [[user_id]], [[role]], [[revision]]
	`).WithContext(ctx).Bind(dbx.Params{
		"groupId": member.GroupID, "userId": member.UserID, "role": role,
		"now": now, "actor": identity.CreatedBy,
	}).One(member)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotionAdminSpaceGroupNotFound
	}
	return err
}

func authorizeCurrentNotionAdminSpaceGroupMutation(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID string,
) (notionAdminIdentity, notionAdminSpaceGroupAuthority, error) {
	current, err := authenticateNotionAdminToken(ctx, app, secret, time.Now().UTC())
	if err != nil {
		return notionAdminIdentity{}, notionAdminSpaceGroupAuthority{}, err
	}
	if current.OrganizationID != expected.OrganizationID || current.TokenID != expected.TokenID ||
		current.CreatedBy != expected.CreatedBy ||
		!current.HasCapability(notionAdminCapabilityPermissionGroupWrite) ||
		!current.CanAccessWorkspace(workspaceID) {
		return notionAdminIdentity{}, notionAdminSpaceGroupAuthority{}, errNotionAdminSpaceGroupForbidden
	}
	authority, err := loadNotionAdminSpaceGroupAuthority(ctx, app, current.OrganizationID, workspaceID)
	return current, authority, err
}

func assertNotionAdminSpaceGroupMemberTarget(
	ctx context.Context,
	app core.App,
	organizationID, workspaceID, groupID, userID string,
) error {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_workspace_permission_groups}} [[group]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[group]].[[workspace_id]]
		JOIN {{leafen_organization_memberships}} [[organization_member]]
			ON [[organization_member]].[[organization]] = [[workspace]].[[organization]]
			AND [[organization_member]].[[user]] = {:userId}
			AND [[organization_member]].[[status]] = {:active}
		WHERE [[group]].[[id]] = {:groupId} AND [[group]].[[workspace_id]] = {:workspaceId}
			AND [[group]].[[deleted_at]] IS NULL AND [[workspace]].[[organization]] = {:organizationId}
			AND ([[workspace]].[[workspace_owner]] = {:userId} OR EXISTS (
				SELECT 1 FROM {{leafen_workspace_memberships}} [[workspace_member]]
				WHERE [[workspace_member]].[[workspace_id]] = [[workspace]].[[id]]
					AND [[workspace_member]].[[user_id]] = {:userId}
					AND [[workspace_member]].[[role]] = {:member}
			))
	`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID, "workspaceId": workspaceID, "groupId": groupID,
		"userId": userID, "active": organizationMemberStatusActive, "member": workspaceRoleMember,
	}).Row(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return errNotionAdminSpaceGroupNotFound
	}
	return nil
}

func finalizeNotionAdminSpaceGroupMutation(
	app core.App,
	identity notionAdminIdentity,
	now int64,
	action, subjectType, subjectID string,
) error {
	if err := bumpOrganizationPolicy(app, identity.OrganizationID, now); err != nil {
		return err
	}
	// Named as the settings' own group lines are: the group then, and the member.
	groupID, memberID, _ := strings.Cut(subjectID, ":")
	return recordWorkspacePermissionGroupAudit(app, workspacePermissionGroupAuditLine{
		organizationID: identity.OrganizationID, actorID: identity.CreatedBy, action: action,
		subjectType: subjectType, subjectID: subjectID, groupID: groupID, memberID: memberID, at: now,
	})
}

func notionAdminSpaceGroupStorageError(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return errors.Join(errNotionAdminSpaceGroupConflict, err)
	}
	return err
}
