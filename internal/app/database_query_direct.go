package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A query in the source's own order whose conditions SQL decides alone — a
// view without sorts or search, unfiltered or filtered by options, people,
// rows or a checked checkbox (database_query_prefilter.go) — is answered at
// once from the source's rows, in a read snapshot, each page after the last
// row of the one before (2026-10-14 load measurement: every query built a
// job of its own, a hundred rows a call, and built it again after any change
// to the database — on 100,000 rows a first page took 1,001 calls and 20 s,
// for each person and after every write). A sorted query, one with a search
// or conditions only its rows' values decide, and one that only counts are
// evaluated whole in the snapshot when their source has no formulas or
// rollups (database_query_evaluated.go), and build their job as before when
// it has; so does a query in order reaching a row whose formulas or rollups
// take the durable dependency path, going on after the last row it gave.

const databaseQueryDirectPhase = "direct"

// errDatabaseQueryNeedsJob: the query can't be answered directly.
var errDatabaseQueryNeedsJob = errors.New("database query needs its job")

type databaseQueryTurnMode int

const (
	// databaseQueryJobTurn advances the query's job; a direct query's
	// cursor goes on in one.
	databaseQueryJobTurn databaseQueryTurnMode = iota
	// databaseQueryDirectTurn answers directly, writing nothing, or gives
	// errDatabaseQueryNeedsJob.
	databaseQueryDirectTurn
	// databaseQueryEitherTurn answers directly when it can and advances the
	// job otherwise.
	databaseQueryEitherTurn
)

// databaseQueryJobTurnBudget is how long one request goes on with a query's
// job while it is pending. A row with computed values takes a few steps; one
// step a request made a 236-row source 670 requests through the write lane,
// and with others writing it waited its turn each time (2026-11-05: alone
// 3.5-5.9s to 2.6s, four at once 17-21s to 8-12s, on a copy of an import).
const databaseQueryJobTurnBudget = 40 * time.Millisecond

// advanceDatabaseQueryTurn answers a query directly in a read snapshot when
// it can, and otherwise advances its job in a transaction.
func advanceDatabaseQueryTurn(
	app core.App,
	principalID string,
	pageID string,
	canonical databaseQueryRequest,
	rawCursor string,
	queryHash string,
) (databaseQueryTurn, error) {
	return advanceDatabaseQueryTurnAs(context.Background(), app, principalID, pageID, canonical, rawCursor, queryHash, databaseQueryMentions{})
}

// advanceDatabaseQueryTurnContext is advanceDatabaseQueryTurn for the app's
// view of a database, its snapshot waiting for a connection no longer than
// the request: its cells' mentions read as the person's screen reads them
// (database_query_reader_mentions.go).
func advanceDatabaseQueryTurnContext(
	ctx context.Context,
	app core.App,
	principalID string,
	pageID string,
	canonical databaseQueryRequest,
	rawCursor string,
	queryHash string,
) (databaseQueryTurn, error) {
	reading := screenQueryMentions
	reading.request = ctx
	// Its search reads dates in its reader's language, kept apart in the hash:
	// another language's rows are not kept for it.
	if language, err := serverTextLanguageIn(ctx, app, principalID); err == nil {
		reading.key += ":" + language
	}
	return advanceDatabaseQueryTurnAs(ctx, app, principalID, pageID, canonical, rawCursor, queryHash, reading)
}

// advanceDatabaseQueryTurnAs is advanceDatabaseQueryTurn for a request,
// whose cells' mentions are read by the reading given: a person's screen,
// a connection's or an agent's (database_query_reader_mentions.go).
func advanceDatabaseQueryTurnAs(
	ctx context.Context,
	app core.App,
	principalID string,
	pageID string,
	canonical databaseQueryRequest,
	rawCursor string,
	queryHash string,
	mentions databaseQueryMentions,
) (databaseQueryTurn, error) {
	turn := databaseQueryTurn{}
	err := runReadSnapshot(ctx, app, func(readApp core.App) error {
		var err error
		turn, err = advanceDatabaseQueryTurnWith(
			readApp, principalID, pageID, canonical, rawCursor, queryHash, databaseQueryDirectTurn, mentions,
		)
		return err
	})
	if !errors.Is(err, errDatabaseQueryNeedsJob) {
		return turn, err
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		var err error
		turn, err = advanceDatabaseQueryJobTurns(txApp, principalID, pageID, canonical, rawCursor, queryHash, mentions)
		return err
	})
	return turn, err
}

// advanceDatabaseQueryJobTurns advances a pending query's job in the
// caller's transaction for up to the turn budget, every step reading the
// context the first one loaded: the steps write no property, person or order
// it holds (2026-11-06: a 236-row source's 673 steps loaded it each time; kept,
// its first answer took 2.0-3.3s against 2.9-4.1s, the same rows, on copies of
// an import). The API's and the MCP tool's queries take their turns here.
// The steps keep their access decisions as a read scope does (access_memo.go):
// they write the job's own progress alone, nothing a decision reads, and the
// transaction holds the database's one writer meanwhile (2026-11-12: some ten
// steps a request each read a database's access, its workspace's fences and
// the records the checks read again, 7,000 of the 31,000 statements a query
// of 97 rows ran).
func advanceDatabaseQueryJobTurns(
	transaction core.App,
	principalID string,
	pageID string,
	canonical databaseQueryRequest,
	rawCursor string,
	queryHash string,
	mentions databaseQueryMentions,
) (databaseQueryTurn, error) {
	started := time.Now()
	txApp := withAccessMemo(transaction)
	contexts := &databaseQueryContextMemo{}
	cursor := rawCursor
	for {
		turn, err := advanceDatabaseQueryTurnMemo(
			txApp, principalID, pageID, canonical, cursor, queryHash, databaseQueryJobTurn, contexts, mentions,
		)
		if err != nil || turn.Response.State != databaseQueryStatePending || turn.Response.NextCursor == nil ||
			time.Since(started) >= databaseQueryJobTurnBudget {
			return turn, err
		}
		cursor = *turn.Response.NextCursor
	}
}

// databaseQueryContextMemo keeps a query's context for the steps of one
// turn; a nil memo loads it for each. It keeps too what the steps' checks
// read that the request's transaction cannot change meanwhile, as its steps
// write the job's own progress alone (advanceDatabaseQueryJobTurns): the
// query's scope, the job's revision, and expired jobs swept once
// (2026-11-12: some ten steps a request each read them again, 6,000 of the
// 12,000 statements a query of 97 rows ran).
type databaseQueryContextMemo struct {
	key      string
	context  databaseQueryContext
	scopeKey string
	scope    databaseQueryScope
	jobID    string
	revision int
	swept    bool
}

func (memo *databaseQueryContextMemo) loadScope(
	app core.App,
	principalID, pageID string,
	canonical databaseQueryRequest,
	queryHash string,
	mentions databaseQueryMentions,
) (databaseQueryScope, error) {
	if memo == nil {
		return loadDatabaseQueryScope(app, principalID, pageID, canonical, queryHash, mentions)
	}
	key := strings.Join([]string{principalID, pageID, canonical.DataSourceID, queryHash, mentions.key}, "\x00")
	if memo.scopeKey != key {
		scope, err := loadDatabaseQueryScope(app, principalID, pageID, canonical, queryHash, mentions)
		if err != nil {
			return databaseQueryScope{}, err
		}
		memo.scopeKey, memo.scope = key, scope
	}
	return memo.scope, nil
}

func (memo *databaseQueryContextMemo) jobRevision(app core.App, job storedDatabaseQueryJob) (int, error) {
	if memo == nil {
		return databaseQueryJobRevision(app, job)
	}
	if memo.jobID != job.ID {
		revision, err := databaseQueryJobRevision(app, job)
		if err != nil {
			return 0, err
		}
		memo.jobID, memo.revision = job.ID, revision
	}
	return memo.revision, nil
}

func (memo *databaseQueryContextMemo) sweepExpired(app core.App) error {
	if memo != nil && memo.swept {
		return nil
	}
	if err := deleteExpiredDatabaseQueryJobs(app); err != nil {
		return err
	}
	if memo != nil {
		memo.swept = true
	}
	return nil
}

func (memo *databaseQueryContextMemo) load(app core.App, job storedDatabaseQueryJob) (databaseQueryContext, error) {
	if memo == nil {
		return loadDatabaseQueryContext(app, job)
	}
	// What the context is read for (loadDatabaseQueryContext).
	key := strings.Join([]string{job.ContentOwnerID, job.PageID, job.DataSourceID, job.ViewID, job.PrincipalID, job.mentions.key}, "\x00")
	if memo.key != key {
		context, err := loadDatabaseQueryContext(app, job)
		if err != nil {
			return databaseQueryContext{}, err
		}
		// The related values its steps work out are kept for the steps after
		// them in this request (database_query_target_window.go).
		context.TargetValues = newDatabaseQueryTargetValues()
		memo.key, memo.context = key, context
	}
	return memo.context, nil
}

// advanceDatabaseQueryTurnTransaction advances the query's job, as the
// automations that act on a frozen selection need.
func advanceDatabaseQueryTurnTransaction(
	app core.App,
	principalID string,
	pageID string,
	canonical databaseQueryRequest,
	rawCursor string,
	queryHash string,
) (databaseQueryTurn, error) {
	return advanceDatabaseQueryTurnWith(app, principalID, pageID, canonical, rawCursor, queryHash, databaseQueryJobTurn, databaseQueryMentions{})
}

func advanceDatabaseQueryTurnWith(
	app core.App,
	principalID string,
	pageID string,
	canonical databaseQueryRequest,
	rawCursor string,
	queryHash string,
	mode databaseQueryTurnMode,
	mentions databaseQueryMentions,
) (databaseQueryTurn, error) {
	return advanceDatabaseQueryTurnMemo(app, principalID, pageID, canonical, rawCursor, queryHash, mode, nil, mentions)
}

func advanceDatabaseQueryTurnMemo(
	app core.App,
	principalID string,
	pageID string,
	canonical databaseQueryRequest,
	rawCursor string,
	queryHash string,
	mode databaseQueryTurnMode,
	contexts *databaseQueryContextMemo,
	mentions databaseQueryMentions,
) (databaseQueryTurn, error) {
	scope, err := contexts.loadScope(app, principalID, pageID, canonical, queryHash, mentions)
	if err != nil {
		return databaseQueryTurn{}, err
	}
	cursor := databaseQueryCursorState{}
	if rawCursor != "" {
		cursor, err = decodeDatabaseQueryCursor(rawCursor)
		if err != nil || cursor.PrincipalID != principalID || cursor.PageID != pageID || cursor.QueryHash != scope.queryHash {
			return databaseQueryTurn{}, errors.New(errorCodeDatabaseQueryCursorInvalid)
		}
	}
	if mode != databaseQueryJobTurn && (rawCursor == "" || cursor.JobID == "") {
		turn, answered, err := answerDatabaseQueryDirectly(app, scope, principalID, pageID, canonical, cursor)
		if err == nil && answered {
			// What the source's property access rules keep from this reader.
			err = redactDatabaseQueryRows(app, principalID, turn.Response.DataSourceID, turn.Response.Rows)
		}
		if err != nil || answered {
			return turn, err
		}
	}
	if mode == databaseQueryDirectTurn {
		return databaseQueryTurn{}, errDatabaseQueryNeedsJob
	}
	return advanceDatabaseQueryJobTurn(app, scope, principalID, pageID, canonical, rawCursor, cursor, contexts)
}

// answerDatabaseQueryDirectly answers a page of a query in the source's
// order, after the row a direct cursor names; false when the query takes
// its job.
func answerDatabaseQueryDirectly(
	app core.App,
	scope databaseQueryScope,
	principalID, pageID string,
	input databaseQueryRequest,
	cursor databaseQueryCursorState,
) (databaseQueryTurn, bool, error) {
	job := storedDatabaseQueryJob{
		PrincipalID: principalID, ContentOwnerID: scope.authorization.Workspace.OwnerID,
		WorkspaceID: scope.authorization.Workspace.WorkspaceID, PageID: pageID,
		DataSourceID: input.DataSourceID, QueryHash: scope.queryHash, State: databaseQueryStateReady,
		SourcePosition: cursor.SortKey, SourceID: cursor.RowID,
		hiddenRows: scope.hiddenRows, mentions: scope.mentions,
	}
	if input.ViewID != nil {
		job.ViewID = *input.ViewID
	}
	queryContext, err := loadDatabaseQueryContext(app, job)
	if err != nil {
		return databaseQueryTurn{}, false, err
	}
	if _, err := authorizeWorkspace(app, principalID, job.WorkspaceID, permissionWorkspaceRead); err == nil {
		queryContext.People.showEmails()
	} else if !isPermissionDenied(err) {
		return databaseQueryTurn{}, false, err
	}
	plan, err := compileDatabaseQueryPlan(input, queryContext)
	if err != nil {
		return databaseQueryTurn{}, false, err
	}
	prefilter := planDatabaseQueryPrefilter(plan, queryContext, principalID, pageID)
	inOrder := len(plan.Sorts) == 0 && prefilter.exact && !input.CountOnly
	switch {
	case inOrder && cursor.Phase != databaseQueryEvaluatedPhase:
	case !inOrder && cursor.Phase != databaseQueryDirectPhase && !databaseQueryComputes(queryContext):
		// database_query_evaluated.go
		job.SourcePosition, job.SourceID = "", ""
		return answerDatabaseQueryEvaluated(app, scope, job, input, queryContext, plan, prefilter, cursor)
	default:
		return databaseQueryTurn{}, false, nil
	}

	// The view's own order (database_query_linked.go): a cursor in it names
	// its last row by that order, not by position.
	order := queryContext.NaturalOrder
	if order.Mode != databaseNaturalOrderPosition {
		job.SourcePosition, job.SourceID = "", ""
	}
	from, params := databaseQuerySourceScope(job, input)
	for key, value := range prefilter.params {
		params[key] = value
	}
	matching := ""
	if len(prefilter.clauses) > 0 {
		matching = " AND " + strings.Join(prefilter.clauses, " AND ")
	}
	rows, err := loadDatabaseQueryDirectRows(app, from+matching, params, order, cursor, input.Limit+1)
	if err != nil {
		return databaseQueryTurn{}, false, err
	}
	hasMore := len(rows) > input.Limit
	if hasMore {
		rows = rows[:input.Limit]
	}
	for index := range rows {
		rows[index].Values = map[string]graphRowValue{}
		rows[index].Computed = map[string]databaseComputedValue{}
	}
	if databaseQueryComputes(queryContext) {
		shapes, err := loadDatabaseQueryRelationShapes(
			app, job.ContentOwnerID, job.PageID, databaseQueryRowIDs(rows), databaseQueryNestedRelationIDs(queryContext),
		)
		if err != nil {
			return databaseQueryTurn{}, false, err
		}
		for index, row := range rows {
			if !databaseQueryRequiresDurableDependencies(shapes[row.ID], queryContext) {
				continue
			}
			if index == 0 {
				return databaseQueryTurn{}, false, nil
			}
			rows, hasMore = rows[:index], true
			break
		}
	}
	if err := hydrateDatabaseQueryRows(app, job.ContentOwnerID, job.PageID, rows); err != nil {
		return databaseQueryTurn{}, false, err
	}
	bounded, dependencyIDs, err := boundedDatabaseQueryRows(rows, queryContext)
	if err != nil {
		return databaseQueryTurn{}, false, err
	}
	dependencyIDs = queryContext.visibleRowIDs(dependencyIDs)
	dependencyValues, err := loadDatabaseQueryValues(
		app, job.ContentOwnerID, queryContext.readablePages(job.PageID), dependencyIDs,
	)
	if err != nil {
		return databaseQueryTurn{}, false, err
	}
	dependencyRows, err := databaseQueryDependencyRows(
		app, job.ContentOwnerID, queryContext.readablePages(job.PageID), dependencyIDs, dependencyValues,
	)
	if err != nil {
		return databaseQueryTurn{}, false, err
	}
	evaluated, evaluatedRows, err := evaluateDatabaseQueryRows(
		bounded, dependencyRows,
		queryContext, plan, principalID, pageID,
	)
	if err != nil {
		return databaseQueryTurn{}, false, err
	}
	walked := bounded[:evaluatedRows]
	if len(walked) < len(rows) {
		hasMore = true
	}

	// The page holds the rows walked, up to its bytes, always with a row.
	kept := make(map[string]databaseQueryRow, len(evaluated))
	for _, row := range evaluated {
		kept[row.Row.ID] = row.Row
	}
	page := make([]databaseQueryRow, 0, len(evaluated))
	last := storedDatabaseQueryRow{}
	held := 0
	for index, row := range walked {
		if index > 0 && held >= databaseQueryResultPageBytes {
			hasMore = true
			break
		}
		last = row
		if result, ok := kept[row.ID]; ok {
			page = append(page, result)
			held += databaseQueryRowBytes(result)
		}
	}
	found := len(page)

	// The rows are counted as the pages give them, the cursor carrying the
	// count: the last page says how many the query kept. Counting them all
	// at the first would read every row the person may see (2026-10-14:
	// 0.45 s for a member's 10,000 of 100,000 rows).
	found += cursor.Progress
	response := databaseQueryResponse{
		PageID: pageID, DataSourceID: job.DataSourceID, State: databaseQueryStateReady,
		Rows: page, HasMore: hasMore, Scanned: found,
	}
	if hasMore {
		next, err := encodeDatabaseQueryCursor(databaseQueryCursorState{
			Version: 1, Kind: "databaseQuery", PrincipalID: principalID, PageID: pageID,
			QueryHash: scope.queryHash, Phase: databaseQueryDirectPhase, Progress: found,
			SortKey: order.cursorKey(last), RowID: last.ID,
		})
		if err != nil {
			return databaseQueryTurn{}, false, err
		}
		response.NextCursor = &next
	} else {
		response.Total = &found
	}
	return databaseQueryTurn{
		Response: response, Status: http.StatusOK, Authorization: scope.authorization, Job: job,
	}, true, nil
}

// databaseQueryRowBytes is about how much of a page a row takes.
func databaseQueryRowBytes(row databaseQueryRow) int {
	size := len(row.ID) + len(row.Position)
	for _, value := range row.Values {
		size += len(value.Value)
	}
	for _, value := range row.Computed {
		size += len(value.Value) + len(value.Text)
	}
	return size
}
