package app

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const databaseQueryLifetime = 24 * time.Hour

type storedDatabaseQueryJob struct {
	ID                           string `db:"id"`
	PrincipalID                  string `db:"principal_id"`
	ContentOwnerID               string `db:"content_owner_id"`
	WorkspaceID                  string `db:"workspace_id"`
	PageID                       string `db:"page_id"`
	DataSourceID                 string `db:"data_source_id"`
	ViewID                       string `db:"view_id"`
	QueryHash                    string `db:"query_hash"`
	SourceRevision               int    `db:"source_revision"`
	RequestJSON                  string `db:"request_json"`
	State                        string `db:"state"`
	SourcePosition               string `db:"source_position"`
	SourceID                     string `db:"source_id"`
	DependencySourceID           string `db:"dependency_source_id"`
	DependencySourcePosition     string `db:"dependency_source_position"`
	DependencyPhase              string `db:"dependency_phase"`
	DependencyAdmissionProperty  int    `db:"dependency_admission_property"`
	DependencyAdmissionCursor    int    `db:"dependency_admission_cursor"`
	DependencyNextOrdinal        int    `db:"dependency_next_ordinal"`
	DependencyEvaluationProperty int    `db:"dependency_evaluation_property"`
	DependencyEvaluationCursor   int    `db:"dependency_evaluation_cursor"`
	DependencyEvaluationJSON     string `db:"dependency_evaluation_json"`
	Scanned                      int    `db:"scanned"`
	ResultCount                  int    `db:"result_count"`
	CreatedAt                    int64  `db:"created_at"`
	ExpiresAt                    int64  `db:"expires_at"`
	// hiddenRows are the source's unlinked items the principal has no role
	// in (database_row_unlinked_access.go): the query never reads them.
	hiddenRows []string `db:"-"`
	// mentions is whose reading the query reads its cells' mentions and its
	// related pages' rows by (database_query_reader_mentions.go); left out,
	// the principal's own.
	mentions databaseQueryMentions `db:"-"`
}

type storedDatabaseQueryProperty struct {
	ID           string `db:"id"`
	DataSourceID string `db:"data_source_id"`
	Type         string `db:"type"`
	Name         string `db:"name"`
	Position     string `db:"position"`
	Config       string `db:"config"`
}

type storedDatabaseQueryRow struct {
	ID                string                           `json:"id" db:"id"`
	Position          string                           `json:"position" db:"position"`
	SubitemParentID   string                           `json:"subitemParentId" db:"subitem_parent_id"`
	SubitemChildCount int                              `json:"subitemChildCount" db:"subitem_child_count"`
	Values            map[string]graphRowValue         `json:"values"`
	Computed          map[string]databaseComputedValue `json:"computed"`
	// Created and Updated are the row page's times, for timestamp filters and sorts.
	Created int64 `json:"-" db:"created"`
	// RowCreated is the row's own created time, which the source's order by
	// created time keeps an index of (database_query_linked.go).
	RowCreated int64 `json:"-" db:"row_created"`
	Updated    int64 `json:"-" db:"updated"`
	// CreatedBy and LastEditedBy are the row page's people, for formulas.
	CreatedBy    string `json:"-" db:"created_by"`
	LastEditedBy string `json:"-" db:"last_edited_by"`
}

type storedDatabaseQueryValue struct {
	RowID      string `db:"row_id"`
	PropertyID string `db:"property_id"`
	Type       string `db:"type"`
	Value      string `db:"value"`
	Spans      string `db:"spans"`
}

func loadDatabaseQueryContext(
	app core.App,
	job storedDatabaseQueryJob,
) (databaseQueryContext, error) {
	context := databaseQueryContext{
		PropertiesByID:   make(map[string]storedDatabaseQueryProperty),
		PropertiesByName: make(map[string]storedDatabaseQueryProperty),
		People:           &databaseFormulaPeople{app: app, names: map[string]string{}, workspaceID: job.WorkspaceID, request: job.mentions.request},
		DataSourceID:     job.DataSourceID,
		Sprints:          newDatabaseRelativeSprints(app),
		Mentions:         job.mentions.reading(app, job.PrincipalID),
	}
	access, err := newDatabasePropertyAccessReader(app, job.PrincipalID, []string{job.DataSourceID})
	if err != nil {
		return context, err
	}
	context.Access = access
	// A data source in the trash is read only by a reading that reads one
	// (databaseQueryMentions.trash).
	shown := databaseSourceNotTrashedSQL("[[source]].[[id]]")
	if job.mentions.trash {
		shown = "TRUE"
	}
	var sources int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[id]] = {:id} AND [[source]].[[owner]] = {:owner}
			AND [[source]].[[page_id]] = {:pageId} AND ` + shown + `
	`).Bind(dbx.Params{
		"id": job.DataSourceID, "owner": job.ContentOwnerID, "pageId": job.PageID,
	}).Row(&sources); err != nil {
		return context, err
	}
	if sources != 1 {
		return context, errors.New(errorCodeDatabaseQueryNotFound)
	}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + job.mentions.computedPropertiesSQL() + `
		ORDER BY [[position]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": job.ContentOwnerID, "pageId": job.PageID,
		"dataSourceId": job.DataSourceID, "limit": maxDatabaseQueryProperties + 1,
	}).All(&context.Properties); err != nil {
		return context, err
	}
	deleted := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[trash]].[[property_id]] FROM {{leafen_database_property_trash}} AS [[trash]]
		JOIN {{leafen_database_properties}} AS [[property]] ON [[property]].[[id]] = [[trash]].[[property_id]]
		WHERE [[property]].[[page_id]] = {:pageId} AND [[property]].[[data_source_id]] = {:dataSourceId}
	`).Bind(dbx.Params{"pageId": job.PageID, "dataSourceId": job.DataSourceID}).Column(&deleted); err != nil {
		return context, err
	}
	context.DeletedPropertyIDs = make(map[string]struct{}, len(deleted))
	for _, propertyID := range deleted {
		context.DeletedPropertyIDs[propertyID] = struct{}{}
	}
	if len(context.Properties) > maxDatabaseQueryProperties {
		return context, errors.New(errorCodeDatabaseQueryDependencyLimit)
	}
	for _, property := range context.Properties {
		context.PropertiesByID[property.ID] = property
		if _, exists := context.PropertiesByName[property.Name]; !exists {
			context.PropertiesByName[property.Name] = property
		}
	}
	if err := addDatabaseQueryRelatedSources(app, job, &context); err != nil {
		return context, err
	}
	addDatabaseQueryRowTimestampProperties(&context, job.DataSourceID)
	if job.ViewID == "" {
		order, err := loadDatabaseNaturalOrder(app, job, "")
		context.NaturalOrder = order
		return context, err
	}
	// A view of this source wherever it sits: a linked view's page was read
	// when the request named it (databaseQuerySourcePage), and its "현재
	// 페이지" values stand for that page.
	view := struct {
		PageID string `db:"page_id"`
		Config string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], [[config]] FROM {{leafen_database_views}}
		WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[data_source_id]] = {:dataSourceId}
	`).Bind(dbx.Params{
		"id": job.ViewID, "owner": job.ContentOwnerID, "dataSourceId": job.DataSourceID,
	}).One(&view); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return context, errors.New(errorCodeDatabaseQueryNotFound)
		}
		return context, err
	}
	context.ViewConfig = json.RawMessage(databaseViewConfigOnPage(view.Config, view.PageID))
	context.NaturalOrder, err = loadDatabaseNaturalOrder(app, job, view.Config)
	return context, err
}

type databaseQueryCursorState struct {
	Version     int    `json:"v"`
	Kind        string `json:"kind"`
	JobID       string `json:"jobId"`
	PrincipalID string `json:"principalId"`
	PageID      string `json:"pageId"`
	QueryHash   string `json:"queryHash"`
	Phase       string `json:"phase"`
	Progress    int    `json:"progress"`
	SortKey     string `json:"sortKey,omitempty"`
	RowID       string `json:"rowId,omitempty"`
}

// createDatabaseQueryJob finds or makes the query's job, its revision read
// by the query's reading, as each of its turns reads it. A query naming no
// time is judged by the day it runs on in its time zone, so a job made on an
// earlier day is made anew: its relative dates ("today", "this week",
// "one_week_ago") read that day (2026-11-11: an API query reused one for up
// to 24 hours, past midnight), and each turn of a job judges by the time it
// was made (databaseQueryJobPlanInput).
func createDatabaseQueryJob(
	app core.App,
	authorization pageAuthorization,
	principalID, pageID, queryHash string,
	input databaseQueryRequest,
	mentions databaseQueryMentions,
) (storedDatabaseQueryJob, error) {
	now := time.Now().UTC()
	requestJSON, err := json.Marshal(input)
	if err != nil {
		return storedDatabaseQueryJob{}, err
	}
	job := storedDatabaseQueryJob{
		ID: core.GenerateDefaultRandomId(), PrincipalID: principalID,
		ContentOwnerID: authorization.Workspace.OwnerID,
		WorkspaceID:    authorization.Workspace.WorkspaceID,
		PageID:         pageID, DataSourceID: input.DataSourceID, QueryHash: queryHash,
		State: databaseQueryStatePending, CreatedAt: now.UnixMilli(),
		ExpiresAt: now.Add(databaseQueryLifetime).UnixMilli(), mentions: mentions,
	}
	madeBefore, err := databaseQueryDayStart(input, now)
	if err != nil {
		return storedDatabaseQueryJob{}, err
	}
	job.SourceRevision, err = databaseQueryJobRevision(app, job)
	if err != nil {
		return storedDatabaseQueryJob{}, err
	}
	if input.ViewID != nil {
		job.ViewID = *input.ViewID
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_query_jobs}}
		WHERE [[principal_id]] = {:principalId} AND [[page_id]] = {:pageId}
			AND [[query_hash]] = {:queryHash} AND [[source_revision]] = {:sourceRevision}
			AND ([[expires_at]] < {:now} OR [[created_at]] < {:madeBefore})
	`).Bind(dbx.Params{
		"principalId": job.PrincipalID, "pageId": job.PageID, "queryHash": job.QueryHash,
		"sourceRevision": job.SourceRevision, "now": now.UnixMilli(), "madeBefore": madeBefore,
	}).Execute(); err != nil {
		return storedDatabaseQueryJob{}, err
	}
	existing := storedDatabaseQueryJob{}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[principal_id]], [[content_owner_id]], [[workspace_id]], [[page_id]],
			[[data_source_id]], [[view_id]], [[query_hash]], [[source_revision]], [[request_json]],
			[[state]], [[source_position]], [[source_id]], [[dependency_source_id]],
			[[dependency_source_position]], [[dependency_phase]],
			[[dependency_admission_property]], [[dependency_admission_cursor]],
			[[dependency_next_ordinal]], [[dependency_evaluation_property]],
			[[dependency_evaluation_cursor]], [[dependency_evaluation_json]],
			[[scanned]], [[result_count]], [[created_at]], [[expires_at]]
		FROM {{leafen_database_query_jobs}}
		WHERE [[principal_id]] = {:principalId} AND [[page_id]] = {:pageId}
			AND [[query_hash]] = {:queryHash} AND [[source_revision]] = {:sourceRevision}
			AND [[expires_at]] >= {:now}
	`).Bind(dbx.Params{
		"principalId": job.PrincipalID, "pageId": job.PageID, "queryHash": job.QueryHash,
		"sourceRevision": job.SourceRevision, "now": now.UnixMilli(),
	}).One(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return storedDatabaseQueryJob{}, err
	}
	_, err = app.DB().Insert(databaseQueryJobsTable, dbx.Params{
		"id": job.ID, "principal_id": job.PrincipalID, "content_owner_id": job.ContentOwnerID,
		"workspace_id": job.WorkspaceID, "page_id": job.PageID,
		"data_source_id": job.DataSourceID, "view_id": job.ViewID,
		"query_hash": job.QueryHash, "source_revision": job.SourceRevision,
		"request_json": string(requestJSON),
		"state":        job.State, "source_position": "", "source_id": "",
		"scanned": 0, "result_count": 0, "created_at": now.UnixMilli(),
		"updated_at": now.UnixMilli(), "expires_at": job.ExpiresAt,
	}).Execute()
	return job, err
}

// databaseQueryDayStart is when the day a query runs on began in its time
// zone, before which a job of it is not reused; 0 for a query naming its
// time, whose hash names it.
func databaseQueryDayStart(input databaseQueryRequest, now time.Time) (int64, error) {
	if input.ReferenceTime != nil {
		return 0, nil
	}
	location, err := time.LoadLocation(input.TimeZone)
	if err != nil {
		return 0, errors.New(errorCodeDatabaseQueryInvalid)
	}
	local := now.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).UnixMilli(), nil
}

// databaseQueryJobPlanInput is the request a job's turn compiles its plan
// from: one naming no time is judged at the time the job was made, every
// turn of it, so a build going past midnight judges each row by one day.
func databaseQueryJobPlanInput(job storedDatabaseQueryJob, input databaseQueryRequest) databaseQueryRequest {
	if input.ReferenceTime == nil && job.CreatedAt > 0 {
		at := time.UnixMilli(job.CreatedAt).UTC().Format(time.RFC3339Nano)
		input.ReferenceTime = &at
	}
	return input
}

func databaseQuerySourceRevision(app core.App, owner, pageID string) (int, error) {
	var revision int
	err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[cursor]]), 0) FROM {{leafen_graph_mutations}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID}).Row(&revision)
	return revision, err
}

func loadDatabaseQueryJob(
	app core.App,
	cursor databaseQueryCursorState,
) (storedDatabaseQueryJob, error) {
	job := storedDatabaseQueryJob{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[principal_id]], [[content_owner_id]], [[workspace_id]], [[page_id]],
			[[data_source_id]], [[view_id]], [[query_hash]], [[source_revision]], [[request_json]], [[state]],
			[[source_position]], [[source_id]], [[dependency_source_id]],
			[[dependency_source_position]], [[dependency_phase]],
			[[dependency_admission_property]], [[dependency_admission_cursor]],
			[[dependency_next_ordinal]], [[dependency_evaluation_property]],
			[[dependency_evaluation_cursor]], [[dependency_evaluation_json]],
			[[scanned]], [[result_count]], [[created_at]], [[expires_at]]
		FROM {{leafen_database_query_jobs}}
		WHERE [[id]] = {:id} AND [[principal_id]] = {:principalId}
			AND [[page_id]] = {:pageId} AND [[query_hash]] = {:queryHash}
			AND [[expires_at]] >= {:now}
	`).Bind(dbx.Params{
		"id": cursor.JobID, "principalId": cursor.PrincipalID,
		"pageId": cursor.PageID, "queryHash": cursor.QueryHash,
		"now": time.Now().UTC().UnixMilli(),
	}).One(&job)
	if errors.Is(err, sql.ErrNoRows) {
		return storedDatabaseQueryJob{}, errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
	return job, err
}

func loadDatabaseQuerySourceRowHeaders(
	app core.App,
	job storedDatabaseQueryJob,
	input databaseQueryRequest,
) ([]storedDatabaseQueryRow, error) {
	rows := []storedDatabaseQueryRow{}
	from, params := databaseQuerySourceScope(job, input)
	params["limit"] = databaseQuerySourceRowsPerTurn
	err := app.DB().NewQuery(databaseQuerySourceHeadersSQL + from + `
		ORDER BY [[row]].[[position]], [[row]].[[id]]
		LIMIT {:limit}
	`).Bind(params).All(&rows)
	if err != nil || len(rows) == 0 {
		return rows, err
	}
	for index := range rows {
		rows[index].Values = map[string]graphRowValue{}
		rows[index].Computed = map[string]databaseComputedValue{}
	}
	return rows, nil
}

func hydrateDatabaseQueryRows(
	app core.App,
	owner, pageID string,
	rows []storedDatabaseQueryRow,
) error {
	values, err := loadDatabaseQueryValues(app, owner, []string{pageID}, databaseQueryRowIDs(rows))
	if err != nil {
		return err
	}
	for index := range rows {
		rows[index].Values = values[rows[index].ID]
	}
	return nil
}

func loadDatabaseQueryValues(
	app core.App,
	owner string,
	pages []string,
	rowIDs []string,
) (map[string]map[string]graphRowValue, error) {
	result := make(map[string]map[string]graphRowValue, len(rowIDs))
	if len(rowIDs) == 0 {
		return result, nil
	}
	params := dbx.Params{"owner": owner, "pages": databaseQueryPagesJSON(pages)}
	placeholders := make([]string, len(rowIDs))
	for index, rowID := range rowIDs {
		key := fmt.Sprintf("row%d", index)
		params[key] = rowID
		placeholders[index] = "{:" + key + "}"
		result[rowID] = map[string]graphRowValue{}
	}
	stored := []storedDatabaseQueryValue{}
	err := app.DB().NewQuery(`
		SELECT [[row_id]], [[property_id]], [[type]], [[value]], [[spans]]
		FROM {{leafen_database_row_values}}
		WHERE [[owner]] = {:owner} AND [[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
			AND [[row_id]] IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY [[row_id]], [[property_id]]
	`).Bind(params).All(&stored)
	if err != nil {
		return nil, err
	}
	for _, value := range stored {
		result[value.RowID][value.PropertyID] = graphRowValue{
			Type: value.Type, Value: json.RawMessage(value.Value), Spans: databaseStoredSpans(value.Spans),
		}
	}
	return result, nil
}

func storeDatabaseQueryTurn(
	app core.App,
	job storedDatabaseQueryJob,
	sourceRows []storedDatabaseQueryRow,
	rows []databaseQueryEvaluatedRow,
	complete bool,
	walk databaseQueryWalk,
) (storedDatabaseQueryJob, error) {
	for _, row := range rows {
		encoded, err := json.Marshal(row.Row)
		if err != nil {
			return job, err
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_query_results}} ([[job_id]], [[row_id]], [[sort_key]], [[row_json]])
			VALUES ({:jobId}, {:rowId}, {:sortKey}, {:rowJSON})
			ON CONFLICT ([[job_id]], [[row_id]]) DO UPDATE SET
				[[sort_key]] = excluded.[[sort_key]], [[row_json]] = excluded.[[row_json]]
		`).Bind(dbx.Params{
			"jobId": job.ID, "rowId": row.Row.ID,
			"sortKey": row.SortKey, "rowJSON": string(encoded),
		}).Execute(); err != nil {
			return job, err
		}
	}
	switch {
	case walk.set:
		job.SourcePosition, job.SourceID = walk.position, walk.id
		job.Scanned += walk.count
	case len(sourceRows) > 0:
		job.SourcePosition = sourceRows[len(sourceRows)-1].Position
		job.SourceID = sourceRows[len(sourceRows)-1].ID
		job.Scanned += len(sourceRows)
	}
	job.ResultCount += len(rows) + walk.counted
	if complete {
		job.State = databaseQueryStateReady
	}
	// A row evaluated again replaces its result, so the results say how many
	// a query found that kept its rows.
	if complete && !walk.countOnly {
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_query_results}} WHERE [[job_id]] = {:jobId}`).
			Bind(dbx.Params{"jobId": job.ID}).Row(&job.ResultCount); err != nil {
			return job, err
		}
	}
	now := time.Now().UTC()
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_query_jobs}}
		SET [[state]] = {:state}, [[source_position]] = {:position}, [[source_id]] = {:rowId},
			[[scanned]] = {:scanned}, [[result_count]] = {:resultCount},
			[[updated_at]] = {:now}, [[expires_at]] = {:expiresAt}
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{
		"state": job.State, "position": job.SourcePosition, "rowId": job.SourceID,
		"scanned": job.Scanned, "resultCount": job.ResultCount, "now": now.UnixMilli(),
		"expiresAt": now.Add(databaseQueryLifetime).UnixMilli(), "id": job.ID,
	}).Execute()
	return job, err
}

// databaseQueryResultPageBytes is how much of its rows a page of a finished
// query's results holds: a page ends early past it, always with a row, and
// its cursor goes on from its last row (a formula's value may be 10,000,000
// characters).
const databaseQueryResultPageBytes = 32 << 20

// loadDatabaseQueryResultRows reads a page of a finished query's rows after
// the row a cursor named, and the page's last row.
func loadDatabaseQueryResultRows(
	app core.App,
	jobID, afterRowID string,
	limit int,
) ([]databaseQueryRow, string, bool, error) {
	afterSortKey := ""
	if afterRowID != "" {
		err := app.DB().NewQuery(`
			SELECT [[sort_key]] FROM {{leafen_database_query_results}}
			WHERE [[job_id]] = {:jobId} AND [[row_id]] = {:rowId}
		`).Bind(dbx.Params{"jobId": jobID, "rowId": afterRowID}).Row(&afterSortKey)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", false, errors.New(errorCodeDatabaseQueryCursorInvalid)
		}
		if err != nil {
			return nil, "", false, err
		}
	}
	stored, err := app.DB().NewQuery(`
		SELECT [[row_id]], [[row_json]]
		FROM {{leafen_database_query_results}}
		WHERE [[job_id]] = {:jobId}
			AND ([[sort_key]] > {:sortKey} OR ([[sort_key]] = {:sortKey} AND [[row_id]] > {:rowId}))
		ORDER BY [[sort_key]], [[row_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": jobID, "sortKey": afterSortKey, "rowId": afterRowID, "limit": limit + 1,
	}).Rows()
	if err != nil {
		return nil, "", false, err
	}
	defer stored.Close()
	rows := []databaseQueryRow{}
	lastRowID := ""
	held := 0
	for stored.Next() {
		if len(rows) == limit || held >= databaseQueryResultPageBytes {
			return rows, lastRowID, true, nil
		}
		var rowID, encoded string
		if err := stored.Scan(&rowID, &encoded); err != nil {
			return nil, "", false, err
		}
		row := databaseQueryRow{}
		if err := json.Unmarshal([]byte(encoded), &row); err != nil {
			return nil, "", false, err
		}
		rows = append(rows, row)
		lastRowID = rowID
		held += len(encoded)
	}
	if err := stored.Err(); err != nil {
		return nil, "", false, err
	}
	return rows, lastRowID, false, nil
}

func encodeDatabaseQueryCursor(cursor databaseQueryCursorState) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(encoded)
	if len(value) > maxDatabaseQueryCursorBytes {
		return "", errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
	return value, nil
}

func decodeDatabaseQueryCursor(raw string) (databaseQueryCursorState, error) {
	cursor := databaseQueryCursorState{}
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxDatabaseQueryCursorBytes {
		return cursor, errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Version != 1 ||
		cursor.Kind != "databaseQuery" || cursor.PrincipalID == "" || cursor.PageID == "" || cursor.QueryHash == "" ||
		cursor.Progress < 0 {
		return databaseQueryCursorState{}, errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
	// A cursor of a query answered directly or evaluated whole names no job
	// and names its page's last row (database_query_direct.go).
	jobless := cursor.Phase == databaseQueryDirectPhase || cursor.Phase == databaseQueryEvaluatedPhase
	if (cursor.JobID == "") != jobless || (jobless && cursor.RowID == "") ||
		(!jobless && cursor.Phase != "build" && cursor.Phase != "result") {
		return databaseQueryCursorState{}, errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
	return cursor, nil
}

func deleteExpiredDatabaseQueryJobs(app core.App) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_query_jobs}} WHERE [[id]] IN (
			SELECT [[id]] FROM {{leafen_database_query_jobs}}
			WHERE [[expires_at]] < {:now} ORDER BY [[expires_at]], [[id]] LIMIT 32
		)
	`).Bind(dbx.Params{"now": time.Now().UTC().UnixMilli()}).Execute()
	return err
}

func databaseQueryRowIDs(rows []storedDatabaseQueryRow) []string {
	ids := make([]string, len(rows))
	for index := range rows {
		ids[index] = rows[index].ID
	}
	return ids
}
