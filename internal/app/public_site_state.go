package app

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's published site: reading it, publishing it and taking it down, as
// the public mutation route asks (public_mutation.go). A site stands apart
// from its page's web link, as Notion's does (seen 2026-11-06): it uses the
// link's page graph, which publishing keeps live (enabled), but neither
// publishing nor taking it down turns the web link (web_enabled) on or off,
// and the link's expiry is the link's own. Publishing keeps who first made
// the site public (markPublicFirstPublish).

func loadPublicMutationSite(
	app core.App, id, workspaceID, pageID string,
) (storedPublicMutationSite, error) {
	row := storedPublicMutationSite{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[root_page_id]],[[share_link_id]],[[slug]],[[title]],[[description]],
			[[theme]],[[show_breadcrumbs]],[[show_search]],[[show_branding]],[[navigation_page_ids]],[[published]]
		FROM {{leafen_public_sites}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[root_page_id]]={:pageId}
	`).Bind(dbx.Params{"id": id, "workspaceId": workspaceID, "pageId": pageID}).One(&row)
	return row, err
}

func publishPublicMutationSite(
	app core.App,
	state storedPublicMutationState,
	input publicMutationInput,
	revision int,
	now time.Time,
) (storedPublicMutationState, string, error) {
	share, token, err := ensurePublicMutationShareLink(app, state, revision, now, false)
	if err != nil {
		return state, "", err
	}
	if _, err = app.DB().NewQuery(`
		UPDATE {{leafen_public_share_links}} SET [[enabled]]=1,[[source_revision]]={:revision},[[updated_at]]={:updatedAt}
		WHERE [[id]]={:id}
	`).Bind(dbx.Params{
		"revision": revision, "updatedAt": now.Format(time.RFC3339Nano), "id": share.ID,
	}).Execute(); err != nil {
		return state, "", err
	}
	navigation, err := json.Marshal(input.NavigationPageIDs)
	if err != nil {
		return state, "", err
	}
	if state.SiteID == "" {
		state.SiteID = securityRandomID()
		_, err = app.DB().NewQuery(`
			INSERT INTO {{leafen_public_sites}}
				([[id]],[[workspace_id]],[[root_page_id]],[[share_link_id]],[[slug]],[[published]],[[revision]],
				 [[updated_at]],[[title]],[[description]],[[theme]],[[show_breadcrumbs]],[[show_search]],
				 [[show_branding]],[[navigation_page_ids]])
			VALUES ({:id},{:workspaceId},{:pageId},{:shareId},{:slug},1,1,{:updatedAt},{:title},
				{:description},{:theme},{:breadcrumbs},{:search},{:branding},{:navigation})
		`).Bind(publicMutationSiteParams(state, input, share.ID, string(navigation), now)).Execute()
	} else {
		params := publicMutationSiteParams(state, input, share.ID, string(navigation), now)
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_public_sites}} SET [[share_link_id]]={:shareId},[[slug]]={:slug},[[published]]=1,
				[[revision]]=[[revision]]+1,[[updated_at]]={:updatedAt},[[title]]={:title},
				[[description]]={:description},[[theme]]={:theme},[[show_breadcrumbs]]={:breadcrumbs},
				[[show_search]]={:search},[[show_branding]]={:branding},[[navigation_page_ids]]={:navigation}
			WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[root_page_id]]={:pageId}
		`).Bind(params).Execute()
	}
	// An address another site holds says so, rather than failing as storage would.
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: leafen_public_sites.slug") {
		return state, "", errPublicMutationSiteAddressTaken
	}
	if err == nil {
		err = markPublicFirstPublish(app, "leafen_public_sites", state.SiteID, state.UpdatedBy, now)
	}
	state.ShareLinkID = share.ID
	return state, token, err
}

func publicMutationSiteParams(
	state storedPublicMutationState, input publicMutationInput, shareID, navigation string, now time.Time,
) dbx.Params {
	return dbx.Params{
		"id": state.SiteID, "workspaceId": state.WorkspaceID, "pageId": state.PageID,
		"shareId": shareID, "slug": input.Slug, "updatedAt": now.Format(time.RFC3339Nano),
		"title": input.Title, "description": input.Description, "theme": input.Theme,
		"breadcrumbs": *input.ShowBreadcrumbs, "search": *input.ShowSearch,
		"branding": *input.ShowBranding, "navigation": navigation,
	}
}

func unpublishPublicMutationSite(
	app core.App, state storedPublicMutationState, revision int, now time.Time,
) error {
	if state.SiteID == "" {
		return publicMutationRefused("the page has no published site to unpublish")
	}
	params := dbx.Params{
		"id": state.SiteID, "workspaceId": state.WorkspaceID, "pageId": state.PageID,
		"updatedAt": now.Format(time.RFC3339Nano),
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_public_sites}} SET [[published]]=0,[[revision]]=[[revision]]+1,[[updated_at]]={:updatedAt}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[root_page_id]]={:pageId}
	`).Bind(params).Execute()
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return publicMutationRefused("the page has no published site to unpublish")
	}
	// The page's graph stays live only for its web link.
	if state.ShareLinkID != "" {
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_public_share_links}} SET [[enabled]]=[[web_enabled]],
				[[source_revision]]={:revision},[[updated_at]]={:updatedAt}
			WHERE [[id]]={:id}
		`).Bind(dbx.Params{
			"revision": revision, "updatedAt": now.Format(time.RFC3339Nano), "id": state.ShareLinkID,
		}).Execute()
	}
	return err
}
