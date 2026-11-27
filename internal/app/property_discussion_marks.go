package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A value with an open discussion stays marked in its table's cell while the
// discussion is open, as Notion's (seen 2026-11-03: a number cell's "12"
// kept its mark until the discussion was resolved). The page holding the
// rows answers for all of them at once, so a table asks once, not per row.
// A discussion on some words of a text value keeps its mark in the value
// (comment_cell_anchors.go); the page names its open ones, whose marks alone
// show (CellCommentMarks), as Notion's (seen 2026-11-11).

// maxPropertyDiscussionMarks bounds one answer; a page with more open value
// discussions marks the first ones.
const maxPropertyDiscussionMarks = 10000

func registerPropertyDiscussionRoutes(event *core.ServeEvent) {
	event.Router.Route(propertyDiscussionListMethod, apiRoot+propertyDiscussionListPath, listPropertyDiscussions)
}

func listPropertyDiscussions(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	if _, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead); err != nil {
		return commentAPIError(event, err, persistentReadComment)
	}
	marks, err := readPropertyDiscussionMarks(event.App, user.Id, pageID)
	if err != nil {
		return commentAPIError(event, err, persistentReadComment)
	}
	rows, err := readRowDiscussionCounts(event.App, user.Id, pageID)
	if err != nil {
		return commentAPIError(event, err, persistentReadComment)
	}
	threads, err := readOpenWordDiscussions(event.App, user.Id, pageID)
	if err != nil {
		return commentAPIError(event, err, persistentReadComment)
	}
	return event.JSON(http.StatusOK, propertyDiscussionListResponse{Marks: marks, Rows: rows, Threads: threads})
}

// readOpenWordDiscussions names the open discussions started on some words of
// a row's text value, leaving out rows the reader can't see.
func readOpenWordDiscussions(app core.App, readerID, pageID string) ([]string, error) {
	found := []struct {
		ID       string `db:"id"`
		RowID    string `db:"row_id"`
		SourceID string `db:"source_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[comment]].[[id]], [[comment]].[[target_id]] AS [[row_id]], [[row]].[[data_source_id]] AS [[source_id]]
		FROM {{leafen_comments}} [[comment]]
		JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]] = [[comment]].[[target_id]]
		WHERE [[comment]].[[page_id]] = {:page} AND [[comment]].[[target_kind]] = {:kind}
			AND COALESCE([[comment]].[[parent_id]], '') = '' AND [[comment]].[[resolved]] = 0
			AND json_extract([[comment]].[[body_json]], '$.propertyId') IS NOT NULL
			AND json_extract([[comment]].[[body_json]], '$.quoteStart') IS NOT NULL
			AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]") + `
		ORDER BY [[comment]].[[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{"page": pageID, "kind": commentTargetDatabaseRow, "limit": maxPropertyDiscussionMarks}).All(&found)
	if err != nil {
		return nil, err
	}
	sources, rows := map[string]struct{}{}, make([]string, 0, len(found))
	for _, thread := range found {
		sources[thread.SourceID], rows = struct{}{}, append(rows, thread.RowID)
	}
	access, err := loadDatabaseWindowRowAccess(app, readerID, workspaceExportSortedKeys(sources), rows)
	if err != nil {
		return nil, err
	}
	threads := make([]string, 0, len(found))
	for _, thread := range found {
		if !access.hides(thread.RowID) {
			threads = append(threads, thread.ID)
		}
	}
	return threads, nil
}

// readRowDiscussionCounts counts the open discussions on each row as a page,
// not on one of its values, leaving out rows the reader can't see: a row with
// any shows their number beside its title, as Notion's does.
func readRowDiscussionCounts(app core.App, readerID, pageID string) ([]rowDiscussionCount, error) {
	found := []struct {
		RowID    string `db:"row_id"`
		SourceID string `db:"source_id"`
		Count    int    `db:"count"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[comment]].[[target_id]] AS [[row_id]], [[row]].[[data_source_id]] AS [[source_id]], COUNT(*) AS [[count]]
		FROM {{leafen_comments}} [[comment]]
		JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]] = [[comment]].[[target_id]]
		WHERE [[comment]].[[page_id]] = {:page} AND [[comment]].[[target_kind]] = {:kind}
			AND COALESCE([[comment]].[[parent_id]], '') = '' AND [[comment]].[[resolved]] = 0
			AND json_extract([[comment]].[[body_json]], '$.propertyId') IS NULL
			AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]") + `
		GROUP BY [[comment]].[[target_id]], [[row]].[[data_source_id]]
		ORDER BY [[comment]].[[target_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{"page": pageID, "kind": commentTargetDatabaseRow, "limit": maxPropertyDiscussionMarks}).All(&found)
	if err != nil {
		return nil, err
	}
	sources, asked := map[string]struct{}{}, make([]string, 0, len(found))
	for _, row := range found {
		sources[row.SourceID], asked = struct{}{}, append(asked, row.RowID)
	}
	access, err := loadDatabaseWindowRowAccess(app, readerID, workspaceExportSortedKeys(sources), asked)
	if err != nil {
		return nil, err
	}
	rows := make([]rowDiscussionCount, 0, len(found))
	for _, row := range found {
		if !access.hides(row.RowID) {
			rows = append(rows, rowDiscussionCount{RowID: row.RowID, Count: row.Count})
		}
	}
	return rows, nil
}

// readPropertyDiscussionMarks counts the open discussions started on each
// row's whole value, by row and property, leaving out rows the reader can't
// see; one on some words of a text marks those words in the text itself.
func readPropertyDiscussionMarks(app core.App, readerID, pageID string) ([]propertyDiscussionMark, error) {
	found := []struct {
		RowID      string `db:"row_id"`
		SourceID   string `db:"source_id"`
		PropertyID string `db:"property_id"`
		Count      int    `db:"count"`
		First      string `db:"first"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[comment]].[[target_id]] AS [[row_id]], [[row]].[[data_source_id]] AS [[source_id]],
			json_extract([[comment]].[[body_json]], '$.propertyId') AS [[property_id]], COUNT(*) AS [[count]],
			MIN([[comment]].[[created_at]] || char(31) || [[comment]].[[id]]) AS [[first]]
		FROM {{leafen_comments}} [[comment]]
		JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]] = [[comment]].[[target_id]]
		WHERE [[comment]].[[page_id]] = {:page} AND [[comment]].[[target_kind]] = {:kind}
			AND COALESCE([[comment]].[[parent_id]], '') = '' AND [[comment]].[[resolved]] = 0
			AND json_extract([[comment]].[[body_json]], '$.propertyId') IS NOT NULL
			AND json_extract([[comment]].[[body_json]], '$.quoteStart') IS NULL
			AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]") + `
		GROUP BY [[comment]].[[target_id]], [[row]].[[data_source_id]], json_extract([[comment]].[[body_json]], '$.propertyId')
		ORDER BY [[comment]].[[target_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{"page": pageID, "kind": commentTargetDatabaseRow, "limit": maxPropertyDiscussionMarks}).All(&found)
	if err != nil {
		return nil, err
	}
	sources, rows := map[string]struct{}{}, make([]string, 0, len(found))
	for _, mark := range found {
		sources[mark.SourceID], rows = struct{}{}, append(rows, mark.RowID)
	}
	access, err := loadDatabaseWindowRowAccess(app, readerID, workspaceExportSortedKeys(sources), rows)
	if err != nil {
		return nil, err
	}
	marks := make([]propertyDiscussionMark, 0, len(found))
	for _, mark := range found {
		if mark.PropertyID == "" || access.hides(mark.RowID) {
			continue
		}
		// The value's first open discussion is the one pressing the value opens.
		_, first, _ := strings.Cut(mark.First, "\x1f")
		marks = append(marks, propertyDiscussionMark{RowID: mark.RowID, PropertyID: mark.PropertyID, Count: mark.Count, CommentID: first})
	}
	return marks, nil
}
