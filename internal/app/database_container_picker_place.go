package app

import "github.com/pocketbase/pocketbase/core"

// A search of the sources to link, search or relate to finds a source by its
// place too, as Notion's does (seen 2026-11-14: "경로부모" lists "경로부모 표"
// by its name, then "w978 목록" by the page "w978 경로부모" it sits in): the
// names of the pages its database sits in, its own page up to the top. Those
// found by their name come first (searchTitleScore); after them, those whose
// place holds every word with their name, then those whose place holds some.
// A place read whole for the search is read again as its reader reads it
// (pagePathAbove) before the source is listed by it, so a page above it the
// reader cannot read finds nothing.

const (
	databaseContainerPickerPlaceScore     = 5
	databaseContainerPickerPlaceSomeScore = 6
)

type databaseContainerPickerPlacePage struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
	Title    string `db:"title"`
}

// places reads, by page, the pages the workspace's databases sit in: each
// database's own page and every page above it, each once.
func (scope databaseContainerPickerScope) places() (map[string]databaseContainerPickerPlacePage, error) {
	pages := []databaseContainerPickerPlacePage{}
	if err := scope.app.DB().NewQuery(`
		WITH RECURSIVE [[place]] ([[id]], [[parent_id]], [[title]]) AS (
			SELECT [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `, [[page]].[[title]]
			FROM {{leafen_pages}} AS [[page]]
			WHERE [[page]].[[workspace]]={:workspaceId} AND [[page]].[[id]] IN (
				SELECT [[container]].[[page_id]] FROM {{leafen_database_containers}} AS [[container]]
				WHERE [[container]].[[owner]]={:owner} AND [[container]].[[workspace_id]]={:workspaceId}
					AND [[container]].[[kind]]='source'
			)
			UNION
			SELECT [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `, [[page]].[[title]]
			FROM {{leafen_pages}} AS [[page]]
			JOIN [[place]] ON [[page]].[[id]]=[[place]].[[parent_id]]
			WHERE [[place]].[[parent_id]] != '' AND [[page]].[[workspace]]={:workspaceId}
		)
		SELECT [[id]], [[parent_id]], [[title]] FROM [[place]]
	`).Bind(scope.params).All(&pages); err != nil {
		return nil, err
	}
	byID := make(map[string]databaseContainerPickerPlacePage, len(pages))
	for _, page := range pages {
		byID[page.ID] = page
	}
	return byID, nil
}

// databaseContainerPickerPlaceTitles are the names of a database's page and
// the pages above it, its own first, each page once.
func databaseContainerPickerPlaceTitles(places map[string]databaseContainerPickerPlacePage, pageID string) []string {
	titles := []string{}
	seen := map[string]bool{}
	for id := pageID; id != "" && !seen[id]; {
		page, ok := places[id]
		if !ok {
			break
		}
		seen[id] = true
		titles = append(titles, page.Title)
		id = page.ParentID
	}
	return titles
}

// databaseContainerPickerPlaceMatch ranks a source its name does not find by its place: every word
// with its name, or some, and whether the place finds it at all.
func databaseContainerPickerPlaceMatch(keywords workspaceSearchKeywords, title string, place []string) (int, bool) {
	all, some := keywords.match(append([]string{title}, place...)...)
	switch {
	case all:
		return databaseContainerPickerPlaceScore, true
	case some:
		return databaseContainerPickerPlaceSomeScore, true
	}
	return 0, false
}

// databaseContainerPickerPlaceHolds is whether a source found by its place
// still is when that place is read as its reader reads it: its page's name
// and the pieces above it the reader can read.
func databaseContainerPickerPlaceHolds(app core.App, reader string, page *core.Record, title string, keywords workspaceSearchKeywords) (bool, error) {
	pieces, err := pagePathAbove(app, reader, page)
	if err != nil {
		return false, err
	}
	place := []string{page.GetString("title")}
	for _, piece := range pieces {
		place = append(place, piece.Title)
	}
	_, held := databaseContainerPickerPlaceMatch(keywords, title, place)
	return held, nil
}
