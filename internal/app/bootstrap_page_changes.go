package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A screen follows another person's page change by reading only the pages
// changed since its list (page_list_changes_schema.go), in the list's own
// shape and under the same rules of who sees what, and takes them over the
// pages it lists: one it may no longer see comes back among removedPageIds.
// Its change check asks for them (workspace_change_catchup.go), so a rename,
// move, trash, deletion for good or favorite shows in about the time of one
// request (2026-11-15, NAS condition: the whole list again took 17–25 s in a
// workspace of 47,000 pages). The screen reads the whole list as before when
// these can't say what changed: a change of who may see what (sharing,
// teamspaces, groups, a wiki made), more than a window's worth, or a list
// older than the changes kept.

// currentPageListChangeSeq is the number of the last page list change kept.
func currentPageListChangeSeq(app core.App) (int64, error) {
	var seq int64
	err := app.DB().NewQuery(`
		SELECT [[seq]] FROM {{leafen_page_list_change_sequence}} WHERE [[singleton]] = 'pages'
	`).Row(&seq)
	return seq, err
}

// bootstrapPageChanges reads, for a member of a workspace, the pages of its
// sidebar changed since a change number, with the number now; false when
// the screen has to read its whole list.
func bootstrapPageChanges(app core.App, principalID, workspaceID string, since int64) (*bootstrapResponse, bool, error) {
	current, err := currentPageListChangeSeq(app)
	if err != nil || since <= 0 || since > current {
		return nil, false, err
	}
	changes := []struct {
		PageID     string `db:"page_id"`
		SubtreeSeq int64  `db:"subtree_seq"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], [[subtree_seq]] FROM {{leafen_page_list_changes}}
			INDEXED BY [[idx_leafen_page_list_changes_seq]]
		WHERE [[workspace_id]] = {:workspaceId} AND [[seq]] > {:since}
		ORDER BY [[seq]]
		LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "since": since, "limit": maxBootstrapPages + 1}).All(&changes); err != nil {
		return nil, false, err
	}
	if len(changes) > maxBootstrapPages {
		return nil, false, nil
	}
	ids := make([]string, 0, len(changes))
	roots := []string{}
	for _, change := range changes {
		if change.PageID == "" {
			return nil, false, nil
		}
		ids = append(ids, change.PageID)
		if change.SubtreeSeq > since {
			roots = append(roots, change.PageID)
		}
	}
	// The pages under one moved, deleted for good or brought back, whose
	// access follows it.
	if len(roots) > 0 {
		under, err := pagesUnder(app, workspaceID, roots, maxBootstrapPages+1)
		if err != nil || len(under)+len(ids) > maxBootstrapPages {
			return nil, false, err
		}
		ids = append(ids, under...)
	}
	response := &bootstrapResponse{
		Workspaces: []workspaceResponse{}, Pages: []pageResponse{}, PageFavorites: []pageFavoriteProjection{},
		WorkspaceRefreshTokens: []workspaceRefreshTokenResponse{}, PageChangeSeq: current, RemovedPageIDs: []string{},
	}
	if len(ids) == 0 {
		return response, true, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, false, err
	}
	params := dbx.Params{"principalId": principalID, "workspaceId": workspaceID, "ids": string(encoded)}
	seen := []storedBootstrapPage{}
	// The changed pages are read by their ids, the workspace and the import
	// mark after the row (+): SQLite, which has no statistics, took the
	// (workspace, import) index and read the workspace's every page.
	if err := app.DB().NewQuery(`
		SELECT ` + bootstrapPageColumnsSQL + `
		FROM {{leafen_pages}} [[page]]
		CROSS JOIN {{leafen_workspaces}} [[workspace]]
		LEFT JOIN {{leafen_page_favorites}} [[favorite_state]]
			ON [[favorite_state]].[[page_id]] = [[page]].[[id]]
			AND [[favorite_state]].[[principal_id]] = {:principalId}
		WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND +[[page]].[[workspace]] = {:workspaceId}
			AND [[workspace]].[[id]] = [[page]].[[workspace]]
			AND [[workspace]].[[deletion_job_id]] = ''
			AND [[page]].[[owner]] = [[workspace]].[[owner]]
			AND +[[page]].[[notion_import_staging]] = FALSE
			AND ` + bootstrapPageSeenSQL("[[page]]", "{:principalId}") + `
			AND ` + pageNotHeldForDeletionSQL("[[page]]", "{:workspaceId}") + `
			AND ` + bootstrapPageCompleteSQL + `
		ORDER BY [[page]].[[created]], [[page]].[[id]]
	`).Bind(params).All(&seen); err != nil {
		return nil, false, err
	}
	visible, err := withoutHiddenBootstrapPages(app, principalID, seen)
	if err != nil {
		return nil, false, err
	}
	if err := appendBootstrapPages(app, principalID, response, visible); err != nil {
		return nil, false, err
	}
	if response.SharedPageIDs, err = sharedPrivatePageIDs(app, visible); err != nil {
		return nil, false, err
	}
	// Those of the others shared with the person, which their sidebar shows
	// only so (bootstrapSharedPagesQuery).
	others := make([]string, 0, len(ids))
	for _, id := range ids {
		if !slices.ContainsFunc(seen, func(row storedBootstrapPage) bool { return row.ID == id }) {
			others = append(others, id)
		}
	}
	shared, err := loadChangedSharedPages(app, principalID, others)
	if err != nil {
		return nil, false, err
	}
	if err := appendBootstrapPages(app, principalID, response, shared); err != nil {
		return nil, false, err
	}
	owned, err := privatelyOwnedPageIDs(app, principalID, shared)
	if err != nil {
		return nil, false, err
	}
	wide, err := workspaceWidePageIDs(app, principalID, shared)
	if err != nil {
		return nil, false, err
	}
	for _, row := range shared {
		if !owned[row.ID] {
			response.SharedPageIDs = append(response.SharedPageIDs, row.ID)
			if wide[row.ID] {
				response.WorkspaceWidePageIDs = append(response.WorkspaceWidePageIDs, row.ID)
			}
		}
	}
	listed := make(map[string]bool, len(response.Pages))
	for _, page := range response.Pages {
		listed[page.ID] = true
	}
	// A database page the sidebar doesn't carry comes with its favorite
	// (changedDatabasePageFavorites); the rest it may no longer see, each
	// once, what they hold not read.
	unlisted := slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return listed[id] })
	favorites, err := changedDatabasePageFavorites(app, principalID, unlisted)
	if err != nil {
		return nil, false, err
	}
	said := make(map[string]bool, len(unlisted))
	for _, favorite := range favorites {
		response.PageFavorites, said[favorite.PageID] = append(response.PageFavorites, favorite), true
	}
	for _, id := range unlisted {
		if !said[id] {
			response.RemovedPageIDs = append(response.RemovedPageIDs, id)
			said[id] = true
		}
	}
	if response.DatabaseFavorites, err = bootstrapDatabaseFavorites(app, principalID, listed); err != nil {
		return nil, false, err
	}
	if response.InlineDatabases, err = bootstrapInlineDatabases(app, principalID, listed); err != nil {
		return nil, false, err
	}
	return response, true, nil
}

// bootstrapSharedParams are the shared walk's parameters (loadBootstrapSharedPages) with these.
func bootstrapSharedParams(params dbx.Params) dbx.Params {
	shared := dbx.Params{
		"activeStatus": organizationMemberStatusActive,
		"ownerRole":    organizationRoleOwner, "adminRole": organizationRoleAdmin,
		"securityRole": organizationRoleSecurityAdmin, "billingRole": organizationRoleBillingAdmin,
		"memberRole": organizationRoleMember, "guestRole": organizationRoleGuest,
	}
	for key, value := range params {
		shared[key] = value
	}
	return shared
}

// pagesUnder are the IDs of the pages under some pages of a workspace,
// however deep, each once, up to a limit.
func pagesUnder(app core.App, workspaceID string, roots []string, limit int) ([]string, error) {
	encoded, err := json.Marshal(roots)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	err = app.DB().NewQuery(`
		WITH RECURSIVE [[under]] ([[id]]) AS (
			SELECT [[value]] FROM json_each({:roots})
			UNION
			SELECT [[child]].[[id]] FROM [[under]]
			CROSS JOIN {{leafen_pages}} [[child]]
			WHERE [[child]].[[workspace]] = {:workspaceId} AND [[child]].[[parent_id]] = [[under]].[[id]]
		)
		SELECT [[id]] FROM [[under]] WHERE [[id]] NOT IN (SELECT [[value]] FROM json_each({:roots}))
		LIMIT {:limit}
	`).Bind(dbx.Params{"roots": string(encoded), "workspaceId": workspaceID, "limit": limit}).Column(&ids)
	return ids, err
}

// loadChangedSharedPages reads, of some changed pages, those shared with the
// person that their sidebar shows only so, as the list's shared part does.
func loadChangedSharedPages(app core.App, principalID string, ids []string) ([]storedBootstrapPage, error) {
	shared := []storedBootstrapPage{}
	if len(ids) == 0 {
		return shared, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	err = app.DB().NewQuery(bootstrapSharedWalkSQL + `
		SELECT ` + bootstrapPageColumnsSQL + `
		FROM (` + bootstrapSharedPageIDsSQL + `) [[shared]]
		CROSS JOIN {{leafen_pages}} [[page]]
		CROSS JOIN {{leafen_workspaces}} [[workspace]]
		LEFT JOIN {{leafen_page_favorites}} [[favorite_state]]
			ON [[favorite_state]].[[page_id]] = [[page]].[[id]]
			AND [[favorite_state]].[[principal_id]] = {:principalId}
		WHERE [[page]].[[id]] = [[shared]].[[id]]
			AND [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND [[workspace]].[[id]] = [[page]].[[workspace]]
			AND [[page]].[[owner]] = [[workspace]].[[owner]]
			AND [[page]].[[notion_import_staging]] = FALSE
			AND [[page]].[[deletion_job_id]] = ''
			AND ` + pageNotHeldForDeletionSQL("[[page]]", "[[page]].[[workspace]]") + `
			AND ` + bootstrapPageCompleteSQL + `
			AND NOT ` + bootstrapPageSeenSQL("[[page]]", "{:principalId}") + `
		ORDER BY [[page]].[[created]], [[page]].[[id]]
	`).Bind(bootstrapSharedParams(dbx.Params{"principalId": principalID, "ids": string(encoded)})).All(&shared)
	return shared, err
}
