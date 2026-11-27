package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A member's search judges hundreds of pages their teamspaces don't open, and where anything is shared with them each needs the shares
// on it and on the pages above it: read page by page, that was four reads a
// page (2026-10-17: 900 pages judged so in one search). For pages whose
// records a read scope holds, the paths above them and the role each page's
// shares give the reader are read at once into its memo, where
// loadPageAccessAncestorIDs and pageShareRoleOn then find them.

type pageShareRoleMemoKey struct{ workspaceID, principalID, pageID string }

// primePageShareRoles reads the paths above pages and the roles their
// shares give a person at once. Outside a read scope, or where nothing in
// the workspace is shared with them, it reads nothing: there the full check
// reads nothing either (effectiveUserPageAccessRole, pageShareRoleOn). A
// workspace's owner is primed as anyone: another's private page is closed
// to them too (privatePageAccessRole), and their search judged such pages
// shared with others by reading each one's path and shares.
func primePageShareRoles(app core.App, principalID string, pageIDs []string) error {
	holder, ok := app.(accessMemoHolder)
	if !ok || principalID == "" {
		return nil
	}
	memo := holder.accessMemo()
	byWorkspace := map[string][]string{}
	for _, pageID := range pageIDs {
		entry, held := memo[recordMemoKey{collection: pagesCollection, id: pageID}]
		page, _ := entry.value.(*core.Record)
		if !held || entry.err != nil || page == nil {
			continue
		}
		workspaceID := page.GetString("workspace")
		if _, known := memo[pageAncestorsMemoKey{workspaceID, pageID}]; !known {
			byWorkspace[workspaceID] = append(byWorkspace[workspaceID], pageID)
		}
	}
	for workspaceID, ids := range byWorkspace {
		shares, err := pageSharesExist(app, workspaceID, principalID)
		if err != nil {
			return err
		}
		if !shares {
			continue
		}
		above, err := loadPagesAccessAncestorIDs(app, workspaceID, ids)
		if err != nil {
			return err
		}
		wanted := []string{}
		seen := map[string]bool{}
		for _, pageID := range ids {
			chain, found := above[pageID]
			if !found {
				memo[pageAncestorsMemoKey{workspaceID, pageID}] = accessMemoEntry{err: permissionDenied()}
				continue
			}
			memo[pageAncestorsMemoKey{workspaceID, pageID}] = accessMemoEntry{value: chain}
			for _, id := range chain {
				if _, known := memo[pageShareRoleMemoKey{workspaceID, principalID, id}]; !known && !seen[id] {
					seen[id] = true
					wanted = append(wanted, id)
				}
			}
		}
		if len(wanted) == 0 {
			continue
		}
		roles, err := readPageShareRoles(app, workspaceID, principalID, wanted)
		if err != nil {
			return err
		}
		for _, id := range wanted {
			memo[pageShareRoleMemoKey{workspaceID, principalID, id}] = accessMemoEntry{value: roles[id]}
		}
	}
	return nil
}

type pageSharesMemoKey struct{ workspaceID, principalID string }

// pageSharesExist is whether anything in a workspace is shared with a
// person, or reached through a link they came in by: where nothing is, a
// page their teamspaces and private pages don't open is closed to them
// without looking for the shares of it and the pages above it (2026-10-15:
// looked for page by page, a search by members took seconds).
func pageSharesExist(app core.App, workspaceID, principalID string) (bool, error) {
	return remembered(app, pageSharesMemoKey{workspaceID, principalID}, func() (bool, error) {
		principals, err := loadPageSharePrincipals(app, workspaceID, principalID)
		if err != nil {
			return false, err
		}
		params := dbx.Params{"workspaceId": workspaceID, "userId": principalID}
		var exists bool
		err = app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_page_permissions}} [[permission]] WHERE ` +
			principals.condition("[[permission]]", "workspaceId", params) + `)
			OR EXISTS (SELECT 1 FROM {{leafen_page_link_visitors}} WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId})`).
			Bind(params).Row(&exists)
		return exists, err
	})
}

// readPageShareRoles is, by page, the widest role each page's own shares
// give a person: the query readPageShareRole asks of the pages together.
func readPageShareRoles(app core.App, workspaceID, principalID string, pageIDs []string) (map[string]int, error) {
	principals, err := loadPageSharePrincipals(app, workspaceID, principalID)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{"workspaceId": workspaceID, "pageIds": string(encoded)}
	rows := []struct {
		PageID string `db:"page_id"`
		Role   int    `db:"role"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[permission]].[[page_id]], MAX(CASE [[permission]].[[role]]
			WHEN 'view' THEN 1 WHEN 'comment' THEN 2
			WHEN 'edit' THEN 3 WHEN 'full_access' THEN 4 ELSE 0 END) AS [[role]]
		FROM {{leafen_page_permissions}} [[permission]]
		INDEXED BY [[idx_leafen_page_permissions_actor_page]]
		WHERE ` + principals.condition("[[permission]]", "workspaceId", params) + `
			AND [[permission]].[[page_id]] IN (SELECT [[value]] FROM json_each({:pageIds}))
		GROUP BY [[permission]].[[page_id]]
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	roles := make(map[string]int, len(rows))
	for _, row := range rows {
		roles[row.PageID] = row.Role
	}
	return roles, nil
}

// primedPageShareRole is the widest role the shares of the pages give, when
// a read scope holds each page's.
func primedPageShareRole(app core.App, workspaceID, principalID string, pageIDs []string) (int, bool) {
	holder, ok := app.(accessMemoHolder)
	if !ok || len(pageIDs) == 0 {
		return 0, false
	}
	memo := holder.accessMemo()
	role := 0
	for _, id := range pageIDs {
		entry, known := memo[pageShareRoleMemoKey{workspaceID, principalID, id}]
		if !known {
			return 0, false
		}
		role = max(role, entry.value.(int))
	}
	return role, true
}
