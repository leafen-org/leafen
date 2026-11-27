package app

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The order every list of sources to link, search or relate to reads
// (readDatabaseContainerPicker), as Notion's (seen 2026-11-11: its source
// lists ask its search for "relevance" with the reader's recent visits to
// boost): a search's best found first (searchTitleScore), then those the list
// asks first (a block's own, its "추천"), then those whose page the reader
// opened, the latest visit first, then the rest by their latest change, then
// by their database and their place in it.
//
// A page reads no further than it lists, not every source to sort them all:
// those asked first and those the reader visited are few (a person keeps the
// newest 200 visits a workspace, page_visits.go), and the rest come in the
// order of their latest change from an index (data_source_recent_index_schema.go).
// A cursor names the last source a page went through, and the next page
// starts right after it. A search still reads every source's title, as only
// the keyword rule tells what it finds, but sorts only those it finds and
// reads the views of no more than a page of them.

// The longest cursor a list hands out: two identifiers and a place.
const databaseContainerPickerMaximumCursor = 2048

type databaseContainerPickerKey struct {
	Score     int    `json:"s"`
	Later     int    `json:"f"`
	Unvisited int    `json:"n"`
	VisitedAt int64  `json:"v"`
	Updated   int64  `json:"u"`
	ID        string `json:"c"`
	Position  string `json:"p"`
	Source    string `json:"d"`
}

func (key databaseContainerPickerKey) compare(other databaseContainerPickerKey) int {
	return cmp.Or(
		cmp.Compare(key.Score, other.Score),
		cmp.Compare(key.Later, other.Later),
		cmp.Compare(key.Unvisited, other.Unvisited),
		cmp.Compare(other.VisitedAt, key.VisitedAt),
		cmp.Compare(other.Updated, key.Updated),
		strings.Compare(key.ID, other.ID),
		strings.Compare(key.Position, other.Position),
		strings.Compare(key.Source, other.Source),
	)
}

func (key databaseContainerPickerKey) cursor() string {
	encoded, _ := json.Marshal(key)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func readDatabaseContainerPickerCursor(cursor string) (databaseContainerPickerKey, bool) {
	key := databaseContainerPickerKey{}
	if len(cursor) > databaseContainerPickerMaximumCursor {
		return key, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || json.Unmarshal(raw, &key) != nil || key.Score < 0 ||
		key.Later < 0 || key.Later > 1 || key.Unvisited < 0 || key.Unvisited > 1 ||
		key.ID == "" || len(key.ID) > maxGraphIdentifierLength || len(key.Source) > maxGraphIdentifierLength {
		return databaseContainerPickerKey{}, false
	}
	return key, true
}

type databaseContainerPickerCandidate struct {
	ID           string `db:"id"`
	PageID       string `db:"page_id"`
	DataSourceID string `db:"data_source_id"`
	ViewID       string `db:"view_id"`
	Title        string `db:"title"`
	Updated      int64  `db:"updated"`
	Position     string `db:"position"`
	// The reader's latest visit to its page, -1 for none.
	VisitedAt int64 `db:"visited_at"`
	key       databaseContainerPickerKey
	// Found by its place, not its name (database_container_picker_place.go).
	byPlace bool
}

func (candidate *databaseContainerPickerCandidate) place(first map[string]bool, score int) {
	candidate.key = databaseContainerPickerKey{
		Score: score, Later: 1, Unvisited: 1, VisitedAt: -1, Updated: candidate.Updated,
		ID: candidate.ID, Position: candidate.Position, Source: candidate.DataSourceID,
	}
	if first[candidate.DataSourceID] {
		candidate.key.Later = 0
	}
	if candidate.VisitedAt >= 0 {
		candidate.key.Unvisited, candidate.key.VisitedAt = 0, candidate.VisitedAt
	}
}

// databaseContainerPickerScope reads a workspace's sources for one reader.
type databaseContainerPickerScope struct {
	app      core.App
	params   dbx.Params
	first    []any
	firstSet map[string]bool
}

func newDatabaseContainerPickerScope(app core.App, owner, workspaceID, reader string, first []any) databaseContainerPickerScope {
	scope := databaseContainerPickerScope{
		app: app, first: first, firstSet: map[string]bool{},
		params: dbx.Params{"owner": owner, "workspaceId": workspaceID, "reader": reader},
	}
	for _, id := range first {
		scope.firstSet[id.(string)] = true
	}
	for name, value := range inlineDatabaseInParams("first", first) {
		scope.params[name] = value
	}
	return scope
}

// The view a new linked database copies: the database's first of the
// source's own.
const databaseContainerPickerViewSQL = `COALESCE((
		SELECT [[view_membership]].[[view_id]]
		FROM {{leafen_database_container_views}} AS [[view_membership]]
		JOIN {{leafen_database_views}} AS [[view]]
			ON [[view]].[[id]]=[[view_membership]].[[view_id]]
			AND [[view]].[[data_source_id]]=[[membership]].[[data_source_id]]
		WHERE [[view_membership]].[[container_id]]=[[container]].[[id]]
		ORDER BY [[view_membership]].[[position]], [[view_membership]].[[view_id]] LIMIT 1
	), '') AS [[view_id]]`

// A source in the trash is offered by no list, as Notion's lists of sources
// to link, relate to or start a database from leave one out (seen
// 2026-11-19: one of a database's sources sent to the trash from its 데이터
// 소스 관리 left the relation window's and the new database window's search,
// and came back with it): its database in the trash, or it kept in its
// database in the trash where it stands (database_source_trash_in_place.go).
var databaseContainerPickerNotTrashedSQL = `NOT EXISTS (
		SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
		WHERE [[trash]].[[container_id]]=[[container]].[[id]]
	) AND NOT ` + databaseSourceTrashedInPlaceSQL("[[membership]].[[data_source_id]]")

// databaseContainerPickerRecentSQL reads the workspace's own sources, the
// latest changed first, each in its database, with the reader's visit to the
// database's page.
var databaseContainerPickerRecentSQL = `
	FROM {{leafen_data_sources}} AS [[source]] INDEXED BY [[` + dataSourceRecentIndex + `]]
	CROSS JOIN {{leafen_database_container_sources}} AS [[membership]]
	CROSS JOIN {{leafen_database_containers}} AS [[container]]
	LEFT JOIN {{leafen_page_visits}} AS [[visit]]
		ON [[visit]].[[owner_id]]={:reader} AND [[visit]].[[page_id]]=[[container]].[[page_id]]
	WHERE [[source]].[[owner]]={:owner} AND [[source]].[[workspace_id]]={:workspaceId}
		AND [[membership]].[[owner]]=[[source]].[[owner]] AND [[membership]].[[workspace_id]]=[[source]].[[workspace_id]]
		AND [[membership]].[[data_source_id]]=[[source]].[[id]] AND [[membership]].[[source_container_id]]=''
		AND [[container]].[[id]]=[[membership]].[[container_id]] AND [[container]].[[owner]]=[[source]].[[owner]]
		AND [[container]].[[workspace_id]]=[[source]].[[workspace_id]] AND [[container]].[[kind]]='source'
		AND ` + databaseContainerPickerNotTrashedSQL

const databaseContainerPickerColumnsSQL = `[[container]].[[id]], [[container]].[[page_id]],
		[[membership]].[[data_source_id]], [[source]].[[title]], [[source]].[[updated]], [[membership]].[[position]]`

// list reads, after `after`, up to `want` sources in the order without a
// search: those asked first and those the reader visited, then the rest from
// the index, from where the cursor left off.
func (scope databaseContainerPickerScope) list(after *databaseContainerPickerKey, want int) ([]databaseContainerPickerCandidate, error) {
	found := []databaseContainerPickerCandidate{}
	if after == nil || after.Later == 0 || after.Unvisited == 0 {
		firstSQL := ""
		if len(scope.first) > 0 {
			firstSQL = `SELECT [[asked]].[[container_id]] AS [[id]] FROM {{leafen_database_container_sources}} AS [[asked]]
				WHERE [[asked]].[[owner]]={:owner} AND [[asked]].[[workspace_id]]={:workspaceId}
					AND [[asked]].[[data_source_id]] IN (` + inlineDatabaseInList("first", scope.first) + `)
				UNION `
		}
		// Each of these databases is read once, from their few ids: joined in
		// the planner's own order it looked every one of them up again for
		// each source of them.
		lead := []databaseContainerPickerCandidate{}
		if err := scope.app.DB().NewQuery(`
			SELECT ` + databaseContainerPickerColumnsSQL + `,
				COALESCE([[visit]].[[visited_at]], -1) AS [[visited_at]], ` + databaseContainerPickerViewSQL + `
			FROM (` + firstSQL + `
				SELECT [[visited]].[[id]] FROM {{leafen_page_visits}} AS [[opened]]
				JOIN {{leafen_database_containers}} AS [[visited]]
					ON [[visited]].[[page_id]]=[[opened]].[[page_id]] AND [[visited]].[[kind]]='source'
				WHERE [[opened]].[[owner_id]]={:reader}
			) AS [[lead]]
			CROSS JOIN {{leafen_database_containers}} AS [[container]]
			CROSS JOIN {{leafen_database_container_sources}} AS [[membership]]
			CROSS JOIN {{leafen_data_sources}} AS [[source]]
			LEFT JOIN {{leafen_page_visits}} AS [[visit]]
				ON [[visit]].[[owner_id]]={:reader} AND [[visit]].[[page_id]]=[[container]].[[page_id]]
			WHERE [[container]].[[id]]=[[lead]].[[id]]
				AND [[container]].[[owner]]={:owner} AND [[container]].[[workspace_id]]={:workspaceId}
				AND [[container]].[[kind]]='source'
				AND [[membership]].[[container_id]]=[[container]].[[id]] AND [[membership]].[[source_container_id]]=''
				AND [[source]].[[id]]=[[membership]].[[data_source_id]]
				AND [[source]].[[owner]]=[[container]].[[owner]]
				AND [[source]].[[workspace_id]]=[[container]].[[workspace_id]]
				AND ` + databaseContainerPickerNotTrashedSQL + `
				AND NOT ` + databaseViewBlockSQL("[[container]]") + `
		`).Bind(scope.params).All(&lead); err != nil {
			return nil, err
		}
		for _, candidate := range lead {
			candidate.place(scope.firstSet, 0)
			if (candidate.key.Later == 0 || candidate.key.Unvisited == 0) && (after == nil || candidate.key.compare(*after) > 0) {
				found = append(found, candidate)
			}
		}
		slices.SortFunc(found, func(left, right databaseContainerPickerCandidate) int { return left.key.compare(right.key) })
		if len(found) >= want {
			return found[:want], nil
		}
		after = nil
	}
	params := dbx.Params{"limit": want - len(found)}
	for name, value := range scope.params {
		params[name] = value
	}
	window := ""
	if after != nil {
		window = `AND [[source]].[[updated]]<={:updated} AND ([[source]].[[updated]]<{:updated} OR
			([[container]].[[id]], [[membership]].[[position]], [[membership]].[[data_source_id]]) > ({:id}, {:position}, {:source}))`
		params["updated"], params["id"], params["position"], params["source"] = after.Updated, after.ID, after.Position, after.Source
	}
	asked := ""
	if len(scope.first) > 0 {
		asked = `AND [[membership]].[[data_source_id]] NOT IN (` + inlineDatabaseInList("first", scope.first) + `)`
	}
	rest := []databaseContainerPickerCandidate{}
	if err := scope.app.DB().NewQuery(`
		SELECT ` + databaseContainerPickerColumnsSQL + `, -1 AS [[visited_at]], ` + databaseContainerPickerViewSQL + `
		` + databaseContainerPickerRecentSQL + `
			AND [[visit]].[[page_id]] IS NULL ` + asked + `
			AND NOT ` + databaseViewBlockSQL("[[container]]") + `
			` + window + `
		ORDER BY [[source]].[[updated]] DESC, [[container]].[[id]], [[membership]].[[position]], [[membership]].[[data_source_id]]
		LIMIT {:limit}
	`).Bind(params).All(&rest); err != nil {
		return nil, err
	}
	for _, candidate := range rest {
		candidate.place(scope.firstSet, 0)
		found = append(found, candidate)
	}
	return found, nil
}

// search reads every source's title and keeps, after `after`, up to `want`
// of those a search's keywords find, by the one rule every search judges a
// title by (workspace_search_keywords.go): those holding every word first,
// then those holding some, as Notion's lists of sources to link find "W411 …"
// by "w409원천" (seen 2026-11-11), ranked as Notion's search ranks titles;
// after them, those found by their place (database_container_picker_place.go).
func (scope databaseContainerPickerScope) search(query string, after *databaseContainerPickerKey, want int) ([]databaseContainerPickerCandidate, error) {
	read := []databaseContainerPickerCandidate{}
	if err := scope.app.DB().NewQuery(`
		SELECT ` + databaseContainerPickerColumnsSQL + `, COALESCE([[visit]].[[visited_at]], -1) AS [[visited_at]]
		` + databaseContainerPickerRecentSQL + `
	`).Bind(scope.params).All(&read); err != nil {
		return nil, err
	}
	places, err := scope.places()
	if err != nil {
		return nil, err
	}
	folded := foldSearchText(query)
	keywords := readWorkspaceSearchKeywords(folded)
	kept := []databaseContainerPickerCandidate{}
	for _, candidate := range read {
		all, some := keywords.match(candidate.Title)
		score := searchTitleScore(folded, foldSearchText(candidate.Title), all)
		if !some {
			if score, some = databaseContainerPickerPlaceMatch(keywords, candidate.Title,
				databaseContainerPickerPlaceTitles(places, candidate.PageID)); !some {
				continue
			}
			candidate.byPlace = true
		}
		candidate.place(scope.firstSet, score)
		if after == nil || candidate.key.compare(*after) > 0 {
			kept = append(kept, candidate)
		}
	}
	slices.SortFunc(kept, func(left, right databaseContainerPickerCandidate) int { return left.key.compare(right.key) })
	return scope.shown(kept, want)
}

// shown keeps, in order, up to `want` of the found sources that are no view
// block (database_view_block.go), each with its view to copy.
func (scope databaseContainerPickerScope) shown(found []databaseContainerPickerCandidate, want int) ([]databaseContainerPickerCandidate, error) {
	kept := []databaseContainerPickerCandidate{}
	for len(found) > 0 && len(kept) < want {
		batch := found[:min(len(found), want-len(kept))]
		found = found[len(batch):]
		ids := []any{}
		for _, candidate := range batch {
			ids = append(ids, candidate.ID)
		}
		views := []struct {
			ID           string `db:"id"`
			DataSourceID string `db:"data_source_id"`
			ViewID       string `db:"view_id"`
		}{}
		if err := scope.app.DB().NewQuery(`
			SELECT [[container]].[[id]], [[membership]].[[data_source_id]], ` + databaseContainerPickerViewSQL + `
			FROM {{leafen_database_containers}} AS [[container]]
			CROSS JOIN {{leafen_database_container_sources}} AS [[membership]]
			WHERE [[container]].[[id]] IN (` + inlineDatabaseInList("container", ids) + `)
				AND [[membership]].[[container_id]]=[[container]].[[id]]
				AND NOT ` + databaseViewBlockSQL("[[container]]") + `
		`).Bind(inlineDatabaseInParams("container", ids)).All(&views); err != nil {
			return nil, err
		}
		viewOf := map[[2]string]string{}
		for _, view := range views {
			viewOf[[2]string{view.ID, view.DataSourceID}] = view.ViewID
		}
		for _, candidate := range batch {
			if view, ok := viewOf[[2]string{candidate.ID, candidate.DataSourceID}]; ok {
				candidate.ViewID = view
				kept = append(kept, candidate)
			}
		}
	}
	return kept, nil
}
