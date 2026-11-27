package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page a teamspace's owners or members have no access to
// (schema_baseline.json) leaves their sidebar, as it leaves
// Notion's (seen 2026-10-13: its parent showed "하위 페이지 없음"). The
// sidebar's visibility index lists a teamspace's pages to everyone in it;
// where any page of the teamspace takes a group's access away, the pages a
// person can't open are taken out as each window is read.

// teamspaceNoAccessPageIDs are those of the pages the person can't open in a
// teamspace where some page gives one of its groups no access.
func teamspaceNoAccessPageIDs(app core.App, principalID string, pageIDs []string) (map[string]bool, error) {
	hidden := map[string]bool{}
	if len(pageIDs) == 0 {
		return hidden, nil
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	candidates := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[page]].[[id]] FROM {{leafen_pages}} [[page]]
		WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[page]].[[teamspace_id]] != ''
			AND EXISTS (
				SELECT 1 FROM {{leafen_page_teamspace_roles}} [[roles]]
				WHERE [[roles]].[[workspace_id]] = [[page]].[[workspace]] AND [[roles]].[[teamspace_id]] = [[page]].[[teamspace_id]]
					AND ([[roles]].[[owner_role]] = 'none' OR [[roles]].[[member_role]] = 'none'))
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&candidates); err != nil {
		return nil, err
	}
	for _, pageID := range candidates {
		if _, err := authorizePageAccessRole(app, principalID, pageID, pageAccessRoleView); isPermissionDenied(err) {
			hidden[pageID] = true
		} else if err != nil {
			return nil, err
		}
	}
	return hidden, nil
}

// listedPagesHidden are those of some pages listed by where they are that
// the person can't open: in a teamspace where a group has no access.
func listedPagesHidden(app core.App, principalID string, pageIDs []string) (map[string]bool, error) {
	return teamspaceNoAccessPageIDs(app, principalID, pageIDs)
}

// withoutHiddenBootstrapPages keeps of a bootstrap window the pages the
// person may open.
func withoutHiddenBootstrapPages(app core.App, principalID string, rows []storedBootstrapPage) ([]storedBootstrapPage, error) {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	hidden, err := listedPagesHidden(app, principalID, ids)
	if err != nil || len(hidden) == 0 {
		return rows, err
	}
	kept := make([]storedBootstrapPage, 0, len(rows))
	for _, row := range rows {
		if !hidden[row.ID] {
			kept = append(kept, row)
		}
	}
	return kept, nil
}
