package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	// Results are kept whole, as many rows as the query gives up to the
	// same bytes as the snapshot, and read a window at a time.
	databaseSQLResultWindow     = 100
	maxDatabaseSQLResultColumns = 256
	maxDatabaseSQLResultBytes   = maxDatabaseSQLStagedBytes
	maxDatabaseSQLPlanWork      = 1_000_000
	databaseSQLExecutionTimeout = 2 * time.Second
)

var errDatabaseSQLQueryResourceLimit = errors.New(errorCodeDatabaseSQLQueryResourceLimit)

func executeDatabaseSQLQuery(
	ctx context.Context,
	app core.App,
	job storedDatabaseSQLQueryJob,
	authorized databaseSQLAuthorizedPlan,
	sources []storedDatabaseSQLQuerySource,
) (storedDatabaseSQLQueryJob, error) {
	work, err := estimateDatabaseSQLPlanWork(authorized.Plan, sources)
	if err != nil || work > maxDatabaseSQLPlanWork {
		return job, errDatabaseSQLQueryResourceLimit
	}
	statement, params, names, err := compileDatabaseSQLExecution(job.ID, authorized)
	if err != nil {
		return job, err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_sql_query_jobs}}
		SET [[state]] = 'executing', [[updated_at]] = {:now}
		WHERE [[id]] = {:jobId} AND [[state]] = 'capturing'
	`).Bind(dbx.Params{"now": time.Now().UTC().UnixMilli(), "jobId": job.ID}).Execute(); err != nil {
		return job, err
	}
	job.State = databaseSQLQueryStateExecuting
	executionContext, cancel := context.WithTimeout(ctx, databaseSQLExecutionTimeout)
	defer cancel()
	rows, err := app.DB().NewQuery(statement).Bind(params).WithContext(executionContext).Rows()
	if err != nil {
		if ctx.Err() != nil {
			return job, ctx.Err()
		}
		if executionContext.Err() != nil {
			return job, refusedUnlessStorage(errDatabaseSQLQueryResourceLimit, err)
		}
		return job, databaseSQLQueryInvalid("the query didn't run: %s%s", databaseSQLEngineReason(err), databaseSQLColumnHint(databaseSQLEngineReason(err), authorized.Sources))
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return job, err
	}
	if len(columns) == 0 || len(columns) > maxDatabaseSQLResultColumns {
		return job, errDatabaseSQLQueryResourceLimit
	}
	outputNames := disambiguateDatabaseSQLResultColumns(databaseSQLWrittenResultColumns(columns, names))
	resultCount, resultBytes := 0, 0
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return job, err
		}
		// Its columns in the query's order, as Notion's SQL gives them.
		result := mcpResultRow{}
		for index, name := range outputNames {
			result.set(name, normalizeDatabaseSQLResultValue(values[index]))
		}
		encoded, err := mcpFetchJSON(result)
		if err != nil {
			return job, err
		}
		resultBytes += len(encoded)
		if resultBytes > maxDatabaseSQLResultBytes {
			return job, errDatabaseSQLQueryResourceLimit
		}
		if _, err := app.DB().Insert(databaseSQLQueryResultsTable, dbx.Params{
			"job_id": job.ID, "ordinal": resultCount, "row_json": string(encoded),
		}).Execute(); err != nil {
			return job, err
		}
		resultCount++
	}
	if err := rows.Err(); err != nil {
		if ctx.Err() != nil {
			return job, ctx.Err()
		}
		if executionContext.Err() != nil {
			return job, refusedUnlessStorage(errDatabaseSQLQueryResourceLimit, err)
		}
		return job, err
	}
	now := time.Now().UTC()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_sql_query_jobs}}
		SET [[state]] = 'ready', [[result_count]] = {:resultCount},
			[[updated_at]] = {:now}, [[expires_at]] = {:expiresAt}
		WHERE [[id]] = {:jobId} AND [[state]] = 'executing'
	`).Bind(dbx.Params{
		"resultCount": resultCount, "now": now.UnixMilli(),
		"expiresAt": now.Add(databaseSQLQueryLifetime).UnixMilli(), "jobId": job.ID,
	}).Execute(); err != nil {
		return job, err
	}
	if err := recordOrganizationAuditEvent(app, organizationAuditEvent{
		ID: organizationAuditID(
			authorized.OrganizationID, "mcp.data_source_sql_query", job.ID,
		),
		OrganizationID: authorized.OrganizationID, WorkspaceID: authorized.WorkspaceID,
		ActorID: job.PrincipalID, Action: "mcp.data_source_sql_query",
		TargetType: "database_sql_query", TargetID: job.ID,
		Metadata: map[string]any{
			"queryHash": job.QueryHash, "sourceCount": len(authorized.Sources),
			"scanned": job.Scanned, "resultCount": resultCount,
		},
		OccurredAt: now.UnixMilli(),
	}); err != nil {
		return job, err
	}
	job.State, job.ResultCount = databaseSQLQueryStateReady, resultCount
	return job, nil
}

func estimateDatabaseSQLPlanWork(
	plan databaseSQLQueryPlan,
	sources []storedDatabaseSQLQuerySource,
) (int, error) {
	rowCounts := make(map[string]int, len(sources))
	for _, source := range sources {
		if source.SourceURL == "" || source.CapturedRows < 0 || !source.Complete {
			return 0, databaseSQLQueryInvalid()
		}
		rowCounts[source.SourceURL] = source.CapturedRows
	}
	_, work, err := walkDatabaseSQLQuery(plan.Tokens, databaseSQLDeclaredSources(plan), func(source string) (int, bool) {
		rows, ok := rowCounts[source]
		return rows, ok
	})
	return work, err
}

// databaseSQLDeclaredSources are the data sources a plan reads, by name.
func databaseSQLDeclaredSources(plan databaseSQLQueryPlan) map[string]struct{} {
	declared := make(map[string]struct{}, len(plan.ReferencedSources))
	for _, source := range plan.ReferencedSources {
		declared[source] = struct{}{}
	}
	return declared
}

// compileDatabaseSQLExecution is the statement a query runs as, its
// parameters, and its result columns' names as written.
func compileDatabaseSQLExecution(
	jobID string,
	authorized databaseSQLAuthorizedPlan,
) (string, dbx.Params, []databaseSQLResultName, error) {
	if jobID == "" || len(authorized.Sources) == 0 {
		return "", nil, nil, databaseSQLQueryInvalid()
	}
	params := dbx.Params{"sqlJobId": jobID}
	commonTables := make([]string, 0, len(authorized.Sources))
	joined := databaseSQLQueryJoinsSources(authorized.Plan)
	for sourceOrdinal, source := range authorized.Sources {
		if len(source.Columns) == 0 {
			return "", nil, nil, databaseSQLQueryInvalid()
		}
		projections := make([]string, 0, len(source.Columns))
		for columnOrdinal, column := range source.Columns {
			pathKey := fmt.Sprintf("sqlPath%d_%d", sourceOrdinal, columnOrdinal)
			params[pathKey] = fmt.Sprintf("$[%d]", columnOrdinal)
			projections = append(projections,
				"json_extract([[row_json]], {:"+pathKey+"}) AS "+quoteDatabaseSQLIdentifier(column.Name),
			)
		}
		commonTables = append(commonTables, databaseSQLSourceTable(quoteDatabaseSQLIdentifier(source.URL), projections, sourceOrdinal, joined))
	}
	// Its quoted names by their exact names (database_sql_query_names.go).
	tokens, err := databaseSQLExactNames(authorized.Plan.Tokens, authorized.Sources)
	if err != nil {
		return "", nil, nil, err
	}
	parts, err := renderDatabaseSQLTokens(tokens, authorized.Plan.Params, params)
	if err != nil {
		return "", nil, nil, err
	}
	statement := "WITH " + strings.Join(commonTables, ", ") +
		" SELECT * FROM (" + strings.Join(parts, " ") + ") AS [[leafen_sql_result]]"
	return statement, params, databaseSQLResultNames(authorized.Plan.Query, tokens, parts), nil
}

// renderDatabaseSQLTokens are a query's tokens as it runs, one part each.
func renderDatabaseSQLTokens(
	tokens []databaseSQLToken,
	values []any,
	params dbx.Params,
) ([]string, error) {
	parts := make([]string, 0, len(tokens))
	parameterOrdinal := 0
	for _, token := range tokens {
		switch token.Kind {
		case databaseSQLTokenWord:
			if token.Raw != "" {
				parts = append(parts, token.Raw)
			} else {
				parts = append(parts, token.Text)
			}
		case databaseSQLTokenNumber, databaseSQLTokenOperator:
			parts = append(parts, token.Text)
		case databaseSQLTokenQuotedIdentifier:
			parts = append(parts, quoteDatabaseSQLIdentifier(token.Text))
		case databaseSQLTokenString:
			parts = append(parts, "'"+strings.ReplaceAll(databaseSQLComposed(token.Text).(string), "'", "''")+"'")
		case databaseSQLTokenParameter:
			if parameterOrdinal >= len(values) {
				return nil, databaseSQLQueryInvalid()
			}
			key := fmt.Sprintf("sqlParam%d", parameterOrdinal)
			value, ok := normalizeDatabaseSQLParameter(values[parameterOrdinal])
			if !ok {
				return nil, databaseSQLQueryInvalid()
			}
			params[key] = databaseSQLComposed(value)
			parts = append(parts, "{:"+key+"}")
			parameterOrdinal++
		case databaseSQLTokenLeftParen:
			parts = append(parts, "(")
		case databaseSQLTokenRightParen:
			parts = append(parts, ")")
		case databaseSQLTokenComma:
			parts = append(parts, ",")
		case databaseSQLTokenDot:
			parts = append(parts, ".")
		default:
			return nil, databaseSQLQueryInvalid()
		}
	}
	if parameterOrdinal != len(values) {
		return nil, databaseSQLQueryInvalid()
	}
	return parts, nil
}

// quoteDatabaseSQLIdentifier writes a name as only a name: SQLite reads a
// double-quoted word that names no column as text (2026-10-23: a misspelt
// column came back as its own name in every row), a backquoted one never.
func quoteDatabaseSQLIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func normalizeDatabaseSQLParameter(value any) (any, bool) {
	switch typed := value.(type) {
	case nil, bool, string, float64:
		return typed, true
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer, true
		}
		decimal, err := typed.Float64()
		return decimal, err == nil
	default:
		return nil, false
	}
}

func disambiguateDatabaseSQLResultColumns(columns []string) []string {
	counts := map[string]int{}
	result := make([]string, len(columns))
	for index, column := range columns {
		name := strings.TrimSpace(column)
		if name == "" {
			name = fmt.Sprintf("column_%d", index+1)
		}
		folded := strings.ToLower(name)
		counts[folded]++
		if counts[folded] > 1 {
			name = fmt.Sprintf("%s [%d]", name, counts[folded])
		}
		result[index] = name
	}
	return result
}

func normalizeDatabaseSQLResultValue(value any) any {
	if bytes, ok := value.([]byte); ok {
		return string(bytes)
	}
	return value
}

// loadDatabaseSQLQueryResults reads a window of a query's results from the
// one at from on, each row as it was written, its columns in order; next is
// where the next window starts, 0 at the end.
func loadDatabaseSQLQueryResults(
	app core.App,
	jobID string,
	from int,
) ([]any, int, error) {
	stored := []struct {
		Ordinal int    `db:"ordinal"`
		RowJSON string `db:"row_json"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[ordinal]], [[row_json]] FROM {{leafen_database_sql_query_results}}
		WHERE [[job_id]] = {:jobId} AND [[ordinal]] >= {:from} ORDER BY [[ordinal]]
		LIMIT {:limit}
	`).Bind(dbx.Params{"jobId": jobID, "from": from, "limit": databaseSQLResultWindow + 1}).All(&stored); err != nil {
		return nil, 0, err
	}
	next := 0
	if len(stored) > databaseSQLResultWindow {
		next = stored[databaseSQLResultWindow].Ordinal
		stored = stored[:databaseSQLResultWindow]
	}
	results := make([]any, 0, len(stored))
	for _, row := range stored {
		written := json.RawMessage(row.RowJSON)
		if !json.Valid(written) || !strings.HasPrefix(row.RowJSON, "{") {
			return nil, 0, databaseSQLQueryInvalid()
		}
		results = append(results, written)
	}
	return results, next, nil
}

func databaseSQLExecutionError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, errDatabaseSQLQueryResourceLimit) {
		return errDatabaseSQLQueryResourceLimit
	}
	if errors.Is(err, errDatabaseSQLQueryInvalid) {
		return errDatabaseSQLQueryInvalid
	}
	return err
}

// databaseSQLEngineReason is what the engine said of a query that didn't
// run — an unknown column, a misspelt word — without its names for its own.
func databaseSQLEngineReason(err error) string {
	text := strings.TrimPrefix(err.Error(), "SQL logic error: ")
	if at := strings.LastIndex(text, " ("); at > 0 && strings.HasSuffix(text, ")") {
		text = text[:at]
	}
	if text == "" || len(text) > 300 || strings.Contains(text, "leafen_") || strings.Contains(text, "sqlParam") {
		return "check its column names against the data sources' properties (notion-fetch), and its SQL"
	}
	return text
}
