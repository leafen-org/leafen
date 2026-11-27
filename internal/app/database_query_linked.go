package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// databaseQuerySourcePage is the page a query reads its rows from: the data
// source's own page. A request may name instead the page a linked view of
// that source sits in — a database shown in another page or in a row's page
// — or a linked view on the source's page; the screen shows those views'
// rows, so the query reads them from the source too, once the reader may
// read every page involved. Anything else is left as asked, to be refused
// where the query loads.
func databaseQuerySourcePage(app core.App, principalID, pageID string, input databaseQueryRequest) (string, error) {
	dataSourceID := strings.TrimSpace(input.DataSourceID)
	if dataSourceID == "" {
		return pageID, nil
	}
	sourcePage := ""
	if err := app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_data_sources}}
		WHERE [[id]] = {:id} AND ` + databaseSourceNotTrashedSQL("{:id}") + `
	`).Bind(dbx.Params{"id": dataSourceID}).Row(&sourcePage); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return pageID, nil
		}
		return "", err
	}
	viewPage := ""
	if input.ViewID != nil && strings.TrimSpace(*input.ViewID) != "" {
		if err := app.DB().NewQuery(`
			SELECT [[page_id]] FROM {{leafen_database_views}}
			WHERE [[id]] = {:id} AND [[data_source_id]] = {:dataSourceId}
		`).Bind(dbx.Params{"id": strings.TrimSpace(*input.ViewID), "dataSourceId": dataSourceID}).Row(&viewPage); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return pageID, nil
			}
			return "", err
		}
	}
	if pageID == sourcePage && (viewPage == "" || viewPage == pageID) {
		return pageID, nil
	}
	if pageID != sourcePage && viewPage != pageID {
		// The page asked for must hold a view of the source.
		var held int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_database_views}}
			WHERE [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
		`).Bind(dbx.Params{"pageId": pageID, "dataSourceId": dataSourceID}).Row(&held); err != nil {
			return "", err
		}
		if held == 0 {
			return pageID, nil
		}
	}
	for _, page := range []string{pageID, sourcePage, viewPage} {
		if page == "" {
			continue
		}
		if _, err := authorizePage(app, principalID, page, permissionWorkspaceRead); err != nil {
			if isPermissionDenied(err) {
				return "", graphNotFoundError()
			}
			return "", err
		}
	}
	return sourcePage, nil
}

// databaseViewConfigOnPage binds a view's "현재 페이지" filter values to the
// page the view sits in — a linked view's own page, not its source's —
// wherever a view's config is read for a query, so a database in a row's
// page lists that row's items as its screen does.
func databaseViewConfigOnPage(raw, pageID string) string {
	if pageID == "" || !strings.Contains(raw, "leafen.current_page") {
		return raw
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return raw
	}
	bound, err := json.Marshal(bindDatabaseViewCurrentPage(value, pageID))
	if err != nil {
		return raw
	}
	return string(bound)
}

func bindDatabaseViewCurrentPage(value any, pageID string) any {
	switch typed := value.(type) {
	case map[string]any:
		if kind, _ := typed["kind"].(string); kind == "leafen.current_page" {
			return pageID
		}
		for key, item := range typed {
			typed[key] = bindDatabaseViewCurrentPage(item, pageID)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = bindDatabaseViewCurrentPage(item, pageID)
		}
		return typed
	}
	return value
}

var errDatabaseViewConfigInvalid = errors.New("database view config invalid")

// databaseViewQueryTerms are the filters, sorts and search a saved view
// shows its rows under, as its screen does: its filter group (or the older
// flat filters), and the filter bar's terms on properties the source still
// has, all of them together. Every query of a view reads them here — a
// view's own query, the Notion view query and the MCP's view mode.
func databaseViewQueryTerms(raw string, known []string) (*databaseQueryFilterGroup, []databaseQuerySort, *string, error) {
	config := struct {
		Search            string                    `json:"search"`
		FilterGroup       *databaseQueryFilterGroup `json:"filterGroup"`
		Filters           []databaseQueryFilter     `json:"filters"`
		FilterConjunction string                    `json:"filterConjunction"`
		QuickFilters      []json.RawMessage         `json:"quickFilters"`
		Sorts             []databaseQuerySort       `json:"sorts"`
	}{}
	if json.Unmarshal([]byte(raw), &config) != nil {
		return nil, nil, nil, errDatabaseViewConfigInvalid
	}
	groups := []databaseQueryFilterGroup{}
	hasTerms := func(group *databaseQueryFilterGroup) bool {
		return group != nil && (len(group.Filters) > 0 || len(group.Groups) > 0)
	}
	if hasTerms(config.FilterGroup) {
		groups = append(groups, *config.FilterGroup)
	} else if len(config.Filters) > 0 {
		conjunction := databaseQueryConjunctionAnd
		if config.FilterConjunction == databaseQueryConjunctionOr {
			conjunction = databaseQueryConjunctionOr
		}
		groups = append(groups, databaseQueryFilterGroup{Conjunction: conjunction, Filters: config.Filters})
	}
	if len(config.QuickFilters) > 0 {
		quick := databaseQueryFilterGroup{Conjunction: databaseQueryConjunctionAnd}
		for _, raw := range config.QuickFilters {
			term := databaseQueryFilterGroup{}
			if json.Unmarshal(raw, &term) == nil && term.Conjunction != "" {
				if notionViewQueryGroupNamesProperty(term, known) {
					quick.Groups = append(quick.Groups, term)
				}
				continue
			}
			filter := databaseQueryFilter{}
			if json.Unmarshal(raw, &filter) != nil {
				return nil, nil, nil, errDatabaseViewConfigInvalid
			}
			if slices.Contains(known, filter.PropertyID) {
				quick.Filters = append(quick.Filters, filter)
			}
		}
		if hasTerms(&quick) {
			groups = append(groups, quick)
		}
	}
	var search *string
	if config.Search != "" {
		search = &config.Search
	}
	switch len(groups) {
	case 0:
		return nil, config.Sorts, search, nil
	case 1:
		return &groups[0], config.Sorts, search, nil
	}
	return &databaseQueryFilterGroup{Conjunction: databaseQueryConjunctionAnd, Groups: groups}, config.Sorts, search, nil
}

// databaseNaturalOrder is the order a query's rows take when it has no
// sorts, and that breaks its sorts' ties: the order its view shows them in,
// as the screen keeps a view's own order
// (web/src/components/database/viewRowOrder.ts). A view kept since keeps
// `rowOrder`: empty, the newest rows first; with rows placed, those in that
// order and the rest right after them, newest first, as Notion's view shows
// rows it has not placed — made through the API or in another view (seen
// 2026-11-11). A view from before keeps the rows' own positions. A query
// naming no view takes its source's first view. A Public API connection's
// query takes no view's order: its rows come newest first by their created
// times, ties too, as Notion's data source and view queries answer whatever
// order a view keeps (seen 2026-11-11: a row placed between others on the
// screen still came first, and an edit moved none). Rows of one created
// time come in the order of their IDs, as Notion's (seen 2026-11-17 twice:
// the rows one notion-create-pages call makes share its time and read back
// so, in rows and view mode alike, after a row made later).
type databaseNaturalOrder struct {
	Mode   string
	Placed map[string]int
	// Order is the placed rows' ids in their order, as the view keeps them.
	Order []string
}

const (
	databaseNaturalOrderPosition = ""
	databaseNaturalOrderNewest   = "newest"
	databaseNaturalOrderPlaced   = "placed"
)

func parseDatabaseNaturalOrder(raw string) databaseNaturalOrder {
	config := struct {
		RowOrder *[]string `json:"rowOrder"`
	}{}
	if json.Unmarshal([]byte(raw), &config) != nil || config.RowOrder == nil {
		return databaseNaturalOrder{}
	}
	if len(*config.RowOrder) == 0 {
		return databaseNaturalOrder{Mode: databaseNaturalOrderNewest}
	}
	// A row placed twice keeps its first place; the order lists each row once,
	// so reading rows from it never gives one twice.
	placed := make(map[string]int, len(*config.RowOrder))
	order := make([]string, 0, len(*config.RowOrder))
	for _, id := range *config.RowOrder {
		if _, seen := placed[id]; !seen {
			placed[id] = len(order)
			order = append(order, id)
		}
	}
	return databaseNaturalOrder{Mode: databaseNaturalOrderPlaced, Placed: placed, Order: order}
}

// key orders one row under the natural order, last in a query's sort key.
func (order databaseNaturalOrder) key(row databaseQueryRow) string {
	switch order.Mode {
	case databaseNaturalOrderNewest:
		return reverseDatabaseSortKey(row.CreatedAt) + databaseCodePointOrderKey(row.ID)
	case databaseNaturalOrderPlaced:
		if index, ok := order.Placed[row.ID]; ok {
			return fmt.Sprintf("0%08d", index)
		}
		return "1" + reverseDatabaseSortKey(row.CreatedAt) + databaseCodePointOrderKey(row.ID)
	}
	return databaseStableSourceOrderKey(row.Position, row.ID)
}

// loadDatabaseNaturalOrder reads the order of a query's view, or of its
// source's first view when it names none; a Public API connection's reading
// reads none (databaseQueryMentions.newest).
func loadDatabaseNaturalOrder(app core.App, job storedDatabaseQueryJob, viewConfig string) (databaseNaturalOrder, error) {
	if job.mentions.newest {
		return databaseNaturalOrder{Mode: databaseNaturalOrderNewest}, nil
	}
	if job.ViewID != "" {
		return parseDatabaseNaturalOrder(viewConfig), nil
	}
	config := ""
	if err := app.DB().NewQuery(`
		SELECT [[config]] FROM {{leafen_database_views}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
		ORDER BY [[position]], [[id]] LIMIT 1
	`).Bind(dbx.Params{
		"owner": job.ContentOwnerID, "pageId": job.PageID, "dataSourceId": job.DataSourceID,
	}).Row(&config); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return databaseNaturalOrder{}, nil
		}
		return databaseNaturalOrder{}, err
	}
	return parseDatabaseNaturalOrder(config), nil
}

// loadDatabaseQueryDirectRows reads a page of a query answered in its
// natural order straight from its source's rows, after the row its cursor
// names: by position; newest first by the rows' created times, which an
// index keeps in order (2026-11-06 on 100,000 rows: 0.03-0.06 s, as by
// position, where evaluating the whole source took 4-17 s after a change),
// those of one time by their IDs; or the placed rows in their order, then
// the rest newest first.
func loadDatabaseQueryDirectRows(
	app core.App, scope string, params dbx.Params, order databaseNaturalOrder, cursor databaseQueryCursorState, limit int,
) ([]storedDatabaseQueryRow, error) {
	readFrom := func(scope, clause, orderBy string, extra dbx.Params, max int) ([]storedDatabaseQueryRow, error) {
		bound := dbx.Params{}
		for key, value := range params {
			bound[key] = value
		}
		for key, value := range extra {
			bound[key] = value
		}
		bound["limit"] = max
		rows := []storedDatabaseQueryRow{}
		err := app.DB().NewQuery(databaseQuerySourceHeadersSQL + scope + clause + orderBy + ` LIMIT {:limit}`).Bind(bound).All(&rows)
		return rows, err
	}
	read := func(clause, orderBy string, extra dbx.Params, max int) ([]storedDatabaseQueryRow, error) {
		return readFrom(scope, clause, orderBy, extra, max)
	}
	after := func(prefix string) (int64, bool) {
		value, ok := strings.CutPrefix(cursor.SortKey, prefix)
		if !ok || cursor.RowID == "" {
			return 0, false
		}
		number, err := strconv.ParseInt(value, 10, 64)
		return number, err == nil
	}
	switch order.Mode {
	case databaseNaturalOrderNewest:
		clause, extra := "", dbx.Params{}
		if created, ok := after("n"); ok {
			clause = ` AND ([[row]].[[created]] < {:afterCreated} OR ([[row]].[[created]] = {:afterCreated} AND [[row]].[[id]] > {:afterId}))`
			extra = dbx.Params{"afterCreated": created, "afterId": cursor.RowID}
		}
		return read(clause, ` ORDER BY [[row]].[[created]] DESC, [[row]].[[id]]`, extra, limit)
	case databaseNaturalOrderPlaced:
		encoded, err := json.Marshal(order.Order)
		if err != nil {
			return nil, err
		}
		rows := []storedDatabaseQueryRow{}
		restAfter, inRest := after("r")
		if !inRest {
			index, ok := after("p")
			if !ok {
				index = -1
			}
			// The placed ids lead the join, so only their rows are read and
			// checked: with the source's rows first, SQLite took its index on
			// the source and checked every row of it (2026-11-06, 100,000
			// rows, 10 placed: 0.3 s a page).
			const rowsFrom = `FROM {{leafen_database_rows}} AS [[row]]`
			if !strings.Contains(scope, rowsFrom) {
				return nil, errors.New("database query scope does not start from its rows")
			}
			placedScope := strings.Replace(scope, rowsFrom,
				`FROM json_each({:placed}) AS [[place]] CROSS JOIN {{leafen_database_rows}} AS [[row]]`, 1)
			placed, err := readFrom(placedScope, ` AND [[row]].[[id]] = [[place]].[[value]] AND [[place]].[[key]] > {:afterIndex}`,
				` ORDER BY [[place]].[[key]]`, dbx.Params{"placed": string(encoded), "afterIndex": index}, limit)
			if err != nil || len(placed) >= limit {
				return placed, err
			}
			rows = placed
		}
		clause := ` AND [[row]].[[id]] NOT IN (SELECT [[value]] FROM json_each({:placed}))`
		extra := dbx.Params{"placed": string(encoded)}
		if inRest {
			clause += ` AND ([[row]].[[created]] < {:afterCreated} OR ([[row]].[[created]] = {:afterCreated} AND [[row]].[[id]] > {:afterId}))`
			extra["afterCreated"], extra["afterId"] = restAfter, cursor.RowID
		}
		rest, err := read(clause, ` ORDER BY [[row]].[[created]] DESC, [[row]].[[id]]`, extra, limit-len(rows))
		return append(rows, rest...), err
	}
	return read("", ` ORDER BY [[row]].[[position]], [[row]].[[id]]`, nil, limit)
}

// cursorKey is what a direct page's cursor keeps of its last row to go on
// after it under the natural order.
func (order databaseNaturalOrder) cursorKey(row storedDatabaseQueryRow) string {
	switch order.Mode {
	case databaseNaturalOrderNewest:
		return "n" + strconv.FormatInt(row.RowCreated, 10)
	case databaseNaturalOrderPlaced:
		if index, ok := order.Placed[row.ID]; ok {
			return "p" + strconv.Itoa(index)
		}
		return "r" + strconv.FormatInt(row.RowCreated, 10)
	}
	return row.Position
}
