package app

import (
	"encoding/json"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// bootstrapShareRootsSQL names, as common table expressions, the
// workspaces of the organizations a person is active in and the pages shared
// with them there: with them, a group they are in, or their verified email;
// with the whole workspace, as its owner or a member it reaches
// (workspaceShareMemberSQL — not a restricted member); the page of a web link they came in through, while it is on and unexpired
// (page_link_access.go), as Notion lists a public page someone opened in the
// app.
var bootstrapShareRootsSQL = `
		[[reader_workspaces]] ([[id]]) AS (
			SELECT [[workspace]].[[id]]
			FROM {{leafen_organization_memberships}} [[organization_membership]]
			CROSS JOIN {{leafen_organizations}} [[organization]]
			CROSS JOIN {{leafen_workspaces}} [[workspace]]
			WHERE [[organization_membership]].[[user]] = {:principalId}
				AND [[organization_membership]].[[status]] = {:activeStatus}
				AND [[organization_membership]].[[role]] IN ({:ownerRole}, {:adminRole}, {:securityRole}, {:billingRole}, {:memberRole}, {:guestRole})
				AND [[organization]].[[id]] = [[organization_membership]].[[organization]]
				AND [[workspace]].[[organization]] = [[organization]].[[id]]
				AND [[workspace]].[[deletion_job_id]] = ''
		),
		[[shared_roots]] ([[id]], [[workspace]]) AS (
			SELECT [[permission]].[[page_id]], [[permission]].[[workspace_id]]
			FROM [[reader_workspaces]] CROSS JOIN {{leafen_page_permissions}} [[permission]]
			WHERE [[permission]].[[workspace_id]] = [[reader_workspaces]].[[id]]
				AND [[permission]].[[principal_type]] = 'user' AND [[permission]].[[principal_id]] = {:principalId}
			UNION
			SELECT [[permission]].[[page_id]], [[permission]].[[workspace_id]]
			FROM [[reader_workspaces]] CROSS JOIN {{leafen_page_permissions}} [[permission]]
			WHERE [[permission]].[[workspace_id]] = [[reader_workspaces]].[[id]]
				AND [[permission]].[[principal_type]] = 'group' AND [[permission]].[[principal_id]] IN (
					SELECT [[group_member]].[[group_id]]
					FROM {{leafen_workspace_permission_group_members}} [[group_member]]
					JOIN {{leafen_workspace_permission_groups}} [[permission_group]]
						ON [[permission_group]].[[id]] = [[group_member]].[[group_id]]
					WHERE [[group_member]].[[user_id]] = {:principalId}
						AND [[permission_group]].[[workspace_id]] = [[reader_workspaces]].[[id]]
						AND [[permission_group]].[[deleted_at]] IS NULL)
			UNION
			SELECT [[permission]].[[page_id]], [[permission]].[[workspace_id]]
			FROM [[reader_workspaces]] CROSS JOIN {{leafen_page_permissions}} [[permission]]
			WHERE [[permission]].[[workspace_id]] = [[reader_workspaces]].[[id]]
				AND [[permission]].[[principal_type]] = 'email'
				AND lower(trim([[permission]].[[principal_id]])) IN (` + verifiedAccountEmailSQL("principalId") + `)
			UNION
			SELECT [[permission]].[[page_id]], [[permission]].[[workspace_id]]
			FROM [[reader_workspaces]] CROSS JOIN {{leafen_page_permissions}} [[permission]]
			JOIN {{leafen_workspaces}} [[member_workspace]] ON [[member_workspace]].[[id]]=[[reader_workspaces]].[[id]]
			WHERE [[permission]].[[workspace_id]]=[[reader_workspaces]].[[id]]
				AND [[permission]].[[principal_type]]='workspace' AND [[permission]].[[principal_id]]=[[reader_workspaces]].[[id]]
				AND ([[member_workspace]].[[workspace_owner]]={:principalId} OR ` + workspaceShareMemberSQL("[[reader_workspaces]].[[id]]", "{:principalId}") + `)
			UNION
			SELECT [[link]].[[root_page_id]], [[link]].[[workspace_id]]
			FROM [[reader_workspaces]] CROSS JOIN {{leafen_page_link_visitors}} [[visitor]]
			JOIN {{leafen_public_share_links}} [[link]] ON [[link]].[[id]]=[[visitor]].[[link_id]]
				AND [[link]].[[workspace_id]]=[[reader_workspaces]].[[id]] AND [[link]].[[web_enabled]]=1
				AND ([[link]].[[expires_at]]='' OR [[link]].[[expires_at]]>strftime('%Y-%m-%dT%H:%M:%fZ','now'))
			WHERE [[visitor]].[[workspace_id]]=[[reader_workspaces]].[[id]] AND [[visitor]].[[user_id]]={:principalId}
		)`

// bootstrapSharedWalkSQL names, as shared_pages, the pages a person opens
// only through shares. The walk down from a shared page stops at a page unlinked from its
// parent's sharing; that page opens by its own shares, as a root.
var bootstrapSharedWalkSQL = `
		WITH RECURSIVE` + bootstrapShareRootsSQL + `,
		[[shared_pages]] ([[id]], [[workspace]]) AS (` + bootstrapSharedBaseSQL + `
			UNION
			-- Every page under it however deep, each once.
			SELECT [[child]].[[id]], [[child]].[[workspace]]
			FROM [[shared_pages]]
			JOIN {{leafen_pages}} [[child]]
				ON [[child]].[[workspace]] = [[shared_pages]].[[workspace]]
				AND [[child]].[[parent_id]] = [[shared_pages]].[[id]]
			WHERE ` + bootstrapSharedStepSQL("[[child]]") + `
		)`

// bootstrapSharedBaseSQL selects, after bootstrapShareRootsSQL, the pages
// the shared pages' walk starts from: each page shared with the person, and
// a guest's own private pages meanwhile.
var bootstrapSharedBaseSQL = `
			SELECT [[shared_roots]].[[id]], [[shared_roots]].[[workspace]]
			FROM [[shared_roots]]
			JOIN {{leafen_pages}} [[root_page]] ON [[root_page]].[[id]] = [[shared_roots]].[[id]]
			WHERE ` + bootstrapSidebarKindSQL("[[root_page]]") + `
			UNION
			-- A guest waiting to be a member keeps private pages meanwhile
			-- (pending_guest_private_pages.go).
			SELECT [[own_root]].[[id]], [[own_root]].[[workspace]]
			FROM [[reader_workspaces]]
			JOIN {{leafen_workspaces}} [[own_workspace]] ON [[own_workspace]].[[id]] = [[reader_workspaces]].[[id]]
			JOIN {{leafen_pages}} [[own_root]] ON [[own_root]].[[workspace]] = [[reader_workspaces]].[[id]]
			WHERE [[own_root]].[[parent_id]] = '' AND [[own_root]].[[teamspace_id]] = ''
				AND [[own_root]].[[private_owner_id]] = {:principalId}
				AND ` + bootstrapSidebarKindSQL("[[own_root]]") + `
				AND [[own_workspace]].[[workspace_owner]] != {:principalId}
				AND NOT EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[own_member]]
					WHERE [[own_member]].[[workspace_id]] = [[reader_workspaces]].[[id]] AND [[own_member]].[[user_id]] = {:principalId}
						AND [[own_member]].[[role]] = 'member')
				AND EXISTS (SELECT 1 FROM {{leafen_access_requests}} [[own_request]]
					WHERE [[own_request]].[[workspace_id]] = [[reader_workspaces]].[[id]] AND [[own_request]].[[requester_id]] = {:principalId}
						AND [[own_request]].[[kind]] = 'membership' AND [[own_request]].[[status]] = 'pending')
				AND COALESCE((SELECT [[own_policy]].[[pending_guest_private_pages]] FROM {{leafen_organization_sharing_security_policies}} [[own_policy]]
					WHERE [[own_policy]].[[organization_id]] = [[own_workspace]].[[organization]]), 1) = 1`

// bootstrapSharedStepSQL says a page (an alias of leafen_pages) right under
// one the shared pages' walk reaches is reached too: one the sidebar lists,
// not unlinked from its parent's sharing.
func bootstrapSharedStepSQL(child string) string {
	return bootstrapSidebarKindSQL(child) + `
				AND NOT EXISTS (
					SELECT 1 FROM {{leafen_page_share_unlinks}} [[unlink]] WHERE [[unlink]].[[page_id]] = ` + child + `.[[id]]
				)`
}

// bootstrapSharedPageIDsSQL selects, after bootstrapSharedWalkSQL, the IDs
// of every page it names.
const bootstrapSharedPageIDsSQL = `SELECT [[id]] FROM [[shared_pages]]`

// bootstrapSharedCandidatesQuery selects, in the order the shared pages'
// window goes, the next {:candidates} pages the walks reach after the
// cursor, by ID and creation alone: the window's longer checks and columns
// then run on them only (bootstrapSharedPagesQuery), where they ran on every
// page shared with the person before the order cut them (2026-11-18: about
// 10,000 shared pages, a window of 1,000 took 0.7 s at the NAS).
var bootstrapSharedCandidatesQuery = bootstrapSharedWalkSQL + `
		SELECT [[candidate]].[[id]], [[candidate]].[[created]]
		FROM (` + bootstrapSharedPageIDsSQL + `) [[shared]]
		CROSS JOIN {{leafen_pages}} [[candidate]]
		WHERE [[candidate]].[[id]] = [[shared]].[[id]]
			AND (
				{:afterCreated} = '' OR [[candidate]].[[created]] > {:afterCreated}
				OR ([[candidate]].[[created]] = {:afterCreated} AND [[candidate]].[[id]] > {:afterId})
			)
		ORDER BY [[candidate]].[[created]], [[candidate]].[[id]]
		LIMIT {:candidates}
	`

// bootstrapSharedPagesQuery reads, of the candidate pages {:ids} (a JSON
// list), those of the window, as loadBootstrapSharedPages says.
var bootstrapSharedPagesQuery = `
		SELECT ` + bootstrapPageColumnsSQL + `
		FROM json_each({:ids}) [[shared]]
		CROSS JOIN {{leafen_pages}} [[page]]
		CROSS JOIN {{leafen_workspaces}} [[workspace]]
		LEFT JOIN {{leafen_page_favorites}} [[favorite_state]]
			ON [[favorite_state]].[[page_id]] = [[page]].[[id]]
			AND [[favorite_state]].[[principal_id]] = {:principalId}
		WHERE [[page]].[[id]] = [[shared]].[[value]]
			AND [[workspace]].[[id]] = [[page]].[[workspace]]
			AND [[page]].[[owner]] = [[workspace]].[[owner]]
			AND [[page]].[[notion_import_staging]] = FALSE
			AND [[page]].[[deletion_job_id]] = ''
			AND ` + pageNotHeldForDeletionSQL("[[page]]", "[[page]].[[workspace]]") + `
			AND ` + bootstrapPageCompleteSQL + `
			AND NOT ` + bootstrapPageSeenSQL("[[page]]", "{:principalId}") + `
		ORDER BY [[page]].[[created]], [[page]].[[id]]
		LIMIT {:limit}
	`

// loadBootstrapSharedPages reads, after the cursor, the pages a person
// opens only through shares that name them, in the workspaces of the
// organizations they are active in: each shared page and the pages under
// it, leaving out pages their teamspaces or private pages already show. The
// walk names the next candidates in order; the window takes those it keeps,
// and reads more candidates, four times as many, only while too few were
// kept. A page under a page in the trash the person can't open says it is
// in the trash itself (markSharedPagesUnderUnseenTrash).
func loadBootstrapSharedPages(
	app core.App,
	principalID string,
	cursor bootstrapCursor,
	limit int,
) ([]storedBootstrapPage, bool, error) {
	rows := []storedBootstrapPage{}
	params := bootstrapSharedParams(dbx.Params{"principalId": principalID})
	afterCreated, afterID := cursor.SharedCreated, cursor.SharedPageID
	for read := 2 * (limit + 1); ; read *= 4 {
		candidates := []struct {
			ID      string `db:"id"`
			Created string `db:"created"`
		}{}
		params["afterCreated"], params["afterId"], params["candidates"] = afterCreated, afterID, read
		if err := app.DB().NewQuery(bootstrapSharedCandidatesQuery).Bind(params).All(&candidates); err != nil {
			return nil, false, err
		}
		ids := make([]string, len(candidates))
		for index, candidate := range candidates {
			ids[index] = candidate.ID
		}
		encoded, err := json.Marshal(ids)
		if err != nil {
			return nil, false, err
		}
		window := []storedBootstrapPage{}
		if err := app.DB().NewQuery(bootstrapSharedPagesQuery).Bind(dbx.Params{
			"principalId": principalID, "ids": string(encoded), "limit": limit + 1 - len(rows),
		}).All(&window); err != nil {
			return nil, false, err
		}
		rows = append(rows, window...)
		if len(rows) > limit {
			return rows[:limit], false, markSharedPagesUnderUnseenTrash(app, principalID, rows[:limit])
		}
		if len(candidates) < read {
			return rows, true, markSharedPagesUnderUnseenTrash(app, principalID, rows)
		}
		afterCreated, afterID = candidates[len(candidates)-1].Created, candidates[len(candidates)-1].ID
	}
}

// bootstrapGuestWorkspaceIDsSQL selects, after bootstrapShareRootsSQL, the
// workspaces where a live page (out of the trash with every page above it,
// pageOutOfTrashPathSQL) is shared with the person, each once however
// many of its pages are (2026-11-21: a guest with two pages of a workspace
// had it twice in the page list's workspaces and in the workspace menu).
var bootstrapGuestWorkspaceIDsSQL = `
			SELECT DISTINCT [[shared_roots]].[[workspace]] AS [[id]] FROM [[shared_roots]]
			JOIN {{leafen_pages}} [[root_page]] ON [[root_page]].[[id]] = [[shared_roots]].[[id]]
			WHERE [[root_page]].[[notion_import_staging]] = FALSE AND [[root_page]].[[deletion_job_id]] = ''
				AND ` + pageOutOfTrashPathSQL("[[root_page]].[[id]]")

// hasBootstrapGuestShare reports whether a live page in the workspace is
// shared with the person, the share that makes them a guest there.
func hasBootstrapGuestShare(app core.App, principalID, workspaceID string) (bool, error) {
	ids := []string{}
	err := app.DB().NewQuery(`
		WITH` + bootstrapShareRootsSQL + `
		SELECT [[guest]].[[id]] FROM (` + bootstrapGuestWorkspaceIDsSQL + `) [[guest]]
		WHERE [[guest]].[[id]] = {:workspaceId}
		LIMIT 1
	`).Bind(dbx.Params{
		"principalId": principalID, "activeStatus": organizationMemberStatusActive,
		"ownerRole": organizationRoleOwner, "adminRole": organizationRoleAdmin,
		"securityRole": organizationRoleSecurityAdmin, "billingRole": organizationRoleBillingAdmin,
		"memberRole": organizationRoleMember, "guestRole": organizationRoleGuest, "workspaceId": workspaceID,
	}).Column(&ids)
	return len(ids) > 0, err
}

// loadBootstrapGuestWorkspaces reads the workspaces a person is a guest in:
// those of the organizations they are active in where a page is shared with
// them but they are neither the workspace's owner nor its member — every one,
// a window at a time.
func loadBootstrapGuestWorkspaces(app core.App, principalID string) ([]storedBootstrapWorkspace, error) {
	all := []storedBootstrapWorkspace{}
	afterCreated, afterID := "", ""
	for {
		window, err := loadBootstrapGuestWorkspaceWindow(app, principalID, afterCreated, afterID, maxBootstrapWorkspaces)
		if err != nil {
			return nil, err
		}
		all = append(all, window...)
		if len(window) < maxBootstrapWorkspaces {
			return all, nil
		}
		afterCreated, afterID = window[len(window)-1].Created, window[len(window)-1].ID
	}
}

// loadBootstrapGuestWorkspaceWindow reads the guest workspaces after one, in
// the order they were made.
func loadBootstrapGuestWorkspaceWindow(
	app core.App, principalID, afterCreated, afterID string, limit int,
) ([]storedBootstrapWorkspace, error) {
	rows := []storedBootstrapWorkspace{}
	err := app.DB().NewQuery(`
		WITH` + bootstrapShareRootsSQL + `
		SELECT [[workspace]].[[id]], [[workspace]].[[organization]], [[workspace]].[[name]],
			[[workspace]].[[icon]], [[workspace]].[[domain]], [[settings]].[[revision]] AS [[settings_revision]],
			[[workspace]].[[workspace_owner]] AS [[owner]], {:guestRole} AS [[role]], [[workspace]].[[created]]
		FROM (` + bootstrapGuestWorkspaceIDsSQL + `) [[guest]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[guest]].[[id]]
		JOIN {{leafen_workspace_settings_state}} [[settings]] ON [[settings]].[[workspace_id]] = [[workspace]].[[id]]
		WHERE [[workspace]].[[workspace_owner]] != {:principalId}
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_workspace_memberships}} [[workspace_membership]]
				WHERE [[workspace_membership]].[[workspace_id]] = [[workspace]].[[id]]
					AND [[workspace_membership]].[[user_id]] = {:principalId})
			AND ({:afterCreated} = '' OR [[workspace]].[[created]] > {:afterCreated}
				OR ([[workspace]].[[created]] = {:afterCreated} AND [[workspace]].[[id]] > {:afterId}))
		ORDER BY [[workspace]].[[created]], [[workspace]].[[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"afterCreated": afterCreated, "afterId": afterID,
		"principalId": principalID, "activeStatus": organizationMemberStatusActive,
		"ownerRole": organizationRoleOwner, "adminRole": organizationRoleAdmin,
		"securityRole": organizationRoleSecurityAdmin, "billingRole": organizationRoleBillingAdmin,
		"memberRole": organizationRoleMember, "guestRole": organizationRoleGuest, "limit": limit,
	}).All(&rows)
	return rows, err
}

// sharedPrivatePageIDs are the reader's private pages among rows that are
// shared with someone, which the sidebar shows as shared. One open to the
// whole workspace stays under private pages, as Notion's (seen 2026-11-05).
func sharedPrivatePageIDs(app core.App, rows []storedBootstrapPage) ([]string, error) {
	params := dbx.Params{}
	placeholders := []string{}
	for _, row := range rows {
		if row.TeamspaceID != "" {
			continue
		}
		key := fmt.Sprintf("privatePage%d", len(placeholders))
		params[key] = row.ID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	ids := []string{}
	if len(placeholders) == 0 {
		return ids, nil
	}
	err := app.DB().NewQuery(`
		SELECT DISTINCT [[page_id]] FROM {{leafen_page_permissions}}
		WHERE [[page_id]] IN (` + joinSQL(placeholders) + `)
			AND [[principal_type]] IN ('user', 'group', 'email')
		ORDER BY [[page_id]]
	`).Bind(params).Column(&ids)
	return ids, err
}

// workspaceWidePageIDs are the rows opened to everyone in the workspace and
// not shared with this person by name. They open and search finds them, but
// the sidebar's shared pages leave them out, as Notion's (seen 2026-11-05).
func workspaceWidePageIDs(app core.App, principalID string, rows []storedBootstrapPage) (map[string]bool, error) {
	params := dbx.Params{"principalId": principalID}
	placeholders := make([]string, 0, len(rows))
	for index, row := range rows {
		key := fmt.Sprintf("widePage%d", index)
		params[key] = row.ID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	if len(placeholders) == 0 {
		return nil, nil
	}
	ids := []string{}
	err := app.DB().NewQuery(`
		SELECT [[wide]].[[page_id]] FROM {{leafen_page_permissions}} [[wide]]
		WHERE [[wide]].[[page_id]] IN (` + joinSQL(placeholders) + `) AND [[wide]].[[principal_type]] = 'workspace'
			AND NOT EXISTS (SELECT 1 FROM {{leafen_page_permissions}} [[named]]
				WHERE [[named]].[[page_id]] = [[wide]].[[page_id]] AND (
					([[named]].[[principal_type]] = 'user' AND [[named]].[[principal_id]] = {:principalId})
					OR ([[named]].[[principal_type]] = 'email' AND lower(trim([[named]].[[principal_id]])) IN (` + verifiedAccountEmailSQL("principalId") + `))
					OR ([[named]].[[principal_type]] = 'group' AND [[named]].[[principal_id]] IN (
						SELECT [[group_member]].[[group_id]] FROM {{leafen_workspace_permission_group_members}} [[group_member]]
						WHERE [[group_member]].[[user_id]] = {:principalId}))))
	`).Bind(params).Column(&ids)
	if err != nil {
		return nil, err
	}
	wide := make(map[string]bool, len(ids))
	for _, id := range ids {
		wide[id] = true
	}
	return wide, nil
}
