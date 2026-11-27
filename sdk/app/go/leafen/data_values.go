package leafen

import (
	"encoding/json"
	"fmt"
)

// How a table's values go to Leafen and come back: options by name, dates
// as a start and an end, the rest as the column's type keeps them.

// Date is a date column's value: its start and, for a range, its end.
type Date struct {
	Start string `json:"start"`
	End   string `json:"end,omitempty"`
}

// Options lists a select, multi-select or status column's choices.
func (column Column) Options() []Option {
	options := []Option{}
	encoded, _ := json.Marshal(column.Config["options"])
	_ = json.Unmarshal(encoded, &options)
	return options
}

func (table *Table) encode(values map[string]any) (map[string]any, error) {
	encoded := map[string]any{}
	for name, value := range values {
		column, err := table.column(name)
		if err != nil {
			return nil, err
		}
		if value != nil && (column.Type == "select" || column.Type == "status" || column.Type == "multi_select") {
			value, err = table.optionIDs(column, value)
			if err != nil {
				return nil, err
			}
		}
		if date, ok := value.(Date); ok {
			value = map[string]any{"start": date.Start, "end": date.End}
		}
		encoded[column.ID] = map[string]any{"type": column.Type, "value": value}
	}
	return encoded, nil
}

func (table *Table) optionIDs(column Column, value any) (any, error) {
	ids := map[string]string{}
	for _, option := range column.Options() {
		ids[option.Name] = option.ID
	}
	find := func(name string) (string, error) {
		if id, ok := ids[name]; ok {
			return id, nil
		}
		return "", &Error{Message: fmt.Sprintf("leafen: the %q column has no %q option", column.Name, name)}
	}
	switch given := value.(type) {
	case string:
		if given == "" {
			return nil, nil
		}
		return find(given)
	case []string:
		found := make([]string, 0, len(given))
		for _, name := range given {
			id, err := find(name)
			if err != nil {
				return nil, err
			}
			found = append(found, id)
		}
		return found, nil
	}
	return nil, &Error{Message: fmt.Sprintf("leafen: the %q column takes option names", column.Name)}
}

// Value decodes a column's value as Leafen keeps it into target.
func (row Row) Value(column string, target any) error {
	found, err := row.table.column(column)
	if err != nil {
		return err
	}
	cell := struct {
		Value json.RawMessage `json:"value"`
	}{}
	if raw, ok := row.values[found.ID]; !ok || json.Unmarshal(raw, &cell) != nil || len(cell.Value) == 0 || string(cell.Value) == "null" {
		return nil
	}
	return json.Unmarshal(cell.Value, target)
}

// Text is a text, title, URL, email or phone column's value, or a
// formula's or rollup's as text.
func (row Row) Text(column string) string {
	found, _ := row.table.Column(column)
	if raw, ok := row.values[found.ID]; ok {
		computed := struct {
			Text *string `json:"text"`
		}{}
		if json.Unmarshal(raw, &computed) == nil && computed.Text != nil {
			return *computed.Text
		}
	}
	var text string
	_ = row.Value(column, &text)
	return text
}

// Number is a number column's value (0 when empty).
func (row Row) Number(column string) float64 {
	var number float64
	_ = row.Value(column, &number)
	return number
}

// Checkbox is a checkbox column's value.
func (row Row) Checkbox(column string) bool {
	var checked bool
	_ = row.Value(column, &checked)
	return checked
}

// Option is a select or status column's option name ("" when empty).
func (row Row) Option(column string) string {
	names := row.Options(column)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// Options are a multi-select column's option names.
func (row Row) Options(column string) []string {
	found, _ := row.table.Column(column)
	var ids []string
	var single string
	if row.Value(column, &ids) != nil {
		_ = row.Value(column, &single)
		ids = []string{single}
	}
	names := map[string]string{}
	for _, option := range found.Options() {
		names[option.ID] = option.Name
	}
	result := []string{}
	for _, id := range ids {
		if name, ok := names[id]; ok {
			result = append(result, name)
		}
	}
	return result
}

// IDs are a person or relation column's people or rows.
func (row Row) IDs(column string) []string {
	ids := []string{}
	_ = row.Value(column, &ids)
	return ids
}

// Date is a date column's value (empty Start when there is none).
func (row Row) Date(column string) Date {
	date := Date{}
	_ = row.Value(column, &date)
	return date
}
