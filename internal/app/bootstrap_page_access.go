package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Which pages a person sees in the sidebar, read when asked rather than kept
// per person and page (migration 613): in each workspace they belong to
// (leafen_bootstrap_workspace_visibility, one row a person and workspace),
// their own private pages and the pages of each teamspace open to them —
// anyone not restricted in the workspace, its owner too, sees one open to the
// rest of it and its default ones, and its members see theirs, by themselves
// or a group — with a wiki's rows among them. A teamspace's pages are read
// through it, so a page or a teamspace member changing changes nothing kept.

// bootstrapTeamspaceAccessSQL says a teamspace (an alias of
// leafen_teamspaces) is open to a person, given by an SQL expression, the
// workspace's owner as anyone else (effectiveTeamspaceRoleOnPage). An
// archived one stays open to its own members, by themselves or a group, as
// its pages do; the sidebar, which lists only the teamspaces in use, shows
// none of its pages.
func bootstrapTeamspaceAccessSQL(teamspace, principal string) string {
	restricted := `EXISTS (SELECT 1 FROM {{leafen_workspace_restricted_members}} [[access_restricted]]
				WHERE [[access_restricted]].[[workspace_id]] = ` + teamspace + `.[[workspace_id]]
					AND [[access_restricted]].[[user_id]] = ` + principal + `)`
	isDefault := teamspaceDefaultSQL(teamspace+".[[workspace_id]]", teamspace+".[[id]]", "access_settings")
	return strings.NewReplacer("__TEAMSPACE__", teamspace, "__PRINCIPAL__", principal,
		"__RESTRICTED__", restricted, "__DEFAULT__", isDefault).Replace(`(
		__TEAMSPACE__.[[deleted_at]] = 0 AND (
			(__TEAMSPACE__.[[archived_at]] = 0 AND __TEAMSPACE__.[[access]] != 'private'
				AND __TEAMSPACE__.[[outside_access]] = 1 AND NOT __RESTRICTED__)
			OR (__TEAMSPACE__.[[archived_at]] = 0 AND __DEFAULT__ AND NOT __RESTRICTED__)
			OR EXISTS (
				SELECT 1 FROM {{leafen_teamspace_members}} [[access_member]]
				WHERE [[access_member]].[[workspace_id]] = __TEAMSPACE__.[[workspace_id]]
					AND [[access_member]].[[teamspace_id]] = __TEAMSPACE__.[[id]]
					AND [[access_member]].[[deleted_at]] = 0
					AND [[access_member]].[[principal_type]] = 'user'
					AND [[access_member]].[[principal_id]] = __PRINCIPAL__)
			OR EXISTS (
				SELECT 1 FROM {{leafen_teamspace_members}} [[access_group_member]]
				JOIN {{leafen_workspace_permission_groups}} [[access_group]]
					ON [[access_group]].[[id]] = [[access_group_member]].[[principal_id]]
					AND [[access_group]].[[workspace_id]] = __TEAMSPACE__.[[workspace_id]]
					AND [[access_group]].[[deleted_at]] IS NULL
				JOIN {{leafen_workspace_permission_group_members}} [[access_group_user]]
					ON [[access_group_user]].[[group_id]] = [[access_group]].[[id]]
					AND [[access_group_user]].[[user_id]] = __PRINCIPAL__
				WHERE [[access_group_member]].[[workspace_id]] = __TEAMSPACE__.[[workspace_id]]
					AND [[access_group_member]].[[teamspace_id]] = __TEAMSPACE__.[[id]]
					AND [[access_group_member]].[[deleted_at]] = 0
					AND [[access_group_member]].[[principal_type]] = 'group')
		)
	)`)
}

// bootstrapSidebarKindSQL says a page (an alias of leafen_pages) is one the
// sidebar lists: not a database's row, but a wiki's — not its templates,
// which are its database's hidden pages, as Notion's, nor one in its
// database's trash, which leaves the sidebar as a page in the trash does.
func bootstrapSidebarKindSQL(page string) string {
	return `(` + page + `.[[data_source_id]] = '' OR NOT ` + page + `.[[` + databaseTemplatePageField + `]]
		AND EXISTS (SELECT 1 FROM {{leafen_data_sources}} [[sidebar_wiki]]
		WHERE [[sidebar_wiki]].[[id]] = ` + page + `.[[data_source_id]] AND [[sidebar_wiki]].[[wiki]] = 1)
		AND NOT EXISTS (SELECT 1 FROM {{leafen_database_row_trash}} [[sidebar_row_trash]]
		WHERE [[sidebar_row_trash]].[[row_id]] = ` + page + `.[[id]]))`
}

// bootstrapPageSeenSQL says a person, given by an SQL expression, sees a
// page (an alias of leafen_pages) in their sidebar: they belong to its
// workspace, and it is theirs to see there (bootstrapPageListedSQL).
func bootstrapPageSeenSQL(page, principal string) string {
	return `(EXISTS (
			SELECT 1 FROM {{leafen_bootstrap_workspace_visibility}} [[seen_workspace]]
			WHERE [[seen_workspace]].[[principal_id]] = ` + principal + ` AND [[seen_workspace]].[[workspace_id]] = ` + page + `.[[workspace]])
		AND ` + bootstrapPageListedSQL(page, principal) + `)`
}

// bootstrapPageListedSQL says a page of a workspace a person belongs to is
// in their sidebar: one it lists, their private page or in a teamspace open
// to them.
func bootstrapPageListedSQL(page, principal string) string {
	return `(` + bootstrapSidebarKindSQL(page) + `
		AND ((` + page + `.[[teamspace_id]] = '' AND ` + page + `.[[private_owner_id]] = ` + principal + `)
			OR EXISTS (
				SELECT 1 FROM {{leafen_teamspaces}} [[seen_teamspace]]
				WHERE [[seen_teamspace]].[[id]] = ` + page + `.[[teamspace_id]] AND [[seen_teamspace]].[[workspace_id]] = ` + page + `.[[workspace]]
					AND ` + bootstrapTeamspaceAccessSQL("[[seen_teamspace]]", principal) + `)))`
}

// bootstrapPageWorkspace is a workspace whose pages a person's sidebar
// lists.
type bootstrapPageWorkspace struct {
	ID      string `db:"workspace_id"`
	Created string `db:"workspace_created"`
}

// loadBootstrapPageWorkspaces are the workspaces a person's pages come from,
// as their workspaces do (loadBootstrapWorkspaces), in the order they come.
func loadBootstrapPageWorkspaces(app core.App, principalID string) ([]bootstrapPageWorkspace, error) {
	rows := []bootstrapPageWorkspace{}
	err := app.DB().NewQuery(`
		SELECT [[visibility]].[[workspace_id]], [[visibility]].[[workspace_created]]
		FROM {{leafen_bootstrap_workspace_visibility}} [[visibility]]
			INDEXED BY [[idx_leafen_bootstrap_workspace_visibility_principal_order]]
		CROSS JOIN {{leafen_workspaces}} [[workspace]]
		CROSS JOIN {{leafen_organizations}} [[organization]]
		CROSS JOIN {{leafen_organization_memberships}} [[organization_membership]]
		WHERE [[visibility]].[[principal_id]] = {:principalId}
			AND [[workspace]].[[id]] = [[visibility]].[[workspace_id]]
			AND [[workspace]].[[deletion_job_id]] = ''
			AND [[organization]].[[id]] = [[workspace]].[[organization]]
			AND [[organization_membership]].[[id]] = [[visibility]].[[organization_membership_id]]
			AND [[organization_membership]].[[organization]] = [[workspace]].[[organization]]
			AND [[organization_membership]].[[user]] = {:principalId}
			AND [[organization_membership]].[[status]] = {:activeStatus}
			AND [[organization_membership]].[[role]] IN ({:ownerRole}, {:adminRole}, {:securityRole}, {:billingRole}, {:memberRole}, {:guestRole})
			AND (([[workspace]].[[workspace_owner]] = {:principalId}
					AND [[visibility]].[[workspace_membership_id]] = '')
				OR ([[workspace]].[[workspace_owner]] != {:principalId}
					AND [[visibility]].[[workspace_membership_id]] = {:principalId}
					AND EXISTS (
						SELECT 1 FROM {{leafen_workspace_memberships}} [[workspace_membership]]
						WHERE [[workspace_membership]].[[workspace_id]] = [[workspace]].[[id]]
							AND [[workspace_membership]].[[user_id]] = {:principalId}
							AND [[workspace_membership]].[[role]] = 'member'
					)))
		ORDER BY [[visibility]].[[workspace_created]], [[visibility]].[[workspace_id]]
	`).Bind(dbx.Params{
		"principalId": principalID, "activeStatus": organizationMemberStatusActive,
		"ownerRole": organizationRoleOwner, "adminRole": organizationRoleAdmin,
		"securityRole": organizationRoleSecurityAdmin, "billingRole": organizationRoleBillingAdmin,
		"memberRole": organizationRoleMember, "guestRole": organizationRoleGuest,
	}).All(&rows)
	return rows, err
}

// loadBootstrapTeamspaces are the teamspaces of a workspace open to a
// person, by their IDs.
func loadBootstrapTeamspaces(app core.App, principalID string, workspace bootstrapPageWorkspace) ([]string, error) {
	ids := []string{}
	err := app.DB().NewQuery(`
		SELECT [[teamspace]].[[id]] FROM {{leafen_teamspaces}} [[teamspace]]
		WHERE [[teamspace]].[[workspace_id]] = {:workspaceId}
			AND ` + bootstrapTeamspaceAccessSQL("[[teamspace]]", "{:principalId}") + `
		ORDER BY [[teamspace]].[[id]]
	`).Bind(dbx.Params{"workspaceId": workspace.ID, "principalId": principalID}).Column(&ids)
	return ids, err
}

// loadBootstrapPages reads, after the cursor, the pages a person's sidebar
// lists: workspace by workspace, as their workspaces come, and in each their
// private pages and then each teamspace open to them, by its ID — each part
// by when its pages were made — so a window costs what it holds.
func loadBootstrapPages(
	app core.App,
	principalID string,
	cursor bootstrapCursor,
	limit int,
) ([]storedBootstrapPage, map[string]string, bool, error) {
	workspaces, err := loadBootstrapPageWorkspaces(app, principalID)
	if err != nil {
		return nil, nil, false, err
	}
	// When each workspace was made, for the cursor after its pages.
	created := make(map[string]string, len(workspaces))
	for _, workspace := range workspaces {
		created[workspace.ID] = workspace.Created
	}
	rows := []storedBootstrapPage{}
	for _, workspace := range workspaces {
		if cursor.PageWorkspace != "" && (workspace.Created < cursor.PageWorkspaceCreated ||
			workspace.Created == cursor.PageWorkspaceCreated && workspace.ID < cursor.PageWorkspace) {
			continue
		}
		here := workspace.ID == cursor.PageWorkspace
		teamspaces, err := loadBootstrapTeamspaces(app, principalID, workspace)
		if err != nil {
			return nil, nil, false, err
		}
		// Their private pages first: no teamspace, "" before any ID.
		for _, teamspace := range append([]string{""}, teamspaces...) {
			if here && teamspace < cursor.PageTeamspace {
				continue
			}
			after := bootstrapCursor{}
			if here && teamspace == cursor.PageTeamspace {
				after.PageCreated, after.PageID = cursor.PageCreated, cursor.PageID
			}
			part, err := loadBootstrapPartPages(app, principalID, workspace.ID, teamspace, after, limit+1-len(rows))
			if err != nil {
				return nil, nil, false, err
			}
			rows = append(rows, part...)
			if len(rows) > limit {
				return rows[:limit], created, false, nil
			}
		}
	}
	return rows, created, true, nil
}

// loadBootstrapPartPages reads, after a page, the pages of one part of a
// workspace: a teamspace's, or with none the person's private pages.
func loadBootstrapPartPages(app core.App, principalID, workspaceID, teamspaceID string, after bootstrapCursor, limit int) ([]storedBootstrapPage, error) {
	part := `[[page]].[[teamspace_id]] = {:teamspaceId}`
	index := `[[idx_leafen_pages_bootstrap_teamspace]]`
	if teamspaceID == "" {
		part = `[[page]].[[teamspace_id]] = '' AND [[page]].[[private_owner_id]] = {:principalId}`
		index = `[[idx_leafen_pages_bootstrap_private]]`
	}
	// After a page, by the index's own order (a row value seeks).
	from := ""
	if after.PageCreated != "" || after.PageID != "" {
		from = ` AND ([[page]].[[created]], [[page]].[[id]]) > ({:afterCreated}, {:afterId})`
	}
	rows := []storedBootstrapPage{}
	err := app.DB().NewQuery(`
		SELECT ` + bootstrapPageColumnsSQL + `
		FROM {{leafen_pages}} [[page]] INDEXED BY ` + index + `
		CROSS JOIN {{leafen_workspaces}} [[workspace]]
		LEFT JOIN {{leafen_page_favorites}} [[favorite_state]]
			ON [[favorite_state]].[[page_id]]=[[page]].[[id]]
			AND [[favorite_state]].[[principal_id]] = {:principalId}
		WHERE [[page]].[[workspace]] = {:workspaceId} AND ` + part + `
			AND [[workspace]].[[id]] = [[page]].[[workspace]]
			AND [[page]].[[owner]] = [[workspace]].[[owner]]
			AND [[page]].[[notion_import_staging]] = FALSE
			AND ` + bootstrapSidebarKindSQL("[[page]]") + `
			AND ` + pageNotHeldForDeletionSQL("[[page]]", "{:workspaceId}") + `
			AND ` + bootstrapPageCompleteSQL + from + `
		ORDER BY [[page]].[[created]], [[page]].[[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"principalId": principalID, "workspaceId": workspaceID, "teamspaceId": teamspaceID,
		"afterCreated": after.PageCreated, "afterId": after.PageID, "limit": limit,
	}).All(&rows)
	return rows, err
}
