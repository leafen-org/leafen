package app

import (
	"bytes"
)

// mcpResultRow is a row of a query's answer as Notion's MCP writes it, its
// keys in the order its columns come (seen 2026-11-11): a SQL row's in its
// SELECT's order, a rows-mode row's in its table's then its url — where a
// map is written by name.
type mcpResultRow struct {
	keys   []string
	values map[string]any
}

// set gives a key its value; a new key goes after those there.
func (row *mcpResultRow) set(key string, value any) {
	if row.values == nil {
		row.values = map[string]any{}
	}
	if _, present := row.values[key]; !present {
		row.keys = append(row.keys, key)
	}
	row.values[key] = value
}

func (row mcpResultRow) MarshalJSON() ([]byte, error) {
	var written bytes.Buffer
	written.WriteByte('{')
	for index, key := range row.keys {
		if index > 0 {
			written.WriteByte(',')
		}
		// < and > as they are, as fetch writes them (mcpFetchJSON).
		name, err := mcpFetchJSON(key)
		if err != nil {
			return nil, err
		}
		value, err := mcpFetchJSON(row.values[key])
		if err != nil {
			return nil, err
		}
		written.WriteString(name)
		written.WriteByte(':')
		written.WriteString(value)
	}
	written.WriteByte('}')
	return written.Bytes(), nil
}
