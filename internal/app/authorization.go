package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	workspaceRoleOwner = "owner"

	errorCodePermissionDenied = "LEAFEN_PERMISSION_DENIED"
	// permissionReasonWorkspaceMoved is a change refused because the
	// workspace moved to another server.
	permissionReasonWorkspaceMoved = "workspaceMoved"
)

type permissionRequirement int

const (
	permissionWorkspaceRead permissionRequirement = iota
	permissionWorkspaceWrite
	permissionWorkspaceOwner
)

type permissionError struct {
	code string
	// reason tells one kind of refusal from the others where a screen
	// answers it apart (guestMFARequired); the code stays permission denied.
	reason string
}

func (e *permissionError) Error() string {
	return e.code
}

type workspaceAuthorization struct {
	Workspace        *core.Record
	Organization     organizationAuthorization
	PrincipalID      string
	WorkspaceID      string
	OwnerID          string
	WorkspaceOwnerID string
	Role             string
	// Restricted is a member who sees only the teamspaces they were added
	// to and the pages shared with them (workspace_restricted_members.go).
	Restricted bool
}

type pageAuthorization struct {
	Workspace  workspaceAuthorization
	Page       *core.Record
	AccessRole pageAccessRole
	// Source is how the role came, where the decision names it (page_access_source.go).
	Source pageAccessSource
}

type storedPermissionRole struct {
	Role       string `db:"role"`
	Restricted bool   `db:"restricted"`
}

type storedWorkspaceSetAuthorization struct {
	ID               string `db:"id"`
	OrganizationID   string `db:"organization_id"`
	OwnerID          string `db:"owner_id"`
	WorkspaceOwnerID string `db:"workspace_owner_id"`
	Role             string `db:"role"`
}

type storedPageSetAuthorization struct {
	ID string `db:"id"`
}

func permissionDenied() error {
	return &permissionError{code: errorCodePermissionDenied}
}

func isPermissionDenied(err error) bool {
	var denied *permissionError
	return errors.As(err, &denied) && denied.code == errorCodePermissionDenied
}

func authorizeWorkspace(
	app core.App,
	principalID string,
	workspaceID string,
	requirement permissionRequirement,
) (workspaceAuthorization, error) {
	return authorizeWorkspaceWithDeletionFence(app, principalID, workspaceID, requirement, "")
}

func authorizeWorkspaceDeletion(
	app core.App,
	principalID string,
	workspaceID string,
	deletionJobID string,
) (workspaceAuthorization, error) {
	if deletionJobID == "" {
		return workspaceAuthorization{}, permissionDenied()
	}
	return authorizeWorkspaceWithDeletionFence(
		app, principalID, workspaceID, permissionWorkspaceOwner, deletionJobID,
	)
}

func authorizeWorkspaceWithDeletionFence(
	app core.App,
	principalID string,
	workspaceID string,
	requirement permissionRequirement,
	allowedDeletionJobID string,
) (workspaceAuthorization, error) {
	return remembered(app, workspaceAccessMemoKey{
		principalID: principalID, workspaceID: workspaceID, allowedDeletionJobID: allowedDeletionJobID, requirement: requirement,
	}, func() (workspaceAuthorization, error) {
		return decideWorkspaceAccess(app, principalID, workspaceID, requirement, allowedDeletionJobID)
	})
}

func decideWorkspaceAccess(
	app core.App,
	principalID string,
	workspaceID string,
	requirement permissionRequirement,
	allowedDeletionJobID string,
) (workspaceAuthorization, error) {
	workspace, err := findRecordRemembered(app, workspacesCollection, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return workspaceAuthorization{}, permissionDenied()
	}
	if err != nil {
		return workspaceAuthorization{}, err
	}
	if fence := workspace.GetString("deletion_job_id"); fence != allowedDeletionJobID {
		return workspaceAuthorization{}, permissionDenied()
	}
	// A workspace moving or moved to another server takes no changes; its
	// owner may still delete or export it (workspace_move_fence.go).
	if requirement == permissionWorkspaceWrite {
		if err := workspaceMoveFence(app, workspaceID, pageAccessRoleEdit); err != nil {
			return workspaceAuthorization{}, err
		}
	}

	authorization := workspaceAuthorization{
		Workspace:        workspace,
		PrincipalID:      principalID,
		WorkspaceID:      workspace.Id,
		OwnerID:          workspace.GetString("owner"),
		WorkspaceOwnerID: workspacePermissionOwnerID(workspace),
	}
	organizationID := workspace.GetString("organization")
	if authorization.OwnerID == "" || organizationID == "" {
		return workspaceAuthorization{}, permissionDenied()
	}
	// A workspace belongs to the organization its organization field names; its
	// owner is where its data is kept, not who owns the organization now
	// (2026-11-05: passing the organization on had locked every workspace,
	// because both had to name the same person).
	organization, err := authorizeOrganization(app, principalID, organizationID, permissionOrganizationActive)
	if err != nil {
		if isPermissionDenied(err) || err == nil {
			return workspaceAuthorization{}, permissionDenied()
		}
		return workspaceAuthorization{}, err
	}
	authorization.Organization = organization
	if authorization.WorkspaceOwnerID == principalID {
		authorization.Role = workspaceRoleOwner
		return authorization, nil
	}
	if requirement == permissionWorkspaceOwner {
		return workspaceAuthorization{}, permissionDenied()
	}

	roles := []storedPermissionRole{}
	err = app.DB().NewQuery(`
		SELECT [[role]], ` + workspaceRestrictedMemberSQL("{:workspaceId}", "{:userId}") + ` AS [[restricted]]
		FROM {{leafen_workspace_memberships}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}
		LIMIT 2
	`).Bind(dbx.Params{"workspaceId": workspace.Id, "userId": principalID}).All(&roles)
	if err != nil {
		return workspaceAuthorization{}, err
	}
	if len(roles) != 1 || roles[0].Role != workspaceRoleMember {
		return workspaceAuthorization{}, permissionDenied()
	}
	authorization.Role = workspaceRoleMember
	authorization.Restricted = roles[0].Restricted
	return authorization, nil
}

func authorizeWorkspaceSet(
	app core.App,
	principalID string,
	workspaceIDs []string,
	requirement permissionRequirement,
) error {
	if len(workspaceIDs) == 0 {
		return permissionDenied()
	}
	// However many workspaces, as one JSON list.
	encoded, err := json.Marshal(workspaceIDs)
	if err != nil {
		return err
	}
	params := dbx.Params{"principalId": principalID, "workspaceIds": string(encoded)}
	rows := []storedWorkspaceSetAuthorization{}
	query := strings.ReplaceAll(`
		SELECT [[workspace]].[[id]], [[workspace]].[[organization]] AS [[organization_id]],
			[[workspace]].[[owner]] AS [[owner_id]],
			__WORKSPACE_PERMISSION_OWNER__ AS [[workspace_owner_id]],
			COALESCE([[membership]].[[role]], '') AS [[role]]
		FROM {{leafen_workspaces}} [[workspace]]
		LEFT JOIN {{leafen_workspace_memberships}} [[membership]]
			ON [[membership]].[[workspace_id]] = [[workspace]].[[id]]
			AND [[membership]].[[user_id]] = {:principalId}
			AND [[membership]].[[role]] = 'member'
		WHERE [[workspace]].[[id]] IN (SELECT [[value]] FROM json_each({:workspaceIds}))
			AND [[workspace]].[[deletion_job_id]] = ''
			AND (__WORKSPACE_PERMISSION_OWNER__ = {:principalId} OR [[membership]].[[user_id]] IS NOT NULL)
	`, "__WORKSPACE_PERMISSION_OWNER__", workspacePermissionOwnerSQL(app, "[[workspace]]"))
	if err := app.DB().NewQuery(query).Bind(params).All(&rows); err != nil {
		return err
	}
	if len(rows) != len(workspaceIDs) {
		return permissionDenied()
	}
	organizationIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		organizationIDs = append(organizationIDs, row.OrganizationID)
	}
	organizations, err := authorizeOrganizationSet(app, principalID, organizationIDs, permissionOrganizationActive)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, row := range rows {
		_, organizationOK := organizations[row.OrganizationID]
		if row.ID == "" || row.OwnerID == "" || !organizationOK {
			return permissionDenied()
		}
		if _, exists := seen[row.ID]; exists {
			return permissionDenied()
		}
		seen[row.ID] = struct{}{}
		if row.WorkspaceOwnerID == principalID {
			continue
		}
		if requirement == permissionWorkspaceOwner || row.Role != workspaceRoleMember {
			return permissionDenied()
		}
	}
	return nil
}

func authorizePage(
	app core.App,
	principalID string,
	pageID string,
	requirement permissionRequirement,
) (pageAuthorization, error) {
	return authorizePageAccessRole(
		app, principalID, pageID,
		pageAccessRoleForWorkspaceRequirement(requirement),
	)
}

func authorizePageSet(
	app core.App,
	principalID string,
	workspaceID string,
	pageIDs []string,
	requirement permissionRequirement,
) (workspaceAuthorization, error) {
	if len(pageIDs) == 0 {
		return workspaceAuthorization{}, permissionDenied()
	}
	authorization, err := authorizeWorkspace(app, principalID, workspaceID, requirement)
	if isPermissionDenied(err) && requirement != permissionWorkspaceOwner {
		// Someone the pages are shared with opens them without being a member
		// of their workspace, as with one page; each page is judged below.
		authorization, err = authorizePageSetGrantWorkspace(app, principalID, workspaceID)
	}
	if err != nil {
		return workspaceAuthorization{}, err
	}

	params := dbx.Params{
		"ownerId":     authorization.OwnerID,
		"workspaceId": authorization.WorkspaceID,
	}
	placeholders := make([]string, 0, len(pageIDs))
	seen := make(map[string]struct{}, len(pageIDs))
	for _, pageID := range pageIDs {
		if _, exists := seen[pageID]; exists {
			continue
		}
		seen[pageID] = struct{}{}
		key := fmt.Sprintf("page%d", len(placeholders))
		params[key] = pageID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []storedPageSetAuthorization{}
	err = app.DB().NewQuery(`
		SELECT [[id]]
		FROM {{leafen_pages}}
		WHERE [[workspace]] = {:workspaceId}
			AND [[owner]] = {:ownerId}
			AND [[notion_import_staging]] = 0
			AND [[id]] IN (` + joinSQL(placeholders) + `)
	`).Bind(params).All(&rows)
	if err != nil {
		return workspaceAuthorization{}, err
	}
	if len(rows) != len(placeholders) {
		return workspaceAuthorization{}, permissionDenied()
	}
	hidden, err := pageDuplicationSetTargetHidden(app, pageIDs, requirement == permissionWorkspaceRead)
	if err != nil {
		return workspaceAuthorization{}, err
	}
	if hidden {
		return workspaceAuthorization{}, permissionDenied()
	}
	fenced, err := pagePermanentDeleteSetFenceActive(app, pageIDs)
	if err != nil {
		return workspaceAuthorization{}, err
	}
	if fenced {
		return workspaceAuthorization{}, permissionDenied()
	}
	if requirement != permissionWorkspaceRead {
		fenced, err := pageDuplicationSetHeldFenceActive(app, pageIDs)
		if err != nil {
			return workspaceAuthorization{}, err
		}
		if fenced {
			return workspaceAuthorization{}, permissionDenied()
		}
	}
	fenced, err = blockMoveSetFenceActive(app, pageIDs)
	if err != nil {
		return workspaceAuthorization{}, err
	}
	if fenced {
		return workspaceAuthorization{}, permissionDenied()
	}
	minimum := pageAccessRoleForWorkspaceRequirement(requirement)
	for pageID := range seen {
		page, err := authorizePageAccessRole(app, principalID, pageID, minimum)
		if err != nil || page.Workspace.WorkspaceID != authorization.WorkspaceID {
			if err == nil || isPermissionDenied(err) {
				return workspaceAuthorization{}, permissionDenied()
			}
			return workspaceAuthorization{}, err
		}
	}
	return authorization, nil
}

func pagePermanentDeleteFenceActive(app core.App, pageID string) (bool, error) {
	// Asked once a read scope: a database item's graph read asked it for
	// reading, editing and suggesting (2026-10-17).
	return remembered(app, permanentDeleteFenceMemoKey{pageID}, func() (bool, error) {
		return readPagePermanentDeleteFence(app, pageID)
	})
}

type permanentDeleteFenceMemoKey struct{ pageID string }

func readPagePermanentDeleteFence(app core.App, pageID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[permanent_delete_ancestors]] ([[id]], [[parent_id]], [[deletion_job_id]]) AS (
			SELECT [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `, [[page]].[[deletion_job_id]]
			FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]] = {:pageId}
			UNION
			SELECT [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `, [[page]].[[deletion_job_id]]
			FROM {{leafen_pages}} [[page]]
			JOIN [[permanent_delete_ancestors]] [[ancestor]]
				ON [[page]].[[id]] = [[ancestor]].[[parent_id]]
			WHERE [[ancestor]].[[parent_id]] != ''
		)
		SELECT COUNT(*) FROM [[permanent_delete_ancestors]]
		WHERE [[deletion_job_id]] != ''
	`).Bind(dbx.Params{"pageId": pageID}).Row(&count)
	return count > 0, err
}

func pagePermanentDeleteSetFenceActive(app core.App, pageIDs []string) (bool, error) {
	params := dbx.Params{}
	placeholders := make([]string, 0, len(pageIDs))
	for index, pageID := range pageIDs {
		key := fmt.Sprintf("fencePage%d", index)
		params[key] = pageID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	var count int
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[permanent_delete_ancestors]] ([[id]], [[parent_id]], [[deletion_job_id]]) AS (
			SELECT [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `, [[page]].[[deletion_job_id]]
			FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]] IN (` + joinSQL(placeholders) + `)
			UNION
			SELECT [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `, [[page]].[[deletion_job_id]]
			FROM {{leafen_pages}} [[page]]
			JOIN [[permanent_delete_ancestors]] [[ancestor]]
				ON [[page]].[[id]] = [[ancestor]].[[parent_id]]
			WHERE [[ancestor]].[[parent_id]] != ''
		)
		SELECT COUNT(*) FROM [[permanent_delete_ancestors]]
		WHERE [[deletion_job_id]] != ''
	`).Bind(params).Row(&count)
	return count > 0, err
}
