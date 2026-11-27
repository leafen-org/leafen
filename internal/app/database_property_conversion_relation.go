package app

import (
	"encoding/json"
	"strings"
)

// A property made a relation reads its cells' rows by what they name, and a
// relation made text writes its pages as mentions.

// RelationFrom gives a cell made a relation the rows of the related data
// source its value names, as Notion's (seen 2026-11-07: a relation made text
// and made a relation again took its pages back): the rows its page mentions
// point at, then the rows whose titles its comma-separated words are. What
// names no such row is not kept, so no cell is left holding what a relation
// can't read.
func (conversion *databasePropertyConversion) RelationFrom(raw, spans json.RawMessage) (json.RawMessage, error) {
	if conversion.relationSource == "" || conversion.names.Rows == nil || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if conversion.relationRows == nil {
		conversion.relationRows, conversion.relationTitles = map[string]struct{}{}, map[string]string{}
		for _, row := range conversion.names.Rows(conversion.relationSource) {
			conversion.relationRows[row.ID] = struct{}{}
			if _, taken := conversion.relationTitles[row.Title]; row.Title != "" && !taken {
				conversion.relationTitles[row.Title] = row.ID
			}
		}
	}
	ids, seen := []string{}, map[string]bool{}
	add := func(id string) {
		if _, known := conversion.relationRows[id]; known && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	parsed := []graphTextSpan{}
	_ = json.Unmarshal(spans, &parsed)
	var words strings.Builder
	for _, span := range parsed {
		if span.Mention == "page" {
			add(span.PageID)
		} else {
			words.WriteString(span.Text)
		}
	}
	text := words.String()
	if len(parsed) == 0 {
		text, _ = conversion.sourceText(raw)
	}
	for _, word := range strings.Split(text, ",") {
		add(conversion.relationTitles[strings.TrimSpace(word)])
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return json.Marshal(ids)
}

// relatedPages names the pages a relation's value holds, those with a title.
func (conversion *databasePropertyConversion) relatedPages(raw json.RawMessage) []string {
	ids := []string{}
	names := []string{}
	if json.Unmarshal(raw, &ids) != nil || conversion.names.Page == nil {
		return names
	}
	for _, id := range ids {
		if name := conversion.names.Page(id); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// relationSpans gives a relation made text page mentions joined by ", ".
func (conversion *databasePropertyConversion) relationSpans(raw json.RawMessage) []graphTextSpan {
	ids := []string{}
	spans := []graphTextSpan{}
	if json.Unmarshal(raw, &ids) != nil || conversion.names.Page == nil {
		return nil
	}
	for _, id := range ids {
		name := conversion.names.Page(id)
		if name == "" {
			continue
		}
		if len(spans) > 0 {
			spans = append(spans, graphTextSpan{Text: ", "})
		}
		spans = append(spans, graphTextSpan{Text: name, Mention: "page", PageID: id})
	}
	return spans
}
