package app

import (
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Workspace analytics' content lists the pages and database pages a viewer
// can read, out of the trash with every page above them, most viewed first,
// then most recently edited.

const (
	workspaceAnalyticsContentLimit = 200
	// Candidates are filtered and checked for access this many at a time.
	workspaceAnalyticsContentBatch = 256
)

type workspaceAnalyticsPage struct {
	PageID          string `json:"pageId"`
	ContainerPageID string `json:"containerPageId,omitempty"`
	Title           string `json:"title"`
	Icon            string `json:"icon"`
	IconType        string `json:"iconType"`
	// What its icon is drawn as without one of its own, as the sidebar
	// draws it: a database or a wiki its table, an empty page its empty
	// document (2026-11-13: a full-page database read as a document).
	Kind string `json:"kind"`
	// ViewBlock is a full-page database that is a view block, called
	// "제목 없음" with no title, and SourceTitles an untitled one's sources
	// while it has several, as the app names it (page_name_kind.go).
	ViewBlock        bool                  `json:"viewBlock,omitempty"`
	SourceTitles     []databaseSourceTitle `json:"sourceTitles,omitempty"`
	IsWiki           bool                  `json:"isWiki,omitempty"`
	WikiDataSourceID string                `json:"wikiDataSourceId,omitempty"`
	HasContent       *bool                 `json:"hasContent,omitempty"`
	TeamspaceID      string                `json:"teamspaceId"`
	CreatedAt        string                `json:"createdAt"`
	CreatedBy        string                `json:"createdBy"`
	LastEditedAt     string                `json:"lastEditedAt"`
	Views            int                   `json:"views"`
	UniqueViewers    int                   `json:"uniqueViewers"`
}

type workspaceAnalyticsContentFilter struct {
	CreatedAfter  int64
	CreatedBefore int64
	CreatedBy     string
	Teamspace     string
	Title         string
}

type storedWorkspaceAnalyticsPage struct {
	ID              string `db:"id"`
	ContainerPageID string `db:"container_page_id"`
	Title           string `db:"title"`
	Icon            string `db:"icon"`
	IconType        string `db:"icon_type"`
	Kind            string `db:"kind"`
	HasContent      int    `db:"has_content"`
	TeamspaceID     string `db:"teamspace_id"`
	Created         string `db:"created"`
	CreatedBy       string `db:"created_by"`
	Updated         string `db:"updated"`
	Views           int    `db:"views"`
	UniqueViewers   int    `db:"unique_viewers"`
}

func parseWorkspaceAnalyticsContentFilter(values url.Values) (workspaceAnalyticsContentFilter, error) {
	filter := workspaceAnalyticsContentFilter{
		CreatedBy: strings.TrimSpace(values.Get("createdBy")),
		Teamspace: strings.TrimSpace(values.Get("teamspace")),
		Title:     foldSearchText(strings.TrimSpace(values.Get("q"))),
	}
	instant := func(name string) (int64, error) {
		raw := strings.TrimSpace(values.Get(name))
		if raw == "" {
			return 0, nil
		}
		return workspaceSearchInstant(name, raw)
	}
	var err error
	if filter.CreatedAfter, err = instant("createdAfter"); err != nil {
		return workspaceAnalyticsContentFilter{}, err
	}
	if filter.CreatedBefore, err = instant("createdBefore"); err != nil {
		return workspaceAnalyticsContentFilter{}, err
	}
	switch {
	case filter.CreatedAfter > 0 && filter.CreatedBefore > 0 && filter.CreatedAfter >= filter.CreatedBefore:
		return workspaceAnalyticsContentFilter{}, refuse(errWorkspaceSearchFilterInvalid,
			"createdAfter is the same as or later than createdBefore, so no page falls between them")
	case len(filter.CreatedBy) > maxWorkspacePermissionIdentifierLength:
		return workspaceAnalyticsContentFilter{}, refuse(errWorkspaceSearchFilterInvalid, "createdBy is at most %d characters", maxWorkspacePermissionIdentifierLength)
	case len(filter.Teamspace) > maxWorkspacePermissionIdentifierLength:
		return workspaceAnalyticsContentFilter{}, refuse(errWorkspaceSearchFilterInvalid, "teamspace is at most %d characters", maxWorkspacePermissionIdentifierLength)
	case len(filter.Title) > maxWorkspaceSearchQueryRunes*4:
		return workspaceAnalyticsContentFilter{}, refuse(errWorkspaceSearchFilterInvalid, "q is at most %d characters", maxWorkspaceSearchQueryRunes)
	}
	return filter, nil
}

func loadWorkspaceAnalyticsContent(
	app core.App,
	authorization workspaceAuthorization,
	principalID string,
	window workspaceAnalyticsWindow,
	filter workspaceAnalyticsContentFilter,
) ([]workspaceAnalyticsPage, bool, error) {
	params := dbx.Params{"workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID, "fromDay": window.FromDay}
	// Both lists come in the same order — most viewed, then most recently
	// edited, then by id — and are read a row at a time, so the filters and
	// access checks below see every candidate until the page fills.
	pages, err := openWorkspaceAnalyticsStream(app, `
		WITH [[views]] AS (
			SELECT [[page_id]], SUM([[total_views]]) AS [[views]] FROM {{leafen_page_analytics_daily}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[day]] >= {:fromDay} GROUP BY [[page_id]]
		), [[viewers]] AS (
			SELECT [[page_id]], COUNT(DISTINCT [[viewer_kind]] || ':' || [[viewer_key]]) AS [[viewers]]
			FROM {{leafen_page_analytics_unique_viewers}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[day]] >= {:fromDay} GROUP BY [[page_id]]
		)
		SELECT [[page]].[[id]], '' AS [[container_page_id]], [[page]].[[title]], [[page]].[[icon]],
			[[page]].[[icon_type]], [[page]].[[kind]], `+pageHasContentSQL+` AS [[has_content]],
			[[page]].[[teamspace_id]], [[page]].[[created]], [[page]].[[created_by]],
			[[page]].[[updated]], COALESCE([[views]].[[views]], 0) AS [[views]],
			COALESCE([[viewers]].[[viewers]], 0) AS [[unique_viewers]]
		FROM {{leafen_pages}} [[page]]
		LEFT JOIN [[views]] ON [[views]].[[page_id]] = [[page]].[[id]]
		LEFT JOIN [[viewers]] ON [[viewers]].[[page_id]] = [[page]].[[id]]
		WHERE [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[owner]] = {:ownerId}
			AND [[page]].[[in_trash]] = 0 AND [[page]].[[notion_import_staging]] = 0
			AND [[page]].[[data_source_id]] = ''
		ORDER BY COALESCE([[views]].[[views]], 0) DESC, [[page]].[[updated]] DESC, [[page]].[[id]]
	`, params)
	if err != nil {
		return nil, false, err
	}
	defer pages.close()
	// Database pages that were viewed, read through the page holding them.
	// Each viewed row's page is found by id; its workspace and owner are
	// read after (+), or SQLite, which keeps no statistics, reads the
	// workspace's every row page.
	items, err := openWorkspaceAnalyticsStream(app, `
		SELECT [[item]].[[row_id]] AS [[id]], [[item]].[[container_page_id]],
			COALESCE((
				SELECT json_extract([[value]].[[value]], '$') FROM {{leafen_database_row_values}} [[value]]
				WHERE [[value]].[[row_id]] = [[item]].[[row_id]] AND [[value]].[[type]] = 'title' LIMIT 1
			), '') AS [[title]],
			[[row_record]].[[icon]], [[row_record]].[[icon_type]], '' AS [[kind]], -1 AS [[has_content]],
			COALESCE([[container]].[[teamspace_id]], '') AS [[teamspace_id]],
			strftime('%Y-%m-%d %H:%M:%fZ', [[item]].[[created]] / 1000.0, 'unixepoch') AS [[created]],
			[[row_record]].[[created_by]],
			strftime('%Y-%m-%d %H:%M:%fZ', [[item]].[[updated]] / 1000.0, 'unixepoch') AS [[updated]],
			[[viewed]].[[views]],
			(SELECT COUNT(DISTINCT [[viewer]].[[viewer_key]]) FROM {{leafen_row_page_analytics_unique_viewers}} [[viewer]]
				WHERE [[viewer]].[[workspace_id]] = {:workspaceId} AND [[viewer]].[[row_id]] = [[item]].[[row_id]]
					AND [[viewer]].[[day]] >= {:fromDay}) AS [[unique_viewers]]
		FROM (
			SELECT [[row_id]], SUM([[total_views]]) AS [[views]] FROM {{leafen_row_page_analytics_daily}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[day]] >= {:fromDay} GROUP BY [[row_id]]
		) [[viewed]]
		JOIN {{leafen_database_row_pages}} [[item]] ON [[item]].[[row_id]] = [[viewed]].[[row_id]]
		JOIN {{leafen_pages}} [[row_record]] ON [[row_record]].[[id]] = [[item]].[[row_id]]
		LEFT JOIN {{leafen_pages}} [[container]] ON [[container]].[[id]] = [[item]].[[container_page_id]]
		WHERE +[[item]].[[workspace_id]] = {:workspaceId} AND +[[item]].[[owner]] = {:ownerId}
			AND `+databaseRowNotTrashedSQL("[[item]].[[row_id]]")+`
		ORDER BY [[viewed]].[[views]] DESC, [[item]].[[updated]] DESC, [[item]].[[row_id]]
	`, params)
	if err != nil {
		return nil, false, err
	}
	defer items.close()

	kept := []workspaceAnalyticsPage{}
	for {
		batch := make([]storedWorkspaceAnalyticsPage, 0, workspaceAnalyticsContentBatch)
		for len(batch) < workspaceAnalyticsContentBatch {
			next := pages
			if pages.next == nil || items.next != nil && workspaceAnalyticsBefore(*items.next, *pages.next) {
				next = items
			}
			if next.next == nil {
				break
			}
			batch = append(batch, *next.next)
			next.advance()
		}
		if pages.err != nil {
			return nil, false, pages.err
		}
		if items.err != nil {
			return nil, false, items.err
		}
		if len(batch) == 0 {
			return withWorkspaceAnalyticsWikis(app, kept, false)
		}
		var places map[string]workspaceSearchPlace
		if filter.Teamspace != "" {
			anchors := make([]string, 0, len(batch))
			for _, candidate := range batch {
				anchors = append(anchors, workspaceAnalyticsAnchor(candidate))
			}
			if places, err = loadWorkspaceSearchPlaces(app, anchors); err != nil {
				return nil, false, err
			}
		}
		// A page under a page in the trash went there with it, and a
		// database page with the page holding its database
		// (loadPageTrashEffects): neither is listed.
		ids := make([]string, 0, len(batch))
		for _, candidate := range batch {
			ids = append(ids, candidate.ID)
		}
		trashed, err := loadPageTrashEffects(app, ids)
		if err != nil {
			return nil, false, err
		}
		// Access is asked once per page holding candidates, within a batch.
		readable := map[string]bool{}
		for _, candidate := range batch {
			if _, inTrash := trashed[candidate.ID]; inTrash {
				continue
			}
			created := workspaceSearchPageTime(candidate.Created)
			if filter.CreatedAfter > 0 && created < filter.CreatedAfter ||
				filter.CreatedBefore > 0 && created >= filter.CreatedBefore ||
				filter.CreatedBy != "" && candidate.CreatedBy != filter.CreatedBy ||
				filter.Title != "" && !strings.Contains(foldSearchText(candidate.Title), filter.Title) {
				continue
			}
			anchor := workspaceAnalyticsAnchor(candidate)
			if filter.Teamspace != "" && !slices.Contains(places[anchor].Teamspaces, filter.Teamspace) {
				continue
			}
			// Only pages the viewer may read, the workspace's owner too: as
			// Notion's, analytics doesn't show someone's private page.
			allowed, seen := readable[anchor]
			if !seen {
				_, err := authorizePageAccessRole(app, principalID, anchor, pageAccessRoleView)
				if err != nil && !isPermissionDenied(err) {
					return nil, false, err
				}
				allowed = err == nil
				readable[anchor] = allowed
			}
			if !allowed {
				continue
			}
			if len(kept) == workspaceAnalyticsContentLimit {
				return withWorkspaceAnalyticsWikis(app, kept, true)
			}
			kept = append(kept, workspaceAnalyticsPage{
				PageID: candidate.ID, ContainerPageID: candidate.ContainerPageID, Title: candidate.Title,
				Icon: candidate.Icon, IconType: candidate.IconType, Kind: readPageKind(candidate.Kind),
				HasContent: workspaceAnalyticsHasContent(candidate.HasContent), TeamspaceID: candidate.TeamspaceID,
				CreatedAt: workspaceAnalyticsStoredTime(candidate.Created), CreatedBy: candidate.CreatedBy,
				LastEditedAt: workspaceAnalyticsStoredTime(candidate.Updated),
				Views:        candidate.Views, UniqueViewers: candidate.UniqueViewers,
			})
		}
	}
}

// withWorkspaceAnalyticsWikis marks the wikis among the pages kept, whose
// icon is their database's table, and the view blocks among its databases.
func withWorkspaceAnalyticsWikis(app core.App, kept []workspaceAnalyticsPage, more bool) ([]workspaceAnalyticsPage, bool, error) {
	pageIDs := make([]string, 0, len(kept))
	for _, page := range kept {
		if page.ContainerPageID == "" {
			pageIDs = append(pageIDs, page.PageID)
		}
	}
	wikis, err := loadWikiPageMetadataSet(app, pageIDs)
	if err != nil {
		return nil, false, err
	}
	kinds, err := loadPageNameKinds(app, pageIDs)
	if err != nil {
		return nil, false, err
	}
	for index, page := range kept {
		if metadata, ok := wikis[page.PageID]; ok && page.ContainerPageID == "" {
			kept[index].IsWiki, kept[index].WikiDataSourceID = metadata.IsWiki, metadata.DataSourceID
		}
		if page.ContainerPageID == "" {
			kept[index].ViewBlock, kept[index].SourceTitles = kinds[page.PageID].ViewBlock, kinds[page.PageID].SourceTitles
		}
	}
	return kept, more, nil
}

// workspaceAnalyticsHasContent is whether a page's body holds anything; a
// database page's (-1) isn't read.
func workspaceAnalyticsHasContent(value int) *bool {
	if value < 0 {
		return nil
	}
	return boolPointer(value == 1)
}

// workspaceAnalyticsBefore orders candidates as both lists come: most
// viewed, then most recently edited, then by id.
func workspaceAnalyticsBefore(left, right storedWorkspaceAnalyticsPage) bool {
	if left.Views != right.Views {
		return left.Views > right.Views
	}
	if left.Updated != right.Updated {
		return left.Updated > right.Updated
	}
	return left.ID < right.ID
}

// workspaceAnalyticsStream reads one ordered list of candidates a row at a
// time; next is the row ahead, nil once the list ends or fails (err).
type workspaceAnalyticsStream struct {
	rows *dbx.Rows
	next *storedWorkspaceAnalyticsPage
	err  error
}

func openWorkspaceAnalyticsStream(app core.App, query string, params dbx.Params) (*workspaceAnalyticsStream, error) {
	rows, err := app.DB().NewQuery(query).Bind(params).Rows()
	if err != nil {
		return nil, err
	}
	stream := &workspaceAnalyticsStream{rows: rows}
	stream.advance()
	return stream, nil
}

func (stream *workspaceAnalyticsStream) advance() {
	stream.next = nil
	if stream.err != nil || !stream.rows.Next() {
		if stream.err == nil {
			stream.err = stream.rows.Err()
		}
		return
	}
	candidate := storedWorkspaceAnalyticsPage{}
	if err := stream.rows.ScanStruct(&candidate); err != nil {
		stream.err = err
		return
	}
	stream.next = &candidate
}

func (stream *workspaceAnalyticsStream) close() { _ = stream.rows.Close() }

// workspaceAnalyticsAnchor is the page whose access and place count: the
// page itself, or the page holding a database page's database.
func workspaceAnalyticsAnchor(candidate storedWorkspaceAnalyticsPage) string {
	if candidate.ContainerPageID != "" {
		return candidate.ContainerPageID
	}
	return candidate.ID
}

func workspaceAnalyticsStoredTime(value string) string {
	parsed, err := types.ParseDateTime(value)
	if err != nil || parsed.IsZero() {
		return ""
	}
	return parsed.Time().UTC().Format(time.RFC3339Nano)
}
