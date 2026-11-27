package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A view query runs the view's filter and sorts once and keeps the ordered
// row IDs for a short time so callers can page through a stable result.
const (
	notionViewQueryLifetime = 15 * time.Minute
	maxNotionViewQueryRows  = 10000
)

type storedNotionViewQuery struct {
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
	PrincipalID string `db:"principal_id"`
	ViewID      string `db:"view_id"`
	RowIDsJSON  string `db:"row_ids_json"`
	Total       int    `db:"total"`
	Incomplete  bool   `db:"incomplete"`
	ExpiresAt   int64  `db:"expires_at"`
}

// serveNotionViewQueryRoute handles /v1/views/{id}/queries[/{query_id}].
func serveNotionViewQueryRoute(event *core.RequestEvent, remainder string) (bool, error) {
	viewID, rest, found := strings.Cut(remainder, "/queries")
	if !found || viewID == "" || strings.Contains(viewID, "/") {
		return false, nil
	}
	if rest == "" && event.Request.Method == http.MethodPost {
		return true, serveNotionViewQueryCreate(event, viewID)
	}
	queryID, ok := strings.CutPrefix(rest, "/")
	if !ok || queryID == "" || strings.Contains(queryID, "/") {
		return false, nil
	}
	if event.Request.Method != http.MethodGet && event.Request.Method != http.MethodDelete {
		return false, nil
	}
	// Each ID in the address is a UUID, the view's said first (seen 2026-11-12).
	for _, id := range [][2]string{{"view", viewID}, {"query", queryID}} {
		if refusal, refused := notionPathIDShapeRefused(id[0], id[1]); refused {
			return true, notionValidationRefused(event, refusal)
		}
	}
	switch event.Request.Method {
	case http.MethodGet:
		return true, serveNotionViewQueryResults(event, viewID, queryID)
	case http.MethodDelete:
		return true, serveNotionViewQueryDelete(event, viewID, queryID)
	}
	return false, nil
}

func notionViewQueryPageSize(raw string, fallback int) (int, bool) {
	if raw == "" {
		return fallback, true
	}
	size, err := strconv.Atoi(raw)
	return size, err == nil && size >= 1 && size <= 100
}

// notionViewQueryFilterGroup is the filter the view shows, as the editor
// derives it: the stored group or older flat filters, and the quick filters
// whose properties still exist.
func notionViewQueryFilterGroup(app core.App, location notionViewLocation) (*databaseQueryFilterGroup, []databaseQuerySort, *string, error) {
	known := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:dataSourceId} AND [[page_id]] = {:pageId}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
	`).Bind(dbx.Params{"dataSourceId": location.DataSourceID, "pageId": location.SourcePageID}).Column(&known); err != nil {
		return nil, nil, nil, err
	}
	// A linked view's "현재 페이지" is the page it sits in, not its source's.
	filterGroup, sorts, search, err := databaseViewQueryTerms(databaseViewConfigOnPage(location.Config, location.PageID), known)
	if err != nil {
		return nil, nil, nil, errNotionDatabaseRowPageProjectionInvalid
	}
	return filterGroup, sorts, search, nil
}

func notionViewQueryGroupNamesProperty(group databaseQueryFilterGroup, known []string) bool {
	for _, filter := range group.Filters {
		if slices.Contains(known, filter.PropertyID) {
			return true
		}
	}
	for _, child := range group.Groups {
		if notionViewQueryGroupNamesProperty(child, known) {
			return true
		}
	}
	return false
}

// runNotionViewQuery collects the ordered row IDs the view shows the
// connection.
func runNotionViewQuery(app core.App, identity notionOAuthBearerIdentity, location notionViewLocation) ([]string, bool, error) {
	filterGroup, sorts, search, err := notionViewQueryFilterGroup(app, location)
	if err != nil {
		return nil, false, err
	}
	return collectNotionDataSourceRows(
		app, identity, location.SourcePageID, location.DataSourceID, filterGroup, sorts, search,
		databaseArchiveScopeActive,
	)
}

// collectNotionDataSourceRows collects a data source's ordered row IDs through
// the native query job, which may answer in several pending turns, up to the
// public result limit; the flag reports rows past it. Its filters and sorts
// read a mention of a page the connection can't reach as "Untitled"
// (database_query_reader_mentions.go).
func collectNotionDataSourceRows(
	app core.App, identity notionOAuthBearerIdentity, sourcePageID, dataSourceID string,
	filterGroup *databaseQueryFilterGroup, sorts []databaseQuerySort, search *string, archiveScope string,
) ([]string, bool, error) {
	principalID, mentions := identity.PrincipalID, notionIdentityQueryMentions(identity)
	canonical, _, queryHash, err := prepareDatabaseQueryRequest(principalID, sourcePageID, databaseQueryRequest{
		DataSourceID: dataSourceID, FilterGroup: filterGroup, Sorts: sorts, Search: search,
		ArchiveScope: archiveScope, TimeZone: "UTC", Limit: maxDatabaseQueryResultRows,
	})
	if err != nil {
		return nil, false, err
	}
	rows, cursor := []string{}, ""
	for turns := 0; turns < maxNotionViewQueryRows; turns++ {
		turn, err := advanceDatabaseQueryTurnAs(
			context.Background(), app, principalID, sourcePageID, canonical, cursor, queryHash, mentions,
		)
		if err != nil {
			return nil, false, err
		}
		if turn.Response.State == databaseQueryStateReady {
			for _, row := range turn.Response.Rows {
				rows = append(rows, row.ID)
			}
			if len(rows) >= maxNotionViewQueryRows {
				return rows[:maxNotionViewQueryRows], true, nil
			}
			if !turn.Response.HasMore {
				return rows, false, nil
			}
		}
		if turn.Response.NextCursor == nil || *turn.Response.NextCursor == "" {
			return nil, false, errNotionDatabaseRowPageProjectionInvalid
		}
		cursor = *turn.Response.NextCursor
	}
	return rows, true, nil
}

func notionViewQueryPage(
	app core.App, query storedNotionViewQuery, start string, pageSize int,
) ([]any, any, bool, error) {
	rowIDs := []string{}
	if json.Unmarshal([]byte(query.RowIDsJSON), &rowIDs) != nil {
		return nil, nil, false, errNotionDatabaseRowPageProjectionInvalid
	}
	offset := 0
	if start != "" {
		identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityRowPage, start)
		offset = slices.Index(rowIDs, identity.InternalID)
		if err != nil || offset < 0 {
			return nil, nil, false, notionViewFilterInvalid("%s", notionStartCursorInvalid+start)
		}
	}
	results := []any{}
	end := min(offset+pageSize, len(rowIDs))
	for _, rowID := range rowIDs[offset:end] {
		publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityRowPage, query.WorkspaceID, rowID)
		if err != nil {
			return nil, nil, false, err
		}
		results = append(results, map[string]any{"object": "page", "id": publicID})
	}
	var next any
	if end < len(rowIDs) {
		publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityRowPage, query.WorkspaceID, rowIDs[end])
		if err != nil {
			return nil, nil, false, err
		}
		next = publicID
	}
	return results, next, next != nil, nil
}

func serveNotionViewQueryCreate(event *core.RequestEvent, viewPublicID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return err
	}
	location, err := loadNotionViewLocation(event.App, workspace.Identity.WorkspaceID, viewPublicID)
	if err == nil {
		_, err = authorizeNotionViewPages(event.App, workspace.Identity, location.PageID, location.SourcePageID, permissionWorkspaceRead)
	}
	if err != nil {
		return notionViewReadError(event, err)
	}
	input := struct {
		PageSize *int `json:"page_size"`
	}{}
	if event.Request.ContentLength != 0 {
		if err := bindNotionStrictJSON(event, &input); err != nil {
			return notionBindRefused(event, err)
		}
	}
	if input.PageSize != nil && (*input.PageSize < 1 || *input.PageSize > 100) {
		return notionPageValidationError(event, "page_size is a whole number from 1 to 100.")
	}
	pageSize := 100
	if input.PageSize != nil {
		pageSize = *input.PageSize
	}
	rows, incomplete, err := runNotionViewQuery(event.App, workspace.Identity, location)
	if err != nil {
		return notionViewReadError(event, err)
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		return notionViewReadError(event, err)
	}
	now := time.Now().UTC()
	query := storedNotionViewQuery{
		ID: uuid.NewString(), WorkspaceID: workspace.Identity.WorkspaceID,
		PrincipalID: workspace.Identity.PrincipalID, ViewID: location.ID, RowIDsJSON: string(encoded),
		Total: len(rows), Incomplete: incomplete, ExpiresAt: now.Add(notionViewQueryLifetime).UnixMilli(),
	}
	if _, err := event.App.DB().NewQuery(`DELETE FROM {{leafen_notion_view_queries}} WHERE [[expires_at]] <= {:now}`).
		Bind(dbx.Params{"now": now.UnixMilli()}).Execute(); err != nil {
		return notionViewReadError(event, err)
	}
	if _, err := event.App.DB().Insert("leafen_notion_view_queries", dbx.Params{
		"id": query.ID, "workspace_id": query.WorkspaceID, "principal_id": query.PrincipalID,
		"connection_id": notionAsyncConnectionID(workspace.Identity), "view_id": query.ViewID,
		"row_ids_json": query.RowIDsJSON, "total": query.Total,
		"incomplete": query.Incomplete, "created_at": now.UnixMilli(), "expires_at": query.ExpiresAt,
	}).Execute(); err != nil {
		return notionViewReadError(event, err)
	}
	results, next, more, err := notionViewQueryPage(event.App, query, "", pageSize)
	if err != nil {
		return notionViewReadError(event, err)
	}
	response := map[string]any{
		"object": "view_query", "id": query.ID, "view_id": viewPublicID,
		"expires_at":  time.UnixMilli(query.ExpiresAt).UTC().Format(time.RFC3339Nano),
		"total_count": query.Total, "results": results, "next_cursor": next, "has_more": more,
	}
	if incomplete {
		response["request_status"] = map[string]any{"type": "incomplete", "incomplete_reason": "query_result_limit_reached"}
	}
	return writeNotionJSON(event, http.StatusOK, response)
}

// loadNotionViewQuery finds a live query the caller's connection started on
// a view they can still read (schema_baseline.json).
func loadNotionViewQuery(
	app core.App, identity notionOAuthBearerIdentity, viewPublicID, queryID string,
) (storedNotionViewQuery, error) {
	location, err := loadNotionViewLocation(app, identity.WorkspaceID, viewPublicID)
	if err == nil {
		_, err = authorizeNotionViewPages(app, identity, location.PageID, location.SourcePageID, permissionWorkspaceRead)
	}
	if err != nil {
		return storedNotionViewQuery{}, err
	}
	query := storedNotionViewQuery{}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[workspace_id]], [[principal_id]], [[view_id]], [[row_ids_json]], [[total]],
			[[incomplete]], [[expires_at]]
		FROM {{leafen_notion_view_queries}}
		WHERE [[id]] = {:id} AND [[view_id]] = {:viewId} AND [[principal_id]] = {:principalId}
			AND [[connection_id]] = {:connectionId}
			AND [[workspace_id]] = {:workspaceId} AND [[expires_at]] > {:now}
	`).Bind(dbx.Params{
		"id": notionBodyIDShown(queryID), "viewId": location.ID,
		"principalId": identity.PrincipalID, "connectionId": notionAsyncConnectionID(identity),
		"workspaceId": identity.WorkspaceID, "now": time.Now().UTC().UnixMilli(),
	}).One(&query)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotionViewQuery{}, errNotionViewQueryNotFound
	}
	return query, err
}

var errNotionViewQueryNotFound = errors.New("view query not found")

// notionViewQueryError answers a query's read that found no query, its view
// unknown or out of reach too, as a view_query Notion's finds none of (seen
// 2026-11-12; notionObjectAsked names it).
func notionViewQueryError(event *core.RequestEvent, err error) error {
	if errors.Is(err, errNotionViewQueryNotFound) || errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) {
		return notionCompatibilityError(event, http.StatusNotFound, notionErrorObjectNotFound, "There's no view_query with the requested ID.")
	}
	return notionViewReadError(event, err)
}

func serveNotionViewQueryResults(event *core.RequestEvent, viewPublicID, queryID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return err
	}
	query, err := loadNotionViewQuery(event.App, workspace.Identity, viewPublicID, queryID)
	if err != nil {
		return notionViewQueryError(event, err)
	}
	pageSize, ok := notionViewQueryPageSize(notionQueryDigits(event.Request.URL.Query().Get("page_size")), 100)
	if !ok {
		return notionPageValidationError(event, "page_size must be between 1 and 100.")
	}
	results, next, more, err := notionViewQueryPage(event.App, query, strings.TrimSpace(event.Request.URL.Query().Get("start_cursor")), pageSize)
	if err != nil {
		return notionViewQueryError(event, err)
	}
	response := map[string]any{
		"object": "list", "results": results, "next_cursor": next, "has_more": more, "type": "page", "page": map[string]any{},
	}
	if query.Incomplete {
		response["request_status"] = map[string]any{"type": "incomplete", "incomplete_reason": "query_result_limit_reached"}
	}
	return writeNotionJSON(event, http.StatusOK, response)
}

// serveNotionViewQueryDelete deletes a query of the connection's by its ID
// whatever view the address names, and says it is deleted though it was
// gone or never there, as Notion does (seen 2026-11-12: a query deleted
// twice, through another view's address, and an unknown one all answer 200).
func serveNotionViewQueryDelete(event *core.RequestEvent, _, queryID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return err
	}
	id := notionBodyIDShown(queryID)
	if _, err := event.App.DB().NewQuery(`
		DELETE FROM {{leafen_notion_view_queries}}
		WHERE [[id]] = {:id} AND [[principal_id]] = {:principalId} AND [[connection_id]] = {:connectionId}
			AND [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{
		"id": id, "principalId": workspace.Identity.PrincipalID,
		"connectionId": notionAsyncConnectionID(workspace.Identity), "workspaceId": workspace.Identity.WorkspaceID,
	}).Execute(); err != nil {
		return notionViewReadError(event, err)
	}
	return writeNotionJSON(event, http.StatusOK, map[string]any{"object": "view_query", "id": id, "deleted": true})
}
