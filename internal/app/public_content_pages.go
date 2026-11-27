package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func decodePublicContentPagesSortKey(
	cursor string,
	rootPageID string,
) (bool, string, string, error) {
	if cursor == "" {
		return true, "", "", nil
	}
	if cursor == "0:"+rootPageID {
		return false, "", "", nil
	}
	if !strings.HasPrefix(cursor, "1:") {
		return false, "", "", errPublicContentCursorNotGiven
	}
	value := strings.TrimPrefix(cursor, "1:")
	separator := strings.LastIndex(value, ":")
	if separator < 0 || separator == len(value)-1 {
		return false, "", "", errPublicContentCursorNotGiven
	}
	position := value[:separator]
	pageID := value[separator+1:]
	return false, position, pageID, nil
}

// loadPublicContentPages lists the pages an authority shows for its
// navigation: its pages and wiki pages. The pages of other databases open
// from their databases, as a page tree does not list them.
func loadPublicContentPages(
	app core.App,
	authorization publicVisibilityAuthorization,
	cursor string,
	currentPage *core.Record,
) ([]publicContentPageSummary, *string, bool, error) {
	includeRoot, cursorPosition, cursorPageID, err := decodePublicContentPagesSortKey(cursor, authorization.RootPageID)
	if err != nil {
		return nil, nil, false, err
	}

	rows := []storedPublicContentPage{}
	limit := maxPublicContentSnapshotPages + 1
	if includeRoot {
		rootPage := currentPage
		if rootPage.Id != authorization.RootPageID {
			rootPage, err = app.FindRecordById(pagesCollection, authorization.RootPageID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, nil, false, errPublicContentNotFound
				}
				return nil, nil, false, err
			}
		}
		if rootPage.GetString("workspace") != authorization.WorkspaceID ||
			rootPage.GetString("owner") != authorization.Workspace.GetString("owner") ||
			rootPage.GetBool("notion_import_staging") || rootPage.GetBool("in_trash") {
			return nil, nil, false, errPublicContentNotFound
		}
		rows = append(rows, storedPublicContentPageOf(rootPage, "0:"+rootPage.Id))
		limit--
	}

	members := []storedPublicContentPage{}
	err = app.DB().NewQuery(`
		SELECT ` + publicContentPageSummaryColumns + `,
			'1:' || [[member]].[[position]] || ':' || [[member]].[[page_id]] AS [[sort_key]]
		FROM {{leafen_public_share_graph_memberships}} [[member]]
			INDEXED BY idx_leafen_public_share_graph_order
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[member]].[[page_id]]
		WHERE [[member]].[[share_link_id]] = {:authorityId}
			AND [[member]].[[root_page_id]] = {:rootPageId}
			AND [[member]].[[source_revision]] = {:sourceRevision}
			AND [[member]].[[state]] = 'complete' AND [[member]].[[page_id]] != {:rootPageId}
			AND ([[member]].[[position]], [[member]].[[page_id]]) >
				({:cursorPosition}, {:cursorPageId})
			AND [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[owner]] = {:ownerId}
			AND [[page]].[[notion_import_staging]] = 0 AND [[page]].[[in_trash]] = 0
			AND ([[page]].[[data_source_id]] = '' OR ` + wikiRowSQL("[[page]]") + `)
		ORDER BY [[member]].[[position]], [[member]].[[page_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"authorityId": authorization.AuthorityID, "rootPageId": authorization.RootPageID,
		"sourceRevision": authorization.SourceRevision, "workspaceId": authorization.WorkspaceID,
		"ownerId": authorization.Workspace.GetString("owner"), "cursorPosition": cursorPosition,
		"cursorPageId": cursorPageID, "limit": limit,
	}).All(&members)
	if err != nil {
		return nil, nil, false, err
	}
	rows = append(rows, members...)

	hasMore := len(rows) > maxPublicContentSnapshotPages
	if hasMore {
		rows = rows[:maxPublicContentSnapshotPages]
	}
	var nextCursor *string
	if hasMore {
		encoded, err := encodePublicContentCursor(publicContentCursor{
			Scope: publicContentCursorPages, AuthorityID: authorization.AuthorityID,
			SourceRevision: authorization.SourceRevision, Value: rows[len(rows)-1].SortKey,
		})
		if err != nil {
			return nil, nil, false, err
		}
		nextCursor = &encoded
	}
	return publicContentPageSummaries(rows), nextCursor, hasMore, nil
}

// storedPublicContentPage is what a public read lists of a page: what names
// it, and what its opening is said by (publicOpeningWords).
type storedPublicContentPage struct {
	ID        string `db:"id"`
	Title     string `db:"title"`
	ParentID  string `db:"parent_id"`
	Icon      string `db:"icon"`
	IconType  string `db:"icon_type"`
	Kind      string `db:"kind"`
	FullWidth int    `db:"full_width"`
	HasCover  int    `db:"has_cover"`
	SmallText int    `db:"small_text"`
	SortKey   string `db:"sort_key"`
}

// publicContentPageSummaryColumns selects a storedPublicContentPage's own
// columns from a query's [[page]]; its sort key is the query's.
const publicContentPageSummaryColumns = `[[page]].[[id]], [[page]].[[title]], [[page]].[[parent_id]],
			[[page]].[[icon]], [[page]].[[icon_type]], [[page]].[[kind]], [[page]].[[full_width]],
			[[page]].[[cover]] != '' AS [[has_cover]], [[page]].[[small_text]]`

func storedPublicContentPageOf(page *core.Record, sortKey string) storedPublicContentPage {
	return storedPublicContentPage{
		ID: page.Id, Title: page.GetString("title"), ParentID: page.GetString("parent_id"),
		Icon: page.GetString("icon"), IconType: page.GetString("icon_type"), Kind: page.GetString("kind"),
		FullWidth: boolInt(page.GetBool("full_width")), HasCover: boolInt(page.GetString("cover") != ""),
		SmallText: boolInt(page.GetBool("small_text")), SortKey: sortKey,
	}
}

// publicContentPageSummaries names each page a visitor may open; a database
// says so, as an untitled one is called by its kind ("새 데이터베이스"), and
// each says its opening, so the screen shown while a page opened from
// another comes is drawn by its head's rules (publicOpeningWords).
func publicContentPageSummaries(rows []storedPublicContentPage) []publicContentPageSummary {
	summaries := make([]publicContentPageSummary, 0, len(rows))
	for _, row := range rows {
		summary := publicContentPageSummary{
			ID: row.ID, Title: row.Title, ParentID: row.ParentID,
			Icon: row.Icon, IconType: row.IconType,
			Opening: publicOpeningWords(row.Kind, row.FullWidth != 0, row.Icon, row.IconType, row.HasCover != 0, row.SmallText != 0),
		}
		if row.Kind == pageKindDatabase {
			summary.Kind = pageKindDatabase
		}
		summaries = append(summaries, summary)
	}
	return summaries
}
