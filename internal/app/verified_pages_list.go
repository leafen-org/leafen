package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The settings' "인증된 페이지", as Notion's (its help, read 2026-10-02):
// the workspace's verified pages a member can open, to search and to filter
// by owner or teamspace — pages verified in or outside a wiki, and databases
// verified on their page — each with its owners, its teamspace, who
// verified it, when, and until when. A verification past its time is left
// out; the expiry runner takes it away and reminds its owners. A page in the
// trash or under a page there, and a database in the trash or on such a
// page, is left out too.

const (
	workspaceVerifiedPagesPath   = "/workspaces/{workspaceId}/verified-pages"
	workspaceVerifiedPagesWindow = 200
)

type verifiedPageRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type verifiedPageItem struct {
	PageID string `json:"pageId"`
	// A database's block on its page; empty for a page.
	BlockID string `json:"blockId,omitempty"`
	Title   string `json:"title"`
	// A page's title's spans as its reader reads them, while it holds a
	// mention (page_title_spans.go).
	TitleSpans []graphTextSpan   `json:"titleSpans,omitempty"`
	Owners     []verifiedPageRef `json:"owners"`
	Teamspace  *verifiedPageRef  `json:"teamspace"`
	VerifiedBy verifiedPageRef   `json:"verifiedBy"`
	VerifiedAt string            `json:"verifiedAt"`
	ExpiresAt  string            `json:"expiresAt"`
	// The pages above it, nearest first, for "다음에 포함" (seen 2026-10-12).
	AncestorIDs []string `json:"ancestorIds"`
}

type verifiedPagesResponse struct {
	Items []verifiedPageItem `json:"items"`
	// More follow from ?cursor=NextCursor.
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type storedVerifiedPage struct {
	ItemKey      string `db:"item_key"`
	PageID       string `db:"page_id"`
	BlockID      string `db:"block_id"`
	DataSourceID string `db:"data_source_id"`
	Title        string `db:"title"`
	TeamspaceID  string `db:"teamspace_id"`
	VerifiedAt   string `db:"verified_at"`
	VerifiedBy   string `db:"verified_by"`
	Verifier     string `db:"verifier"`
	ExpiresAt    string `db:"verification_expires_at"`
}

func registerWorkspaceVerifiedPageRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+workspaceVerifiedPagesPath, listWorkspaceVerifiedPages)
}

func listWorkspaceVerifiedPages(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead); err != nil {
		return wikiAPIError(event, err, "verified-pages")
	}
	after, err := decodeTextIDCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWikiInvalid, "Give the nextCursor of the last list, or none to start.")
	}
	// Pages and databases together, newest verification first, a bounded
	// batch looked at each time; the next window goes on from the last one.
	// The verified pages are found by their own index (migration 826), the
	// trash and import marks read after the row (+).
	scan := workspaceVerifiedPagesWindow * 4
	rows := []storedVerifiedPage{}
	if err := event.App.DB().NewQuery(`
		SELECT * FROM (
			SELECT 'page:' || [[page]].[[id]] AS [[item_key]], [[page]].[[id]] AS [[page_id]], '' AS [[block_id]],
				'' AS [[data_source_id]], [[page]].[[title]],
				[[page]].[[teamspace_id]], [[page]].[[verified_at]], [[page]].[[verified_by]],
				COALESCE([[person]].[[name]], '') AS [[verifier]], [[page]].[[verification_expires_at]]
			FROM {{leafen_pages}} [[page]]
			LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[page]].[[verified_by]]
			WHERE [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[verified_at]] != ''
				AND +[[page]].[[in_trash]] = FALSE AND [[page]].[[archived_at]] = '' AND +[[page]].[[notion_import_staging]] = FALSE
			UNION ALL
			SELECT 'source:' || [[source]].[[id]], [[source]].[[page_id]],
				COALESCE((SELECT [[container]].[[block_id]] FROM {{leafen_database_container_sources}} [[membership]]
					JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[id]] = [[membership]].[[container_id]]
					WHERE [[membership]].[[data_source_id]] = [[source]].[[id]] AND [[container]].[[kind]] = 'source'
						AND [[container]].[[page_id]] = [[source]].[[page_id]]
					ORDER BY [[container]].[[id]] LIMIT 1), ''),
				[[source]].[[id]], [[source]].[[title]], [[page]].[[teamspace_id]],
				[[source]].[[verified_at]], [[source]].[[verified_by]], COALESCE([[person]].[[name]], ''),
				[[source]].[[verification_expires_at]]
			FROM {{leafen_data_sources}} [[source]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[source]].[[page_id]]
				AND [[page]].[[in_trash]] = FALSE AND [[page]].[[archived_at]] = '' AND [[page]].[[notion_import_staging]] = FALSE
			LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[source]].[[verified_by]]
			WHERE [[source]].[[workspace_id]] = {:workspaceId} AND [[source]].[[verified_at]] != ''
				AND ` + databaseSourceNotTrashedSQL("[[source]].[[id]]") + `
		)
		WHERE {:afterKey} = '' OR [[verified_at]] < {:afterAt} OR ([[verified_at]] = {:afterAt} AND [[item_key]] > {:afterKey})
		ORDER BY [[verified_at]] DESC, [[item_key]] LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "afterAt": after.At, "afterKey": after.ID, "limit": scan + 1}).All(&rows); err != nil {
		return wikiAPIError(event, err, "verified-pages")
	}
	more := len(rows) > scan
	if more {
		rows = rows[:scan]
	}
	// A page under a page in the trash went there with it, and a database
	// with its page (loadPageTrashEffects): neither is listed.
	listedPages := make([]string, 0, len(rows))
	for _, row := range rows {
		listedPages = append(listedPages, row.PageID)
	}
	trashed, err := loadPageTrashEffects(event.App, listedPages)
	if err != nil {
		return wikiAPIError(event, err, "verified-pages")
	}

	now := time.Now().UTC()
	teamspaces := map[string]*verifiedPageRef{}
	response := verifiedPagesResponse{Items: []verifiedPageItem{}}
	// The data source each listed database is, by its place in the list.
	sources := []string{}
	for index, row := range rows {
		if until, err := time.Parse(time.RFC3339Nano, row.ExpiresAt); err == nil && !until.After(now) {
			continue
		}
		if _, inTrash := trashed[row.PageID]; inTrash {
			continue
		}
		// Only the pages the reader can open, as the rest of settings lists them.
		if _, err := authorizePage(event.App, user.Id, row.PageID, permissionWorkspaceRead); err != nil {
			if isPermissionDenied(err) {
				continue
			}
			return wikiAPIError(event, err, "verified-pages")
		}
		verification, err := loadVerification(event.App, verificationTarget{
			PageID: row.PageID, BlockID: row.BlockID, DataSourceID: row.DataSourceID,
		})
		if err != nil {
			return wikiAPIError(event, err, "verified-pages")
		}
		item := verifiedPageItem{
			PageID: row.PageID, BlockID: row.BlockID, Title: row.Title, Owners: []verifiedPageRef{},
			VerifiedBy: verifiedPageRef{ID: row.VerifiedBy, Name: row.Verifier},
			VerifiedAt: row.VerifiedAt, ExpiresAt: row.ExpiresAt,
		}
		ancestors, err := mcpPageAncestorIDs(event.App, row.PageID)
		if err != nil {
			return wikiAPIError(event, err, "verified-pages")
		}
		item.AncestorIDs = ancestors
		for _, owner := range verification.Owners {
			item.Owners = append(item.Owners, verifiedPageRef{ID: owner.UserID, Name: owner.Name})
		}
		if row.TeamspaceID != "" {
			teamspace, known := teamspaces[row.TeamspaceID]
			if !known {
				// A teamspace the reader can't see stays unnamed.
				_, stored, _, err := authorizeTeamspace(event.App, user.Id, workspaceID, row.TeamspaceID, false)
				if err == nil {
					teamspace = &verifiedPageRef{ID: stored.ID, Name: stored.Name}
				} else if !errors.Is(err, errTeamspaceNotFound) {
					return wikiAPIError(event, err, "verified-pages")
				}
				teamspaces[row.TeamspaceID] = teamspace
			}
			item.Teamspace = teamspace
		}
		response.Items, sources = append(response.Items, item), append(sources, row.DataSourceID)
		if len(response.Items) == workspaceVerifiedPagesWindow && (index+1 < len(rows) || more) {
			response.NextCursor = encodeTextIDCursor(row.VerifiedAt, row.ItemKey)
			break
		}
	}
	if response.NextCursor == "" && more {
		response.NextCursor = encodeTextIDCursor(rows[len(rows)-1].VerifiedAt, rows[len(rows)-1].ItemKey)
	}
	response.HasMore = response.NextCursor != ""
	verifiedItemsAsRead(event.App, user.Id, response.Items, sources)
	return event.JSON(http.StatusOK, response)
}

// verifiedItemsAsRead puts the listed pages' titles and databases' names as
// their reader reads them (page_name_mentions.go).
func verifiedItemsAsRead(app core.App, readerID string, items []verifiedPageItem, sources []string) {
	titles, names := map[string]*string{}, map[string]*string{}
	for index := range items {
		if sources[index] != "" {
			names[sources[index]] = &items[index].Title
		} else {
			titles[items[index].PageID] = &items[index].Title
		}
	}
	closer := readerCloser(app, readerID)
	pageTitlesAsReadBy(app, closer, titles, true)
	sourceNamesAsReadBy(app, closer, names)
	plain := make(map[string]string, len(titles))
	for id, title := range titles {
		plain[id] = *title
	}
	spans := titleSpansAsReadFor(app, closer, plain)
	for index := range items {
		if sources[index] == "" {
			items[index].TitleSpans = spans[items[index].PageID]
		}
	}
}
