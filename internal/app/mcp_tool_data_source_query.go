package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	mcpDataSourceQueryModeRows = "rows"
	mcpDataSourceQueryModeView = "view"
	mcpDataSourceQueryModeSQL  = "sql"
	// How long one call goes on with a SQL query's snapshot, and with a
	// rows or view query's job.
	mcpDataSourceSQLCallBudget = 8 * time.Second
	mcpDataSourceURLPrefix     = "collection://"
)

type notionMCPQueryDataSourcesInput struct {
	Data notionMCPQueryDataSourcesData `json:"data" jsonschema:"Official Notion data-source query envelope"`
}

type notionMCPQueryDataSourcesData struct {
	Mode           string              `json:"mode,omitempty" jsonschema:"Query mode: rows, sql or view (a saved view's rows, under its filters and sorts)"`
	ViewURL        string              `json:"view_url,omitempty" jsonschema:"For view mode: the view, as a view:// URL from notion-fetch"`
	PageSize       int                 `json:"page_size,omitzero" jsonschema:"For view mode: rows per answer; defaults to 100"`
	DataSourceURL  string              `json:"data_source_url,omitempty" jsonschema:"Single Leafen data source URL in collection://ID form for rows mode"`
	Limit          int                 `json:"limit,omitempty" jsonschema:"For rows mode: rows per answer; defaults to 50 and cannot exceed 100"`
	Filter         map[string]any      `json:"filter,omitempty" jsonschema:"For rows mode: a group {type:group, operator:and|or, filters:[…]} of property filters {type:property, property, propertyType, operator, value:{type:exact, value}}, one nested group level, as Notion's"`
	Sort           []notionMCPRowsSort `json:"sort,omitempty" jsonschema:"For rows mode: property sorts applied in order"`
	DataSourceURLs []string            `json:"data_source_urls,omitempty" jsonschema:"Authorized collection:// data source URLs referenced by a SQL query"`
	Query          string              `json:"query,omitempty" jsonschema:"Read-only logical SQL query using quoted collection:// URLs as table names; no VALUES: inline rows as WITH \"ids\"(\"x\") AS (SELECT 'a' UNION ALL SELECT 'b'). Title and text values read as plain text, and one holding a mention, a link or an equation as notion-fetch's Markdown, which written back keeps them; formatting alone is left out: rewrite a formatted one from rows mode or notion-fetch"`
	Params         []any               `json:"params,omitempty" jsonschema:"Bind values for SQL question-mark placeholders, in order: each a string, a number, true or false, or null"`
	// What a view's query asks for, from its settings
	// (queryMCPDataSourceView): the view itself, for the rows it doesn't
	// sort to stand in its order, as its page shows them.
	viewID      string
	filterGroup *databaseQueryFilterGroup
	sorts       []databaseQuerySort
	search      *string
	// sourceNames are the data sources data_source_urls names by the public
	// API's IDs, each to the URL the tools name it by (mcpSQLSourceNames).
	sourceNames map[string]string
	StartCursor string `json:"start_cursor,omitempty" jsonschema:"The next_cursor the last answer gave, with the same arguments: the query's next results (rows, view and sql modes)"`
}

// notionMCPQueryDataSourcesOutput is a query's answer as Notion's MCP gives it (seen 2026-11-11): its rows, whether more follow, a view's next cursor
// and the data sources a SQL query read — a rows or SQL query's next cursor and warnings too, Leafen's own; one left unfinished is pending (markPending).
type notionMCPQueryDataSourcesOutput struct {
	Results           []any    `json:"results"`
	HasMore           bool     `json:"has_more"`
	NextCursor        string   `json:"next_cursor,omitempty"`
	DataSourceIDs     []string `json:"data_source_ids,omitempty"`
	Status            string   `json:"status,omitempty"`
	RetryAfterSeconds *int     `json:"retry_after_seconds,omitempty"`
	NextAction        string   `json:"next_action,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
}

type mcpDataSourceLocation struct {
	ID          string `db:"id"`
	OwnerID     string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
	Title       string `db:"title"`
}

type notionMCPDataSourceProperty struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Config map[string]any `json:"config"`
}

type notionMCPDataSourceView struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Config map[string]any `json:"config"`
	// DataSourceID is the source a database's view shows.
	DataSourceID string `json:"-"`
}

type storedMCPDataSourceProperty struct {
	ID       string `db:"id"`
	Name     string `db:"name"`
	Type     string `db:"type"`
	Position string `db:"position"`
	Config   string `db:"config"`
}

type storedMCPDataSourceView struct {
	ID           string `db:"id"`
	Name         string `db:"name"`
	Type         string `db:"type"`
	Position     string `db:"position"`
	Config       string `db:"config"`
	DataSourceID string `db:"data_source_id"`
}

func registerMCPDataSourceQueryTool(
	server *mcp.Server,
	app core.App,
	readOnly *mcp.ToolAnnotations,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-query-data-sources", Title: "Query Leafen data sources",
		Description: mcpQueryDataSourcesDescription,
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPQueryDataSourcesInput) (*mcp.CallToolResult, notionMCPQueryDataSourcesOutput, error) {
		mode := strings.TrimSpace(input.Data.Mode)
		if mode == "" && strings.TrimSpace(input.Data.Query) != "" {
			// A query without a mode is SQL, as Notion's.
			mode = mcpDataSourceQueryModeSQL
			input.Data.Mode = mode
		}
		var answer func(context.Context, core.App, notionMCPQueryDataSourcesData) (*mcp.CallToolResult, notionMCPQueryDataSourcesOutput, error)
		switch mode {
		case mcpDataSourceQueryModeRows:
			answer = queryMCPDataSourceRows
		case mcpDataSourceQueryModeSQL:
			answer = queryMCPDataSourceSQL
		case mcpDataSourceQueryModeView:
			answer = queryMCPDataSourceView
		default:
			return nil, notionMCPQueryDataSourcesOutput{}, mcpInvalid("data.mode is rows (one data source's rows by data_source_url) or sql (a SELECT over data_source_urls)")
		}
		result, output, err := answer(ctx, app, input.Data)
		if result != nil || err != nil {
			return result, output, err
		}
		// The answer's text keeps its keys' order and its rows' columns',
		// and writes < and > as they are, as Notion's; its structured
		// content is the same answer by name.
		text, err := mcpFetchJSON(output)
		if err != nil {
			return nil, notionMCPQueryDataSourcesOutput{}, failedBecause(errMCPToolFailed, err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, output, nil
	})
}

// queryMCPDataSourceView reads a saved view's rows, under its filters, its
// filter bar and its sorts, as a person's view shows them.
func queryMCPDataSourceView(
	ctx context.Context, app core.App, input notionMCPQueryDataSourcesData,
) (*mcp.CallToolResult, notionMCPQueryDataSourcesOutput, error) {
	raw := strings.TrimSpace(input.ViewURL)
	if _, query, found := strings.Cut(raw, "?"); found && !strings.HasPrefix(raw, mcpViewURLPrefix) {
		if values, err := url.ParseQuery(query); err == nil && values.Get("v") != "" {
			raw = values.Get("v")
		}
	}
	viewID, ok := parseMCPViewIdentifier(raw)
	if !ok {
		return nil, notionMCPQueryDataSourcesOutput{}, mcpInvalid("mode view takes view_url, a view:// URL from notion-fetch")
	}
	// A view of a data source in the trash reads its rows too, though fetch
	// finds no such view (mcp_fetch_trash.go).
	view, err := loadMCPViewLocationToRead(app, viewID)
	if err != nil {
		return nil, notionMCPQueryDataSourcesOutput{}, mcpDataSourceQueryError(err)
	}
	source, err := loadMCPDataSourceLocationToRead(app, view.DataSourceID)
	if err != nil {
		return nil, notionMCPQueryDataSourcesOutput{}, mcpDataSourceQueryError(err)
	}
	filterGroup, sorts, search, err := notionViewQueryFilterGroup(app, notionViewLocation{
		DataSourceID: view.DataSourceID, PageID: view.PageID, SourcePageID: source.PageID, Config: view.Config,
	})
	if err != nil {
		return nil, notionMCPQueryDataSourcesOutput{}, failedBecause(errMCPToolFailed, err)
	}
	limit := input.PageSize
	if limit == 0 {
		limit = maxDatabaseQueryResultRows
	}
	return queryMCPDataSourceRows(ctx, app, notionMCPQueryDataSourcesData{
		Mode: mcpDataSourceQueryModeView, DataSourceURL: mcpDataSourceURLPrefix + view.DataSourceID, Limit: limit,
		StartCursor: input.StartCursor, viewID: view.ID, filterGroup: filterGroup, sorts: sorts, search: search,
	})
}

func optionalCursor(cursor string) *string {
	if strings.TrimSpace(cursor) == "" {
		return nil
	}
	return &cursor
}

func queryMCPDataSourceSQL(
	ctx context.Context,
	app core.App,
	input notionMCPQueryDataSourcesData,
) (*mcp.CallToolResult, notionMCPQueryDataSourcesOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPQueryDataSourcesOutput{}, err
	}
	// A data source the public API names reads as the tools name it, in
	// data_source_urls and the query alike.
	input.sourceNames = mcpSQLSourceNames(app, input.DataSourceURLs)
	plan, err := compileDatabaseSQLQueryPlan(input)
	if err != nil {
		return nil, notionMCPQueryDataSourcesOutput{}, mcpSQLQueryInvalid(err)
	}
	from := 0
	if cursor := strings.TrimSpace(input.StartCursor); cursor != "" {
		if from, err = strconv.Atoi(cursor); err != nil || from <= 0 {
			return nil, notionMCPQueryDataSourcesOutput{}, mcpInvalid("start_cursor is the next_cursor this query gave")
		}
	}
	output := notionMCPQueryDataSourcesOutput{Results: []any{}, HasMore: true, Warnings: databaseSQLUnreadWarnings(plan)}
	for _, address := range plan.DataSourceURLs {
		output.DataSourceIDs = append(output.DataSourceIDs, strings.TrimPrefix(address, mcpDataSourceURLPrefix))
	}
	state := databaseSQLQueryStateCapturing
	// A ready query's results, a window at a time from the cursor on.
	readResults := func(txApp core.App, job storedDatabaseSQLQueryJob) error {
		results, next, err := loadDatabaseSQLQueryResults(txApp, job.ID, from)
		if err != nil {
			return err
		}
		output.Results, state, output.HasMore = results, job.State, next > 0
		if next > 0 {
			output.NextCursor = strconv.Itoa(next)
		}
		return nil
	}
	authorized := databaseSQLAuthorizedPlan{}
	job := storedDatabaseSQLQueryJob{}
	semanticCode, refused := "", error(errDatabaseSQLQueryInvalid)
	// One step of the query's durable snapshot, its own transaction. The call
	// goes on stepping for a while, so a query of small sources answers at
	// once as Notion's does and only a long one says to call again.
	step := func(txApp core.App) error {
		var err error
		authorized, err = authorizeDatabaseSQLQueryPlanTransaction(txApp, authority, plan)
		if err != nil {
			return err
		}
		job, err = admitDatabaseSQLQueryJob(txApp, authorized, authority.PrincipalID, strings.TrimSpace(input.StartCursor) == "")
		if err != nil {
			return err
		}
		sources, err := loadDatabaseSQLQuerySources(txApp, job.ID)
		if err != nil {
			return err
		}
		if err := validateDatabaseSQLQueryJobSources(job, sources, authorized); err != nil {
			return err
		}
		if job.State == databaseSQLQueryStateFailed {
			semanticCode = job.FailureCode
			return nil
		}
		if job.State == databaseSQLQueryStateReady {
			return readResults(txApp, job)
		}
		if job.State != databaseSQLQueryStateCapturing || job.CancelRequested ||
			job.SourceOrdinal < 0 || job.SourceOrdinal > len(sources) {
			return databaseSQLQueryInvalid()
		}
		if job.SourceOrdinal < len(sources) {
			job, err = advanceDatabaseSQLSourceCapture(txApp, authority, job, sources[job.SourceOrdinal])
			if err != nil {
				if errors.Is(err, errDatabaseSQLQueryResourceLimit) {
					semanticCode = errorCodeDatabaseSQLQueryResourceLimit
					return failDatabaseSQLQueryJob(txApp, job.ID, semanticCode)
				}
				return err
			}
			output.markPending(mcpSQLQueryPendingAction(job, sources))
			return nil
		}
		job, err = executeDatabaseSQLQuery(ctx, txApp, job, authorized, sources)
		if err != nil {
			if errors.Is(err, errDatabaseSQLQueryResourceLimit) {
				semanticCode = errorCodeDatabaseSQLQueryResourceLimit
			} else if errors.Is(err, errDatabaseSQLQueryInvalid) {
				semanticCode, refused = errorCodeDatabaseSQLQueryInvalid, err
			} else {
				return err
			}
			return failDatabaseSQLQueryJob(txApp, job.ID, semanticCode)
		}
		return readResults(txApp, job)
	}
	for started := time.Now(); ; {
		output.clearPending()
		err = runDatabaseSQLQueryStep(app, step)
		if err != nil || semanticCode != "" || state != databaseSQLQueryStateCapturing ||
			ctx.Err() != nil || time.Since(started) >= mcpDataSourceSQLCallBudget {
			break
		}
	}
	if ctx.Err() != nil {
		_ = cancelDatabaseSQLQueryJob(
			app, authority.PrincipalID, authorized.WorkspaceID, authorized.CanonicalHash,
		)
		return nil, notionMCPQueryDataSourcesOutput{}, ctx.Err()
	}
	if err != nil {
		return nil, notionMCPQueryDataSourcesOutput{}, notionImportFillingRefusal(ctx, app, databaseSQLAuthorizedPlanError(err), output.DataSourceIDs...)
	}
	if semanticCode == errorCodeDatabaseSQLQueryInvalid {
		// Why it didn't run, on the call that ran it; a call after reads the stored refusal.
		return nil, notionMCPQueryDataSourcesOutput{}, mcpSQLQueryInvalid(refused)
	}
	if semanticCode == errorCodeDatabaseSQLQueryResourceLimit {
		body, marshalErr := json.Marshal(databaseSQLResourceLimitRefusal())
		if marshalErr != nil {
			return nil, notionMCPQueryDataSourcesOutput{}, failedBecause(errMCPToolFailed, marshalErr)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}, IsError: true,
		}, notionMCPQueryDataSourcesOutput{}, nil
	}
	return nil, mcpQueryPublicDataSourceIDs(app, withMCPQueryNotices(app, output, output.DataSourceIDs...)), nil
}

func queryMCPDataSourceRows(
	ctx context.Context,
	app core.App,
	input notionMCPQueryDataSourcesData,
) (*mcp.CallToolResult, notionMCPQueryDataSourcesOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPQueryDataSourcesOutput{}, err
	}
	mode := strings.TrimSpace(input.Mode)
	// The public API's data source ID, bare or in a collection:// URL, reads as the tools' (mcpLocalID).
	dataSourceID, ok := mcpLocalDataSourceID(app, input.DataSourceURL)
	limit := input.Limit
	if limit == 0 {
		limit = 50
	}
	if mode != mcpDataSourceQueryModeRows && input.viewID == "" || !ok || limit < 1 || limit > maxDatabaseQueryResultRows ||
		len(input.DataSourceURLs) != 0 || strings.TrimSpace(input.Query) != "" || input.Params != nil {
		return nil, notionMCPQueryDataSourcesOutput{}, mcpInvalid("mode rows takes data_source_url (collection://…) and a limit from 1 to %d, without data_source_urls, query or params", maxDatabaseQueryResultRows)
	}
	location, err := loadMCPDataSourceLocationToRead(app, dataSourceID)
	if err != nil {
		return nil, notionMCPQueryDataSourcesOutput{}, mcpDataSourceQueryError(err)
	}
	page, err := authorizePage(app, authority.PrincipalID, location.PageID, permissionWorkspaceRead)
	if err != nil || page.Workspace.WorkspaceID != location.WorkspaceID ||
		page.Workspace.OwnerID != location.OwnerID {
		return nil, notionMCPQueryDataSourcesOutput{}, notionImportFillingRefusal(ctx, app, errMCPCantRead, dataSourceID)
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: location.WorkspaceID,
	}, permissionWorkspaceRead)
	if err != nil || workspace.WorkspaceID != location.WorkspaceID {
		return nil, notionMCPQueryDataSourcesOutput{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	if input.Filter != nil || len(input.Sort) > 0 {
		input.filterGroup, input.sorts, err = mcpRowsFilterAndSorts(app, location, authority.PrincipalID, input.Filter, input.Sort)
		if err != nil {
			return nil, notionMCPQueryDataSourcesOutput{}, err
		}
	}
	request := databaseQueryRequest{
		DataSourceID: dataSourceID, TimeZone: authority.clock().String(), Limit: limit, // the person's days (person_clock.go)
		FilterGroup: input.filterGroup, Sorts: input.sorts, Search: input.search, Cursor: optionalCursor(input.StartCursor),
	}
	// A view's rows it doesn't sort stand in its own order, as Notion's
	// view mode gives them (2026-11-11: each view's, as its page shows it);
	// rows mode's, in the source's first view's.
	if input.viewID != "" {
		request.ViewID = &input.viewID
	}
	queryInput, rawCursor, queryHash, err := prepareDatabaseQueryRequest(authority.PrincipalID, location.PageID, request)
	if err != nil {
		return nil, notionMCPQueryDataSourcesOutput{}, mcpDataSourceQueryError(err)
	}
	output := notionMCPQueryDataSourcesOutput{Results: []any{}, HasMore: true}
	listed := []contentRead{}
	// Its filters, sorts and computed values read a mention of a page the
	// agent can't open as "Untitled" (database_query_reader_mentions.go).
	mentions := mcpQueryMentions(authority)
	// Answered directly in a read snapshot when it can be, in its job
	// otherwise (database_query_direct.go).
	pending := false
	answer := func(txApp core.App, mode databaseQueryTurnMode) error {
		var turn databaseQueryTurn
		var err error
		if mode == databaseQueryJobTurn {
			// As the API's query, a call goes on for the turn's budget
			// (2026-11-06: one step a call asked an agent 668 calls, 7-9s,
			// for a 236-row source; now about 34, 2s).
			turn, err = advanceDatabaseQueryJobTurns(txApp, authority.PrincipalID, location.PageID, queryInput, rawCursor, queryHash, mentions)
		} else {
			turn, err = advanceDatabaseQueryTurnWith(
				txApp, authority.PrincipalID, location.PageID, queryInput, rawCursor, queryHash, mode, mentions,
			)
		}
		if err != nil {
			return err
		}
		if turn.Authorization.Workspace.WorkspaceID != workspace.WorkspaceID {
			return errMCPOutsideConnection
		}
		output.HasMore = turn.Response.HasMore
		pending = turn.Response.State == databaseQueryStatePending
		if pending {
			action, err := mcpRowsQueryPendingAction(txApp, turn)
			output.markPending(action)
			return err
		}
		queryContext, err := loadDatabaseQueryContext(txApp, turn.Job)
		if err != nil {
			return err
		}
		facts, err := loadMCPDatabaseRowFacts(txApp, turn.Response.Rows)
		if err != nil {
			return err
		}
		// The rows read as the principal may see them, naming only the pages it may open
		// (mcp_reader_mentions.go), with each property the schema shows them, a view's hidden
		// ones too, as Notion's view mode gives a table's and a board's (seen 2026-11-17 twice).
		if err := redactDatabaseQueryRows(txApp, authority.PrincipalID, dataSourceID, turn.Response.Rows); err != nil {
			return err
		}
		mcpRowsAsRead(txApp, authority, workspace.WorkspaceID, turn.Response.Rows)
		properties, _, err := loadMCPDataSourceSchema(txApp, location, authority.PrincipalID)
		if err != nil {
			return err
		}
		shown := make(map[string]bool, len(properties))
		for _, property := range properties {
			shown[property.ID] = true
		}
		ids, err := loadNotionPropertyIDs(txApp, dataSourceID)
		if err != nil {
			return err
		}
		output.Results = projectMCPDataSourceRows(
			authority, turn.Response.Rows, queryContext, facts, shown, ids,
			mcpPropertyMentions(txApp, authority, workspace.WorkspaceID),
		)
		// With more rows, the cursor the next call passes as start_cursor
		// reads on from there, in rows mode as in view mode (2026-11-12: a
		// rows answer said has_more with no way past its first page).
		if turn.Response.NextCursor != nil {
			output.NextCursor = *turn.Response.NextCursor
		}
		for _, row := range turn.Response.Rows {
			listed = append(listed, contentRead{Kind: "row", ID: row.ID, How: contentReadList})
		}
		return nil
	}
	err = runReadSnapshot(ctx, app, func(readApp core.App) error {
		return answer(readApp, databaseQueryDirectTurn)
	})
	// Its job goes on a transaction at a time, each a turn's budget on the
	// write connection, until it is done or the call's budget is spent; the
	// next call goes on from there. One transaction a call, an agent called
	// 60 times over two minutes and got no row of a 37-row source of many
	// rollups (2026-11-12; a 120-row synthetic one took 20 calls).
	// Each transaction keeps its access decisions as the job's steps do
	// (advanceDatabaseQueryJobTurns): the answer it then reads writes
	// nothing, and the related rows its cells name, each read as the agent
	// may open it, judged their database's page again for each row (2026-11-12:
	// half of a 40-row contracts source's 15,700 statements, 500 related rows).
	if errors.Is(err, errDatabaseQueryNeedsJob) {
		for started := time.Now(); ; {
			output.clearPending()
			err = app.RunInTransaction(func(txApp core.App) error {
				return answer(withAccessMemo(txApp), databaseQueryJobTurn)
			})
			if err != nil || !pending || ctx.Err() != nil || time.Since(started) >= mcpDataSourceSQLCallBudget {
				break
			}
		}
	}
	if err != nil {
		return nil, notionMCPQueryDataSourcesOutput{}, mcpDataSourceQueryError(err)
	}
	recordMCPReads(app, authority, workspace.WorkspaceID, listed)
	return nil, withMCPQueryNotices(app, output, dataSourceID), nil
}

// mcpRowsQueryPendingAction says how far a rows or view query's job got
// when a call's budget ran out before its rows were worked out: the rows
// looked at of the source's, and the formula or rollup being worked out on
// one, so an agent reading no results can tell it is going on.
func mcpRowsQueryPendingAction(app core.App, turn databaseQueryTurn) (string, error) {
	job := turn.Job
	var total int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_rows}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
			AND ` + databaseRowNotTrashedSQL("[[id]]") + `
	`).Bind(dbx.Params{
		"owner": job.ContentOwnerID, "pageId": job.PageID, "dataSourceId": job.DataSourceID,
	}).Row(&total); err != nil {
		return "", err
	}
	working := ""
	if job.DependencySourceID != "" && job.DependencyPhase == databaseQueryDependencyPhaseEvaluation {
		queryContext, err := loadDatabaseQueryContext(app, job)
		if err != nil {
			return "", err
		}
		if index := job.DependencyEvaluationProperty; index >= 0 && index < len(queryContext.Properties) {
			working = fmt.Sprintf(", now working out %q on a row", queryContext.Properties[index].Name)
		}
	}
	return fmt.Sprintf(
		"Leafen is still working out this query's formulas and rollups: %d of the source's %d rows looked at so far%s. "+
			"Call notion-query-data-sources again with the same arguments to go on from there.",
		turn.Response.Scanned, total, working,
	), nil
}

// mcpSQLSourceNames reads each of a SQL query's data_source_urls that names
// a data source by the public API's ID, as Notion's MCP takes its API's IDs,
// as the URL the tools name it by (mcpLocalID); the query's FROM and JOIN
// and its columns' table names read the same (compileDatabaseSQLQueryPlan).
func mcpSQLSourceNames(app core.App, addresses []string) map[string]string {
	names := map[string]string{}
	for _, address := range addresses {
		given, ok := parseMCPDataSourceURL(address)
		if !ok {
			continue
		}
		if local, ok := parseMCPDataSourceURL(mcpLocalID(app, mcpDataSourceURLPrefix+given)); ok && local != given {
			names[mcpDataSourceURLPrefix+given] = mcpDataSourceURLPrefix + local
		}
	}
	return names
}

func parseMCPDataSourceURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, mcpDataSourceURLPrefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(raw, mcpDataSourceURLPrefix))
	return id, id != "" && len(id) <= maxGraphIdentifierLength &&
		!strings.ContainsAny(id, "/?#")
}

// loadMCPDataSourceSchema reads a data source's properties and views as
// the principal sees them: a property no row shows them under its access
// rule leaves the schema.
func loadMCPDataSourceSchema(
	app core.App,
	location mcpDataSourceLocation,
	principalID string,
) ([]notionMCPDataSourceProperty, []notionMCPDataSourceView, error) {
	storedProperties := []storedMCPDataSourceProperty{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[type]], [[position]], [[config]]
		FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
			AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": location.OwnerID, "workspaceId": location.WorkspaceID,
		"pageId": location.PageID, "dataSourceId": location.ID,
		"limit": maxDatabaseQueryProperties + 1,
	}).All(&storedProperties); err != nil {
		return nil, nil, err
	}
	storedViews := []storedMCPDataSourceView{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[type]], [[position]], [[config]]
		FROM {{leafen_database_views}}
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
			AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
			AND [[container_id]] = ''
		ORDER BY [[position]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": location.OwnerID, "workspaceId": location.WorkspaceID,
		"pageId": location.PageID, "dataSourceId": location.ID,
		"limit": maxGraphMutationOperations + 1,
	}).All(&storedViews); err != nil {
		return nil, nil, err
	}
	if len(storedProperties) > maxDatabaseQueryProperties || len(storedViews) > maxGraphMutationOperations {
		return nil, nil, errMCPToolFailed
	}
	reader, err := newDatabasePropertyAccessReader(app, principalID, []string{location.ID})
	if err != nil {
		return nil, nil, err
	}
	properties := make([]notionMCPDataSourceProperty, 0, len(storedProperties))
	for _, stored := range storedProperties {
		if hidden, err := notionSchemaPropertyHidden(reader, location.ID, stored.ID); err != nil {
			return nil, nil, err
		} else if hidden {
			continue
		}
		config := map[string]any{}
		if err := json.Unmarshal([]byte(stored.Config), &config); err != nil || config == nil {
			return nil, nil, errors.New(errorCodeGraphReadFailed)
		}
		properties = append(properties, notionMCPDataSourceProperty{
			ID: stored.ID, Name: stored.Name, Type: stored.Type, Config: config,
		})
	}
	views := make([]notionMCPDataSourceView, 0, len(storedViews))
	for _, stored := range storedViews {
		config := map[string]any{}
		if err := json.Unmarshal([]byte(stored.Config), &config); err != nil || config == nil {
			return nil, nil, errors.New(errorCodeGraphReadFailed)
		}
		views = append(views, notionMCPDataSourceView{
			ID: stored.ID, Name: stored.Name, Type: stored.Type, Config: config,
		})
	}
	return properties, views, nil
}

func loadMCPDataSourceLocation(app core.App, dataSourceID string) (mcpDataSourceLocation, error) {
	return loadMCPDataSourceLocationWhere(app, dataSourceID, databaseSourceNotTrashedSQL("[[source]].[[id]]"))
}

// loadMCPDataSourceLocationToRead also finds a source in the trash — kept in
// its database where it stands, or with its database — which Notion's MCP
// reads as any: its fetch, and its rows by rows, sql and a view's query
// (mcp_fetch_trash.go).
func loadMCPDataSourceLocationToRead(app core.App, dataSourceID string) (mcpDataSourceLocation, error) {
	return loadMCPDataSourceLocationWhere(app, dataSourceID, "TRUE")
}

func loadMCPDataSourceLocationWhere(app core.App, dataSourceID, shown string) (mcpDataSourceLocation, error) {
	location := mcpDataSourceLocation{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]], [[source]].[[owner]], [[source]].[[workspace_id]],
			[[source]].[[page_id]], [[source]].[[title]]
		FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[id]] = {:id} AND ` + shown + `
	`).Bind(dbx.Params{"id": dataSourceID}).One(&location)
	if errors.Is(err, sql.ErrNoRows) {
		return mcpDataSourceLocation{}, mcpDenied("that data source is gone, or isn't one this connection reaches")
	}
	return location, err
}

func mcpDataSourceQueryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errMCPScopeDenied) {
		return err
	}
	if isPermissionDenied(err) {
		return errMCPCantRead
	}
	// What an ID names is gone, or never was: the tool says so, not that
	// it couldn't finish (2026-11-13: an unknown database_id did).
	if errors.Is(err, sql.ErrNoRows) {
		return errMCPGoneOrUnreached
	}
	var requestError *graphRequestError
	if errors.As(err, &requestError) {
		if requestError.status == http.StatusNotFound {
			return mcpGraphReason(errMCPScopeDenied, err)
		}
		return mcpGraphReason(errMCPToolInvalid, err)
	}
	switch err.Error() {
	case errorCodePageNotFound, errorCodeDatabaseQueryNotFound:
		return mcpDenied("that page or query is gone, or isn't one this connection reaches")
	case errorCodeDatabaseQueryInvalid, errorCodeDatabaseQueryCursorInvalid,
		errorCodeDatabaseQueryDependencyLimit:
		return mcpInvalid("the query didn't read: check its filter and sorts against the data source's properties (notion-fetch), or start_cursor against the one it gave")
	default:
		return failedBecause(errMCPToolFailed, err)
	}
}

// mcpSQLQueryInvalid is a refused SQL query as the tool says it: why, where
// the query's reading knows, else the form Leafen runs.
func mcpSQLQueryInvalid(err error) error {
	if reason := databaseSQLQueryReason(err); reason != "" {
		return mcpInvalid("%s", reason)
	}
	return mcpInvalid("the query didn't read as SQL Leafen runs: one SELECT over the data sources named in data_source_urls, by their names as notion-fetch gives them")
}
