package app

import (
	"fmt"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// "검색 결과에서 숨기기" on a page open to everyone in the workspace, as
// Notion's (seen 2026-11-05): its address still opens it, but search shows it
// only to those it is shared with some other way — a share of their own, or
// the page's place: their own private pages or a teamspace that opens it to
// them (2026-11-15: a person's own private page they had shared
// with the workspace and hidden went missing from their own search).

// workspaceSearchHiddenGrants holds the workspaces, among the pages a search
// judges, with a page kept out of search; most have none, and cost one read.
type workspaceSearchHiddenGrants map[string]bool

func loadWorkspaceSearchHiddenGrants(app core.App, pageIDs []string) (workspaceSearchHiddenGrants, error) {
	params := dbx.Params{}
	keys := make([]string, 0, len(pageIDs))
	for index, id := range pageIDs {
		key := fmt.Sprintf("page%d", index)
		params[key] = id
		keys = append(keys, "{:"+key+"}")
	}
	if len(keys) == 0 {
		return nil, nil
	}
	workspaces := []string{}
	err := app.DB().NewQuery(`SELECT DISTINCT [[permission]].[[workspace_id]] FROM {{leafen_page_permissions}} [[permission]]
	 WHERE [[permission]].[[hide_from_search]]=1 AND [[permission]].[[workspace_id]] IN (
	  SELECT [[page]].[[workspace]] FROM {{` + pagesCollection + `}} [[page]] WHERE [[page]].[[id]] IN (` + joinSQL(keys) + `))`).Bind(params).Column(&workspaces)
	if err != nil || len(workspaces) == 0 {
		return nil, err
	}
	grants := workspaceSearchHiddenGrants{}
	for _, id := range workspaces {
		grants[id] = true
	}
	return grants, nil
}

// keepsFromSearch says whether a page the person may open reaches them only
// through a workspace-wide grant kept out of search. The nearest such grant on
// the way up decides.
func (grants workspaceSearchHiddenGrants) keepsFromSearch(app core.App, principalID, pageID string) (bool, error) {
	if len(grants) == 0 {
		return false, nil
	}
	var workspaceID string
	if err := app.DB().NewQuery(`SELECT [[workspace]] FROM {{` + pagesCollection + `}} WHERE [[id]]={:id}`).Bind(dbx.Params{"id": pageID}).Row(&workspaceID); err != nil {
		return false, err
	}
	if !grants[workspaceID] {
		return false, nil
	}
	path, err := loadPageAccessAncestorIDs(app, workspaceID, pageID)
	if err != nil {
		return false, err
	}
	rows := []struct {
		PageID string `db:"page_id"`
		Hide   bool   `db:"hide_from_search"`
	}{}
	params := dbx.Params{"workspace": workspaceID}
	keys := make([]string, 0, len(path))
	for index, id := range path {
		key := fmt.Sprintf("path%d", index)
		params[key] = id
		keys = append(keys, "{:"+key+"}")
	}
	if err := app.DB().NewQuery(`SELECT [[page_id]],[[hide_from_search]] FROM {{leafen_page_permissions}}
	 WHERE [[workspace_id]]={:workspace} AND [[principal_type]]='workspace' AND [[principal_id]]={:workspace}
	 AND [[page_id]] IN (` + joinSQL(keys) + `)`).Bind(params).All(&rows); err != nil {
		return false, err
	}
	nearest := -1
	hidden := false
	for _, row := range rows {
		if at := slices.Index(path, row.PageID); at >= 0 && (nearest < 0 || at < nearest) {
			nearest, hidden = at, row.Hide
		}
	}
	if !hidden {
		return false, nil
	}
	if placed, err := pagePlaceOpensTo(app, principalID, workspaceID, pageID); err != nil || placed {
		return false, err
	}
	principals, err := loadPageSharePrincipals(app, workspaceID, principalID)
	if err != nil {
		return false, err
	}
	principals.WorkspaceMember = false
	condition := principals.condition("[[permission]]", "workspace", params)
	var shared int
	if err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_page_permissions}} [[permission]]
	 WHERE [[permission]].[[page_id]] IN (` + joinSQL(keys) + `) AND ` + condition + `)`).Bind(params).Row(&shared); err != nil {
		return false, err
	}
	return shared == 0, nil
}

// pagePlaceOpensTo says whether a page's place opens it to the person
// without any share, as decidePageAccessRole finds: their own private page,
// a teamspace that gives them a role on it.
func pagePlaceOpensTo(app core.App, principalID, workspaceID, pageID string) (bool, error) {
	page, err := findRecordRemembered(app, pagesCollection, pageID)
	if err != nil {
		return false, err
	}
	if privatePageAccessRole(page, principalID) >= pageAccessRoleView {
		return true, nil
	}
	teamspaceID := page.GetString("teamspace_id")
	if teamspaceID == "" {
		return false, nil
	}
	_, role, err := effectiveTeamspaceRoleOnPage(app, principalID, workspaceID, teamspaceID, pageID)
	if err != nil && !isPermissionDenied(err) {
		return false, err
	}
	return role >= pageAccessRoleView, nil
}
