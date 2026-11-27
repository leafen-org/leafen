package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
)

// A database title or text value Notion formatted keeps its formatting,
// links, equations and mentions as the cell's spans.

// notionImportRichTextCell reads a Notion title or text value into its text
// and the spans that keep its formatting, links, equations and mentions. A
// page or database mention keeps its Notion object until the import is
// published, and a person mention keeps the imported person until people are
// mapped, as block text does. It reports false when something can't be kept:
// a mention with no native counterpart stays its text, a template's "when
// duplicated" mention too, which only a template's body keeps (a value's
// cell holds none), and a value whose
// formatting a cell can't hold keeps only its text. A title's spans hold
// words as long as a title (validGraphTitleSpans).
func notionImportRichTextCell(parts []notionProviderRichText, title bool) (string, []graphTextSpan, bool) {
	text, formatted := notionImportPlainRichText(parts)
	if !formatted {
		return text, nil, true
	}
	kept := true
	for _, part := range parts {
		switch part.Type {
		case "", "text", "equation":
		case "mention":
			kept = kept && part.Mention.Type != "template_mention" &&
				(part.Mention.Type == "user" || notionProviderMentionReference(part.Mention) != nil)
		default:
			kept = false
		}
	}
	plan, err := notionProviderRichTextSourcePlan(parts, false)
	if err != nil {
		return text, nil, false
	}
	kept = kept && len(plan.Omitted) == 0
	spans := make([]graphTextSpan, 0, len(plan.Spans))
	for _, source := range plan.Spans {
		span, err := notionSourceGraphSpan(source)
		if err != nil {
			return text, nil, false
		}
		if span.Mention == graphTemplateMention {
			span = graphTextSpan{Text: span.Text, Bold: span.Bold, Italic: span.Italic, Underline: span.Underline,
				Strikethrough: span.Strikethrough, Code: span.Code, Color: span.Color, Background: span.Background}
		}
		spans = append(spans, span)
	}
	spans = notionImportTidySpans(spans)
	if len(spans) > 0 && (title && !validGraphTitleSpans(spans) || !title && !validGraphBlockCaption(spans)) {
		return text, nil, false
	}
	return plan.Text, spans, kept
}

// notionImportTidySpans is the one rule imported formatted text keeps its
// spans by, however it came to be (read from Notion, or a mention whose
// target the import did not bring made plain words later): words left with
// no formatting at all keep no spans, as text Notion never formatted, and
// formatted ones keep their pieces as Notion gave them, as a text value
// reads back (admitGraphRichTextSpans).
func notionImportTidySpans(spans []graphTextSpan) []graphTextSpan {
	for _, span := range spans {
		if span != (graphTextSpan{Text: span.Text}) {
			return spans
		}
	}
	return nil
}

// notionImportFittedCell cuts a row's title or text to the characters its
// cell holds (a title's maxGraphTitleRunes, a text's maxGraphTextCharacters)
// and names what it left out, when it had to: a title shortened, a text
// truncated.
func notionImportFittedCell(text string, spans []graphTextSpan, title bool) (string, []graphTextSpan, string) {
	bound, shortened := maxGraphTextCharacters, notionImportIssuePropertyValueTruncated
	if title {
		bound, shortened = maxGraphTitleRunes, notionImportIssuePropertyTitleShortened
	}
	runes := []rune(text)
	if len(runes) <= bound {
		return text, spans, ""
	}
	kept, left := make([]graphTextSpan, 0, len(spans)), bound
	for _, span := range spans {
		if left <= 0 {
			break
		}
		spanRunes := []rune(span.Text)
		if len(spanRunes) > left {
			spanRunes = spanRunes[:left]
			span.Text = string(spanRunes)
		}
		left -= len(spanRunes)
		kept = append(kept, span)
	}
	return string(runes[:bound]), kept, shortened
}

func notionImportPlainRichText(parts []notionProviderRichText) (string, bool) {
	var text strings.Builder
	formatted := false
	for _, part := range parts {
		text.WriteString(part.PlainText)
		annotations := part.Annotations
		color := strings.TrimSpace(annotations.Color)
		if part.Type != "" && part.Type != "text" || part.Text.Link != nil ||
			annotations.Bold || annotations.Italic || annotations.Strikethrough || annotations.Underline ||
			annotations.Code || color != "" && color != "default" {
			formatted = true
		}
	}
	return text.String(), formatted
}

type notionImportPendingCell struct {
	RowID           string `db:"row_id"`
	PropertyID      string `db:"property_id"`
	DataSourceID    string `db:"data_source_id"`
	Position        string `db:"position"`
	SubitemParentID string `db:"subitem_parent_id"`
	Type            string `db:"type"`
	Value           string `db:"value"`
	Spans           string `db:"spans"`
}

// resolveNotionImportRowTextReferences points the pending mentions in the
// formatted text of the database rows a page holds at what the import
// brought in. A mention whose target the import did not bring stays its
// text, and its property reports the formatting it lost.
func resolveNotionImportRowTextReferences(
	jobID, pageID string,
	resolver *notionImportReferenceResolver,
) ([]graphOperation, error) {
	app := resolver.app
	cells := []notionImportPendingCell{}
	if err := app.DB().NewQuery(`
		SELECT [[value]].[[row_id]],[[value]].[[property_id]],[[row]].[[data_source_id]],[[row]].[[position]],
			[[row]].[[subitem_parent_id]],[[value]].[[type]],[[value]].[[value]],[[value]].[[spans]]
		FROM {{leafen_database_row_values}} [[value]] JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]]=[[value]].[[row_id]]
		WHERE [[value]].[[page_id]]={:pageId}
			AND (instr([[value]].[[spans]], {:page})>0 OR instr([[value]].[[spans]], {:database})>0
				OR instr([[value]].[[spans]], {:link})>0)
		ORDER BY [[value]].[[row_id]],[[value]].[[property_id]]
	`).Bind(dbx.Params{
		"pageId": pageID, "link": `"` + notionImportPendingPageLinkPrefix,
		"page": `"` + notionImportPendingPagePrefix, "database": `"` + notionImportPendingDatabasePrefix,
	}).All(&cells); err != nil {
		return nil, err
	}
	rows := map[string]*graphDatabaseRow{}
	order := []string{}
	lostProperties := map[string]struct{}{}
	for _, cell := range cells {
		spans := []graphTextSpan{}
		if json.Unmarshal([]byte(cell.Spans), &spans) != nil {
			return nil, errors.New(errorCodeNotionImportCreateFailed)
		}
		lost, err := resolveNotionImportSpans(spans, resolver)
		if err != nil {
			return nil, err
		}
		// A cell whose spans hold no formatting left keeps none, as the first
		// read would have.
		spans = notionImportTidySpans(spans)
		if lost {
			lostProperties[cell.PropertyID] = struct{}{}
		}
		row := rows[cell.RowID]
		if row == nil {
			var parent *string
			if cell.SubitemParentID != "" {
				value := cell.SubitemParentID
				parent = &value
			}
			row = &graphDatabaseRow{
				ID: cell.RowID, DataSourceID: cell.DataSourceID, Position: cell.Position,
				SubitemParentID: parent, Values: map[string]graphRowValue{},
			}
			rows[cell.RowID] = row
			order = append(order, cell.RowID)
		}
		row.Values[cell.PropertyID] = graphRowValue{Type: cell.Type, Value: json.RawMessage(cell.Value), Spans: spans}
	}
	for propertyID := range lostProperties {
		propertySource := ""
		err := app.DB().NewQuery(`
			SELECT [[source_id]] FROM {{leafen_notion_import_mappings}}
			WHERE [[job_id]]={:jobId} AND [[native_id]]={:propertyId} AND [[kind]]='property'
		`).Bind(dbx.Params{"jobId": jobID, "propertyId": propertyID}).Row(&propertySource)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := recordNotionImportPropertyIssue(
			app, jobID, propertySource, notionImportIssuePropertyFormattingOmitted, false,
		); err != nil {
			return nil, err
		}
	}
	operations := make([]graphOperation, 0, len(order))
	for _, rowID := range order {
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: rows[rowID]})
	}
	return operations, nil
}

// resolveNotionImportSpans points the pending mentions and page links in
// formatted text — a title, a text value — at what the import brought in,
// and reports whether one named what it did not bring. Such a mention keeps
// its words, linked to it in Notion, as a line's does
// (notion_import_link_outside.go; 2026-11-22: a text value's mention of a
// page the move didn't bring came as its words alone, its address gone,
// while the same mention in a line linked to Notion); such a link to a block
// it can't find goes.
func resolveNotionImportSpans(spans []graphTextSpan, resolver *notionImportReferenceResolver) (bool, error) {
	lost := false
	for index := range spans {
		span := &spans[index]
		if notionImportPendingPageLink(span.Link) {
			target, err := resolver.resolveLink(span.Link)
			if err != nil {
				return false, err
			}
			span.Link = target
			lost = lost || target == ""
		}
		attribute, pending := "pageId", span.PageID
		if span.Mention == "database" {
			attribute, pending = "databaseId", span.DatabaseID
		}
		if !notionImportPendingReference(attribute, pending) {
			continue
		}
		anchor := span.BlockID
		nativeID, blockID, err := resolver.resolveMention(attribute, pending, anchor)
		if err != nil {
			return false, err
		}
		span.BlockID = blockID
		switch {
		case nativeID == "":
			span.Mention, span.PageID, span.DatabaseID, span.BlockID = "", "", "", ""
			span.Link = notionImportOutsideAddress(pending, anchor)
			// The report still says the value lost a mention.
			lost = true
		case attribute == "pageId":
			span.PageID = nativeID
		default:
			span.DatabaseID = nativeID
			// A data source's takes its name (notion_import_data_source_resolve.go).
			if words, named := notionImportSourceMentionWords(resolver.app, attribute, nativeID); named {
				span.Text = words
			}
		}
	}
	return lost, nil
}
