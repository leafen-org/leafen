package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
)

// A page is found by its path too, as Notion's search finds it: the titles
// of the pages and databases above it hold keywords for it (seen 2026-11-04,
// 2026-11-10). Its path and its own words together hold every keyword — the
// row "W388 fd row Brindavo" of the database "W388 Zelquor fulldb" answers
// "zelquor brindavo" — or, for several keywords, its path holds some while
// its own words hold none ("W388 fd plain row"), found after the pages
// matching some by their own words (seen 2026-11-11, with an inline
// database's rows by its name and a wiki's pages by the wiki's).

// workspaceSearchPathUnits bounds the keywords a path is read for, one bit each.
const workspaceSearchPathUnits = 32

// workspaceSearchPathRead is what a search finds by its pages' paths, read
// once: the pages whose path and words hold every keyword — those whose
// titles hold some first, as Notion's search lists "W388 fd row Brindavo"
// before "W388 fd body row" (seen 2026-11-11) — and those whose path holds
// some of them, each by page.
type workspaceSearchPathRead struct {
	read     bool
	all      []workspaceSearchCandidate
	some     []workspaceSearchCandidate
	complete map[string]bool
}

// pathMatches reads, once, the pages found by their paths. A place is a page
// whose title holds a keyword, or one under it however deep: what it holds is
// under it too — its sub-pages, the databases in its body, its database's
// templates and items, an item's sub-pages — and passes on the keywords its
// path and title hold, an item those of its database's name besides. A page
// holding every keyword in its own words isn't one: its words find it.
func (tiers workspaceSearchTiers) pathMatches() (*workspaceSearchPathRead, error) {
	path := tiers.path
	if path.read {
		return path, nil
	}
	path.read, path.complete = true, map[string]bool{}
	units := tiers.match.TitleTerms
	if len(units) == 0 || len(units) > workspaceSearchPathUnits || len(units) != len(tiers.match.LooseTerms) {
		return path, nil
	}
	// Among the pages matching some keywords a word before the last typed is
	// held whole (workspaceSearchKeywords.someWhole): its whole terms are read
	// too, each a unit past the others (wholeUnits, by its unit).
	titledTerms, ownTerms := append([]string{}, units...), append([]string{}, tiers.match.LooseTerms...)
	wholeUnits := map[int]int{}
	if len(tiers.match.SomeTitleTerms) == len(units) && len(tiers.match.SomeLooseTerms) == len(units) {
		for unit, term := range tiers.match.SomeTitleTerms {
			if term != units[unit] {
				wholeUnits[unit] = len(titledTerms)
				titledTerms, ownTerms = append(titledTerms, term), append(ownTerms, tiers.match.SomeLooseTerms[unit])
			}
		}
	}
	titled := map[string]uint64{}
	for unit, term := range titledTerms {
		ids := []string{}
		if err := tiers.app.DB().NewQuery(`
			SELECT [[projection]].[[page_id]]
			FROM {{leafen_search_pages_fts}} [[fts]]
			CROSS JOIN {{leafen_search_pages}} [[projection]] INDEXED BY [[idx_leafen_search_pages_fts_row]]
				ON [[projection]].[[fts_row]] = [[fts]].[[rowid]]
			WHERE {{leafen_search_pages_fts}} MATCH {:titled}
			LIMIT {:bound}
		`).Bind(dbx.Params{
			"titled": workspaceSearchScopedMatch(tiers.workspaceID, workspaceSearchTitleMatch(term)), "bound": workspaceSearchPathMatches,
		}).Column(&ids); err != nil {
			return nil, err
		}
		for _, id := range ids {
			titled[id] |= 1 << unit
		}
	}
	above, err := tiers.pathsUnder(titled)
	if err != nil || len(above) == 0 {
		return path, err
	}
	candidates := make([]string, 0, len(above))
	for id := range above {
		candidates = append(candidates, id)
	}
	encoded, err := json.Marshal(candidates)
	if err != nil {
		return nil, err
	}
	indexed := []string{}
	if err := tiers.app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_search_pages}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"workspaceId": tiers.workspaceID, "ids": string(encoded)}).Column(&indexed); err != nil {
		return nil, err
	}
	// Each keyword is matched once, its matches then kept to the places' rows
	// (the + keeps those rows from the index): asked of the places' rows one
	// at a time, the index read a start of a word's every match again for
	// each, most of a time sort's seven seconds on a NAS over five hundred
	// places (2026-11-13).
	own := map[string]uint64{}
	for unit, term := range ownTerms {
		ids := []string{}
		if err := tiers.app.DB().NewQuery(`
			SELECT [[projection]].[[page_id]]
			FROM {{leafen_search_pages_fts}} [[fts]]
			CROSS JOIN {{leafen_search_pages}} [[projection]] INDEXED BY [[idx_leafen_search_pages_fts_row]]
				ON [[projection]].[[fts_row]] = [[fts]].[[rowid]]
			WHERE {{leafen_search_pages_fts}} MATCH {:term} AND +[[fts]].[[rowid]] IN (
				SELECT [[fts_row]] FROM {{leafen_search_pages}}
				WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] IN (SELECT [[value]] FROM json_each({:ids}))
			)
		`).Bind(dbx.Params{
			"term": workspaceSearchScopedMatch(tiers.workspaceID, term), "workspaceId": tiers.workspaceID, "ids": string(encoded),
		}).Column(&ids); err != nil {
			return nil, err
		}
		for _, id := range ids {
			own[id] |= 1 << unit
		}
	}
	every := uint64(1)<<len(units) - 1
	phrases := uint64(1)<<tiers.match.Phrases - 1
	// The units held as the pages matching some hold them, a word before the
	// last typed by its whole unit.
	someHeld := func(bits uint64) uint64 {
		for unit, at := range wholeUnits {
			bits = bits&^(1<<unit) | (bits>>at&1)<<unit
		}
		return bits & every
	}
	for _, id := range indexed {
		held := above[id] | own[id]
		some := someHeld(held)
		switch {
		case own[id]&every == every:
		case held&every == every:
			titledFirst := int64(1)
			if titled[id]&every != 0 {
				titledFirst = 0
			}
			path.all = append(path.all, workspaceSearchCandidate{PageID: id, Score: workspaceSearchTierPath, Row: titledFirst})
			path.complete[id] = true
		case tiers.match.Some != "" && someHeld(own[id]) == 0 && some != 0 && some&phrases == phrases:
			path.some = append(path.some, workspaceSearchCandidate{PageID: id, Score: workspaceSearchTierSomePath})
		}
	}
	for _, found := range [][]workspaceSearchCandidate{path.all, path.some} {
		sort.Slice(found, func(left, right int) bool { return workspaceSearchBefore(found[left], found[right], true) })
	}
	return path, nil
}

// pathsUnder is, for each place under the titled ones, the keywords the
// titles above it hold, read a level at a time; a place reached again by
// another way holds those of both. Each way down is read from the level's
// places one by one, each found by its key: kept as a list to look in, the
// level's two thousand places were sorted into one five times a level, a
// third of a search's reading of paths on a NAS (2026-11-13).
func (tiers workspaceSearchTiers) pathsUnder(titled map[string]uint64) (map[string]uint64, error) {
	above := map[string]uint64{}
	passes := map[string]uint64{}
	for id, held := range titled {
		passes[id] = held
	}
	for len(passes) > 0 {
		// In order, so the places past the bound left out are the same at
		// each call of a search.
		parents := make([]string, 0, len(passes))
		for id := range passes {
			parents = append(parents, id)
		}
		sort.Strings(parents)
		encoded, err := json.Marshal(parents)
		if err != nil {
			return nil, err
		}
		held := []struct {
			ID       string `db:"id"`
			ParentID string `db:"parent_id"`
			// Opens is whether what it holds is under it: a page's, a
			// database's or an item's, not a template's.
			Opens bool `db:"opens"`
		}{}
		if err := tiers.app.DB().NewQuery(`
			SELECT [[page]].[[id]], [[page]].[[parent_id]], TRUE AS [[opens]]
			FROM json_each({:ids}) [[place]]
			CROSS JOIN {{leafen_pages}} [[page]] ON [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[parent_id]] = [[place]].[[value]]
			UNION ALL
			SELECT [[container]].[[id]], [[container]].[[page_id]], TRUE FROM json_each({:ids}) [[place]]
			CROSS JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[page_id]] = [[place]].[[value]]
				AND [[container]].[[kind]] = 'source' AND [[container]].[[workspace_id]] = {:workspaceId}
			UNION ALL
			SELECT [[template]].[[id]], [[template]].[[page_id]], FALSE FROM json_each({:ids}) [[place]]
			CROSS JOIN {{leafen_pages}} [[placed]] ON [[placed]].[[id]] = [[place]].[[value]]
			CROSS JOIN {{leafen_database_templates}} [[template]] ON [[template]].[[workspace_id]] = {:workspaceId}
				AND [[template]].[[owner]] = [[placed]].[[owner]] AND [[template]].[[page_id]] = [[placed]].[[id]]
			UNION ALL
			SELECT [[row_page]].[[row_id]], [[row_page]].[[container_page_id]], TRUE FROM json_each({:ids}) [[place]]
			CROSS JOIN {{leafen_pages}} [[placed]] ON [[placed]].[[id]] = [[place]].[[value]]
			CROSS JOIN {{leafen_database_row_pages}} [[row_page]] ON [[row_page]].[[owner]] = [[placed]].[[owner]]
				AND [[row_page]].[[workspace_id]] = {:workspaceId} AND [[row_page]].[[container_page_id]] = [[placed]].[[id]]
			UNION ALL
			SELECT [[row_page]].[[row_id]], [[container]].[[id]], TRUE FROM json_each({:ids}) [[place]]
			CROSS JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[id]] = [[place]].[[value]]
				AND [[container]].[[kind]] = 'source'
			CROSS JOIN {{leafen_database_container_sources}} [[member]] ON [[member]].[[workspace_id]] = {:workspaceId}
				AND [[member]].[[container_id]] = [[container]].[[id]]
			CROSS JOIN {{leafen_database_row_pages}} [[row_page]] ON [[row_page]].[[owner]] = [[member]].[[owner]]
				AND [[row_page]].[[workspace_id]] = {:workspaceId} AND [[row_page]].[[container_page_id]] = [[container]].[[page_id]]
				AND [[row_page]].[[data_source_id]] = [[member]].[[data_source_id]]
		`).Bind(dbx.Params{"workspaceId": tiers.workspaceID, "ids": string(encoded)}).All(&held); err != nil {
			return nil, err
		}
		next := map[string]uint64{}
		for _, place := range held {
			// An item is under its database's page and its database, whose
			// name its path names.
			reached := passes[place.ParentID]
			before, seen := above[place.ID]
			if seen && before|reached == before || !seen && len(above) >= workspaceSearchPathMatches {
				continue
			}
			above[place.ID] = before | reached
			if place.Opens {
				next[place.ID] = above[place.ID] | titled[place.ID]
			}
		}
		passes = next
	}
	return above, nil
}
