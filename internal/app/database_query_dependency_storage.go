package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	databaseQueryDependencyPhaseAdmission  = "admission"
	databaseQueryDependencyPhaseExpansion  = "expansion"
	databaseQueryDependencyPhaseEvaluation = "evaluation"
	databaseQueryDependencyPhaseCleanup    = "cleanup"
	databaseQueryDependencyPhaseReset      = "reset"

	databaseQueryDependencyKindDirect = "direct"
	databaseQueryDependencyKindMedian = "median"
)

func databaseQueryDependencySyntheticID(jobID, kind, identity string) string {
	digest := sha256.Sum256([]byte(jobID + "\x00" + kind + "\x00" + identity))
	return fmt.Sprintf("leafen-%s-%x", kind, digest[:])
}

func loadDatabaseQueryRowsByID(
	app core.App,
	owner string,
	pages []string,
	rowIDs []string,
) (map[string]storedDatabaseQueryRow, error) {
	return loadDatabaseQueryRowsByIDReach(app, owner, pages, rowIDs, false)
}

// loadDatabaseQuerySourceRowsByID reads rows of the query's own source as
// the durable path works them out (loadDatabaseQueryRowsByID), each counting
// the sub-items its reader sees, as every row the query answers does
// (databaseQueryReaderChildCountSQL).
func loadDatabaseQuerySourceRowsByID(
	app core.App,
	job storedDatabaseQueryJob,
	rowIDs []string,
) (map[string]storedDatabaseQueryRow, error) {
	rows, err := loadDatabaseQueryRowsByID(app, job.ContentOwnerID, []string{job.PageID}, rowIDs)
	if err != nil || len(rows) == 0 {
		return rows, err
	}
	ids, err := json.Marshal(rowIDs)
	if err != nil {
		return nil, err
	}
	hidden := []byte("[]")
	if len(job.hiddenRows) > 0 {
		if hidden, err = json.Marshal(job.hiddenRows); err != nil {
			return nil, err
		}
	}
	counts := []struct {
		ID    string `db:"id"`
		Count int    `db:"subitem_child_count"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[row]].[[id]], ` + databaseQueryReaderChildCountSQL + ` AS [[subitem_child_count]]
		FROM {{leafen_database_rows}} AS [[row]]
		WHERE [[row]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(ids), "hiddenRows": string(hidden)}).All(&counts); err != nil {
		return nil, err
	}
	for _, count := range counts {
		if row, ok := rows[count.ID]; ok {
			row.SubitemChildCount = count.Count
			rows[count.ID] = row
		}
	}
	return rows, nil
}

// loadDatabaseQueryRowsByIDReach is loadDatabaseQueryRowsByID with the rows
// in the trash too when withTrashed: a row page read in the trash computes
// its own formulas and rollups, as Notion's does (2026-11-12: its page
// answered 500 once it was in the trash), from rows out of it.
func loadDatabaseQueryRowsByIDReach(
	app core.App,
	owner string,
	pages []string,
	rowIDs []string,
	withTrashed bool,
) (map[string]storedDatabaseQueryRow, error) {
	result := make(map[string]storedDatabaseQueryRow, len(rowIDs))
	if len(rowIDs) == 0 {
		return result, nil
	}
	params := dbx.Params{"owner": owner, "pages": databaseQueryPagesJSON(pages)}
	placeholders := make([]string, len(rowIDs))
	for index, rowID := range rowIDs {
		key := fmt.Sprintf("row%d", index)
		params[key] = rowID
		placeholders[index] = "{:" + key + "}"
	}
	notTrashed := "AND " + databaseRowNotTrashedSQL("[[row]].[[id]]")
	if withTrashed {
		notTrashed = ""
	}
	rows := []storedDatabaseQueryRow{}
	if err := app.DB().NewQuery(`
		SELECT [[row]].[[id]], [[row]].[[position]], [[row]].[[subitem_parent_id]], [[row]].[[subitem_child_count]],
			COALESCE([[page]].[[created]], [[row]].[[created]]) AS [[created]],
			COALESCE([[page]].[[updated]], [[row]].[[updated]]) AS [[updated]],
			COALESCE([[row_record]].[[created_by]], '') AS [[created_by]],
			COALESCE([[row_record]].[[last_edited_by]], '') AS [[last_edited_by]]
		FROM {{leafen_database_rows}} AS [[row]]
		LEFT JOIN {{leafen_database_row_pages}} AS [[page]] ON [[page]].[[row_id]] = [[row]].[[id]]
		LEFT JOIN {{leafen_pages}} AS [[row_record]] ON [[row_record]].[[id]] = [[row]].[[id]]
		WHERE [[row]].[[owner]] = {:owner} AND [[row]].[[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
			AND [[row]].[[id]] IN (` + strings.Join(placeholders, ",") + `)
			` + notTrashed + `
		ORDER BY [[row]].[[id]]
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	values, err := loadDatabaseQueryValues(app, owner, pages, databaseQueryRowIDs(rows))
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		row.Values = values[row.ID]
		row.Computed = map[string]databaseComputedValue{}
		result[row.ID] = row
	}
	return result, nil
}

// databaseQueryDependencyRows are the related rows a query's formulas and
// rollups read, with their values and their pages' created and edited times
// and people, which a rollup or a formula reads of them as Notion's do.
func databaseQueryDependencyRows(
	app core.App,
	owner string,
	pages []string,
	ids []string,
	values map[string]map[string]graphRowValue,
) (map[string]storedDatabaseQueryRow, error) {
	headers, err := loadDatabaseQueryRowHeadersByID(app, owner, pages, ids)
	if err != nil {
		return nil, err
	}
	rows := make(map[string]storedDatabaseQueryRow, len(ids))
	for _, id := range ids {
		row := headers[id]
		row.ID, row.Values, row.Computed = id, values[id], map[string]databaseComputedValue{}
		rows[id] = row
	}
	return rows, nil
}

func loadDatabaseQueryRowHeadersByID(
	app core.App,
	owner string,
	pages []string,
	rowIDs []string,
) (map[string]storedDatabaseQueryRow, error) {
	result := make(map[string]storedDatabaseQueryRow, len(rowIDs))
	if len(rowIDs) == 0 {
		return result, nil
	}
	params := dbx.Params{"owner": owner, "pages": databaseQueryPagesJSON(pages)}
	placeholders := make([]string, len(rowIDs))
	for index, rowID := range rowIDs {
		key := fmt.Sprintf("row%d", index)
		params[key] = rowID
		placeholders[index] = "{:" + key + "}"
	}
	rows := []storedDatabaseQueryRow{}
	if err := app.DB().NewQuery(`
		SELECT [[row]].[[id]], [[row]].[[position]], [[row]].[[subitem_parent_id]], [[row]].[[subitem_child_count]],
			COALESCE([[page]].[[created]], [[row]].[[created]]) AS [[created]],
			COALESCE([[page]].[[updated]], [[row]].[[updated]]) AS [[updated]],
			COALESCE([[row_record]].[[created_by]], '') AS [[created_by]],
			COALESCE([[row_record]].[[last_edited_by]], '') AS [[last_edited_by]]
		FROM {{leafen_database_rows}} AS [[row]]
		LEFT JOIN {{leafen_database_row_pages}} AS [[page]] ON [[page]].[[row_id]] = [[row]].[[id]]
		LEFT JOIN {{leafen_pages}} AS [[row_record]] ON [[row_record]].[[id]] = [[row]].[[id]]
		WHERE [[row]].[[owner]] = {:owner} AND [[row]].[[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
			AND [[row]].[[id]] IN (` + strings.Join(placeholders, ",") + `)
			AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]") + `
		ORDER BY [[row]].[[id]]
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		row.Values = map[string]graphRowValue{}
		row.Computed = map[string]databaseComputedValue{}
		result[row.ID] = row
	}
	return result, nil
}

// loadDatabaseQueryRollupRelation is what a rollup reads of a relation on a
// related row: the rows it holds, which a filter on the rollup matches by
// their IDs as Notion's does (seen 2026-11-12), and the names it shows of
// them. Computed rows leave their relations out, so the one a rollup reads is
// read on its own.
func loadDatabaseQueryRollupRelation(
	app core.App, owner string, context databaseQueryContext, rowID, propertyID string,
) (any, any, error) {
	raw := ""
	err := app.DB().NewQuery(`
		SELECT [[value]] FROM {{leafen_database_row_values}}
		WHERE [[owner]] = {:owner} AND [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}
	`).Bind(dbx.Params{"owner": owner, "rowId": rowID, "propertyId": propertyID}).Row(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	ids := []string{}
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return nil, nil, nil
	}
	ids = context.visibleRowIDs(ids)
	if len(ids) == 0 {
		return nil, nil, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, nil, err
	}
	pages := []struct {
		ID    string `db:"id"`
		Title string `db:"title"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[title]] FROM {{leafen_pages}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[in_trash]] = FALSE
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&pages); err != nil {
		return nil, nil, err
	}
	titles := make(map[string]string, len(pages))
	for _, page := range pages {
		titles[page.ID] = page.Title
	}
	if err := context.pageTitlesAsRead(app, titles); err != nil {
		return nil, nil, err
	}
	held, names := make([]any, 0, len(ids)), make([]any, 0, len(ids))
	for _, id := range ids {
		title, ok := titles[id]
		if !ok {
			continue
		}
		held = append(held, id)
		if strings.TrimSpace(title) != "" {
			names = append(names, title)
		}
	}
	var rows, shown any
	if len(held) > 0 {
		rows = held
	}
	if len(names) > 0 {
		shown = names
	}
	return rows, shown, nil
}

func loadDatabaseQueryComputedRowsByID(
	app core.App,
	owner string,
	pages []string,
	rowIDs []string,
) (map[string]storedDatabaseQueryRow, error) {
	result, err := loadDatabaseQueryRowHeadersByID(app, owner, pages, rowIDs)
	if err != nil || len(result) == 0 {
		return result, err
	}
	params := dbx.Params{
		"owner": owner, "pages": databaseQueryPagesJSON(pages),
		"relationType": graphValueTypeRelation,
	}
	placeholders := make([]string, len(rowIDs))
	for index, rowID := range rowIDs {
		key := fmt.Sprintf("row%d", index)
		params[key] = rowID
		placeholders[index] = "{:" + key + "}"
	}
	values := []storedDatabaseQueryValue{}
	if err := app.DB().NewQuery(`
		SELECT [[row_id]], [[property_id]], [[type]], [[value]], [[spans]]
		FROM {{leafen_database_row_values}}
		WHERE [[owner]] = {:owner} AND [[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
			AND [[type]] != {:relationType}
			AND [[row_id]] IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY [[row_id]], [[property_id]]
	`).Bind(params).All(&values); err != nil {
		return nil, err
	}
	for _, value := range values {
		row, ok := result[value.RowID]
		if !ok {
			continue
		}
		row.Values[value.PropertyID] = graphRowValue{
			Type: value.Type, Value: json.RawMessage(value.Value), Spans: databaseStoredSpans(value.Spans),
		}
		result[value.RowID] = row
	}
	return result, nil
}

func initializeDatabaseQueryDependencies(
	app core.App,
	job storedDatabaseQueryJob,
	row storedDatabaseQueryRow,
) (storedDatabaseQueryJob, error) {
	if job.DependencySourceID != "" {
		if job.DependencySourceID != row.ID || job.DependencySourcePosition != row.Position {
			return job, errors.New(errorCodeDatabaseQueryCursorInvalid)
		}
		if job.DependencyPhase == "" {
			job.DependencyPhase = databaseQueryDependencyPhaseReset
			return job, persistDatabaseQueryDependencyProgress(app, job)
		}
		return job, nil
	}
	job.DependencySourceID = row.ID
	job.DependencySourcePosition = row.Position
	job.DependencyPhase = databaseQueryDependencyPhaseAdmission
	job.DependencyAdmissionProperty = 0
	job.DependencyAdmissionCursor = 0
	job.DependencyNextOrdinal = 0
	job.DependencyEvaluationProperty = 0
	job.DependencyEvaluationCursor = -1
	job.DependencyEvaluationJSON = "{}"
	return job, persistDatabaseQueryDependencyProgress(app, job)
}

func advanceDatabaseQueryDependencyAdmission(
	app core.App,
	job storedDatabaseQueryJob,
	row storedDatabaseQueryRow,
	context databaseQueryContext,
) (storedDatabaseQueryJob, error) {
	items, propertyCursor, itemCursor, complete, err := databaseQueryRelationWindow(
		app, job.ContentOwnerID, job.PageID, row.ID, context, job.DependencyAdmissionProperty,
		job.DependencyAdmissionCursor, databaseQueryDependencyRowsPerTurn,
	)
	if err != nil {
		return job, err
	}
	// The window's items are written in one statement, each kept or ignored
	// as on its own (2026-11-12: one statement each was a third of the
	// statements a query of 97 rows relating to 100 rows each ran).
	if len(items) > 0 {
		values := make([]string, 0, len(items))
		params := dbx.Params{"jobId": job.ID, "sourceRowId": row.ID}
		for index, item := range items {
			key := strconv.Itoa(index)
			values = append(values, "({:jobId}, {:sourceRowId}, {:d"+key+"}, {:t"+key+"}, {:o"+key+"}, 1, '', 'direct', {:r"+key+"})")
			params["d"+key] = databaseQueryDependencySyntheticID(
				job.ID, databaseQueryDependencyKindDirect,
				fmt.Sprintf("%08d:%08d", item.PropertyIndex, item.Occurrence),
			)
			params["t"+key], params["o"+key], params["r"+key] = item.TargetID, job.DependencyNextOrdinal, item.PropertyID
			job.DependencyNextOrdinal++
		}
		if _, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_database_query_dependency_values}}
				([[job_id]], [[source_row_id]], [[dependency_row_id]], [[target_row_id]],
				 [[ordinal]], [[processed]], [[row_json]], [[kind]], [[relation_property]])
			VALUES ` + strings.Join(values, ", ")).Bind(params).Execute(); err != nil {
			return job, err
		}
	}
	job.DependencyAdmissionProperty = propertyCursor
	job.DependencyAdmissionCursor = itemCursor
	// Evaluation reads the related rows each rollup needs, and through them
	// only the relations their computed values read.
	if complete {
		job.DependencyPhase = databaseQueryDependencyPhaseEvaluation
	}
	return job, persistDatabaseQueryDependencyProgress(app, job)
}

func persistDatabaseQueryDependencyProgress(app core.App, job storedDatabaseQueryJob) error {
	now := time.Now().UTC()
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_query_jobs}}
		SET [[dependency_source_id]] = {:sourceRowId},
			[[dependency_source_position]] = {:sourcePosition}, [[dependency_phase]] = {:phase},
			[[dependency_admission_property]] = {:admissionProperty},
			[[dependency_admission_cursor]] = {:admissionCursor},
			[[dependency_next_ordinal]] = {:nextOrdinal},
			[[dependency_evaluation_property]] = {:evaluationProperty},
			[[dependency_evaluation_cursor]] = {:evaluationCursor},
			[[dependency_evaluation_json]] = {:evaluationJSON},
			[[updated_at]] = {:now}, [[expires_at]] = {:expiresAt}
		WHERE [[id]] = {:jobId}
	`).Bind(dbx.Params{
		"sourceRowId": job.DependencySourceID, "sourcePosition": job.DependencySourcePosition,
		"phase": job.DependencyPhase, "admissionProperty": job.DependencyAdmissionProperty,
		"admissionCursor": job.DependencyAdmissionCursor, "nextOrdinal": job.DependencyNextOrdinal,
		"evaluationProperty": job.DependencyEvaluationProperty,
		"evaluationCursor":   job.DependencyEvaluationCursor,
		"evaluationJSON":     job.DependencyEvaluationJSON, "jobId": job.ID,
		"now": now.UnixMilli(), "expiresAt": now.Add(databaseQueryLifetime).UnixMilli(),
	}).Execute()
	return err
}

func clearDatabaseQueryDependencies(
	app core.App,
	job storedDatabaseQueryJob,
) (bool, error) {
	if job.DependencySourceID == "" {
		return true, nil
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_query_dependency_values}}
		WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_database_query_dependency_values}}
			WHERE [[job_id]] = {:jobId} AND [[source_row_id]] = {:sourceRowId}
			ORDER BY [[dependency_row_id]] LIMIT {:limit}
		)
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourceRowId": job.DependencySourceID,
		"limit": databaseQueryDependencyRowsPerTurn,
	}).Execute(); err != nil {
		return false, err
	}
	var remains int
	if err := app.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{leafen_database_query_dependency_values}}
			WHERE [[job_id]] = {:jobId} AND [[source_row_id]] = {:sourceRowId}
			LIMIT 1
		)
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourceRowId": job.DependencySourceID,
	}).Row(&remains); err != nil {
		return false, err
	}
	if remains != 0 {
		return false, nil
	}
	job.DependencySourceID = ""
	job.DependencySourcePosition = ""
	job.DependencyPhase = ""
	job.DependencyAdmissionProperty = 0
	job.DependencyAdmissionCursor = 0
	job.DependencyNextOrdinal = 0
	job.DependencyEvaluationProperty = 0
	job.DependencyEvaluationCursor = -1
	job.DependencyEvaluationJSON = "{}"
	return true, persistDatabaseQueryDependencyProgress(app, job)
}

func loadPendingDatabaseQuerySourceRow(
	app core.App,
	job storedDatabaseQueryJob,
) (storedDatabaseQueryRow, error) {
	rows, err := loadDatabaseQueryComputedRowsByID(
		app, job.ContentOwnerID, []string{job.PageID}, []string{job.DependencySourceID},
	)
	if err != nil {
		return storedDatabaseQueryRow{}, err
	}
	row, ok := rows[job.DependencySourceID]
	if !ok || row.Position != job.DependencySourcePosition {
		return storedDatabaseQueryRow{}, errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
	return row, nil
}

func databaseQueryHasPendingDependencies(app core.App, job storedDatabaseQueryJob) (bool, error) {
	if job.DependencySourceID == "" {
		return false, nil
	}
	var exists int
	err := app.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{leafen_database_query_dependency_values}}
			WHERE [[job_id]] = {:jobId} AND [[source_row_id]] = {:sourceRowId}
			LIMIT 1
		)
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourceRowId": job.DependencySourceID,
	}).Row(&exists)
	return exists != 0, err
}
