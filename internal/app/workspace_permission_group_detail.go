package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A group opened from 설정 → 사람 → 그룹: its name, members and teamspaces
// for the people of the workspace, and, for its owner, renaming it, adding and
// removing members and deleting it — the changes the admin API makes, from
// the owner's session. Each changes the organization's policy generation and
// is audited.

type workspacePermissionGroupMember struct {
	UserID string `json:"userId" db:"user_id"`
}

type workspacePermissionGroupDetail struct {
	workspacePermissionGroupItem
	Members []workspacePermissionGroupMember `json:"members"`
	// The teamspaces the group is in that the reader sees, by name — the
	// window's 팀스페이스 tab, as Notion's (seen 2026-11-09).
	Teamspaces []workspacePermissionGroupTeamspace `json:"teamspaces"`
}

type workspacePermissionGroupTeamspace struct {
	ID   string `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
	Icon string `json:"icon" db:"icon"`
}

type workspacePermissionGroupChange struct {
	Name *string `json:"name"`
	// The group's icon: an emoji, or "" for none.
	Icon *string `json:"icon"`
	// Members to add, and to remove, by user ID.
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

var errWorkspacePermissionGroupMissing = errors.New("workspace permission group not found")

func registerWorkspacePermissionGroupDetailRoutes(event *core.ServeEvent) {
	path := apiRoot + "/workspaces/{workspaceId}/groups/{groupId}"
	event.Router.GET(path, readWorkspacePermissionGroup)
	event.Router.PATCH(path, changeWorkspacePermissionGroup)
	event.Router.DELETE(path, deleteWorkspacePermissionGroup)
}

func workspacePermissionGroupPath(event *core.RequestEvent) (string, string, bool) {
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	groupID := strings.TrimSpace(event.Request.PathValue("groupId"))
	valid := workspaceID != "" && groupID != "" &&
		len(workspaceID) <= maxWorkspacePermissionIdentifierLength && len(groupID) <= maxWorkspacePermissionIdentifierLength
	return workspaceID, groupID, valid
}

func workspacePermissionGroupError(event *core.RequestEvent, err error, stage string) error {
	switch {
	case isPermissionDenied(err):
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	case errors.Is(err, errWorkspacePermissionGroupMissing):
		return apiError(event, http.StatusNotFound, errorCodeGroupNotFound, "Group not found.")
	case errors.Is(err, errWorkspacePermissionGroupConflict), errors.Is(err, errNotionAdminSpaceGroupConflict):
		return apiError(event, http.StatusConflict, errorCodeWorkspaceMemberConflict, "A group already has this name.")
	case errors.Is(err, errWorkspacePermissionGroupMember):
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, workspacePermissionGroupMemberRefusal("add", err))
	case errors.Is(err, errWorkspacePermissionGroupIcon):
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the group's icon.")
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceMemberMutationFailed,
		"Couldn't change the group.", "workspace_permission_groups", stage, errorCodeWorkspaceMemberMutationFailed, err)
}

func readWorkspacePermissionGroup(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID, groupID, valid := workspacePermissionGroupPath(event)
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the group.")
	}
	access, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if err != nil {
		return workspacePermissionGroupError(event, err, "authorize")
	}
	detail, err := loadWorkspacePermissionGroupDetail(event.App, workspaceID, groupID)
	if err == nil {
		detail.Teamspaces, err = loadWorkspacePermissionGroupTeamspaces(event.App, access, groupID)
	}
	if err != nil {
		return workspacePermissionGroupError(event, err, "read")
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, detail)
}

func loadWorkspacePermissionGroupDetail(app core.App, workspaceID, groupID string) (workspacePermissionGroupDetail, error) {
	detail := workspacePermissionGroupDetail{Members: []workspacePermissionGroupMember{}}
	err := app.DB().NewQuery(`
		SELECT [[group]].[[id]], [[group]].[[name]], [[group]].[[icon]], [[group]].[[created_at]],
			(SELECT COUNT(*) FROM {{leafen_workspace_permission_group_members}} [[member]]
				WHERE [[member]].[[group_id]] = [[group]].[[id]]) AS [[member_count]]
		FROM {{leafen_workspace_permission_groups}} [[group]]
		WHERE [[group]].[[id]] = {:groupId} AND [[group]].[[workspace_id]] = {:workspaceId} AND [[group]].[[deleted_at]] IS NULL
	`).Bind(dbx.Params{"workspaceId": workspaceID, "groupId": groupID}).One(&detail.workspacePermissionGroupItem)
	if errors.Is(err, sql.ErrNoRows) {
		return detail, errWorkspacePermissionGroupMissing
	}
	if err != nil {
		return detail, err
	}
	err = app.DB().NewQuery(`
		SELECT [[user_id]] FROM {{leafen_workspace_permission_group_members}}
		WHERE [[group_id]] = {:groupId} ORDER BY [[created_at]], [[user_id]]
	`).Bind(dbx.Params{"groupId": groupID}).All(&detail.Members)
	return detail, err
}

// loadWorkspacePermissionGroupTeamspaces lists the live teamspaces the group
// is a member of, as the reader may see them.
func loadWorkspacePermissionGroupTeamspaces(
	app core.App, access workspaceAuthorization, groupID string,
) ([]workspacePermissionGroupTeamspace, error) {
	teamspaces := []workspacePermissionGroupTeamspace{}
	err := app.DB().NewQuery(`
		WITH [[effective_members]]([[teamspace_id]], [[user_id]]) AS (` + teamspaceEffectiveMembersSQL() + `)
		SELECT [[teamspace]].[[id]], [[teamspace]].[[name]], [[teamspace]].[[icon]]
		FROM {{leafen_teamspaces}} [[teamspace]]
		WHERE ` + workspacePermissionGroupTeamspacesWhere("{:groupId}") + `
		ORDER BY lower([[teamspace]].[[name]]), [[teamspace]].[[id]]
	`).Bind(workspacePermissionGroupReaderParams(access, dbx.Params{"groupId": groupID})).All(&teamspaces)
	return teamspaces, err
}

// workspacePermissionGroupTeamspacesWhere picks, from [[teamspace]], the live
// teamspaces of the workspace the group `group` names is a member of, as the
// reader may see them: the owner every one, anyone else those that aren't
// private and the private ones they are in (through [[effective_members]]).
// The group's window lists them and its row in the list counts them.
func workspacePermissionGroupTeamspacesWhere(group string) string {
	return `[[teamspace]].[[workspace_id]] = {:workspaceId}
			AND [[teamspace]].[[archived_at]] = 0 AND [[teamspace]].[[deleted_at]] = 0
			AND EXISTS (SELECT 1 FROM {{leafen_teamspace_members}} [[group_seat]]
				WHERE [[group_seat]].[[teamspace_id]] = [[teamspace]].[[id]] AND [[group_seat]].[[workspace_id]] = {:workspaceId}
					AND [[group_seat]].[[deleted_at]] = 0 AND [[group_seat]].[[principal_type]] = 'group'
					AND [[group_seat]].[[principal_id]] = ` + group + `)
			AND ({:owner} OR NOT {:restricted} AND [[teamspace]].[[access]] <> 'private'
				OR EXISTS (SELECT 1 FROM [[effective_members]] [[reader]]
					WHERE [[reader]].[[teamspace_id]] = [[teamspace]].[[id]] AND [[reader]].[[user_id]] = {:readerId}))`
}

func workspacePermissionGroupReaderParams(access workspaceAuthorization, more dbx.Params) dbx.Params {
	params := dbx.Params{
		"workspaceId": access.WorkspaceID, "readerId": access.PrincipalID,
		"owner": access.PrincipalID == access.WorkspaceOwnerID, "restricted": access.Restricted,
	}
	for key, value := range more {
		params[key] = value
	}
	return params
}

// changeWorkspacePermissionGroup renames a group, changes its icon, or adds
// and removes its members, for the workspace's owner.
func changeWorkspacePermissionGroup(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID, groupID, valid := workspacePermissionGroupPath(event)
	var input workspacePermissionGroupChange
	body, err := io.ReadAll(io.LimitReader(event.Request.Body, 64<<10))
	if !valid || err != nil || json.Unmarshal(body, &input) != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the group.")
	}
	add, remove := uniqueTrimmed(input.Add), uniqueTrimmed(input.Remove)
	name := ""
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
		if name == "" || len([]rune(name)) > 200 {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the group's name.")
		}
	}
	icon := ""
	if input.Icon != nil {
		var ok bool
		if icon, ok = normalizeWorkspacePermissionGroupIcon(*input.Icon); !ok {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the group's icon.")
		}
	}
	access, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner)
	if err != nil {
		return workspacePermissionGroupError(event, err, "authorize")
	}
	release := acquireProductOperationLock(notionAdminSpaceGroupMutationLockNamespace, workspaceID)
	defer release()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		before, err := loadWorkspacePermissionGroupDetail(txApp, workspaceID, groupID)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if input.Icon != nil && icon != before.Icon {
			if !checkHeldIconFile(txApp, workspaceID, icon) {
				return errWorkspacePermissionGroupIcon
			}
			if _, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_workspace_permission_groups}} SET [[icon]] = {:icon} WHERE [[id]] = {:id}
			`).Bind(dbx.Params{"icon": icon, "id": groupID}).Execute(); err != nil {
				return err
			}
			// The picture it showed goes unless something else shows it (held_icon_files.go).
			if err := settleHeldIconFiles(txApp, workspaceID, before.Icon, icon); err != nil {
				return err
			}
		}
		if name != "" {
			if err := renameWorkspacePermissionGroup(txApp, workspaceID, groupID, user.Id, name, now); err != nil {
				return err
			}
		}
		if err := addWorkspacePermissionGroupMembers(txApp, workspaceID, groupID, user.Id, add, now); err != nil {
			return err
		}
		for _, userID := range remove {
			if _, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_workspace_permission_group_members}} WHERE [[group_id]] = {:groupId} AND [[user_id]] = {:userId}
			`).Bind(dbx.Params{"groupId": groupID, "userId": userID}).Execute(); err != nil {
				return err
			}
		}
		// Who the group's shares reach changed (people_change_graphs.go).
		if len(add)+len(remove) > 0 {
			if err := announcePeopleChange(txApp, workspaceID, groupID); err != nil {
				return err
			}
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_workspace_permission_groups}} SET [[revision]] = [[revision]] + 1, [[updated_at]] = {:now}, [[updated_by]] = {:actor}
			WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": groupID, "now": now, "actor": user.Id}).Execute()
		if err != nil {
			return err
		}
		return finishWorkspacePermissionGroupChange(txApp, access, now, func() error {
			return recordWorkspacePermissionGroupChangeAudit(
				txApp, access, user.Id, groupID, before, name, input.Icon != nil && icon != before.Icon, add, remove, now,
			)
		})
	})
	if err != nil {
		return workspacePermissionGroupError(event, err, "change")
	}
	detail, err := loadWorkspacePermissionGroupDetail(event.App, workspaceID, groupID)
	if err == nil {
		detail.Teamspaces, err = loadWorkspacePermissionGroupTeamspaces(event.App, access, groupID)
	}
	if err != nil {
		return workspacePermissionGroupError(event, err, "reread")
	}
	return event.JSON(http.StatusOK, detail)
}

func renameWorkspacePermissionGroup(app core.App, workspaceID, groupID, actorID, name string, now int64) error {
	var duplicate int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_workspace_permission_groups}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[normalized_name]] = {:normalizedName} AND [[deleted_at]] IS NULL AND [[id]] != {:id}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "normalizedName": strings.ToLower(name), "id": groupID}).Row(&duplicate); err != nil {
		return err
	}
	if duplicate != 0 {
		return errWorkspacePermissionGroupConflict
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_workspace_permission_groups}} SET [[name]] = {:name}, [[normalized_name]] = {:normalizedName},
			[[updated_at]] = {:now}, [[updated_by]] = {:actor}
		WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[deleted_at]] IS NULL
	`).Bind(dbx.Params{
		"name": name, "normalizedName": strings.ToLower(name), "now": now, "actor": actorID, "id": groupID, "workspaceId": workspaceID,
	}).Execute()
	if err != nil {
		return notionAdminSpaceGroupStorageError(err)
	}
	return nil
}

// addWorkspacePermissionGroupMembers adds people of the workspace — its owner
// and members — that the group doesn't have yet.
func addWorkspacePermissionGroupMembers(app core.App, workspaceID, groupID, actorID string, userIDs []string, now int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	if err := workspacePermissionGroupOutsiders(app, workspaceID, userIDs); err != nil {
		return err
	}
	for _, userID := range userIDs {
		if _, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_workspace_permission_group_members}} (
				[[group_id]], [[user_id]], [[role]], [[revision]], [[created_at]], [[updated_at]], [[updated_by]]
			) VALUES ({:groupId}, {:userId}, 'member', 1, {:now}, {:now}, {:actor})
		`).Bind(dbx.Params{"groupId": groupID, "userId": userID, "now": now, "actor": actorID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// deleteWorkspacePermissionGroup deletes a group for the workspace's owner;
// what was shared with it no longer reaches its members.
func deleteWorkspacePermissionGroup(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID, groupID, valid := workspacePermissionGroupPath(event)
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the group.")
	}
	access, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner)
	if err != nil {
		return workspacePermissionGroupError(event, err, "authorize")
	}
	// A key's call asks its person only for a group there is (user_key_acting_ask.go).
	if userKeyAsksWhereItActs(event) {
		live := 0
		if err := event.App.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspace_permission_groups}}
			WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[deleted_at]] IS NULL`).
			Bind(dbx.Params{"id": groupID, "workspaceId": workspaceID}).Row(&live); err != nil || live != 1 {
			return workspacePermissionGroupError(event, errors.Join(errWorkspacePermissionGroupMissing, err), "delete")
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}
	}
	release := acquireProductOperationLock(notionAdminSpaceGroupMutationLockNamespace, workspaceID)
	defer release()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		now := time.Now().UTC().UnixMilli()
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_workspace_permission_groups}}
			SET [[deleted_at]] = {:now}, [[revision]] = [[revision]] + 1, [[updated_at]] = {:now}, [[updated_by]] = {:actor}
			WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[deleted_at]] IS NULL
		`).Bind(dbx.Params{"now": now, "actor": user.Id, "id": groupID, "workspaceId": workspaceID}).Execute()
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			return errors.Join(errWorkspacePermissionGroupMissing, err)
		}
		var icon string
		if err := txApp.DB().NewQuery(`SELECT [[icon]] FROM {{leafen_workspace_permission_groups}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": groupID}).Row(&icon); err != nil {
			return err
		}
		if err := settleHeldIconFiles(txApp, workspaceID, icon); err != nil {
			return err
		}
		// Its shares reach no one now (people_change_graphs.go).
		if err := announcePeopleChange(txApp, workspaceID, groupID); err != nil {
			return err
		}
		return finishWorkspacePermissionGroupChange(txApp, access, now, func() error {
			return recordWorkspacePermissionGroupAudit(txApp, workspacePermissionGroupAuditLine{
				organizationID: access.Organization.OrganizationID, workspaceID: workspaceID, actorID: user.Id,
				action: "workspace.permission_group.delete", groupID: groupID, at: now,
			})
		})
	})
	if err != nil {
		return workspacePermissionGroupError(event, err, "delete")
	}
	return event.NoContent(http.StatusNoContent)
}

// finishWorkspacePermissionGroupChange tells the organization's policy a
// group changed and writes the change's audit lines.
func finishWorkspacePermissionGroupChange(app core.App, access workspaceAuthorization, now int64, record func() error) error {
	if access.Organization.OrganizationID == "" {
		return nil
	}
	if err := bumpOrganizationPolicy(app, access.Organization.OrganizationID, now); err != nil {
		return err
	}
	return record()
}
