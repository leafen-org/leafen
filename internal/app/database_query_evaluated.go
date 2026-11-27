package app

import (
	"container/list"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A sorted query, one with a search, or one whose conditions only its rows'
// values decide, on a source without formulas or rollups, is evaluated
// whole in the read snapshot: the rows the person may see, their values, and
// each kept row's place in the query's order (2026-10-14: the job built such
// a query a hundred rows a call, for each person and after any change — on
// 100,000 rows 1,001 calls). The page is read from the places, and so are
// the next pages: the places are kept for the person, the query and the
// source as it is, for a minute at most, so a change in what the person may
// see that moves no revision holds no longer than that. A source with
// formulas or rollups, whose values may read many related rows, and one with
// more rows than an evaluation holds, build their job as before.

const (
	databaseQueryEvaluatedPhase = "evaluated"
	// databaseQueryEvaluatedRows is the most rows one evaluation reads.
	databaseQueryEvaluatedRows = 250_000
	// databaseQueryEvaluationBatch is how many rows' values it reads at once.
	databaseQueryEvaluationBatch = 1000
	// databaseQueryEvaluationsAtOnce is how many evaluations run at a time.
	databaseQueryEvaluationsAtOnce = 4
	// databaseQueryEvaluationsBytes is how much the kept evaluations hold,
	// each a quarter at most.
	databaseQueryEvaluationsBytes = 128 << 20
	// databaseQueryCursorSortKeyBytes is the longest sort key a cursor carries;
	// past it the cursor names its row alone.
	databaseQueryCursorSortKeyBytes = 1024
)

type databaseQueryPlace struct {
	ID      string
	SortKey string
}

// databaseQueryEvaluation is a query's rows in its order. A kept one holds
// their IDs alone: a cursor finds its row among them, and an evaluation made
// afresh, with its sort keys, places a row gone since.
type databaseQueryEvaluation struct {
	key   string
	ids   []string
	keys  []string
	at    map[string]int
	bytes int
}

var databaseQueryEvaluationSlots = make(chan struct{}, databaseQueryEvaluationsAtOnce)

var databaseQueryEvaluations = struct {
	sync.Mutex
	entries map[string]*list.Element
	order   *list.List
	bytes   int
}{entries: map[string]*list.Element{}, order: list.New()}

func keptDatabaseQueryEvaluation(key string) *databaseQueryEvaluation {
	databaseQueryEvaluations.Lock()
	defer databaseQueryEvaluations.Unlock()
	element, ok := databaseQueryEvaluations.entries[key]
	if !ok {
		return nil
	}
	databaseQueryEvaluations.order.MoveToFront(element)
	return element.Value.(*databaseQueryEvaluation)
}

// keepDatabaseQueryEvaluation keeps an evaluation's order, its sort keys
// left to the one made afresh.
func keepDatabaseQueryEvaluation(evaluation *databaseQueryEvaluation) {
	if evaluation.bytes > databaseQueryEvaluationsBytes/4 {
		return
	}
	databaseQueryEvaluations.Lock()
	defer databaseQueryEvaluations.Unlock()
	if _, ok := databaseQueryEvaluations.entries[evaluation.key]; ok {
		return
	}
	kept := *evaluation
	kept.keys = nil
	databaseQueryEvaluations.entries[evaluation.key] = databaseQueryEvaluations.order.PushFront(&kept)
	databaseQueryEvaluations.bytes += evaluation.bytes
	for databaseQueryEvaluations.bytes > databaseQueryEvaluationsBytes {
		oldest := databaseQueryEvaluations.order.Back()
		kept := oldest.Value.(*databaseQueryEvaluation)
		databaseQueryEvaluations.order.Remove(oldest)
		delete(databaseQueryEvaluations.entries, kept.key)
		databaseQueryEvaluations.bytes -= kept.bytes
	}
}

// answerDatabaseQueryEvaluated answers a page, or the count, of a query
// evaluated whole; false when it takes its job.
func answerDatabaseQueryEvaluated(
	app core.App,
	scope databaseQueryScope,
	job storedDatabaseQueryJob,
	input databaseQueryRequest,
	queryContext databaseQueryContext,
	plan databaseQueryPlan,
	prefilter databaseQueryPrefilter,
	cursor databaseQueryCursorState,
) (databaseQueryTurn, bool, error) {
	revision, err := databaseQueryJobRevision(app, job)
	if err != nil {
		return databaseQueryTurn{}, false, err
	}
	// Without a reference time a query reads the time it runs at.
	at := time.Now().UTC().Truncate(time.Minute).Format(time.RFC3339)
	if input.ReferenceTime != nil {
		at = *input.ReferenceTime
	}
	key := strings.Join([]string{job.PrincipalID, job.PageID, scope.queryHash, strconv.Itoa(revision), at}, "\x00")
	evaluate := func() (*databaseQueryEvaluation, bool, error) {
		evaluation, complete, err := evaluateDatabaseQueryWhole(app, job, input, queryContext, plan, prefilter)
		if err != nil || !complete {
			return nil, false, err
		}
		evaluation.key = key
		keepDatabaseQueryEvaluation(evaluation)
		return evaluation, true, nil
	}
	evaluation := keptDatabaseQueryEvaluation(key)
	if evaluation == nil {
		var complete bool
		if evaluation, complete, err = evaluate(); err != nil || !complete {
			return databaseQueryTurn{}, false, err
		}
	}
	start := 0
	if cursor.Phase == databaseQueryEvaluatedPhase {
		index, found := evaluation.at[cursor.RowID]
		if !found && evaluation.keys == nil {
			var complete bool
			if evaluation, complete, err = evaluate(); err != nil || !complete {
				return databaseQueryTurn{}, false, err
			}
			index, found = evaluation.at[cursor.RowID]
		}
		switch {
		case found:
			start = index + 1
		case cursor.SortKey != "":
			start = sort.Search(len(evaluation.ids), func(index int) bool {
				key := evaluation.keys[index]
				return key > cursor.SortKey || (key == cursor.SortKey && evaluation.ids[index] > cursor.RowID)
			})
		default:
			return databaseQueryTurn{}, false, errors.New(errorCodeDatabaseQueryCursorInvalid)
		}
	}

	total := len(evaluation.ids)
	response := databaseQueryResponse{
		PageID: job.PageID, DataSourceID: job.DataSourceID, State: databaseQueryStateReady,
		Rows: []databaseQueryRow{}, Scanned: total, Total: &total,
	}
	turn := databaseQueryTurn{Status: http.StatusOK, Authorization: scope.authorization, Job: job}
	if input.CountOnly {
		turn.Response = response
		return turn, true, nil
	}
	end := min(total, start+input.Limit)
	rows, err := loadDatabaseQueryPlacedRows(app, job, input, queryContext, plan, evaluation.ids[start:end])
	if err != nil {
		return databaseQueryTurn{}, false, err
	}
	// The page holds its rows up to its bytes, always with one.
	held := 0
	for index := range rows {
		if index > 0 && held >= databaseQueryResultPageBytes {
			rows, end = rows[:index], start+index
			break
		}
		held += databaseQueryRowBytes(rows[index])
	}
	response.Rows = rows
	response.HasMore = end < total
	if response.HasMore {
		next := databaseQueryCursorState{
			Version: 1, Kind: "databaseQuery", PrincipalID: job.PrincipalID, PageID: job.PageID,
			QueryHash: scope.queryHash, Phase: databaseQueryEvaluatedPhase, Progress: end, RowID: evaluation.ids[end-1],
		}
		if evaluation.keys != nil && len(evaluation.keys[end-1]) <= databaseQueryCursorSortKeyBytes {
			next.SortKey = evaluation.keys[end-1]
		}
		encoded, err := encodeDatabaseQueryCursor(next)
		if err != nil {
			return databaseQueryTurn{}, false, err
		}
		response.NextCursor = &encoded
	}
	turn.Response = response
	return turn, true, nil
}

// evaluateDatabaseQueryWhole places every row of the query, in its order;
// not complete when the source has more rows than an evaluation reads.
func evaluateDatabaseQueryWhole(
	app core.App,
	job storedDatabaseQueryJob,
	input databaseQueryRequest,
	queryContext databaseQueryContext,
	plan databaseQueryPlan,
	prefilter databaseQueryPrefilter,
) (*databaseQueryEvaluation, bool, error) {
	databaseQueryEvaluationSlots <- struct{}{}
	defer func() { <-databaseQueryEvaluationSlots }()
	from, params := databaseQuerySourceScope(job, input)
	for key, value := range prefilter.params {
		params[key] = value
	}
	matching := ""
	if len(prefilter.clauses) > 0 {
		matching = " AND " + strings.Join(prefilter.clauses, " AND ")
	}
	params["limit"] = databaseQueryEvaluatedRows + 1
	rows := []storedDatabaseQueryRow{}
	// What the conditions and the order read of a row; its sub-items' count
	// comes with the page.
	if err := app.DB().NewQuery(`
		SELECT [[row]].[[id]], [[row]].[[position]], [[row]].[[subitem_parent_id]],
			COALESCE([[page]].[[created]], [[row]].[[created]]) AS [[created]],
			COALESCE([[page]].[[updated]], [[row]].[[updated]]) AS [[updated]],
			COALESCE([[row_record]].[[created_by]], '') AS [[created_by]],
			COALESCE([[row_record]].[[last_edited_by]], '') AS [[last_edited_by]]` + from + matching + `
		LIMIT {:limit}`).Bind(params).All(&rows); err != nil {
		return nil, false, err
	}
	if len(rows) > databaseQueryEvaluatedRows {
		return nil, false, nil
	}
	places := make([]databaseQueryPlace, 0, len(rows))
	for start := 0; start < len(rows); start += databaseQueryEvaluationBatch {
		batch := rows[start:min(len(rows), start+databaseQueryEvaluationBatch)]
		values, err := loadDatabaseQueryRowValues(app, job.ContentOwnerID, job.PageID, databaseQueryRowIDs(batch))
		if err != nil {
			return nil, false, err
		}
		queryContext.warmMentions(values)
		for _, row := range batch {
			row.Values = values[row.ID]
			if row.Values == nil {
				row.Values = map[string]graphRowValue{}
			}
			row.Computed = map[string]databaseComputedValue{}
			evaluated, accepted, err := evaluateDatabaseQueryRow(row, nil, nil, queryContext, plan, job.PrincipalID, job.PageID)
			if err != nil {
				return nil, false, err
			}
			if accepted {
				places = append(places, databaseQueryPlace{ID: row.ID, SortKey: evaluated.SortKey})
			}
		}
	}
	// As a finished job's results are read: by sort key, then by row.
	sort.Slice(places, func(left, right int) bool {
		a, b := places[left], places[right]
		return a.SortKey < b.SortKey || (a.SortKey == b.SortKey && a.ID < b.ID)
	})
	evaluation := &databaseQueryEvaluation{
		ids: make([]string, len(places)), keys: make([]string, len(places)), at: make(map[string]int, len(places)),
	}
	for index, place := range places {
		evaluation.ids[index], evaluation.keys[index] = place.ID, place.SortKey
		evaluation.at[place.ID] = index
		evaluation.bytes += len(place.ID) + 80
	}
	return evaluation, true, nil
}

// loadDatabaseQueryRowValues reads some rows' values of the source's page.
func loadDatabaseQueryRowValues(app core.App, owner, pageID string, rowIDs []string) (map[string]map[string]graphRowValue, error) {
	result := make(map[string]map[string]graphRowValue, len(rowIDs))
	if len(rowIDs) == 0 {
		return result, nil
	}
	encoded, err := json.Marshal(rowIDs)
	if err != nil {
		return nil, err
	}
	stored := []storedDatabaseQueryValue{}
	if err := app.DB().NewQuery(`
		SELECT [[row_id]], [[property_id]], [[type]], [[value]], [[spans]]
		FROM {{leafen_database_row_values}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[row_id]] IN (SELECT [[value]] FROM json_each({:rows}))
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "rows": string(encoded)}).All(&stored); err != nil {
		return nil, err
	}
	for _, value := range stored {
		values, ok := result[value.RowID]
		if !ok {
			values = map[string]graphRowValue{}
			result[value.RowID] = values
		}
		values[value.PropertyID] = graphRowValue{
			Type: value.Type, Value: json.RawMessage(value.Value), Spans: databaseStoredSpans(value.Spans),
		}
	}
	return result, nil
}

// loadDatabaseQueryPlacedRows reads a page of placed rows as the query gives
// them; a row gone from what the person may see is left out.
func loadDatabaseQueryPlacedRows(
	app core.App,
	job storedDatabaseQueryJob,
	input databaseQueryRequest,
	queryContext databaseQueryContext,
	plan databaseQueryPlan,
	ids []string,
) ([]databaseQueryRow, error) {
	if len(ids) == 0 {
		return []databaseQueryRow{}, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	from, params := databaseQuerySourceScope(job, input)
	params["placed"] = string(encoded)
	// The page's rows found by their IDs, not by walking the source.
	from = strings.Replace(from, "FROM {{leafen_database_rows}} AS [[row]]",
		"FROM json_each({:placed}) AS [[placed]] CROSS JOIN {{leafen_database_rows}} AS [[row]]", 1)
	found := []storedDatabaseQueryRow{}
	if err := app.DB().NewQuery(databaseQuerySourceHeadersSQL + from + `
		AND [[row]].[[id]] = [[placed]].[[value]]`).Bind(params).All(&found); err != nil {
		return nil, err
	}
	byID := make(map[string]storedDatabaseQueryRow, len(found))
	for _, row := range found {
		byID[row.ID] = row
	}
	ordered := make([]storedDatabaseQueryRow, 0, len(found))
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			row.Values, row.Computed = map[string]graphRowValue{}, map[string]databaseComputedValue{}
			ordered = append(ordered, row)
		}
	}
	if err := hydrateDatabaseQueryRows(app, job.ContentOwnerID, job.PageID, ordered); err != nil {
		return nil, err
	}
	rows := make([]databaseQueryRow, 0, len(ordered))
	for _, row := range ordered {
		evaluated, _, err := evaluateDatabaseQueryRow(row, nil, nil, queryContext, plan, job.PrincipalID, job.PageID)
		if err != nil {
			return nil, err
		}
		if evaluated.Row.ID == "" {
			// A row the conditions no longer keep is read as the page has it.
			evaluated.Row = databaseQueryRow{
				ID: row.ID, Position: row.Position, SubitemParentID: databaseQueryOptionalID(row.SubitemParentID),
				SubitemChildCount: row.SubitemChildCount, Values: row.Values, Computed: row.Computed,
				CreatedAt: databaseQueryRowTime(row.Created), CreatedBy: row.CreatedBy,
				UpdatedAt: databaseQueryRowTime(row.Updated), LastEditedBy: row.LastEditedBy,
			}
		}
		rows = append(rows, evaluated.Row)
	}
	return rows, nil
}
