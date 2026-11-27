package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's groups in 설정 → 사람 → 그룹, as Notion's (seen
// 2026-10-12): the people of the workspace read them, and its owner makes
// one with an icon, a name and its first members ("새 그룹 생성"). They are the same
// permission groups pages and teamspaces are shared with, so a new one
// changes the organization's policy generation as the admin API's does.

type workspacePermissionGroupItem struct {
	ID          string `json:"id" db:"id"`
	Name        string `json:"name" db:"name"`
	Icon        string `json:"icon" db:"icon"`
	MemberCount int    `json:"memberCount" db:"member_count"`
	CreatedAt   int64  `json:"createdAt" db:"created_at"`
}

// A group as the list shows it: with its owners, by user ID, and how many
// teamspaces it is in that the reader sees — the 그룹 소유자 and 연결된
// 팀스페이스 columns, as Notion's — and the marks by its name, as Notion's
// (seen 2026-11-15): whether the reader is in it ("그룹 멤버") and whether it
// holds a restricted member ("제한됨"), the latter not told to a restricted
// reader, who is not shown who else is restricted.
type workspacePermissionGroupListItem struct {
	ID                  string   `json:"id" db:"id"`
	Name                string   `json:"name" db:"name"`
	Icon                string   `json:"icon" db:"icon"`
	MemberCount         int      `json:"memberCount" db:"member_count"`
	CreatedAt           int64    `json:"createdAt" db:"created_at"`
	TeamspaceCount      int      `json:"teamspaceCount" db:"teamspace_count"`
	ReaderMember        bool     `json:"readerMember" db:"reader_member"`
	HasRestrictedMember bool     `json:"hasRestrictedMember" db:"has_restricted_member"`
	Owners              []string `json:"owners" db:"-"`
}

type workspacePermissionGroupList struct {
	Groups []workspacePermissionGroupListItem `json:"groups"`
}

type workspacePermissionGroupCreateInput struct {
	Name      string   `json:"name"`
	Icon      string   `json:"icon"`
	MemberIDs []string `json:"memberIds"`
}

var (
	errWorkspacePermissionGroupConflict = errors.New("workspace permission group name taken")
	errWorkspacePermissionGroupMember   = errors.New("workspace permission group member outside the workspace")
	errWorkspacePermissionGroupIcon     = errors.New("workspace permission group icon is not the workspace's picture")
)

func registerWorkspacePermissionGroupRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/groups", readWorkspacePermissionGroups)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/groups", createWorkspacePermissionGroup)
}

func readWorkspacePermissionGroups(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the workspace.")
	}
	access, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceMemberListFailed,
			"Couldn't load the groups.", "workspace_permission_groups", "authorize", errorCodeWorkspaceMemberListFailed, err)
	}
	groups, err := loadWorkspacePermissionGroups(event.App, access)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceMemberListFailed,
			"Couldn't load the groups.", "workspace_permission_groups", "read", errorCodeWorkspaceMemberListFailed, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, workspacePermissionGroupList{Groups: groups})
}

func loadWorkspacePermissionGroups(app core.App, access workspaceAuthorization) ([]workspacePermissionGroupListItem, error) {
	groups := []workspacePermissionGroupListItem{}
	err := app.DB().NewQuery(`
		WITH [[effective_members]]([[teamspace_id]], [[user_id]]) AS (` + teamspaceEffectiveMembersSQL() + `)
		SELECT [[group]].[[id]], [[group]].[[name]], [[group]].[[icon]], [[group]].[[created_at]],
			(SELECT COUNT(*) FROM {{leafen_workspace_permission_group_members}} [[member]]
				WHERE [[member]].[[group_id]] = [[group]].[[id]]) AS [[member_count]],
			(SELECT COUNT(*) FROM {{leafen_teamspaces}} [[teamspace]]
				WHERE ` + workspacePermissionGroupTeamspacesWhere("[[group]].[[id]]") + `) AS [[teamspace_count]],
			EXISTS (SELECT 1 FROM {{leafen_workspace_permission_group_members}} [[reader_seat]]
				WHERE [[reader_seat]].[[group_id]] = [[group]].[[id]] AND [[reader_seat]].[[user_id]] = {:readerId}) AS [[reader_member]],
			NOT {:restricted} AND EXISTS (SELECT 1 FROM {{leafen_workspace_permission_group_members}} [[held]]
				WHERE [[held]].[[group_id]] = [[group]].[[id]]
					AND ` + workspaceRestrictedMemberSQL("{:workspaceId}", "[[held]].[[user_id]]") + `) AS [[has_restricted_member]]
		FROM {{leafen_workspace_permission_groups}} [[group]]
		WHERE [[group]].[[workspace_id]] = {:workspaceId} AND [[group]].[[deleted_at]] IS NULL
		ORDER BY [[group]].[[normalized_name]], [[group]].[[id]]
	`).Bind(workspacePermissionGroupReaderParams(access, nil)).All(&groups)
	if err != nil || len(groups) == 0 {
		return groups, err
	}
	owners := []struct {
		GroupID string `db:"group_id"`
		UserID  string `db:"user_id"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[member]].[[group_id]], [[member]].[[user_id]]
		FROM {{leafen_workspace_permission_group_members}} [[member]]
		JOIN {{leafen_workspace_permission_groups}} [[group]] ON [[group]].[[id]] = [[member]].[[group_id]]
		WHERE [[group]].[[workspace_id]] = {:workspaceId} AND [[group]].[[deleted_at]] IS NULL AND [[member]].[[role]] = 'owner'
		ORDER BY [[member]].[[created_at]], [[member]].[[user_id]]
	`).Bind(dbx.Params{"workspaceId": access.WorkspaceID}).All(&owners)
	byGroup := map[string][]string{}
	for _, owner := range owners {
		byGroup[owner.GroupID] = append(byGroup[owner.GroupID], owner.UserID)
	}
	for index := range groups {
		groups[index].Owners = byGroup[groups[index].ID]
		if groups[index].Owners == nil {
			groups[index].Owners = []string{}
		}
	}
	return groups, err
}

func createWorkspacePermissionGroup(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	var input workspacePermissionGroupCreateInput
	body, err := io.ReadAll(io.LimitReader(event.Request.Body, 64<<10))
	if err != nil || json.Unmarshal(body, &input) != nil || workspaceID == "" ||
		len(workspaceID) > maxWorkspacePermissionIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the group.")
	}
	name := strings.TrimSpace(input.Name)
	members := uniqueTrimmed(input.MemberIDs)
	icon, iconOK := normalizeWorkspacePermissionGroupIcon(input.Icon)
	if name == "" || len([]rune(name)) > 200 || !iconOK {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the group's name and members.")
	}
	access, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner)
	if err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceMemberMutationFailed,
			"Couldn't make the group.", "workspace_permission_groups", "authorize", errorCodeWorkspaceMemberMutationFailed, err)
	}
	group, err := insertWorkspacePermissionGroup(event.App, access, user.Id, name, icon, members)
	switch {
	case errors.Is(err, errWorkspacePermissionGroupConflict):
		return apiError(event, http.StatusConflict, errorCodeWorkspaceMemberConflict, "A group already has this name.")
	case errors.Is(err, errWorkspacePermissionGroupMember):
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, workspacePermissionGroupMemberRefusal("memberIds", err))
	case errors.Is(err, errWorkspacePermissionGroupIcon):
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the group's icon.")
	case err != nil:
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceMemberMutationFailed,
			"Couldn't make the group.", "workspace_permission_groups", "create", errorCodeWorkspaceMemberMutationFailed, err)
	}
	return event.JSON(http.StatusCreated, group)
}

func uniqueTrimmed(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func insertWorkspacePermissionGroup(
	app core.App,
	access workspaceAuthorization,
	actorID, name, icon string,
	memberIDs []string,
) (workspacePermissionGroupItem, error) {
	release := acquireProductOperationLock(notionAdminSpaceGroupMutationLockNamespace, access.WorkspaceID)
	defer release()
	now := time.Now().UTC().UnixMilli()
	group := workspacePermissionGroupItem{ID: core.GenerateDefaultRandomId(), Name: name, Icon: icon, MemberCount: len(memberIDs), CreatedAt: now}
	err := app.RunInTransaction(func(txApp core.App) error {
		var duplicate int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_workspace_permission_groups}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[normalized_name]] = {:normalizedName} AND [[deleted_at]] IS NULL
		`).Bind(dbx.Params{"workspaceId": access.WorkspaceID, "normalizedName": strings.ToLower(name)}).Row(&duplicate); err != nil {
			return err
		}
		if duplicate != 0 {
			return errWorkspacePermissionGroupConflict
		}
		if err := workspacePermissionGroupOutsiders(txApp, access.WorkspaceID, memberIDs); err != nil {
			return err
		}
		if !checkHeldIconFile(txApp, access.WorkspaceID, icon) {
			return errWorkspacePermissionGroupIcon
		}
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_permission_groups}} (
				[[id]], [[workspace_id]], [[name]], [[icon]], [[normalized_name]], [[revision]],
				[[created_at]], [[updated_at]], [[created_by]], [[updated_by]]
			) VALUES ({:id}, {:workspaceId}, {:name}, {:icon}, {:normalizedName}, 1, {:now}, {:now}, {:actor}, {:actor})
		`).Bind(dbx.Params{
			"id": group.ID, "workspaceId": access.WorkspaceID, "name": name, "icon": icon,
			"normalizedName": strings.ToLower(name), "now": now, "actor": actorID,
		}).Execute(); err != nil {
			return notionAdminSpaceGroupStorageError(err)
		}
		if err := settleHeldIconFiles(txApp, access.WorkspaceID, icon); err != nil {
			return err
		}
		for _, memberID := range memberIDs {
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_workspace_permission_group_members}} (
					[[group_id]], [[user_id]], [[role]], [[revision]], [[created_at]], [[updated_at]], [[updated_by]]
				) VALUES ({:groupId}, {:userId}, 'member', 1, {:now}, {:now}, {:actor})
			`).Bind(dbx.Params{"groupId": group.ID, "userId": memberID, "now": now, "actor": actorID}).Execute(); err != nil {
				return err
			}
		}
		if access.Organization.OrganizationID == "" {
			return nil
		}
		if err := bumpOrganizationPolicy(txApp, access.Organization.OrganizationID, now); err != nil {
			return err
		}
		line := workspacePermissionGroupAuditLine{
			organizationID: access.Organization.OrganizationID, workspaceID: access.WorkspaceID, actorID: actorID,
			action: "workspace.permission_group.create", groupID: group.ID, at: now,
		}
		if err := recordWorkspacePermissionGroupAudit(txApp, line); err != nil {
			return err
		}
		// The members it was made with are each a line of their own, as a
		// member added later is (recordWorkspacePermissionGroupChangeAudit):
		// the log says who came into the group, not only that it was made.
		for _, memberID := range memberIDs {
			member := line
			member.action, member.memberID = "workspace.permission_group.member_add", memberID
			if err := recordWorkspacePermissionGroupAudit(txApp, member); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errNotionAdminSpaceGroupConflict) {
		err = errWorkspacePermissionGroupConflict
	}
	return group, err
}
