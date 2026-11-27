package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Page and row-page shares use the same ancestor path, to the top however
// deep (each page once, so a cycle ends). It ends at a page whose sharing is
// unlinked from its parent's (schema_baseline.json): that page's own
// shares stand for everything above it. The path is the page first, then
// each parent.
func loadPageAccessAncestorIDs(app core.App, workspaceID, pageID string) ([]string, error) {
	ids, err := remembered(app, pageAncestorsMemoKey{workspaceID, pageID}, func() ([]string, error) {
		return readPageAccessAncestorIDs(app, workspaceID, pageID)
	})
	return slices.Clone(ids), err
}

// The path above a page, kept for a read (access_memo.go); primePageShareRoles
// reads many at once.
type pageAncestorsMemoKey struct{ workspaceID, pageID string }

func readPageAccessAncestorIDs(app core.App, workspaceID, pageID string) ([]string, error) {
	ancestors := []storedPageAccessAncestor{}
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[ancestors]] ([[id]], [[parent_id]]) AS (
			SELECT [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + ` FROM {{leafen_pages}} [[page]]
			WHERE [[page]].[[id]]={:pageId} AND [[page]].[[workspace]]={:workspaceId}
			UNION
			SELECT [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `
			FROM {{leafen_pages}} [[page]]
			JOIN [[ancestors]] ON [[page]].[[id]]=[[ancestors]].[[parent_id]]
			WHERE [[ancestors]].[[parent_id]] != ''
				AND [[page]].[[workspace]]={:workspaceId}
				AND NOT EXISTS (
					SELECT 1 FROM {{leafen_page_share_unlinks}} [[unlink]] WHERE [[unlink]].[[page_id]]=[[ancestors]].[[id]]
				)
		)
		SELECT [[id]], [[parent_id]] FROM [[ancestors]]
	`).Bind(dbx.Params{"pageId": pageID, "workspaceId": workspaceID}).All(&ancestors)
	if err != nil {
		return nil, err
	}
	parents := make(map[string]string, len(ancestors))
	for _, ancestor := range ancestors {
		parents[ancestor.ID] = ancestor.ParentID
	}
	if _, found := parents[pageID]; !found {
		return nil, permissionDenied()
	}
	pageIDs := make([]string, 0, len(ancestors))
	seen := make(map[string]bool, len(ancestors))
	for current := pageID; current != "" && !seen[current]; current = parents[current] {
		if _, found := parents[current]; !found {
			break
		}
		seen[current] = true
		pageIDs = append(pageIDs, current)
	}
	return pageIDs, nil
}

// loadPagesAccessAncestorIDs is loadPageAccessAncestorIDs for many pages of
// a workspace at once, by page; a page not found has none.
func loadPagesAccessAncestorIDs(app core.App, workspaceID string, pageIDs []string) (map[string][]string, error) {
	result := map[string][]string{}
	if len(pageIDs) == 0 {
		return result, nil
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	ancestors := []struct {
		Anchor string `db:"anchor"`
		storedPageAccessAncestor
	}{}
	err = app.DB().NewQuery(`
		WITH RECURSIVE [[ancestors]] ([[anchor]], [[id]], [[parent_id]]) AS (
			SELECT [[page]].[[id]], [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + ` FROM {{leafen_pages}} [[page]]
			WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:pageIds})) AND [[page]].[[workspace]]={:workspaceId}
			UNION
			SELECT [[ancestors]].[[anchor]], [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `
			FROM {{leafen_pages}} [[page]]
			JOIN [[ancestors]] ON [[page]].[[id]]=[[ancestors]].[[parent_id]]
			WHERE [[ancestors]].[[parent_id]] != ''
				AND [[page]].[[workspace]]={:workspaceId}
				AND NOT EXISTS (
					SELECT 1 FROM {{leafen_page_share_unlinks}} [[unlink]] WHERE [[unlink]].[[page_id]]=[[ancestors]].[[id]]
				)
		)
		SELECT [[anchor]], [[id]], [[parent_id]] FROM [[ancestors]]
	`).Bind(dbx.Params{"pageIds": string(encoded), "workspaceId": workspaceID}).All(&ancestors)
	if err != nil {
		return nil, err
	}
	parentsByAnchor := map[string]map[string]string{}
	for _, ancestor := range ancestors {
		parents := parentsByAnchor[ancestor.Anchor]
		if parents == nil {
			parents = map[string]string{}
			parentsByAnchor[ancestor.Anchor] = parents
		}
		parents[ancestor.ID] = ancestor.ParentID
	}
	for pageID, parents := range parentsByAnchor {
		if _, found := parents[pageID]; !found {
			continue
		}
		above := make([]string, 0, len(parents))
		seen := make(map[string]bool, len(parents))
		for current := pageID; current != "" && !seen[current]; current = parents[current] {
			if _, found := parents[current]; !found {
				break
			}
			seen[current] = true
			above = append(above, current)
		}
		result[pageID] = above
	}
	return result, nil
}
