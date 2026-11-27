package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"

	"github.com/pocketbase/pocketbase/core"
)

// POST /v1/data_sources/{id}/query returns the row pages the official filter
// and sorts select, a page at a time. Each request runs the native query
// afresh, and a cursor is the next row page's public ID. Rows that are not
// archived answer by default and archived rows with is_archived; the archive
// partition applies before filter and sorts. Leafen data sources hold only
// pages, so a query for data source results answers with an empty list.

type notionDataSourceQueryRequest struct {
	Filter      json.RawMessage    `json:"filter"`
	Sorts       json.RawMessage    `json:"sorts"`
	StartCursor *string            `json:"start_cursor"`
	PageSize    *notionWholeNumber `json:"page_size"`
	IsArchived  *bool              `json:"is_archived"`
	ResultType  *string            `json:"result_type"`
}

func serveNotionDataSourceQuery(event *core.RequestEvent, publicID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return err
	}
	identity, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDataSource, publicID)
	if errors.Is(err, errInvalidNotionObjectIdentity) ||
		err == nil && identity.Workspace != workspace.Identity.WorkspaceID {
		return notionPageNotFound(event)
	}
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	authorization, err := authorizeNotionDataSourceParentCurrent(
		event.App, workspace.Identity, identity.InternalID, permissionWorkspaceRead,
	)
	if err == nil {
		// One under a page in the trash isn't found for a query either
		// (notion_trash_reads.go).
		var place notionTrashPlace
		if place, err = notionPageTrashPlace(event.App, authorization.Container.PageID); err == nil && place.inTrash() {
			err = errDatabaseRowPageNotFound
		}
	}
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	input := notionDataSourceQueryRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil && !errors.Is(err, io.EOF) {
		return notionBindRefused(event, err)
	}
	pageSize := 100
	if input.PageSize != nil {
		// Here 0 is refused, though a list's own size elsewhere (seen 2026-11-04).
		if *input.PageSize < 1 {
			return notionPageValidationError(event, "page_size must be 1 or more.")
		}
		pageSize = min(int(*input.PageSize), 100)
	}
	if input.ResultType != nil && *input.ResultType != "page" && *input.ResultType != "data_source" {
		return notionPageValidationError(event, "result_type must be page or data_source.")
	}
	// A property the connection sees in no row is one the source doesn't
	// have, a property Notion's finds none of (seen 2026-10-12).
	scope, err := notionViewReaderScope(event.App, notionViewLocation{
		WorkspaceID: workspace.Identity.WorkspaceID, DataSourceID: identity.InternalID,
		SourcePageID: authorization.Container.PageID,
	}, workspace.Identity.PrincipalID)
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	scope = scope.withTimestampSorts()
	filterGroup, err := scope.nativeViewFilterGroup(input.Filter)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	sorts, err := scope.nativeViewSorts(input.Sorts)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	rows, incomplete := []string{}, false
	// A row is archived itself or with the archived page holding its
	// database, which archives every row.
	wantArchived := input.IsArchived != nil && *input.IsArchived
	_, containerArchived, err := loadPageArchiveEffect(event.App, authorization.Container.PageID)
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	archiveScope := databaseArchiveScopeActive
	if wantArchived && containerArchived {
		archiveScope = databaseArchiveScopeAll
	} else if wantArchived {
		archiveScope = databaseArchiveScopeArchived
	}
	if (wantArchived || !containerArchived) && (input.ResultType == nil || *input.ResultType == "page") {
		rows, incomplete, err = collectNotionDataSourceRows(
			event.App, workspace.Identity, authorization.Container.PageID, identity.InternalID,
			filterGroup, sorts, nil, archiveScope,
		)
		if err != nil {
			return notionDataSourceMutationError(event, err)
		}
	}
	offset := 0
	if input.StartCursor != nil {
		cursor, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityRowPage, *input.StartCursor)
		offset = slices.Index(rows, cursor.InternalID)
		if err != nil || cursor.Workspace != workspace.Identity.WorkspaceID || offset < 0 {
			return notionStartCursorRefused(event, *input.StartCursor)
		}
	}
	end := min(offset+pageSize, len(rows))
	wanted := notionDataSourceQueryProperties(event.Request.URL.Query())
	results := make([]any, 0, end-offset)
	recordAPIReads(event.App, workspace.Identity, sortedReadIDs("row", contentReadList, rows[offset:end]))
	pages, err := loadNotionDatabaseRowPageResponses(event, workspace.Identity, rows[offset:end], permissionWorkspaceRead)
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	for _, page := range pages {
		if wanted != nil {
			page["properties"] = notionDataSourceQuerySelectedProperties(page["properties"], wanted)
		}
		results = append(results, page)
	}
	var next any
	if end < len(rows) {
		if next, err = ensureNotionObjectIdentity(
			event.App, notionObjectIdentityRowPage, workspace.Identity.WorkspaceID, rows[end],
		); err != nil {
			return notionDataSourceReadError(event, err)
		}
	}
	// Notion's answer names a request_status only when it stopped short
	// (seen 2026-10-04: a complete query answer has none), and its request.
	response := map[string]any{
		"object": "list", "results": results, "next_cursor": next, "has_more": next != nil,
		"type": "page_or_data_source", "page_or_data_source": map[string]any{},
	}
	if incomplete {
		response["request_status"] = map[string]any{"type": "incomplete", "incomplete_reason": "query_result_limit_reached"}
	}
	return writeNotionJSON(event, http.StatusOK, response)
}

// notionDataSourceQueryProperties reads filter_properties, which names
// properties by name or by public ID, encoded or not (seen 2026-11-09: Notion
// takes "Num", "y%7C%3B%5C" and "y|;\"); nil keeps every property.
func notionDataSourceQueryProperties(query url.Values) map[string]struct{} {
	names := append(query["filter_properties[]"], query["filter_properties"]...)
	if len(names) == 0 {
		return nil
	}
	wanted := make(map[string]struct{}, len(names)*2)
	for _, name := range names {
		wanted[name] = struct{}{}
		wanted[notionPropertyIDKey(name)] = struct{}{}
	}
	return wanted
}

func notionDataSourceQuerySelectedProperties(properties any, wanted map[string]struct{}) notionPropertyObjects {
	selected := notionPropertyObjects{}
	all, ok := properties.(notionPropertyObjects)
	if !ok {
		all, _ = properties.(map[string]any)
	}
	for name, value := range all {
		id := ""
		if object, ok := value.(map[string]any); ok {
			id, _ = object["id"].(string)
		}
		_, byName := wanted[name]
		_, byID := wanted[notionPropertyIDKey(id)]
		if byName || byID {
			selected[name] = value
		}
	}
	return selected
}
