package app

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion MCP search narrows and sorts as the official tool does: by title,
// by who created or last edited a page, by when it was created or last
// edited, by teamspace, to a page and its sub-pages, to one data source's
// pages, and to live, archived or verified pages. Every option is open to
// every workspace.

const (
	notionMCPSearchDefaultResults      = 10
	notionMCPSearchDefaultHighlight    = 200
	maxNotionMCPSearchHighlight        = 500
	maxNotionMCPSearchFilterIdentities = 100
)

type notionMCPSearchFilters struct {
	TitleOnly           bool                      `json:"title_only,omitzero" jsonschema:"Match the query only against page and database titles"`
	CreatedByUserIDs    []string                  `json:"created_by_user_ids,omitempty" jsonschema:"Only pages created by these users (user IDs from notion-get-users, or self)"`
	EditedByUserIDs     []string                  `json:"edited_by_user_ids,omitempty" jsonschema:"Only pages last edited by these users (user IDs from notion-get-users, or self)"`
	CreatedDateRange    *notionMCPSearchDateRange `json:"created_date_range,omitempty" jsonschema:"Only pages created within this date range"`
	LastEditedDateRange *notionMCPSearchDateRange `json:"last_edited_date_range,omitempty" jsonschema:"Only pages last edited within this date range"`
	TeamspaceIDs        []string                  `json:"teamspace_ids,omitempty" jsonschema:"Only pages inside one of these teams (teamspaces) from notion-get-teams"`
	ContentStatus       string                    `json:"content_status,omitempty" jsonschema:"all_without_archived (the default), all_with_archived, verified_only or archived_only"`
}

type notionMCPSearchDateRange struct {
	StartDate string `json:"start_date,omitempty" jsonschema:"First day of the range, as an ISO 8601 date, a day in the person's time zone"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"Last day of the range, as an ISO 8601 date, a day in the person's time zone"`
}

// notionMCPSearchFilter reads the official search options into the native
// search filter of the chosen workspace.
func notionMCPSearchFilter(
	app core.App, input notionMCPSearchInput, principalID, workspaceID string, clock *time.Location,
) (workspaceSearchFilter, error) {
	filter := workspaceSearchFilter{Content: workspaceSearchContentActive}
	switch strings.TrimSpace(input.Sort) {
	case "", "relevance":
		filter.Sort = workspaceSearchSortBest
	case "last_edited":
		filter.Sort = workspaceSearchSortEditedNewest
	case "created":
		filter.Sort = workspaceSearchSortCreatedNewest
	default:
		return workspaceSearchFilter{}, mcpInvalid("sort is relevance, last_edited or created")
	}
	if raw := strings.TrimSpace(input.PageURL); raw != "" {
		in, ok, err := mcpSearchPageKeptTo(app, raw)
		if err != nil {
			return workspaceSearchFilter{}, failedBecause(errMCPToolFailed, err)
		}
		if !ok {
			return workspaceSearchFilter{}, mcpInvalid("page_url is a page's or database's URL or ID, as notion-search or notion-fetch gives it; a data source is kept to with data_source_url")
		}
		filter.In = in
	}
	if raw := strings.TrimSpace(input.DataSourceURL); raw != "" {
		dataSourceID, ok := parseMCPDataSourceIdentifier(raw)
		if !ok {
			return workspaceSearchFilter{}, mcpInvalid("data_source_url is a data source's collection:// URL, as notion-fetch gives it")
		}
		filter.DataSources = []string{dataSourceID}
	}
	options := input.Filters
	if options == nil {
		options = &notionMCPSearchFilters{}
	}
	filter.TitleOnly = options.TitleOnly
	var err error
	if filter.CreatedBy, err = notionMCPSearchUsers(options.CreatedByUserIDs, principalID); err != nil {
		return workspaceSearchFilter{}, err
	}
	if filter.EditedBy, err = notionMCPSearchUsers(options.EditedByUserIDs, principalID); err != nil {
		return workspaceSearchFilter{}, err
	}
	if filter.CreatedAfter, filter.CreatedBefore, err = notionMCPSearchDays(options.CreatedDateRange, clock); err != nil {
		return workspaceSearchFilter{}, err
	}
	if filter.EditedAfter, filter.EditedBefore, err = notionMCPSearchDays(options.LastEditedDateRange, clock); err != nil {
		return workspaceSearchFilter{}, err
	}
	if len(options.TeamspaceIDs) > maxNotionMCPSearchFilterIdentities {
		return workspaceSearchFilter{}, mcpInvalid("filters.teamspace_ids names at most %d teams", maxNotionMCPSearchFilterIdentities)
	}
	// Search keeps to the teamspaces named in teamspace_id and
	// teamspace_ids together; a workspace's own ID there is all of it.
	teamspaces := slices.Clone(options.TeamspaceIDs)
	if id := strings.TrimSpace(input.TeamspaceID); id != "" {
		teamspaces = append(teamspaces, id)
	}
	whole := false
	for _, raw := range teamspaces {
		id := strings.TrimSpace(raw)
		if id == "" || len(id) > maxWorkspacePermissionIdentifierLength {
			return workspaceSearchFilter{}, mcpInvalid("a team ID is one notion-get-teams gives")
		}
		if id == workspaceID {
			whole = true
		} else if !slices.Contains(filter.Teamspaces, id) {
			filter.Teamspaces = append(filter.Teamspaces, id)
		}
	}
	if whole {
		filter.Teamspaces = nil
	}
	slices.Sort(filter.Teamspaces)
	switch strings.TrimSpace(options.ContentStatus) {
	case "", "all_without_archived":
	case "all_with_archived":
		filter.Content = workspaceSearchContentAll
	case "archived_only":
		filter.Content = workspaceSearchContentArchived
	case "verified_only":
		filter.Content = workspaceSearchContentVerified
	default:
		return workspaceSearchFilter{}, mcpInvalid("filters.content_status is all_without_archived, all_with_archived, verified_only or archived_only")
	}
	return filter, nil
}

func notionMCPSearchUsers(raw []string, principalID string) ([]string, error) {
	if len(raw) > maxNotionMCPSearchFilterIdentities {
		return nil, mcpInvalid("a user filter names at most %d users", maxNotionMCPSearchFilterIdentities)
	}
	users := []string{}
	for _, value := range raw {
		value = strings.TrimSpace(value)
		userID := principalID
		if value != "self" {
			internalID, err := resolveNotionCompatibleUserID(value)
			if err != nil {
				return nil, mcpInvalid("a user filter takes user IDs from notion-get-users, or self")
			}
			userID = internalID
		}
		if !slices.Contains(users, userID) {
			users = append(users, userID)
		}
	}
	if len(users) == 0 {
		return nil, nil
	}
	slices.Sort(users)
	return users, nil
}

// notionMCPSearchDays reads a date range as the instants from the start of
// its first day to the end of its last day, the person's days on the clock
// (person_clock.go), where they were UTC's.
func notionMCPSearchDays(days *notionMCPSearchDateRange, clock *time.Location) (int64, int64, error) {
	if days == nil {
		return 0, 0, nil
	}
	var after, before int64
	if raw := strings.TrimSpace(days.StartDate); raw != "" {
		start, err := time.ParseInLocation(time.DateOnly, raw, clock)
		if err != nil {
			return 0, 0, mcpInvalid("a date range's start_date is a day as 2026-10-23")
		}
		after = start.UnixMilli()
	}
	if raw := strings.TrimSpace(days.EndDate); raw != "" {
		end, err := time.ParseInLocation(time.DateOnly, raw, clock)
		if err != nil {
			return 0, 0, mcpInvalid("a date range's end_date is a day as 2026-10-23")
		}
		before = end.AddDate(0, 0, 1).UnixMilli()
	}
	if after > 0 && before > 0 && after >= before || after < 0 || before < 0 {
		return 0, 0, mcpInvalid("a date range's start_date comes before its end_date")
	}
	return after, before, nil
}

// parseMCPSearchPageReference reads a page's address (…/p/ID, …/pages/ID,
// one of Notion's) or its ID, as written (mcpReferenceID).
func parseMCPSearchPageReference(raw string) (string, bool) {
	raw = mcpReferenceID(raw)
	return raw, raw != "" && len(raw) <= maxWorkspacePermissionIdentifierLength && !strings.ContainsAny(raw, "/?#")
}

// mcpSearchPageKeptTo is what a search keeps to (workspaceSearchFilter.In):
// the page page_url names, read as every tool reads a page (mcpLocalID) — by
// its ID, with its dashes or without them, or by an address of it here or in
// Notion (2026-11-22: its address here, /p/ and 32 digits, found nothing,
// while the ID alone found its pages) — and the pages under it. A database
// named so keeps to itself, its pages and theirs, as Notion's (seen
// 2026-11-22 twice, by its ID and its address: an inline database finds
// itself, its rows and a row's sub-page, not the page holding it; a full-page
// one itself and its rows); a data source is no page, as Notion's refuses it
// ("Invalid page URL or ID. Must be an existing page.", by its ID and its
// collection:// URL alike).
func mcpSearchPageKeptTo(app core.App, raw string) ([]string, bool, error) {
	if _, ok := parseMCPSearchPageReference(raw); !ok {
		return nil, false, nil
	}
	id := mcpLocalID(app, raw)
	if strings.HasPrefix(id, mcpDataSourceURLPrefix) {
		return nil, false, nil
	}
	if !mcpDatabaseContainerExistsToRead(app, id) {
		return []string{id}, true, nil
	}
	location, err := loadMCPDatabaseContainerLocationWith(app, id, true)
	if err != nil {
		return nil, false, err
	}
	// A full-page database is its page, its pages placed under it.
	if mcpFullPageDatabaseContainer(app, location.PageID) == id {
		return []string{location.PageID}, true, nil
	}
	// An inline one is placed by the page holding it, as its pages are:
	// they are kept to by name.
	in := []string{id}
	rows := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[row]].[[row_id]] FROM {{leafen_database_row_pages}} AS [[row]]
		JOIN {{leafen_database_container_sources}} AS [[membership]] ON [[membership]].[[data_source_id]] = [[row]].[[data_source_id]]
		WHERE [[membership]].[[container_id]] = {:id}
		ORDER BY [[row]].[[row_id]]
	`).Bind(dbx.Params{"id": id}).Column(&rows); err != nil {
		return nil, false, err
	}
	return append(in, rows...), true, nil
}

// notionMCPSearchHighlight shortens a highlight to the length asked for as
// Notion's does (seen 2026-11-09): one character short of it, back to the
// last space, then "..."; zero leaves it out.
func notionMCPSearchHighlight(highlight string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(highlight) <= limit {
		return highlight
	}
	cut := string([]rune(highlight)[:limit-1])
	if space := strings.LastIndex(cut, " "); space > 0 {
		cut = cut[:space]
	}
	return strings.TrimRight(cut, " ") + "..."
}
