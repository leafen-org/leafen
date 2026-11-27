package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	databaseSQLQueryStateCapturing = "capturing"
	databaseSQLQueryStateExecuting = "executing"
	databaseSQLQueryStateReady     = "ready"
	databaseSQLQueryStateCancelled = "cancelled"
	databaseSQLQueryStateFailed    = "failed"

	// The snapshot holds as many rows as fit its bytes: the most one
	// query's two seconds and plan budget work through.
	maxDatabaseSQLStagedBytes = 16 * 1024 * 1024
	databaseSQLQueryLifetime  = 24 * time.Hour
)

type storedDatabaseSQLQueryJob struct {
	ID              string `db:"id"`
	PrincipalID     string `db:"principal_id"`
	ContentOwnerID  string `db:"content_owner_id"`
	WorkspaceID     string `db:"workspace_id"`
	QueryHash       string `db:"query_hash"`
	RequestJSON     string `db:"request_json"`
	State           string `db:"state"`
	SourceOrdinal   int    `db:"source_ordinal"`
	Scanned         int    `db:"scanned"`
	StagedBytes     int    `db:"staged_bytes"`
	ResultCount     int    `db:"result_count"`
	CancelRequested bool   `db:"cancel_requested"`
	FailureCode     string `db:"failure_code"`
	CreatedAt       int64  `db:"created_at"`
	UpdatedAt       int64  `db:"updated_at"`
	ExpiresAt       int64  `db:"expires_at"`
}

type storedDatabaseSQLQuerySource struct {
	JobID             string `db:"job_id"`
	Ordinal           int    `db:"ordinal"`
	SourceURL         string `db:"source_url"`
	DataSourceID      string `db:"data_source_id"`
	SourceContainerID string `db:"source_container_id"`
	PageID            string `db:"page_id"`
	SourceRevision    int    `db:"source_revision"`
	ColumnsJSON       string `db:"columns_json"`
	CaptureQueryHash  string `db:"capture_query_hash"`
	CaptureCursor     string `db:"capture_cursor"`
	CapturedRows      int    `db:"captured_rows"`
	Complete          bool   `db:"complete"`
}

type databaseSQLStoredRequest struct {
	Query          string   `json:"query"`
	Params         []any    `json:"params"`
	DataSourceURLs []string `json:"dataSourceUrls"`
}

const databaseSQLQueryJobSelect = `
	SELECT [[id]], [[principal_id]], [[content_owner_id]], [[workspace_id]], [[query_hash]],
		[[request_json]], [[state]], [[source_ordinal]], [[scanned]], [[staged_bytes]],
		[[result_count]], [[cancel_requested]], [[failure_code]], [[created_at]], [[updated_at]], [[expires_at]]
	FROM {{leafen_database_sql_query_jobs}}
`

// admitDatabaseSQLQueryJob is the query's job: the one under way or done,
// or a new one. A finished query asked anew (fresh, without start_cursor)
// is taken again when a source it read has changed since; a later page of
// it reads the results it gave.
func admitDatabaseSQLQueryJob(
	app core.App,
	authorized databaseSQLAuthorizedPlan,
	principalID string,
	fresh bool,
) (storedDatabaseSQLQueryJob, error) {
	if len(authorized.Sources) == 0 || authorized.CanonicalHash == "" || principalID == "" {
		return storedDatabaseSQLQueryJob{}, databaseSQLQueryInvalid()
	}
	now := time.Now().UTC()
	if err := deleteExpiredDatabaseSQLQueryJobs(app, now.UnixMilli()); err != nil {
		return storedDatabaseSQLQueryJob{}, err
	}
	existing := storedDatabaseSQLQueryJob{}
	err := app.DB().NewQuery(databaseSQLQueryJobSelect + `
		WHERE [[principal_id]] = {:principalId} AND [[workspace_id]] = {:workspaceId}
			AND [[query_hash]] = {:queryHash} AND [[expires_at]] >= {:now}
	`).Bind(dbx.Params{
		"principalId": principalID, "workspaceId": authorized.WorkspaceID,
		"queryHash": authorized.CanonicalHash, "now": now.UnixMilli(),
	}).One(&existing)
	if err == nil {
		if existing.ContentOwnerID != authorized.OwnerID {
			return storedDatabaseSQLQueryJob{}, databaseSQLQueryInvalid()
		}
		// A query is judged by the day it runs on: one made on an earlier
		// day (UTC, as SQLite's 'now') is made anew, as a cancelled one is,
		// so DATE('now') never reads an older day (createDatabaseQueryJob).
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
		kept := existing.State != databaseSQLQueryStateCancelled && existing.CreatedAt >= today
		if kept && fresh && (existing.State == databaseSQLQueryStateReady || existing.State == databaseSQLQueryStateFailed) {
			if kept, err = databaseSQLQuerySourcesUnchanged(app, existing.ID, authorized); err != nil {
				return storedDatabaseSQLQueryJob{}, err
			}
		}
		if kept {
			return existing, nil
		}
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_database_sql_query_jobs}} WHERE [[id]] = {:jobId}
		`).Bind(dbx.Params{"jobId": existing.ID}).Execute(); err != nil {
			return storedDatabaseSQLQueryJob{}, err
		}
		err = sql.ErrNoRows
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return storedDatabaseSQLQueryJob{}, err
	}
	requestJSON, err := json.Marshal(databaseSQLStoredRequest{
		Query: authorized.Plan.Query, Params: authorized.Plan.Params,
		DataSourceURLs: authorized.Plan.DataSourceURLs,
	})
	if err != nil {
		return storedDatabaseSQLQueryJob{}, err
	}
	job := storedDatabaseSQLQueryJob{
		ID: core.GenerateDefaultRandomId(), PrincipalID: principalID,
		ContentOwnerID: authorized.OwnerID, WorkspaceID: authorized.WorkspaceID,
		QueryHash: authorized.CanonicalHash, RequestJSON: string(requestJSON),
		State:     databaseSQLQueryStateCapturing,
		CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
		ExpiresAt: now.Add(databaseSQLQueryLifetime).UnixMilli(),
	}
	if _, err := app.DB().Insert(databaseSQLQueryJobsTable, dbx.Params{
		"id": job.ID, "principal_id": job.PrincipalID,
		"content_owner_id": job.ContentOwnerID, "workspace_id": job.WorkspaceID,
		"query_hash": job.QueryHash, "request_json": job.RequestJSON,
		"state": job.State, "source_ordinal": 0, "scanned": 0, "staged_bytes": 0,
		"result_count": 0, "cancel_requested": false, "failure_code": "",
		"created_at": job.CreatedAt, "updated_at": job.UpdatedAt, "expires_at": job.ExpiresAt,
	}).Execute(); err != nil {
		return storedDatabaseSQLQueryJob{}, err
	}
	for ordinal, source := range authorized.Sources {
		columnsJSON, err := json.Marshal(source.Columns)
		if err != nil {
			return storedDatabaseSQLQueryJob{}, err
		}
		_, _, captureHash, err := prepareDatabaseQueryRequest(
			principalID, source.PageID,
			databaseQueryRequest{
				DataSourceID: source.ID, TimeZone: "UTC", Limit: maxDatabaseQueryResultRows,
			},
		)
		if err != nil {
			return storedDatabaseSQLQueryJob{}, err
		}
		if _, err := app.DB().Insert(databaseSQLQuerySourcesTable, dbx.Params{
			"job_id": job.ID, "ordinal": ordinal, "source_url": source.URL,
			"data_source_id": source.ID, "source_container_id": source.SourceContainerID,
			"page_id": source.PageID, "source_revision": source.Revision,
			"columns_json": string(columnsJSON), "capture_query_hash": captureHash,
			"capture_cursor": "", "captured_rows": 0, "complete": false,
		}).Execute(); err != nil {
			return storedDatabaseSQLQueryJob{}, err
		}
	}
	return job, nil
}

func loadDatabaseSQLQuerySources(
	app core.App,
	jobID string,
) ([]storedDatabaseSQLQuerySource, error) {
	sources := []storedDatabaseSQLQuerySource{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]], [[ordinal]], [[source_url]], [[data_source_id]],
			[[source_container_id]], [[page_id]], [[source_revision]], [[columns_json]],
			[[capture_query_hash]], [[capture_cursor]], [[captured_rows]], [[complete]]
		FROM {{leafen_database_sql_query_sources}}
		WHERE [[job_id]] = {:jobId} ORDER BY [[ordinal]]
	`).Bind(dbx.Params{"jobId": jobID}).All(&sources)
	return sources, err
}

func validateDatabaseSQLQueryJobSources(
	job storedDatabaseSQLQueryJob,
	stored []storedDatabaseSQLQuerySource,
	authorized databaseSQLAuthorizedPlan,
) error {
	if job.PrincipalID == "" || job.ContentOwnerID != authorized.OwnerID ||
		job.WorkspaceID != authorized.WorkspaceID || len(stored) != len(authorized.Sources) {
		return errMCPChangedMeanwhile
	}
	for ordinal, source := range stored {
		current := authorized.Sources[ordinal]
		if source.Ordinal != ordinal || source.SourceURL != current.URL ||
			source.DataSourceID != current.ID || source.SourceContainerID != current.SourceContainerID ||
			source.PageID != current.PageID {
			return mcpDenied("a data source the query read changed since its first page: run the query again without start_cursor")
		}
	}
	return nil
}

func advanceDatabaseSQLSourceCapture(
	app core.App,
	authority mcpAuthority,
	job storedDatabaseSQLQueryJob,
	source storedDatabaseSQLQuerySource,
) (storedDatabaseSQLQueryJob, error) {
	columns := []databaseSQLLogicalColumn{}
	if json.Unmarshal([]byte(source.ColumnsJSON), &columns) != nil || len(columns) == 0 {
		return job, databaseSQLQueryInvalid()
	}
	canonical, _, captureHash, err := prepareDatabaseQueryRequest(
		job.PrincipalID, source.PageID,
		databaseQueryRequest{
			DataSourceID: source.DataSourceID, TimeZone: "UTC", Limit: maxDatabaseQueryResultRows,
		},
	)
	if err != nil || captureHash != source.CaptureQueryHash {
		return job, databaseSQLQueryInvalid()
	}
	turn, err := advanceDatabaseQueryTurnWith(
		app, job.PrincipalID, source.PageID, canonical, source.CaptureCursor, captureHash, databaseQueryEitherTurn,
		mcpSQLSnapshotMentions(authority),
	)
	if err != nil {
		return job, err
	}
	now := time.Now().UTC()
	if turn.Response.State == databaseQueryStatePending {
		if turn.Response.NextCursor == nil || *turn.Response.NextCursor == "" {
			return job, databaseSQLQueryInvalid()
		}
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_database_sql_query_sources}}
			SET [[capture_cursor]] = {:cursor}
			WHERE [[job_id]] = {:jobId} AND [[ordinal]] = {:ordinal}
		`).Bind(dbx.Params{
			"cursor": *turn.Response.NextCursor, "jobId": job.ID, "ordinal": source.Ordinal,
		}).Execute()
		if err == nil {
			err = touchDatabaseSQLQueryJob(app, job.ID, now)
		}
		return job, err
	}
	queryContext, err := loadDatabaseQueryContext(app, turn.Job)
	if err != nil {
		return job, err
	}
	// The snapshot holds only what the principal may see of each row, and
	// names only the pages it may open (mcp_reader_mentions.go).
	if err := redactDatabaseQueryRows(app, job.PrincipalID, source.DataSourceID, turn.Response.Rows); err != nil {
		return job, err
	}
	mcpRowsAsRead(app, authority, job.WorkspaceID, turn.Response.Rows)
	facts, err := loadMCPDatabaseRowFacts(app, turn.Response.Rows)
	if err != nil {
		return job, err
	}
	ids, err := loadNotionPropertyIDs(app, source.DataSourceID)
	if err != nil {
		return job, err
	}
	mentions := mcpPropertyMentions(app, authority, job.WorkspaceID)
	rows, encodedBytes, err := projectDatabaseSQLSourceRows(authority, turn.Response.Rows, queryContext, columns, facts, ids, mentions)
	if err != nil {
		return job, err
	}
	if job.StagedBytes+encodedBytes > maxDatabaseSQLStagedBytes {
		return job, errDatabaseSQLQueryResourceLimit
	}
	for _, row := range rows {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_sql_query_rows}}
				([[job_id]], [[source_ordinal]], [[row_id]], [[row_json]])
			VALUES ({:jobId}, {:sourceOrdinal}, {:rowId}, {:rowJSON})
			ON CONFLICT ([[job_id]], [[source_ordinal]], [[row_id]]) DO UPDATE SET
				[[row_json]] = excluded.[[row_json]]
		`).Bind(dbx.Params{
			"jobId": job.ID, "sourceOrdinal": source.Ordinal,
			"rowId": row.ID, "rowJSON": row.JSON,
		}).Execute(); err != nil {
			return job, err
		}
	}
	// An agent's SQL query reads every item it takes in (read_log.go).
	staged := make([]contentRead, 0, len(rows))
	for _, row := range rows {
		staged = append(staged, contentRead{Kind: "row", ID: row.ID, How: contentReadList})
	}
	recordContentReads(app, job.WorkspaceID, contentReader{UserID: job.PrincipalID, Via: contentReadViaMCP}, staged)
	nextCursor := ""
	complete := !turn.Response.HasMore
	if turn.Response.HasMore {
		if turn.Response.NextCursor == nil || *turn.Response.NextCursor == "" {
			return job, databaseSQLQueryInvalid()
		}
		nextCursor = *turn.Response.NextCursor
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_sql_query_sources}}
		SET [[capture_cursor]] = {:cursor}, [[captured_rows]] = [[captured_rows]] + {:captured},
			[[complete]] = {:complete}
		WHERE [[job_id]] = {:jobId} AND [[ordinal]] = {:ordinal}
	`).Bind(dbx.Params{
		"cursor": nextCursor, "captured": len(rows), "complete": complete,
		"jobId": job.ID, "ordinal": source.Ordinal,
	}).Execute(); err != nil {
		return job, err
	}
	job.Scanned += len(rows)
	job.StagedBytes += encodedBytes
	if complete {
		job.SourceOrdinal++
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_sql_query_jobs}}
		SET [[source_ordinal]] = {:sourceOrdinal}, [[scanned]] = {:scanned},
			[[staged_bytes]] = {:stagedBytes}, [[updated_at]] = {:now}, [[expires_at]] = {:expiresAt}
		WHERE [[id]] = {:jobId} AND [[state]] = 'capturing'
	`).Bind(dbx.Params{
		"sourceOrdinal": job.SourceOrdinal, "scanned": job.Scanned,
		"stagedBytes": job.StagedBytes, "now": now.UnixMilli(),
		"expiresAt": now.Add(databaseSQLQueryLifetime).UnixMilli(), "jobId": job.ID,
	}).Execute(); err != nil {
		return job, err
	}
	return job, nil
}

// databaseSQLTimeLayout is a time as SQL reads it, to the second as Notion's
// does (seen 2026-11-05 for createdTime, 2026-11-11 for a date's parts and a
// created or edited time: "2026-11-21 01:00:00Z"), but as the wall-clock time
// of the clock fetch gives it on (person_clock.go), without its offset:
// "2026-11-21 10:00:00". SQLite's date() turns a time with an offset into
// UTC's day, so a filter by the person's day missed what fell before 09:00
// in Seoul and took the next Monday's early hours into a week (2026-11-24);
// a wall-clock time keeps date(), and bounds such as >= '2026-11-23', on
// their day.
const databaseSQLTimeLayout = "2006-01-02 15:04:05"

// databaseSQLTimeText is a time fetch gives (personClockMillisLayout) as SQL
// reads it; a day, or a time on no clock, as it is.
func databaseSQLTimeText(value any) any {
	if text, ok := value.(string); ok {
		if instant, err := time.Parse(personClockMillisLayout, text); err == nil {
			return instant.Format(databaseSQLTimeLayout)
		}
	}
	return value
}

type databaseSQLStagedRow struct {
	ID   string
	JSON string
}

// projectDatabaseSQLSourceRows stages each row as fetch's table has it,
// reading its values where fetch reads a page's (mcpDatabasePropertyParts),
// a title or text holding a mention, a link or an equation as its Markdown
// by what named names (databaseSQLTextValue); given none, as its words.
func projectDatabaseSQLSourceRows(
	authority mcpAuthority,
	rows []databaseQueryRow,
	queryContext databaseQueryContext,
	columns []databaseSQLLogicalColumn,
	facts map[string]mcpDatabaseRowFacts,
	ids notionPropertyIDs,
	named ...notionMarkdownMentions,
) ([]databaseSQLStagedRow, int, error) {
	result := make([]databaseSQLStagedRow, 0, len(rows))
	encodedBytes := 0
	for _, row := range rows {
		projected := make([]any, 0, len(columns))
		parts := map[string]map[string]any{}
		for _, column := range columns {
			if column.Shape != databaseSQLColumnShape {
				return nil, 0, databaseSQLQueryInvalid()
			}
			if column.Synthetic {
				switch column.Name {
				case databaseSQLSyntheticIDColumn:
					projected = append(projected, row.ID)
				case databaseSQLSyntheticCreatedColumn:
					projected = append(projected, time.UnixMilli(facts[row.ID].CreatedAt).In(authority.clock()).Format(databaseSQLTimeLayout))
				default:
					projected = append(projected, mcpPageURL(authority, row.ID))
				}
				continue
			}
			property, exists := queryContext.PropertiesByID[column.PropertyID]
			if !exists {
				return nil, 0, databaseSQLQueryInvalid()
			}
			stored := mcpDatabaseQueryProperty(property, row, ids)
			values, read := parts[column.PropertyID]
			if !read {
				values = mcpDatabasePropertyParts(authority, stored, facts[row.ID])
				parts[column.PropertyID] = values
			}
			value := values[column.Part]
			switch normalizeNotionDatabaseRowPagePropertyType(property.Type) {
			case graphValueTypeTitle, graphValueTypeRichText:
				if len(named) > 0 {
					value = databaseSQLTextValue(stored, value, named[0])
				}
			case graphValueTypeDate, graphValueTypeCreatedTime, graphValueTypeLastEditedTime:
				value = databaseSQLTimeText(value)
			case graphValueTypeCreatedBy, graphValueTypeLastEditedBy:
				// As Notion's rows name them (notionPersonSQLPrefix).
				if address, named := value.(string); named {
					value = notionPersonSQLPrefix + notionPersonReferenceID(address)
				}
			}
			projected = append(projected, databaseSQLComposed(value))
		}
		encoded, err := json.Marshal(projected)
		if err != nil {
			return nil, 0, err
		}
		encodedBytes += len(encoded)
		result = append(result, databaseSQLStagedRow{ID: row.ID, JSON: string(encoded)})
	}
	return result, encodedBytes, nil
}

func touchDatabaseSQLQueryJob(app core.App, jobID string, now time.Time) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_sql_query_jobs}}
		SET [[updated_at]] = {:now}, [[expires_at]] = {:expiresAt}
		WHERE [[id]] = {:jobId}
	`).Bind(dbx.Params{
		"now": now.UnixMilli(), "expiresAt": now.Add(databaseSQLQueryLifetime).UnixMilli(),
		"jobId": jobID,
	}).Execute()
	return err
}

func deleteExpiredDatabaseSQLQueryJobs(app core.App, now int64) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_sql_query_jobs}} WHERE [[id]] IN (
			SELECT [[id]] FROM {{leafen_database_sql_query_jobs}}
			WHERE [[expires_at]] < {:now}
			ORDER BY [[expires_at]], [[id]] LIMIT 32
		)
	`).Bind(dbx.Params{"now": now}).Execute()
	return err
}

func failDatabaseSQLQueryJob(app core.App, jobID, code string) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_sql_query_jobs}}
		SET [[state]] = 'failed', [[failure_code]] = {:code}, [[updated_at]] = {:now}
		WHERE [[id]] = {:jobId} AND [[state]] IN ('capturing','executing')
	`).Bind(dbx.Params{
		"code": code, "now": time.Now().UTC().UnixMilli(), "jobId": jobID,
	}).Execute()
	return err
}

func cancelDatabaseSQLQueryJob(
	app core.App,
	principalID, workspaceID, queryHash string,
) error {
	if principalID == "" || workspaceID == "" || queryHash == "" {
		return nil
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_sql_query_jobs}}
		SET [[state]] = 'cancelled', [[cancel_requested]] = 1, [[updated_at]] = {:now}
		WHERE [[principal_id]] = {:principalId} AND [[workspace_id]] = {:workspaceId}
			AND [[query_hash]] = {:queryHash} AND [[state]] IN ('capturing','executing')
	`).Bind(dbx.Params{
		"now": time.Now().UTC().UnixMilli(), "principalId": principalID,
		"workspaceId": workspaceID, "queryHash": queryHash,
	}).Execute()
	return err
}
