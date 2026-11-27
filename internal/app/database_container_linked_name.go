package app

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// validateLinkedDatabaseName holds a linked database's own name and icon to
// a database's: a linked database has them on its block, apart from its
// source's, as Notion's linked databases do (seen 2026-10-15); another
// database has none. The name keeps its formatting beside it, spelling it,
// as a data source's name does (data_source_title_spans.go).
func validateLinkedDatabaseName(raw map[string]json.RawMessage, content databaseContainerBlockContent) error {
	title, titled := raw["linkedTitle"]
	spans, formatted := raw["linkedTitleSpans"]
	icon, iconed := raw["linkedIcon"]
	iconType, typed := raw["linkedIconType"]
	if err := validateDatabasePageNameSpans(raw); err != nil {
		return err
	}
	if !titled && !formatted && !iconed && !typed {
		return nil
	}
	if !content.LinkedDatabaseSource {
		return graphInputError(errorCodeGraphInvalid, "Only a linked database has a name of its own.")
	}
	if titled || formatted {
		var text string
		if !titled || json.Unmarshal(title, &text) != nil || text == "" || strings.TrimSpace(text) != text ||
			utf8.RuneCountInString(text) > maxGraphTitleRunes {
			return graphInputError(errorCodeGraphInvalid, "Check the linked database's name.")
		}
		if formatted && !validBlockNameSpans(spans, text) {
			return graphInputError(errorCodeGraphInvalid, "Check the linked database name's formatting.")
		}
	}
	if iconed || typed {
		var glyph, kind string
		if !iconed || !typed || json.Unmarshal(icon, &glyph) != nil || json.Unmarshal(iconType, &kind) != nil {
			return graphInputError(errorCodeGraphInvalid, "Check the linked database's icon.")
		}
		shape := graphDataSource{Icon: &glyph, IconType: &kind}
		if normalizeGraphDataSourceIcon(&shape) != nil || *shape.Icon != glyph || *shape.IconType != kind ||
			kind == graphDataSourceIconNone {
			return graphInputError(errorCodeGraphInvalid, "Check the linked database's icon.")
		}
	}
	return nil
}

// validBlockNameSpans is whether a name's formatting kept in a block's
// content spells the name with formatting a database's name takes.
func validBlockNameSpans(raw json.RawMessage, title string) bool {
	spans := []graphTextSpan{}
	if json.Unmarshal(raw, &spans) != nil || len(spans) == 0 {
		return false
	}
	encoded, _ := json.Marshal(title)
	normalized, err := normalizeGraphNameSpans(encoded, spans)
	return err == nil && normalized != nil
}

// validateDatabasePageNameSpans holds the name a full-page database's page
// keeps for it as the editor writes it (database_page_name_spans.go) —
// spans spelling it, with or without formatting, or none for an empty name —
// to spans a database's name takes; whether they still spell the page's
// title is read where they are shown.
func validateDatabasePageNameSpans(raw map[string]json.RawMessage) error {
	spans, present := raw["pageTitleSpans"]
	if !present {
		return nil
	}
	parsed := []graphTextSpan{}
	if json.Unmarshal(spans, &parsed) != nil {
		return graphInputError(errorCodeGraphInvalid, "Check the database name's formatting.")
	}
	if len(parsed) == 0 {
		return nil
	}
	spelled := spelledName(parsed)
	encoded, _ := json.Marshal(spelled)
	if _, err := normalizeGraphNameSpans(encoded, parsed); err != nil ||
		utf8.RuneCountInString(spelled) > maxGraphTitleRunes || strings.TrimSpace(spelled) != spelled {
		return graphInputError(errorCodeGraphInvalid, "Check the database name's formatting.")
	}
	return nil
}
