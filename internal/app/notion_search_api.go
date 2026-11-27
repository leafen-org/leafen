package app

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The official search lists what the integration can open whose title
// matches the query: ordinary pages, database pages and data sources. A title
// is judged as the app's search judges one (workspace_search_keywords.go):
// every quoted phrase whole and some of the other words, each the start of a
// word, those holding all of them first, as Notion's search API answers
// (seen 2026-11-11: "lowix" finds no "Brendlowix", "brend" does; `zorvek
// vandrel` finds a title holding either; `"zorvek kravnid" team` neither of
// a title with the phrase and one with "team"). It matches titles only. A
// full-page database's page is found through its data source,
// as Notion shows the database rather than a page. Linked databases add no
// data source of their own, and deleted database pages leave no trash. A
// data source its database keeps in the trash where it stands — sent there
// through the API, or from 데이터 소스 관리 with its views — is in the trash
// as every reader takes it (databaseSourceTrashedInPlaceSQL), as its object
// says (in_trash): found by a search for the trash alone, as a page in the
// trash is, its pages under it in neither.

const (
	notionAPISearchPath = notionAPIRoot + "/search"
	// notionSearchCandidateLimit bounds the titles one search reads for each
	// kind; a larger workspace answers with request_status incomplete.
	notionSearchCandidateLimit = 20000
)

const (
	notionSearchPage       = "page"
	notionSearchRowPage    = "row"
	notionSearchDataSource = "data_source"
)

type notionSearchRequest struct {
	Query  *string `json:"query,omitempty"`
	Filter *struct {
		Property *string `json:"property,omitempty"`
		Value    *string `json:"value,omitempty"`
		InTrash  *bool   `json:"in_trash,omitempty"`
	} `json:"filter,omitempty"`
	Sort *struct {
		Property  *string `json:"property,omitempty"`
		Timestamp *string `json:"timestamp,omitempty"`
		Direction *string `json:"direction,omitempty"`
	} `json:"sort,omitempty"`
	StartCursor *string            `json:"start_cursor,omitempty"`
	PageSize    *notionWholeNumber `json:"page_size,omitempty"`
}

type notionSearchCandidate struct {
	Kind         string
	ID           string
	DataSourceID string
	Title        string
	Edited       int64
	score        int
	// trashHolder is the page whose trash state the result follows
	// (notion_search_trash.go).
	trashHolder string
}

// notionSearchOptions is a validated search request.
type notionSearchOptions struct {
	query     string
	keywords  workspaceSearchKeywords
	object    string
	inTrash   bool
	byEdited  bool
	ascending bool
	pageSize  int
}

func serveNotionSearch(event *core.RequestEvent) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return err
	}
	input := notionSearchRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil && !errors.Is(err, io.EOF) {
		return notionBindRefused(event, err)
	}
	options, message := notionSearchRequestOptions(input)
	if message != "" {
		return notionPageValidationError(event, message)
	}
	identity := workspace.Identity
	visible, incomplete, refused, err := notionSearchVisible(event, identity, options, input.StartCursor)
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	if refused {
		return notionStartCursorRefused(event, *input.StartCursor)
	}
	var next any
	if len(visible) > options.pageSize {
		if next, err = notionSearchPublicID(event.App, identity.WorkspaceID, visible[options.pageSize]); err != nil {
			return notionDataSourceReadError(event, err)
		}
		visible = visible[:options.pageSize]
	}
	results := make([]any, 0, len(visible))
	listed := []contentRead{}
	for _, candidate := range visible {
		object, err := notionSearchObject(event, identity, candidate)
		if err != nil {
			return notionDataSourceReadError(event, err)
		}
		results = append(results, object)
		// A page or an item found comes with its properties, which is reading them.
		switch candidate.Kind {
		case notionSearchPage:
			listed = append(listed, contentRead{Kind: "page", ID: candidate.ID, How: contentReadList})
		case notionSearchRowPage:
			listed = append(listed, contentRead{Kind: "row", ID: candidate.ID, How: contentReadList})
		}
	}
	recordAPIReads(event.App, identity, listed)
	// Notion's answer names a request_status only when it stopped short
	// (seen 2026-10-04: a complete search answer has none).
	response := map[string]any{
		"object": "list", "results": results, "next_cursor": next, "has_more": next != nil,
		"type": "page_or_data_source", "page_or_data_source": map[string]any{},
	}
	if incomplete {
		response["request_status"] = map[string]any{"type": "incomplete", "incomplete_reason": "query_result_limit_reached"}
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return writeNotionJSON(event, http.StatusOK, response)
}

func notionSearchRequestOptions(input notionSearchRequest) (notionSearchOptions, string) {
	options := notionSearchOptions{pageSize: 100}
	if input.Query != nil {
		query := strings.TrimSpace(*input.Query)
		if utf8.RuneCountInString(query) > maxWorkspaceSearchQueryRunes {
			return options, "The search query is too long."
		}
		options.query = foldSearchText(query)
		options.keywords = readWorkspaceSearchKeywords(options.query)
	}
	if filter := input.Filter; filter != nil {
		if filter.Property == nil && filter.Value == nil && filter.InTrash == nil {
			return options, "The search filter is invalid."
		}
		if filter.Property != nil || filter.Value != nil {
			if filter.Property == nil || *filter.Property != "object" || filter.Value == nil ||
				*filter.Value != "page" && *filter.Value != "data_source" {
				return options, "The search filter must name the object page or data_source."
			}
			options.object = *filter.Value
		}
		options.inTrash = filter.InTrash != nil && *filter.InTrash
	}
	if sort := input.Sort; sort != nil {
		switch {
		case sort.Property != nil && *sort.Property == "relevance" && sort.Timestamp == nil && sort.Direction == nil:
		case sort.Property == nil && sort.Timestamp != nil && *sort.Timestamp == "last_edited_time" &&
			sort.Direction != nil && (*sort.Direction == "ascending" || *sort.Direction == "descending"):
			options.byEdited, options.ascending = true, *sort.Direction == "ascending"
		default:
			return options, "The search sort must be relevance or last_edited_time with a direction."
		}
	}
	if input.PageSize != nil {
		options.pageSize = notionListPageSize(int(*input.PageSize), options.pageSize)
	}
	return options, ""
}

// loadNotionSearchCandidates reads the matching titles of each wanted kind in
// search order. It reports whether a kind had more titles than one search
// reads. With a window the page titles are those from where the window
// starts (notion_search_window.go): the list then ends at the last page
// title read, and found is false when the window's cursor names no result.
func loadNotionSearchCandidates(
	app core.App, workspaceID string, options notionSearchOptions, window *notionSearchWindow,
) (candidates []notionSearchCandidate, incomplete, cut, found bool, err error) {
	params := dbx.Params{"workspaceId": workspaceID, "inTrash": options.inTrash, "limit": notionSearchCandidateLimit + 1}
	type stored struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
		Title        string `db:"title"`
		Updated      string `db:"updated"`
		EditedMillis int64  `db:"edited"`
		PageID       string `db:"page_id"`
	}
	queries := []struct {
		kind  string
		query string
	}{}
	if options.object != "data_source" {
		if window == nil {
			queries = append(queries, struct{ kind, query string }{notionSearchPage, `
			SELECT [[page]].[[id]], '' AS [[data_source_id]], [[page]].[[title]], [[page]].[[updated]], 0 AS [[edited]]
			FROM {{leafen_pages}} AS [[page]]
			WHERE ` + notionSearchPageTitlesSQL + `
			ORDER BY [[page]].[[updated]] DESC, [[page]].[[id]] LIMIT {:limit}`})
		}
		{
			queries = append(queries, struct{ kind, query string }{notionSearchRowPage, `
				SELECT [[row]].[[id]], [[row]].[[data_source_id]],
					COALESCE((SELECT json_extract([[value]].[[value]], '$') FROM {{leafen_database_row_values}} AS [[value]]
						WHERE [[value]].[[row_id]] = [[row]].[[id]] AND [[value]].[[type]] = 'title' LIMIT 1), '') AS [[title]],
					'' AS [[updated]], COALESCE([[row_page]].[[updated]], [[row]].[[updated]]) AS [[edited]]
				FROM {{leafen_database_rows}} AS [[row]]
				LEFT JOIN {{leafen_database_row_pages}} AS [[row_page]] ON [[row_page]].[[row_id]] = [[row]].[[id]]
				WHERE [[row]].[[workspace_id]] = {:workspaceId}
					AND EXISTS (SELECT 1 FROM {{leafen_database_row_trash}} AS [[row_trash]]
						WHERE [[row_trash]].[[row_id]] = [[row]].[[id]] AND [[row_trash]].[[deleting]] = 0) = {:inTrash}
				ORDER BY [[edited]] DESC, [[row]].[[id]] LIMIT {:limit}`})
		}
	}
	if options.object != "page" {
		queries = append(queries, struct{ kind, query string }{notionSearchDataSource, `
			SELECT [[source]].[[id]], [[source]].[[id]] AS [[data_source_id]], [[source]].[[title]],
				'' AS [[updated]], [[source]].[[updated]] AS [[edited]], [[container]].[[page_id]]
			FROM {{leafen_data_sources}} AS [[source]]
			JOIN {{leafen_database_container_sources}} AS [[membership]]
				ON [[membership]].[[data_source_id]] = [[source]].[[id]] AND [[membership]].[[source_container_id]] = ''
			JOIN {{leafen_database_containers}} AS [[container]]
				ON [[container]].[[id]] = [[membership]].[[container_id]] AND [[container]].[[kind]] = 'source'
			WHERE [[source]].[[workspace_id]] = {:workspaceId} AND NOT ` + databaseViewBlockSQL("[[container]]") + `
				AND (EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
					WHERE [[trash]].[[container_id]] = [[container]].[[id]])
					OR ` + databaseSourceTrashedInPlaceSQL("[[source]].[[id]]") + `) = {:inTrash}
			ORDER BY [[source]].[[updated]] DESC, [[source]].[[id]] LIMIT {:limit}`})
	}
	candidates = []notionSearchCandidate{}
	query := workspaceSearchPlainText(options.query)
	for _, kind := range queries {
		rows := []stored{}
		if err := app.DB().NewQuery(kind.query).Bind(params).All(&rows); err != nil {
			return nil, false, false, false, err
		}
		if len(rows) > notionSearchCandidateLimit {
			rows, incomplete = rows[:notionSearchCandidateLimit], true
		}
		for _, row := range rows {
			candidate := notionSearchCandidate{
				Kind: kind.kind, ID: row.ID, DataSourceID: row.DataSourceID, Title: row.Title, Edited: row.EditedMillis,
				trashHolder: notionSearchTrashHolder(kind.kind, row.ID, row.PageID),
			}
			// Without a query every title is found, all alike.
			if options.query != "" {
				all, some := options.keywords.match(row.Title)
				if !some {
					continue
				}
				candidate.score = searchTitleScore(query, foldSearchText(row.Title), all)
			}
			if kind.kind == notionSearchPage {
				candidate.Edited = notionSearchPageMillis(row.Updated)
			}
			candidates = append(candidates, candidate)
		}
	}
	found = true
	if window != nil && options.object != "data_source" {
		pages := []notionSearchCandidate{}
		if pages, cut, found, err = loadNotionSearchPageWindow(app, workspaceID, options, window, candidates); err != nil || !found {
			return nil, false, false, found, err
		}
		candidates = append(candidates, pages...)
		if cut && window.pages >= notionSearchCandidateLimit {
			cut, incomplete = false, true
		}
	}
	slices.SortStableFunc(candidates, func(left, right notionSearchCandidate) int {
		if !options.byEdited && left.score != right.score {
			return left.score - right.score
		}
		if left.Edited != right.Edited {
			if (left.Edited < right.Edited) == options.ascending {
				return -1
			}
			return 1
		}
		if left.Kind != right.Kind {
			return strings.Compare(left.Kind, right.Kind)
		}
		return strings.Compare(left.ID, right.ID)
	})
	if cut {
		// What comes after the last page title read may come after page
		// titles not read yet.
		for last := len(candidates) - 1; last >= 0; last-- {
			if candidates[last].Kind == notionSearchPage {
				candidates = candidates[:last+1]
				break
			}
		}
	}
	return candidates, incomplete, cut, found, nil
}

// notionSearchTrashHolder is the page whose trash state a result follows:
// a page or database page its own, a data source its database's page.
func notionSearchTrashHolder(kind, id, pageID string) string {
	if kind == notionSearchDataSource {
		return pageID
	}
	return id
}

func notionSearchPageMillis(value string) int64 {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.000Z", "2006-01-02 15:04:05Z"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UnixMilli()
		}
	}
	return 0
}

// notionSearchCursorResult reads the kind and id of the result a cursor
// names, or no kind.
func notionSearchCursorResult(app core.App, workspaceID, cursor string) (kind, internalID string) {
	if pageID, err := resolveNotionPageRecordID(app, strings.TrimSpace(cursor)); err == nil && notionPublicIDNamesPage(app, strings.TrimSpace(cursor)) {
		kind, internalID = notionSearchPage, pageID
	} else {
		for _, object := range []struct{ identity, kind string }{
			{notionObjectIdentityRowPage, notionSearchRowPage}, {notionObjectIdentityDataSource, notionSearchDataSource},
		} {
			resolved, err := resolveNotionObjectIdentity(app, object.identity, cursor)
			if err == nil && resolved.Workspace == workspaceID {
				kind, internalID = object.kind, resolved.InternalID
				break
			}
		}
	}
	return kind, internalID
}

// notionSearchCandidateVisible reports whether the integration may open a
// result. Database pages follow their data source, checked once per source.
func notionSearchCandidateVisible(
	app core.App, identity notionOAuthBearerIdentity, candidate notionSearchCandidate, sources map[string]bool, rows map[string]*databaseRowAccess,
) (bool, error) {
	switch candidate.Kind {
	case notionSearchPage:
		// A page an operation holds a while is still found (page_access_hold.go).
		page, err := pageAccessDespiteHold(authorizePage(app, identity.PrincipalID, candidate.ID, permissionWorkspaceRead))
		if isPermissionDenied(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return page.Workspace.WorkspaceID == identity.WorkspaceID && notionOAuthIdentityAllowsPage(app, identity, page), nil
	case notionSearchRowPage:
		// A row put in the connection's pages on its own is found as it opens.
		if identity.ResourceIDs[candidate.ID] == "page" {
			_, err := authorizeNotionDatabaseRowPageCurrentState(app, identity, candidate.ID, permissionWorkspaceRead, notionRowOutOfTrash)
			if err == nil {
				return true, nil
			}
			if !errors.Is(err, errDatabaseRowPageNotFound) && !isPermissionDenied(err) {
				return false, err
			}
		}
		allowed, known := sources[candidate.DataSourceID]
		if !known {
			_, err := authorizeNotionDataSourceParentCurrent(app, identity, candidate.DataSourceID, permissionWorkspaceRead)
			if err != nil && !errors.Is(err, errDatabaseRowPageNotFound) && !isPermissionDenied(err) {
				return false, err
			}
			allowed = err == nil || pageAccessHeld(err) != ""
			sources[candidate.DataSourceID] = allowed
		}
		if !allowed {
			return false, nil
		}
		// The table doesn't speak for an item an unlinked page hides.
		access, read := rows[candidate.DataSourceID]
		if !read {
			loaded, err := loadDatabaseRowAccess(app, identity.PrincipalID, []string{candidate.DataSourceID})
			if err != nil {
				return false, err
			}
			access = loaded
			rows[candidate.DataSourceID] = access
		}
		return !access.hides(candidate.ID), nil
	}
	_, _, err := authorizeNotionDataSourceAnyState(app, identity, candidate.ID, permissionWorkspaceRead)
	if errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) {
		return false, nil
	}
	return err == nil, err
}

func notionSearchPublicID(app core.App, workspaceID string, candidate notionSearchCandidate) (string, error) {
	switch candidate.Kind {
	case notionSearchPage:
		return notionPagePublicID(app, candidate.ID)
	case notionSearchRowPage:
		return ensureNotionObjectIdentity(app, notionObjectIdentityRowPage, workspaceID, candidate.ID)
	}
	return ensureNotionObjectIdentity(app, notionObjectIdentityDataSource, workspaceID, candidate.ID)
}

func notionSearchObject(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, candidate notionSearchCandidate,
) (map[string]any, error) {
	var object map[string]any
	var err error
	switch candidate.Kind {
	case notionSearchPage:
		record, findErr := event.App.FindRecordById(pagesCollection, candidate.ID)
		if findErr != nil {
			return nil, findErr
		}
		object, err = notionPageObject(event.App, event.Request, identity, record)
	case notionSearchRowPage:
		object, err = loadNotionDatabaseRowPageResponse(event, identity, candidate.ID, permissionWorkspaceRead)
	default:
		object, err = loadNotionDataSourceResponse(event.App, event.Request, identity, candidate.ID, permissionWorkspaceRead)
	}
	if err != nil {
		return nil, err
	}
	return object, nil
}
