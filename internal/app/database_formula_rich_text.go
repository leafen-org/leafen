package app

import (
	"encoding/json"
	"strings"
)

// A formula's rich text is text with formatting, links and mentions, as
// Notion's formulas write it: style() and link() format text, and a date,
// page or person joined to text is a mention of it. Its spans are those a
// title or text cell keeps: a date or person mention's text starts with "@",
// a page mention's is the page's title. Functions that read text read it
// without the "@"; the cell and the Public API show every mention with it,
// and length() counts a mention as one character. Text without formatting,
// links or mentions stays plain text.

type databaseFormulaRichText []graphTextSpan

// databaseFormulaStyleColors are the colors style() writes, and their
// backgrounds.
var databaseFormulaStyleColors = map[string]bool{
	"gray": true, "brown": true, "orange": true, "yellow": true, "green": true, "blue": true, "purple": true,
	"pink": true, "red": true,
}

// spans is a value as rich text spans: a date, page or person as a mention,
// a list's items between commas, anything else as its text.
func (environment *databaseFormulaEnvironment) spans(value any) []graphTextSpan {
	switch typed := value.(type) {
	case databaseFormulaRichText:
		return typed
	case databaseFormulaDate:
		start, end, _ := strings.Cut(typed.Text, "/")
		return []graphTextSpan{{
			Text: "@" + environment.dateText(typed), Mention: "date", Date: start, DateEnd: end, DateTimeZone: typed.Zone,
		}}
	case databaseFormulaReference:
		if typed.Kind == "person" {
			return []graphTextSpan{{Text: "@" + typed.text(), Mention: "person", UserID: typed.ID}}
		}
		// An untitled page's mention reads "New page", as Notion's API serves
		// one joined to text ("@New page", seen 2026-11-12), in the reader's
		// language.
		name := typed.Name
		if name == "" {
			name = environment.locale().untitledPage
		}
		return []graphTextSpan{{Text: name, Mention: "page", PageID: typed.ID}}
	case []any:
		var spans []graphTextSpan
		for index, item := range typed {
			if index > 0 {
				spans = append(spans, graphTextSpan{Text: ","})
			}
			spans = append(spans, environment.spans(item)...)
		}
		return spans
	}
	return []graphTextSpan{{Text: databaseFormulaText(value)}}
}

func databaseFormulaSameMarks(left, right graphTextSpan) bool {
	left.Text, right.Text = "", ""
	return left == right
}

// databaseFormulaRich is spans as a value: rich text when a span is
// formatted, a link or a mention, else its plain text. Neighbouring text
// spans alike are one span, and empty text spans are left out.
func databaseFormulaRich(spans []graphTextSpan) any {
	var merged databaseFormulaRichText
	rich := false
	for _, span := range spans {
		if span.Mention == "" && span.Text == "" {
			continue
		}
		if last := len(merged) - 1; span.Mention == "" && last >= 0 && merged[last].Mention == "" &&
			databaseFormulaSameMarks(merged[last], span) {
			merged[last].Text += span.Text
			continue
		}
		merged = append(merged, span)
		rich = rich || span != graphTextSpan{Text: span.Text}
	}
	if !rich {
		return merged.plain()
	}
	return merged
}

// plain is the text functions read: a mention without its "@".
func (text databaseFormulaRichText) plain() string {
	var result strings.Builder
	for _, span := range text {
		if span.Mention == "date" || span.Mention == "person" {
			result.WriteString(strings.TrimPrefix(span.Text, "@"))
		} else {
			result.WriteString(span.Text)
		}
	}
	return result.String()
}

// shown is the text the cell and the Public API show: every mention with
// its "@".
func (text databaseFormulaRichText) shown() string {
	var result strings.Builder
	for _, span := range text {
		if span.Mention == "page" {
			result.WriteString("@")
		}
		result.WriteString(span.Text)
	}
	return result.String()
}

// length counts a mention as one character, and the text between mentions
// by its characters (database_formula_characters.go).
func (text databaseFormulaRichText) length() int {
	length, run := 0, ""
	for _, span := range text {
		if span.Mention != "" {
			length += databaseFormulaCharacterCount(run) + 1
			run = ""
		} else {
			run += span.Text
		}
	}
	return length + databaseFormulaCharacterCount(run)
}

// mapText writes each text span's text anew; mentions stay.
func (text databaseFormulaRichText) mapText(write func(string) string) any {
	spans := make([]graphTextSpan, len(text))
	for index, span := range text {
		if span.Mention == "" {
			span.Text = write(span.Text)
		}
		spans[index] = span
	}
	return databaseFormulaRich(spans)
}

// style formats a value: b, i, u, s and c for bold, italic, underline,
// strikethrough and code, and a color or a color's background; other names
// are passed over, and a later color wins.
func (environment *databaseFormulaEnvironment) style(values []any) any {
	if len(values) == 0 {
		return ""
	}
	spans := append([]graphTextSpan(nil), environment.spans(values[0])...)
	for _, value := range values[1:] {
		name, _ := value.(string)
		for index := range spans {
			span := &spans[index]
			switch {
			case name == "b":
				span.Bold = true
			case name == "i":
				span.Italic = true
			case name == "u":
				span.Underline = true
			case name == "s":
				span.Strikethrough = true
			case name == "c":
				span.Code = true
			case strings.HasSuffix(name, "_background") && databaseFormulaStyleColors[strings.TrimSuffix(name, "_background")]:
				// A text color and a background go together, as in Notion.
				span.Color, span.Background = textSpanColors(span.Color, span.Background)
				span.Background = name
			case databaseFormulaStyleColors[name]:
				span.Color, span.Background = textSpanColors(span.Color, span.Background)
				span.Color = name
			}
		}
	}
	return databaseFormulaRich(spans)
}

// unstyle takes the named formatting away, or all of it when none is named;
// links and mentions stay.
func (environment *databaseFormulaEnvironment) unstyle(values []any) any {
	if len(values) == 0 {
		return ""
	}
	spans := append([]graphTextSpan(nil), environment.spans(values[0])...)
	for index := range spans {
		span := &spans[index]
		if len(values) == 1 {
			span.Bold, span.Italic, span.Underline, span.Strikethrough, span.Code, span.Color, span.Background = false, false, false, false, false, "", ""
			continue
		}
		for _, value := range values[1:] {
			switch name, _ := value.(string); {
			case name == "b":
				span.Bold = false
			case name == "i":
				span.Italic = false
			case name == "u":
				span.Underline = false
			case name == "s":
				span.Strikethrough = false
			case name == "c":
				span.Code = false
			case name == span.Color:
				span.Color = ""
			case name == span.Background:
				span.Background = ""
			}
		}
	}
	return databaseFormulaRich(spans)
}

// link makes a value's text a link; mentions stay what they are.
func (environment *databaseFormulaEnvironment) link(label, url any) any {
	spans := append([]graphTextSpan(nil), environment.spans(label)...)
	address := databaseFormulaText(url)
	for index := range spans {
		if spans[index].Mention == "" && address != "" {
			spans[index].Link = address
		}
	}
	return databaseFormulaRich(spans)
}

// trim takes white space from the ends of rich text's outer text spans.
func (text databaseFormulaRichText) trim() any {
	spans := append([]graphTextSpan(nil), text...)
	if len(spans) > 0 && spans[0].Mention == "" {
		spans[0].Text = strings.TrimLeftFunc(spans[0].Text, databaseFormulaTrimmed)
	}
	if last := len(spans) - 1; last >= 0 && spans[last].Mention == "" {
		spans[last].Text = strings.TrimRightFunc(spans[last].Text, databaseFormulaTrimmed)
	}
	return databaseFormulaRich(spans)
}

// repeat writes rich text count times, whole: when the copies' text would
// pass a value's, the formula fails before they are written.
func (text databaseFormulaRichText) repeat(count int) (any, error) {
	units := 0
	for _, span := range text {
		units += databaseFormulaTextLength(span.Text) + databaseFormulaTextLength(span.Link)
	}
	if units*count > maxDatabaseFormulaValueText {
		return nil, errDatabaseFormulaInvalid
	}
	spans := make([]graphTextSpan, 0, len(text)*count)
	for range count {
		spans = append(spans, text...)
	}
	return databaseFormulaRich(spans), nil
}

// databaseFormulaStoredRichText reads a stored {"rich_text": spans} value.
func databaseFormulaStoredRichText(object map[string]any) (databaseFormulaRichText, bool) {
	raw, ok := object["rich_text"]
	if !ok {
		return nil, false
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var spans []graphTextSpan
	if json.Unmarshal(encoded, &spans) != nil {
		return nil, false
	}
	return spans, true
}
