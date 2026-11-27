package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page under a page whose web link is on opens by that link too (the
// link's page graph, public_share_graph_upkeep.go), and its share window
// says so, as Notion's does (seen 2026-11-06): general access reads "링크가
// 있는 웹의 모든 사용자" with the role the nearest such page above gives.
func populatePublicMutationInheritedWebLink(ctx context.Context, app core.App, state *publicMutationState) error {
	if state.WebSharingEnabled {
		return nil
	}
	link, err := loadInheritedWebLink(ctx, app, state.WorkspaceID, state.PageID)
	state.InheritedWebLink = link
	return err
}

// loadInheritedWebLink is the web link of the nearest page above whose link
// is on, unexpired and opens this page, or nil.
func loadInheritedWebLink(ctx context.Context, app core.App, workspaceID, pageID string) (*publicMutationInheritedWebLink, error) {
	ids, err := loadPageAccessAncestorIDs(app, workspaceID, pageID)
	if err != nil || len(ids) < 2 {
		return nil, err
	}
	ancestors := ids[1:]
	encoded, err := json.Marshal(ancestors)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		PageID    string `db:"root_page_id"`
		Role      string `db:"role"`
		ExpiresAt string `db:"expires_at"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[link]].[[root_page_id]], [[link]].[[role]], [[link]].[[expires_at]]
		FROM {{leafen_public_share_links}} [[link]]
		JOIN {{leafen_public_share_graph_memberships}} [[member]] ON [[member]].[[share_link_id]]=[[link]].[[id]]
			AND [[member]].[[page_id]]={:pageId} AND [[member]].[[source_revision]]=[[link]].[[source_revision]]
		WHERE [[link]].[[workspace_id]]={:workspaceId} AND [[link]].[[web_enabled]]=1
			AND [[link]].[[root_page_id]] IN (SELECT [[value]] FROM json_each({:ancestors}))
	`).WithContext(ctx).Bind(dbx.Params{
		"pageId": pageID, "workspaceId": workspaceID, "ancestors": string(encoded),
	}).All(&rows); err != nil || len(rows) == 0 {
		return nil, err
	}
	now := time.Now().UTC()
	for _, ancestor := range ancestors {
		for _, row := range rows {
			if row.PageID != ancestor || publicVisibilityExpired(row.ExpiresAt, now) {
				continue
			}
			titles, err := loadPageTitles(ctx, app, workspaceID, []string{ancestor})
			if err != nil {
				return nil, err
			}
			return &publicMutationInheritedWebLink{
				PageID: ancestor, PageTitle: titles[ancestor], Role: row.Role, ExpiresAt: row.ExpiresAt,
			}, nil
		}
	}
	return nil, nil
}

// copyInheritedWebLink gives a page whose sharing leaves its parent's its own
// web link, with the role and expiry of the link above that opened it, as
// Notion's unlinked page keeps "링크가 있는 웹의 모든 사용자" (seen
// 2026-11-06): the page's graph leaves the link above
// (publicShareGraphLiveChildrenSQL), so it would close otherwise.
func copyInheritedWebLink(app core.App, authorization pageAuthorization, now time.Time) error {
	workspaceID, pageID := authorization.Workspace.WorkspaceID, authorization.Page.Id
	above, err := loadInheritedWebLink(context.Background(), app, workspaceID, pageID)
	if err != nil || above == nil {
		return err
	}
	stored, err := loadStoredPublicMutationState(app, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		stored, err = storedPublicMutationState{WorkspaceID: workspaceID, PageID: pageID}, nil
	}
	if err != nil {
		return err
	}
	if stored.ShareLinkID != "" {
		own, err := loadPublicMutationShareLink(app, stored.ShareLinkID, workspaceID, pageID)
		if err != nil || own.WebEnabled {
			return err
		}
	}
	revision := stored.Revision + 1
	share, _, err := ensurePublicMutationShareLink(app, stored, revision, now, false)
	if err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_public_share_links}} SET [[web_enabled]]=1,[[enabled]]=1,[[role]]={:role},[[expires_at]]={:expiresAt},
			[[source_revision]]={:revision},[[updated_at]]={:updatedAt} WHERE [[id]]={:id}
	`).Bind(dbx.Params{
		"role": above.Role, "expiresAt": above.ExpiresAt, "revision": revision,
		"updatedAt": now.Format(time.RFC3339Nano), "id": share.ID,
	}).Execute(); err != nil {
		return err
	}
	if err := markPublicFirstPublish(app, "leafen_public_share_links", share.ID, authorization.Workspace.PrincipalID, now); err != nil {
		return err
	}
	return startPublicShareGraph(app, share.ID, pageID, revision, now)
}

// startPublicShareGraph lays a link's graph again from its root at a new
// revision, for the upkeep runner to expand.
func startPublicShareGraph(app core.App, linkID, rootPageID string, revision int, now time.Time) error {
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_public_share_graph_memberships}} WHERE [[share_link_id]]={:linkId} AND [[source_revision]]!={:revision}
	`).Bind(dbx.Params{"linkId": linkID, "revision": revision}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_public_share_graph_memberships}}
			([[share_link_id]],[[root_page_id]],[[page_id]],[[source_revision]],[[state]],[[updated_at]],[[position]])
		VALUES ({:linkId},{:rootPageId},{:rootPageId},{:revision},'building',{:now},'')
		ON CONFLICT ([[share_link_id]],[[page_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"linkId": linkID, "rootPageId": rootPageID, "revision": revision, "now": now.Format(time.RFC3339Nano),
	}).Execute()
	return err
}

// leavePublicShareGraphsAbove takes a page whose sharing was just unlinked
// out of the graphs of the links above it at once, so nothing opens it by
// them from now; the pages under it follow through the upkeep queue.
func leavePublicShareGraphsAbove(app core.App, pageID string) error {
	params := dbx.Params{"pageId": pageID, "queuedAt": time.Now().UnixMilli()}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_public_share_graph_jobs}} ([[share_link_id]], [[page_id]], [[queued_at]])
		SELECT [[member]].[[share_link_id]], [[member]].[[page_id]], {:queuedAt}
		FROM {{leafen_public_share_graph_memberships}} [[member]]
		WHERE [[member]].[[page_id]] IN (` + publicShareGraphChildrenSQL("{:pageId}") + `)
			AND [[member]].[[share_link_id]] IN (SELECT [[share_link_id]] FROM {{leafen_public_share_graph_memberships}}
				WHERE [[page_id]] = {:pageId} AND [[root_page_id]] != {:pageId})
		ON CONFLICT ([[share_link_id]], [[page_id]]) DO NOTHING
	`).Bind(params).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_public_share_graph_memberships}} WHERE [[page_id]] = {:pageId} AND [[root_page_id]] != {:pageId}
	`).Bind(params).Execute()
	return err
}

// joinPublicShareGraphsAbove puts a page whose sharing was just linked again
// in the live graphs its parent is in at once; the upkeep runner expands the
// pages under it.
func joinPublicShareGraphsAbove(app core.App, pageID string) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_public_share_graph_memberships}}
			([[share_link_id]], [[root_page_id]], [[page_id]], [[source_revision]], [[state]], [[updated_at]], [[position]])
		SELECT [[member]].[[share_link_id]], [[member]].[[root_page_id]], [[page]].[[id]], [[member]].[[source_revision]],
			'building', {:now}, [[page]].[[id]]
		FROM {{leafen_pages}} [[page]]
		JOIN {{leafen_public_share_graph_memberships}} [[member]] ON [[member]].[[page_id]] = ` + pageAncestryParentSQL("[[page]]") + `
		JOIN {{leafen_public_share_links}} [[link]] ON [[link]].[[id]] = [[member]].[[share_link_id]]
			AND [[link]].[[source_revision]] = [[member]].[[source_revision]] AND [[link]].[[enabled]] = 1
		WHERE [[page]].[[id]] = {:pageId}
		ON CONFLICT ([[share_link_id]], [[page_id]]) DO NOTHING
	`).Bind(dbx.Params{"pageId": pageID, "now": time.Now().UTC().Format(time.RFC3339Nano)}).Execute()
	return err
}
