package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Search narrows its results as Notion's search does: to matches in titles,
// to pages people created or last edited, to teamspaces, to pages inside
// chosen pages and their sub-pages, to the items of chosen data sources, to
// pages created or edited within a time range, and to live, archived or
// verified pages. It sorts by best match, or by when pages were last edited
// or created, newest or oldest first. A database item is inside, and in the
// teamspace of, the page that holds its database, and is archived when it or
// that page is.

const (
	workspaceSearchSortBest          = "best"
	workspaceSearchSortEditedNewest  = "edited_desc"
	workspaceSearchSortEditedOldest  = "edited_asc"
	workspaceSearchSortCreatedNewest = "created_desc"
	workspaceSearchSortCreatedOldest = "created_asc"

	// Search shows live pages unless asked for archived pages, which are
	// archived themselves or inside an archived page, for both, or for
	// pages with a current verification.
	workspaceSearchContentActive   = "active"
	workspaceSearchContentArchived = "archived"
	workspaceSearchContentAll      = "all"
	workspaceSearchContentVerified = "verified"
)

var errWorkspaceSearchFilterInvalid = errors.New(errorCodeWorkspaceSearchInvalid)

type workspaceSearchFilter struct {
	TitleOnly   bool
	CreatedBy   []string
	EditedBy    []string
	Teamspaces  []string
	In          []string
	DataSources []string
	// After and Before bound, in Unix milliseconds, when a page was created
	// or edited; zero leaves that side open. The others bound only when it
	// was created, or only when it was last edited.
	After         int64
	Before        int64
	CreatedAfter  int64
	CreatedBefore int64
	EditedAfter   int64
	EditedBefore  int64
	// Content is active (live pages, the default), archived, all or
	// verified.
	Content string
	Sort    string
}

func parseWorkspaceSearchFilter(values url.Values) (workspaceSearchFilter, error) {
	filter := workspaceSearchFilter{
		Sort: strings.TrimSpace(values.Get("sort")), Content: strings.TrimSpace(values.Get("contentState")),
	}
	switch filter.Content {
	case "":
		filter.Content = workspaceSearchContentActive
	case workspaceSearchContentActive, workspaceSearchContentArchived, workspaceSearchContentAll, workspaceSearchContentVerified:
	default:
		return workspaceSearchFilter{}, refuse(errWorkspaceSearchFilterInvalid, "contentState is %s, not %q", notionOrList([]string{
			workspaceSearchContentActive, workspaceSearchContentArchived, workspaceSearchContentAll, workspaceSearchContentVerified,
		}), filter.Content)
	}
	switch filter.Sort {
	case "":
		filter.Sort = workspaceSearchSortBest
	case workspaceSearchSortBest, workspaceSearchSortEditedNewest, workspaceSearchSortEditedOldest,
		workspaceSearchSortCreatedNewest, workspaceSearchSortCreatedOldest:
	default:
		return workspaceSearchFilter{}, refuse(errWorkspaceSearchFilterInvalid, "sort is %s, not %q", notionOrList([]string{
			workspaceSearchSortBest, workspaceSearchSortEditedNewest, workspaceSearchSortEditedOldest,
			workspaceSearchSortCreatedNewest, workspaceSearchSortCreatedOldest,
		}), filter.Sort)
	}
	switch values.Get("titleOnly") {
	case "", "false":
	case "true":
		filter.TitleOnly = true
	default:
		return workspaceSearchFilter{}, refuse(errWorkspaceSearchFilterInvalid, "titleOnly is true or false, not %q", values.Get("titleOnly"))
	}
	list := func(name string) ([]string, error) {
		raw := strings.TrimSpace(values.Get(name))
		if raw == "" {
			return nil, nil
		}
		// Any number of values, as many as the address carries; the queries
		// read them as one JSON list.
		ids := []string{}
		seen := map[string]struct{}{}
		for _, part := range strings.Split(raw, ",") {
			id := strings.TrimSpace(part)
			if id == "" {
				return nil, refuse(errWorkspaceSearchFilterInvalid, "%s holds an empty ID between its commas", name)
			}
			if len(id) > maxWorkspacePermissionIdentifierLength {
				return nil, refuse(errWorkspaceSearchFilterInvalid, "%s holds an ID over %d characters", name, maxWorkspacePermissionIdentifierLength)
			}
			if _, again := seen[id]; !again {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		return ids, nil
	}
	instant := func(name string) (int64, error) {
		raw := strings.TrimSpace(values.Get(name))
		if raw == "" {
			return 0, nil
		}
		return workspaceSearchInstant(name, raw)
	}
	var err error
	if filter.CreatedBy, err = list("createdBy"); err != nil {
		return workspaceSearchFilter{}, err
	}
	if filter.Teamspaces, err = list("teamspace"); err != nil {
		return workspaceSearchFilter{}, err
	}
	if filter.In, err = list("in"); err != nil {
		return workspaceSearchFilter{}, err
	}
	if filter.After, err = instant("after"); err != nil {
		return workspaceSearchFilter{}, err
	}
	if filter.Before, err = instant("before"); err != nil {
		return workspaceSearchFilter{}, err
	}
	if filter.After > 0 && filter.Before > 0 && filter.After >= filter.Before {
		return workspaceSearchFilter{}, refuse(errWorkspaceSearchFilterInvalid, "after is the same as or later than before, so no page falls between them")
	}
	// The search window's date filter bounds when pages were created or when
	// they were last edited, as Notion's "생성일 · 최종 편집" (seen 2026-10-12);
	// without it, either.
	switch values.Get("dateField") {
	case "":
	case "created":
		filter.CreatedAfter, filter.CreatedBefore, filter.After, filter.Before = filter.After, filter.Before, 0, 0
	case "edited":
		filter.EditedAfter, filter.EditedBefore, filter.After, filter.Before = filter.After, filter.Before, 0, 0
	default:
		return workspaceSearchFilter{}, refuse(errWorkspaceSearchFilterInvalid, "dateField is created or edited, not %q", values.Get("dateField"))
	}
	return filter, nil
}

// workspaceSearchInstant reads a filter's time, an RFC 3339 instant after
// 1970, in Unix milliseconds.
func workspaceSearchInstant(name, raw string) (int64, error) {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return 0, refuse(errWorkspaceSearchFilterInvalid, "%s is an RFC 3339 time such as 2026-01-31T09:00:00Z, not %q", name, raw)
	}
	if parsed.UnixMilli() <= 0 {
		return 0, refuse(errWorkspaceSearchFilterInvalid, "%s is after 1970", name)
	}
	return parsed.UnixMilli(), nil
}

// narrows reports whether the filter changes what plain search shows.
func (filter workspaceSearchFilter) narrows() bool {
	return filter.TitleOnly || len(filter.CreatedBy) > 0 || len(filter.EditedBy) > 0 || len(filter.Teamspaces) > 0 ||
		len(filter.In) > 0 || len(filter.DataSources) > 0 || filter.After > 0 || filter.Before > 0 ||
		filter.CreatedAfter > 0 || filter.CreatedBefore > 0 || filter.EditedAfter > 0 || filter.EditedBefore > 0 ||
		filter.content() != workspaceSearchContentActive || filter.Sort != workspaceSearchSortBest
}

func (filter workspaceSearchFilter) content() string {
	if filter.Content == "" {
		return workspaceSearchContentActive
	}
	return filter.Content
}

// forBrowse reads a search without keywords as browsing, which needs a
// filter or a time sort; without keywords the best match is the most
// recently edited.
func (filter workspaceSearchFilter) forBrowse() (workspaceSearchFilter, bool) {
	if !filter.narrows() {
		return filter, false
	}
	if filter.Sort == workspaceSearchSortBest {
		filter.Sort = workspaceSearchSortEditedNewest
	}
	return filter, true
}

// bestMatch reports whether results rank by how well their titles match.
func (filter workspaceSearchFilter) bestMatch() bool {
	return filter.Sort == workspaceSearchSortBest
}

// hashKey is what a search position is bound to besides the query; plain
// search keeps the key it always had.
func (filter workspaceSearchFilter) hashKey() string {
	if !filter.narrows() {
		return ""
	}
	times := []string{}
	for _, at := range []int64{
		filter.After, filter.Before, filter.CreatedAfter, filter.CreatedBefore, filter.EditedAfter, filter.EditedBefore,
	} {
		times = append(times, strconv.FormatInt(at, 10))
	}
	return "\x00" + strconv.FormatBool(filter.TitleOnly) + "\x00" + strings.Join(filter.CreatedBy, ",") +
		"\x00" + strings.Join(filter.Teamspaces, ",") + "\x00" + strings.Join(filter.In, ",") +
		"\x00" + strings.Join(times[:2], "\x00") + "\x00" + filter.Sort +
		"\x00" + strings.Join(filter.EditedBy, ",") + "\x00" + strings.Join(filter.DataSources, ",") +
		"\x00" + strings.Join(times[2:], "\x00") + "\x00" + filter.content()
}

type workspaceSearchCandidateFacts struct {
	ID           string `db:"id"`
	CreatedBy    string `db:"created_by"`
	LastEditedBy string `db:"last_edited_by"`
	TeamspaceID  string `db:"teamspace_id"`
	Created      string `db:"created"`
	Updated      string `db:"updated"`
	// Anchor is the page whose place counts: the page itself, a database
	// item's own page among them, whose place runs through the page above it.
	Anchor string `db:"anchor"`
	// DataSourceID and Archived are a database item's own.
	DataSourceID string `db:"data_source_id"`
	Archived     string `db:"archived"`
	created      int64
	updated      int64
}

// applyWorkspaceSearchFilter keeps the readable candidates in the content
// state asked for that the filter admits and, for a sort by time, ranks
// them by that time.
func applyWorkspaceSearchFilter(
	app core.App, filter workspaceSearchFilter, candidates []workspaceSearchCandidate,
) ([]workspaceSearchCandidate, error) {
	if len(candidates) == 0 {
		return candidates, nil
	}
	if filter.TitleOnly {
		candidates = slices.DeleteFunc(slices.Clone(candidates), func(candidate workspaceSearchCandidate) bool {
			return !workspaceSearchTitleOnlyTier(candidate.Score)
		})
	}
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.PageID)
	}
	facts, err := loadWorkspaceSearchCandidateFacts(app, ids)
	if err != nil {
		return nil, err
	}
	content, err := loadWorkspaceSearchContentStates(app, filter.content(), facts)
	if err != nil {
		return nil, err
	}
	var places map[string]workspaceSearchPlace
	if len(filter.In) > 0 || len(filter.Teamspaces) > 0 {
		anchors := make([]string, 0, len(facts))
		for _, fact := range facts {
			anchors = append(anchors, fact.Anchor)
		}
		if places, err = loadWorkspaceSearchPlaces(app, anchors); err != nil {
			return nil, err
		}
	}
	kept := make([]workspaceSearchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		fact, found := facts[candidate.PageID]
		if !found || !content[candidate.PageID] {
			continue
		}
		if len(filter.CreatedBy) > 0 && !slices.Contains(filter.CreatedBy, fact.CreatedBy) {
			continue
		}
		if len(filter.EditedBy) > 0 && !slices.Contains(filter.EditedBy, fact.LastEditedBy) {
			continue
		}
		if len(filter.DataSources) > 0 && !slices.Contains(filter.DataSources, fact.DataSourceID) {
			continue
		}
		place := places[fact.Anchor]
		if len(filter.In) > 0 && !slices.ContainsFunc(filter.In, func(id string) bool {
			return id == candidate.PageID || slices.Contains(place.Ancestors, id)
		}) {
			continue
		}
		if len(filter.Teamspaces) > 0 && !slices.ContainsFunc(place.Teamspaces, func(id string) bool {
			return slices.Contains(filter.Teamspaces, id)
		}) && !slices.Contains(filter.Teamspaces, fact.TeamspaceID) {
			continue
		}
		if (filter.After > 0 || filter.Before > 0) &&
			!workspaceSearchWithin(filter.After, filter.Before, fact.created) &&
			!workspaceSearchWithin(filter.After, filter.Before, fact.updated) {
			continue
		}
		if !workspaceSearchWithin(filter.CreatedAfter, filter.CreatedBefore, fact.created) ||
			!workspaceSearchWithin(filter.EditedAfter, filter.EditedBefore, fact.updated) {
			continue
		}
		switch filter.Sort {
		case workspaceSearchSortEditedNewest:
			candidate.Score = -fact.updated
		case workspaceSearchSortEditedOldest:
			candidate.Score = fact.updated
		case workspaceSearchSortCreatedNewest:
			candidate.Score = -fact.created
		case workspaceSearchSortCreatedOldest:
			candidate.Score = fact.created
		}
		kept = append(kept, candidate)
	}
	return kept, nil
}

func workspaceSearchWithin(after, before, at int64) bool {
	return (after == 0 || at >= after) && (before == 0 || at < before)
}

// loadWorkspaceSearchCandidateFacts reads who made and last edited each
// candidate, where it is and when it was created and last edited.
func loadWorkspaceSearchCandidateFacts(app core.App, ids []string) (map[string]workspaceSearchCandidateFacts, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	pages := []workspaceSearchCandidateFacts{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[created_by]], [[last_edited_by]], [[teamspace_id]], [[created]], [[updated]],
			[[id]] AS [[anchor]], '' AS [[data_source_id]], '' AS [[archived]]
		FROM {{leafen_pages}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&pages); err != nil {
		return nil, err
	}
	items := []workspaceSearchCandidateFacts{}
	if err := app.DB().NewQuery(`
		SELECT [[item]].[[row_id]] AS [[id]], COALESCE([[row_record]].[[created_by]], '') AS [[created_by]],
			COALESCE([[row_record]].[[last_edited_by]], '') AS [[last_edited_by]],
			COALESCE([[page]].[[teamspace_id]], '') AS [[teamspace_id]],
			CAST([[item]].[[created]] AS TEXT) AS [[created]], CAST([[item]].[[updated]] AS TEXT) AS [[updated]],
			[[item]].[[row_id]] AS [[anchor]], [[item]].[[data_source_id]],
			COALESCE([[row_record]].[[archived_at]], '') AS [[archived]]
		FROM {{leafen_database_row_pages}} [[item]]
		LEFT JOIN {{leafen_pages}} [[row_record]] ON [[row_record]].[[id]] = [[item]].[[row_id]]
		LEFT JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[item]].[[container_page_id]]
		WHERE [[item]].[[row_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&items); err != nil {
		return nil, err
	}
	// A database held in a page's body is placed by that page; a template
	// is a page record, placed through its database (workspace_search_databases.go).
	databases, err := loadSearchInlineDatabaseFacts(app, string(encoded))
	if err != nil {
		return nil, err
	}
	items = append(items, databases...)
	facts := make(map[string]workspaceSearchCandidateFacts, len(pages)+len(items))
	for _, fact := range pages {
		fact.created, fact.updated = workspaceSearchPageTime(fact.Created), workspaceSearchPageTime(fact.Updated)
		facts[fact.ID] = fact
	}
	for _, fact := range items {
		fact.created, _ = strconv.ParseInt(fact.Created, 10, 64)
		fact.updated, _ = strconv.ParseInt(fact.Updated, 10, 64)
		facts[fact.ID] = fact
	}
	return facts, nil
}

// workspaceSearchSortScores gives each match its time sort's score, the
// times read in one pass: a time sort ranks every match by them, the rest
// of the filter applied as the matches are checked. Keeping to titles goes
// by the title scores the matches come with.
func workspaceSearchSortScores(app core.App, filter workspaceSearchFilter, candidates []workspaceSearchCandidate) ([]workspaceSearchCandidate, error) {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.PageID)
	}
	facts, err := loadWorkspaceSearchCandidateFacts(app, ids)
	if err != nil {
		return nil, err
	}
	scored := make([]workspaceSearchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		fact, found := facts[candidate.PageID]
		if !found || filter.TitleOnly && !workspaceSearchTitleOnlyTier(candidate.Score) {
			continue
		}
		switch filter.Sort {
		case workspaceSearchSortEditedNewest:
			candidate.Score = -fact.updated
		case workspaceSearchSortEditedOldest:
			candidate.Score = fact.updated
		case workspaceSearchSortCreatedNewest:
			candidate.Score = -fact.created
		case workspaceSearchSortCreatedOldest:
			candidate.Score = fact.created
		}
		scored = append(scored, candidate)
	}
	return scored, nil
}

// workspaceSearchPageTime reads a page's stored time in Unix milliseconds.
func workspaceSearchPageTime(value string) int64 {
	parsed, err := types.ParseDateTime(value)
	if err != nil || parsed.IsZero() {
		return 0
	}
	return parsed.Time().UnixMilli()
}

type workspaceSearchPlace struct {
	Ancestors  []string
	Teamspaces []string
}

// loadWorkspaceSearchPlaces reads the pages above each anchor page and the
// teamspaces they are in, through database pages to the pages above them,
// to the top however deep; each page is reached once, so a chain that comes
// back on itself ends.
func loadWorkspaceSearchPlaces(app core.App, anchors []string) (map[string]workspaceSearchPlace, error) {
	encoded, err := json.Marshal(anchors)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		Anchor      string `db:"anchor"`
		PageID      string `db:"page_id"`
		TeamspaceID string `db:"teamspace_id"`
	}{}
	if err := app.DB().NewQuery(`
		WITH RECURSIVE [[chain]]([[anchor]], [[page_id]]) AS (
			SELECT [[value]], [[value]] FROM json_each({:anchors})
			UNION
			SELECT [[chain]].[[anchor]], ` + pageAncestryParentSQL("[[page]]") + `
			FROM [[chain]] JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[chain]].[[page_id]]
			WHERE ` + pageAncestryParentSQL("[[page]]") + ` != ''
		)
		SELECT [[chain]].[[anchor]], [[chain]].[[page_id]], COALESCE([[page]].[[teamspace_id]], '') AS [[teamspace_id]]
		FROM [[chain]] LEFT JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[chain]].[[page_id]]
	`).Bind(dbx.Params{"anchors": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	places := make(map[string]workspaceSearchPlace, len(anchors))
	for _, row := range rows {
		place := places[row.Anchor]
		place.Ancestors = append(place.Ancestors, row.PageID)
		if row.TeamspaceID != "" && !slices.Contains(place.Teamspaces, row.TeamspaceID) {
			place.Teamspaces = append(place.Teamspaces, row.TeamspaceID)
		}
		places[row.Anchor] = place
	}
	return places, nil
}

// workspaceSearchTextsRefusal is why a search's page, words or line aren't
// ones it reads, or "": a page named, q within its length (and given, when
// required), and the line beside it within limit.
func workspaceSearchTextsRefusal(page bool, query string, required bool, lineName, line string, limit int) string {
	switch count := utf8.RuneCountInString(query); {
	case !page:
		return "pageId is empty"
	case required && query == "":
		return "q is empty"
	case count > maxWorkspaceSearchQueryRunes:
		return fmt.Sprintf("q is at most %d characters, not %d", maxWorkspaceSearchQueryRunes, count)
	}
	if count := utf8.RuneCountInString(line); count > limit {
		return fmt.Sprintf("%s is at most %d characters, not %d", lineName, limit, count)
	}
	return ""
}
