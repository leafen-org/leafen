package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A template's text property values keep their formatting as a row's text
// cells do: the plain text is the value every reader reads, and the spans
// — bold, links, mentions, the template's "when duplicated" mentions — sit
// beside it, by property, only while they spell that text. A page made from
// the template takes both, its template mentions given their value.

// databaseTemplateTextSpans keeps, of the spans given by property, those of
// a text property whose value in the template's values they spell, as the
// JSON the template stores. When strict, spans that don't fit refuse the
// change instead of being left out.
func databaseTemplateTextSpans(
	textProperties map[string]bool, propertiesJSON string, spansJSON []byte, strict bool,
) (string, error) {
	if len(spansJSON) == 0 {
		return "{}", nil
	}
	given := map[string][]graphTextSpan{}
	if json.Unmarshal(spansJSON, &given) != nil {
		return "", databaseToolbarRefused("propertySpans is an object of text spans by property ID")
	}
	values := map[string]json.RawMessage{}
	if json.Unmarshal([]byte(propertiesJSON), &values) != nil {
		return "", databaseToolbarRefused("properties is an object of values by property ID")
	}
	kept := map[string][]graphTextSpan{}
	for propertyID, spans := range given {
		value, exists := values[propertyID]
		if !exists || !textProperties[propertyID] {
			if strict && len(spans) > 0 {
				return "", databaseToolbarRefused("propertySpans.%s names no text property with a value in properties", propertyID)
			}
			continue
		}
		normalized, err := normalizeDatabaseTemplateTextSpans(value, spans)
		if err != nil {
			if strict {
				return "", databaseToolbarRefused("propertySpans.%s: %s", propertyID,
					firstNonempty(refusalReason(err), "the spans don't spell the property's value"))
			}
			continue
		}
		if len(normalized) > 0 {
			kept[propertyID] = normalized
		}
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// normalizeDatabaseTemplateTextSpans admits a template text value's spans as
// a text cell's are, and the template's "when duplicated" mentions besides:
// they spell the value, and spans that only repeat it keep nothing.
func normalizeDatabaseTemplateTextSpans(value json.RawMessage, spans []graphTextSpan) ([]graphTextSpan, error) {
	if len(spans) == 0 {
		return nil, nil
	}
	var text string
	if !graphRichTextFits(spans, maxGraphTextCharacters) {
		return nil, databaseToolbarRefused("the spans are over %d characters", maxGraphTextCharacters)
	}
	if json.Unmarshal(value, &text) != nil {
		return nil, databaseToolbarRefused("the property's value in properties isn't text")
	}
	var spelled strings.Builder
	formatted := false
	for _, span := range spans {
		if span.Mention == graphTemplateMention {
			if !validGraphTemplateMention(span.Template) || span.Link != "" || span.CommentID != "" ||
				span.PageID != "" || span.BlockID != "" || span.DatabaseID != "" || span.CustomEmojiID != "" || span.UserID != "" ||
				span.Date != "" || span.DateEnd != "" || span.DateTimeZone != "" || span.DateReminder != "" ||
				span.IconURL != "" || !validGraphLinkMentionPreview(span) || span.Equation != "" || span.Color != "" && !validNotionBlockColor(span.Color) ||
				span.Background != "" && !validNotionSpanBackground(span.Background) ||
				span.Suggestion != "" || span.SuggestionID != "" || span.SuggestionBefore != "" {
				return nil, databaseToolbarRefused("a template mention span holds only template (now, today or me), text, color and background")
			}
		} else if span.Template != "" {
			return nil, databaseToolbarRefused("a span with template is a template mention: its mention is template")
		} else if !validGraphContentTextSpan(span) {
			return nil, databaseToolbarRefused("a span %q isn't one a text value holds", span.Text)
		}
		spelled.WriteString(span.Text)
		formatted = formatted || span != graphTextSpan{Text: span.Text}
	}
	if spelled.String() != text {
		return nil, databaseToolbarRefused("the spans' text doesn't spell the property's value in properties")
	}
	if !formatted {
		return nil, nil
	}
	return spans, nil
}

// databaseTemplateTextProperties names the text properties of the data
// source a template belongs to, found by their page and source; the owner
// is read after (+), or SQLite, which keeps no statistics, reads the owner's
// every property.
func databaseTemplateTextProperties(app core.App, authority databaseToolbarConfigAuthority) (map[string]bool, error) {
	ids := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_properties}}
		WHERE +[[owner]]={:owner} AND [[page_id]]={:pageId} AND [[data_source_id]]={:dataSourceId}
			AND [[type]]={:type}`).Bind(dbx.Params{
		"owner": authority.OwnerID, "pageId": authority.PageID, "dataSourceId": authority.DataSourceID,
		"type": graphValueTypeRichText,
	}).Column(&ids); err != nil {
		return nil, err
	}
	text := make(map[string]bool, len(ids))
	for _, id := range ids {
		text[id] = true
	}
	return text, nil
}

// databaseTemplateUsedText gives a text value's spans their template
// mentions' values and the value the resolved spans spell.
func databaseTemplateUsedText(use databaseTemplateUse, spans []graphTextSpan) (json.RawMessage, []graphTextSpan) {
	resolved := use.resolve(append([]graphTextSpan(nil), spans...))
	var text strings.Builder
	for _, span := range resolved {
		text.WriteString(span.Text)
	}
	value, _ := json.Marshal(text.String())
	return value, resolved
}
