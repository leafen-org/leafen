package leafen

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Tables: the databases the app's data setup made, read and written by
// their columns' names. A table is found by its setup step's key in the
// organization a call acts in; its rows are read with Leafen's database
// query (conditions, order and search as a view applies them, formulas
// worked out) and written as a person's edit of a row is, so a change keeps
// the row's place and every value it doesn't name.

// Table is one of the app's databases.
type Table struct {
	Tenancy      string
	Key          string
	PageID       string
	DataSourceID string
	// Columns in the table's order.
	Columns []Column
}

// Column is one of a table's properties.
type Column struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Position string         `json:"position"`
	Config   map[string]any `json:"config"`
}

// Option is a choice of a select, multi-select or status column.
type Option struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Query narrows and orders a table's rows by its columns' names.
type Query struct {
	// Where keeps the rows every condition holds for.
	Where []Where
	// Sort orders them; without it they keep the table's own order.
	Sort []Sort
	// Search keeps the rows whose text contains it.
	Search string
	// Archived reads the archived rows instead of the others.
	Archived bool
	// Limit is how many rows a page of Find holds, 1 to 100 (100 when 0).
	Limit int
}

// Where is one condition: an operator the column's type takes ("equals",
// "does_not_equal", "contains", "does_not_contain", "is_empty",
// "is_not_empty", "greater_than", "less_than", "before", "after",
// "on_or_before", "on_or_after", …) and a value — an option's name, a
// person's or row's ID, text, a number, true or false, a date.
type Where struct {
	Column   string
	Operator string
	Value    any
}

// Sort orders rows by a column.
type Sort struct {
	Column     string
	Descending bool
}

// Row is a table's row as it was read.
type Row struct {
	ID           string
	Position     string
	CreatedAt    string
	CreatedBy    string
	UpdatedAt    string
	LastEditedBy string
	table        *Table
	values       map[string]json.RawMessage
}

// ErrNoRow says a row isn't in the table.
var ErrNoRow = errors.New("leafen: no such row in the table")

// OpenTable finds the table its data setup step's key names, in the
// organization tenancy names (empty: the run's own).
func OpenTable(tenancy, key string) (*Table, error) {
	raw, err := callAPI(tenancy, "GET", "/app-data", "", nil)
	if err != nil {
		return nil, err
	}
	data := struct {
		Items []struct {
			Key          string   `json:"key"`
			PageID       string   `json:"pageId"`
			DataSourceID string   `json:"dataSourceId"`
			Properties   []Column `json:"properties"`
		} `json:"items"`
	}{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	for _, item := range data.Items {
		if item.Key != key || item.DataSourceID == "" {
			continue
		}
		if item.PageID == "" || len(item.Properties) == 0 {
			return nil, &Error{Message: fmt.Sprintf("leafen: the %q table can't be read here", key)}
		}
		return &Table{Tenancy: tenancy, Key: key, PageID: item.PageID, DataSourceID: item.DataSourceID, Columns: item.Properties}, nil
	}
	return nil, &Error{Message: fmt.Sprintf("leafen: no %q table is set up here yet", key)}
}

// Column finds a column by its name.
func (table *Table) Column(name string) (Column, bool) {
	for _, column := range table.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return Column{}, false
}

func (table *Table) column(name string) (Column, error) {
	if column, ok := table.Column(name); ok {
		return column, nil
	}
	return Column{}, &Error{Message: fmt.Sprintf("leafen: the %q table has no %q column", table.Key, name)}
}

// Find reads a page of the rows the query keeps, from where cursor (empty
// at first) says, and the cursor of the next page — empty after the last.
// Leafen works through a large table a part at a time, so a page may come
// back empty before the last one.
func (table *Table) Find(query Query, cursor string) ([]Row, string, error) {
	rows, next, _, err := table.page(query, cursor, false)
	return rows, next, err
}

// Count is how many rows the query keeps: about one host call for every
// five thousand rows Leafen walks when every condition names options, people,
// rows or a checked checkbox (and there is no search), otherwise as Find
// reads them.
func (table *Table) Count(query Query) (int, error) {
	query.Limit = 1
	cursor := ""
	for {
		_, next, total, err := table.page(query, cursor, true)
		if err != nil || total != nil {
			if total == nil {
				return 0, err
			}
			return *total, err
		}
		if next == "" {
			return 0, &Error{Message: "leafen: the query ended without a count"}
		}
		cursor = next
	}
}

func (table *Table) page(query Query, cursor string, countOnly bool) ([]Row, string, *int, error) {
	request := map[string]any{"dataSourceId": table.DataSourceID, "limit": query.Limit, "countOnly": countOnly}
	if query.Limit == 0 {
		request["limit"] = 100
	}
	if query.Archived {
		request["archiveScope"] = "archived"
	}
	if query.Search != "" {
		request["search"] = query.Search
	}
	filters := []any{}
	for _, where := range query.Where {
		column, err := table.column(where.Column)
		if err != nil {
			return nil, "", nil, err
		}
		filters = append(filters, map[string]any{"propertyId": column.ID, "operator": where.Operator, "value": where.Value})
	}
	if len(filters) > 0 {
		request["filterGroup"] = map[string]any{"conjunction": "and", "filters": filters, "groups": []any{}}
	}
	sorts := []any{}
	for _, sort := range query.Sort {
		column, err := table.column(sort.Column)
		if err != nil {
			return nil, "", nil, err
		}
		direction := "ascending"
		if sort.Descending {
			direction = "descending"
		}
		sorts = append(sorts, map[string]any{"propertyId": column.ID, "direction": direction})
	}
	request["sorts"] = sorts
	if cursor != "" {
		request["cursor"] = cursor
	}
	raw, err := callAPI(table.Tenancy, "POST", "/pages/"+table.PageID+"/database-query", "", request)
	if err != nil {
		return nil, "", nil, err
	}
	answer := struct {
		Rows []struct {
			ID           string                     `json:"id"`
			Position     string                     `json:"position"`
			CreatedAt    string                     `json:"createdAt"`
			CreatedBy    string                     `json:"createdBy"`
			UpdatedAt    string                     `json:"updatedAt"`
			LastEditedBy string                     `json:"lastEditedBy"`
			Values       map[string]json.RawMessage `json:"values"`
			Computed     map[string]json.RawMessage `json:"computed"`
		} `json:"rows"`
		NextCursor string `json:"nextCursor"`
		HasMore    bool   `json:"hasMore"`
		Total      *int   `json:"total"`
	}{}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, "", nil, err
	}
	rows := make([]Row, 0, len(answer.Rows))
	for _, item := range answer.Rows {
		values := item.Values
		if values == nil {
			values = map[string]json.RawMessage{}
		}
		for id, computed := range item.Computed {
			values[id] = computed
		}
		rows = append(rows, Row{
			ID: item.ID, Position: item.Position, CreatedAt: item.CreatedAt, CreatedBy: item.CreatedBy,
			UpdatedAt: item.UpdatedAt, LastEditedBy: item.LastEditedBy, table: table, values: values,
		})
	}
	if !answer.HasMore {
		return rows, "", answer.Total, nil
	}
	return rows, answer.NextCursor, answer.Total, nil
}

// Rows goes through every row the query keeps, to the last. Each page is a
// host call, and a run has 200: Leafen walks a hundred rows of the table a
// call — five thousand when the conditions name options, people, rows or a
// checked checkbox — and gives back a hundred rows a call.
func (table *Table) Rows(query Query) iter.Seq2[Row, error] {
	return func(yield func(Row, error) bool) {
		cursor := ""
		for {
			rows, next, err := table.Find(query, cursor)
			if err != nil {
				yield(Row{}, err)
				return
			}
			for _, row := range rows {
				if !yield(row, nil) {
					return
				}
			}
			if next == "" {
				return
			}
			cursor = next
		}
	}
}

// Row reads one row by its ID, in one host call: its kept values — formulas
// and rollups come only with Find and Rows. ErrNoRow when it isn't in the
// table.
func (table *Table) Row(id string) (Row, error) {
	raw, err := callAPI(table.Tenancy, "GET", "/database-rows/"+url.PathEscape(id)+"/page", "limit=1", nil)
	var refused *Error
	if errors.As(err, &refused) && strings.HasPrefix(refused.Message, "404 ") {
		return Row{}, ErrNoRow
	}
	if err != nil {
		return Row{}, err
	}
	answer := struct {
		Row struct {
			ID           string                     `json:"id"`
			DataSourceID string                     `json:"dataSourceId"`
			Position     string                     `json:"position"`
			CreatedAt    string                     `json:"createdAt"`
			CreatedBy    string                     `json:"createdBy"`
			UpdatedAt    string                     `json:"updatedAt"`
			LastEditedBy string                     `json:"lastEditedBy"`
			TrashedAt    string                     `json:"trashedAt"`
			Values       map[string]json.RawMessage `json:"values"`
		} `json:"row"`
	}{}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return Row{}, err
	}
	found := answer.Row
	if found.DataSourceID != table.DataSourceID || found.TrashedAt != "" {
		return Row{}, ErrNoRow
	}
	if found.Values == nil {
		found.Values = map[string]json.RawMessage{}
	}
	return Row{
		ID: found.ID, Position: found.Position, CreatedAt: found.CreatedAt, CreatedBy: found.CreatedBy,
		UpdatedAt: found.UpdatedAt, LastEditedBy: found.LastEditedBy, table: table, values: found.Values,
	}, nil
}

// Add makes a row after the ones the app added before, with values by
// column name (see Update), and returns its ID.
func (table *Table) Add(values map[string]any) (string, error) {
	encoded, err := table.encode(values)
	if err != nil {
		return "", err
	}
	id := newID()
	_, err = callAPI(table.Tenancy, "POST", "/pages/"+table.PageID+"/content-graph/mutations", "", map[string]any{
		"mutationId": newID(),
		"operations": []any{map[string]any{"kind": "upsertRow", "row": map[string]any{
			"id": id, "dataSourceId": table.DataSourceID, "position": fmt.Sprintf("t%013d", time.Now().UnixMilli()), "values": encoded,
		}}},
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// Update changes the values it names, by column name, and leaves the rest
// of the row as it is. A value is what the column's type holds: text; a
// number; true or false; an option's name (a list of names for a
// multi-select); a list of people's or rows' IDs; a date as "2006-01-02",
// "start/end" or a Date; nil clears it. An option the column doesn't have is
// refused, not dropped.
func (table *Table) Update(id string, values map[string]any) error {
	encoded, err := table.encode(values)
	if err != nil {
		return err
	}
	_, err = callAPI(table.Tenancy, "POST", "/database-rows/"+url.PathEscape(id)+"/page/values", "", map[string]any{
		"mutationId": newID(), "values": encoded,
	})
	return err
}

// UpdateMany changes many rows, their values by row ID as Update takes
// them: a hundred rows to one host call and one write of the app's budget.
func (table *Table) UpdateMany(rows map[string]map[string]any) error {
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for start := 0; start < len(ids); start += 100 {
		listed := []map[string]any{}
		for _, id := range ids[start:min(start+100, len(ids))] {
			encoded, err := table.encode(rows[id])
			if err != nil {
				return err
			}
			listed = append(listed, map[string]any{"rowId": id, "values": encoded})
		}
		if _, err := callAPI(table.Tenancy, "POST", "/pages/"+table.PageID+"/database-row-values", "", map[string]any{
			"mutationId": newID(), "rows": listed,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Remove moves a row to the trash, where people can restore it, in two host
// calls; a row already there stays. ErrNoRow when it isn't in the table.
func (table *Table) Remove(id string) error {
	path := "/database-rows/" + url.PathEscape(id) + "/page"
	raw, err := callAPI(table.Tenancy, "GET", path, "limit=1", nil)
	var refused *Error
	if errors.As(err, &refused) && strings.HasPrefix(refused.Message, "404 ") {
		return ErrNoRow
	}
	if err != nil {
		return err
	}
	found := struct {
		Row struct {
			DataSourceID      string `json:"dataSourceId"`
			TrashedAt         string `json:"trashedAt"`
			AuthorityRevision int    `json:"authorityRevision"`
		} `json:"row"`
	}{}
	if err := json.Unmarshal(raw, &found); err != nil {
		return err
	}
	if found.Row.DataSourceID != table.DataSourceID {
		return ErrNoRow
	}
	if found.Row.TrashedAt != "" {
		return nil
	}
	_, err = callAPI(table.Tenancy, "POST", path+"/trash", "", map[string]any{
		"requestId": newID(), "expectedAuthorityRevision": found.Row.AuthorityRevision,
	})
	return err
}

func callAPI(tenancy, method, path, query string, body any) (json.RawMessage, error) {
	answer, err := API(APIRequest{Tenancy: tenancy, Method: method, Path: path, Query: query, Body: body})
	if err != nil {
		return nil, err
	}
	if answer.Status < 300 {
		return answer.Body, nil
	}
	refusal := struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{}
	detail := strings.TrimSpace(answer.Text)
	if json.Unmarshal(answer.Body, &refusal) == nil && refusal.Error.Code != "" {
		detail = refusal.Error.Code + ": " + refusal.Error.Message
	}
	return nil, &Error{Message: fmt.Sprintf("%d %s %s: %s", answer.Status, method, path, detail)}
}

func newID() string {
	bytes := make([]byte, 12)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
