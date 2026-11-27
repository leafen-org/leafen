package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var errPublicVisibilityNotFound = errors.New("public visibility not found")
var publicSiteSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type publicVisibilityAuthorization struct {
	AuthorityID    string
	RootPageID     string
	SiteID         string
	SourceRevision int
	Workspace      *core.Record
	WorkspaceID    string
	// ViewerID is, for a view link, the person who made it: the pages its
	// page links and mentions name are those that person may read, not the
	// published ones (view_links.go).
	ViewerID string
}

type storedPublicShareAuthority struct {
	LinkID         string `db:"link_id"`
	WorkspaceID    string `db:"workspace_id"`
	RootPageID     string `db:"root_page_id"`
	SiteID         string `db:"site_id"`
	SourceRevision int    `db:"source_revision"`
	ExpiresAt      string `db:"expires_at"`
}

type storedPublicVisibilityPage struct {
	ID string `db:"id"`
}

type storedPublicVisibilityMembership struct {
	PageID string `db:"page_id"`
}

func publicVisibilityTokenDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func authorizePublicSharePages(
	app core.App,
	token string,
	pageIDs []string,
	now time.Time,
) (publicVisibilityAuthorization, error) {
	rows := []storedPublicShareAuthority{}
	err := app.DB().NewQuery(`
		SELECT [[id]] AS [[link_id]], '' AS [[site_id]], [[workspace_id]], [[root_page_id]],
			[[source_revision]], [[expires_at]]
		FROM {{leafen_public_share_links}}
		WHERE [[token_digest]] = {:tokenDigest} AND [[enabled]] = 1 AND [[web_enabled]] = 1
		LIMIT 2
	`).Bind(dbx.Params{
		"tokenDigest": publicVisibilityTokenDigest(token),
	}).All(&rows)
	if err != nil {
		return publicVisibilityAuthorization{}, err
	}
	if len(rows) != 1 || publicVisibilityExpired(rows[0].ExpiresAt, now) {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}
	return authorizePublicVisibilityPages(app, rows[0], pageIDs)
}

// publicSitePageIDPart is the page id a site's first address ends with
// (defaultSiteSlug in the web app: the title's letters, "-", and up to 16 of
// the page id's letters), or "" for an address without one.
func publicSitePageIDPart(slug string) string {
	part := slug
	if at := strings.LastIndexByte(slug, '-'); at >= 0 {
		part = slug[at+1:]
	}
	if len(part) < 15 || len(part) > 16 {
		return ""
	}
	return part
}

func authorizePublicSitePages(
	app core.App,
	slug string,
	pageIDs []string,
	now time.Time,
) (publicVisibilityAuthorization, error) {
	rows, err := loadPublicSiteAuthorities(app, `[[site]].[[slug]] = {:slug}`, dbx.Params{"slug": slug})
	if err != nil {
		return publicVisibilityAuthorization{}, err
	}
	// A site whose address was changed still opens at its first one, its
	// title and its page's id, as Notion's old path does: links people were
	// given and visitors' open tabs keep working (seen 2026-11-07). An
	// address another site holds now is that site's.
	if idPart := publicSitePageIDPart(slug); len(rows) == 0 && idPart != "" {
		rows, err = loadPublicSiteAuthorities(app,
			`substr(lower(replace([[site]].[[root_page_id]], '-', '')), 1, 16) = {:idPart}`, dbx.Params{"idPart": idPart})
		if err != nil {
			return publicVisibilityAuthorization{}, err
		}
	}
	// A site is up while it is published: its page's web link, with its
	// expiry, is a choice of its own (schema_baseline.json).
	if len(rows) != 1 {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}
	return authorizePublicVisibilityPages(app, rows[0], pageIDs)
}

// loadPublicSiteAuthorities reads the published sites a condition on the
// site names, with the web link each one is served through.
func loadPublicSiteAuthorities(app core.App, where string, params dbx.Params) ([]storedPublicShareAuthority, error) {
	rows := []storedPublicShareAuthority{}
	err := app.DB().NewQuery(`
		SELECT [[link]].[[id]] AS [[link_id]], [[site]].[[id]] AS [[site_id]], [[link]].[[workspace_id]],
			[[link]].[[root_page_id]], [[link]].[[source_revision]], [[link]].[[expires_at]]
		FROM {{leafen_public_sites}} [[site]]
		JOIN {{leafen_public_share_links}} [[link]] ON [[link]].[[id]] = [[site]].[[share_link_id]]
		WHERE ` + where + ` AND [[site]].[[published]] = 1
			AND [[site]].[[workspace_id]] = [[link]].[[workspace_id]]
			AND [[site]].[[root_page_id]] = [[link]].[[root_page_id]]
			AND [[link]].[[enabled]] = 1
		LIMIT 2
	`).Bind(params).All(&rows)
	return rows, err
}

func publicVisibilityExpired(value string, now time.Time) bool {
	if value == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, value)
	return err != nil || !expiresAt.After(now)
}

func authorizePublicVisibilityPages(
	app core.App,
	authority storedPublicShareAuthority,
	pageIDs []string,
) (publicVisibilityAuthorization, error) {
	if len(pageIDs) == 0 {
		pageIDs = []string{authority.RootPageID}
	}
	requested := make(map[string]struct{}, len(pageIDs))
	for _, pageID := range pageIDs {
		requested[pageID] = struct{}{}
	}
	if len(requested) == 0 {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}
	allPages := make(map[string]struct{}, len(requested)+1)
	for pageID := range requested {
		allPages[pageID] = struct{}{}
	}
	allPages[authority.RootPageID] = struct{}{}

	params := dbx.Params{
		"ownerId":     "",
		"workspaceId": authority.WorkspaceID,
	}
	placeholders := make([]string, 0, len(allPages))
	for pageID := range allPages {
		key := fmt.Sprintf("visibilityPage%d", len(placeholders))
		params[key] = pageID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	workspace, err := app.FindRecordById(workspacesCollection, authority.WorkspaceID)
	if err != nil {
		return publicVisibilityAuthorization{}, refusedUnlessStorage(errPublicVisibilityNotFound, err)
	}
	// What the organization published stays down while it has publishing to
	// the web turned off.
	turnedOff, err := publicSharingTurnedOff(app, workspace.GetString("organization"))
	if err != nil {
		return publicVisibilityAuthorization{}, err
	}
	if turnedOff {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}
	params["ownerId"] = workspace.GetString("owner")
	pages := []storedPublicVisibilityPage{}
	err = app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_pages}} INDEXED BY [[idx_leafen_pages_export_ygo_window]]
		WHERE [[workspace]] = {:workspaceId} AND [[owner]] = {:ownerId}
			AND [[notion_import_staging]] = 0
			AND [[id]] IN (` + joinSQL(placeholders) + `)
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_teamspaces}} [[teamspace]]
				WHERE [[teamspace]].[[id]] = {{leafen_pages}}.[[teamspace_id]] AND [[teamspace]].[[disable_publishing]] = 1)
	`).Bind(params).All(&pages)
	if err != nil {
		return publicVisibilityAuthorization{}, err
	}
	if len(pages) != len(allPages) {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}
	// A page in the trash, or under one, is no longer published, nor is
	// anything under it (pageTrashWalkSQL, the one trash rule).
	ids := make([]string, 0, len(allPages))
	for pageID := range allPages {
		ids = append(ids, pageID)
	}
	if trashed, err := loadPageTrashEffects(app, ids); err != nil {
		return publicVisibilityAuthorization{}, err
	} else if len(trashed) > 0 {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}

	descendants := make([]string, 0, len(requested))
	for pageID := range requested {
		if pageID != authority.RootPageID {
			descendants = append(descendants, pageID)
		}
	}
	if len(descendants) > 0 {
		membershipParams := dbx.Params{
			"linkId": authority.LinkID, "rootPageId": authority.RootPageID,
			"sourceRevision": authority.SourceRevision,
		}
		membershipPlaceholders := make([]string, 0, len(descendants))
		for index, pageID := range descendants {
			key := fmt.Sprintf("membershipPage%d", index)
			membershipParams[key] = pageID
			membershipPlaceholders = append(membershipPlaceholders, "{:"+key+"}")
		}
		memberships := []storedPublicVisibilityMembership{}
		err = app.DB().NewQuery(`
			SELECT [[page_id]] FROM {{leafen_public_share_graph_memberships}}
			WHERE [[share_link_id]] = {:linkId} AND [[root_page_id]] = {:rootPageId}
				AND [[source_revision]] = {:sourceRevision} AND [[state]] = 'complete'
				AND [[page_id]] IN (` + joinSQL(membershipPlaceholders) + `)
			LIMIT {:limit}
		`).Bind(mergeDBParams(membershipParams, dbx.Params{
			"limit": len(descendants) + 1,
		})).All(&memberships)
		if err != nil {
			return publicVisibilityAuthorization{}, err
		}
		if len(memberships) != len(descendants) {
			return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
		}
	}

	return publicVisibilityAuthorization{
		AuthorityID:    authority.LinkID,
		RootPageID:     authority.RootPageID,
		SiteID:         authority.SiteID,
		SourceRevision: authority.SourceRevision,
		Workspace:      workspace,
		WorkspaceID:    authority.WorkspaceID,
	}, nil
}

func mergeDBParams(left dbx.Params, right dbx.Params) dbx.Params {
	for key, value := range right {
		left[key] = value
	}
	return left
}
