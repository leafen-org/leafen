package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const maxDatabaseQueryBodyBytes = 256 * 1024

func registerDatabaseQueryRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+databaseQueryPath, handleDatabaseQuery).
		Bind(apis.BodyLimit(maxDatabaseQueryBodyBytes))
	registerDatabaseDependencyRoutes(event)
}

func handleDatabaseQuery(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := databaseQueryRequest{}
	if pageID == "" || event.BindBody(&input) != nil {
		return writeDatabaseQueryError(event, errors.New(errorCodeDatabaseQueryInvalid))
	}
	// Read as its asker means it (database_query_asked.go); a linked view reads its source's rows.
	databaseQueryAsAsked(event, user.Id, &input)
	pageID, err := databaseQuerySourcePage(event.App, user.Id, pageID, input)
	if err != nil {
		return writeDatabaseQueryError(event, err)
	}
	canonical, rawCursor, queryHash, err := prepareDatabaseQueryRequest(user.Id, pageID, input)
	if err != nil {
		return writeDatabaseQueryError(event, err)
	}
	turn, err := advanceDatabaseQueryTurnContext(
		event.Request.Context(), event.App, user.Id, pageID, canonical, rawCursor, queryHash,
	)
	if err != nil {
		return writeDatabaseQueryError(event, err)
	}
	// The items a view lists are read for the read log (read_log.go).
	if len(turn.Response.Rows) > 0 {
		listed := make([]contentRead, 0, len(turn.Response.Rows))
		for _, row := range turn.Response.Rows {
			listed = append(listed, contentRead{Kind: "row", ID: row.ID, How: contentReadList})
		}
		recordContentReads(event.App, turn.Authorization.Workspace.WorkspaceID, contentReader{UserID: user.Id, Via: contentReadViaApp}, listed)
	}
	// What names a page closed to this reader (content_graph_reader_mentions.go).
	queryRowsAsRead(event.App, user.Id, turn.Response.Rows)
	return event.JSON(turn.Status, turn.Response)
}

type databaseQueryTurn struct {
	Response      databaseQueryResponse
	Status        int
	Authorization pageAuthorization
	Job           storedDatabaseQueryJob
}

func prepareDatabaseQueryRequest(
	principalID string,
	pageID string,
	input databaseQueryRequest,
) (databaseQueryRequest, string, string, error) {
	rawCursor := ""
	if input.Cursor != nil {
		rawCursor = strings.TrimSpace(*input.Cursor)
	}
	canonical, queryHash, err := canonicalDatabaseQueryRequest(principalID, pageID, input)
	return canonical, rawCursor, queryHash, err
}

// databaseQueryScope is what a principal's query reaches: the page as they
// may read it and the source's unlinked items they hold no role in, which
// name the query apart.
type databaseQueryScope struct {
	authorization pageAuthorization
	hiddenRows    []string
	queryHash     string
	// mentions is whose reading its cells' mentions are read by, which
	// names the query apart too (database_query_reader_mentions.go).
	mentions databaseQueryMentions
}

func loadDatabaseQueryScope(
	app core.App,
	principalID, pageID string,
	canonical databaseQueryRequest,
	queryHash string,
	mentions databaseQueryMentions,
) (databaseQueryScope, error) {
	authorization, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return databaseQueryScope{}, graphNotFoundError()
	}
	if err != nil {
		return databaseQueryScope{}, err
	}
	// An unlinked item the principal has no role in stays out of the query,
	// and a change in which those are starts it anew.
	hiddenRows, err := databaseQueryHiddenRows(app, principalID, canonical.DataSourceID)
	if err != nil {
		return databaseQueryScope{}, err
	}
	queryHash = databaseQueryHashWithHiddenRows(queryHash, hiddenRows)
	return databaseQueryScope{
		authorization: authorization, hiddenRows: hiddenRows,
		queryHash: mentions.withMentionsHash(queryHash), mentions: mentions,
	}, nil
}

// advanceDatabaseQueryJobTurn advances the query's job; a direct query's
// cursor starts one whose results go on after the last row it gave.
func advanceDatabaseQueryJobTurn(
	app core.App,
	scope databaseQueryScope,
	principalID string,
	pageID string,
	canonical databaseQueryRequest,
	rawCursor string,
	cursor databaseQueryCursorState,
	contexts *databaseQueryContextMemo,
) (databaseQueryTurn, error) {
	if err := contexts.sweepExpired(app); err != nil {
		return databaseQueryTurn{}, err
	}
	authorization, queryHash := scope.authorization, scope.queryHash
	hiddenRows := scope.hiddenRows
	job := storedDatabaseQueryJob{}
	var err error
	if rawCursor == "" || cursor.JobID == "" {
		job, err = createDatabaseQueryJob(app, authorization, principalID, pageID, queryHash, canonical, scope.mentions)
		if err != nil {
			return databaseQueryTurn{}, err
		}
		cursor = databaseQueryCursorState{
			Version: 1, Kind: "databaseQuery", JobID: job.ID,
			PrincipalID: principalID, PageID: pageID, QueryHash: queryHash,
			Phase: "build", Progress: job.Scanned, RowID: cursor.RowID,
		}
	} else {
		job, err = loadDatabaseQueryJob(app, cursor)
		if err != nil {
			return databaseQueryTurn{}, err
		}
	}
	job.hiddenRows = hiddenRows
	job.mentions = scope.mentions
	if job.State == databaseQueryStatePending {
		sourceRevision, err := contexts.jobRevision(app, job)
		if err != nil {
			return databaseQueryTurn{}, err
		}
		if sourceRevision != job.SourceRevision {
			return databaseQueryTurn{}, errors.New(errorCodeDatabaseQueryCursorInvalid)
		}
	}
	response, status, err := advanceDatabaseQuery(app, job, cursor, canonical, contexts)
	if err == nil {
		// What the source's property access rules keep from this reader.
		err = redactDatabaseQueryRows(app, principalID, response.DataSourceID, response.Rows)
	}
	return databaseQueryTurn{
		Response: response, Status: status, Authorization: authorization, Job: job,
	}, err
}

func advanceDatabaseQuery(
	app core.App,
	job storedDatabaseQueryJob,
	cursor databaseQueryCursorState,
	input databaseQueryRequest,
	contexts *databaseQueryContextMemo,
) (databaseQueryResponse, int, error) {
	if job.State == databaseQueryStatePending {
		if cursor.Progress > job.Scanned {
			return databaseQueryResponse{}, 0, errors.New(errorCodeDatabaseQueryCursorInvalid)
		}
		if cursor.Progress < job.Scanned {
			cursor.Phase, cursor.Progress, cursor.SortKey = "build", job.Scanned, ""
			next, err := encodeDatabaseQueryCursor(cursor)
			if err != nil {
				return databaseQueryResponse{}, 0, err
			}
			return databaseQueryResponse{
				PageID: job.PageID, DataSourceID: job.DataSourceID,
				State: databaseQueryStatePending, Rows: []databaseQueryRow{},
				NextCursor: &next, HasMore: true, Scanned: job.Scanned,
			}, http.StatusAccepted, nil
		}
		context, err := contexts.load(app, job)
		if err != nil {
			return databaseQueryResponse{}, 0, err
		}
		// A member of the workspace reads people's emails in formulas; a
		// guest reads none.
		if _, err := authorizeWorkspace(app, job.PrincipalID, job.WorkspaceID, permissionWorkspaceRead); err == nil {
			context.People.showEmails()
		} else if !isPermissionDenied(err) {
			return databaseQueryResponse{}, 0, err
		}
		plan, err := compileDatabaseQueryPlan(databaseQueryJobPlanInput(job, input), context)
		if err != nil {
			return databaseQueryResponse{}, 0, err
		}
		if job.DependencySourceID != "" {
			row, err := loadPendingDatabaseQuerySourceRow(app, job)
			if err != nil {
				return databaseQueryResponse{}, 0, err
			}
			job, complete, err := advanceDurableDatabaseQueryRow(
				app, job, row, context, plan,
			)
			if err != nil {
				return databaseQueryResponse{}, 0, err
			}
			if !complete {
				return pendingDatabaseQueryResponse(job, cursor)
			}
			return pendingDatabaseQueryResponse(job, cursor)
		}
		prefilter := planDatabaseQueryPrefilter(plan, context, job.PrincipalID, job.PageID)
		if input.CountOnly && prefilter.exact {
			walk, err := countDatabaseQuerySourceWindow(app, job, input, prefilter)
			if err != nil {
				return databaseQueryResponse{}, 0, err
			}
			walk.countOnly = true
			if job, err = storeDatabaseQueryTurn(app, job, nil, nil, walk.end, walk); err != nil {
				return databaseQueryResponse{}, 0, err
			}
			if !walk.end {
				return pendingDatabaseQueryResponse(job, cursor)
			}
			return readyDatabaseQueryResponse(app, job, cursor, input)
		}
		sourceRows, walk, err := loadDatabaseQuerySourceWindow(app, job, input, prefilter)
		if err != nil {
			return databaseQueryResponse{}, 0, err
		}
		relationShapes := map[string]databaseQueryRelationShape{}
		if databaseQueryComputes(context) {
			relationShapes, err = loadDatabaseQueryRelationShapes(
				app, job.ContentOwnerID, job.PageID, databaseQueryRowIDs(sourceRows),
				databaseQueryNestedRelationIDs(context),
			)
			if err != nil {
				return databaseQueryResponse{}, 0, err
			}
		}
		sourceWindowTruncated := false
		for index, row := range sourceRows {
			if !databaseQueryRequiresDurableDependencies(relationShapes[row.ID], context) {
				continue
			}
			if index > 0 {
				sourceRows = sourceRows[:index]
				sourceWindowTruncated = true
				break
			}
			// database_query_closure_rows.go
			if response, status, done, err := advanceDatabaseQueryClosureRows(app, job, cursor, input, sourceRows, walk, relationShapes, context, plan); err != nil || done {
				return response, status, err
			}
			job, complete, err := advanceDurableDatabaseQueryRow(
				app, job, row, context, plan,
			)
			if err != nil {
				return databaseQueryResponse{}, 0, err
			}
			if !complete {
				return pendingDatabaseQueryResponse(job, cursor)
			}
			return pendingDatabaseQueryResponse(job, cursor)
		}
		if err := hydrateDatabaseQueryRows(
			app, job.ContentOwnerID, job.PageID, sourceRows,
		); err != nil {
			return databaseQueryResponse{}, 0, err
		}
		boundedRows, dependencyIDs, err := boundedDatabaseQueryRows(sourceRows, context)
		if err != nil {
			return databaseQueryResponse{}, 0, err
		}
		dependencyIDs = context.visibleRowIDs(dependencyIDs)
		dependencyValues, err := loadDatabaseQueryValues(
			app, job.ContentOwnerID, context.readablePages(job.PageID), dependencyIDs,
		)
		if err != nil {
			return databaseQueryResponse{}, 0, err
		}
		dependencyRows, err := databaseQueryDependencyRows(
			app, job.ContentOwnerID, context.readablePages(job.PageID), dependencyIDs, dependencyValues,
		)
		if err != nil {
			return databaseQueryResponse{}, 0, err
		}
		context.warmMentions(databaseQueryWindowCells(boundedRows, dependencyValues))
		evaluated, evaluatedRows, err := evaluateDatabaseQueryRows(
			boundedRows, dependencyRows,
			context, plan, job.PrincipalID, job.PageID,
		)
		if err != nil {
			return databaseQueryResponse{}, 0, err
		}
		boundedRows = boundedRows[:evaluatedRows]
		finished := !sourceWindowTruncated && len(boundedRows) == len(sourceRows)
		if !finished {
			walk = databaseQueryWalk{}
		}
		complete := finished && walk.end
		job, err = storeDatabaseQueryTurn(app, job, boundedRows, evaluated, complete, walk)
		if err != nil {
			return databaseQueryResponse{}, 0, err
		}
		if !complete {
			return pendingDatabaseQueryResponse(job, cursor)
		}
	}

	return readyDatabaseQueryResponse(app, job, cursor, input)
}

// readyDatabaseQueryResponse is a page of a finished query's rows.
func readyDatabaseQueryResponse(
	app core.App,
	job storedDatabaseQueryJob,
	cursor databaseQueryCursorState,
	input databaseQueryRequest,
) (databaseQueryResponse, int, error) {
	// A building query's cursor names a row only when the query was answered
	// directly before: its results go on after the last row it gave.
	afterRowID := cursor.RowID
	rows, rowID, hasMore, err := loadDatabaseQueryResultRows(app, job.ID, afterRowID, input.Limit)
	if err != nil {
		return databaseQueryResponse{}, 0, err
	}
	total := job.ResultCount
	response := databaseQueryResponse{
		PageID: job.PageID, DataSourceID: job.DataSourceID,
		State: databaseQueryStateReady, Rows: rows, HasMore: hasMore, Scanned: job.Scanned, Total: &total,
	}
	if hasMore {
		// The cursor names the page's last row, not its sort key: a key is
		// as long as the text it sorts by.
		cursor.Phase, cursor.SortKey, cursor.RowID = "result", "", rowID
		next, err := encodeDatabaseQueryCursor(cursor)
		if err != nil {
			return databaseQueryResponse{}, 0, err
		}
		response.NextCursor = &next
	}
	return response, http.StatusOK, nil
}

func advanceDurableDatabaseQueryRow(
	app core.App,
	job storedDatabaseQueryJob,
	row storedDatabaseQueryRow,
	context databaseQueryContext,
	plan databaseQueryPlan,
) (storedDatabaseQueryJob, bool, error) {
	context.FormulaTime = plan.formulaTime(context.FormulaTime.Language)
	job, err := initializeDatabaseQueryDependencies(app, job, row)
	if err != nil {
		return job, false, err
	}
	switch job.DependencyPhase {
	case databaseQueryDependencyPhaseAdmission:
		job, err = advanceDatabaseQueryDependencyAdmission(app, job, row, context)
		return job, false, err
	case databaseQueryDependencyPhaseExpansion:
		// A job an earlier version saved while it walked every relation goes
		// on to evaluation, which reads each relation it needs.
		job.DependencyPhase = databaseQueryDependencyPhaseEvaluation
		return job, false, persistDatabaseQueryDependencyProgress(app, job)
	case databaseQueryDependencyPhaseEvaluation:
		var evaluated *databaseQueryEvaluatedRow
		job, evaluated, err = advanceDatabaseQueryDependencyEvaluation(
			app, job, row, context, plan,
		)
		if err != nil || evaluated == nil {
			return job, false, err
		}
		rows := []databaseQueryEvaluatedRow{}
		if evaluated.Row.ID != "" {
			rows = append(rows, *evaluated)
		}
		job, err = storeDatabaseQueryTurn(
			app, job, []storedDatabaseQueryRow{row}, rows, false, databaseQueryWalk{},
		)
		if err != nil {
			return job, false, err
		}
		job.DependencyPhase = databaseQueryDependencyPhaseCleanup
		if err := persistDatabaseQueryDependencyProgress(app, job); err != nil {
			return job, false, err
		}
		return job, false, nil
	case databaseQueryDependencyPhaseCleanup, databaseQueryDependencyPhaseReset:
		reset := job.DependencyPhase == databaseQueryDependencyPhaseReset
		complete, err := clearDatabaseQueryDependencies(app, job)
		if err != nil || !complete {
			return job, false, err
		}
		job.DependencySourceID, job.DependencySourcePosition, job.DependencyPhase = "", "", ""
		return job, !reset, nil
	default:
		return job, false, errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
}

func pendingDatabaseQueryResponse(
	job storedDatabaseQueryJob,
	cursor databaseQueryCursorState,
) (databaseQueryResponse, int, error) {
	cursor.Phase, cursor.Progress, cursor.SortKey = "build", job.Scanned, ""
	next, err := encodeDatabaseQueryCursor(cursor)
	if err != nil {
		return databaseQueryResponse{}, 0, err
	}
	return databaseQueryResponse{
		PageID: job.PageID, DataSourceID: job.DataSourceID,
		State: databaseQueryStatePending, Rows: []databaseQueryRow{},
		NextCursor: &next, HasMore: true, Scanned: job.Scanned,
	}, http.StatusAccepted, nil
}

func writeDatabaseQueryError(event *core.RequestEvent, err error) error {
	status, code, message := classifyDatabaseQueryError(err)
	return apiErrorWithCause(
		event, status, code, message, "database-query", "database-query.advance", code, err,
	)
}

func classifyDatabaseQueryError(err error) (int, string, string) {
	status := http.StatusInternalServerError
	code := errorCodeDatabaseQueryFailed
	message := "Couldn't load the database."
	if isSQLiteBusyError(err) {
		status, code, message = http.StatusServiceUnavailable, errorCodeDatabaseQueryDatabaseBusy,
			"The database is busy. Retry the same query shortly."
	} else if isSQLiteStorageUnavailableError(err) {
		status, code, message = http.StatusServiceUnavailable, errorCodeDatabaseQueryStorageUnavailable,
			"Server storage is unavailable. Check the storage state, then retry the same query."
	} else {
		switch err.Error() {
		case errorCodePageNotFound:
			status, code, message = http.StatusNotFound, errorCodePageNotFound, "Page not found."
		case errorCodeDatabaseQueryNotFound:
			status, code, message = http.StatusNotFound, errorCodeDatabaseQueryNotFound, "Database not found."
		case errorCodeDatabaseQueryInvalid:
			status, code, message = http.StatusBadRequest, errorCodeDatabaseQueryInvalid, "Check the database query conditions."
		case errorCodeDatabaseQueryCursorInvalid:
			status, code, message = http.StatusBadRequest, errorCodeDatabaseQueryCursorInvalid, "Check the database query position."
		case errorCodeDatabaseQueryPropertyHidden:
			status, code, message = http.StatusForbidden, errorCodeDatabaseQueryPropertyHidden,
				"This view filters, sorts or groups by a property you can't see."
		case errorCodeDatabaseQueryDependencyLimit:
			status, code, message = http.StatusUnprocessableEntity, errorCodeDatabaseQueryDependencyLimit, "Reduce the one-row relation range of the computed property."
		}
	}
	return status, code, message
}

// databaseQueryHiddenRows are the data source's unlinked items the
// principal has no role in, and the sub-items under them, which its views
// leave out as Notion's do (seen 2026-10-15: a guest's table shows neither,
// though the sub-item's page still opens for them).
func databaseQueryHiddenRows(app core.App, principalID, dataSourceID string) ([]string, error) {
	access, err := loadDatabaseRowAccess(app, principalID, []string{dataSourceID})
	if err != nil || access == nil {
		return nil, err
	}
	unlinked := []string{}
	for rowID := range access.roles {
		if access.hides(rowID) {
			unlinked = append(unlinked, rowID)
		}
	}
	if len(unlinked) == 0 {
		return []string{}, nil
	}
	encoded, err := json.Marshal(unlinked)
	if err != nil {
		return nil, err
	}
	hidden := []string{}
	err = app.DB().NewQuery(`
		WITH RECURSIVE [[under]]([[id]]) AS (
			SELECT [[value]] FROM json_each({:unlinked})
			UNION
			SELECT [[child]].[[id]] FROM {{leafen_database_rows}} AS [[child]]
			JOIN [[under]] ON [[child]].[[subitem_parent_id]] = [[under]].[[id]]
			WHERE ` + databaseSourceRowsSQL("[[child]]", "{:dataSourceId}") + `
		)
		SELECT [[id]] FROM [[under]] ORDER BY [[id]]
	`).Bind(dbx.Params{"unlinked": string(encoded), "dataSourceId": dataSourceID}).Column(&hidden)
	return hidden, err
}

// databaseQueryHashWithHiddenRows names a query apart for each set of
// hidden items, so a finished one is never reused across a change in it.
func databaseQueryHashWithHiddenRows(queryHash string, hidden []string) string {
	if len(hidden) == 0 {
		return queryHash
	}
	digest := sha256.Sum256([]byte(queryHash + "\x00hidden\x00" + strings.Join(hidden, "\x00")))
	return hex.EncodeToString(digest[:])
}
