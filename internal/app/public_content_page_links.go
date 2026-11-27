package app

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's public pages open a linked page only when that page is published
// too: a page anyone with the link can open also opens from a page that links
// to or mentions it. A share link or site therefore shows its own graph and,
// beyond it, a page that another enabled, unexpired share link publishes
// (that link's root or a page of its complete graph) when a page it shows
// links to or mentions that page or one of its published ancestors, in turn.
// A page link whose target is not published shows "No access" and carries
// neither the target's id nor its title. Page links name their target with
// childPageId; an older pageId is read the same way.

// A chain is followed to its end, however long, and through every page that
// links to one on it or holds a file: the walk goes on only through
// published pages, so it spans the published part of the workspace, each
// page once, and a read remembers what it has decided.

const (
	// Pages one query reads.
	publicLinkedPageWindow = 256

	publicPageLinkUnavailableKey = "pageLinkUnavailable"
	// publicPageLinkTitleSpansKey holds the spans of a page link's target's
	// title, as the visitor reads it, while its name holds a mention: their
	// screen draws its custom emoji and says its mentions by them, as a
	// member's does (TitleWithCustomEmoji.tsx).
	publicPageLinkTitleSpansKey = "childPageTitleSpans"
	// publicPageLinkHasContentKey says whether a page link's target's body
	// holds anything.
	publicPageLinkHasContentKey = "childPageHasContent"
	// publicPageLinkSourceTitlesKey holds, for a link to an untitled
	// full-page database of several sources, their titles, which name it.
	publicPageLinkSourceTitlesKey = "childPageSourceTitles"
)

var publicPageLinkBlockTypes = map[string]struct{}{
	graphBlockTypeLinkToPage: {}, graphBlockTypeChildPage: {}, graphBlockTypeChildDatabase: {},
}

type publicLinkedReach struct {
	app           core.App
	authorization publicVisibilityAuthorization
	now           time.Time
	// shown holds pages this authority already shows in the current read.
	shown     map[string]bool
	published map[string]bool
	// decided holds whether each page asked about is reachable.
	decided map[string]bool
}

func newPublicLinkedReach(
	app core.App, authorization publicVisibilityAuthorization, now time.Time, shownPageIDs ...string,
) *publicLinkedReach {
	reach := &publicLinkedReach{
		app: app, authorization: authorization, now: now,
		shown: map[string]bool{authorization.RootPageID: true}, published: map[string]bool{},
		decided: map[string]bool{},
	}
	for _, pageID := range shownPageIDs {
		reach.shown[pageID] = true
	}
	return reach
}

// authorizePublicLinkedContentPage admits a page outside the authority's own
// graph when a chain of published page links reaches it.
func authorizePublicLinkedContentPage(
	app core.App, input publicContentSnapshotRequest, now time.Time,
) (publicVisibilityAuthorization, error) {
	var authorization publicVisibilityAuthorization
	var err error
	if input.SourceKind == publicContentSourceShare {
		authorization, err = authorizePublicSharePages(app, input.Token, nil, now)
	} else {
		authorization, err = authorizePublicSitePages(app, input.SiteSlug, nil, now)
	}
	if err != nil {
		return publicVisibilityAuthorization{}, err
	}
	live, err := loadPublicLinkedPages(app, authorization, []string{input.PageID})
	if err != nil {
		return publicVisibilityAuthorization{}, err
	}
	if _, found := live[input.PageID]; !found {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}
	reachable, err := newPublicLinkedReach(app, authorization, now).reachable(input.PageID)
	if err != nil {
		return publicVisibilityAuthorization{}, err
	}
	if !reachable {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}
	return authorization, nil
}

// reachable reports whether the authority shows the page or a chain of
// published page links leads to it from a page the authority shows.
func (reach *publicLinkedReach) reachable(pageID string) (bool, error) {
	if decided, known := reach.decided[pageID]; known {
		return decided, nil
	}
	// A view link shows its page as its maker sees it (2026-11-07: a child
	// page its maker reads stood as "No access" there): a linked page is named
	// when that person may read it; the link itself opens nothing else.
	if reach.authorization.ViewerID != "" {
		readable := false
		if _, err := authorizePage(reach.app, reach.authorization.ViewerID, pageID, permissionWorkspaceRead); err == nil {
			trashed, err := pageInTrashPath(reach.app, pageID)
			if err != nil {
				return false, err
			}
			readable = !trashed
		}
		reach.decided[pageID] = readable
		return readable, nil
	}
	reachable, err := reach.walk(pageID)
	if err == nil {
		reach.decided[pageID] = reachable
	}
	return reachable, err
}

func (reach *publicLinkedReach) walk(pageID string) (bool, error) {
	visited := map[string]bool{}
	queue := []string{pageID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		// A published page's sub-pages are published with it, so a page is
		// reached through any link to it or to one of its published ancestors.
		for current != "" && !visited[current] {
			visited[current] = true
			shown, err := reach.isShown(current)
			if err != nil || shown {
				return shown, err
			}
			published, err := reach.isPublished(current)
			if err != nil {
				return false, err
			}
			if !published {
				break
			}
			linked, err := reach.linkedFromShown(current)
			if err != nil || linked {
				return linked, err
			}
			sources, err := reach.linkingPages(current)
			if err != nil {
				return false, err
			}
			for _, source := range sources {
				if !visited[source] {
					queue = append(queue, source)
				}
			}
			current, err = reach.parentPage(current)
			if err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

func (reach *publicLinkedReach) isShown(pageID string) (bool, error) {
	if reach.shown[pageID] {
		return true, nil
	}
	count := 0
	err := reach.app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_public_share_graph_memberships}}
		WHERE [[share_link_id]] = {:linkId} AND [[page_id]] = {:pageId}
			AND [[root_page_id]] = {:rootPageId} AND [[source_revision]] = {:sourceRevision}
			AND [[state]] = 'complete'
	`).Bind(dbx.Params{
		"linkId": reach.authorization.AuthorityID, "pageId": pageID,
		"rootPageId": reach.authorization.RootPageID, "sourceRevision": reach.authorization.SourceRevision,
	}).Row(&count)
	if count > 0 {
		reach.shown[pageID] = true
	}
	return count > 0, err
}

// isPublished reports whether a live share link of the workspace, unexpired
// or with its site published, publishes the live page.
func (reach *publicLinkedReach) isPublished(pageID string) (bool, error) {
	if published, found := reach.published[pageID]; found {
		return published, nil
	}
	expiries := []string{}
	// A published site keeps its pages published whatever its web link's
	// expiry (schema_baseline.json). The page and each link's
	// root are found by id; workspace, owner and flags are read after (+),
	// or SQLite reads the owner's every page.
	err := reach.app.DB().NewQuery(`
		SELECT CASE WHEN EXISTS (SELECT 1 FROM {{leafen_public_sites}} [[site]]
				WHERE [[site]].[[share_link_id]] = [[link]].[[id]] AND [[site]].[[published]] = 1)
			THEN '' ELSE [[link]].[[expires_at]] END AS [[open_until]]
		FROM {{leafen_public_share_links}} [[link]]
		JOIN {{leafen_public_share_graph_memberships}} [[member]]
			ON [[member]].[[share_link_id]] = [[link]].[[id]] AND [[member]].[[page_id]] = {:pageId}
				AND [[member]].[[root_page_id]] = [[link]].[[root_page_id]]
				AND [[member]].[[source_revision]] = [[link]].[[source_revision]]
				AND [[member]].[[state]] = 'complete'
		JOIN {{leafen_pages}} [[root]] ON [[root]].[[id]] = [[link]].[[root_page_id]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[member]].[[page_id]]
		WHERE [[link]].[[workspace_id]] = {:workspaceId} AND [[link]].[[enabled]] = 1
			AND +[[root]].[[workspace]] = {:workspaceId} AND +[[root]].[[owner]] = {:ownerId}
			AND +[[root]].[[notion_import_staging]] = 0 AND ` + pageOutOfTrashPathSQL("[[root]].[[id]]") + `
			AND +[[page]].[[workspace]] = {:workspaceId} AND +[[page]].[[owner]] = {:ownerId}
			AND +[[page]].[[in_trash]] = 0 AND +[[page]].[[notion_import_staging]] = 0
		ORDER BY [[open_until]] = '' DESC, [[open_until]] DESC
		LIMIT 8
	`).Bind(dbx.Params{
		"pageId": pageID, "workspaceId": reach.authorization.WorkspaceID,
		"ownerId": reach.authorization.Workspace.GetString("owner"),
	}).Column(&expiries)
	if err != nil {
		return false, err
	}
	published := false
	for _, expiresAt := range expiries {
		if !publicVisibilityExpired(expiresAt, reach.now) {
			published = true
			break
		}
	}
	reach.published[pageID] = published
	return published, nil
}

// linkedFromShown reports whether a page the authority shows links to or
// mentions the page, however many other pages link to it.
func (reach *publicLinkedReach) linkedFromShown(pageID string) (bool, error) {
	params := dbx.Params{
		"pageId": pageID, "workspaceId": reach.authorization.WorkspaceID,
		"ownerId": reach.authorization.Workspace.GetString("owner"),
		"linkId":  reach.authorization.AuthorityID, "rootPageId": reach.authorization.RootPageID,
		"sourceRevision": reach.authorization.SourceRevision,
	}
	placeholders := make([]string, 0, len(reach.shown))
	for shownID := range reach.shown {
		key := fmt.Sprintf("shownPage%d", len(placeholders))
		params[key] = shownID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	found := 0
	// The references to the page are found by their target and each source
	// page by its id; the page's workspace, owner and flags are read after
	// (+), or SQLite reads the owner's every page.
	err := reach.app.DB().NewQuery(`
		SELECT COUNT(*) FROM (
			SELECT 1 FROM {{leafen_block_page_references}} [[reference]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[reference]].[[source_page_id]]
			WHERE [[reference]].[[workspace_id]] = {:workspaceId} AND [[reference]].[[target_page_id]] = {:pageId}
				AND +[[page]].[[workspace]] = {:workspaceId} AND +[[page]].[[owner]] = {:ownerId}
				AND +[[page]].[[notion_import_staging]] = 0 AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
				AND ([[reference]].[[source_page_id]] IN (` + joinSQL(placeholders) + `) OR EXISTS (
					SELECT 1 FROM {{leafen_public_share_graph_memberships}} [[member]]
					WHERE [[member]].[[share_link_id]] = {:linkId}
						AND [[member]].[[page_id]] = [[reference]].[[source_page_id]]
						AND [[member]].[[root_page_id]] = {:rootPageId}
						AND [[member]].[[source_revision]] = {:sourceRevision}
						AND [[member]].[[state]] = 'complete'
				))
			LIMIT 1
		)
	`).Bind(params).Row(&found)
	return found > 0, err
}

// linkingPages returns every live page whose blocks, title or cells link to
// or mention the page (page_field_references_schema.go), a window at a time.
func (reach *publicLinkedReach) linkingPages(pageID string) ([]string, error) {
	sources := []string{}
	after := ""
	for {
		window := []string{}
		if err := reach.app.DB().NewQuery(`
			SELECT DISTINCT [[reference]].[[source_page_id]]
			FROM {{leafen_block_page_references}} [[reference]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[reference]].[[source_page_id]]
			WHERE [[reference]].[[workspace_id]] = {:workspaceId} AND [[reference]].[[target_page_id]] = {:pageId}
				AND [[reference]].[[source_page_id]] != {:pageId} AND [[reference]].[[source_page_id]] > {:after}
				AND [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[owner]] = {:ownerId}
				AND [[page]].[[notion_import_staging]] = 0 AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
			ORDER BY [[reference]].[[source_page_id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"pageId": pageID, "workspaceId": reach.authorization.WorkspaceID, "after": after,
			"ownerId": reach.authorization.Workspace.GetString("owner"), "limit": publicLinkedPageWindow,
		}).Column(&window); err != nil {
			return nil, err
		}
		sources = append(sources, window...)
		if len(window) < publicLinkedPageWindow {
			return sources, nil
		}
		after = window[len(window)-1]
	}
}

// parentPage names the page above the page, for a database page the page
// holding its database.
func (reach *publicLinkedReach) parentPage(pageID string) (string, error) {
	parentID := ""
	err := reach.app.DB().NewQuery(`
		SELECT ` + pageAncestryParentSQL("[[page]]") + ` FROM {{leafen_pages}} [[page]]
		WHERE [[page]].[[id]] = {:pageId} AND [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[owner]] = {:ownerId}
	`).Bind(dbx.Params{
		"pageId": pageID, "workspaceId": reach.authorization.WorkspaceID,
		"ownerId": reach.authorization.Workspace.GetString("owner"),
	}).Row(&parentID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return parentID, err
}
