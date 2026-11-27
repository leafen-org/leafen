package app

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

// An action's words — a notification's, a mail's, Slack's or a bot's
// message — are written out, or one formula, as Notion's message box takes
// one with Σ (seen 2026-10-22): computed when the action runs from the page
// that triggered it, the run's names (executionActor, pageCreator,
// executionTime, executionDate, triggerPage) and its variables, and sent as
// text as format() writes it. Written words may hold mentions, as Notion's
// "@" puts them in (seen 2026-10-23): a person, a page or a date, and what
// the run gives — the trigger page, one of its properties, who ran it, when,
// a variable — as the formula that reads it. Words keep their marks — bold,
// italic, underline, strikethrough, code and a link — as Notion's message
// box keeps those typed or pasted (seen 2026-11-11). A notification and a
// button's confirmation show the mentions and marks; a mail, Slack and a bot
// read their text. A formula keeps the words it was made from, which come
// back when it is made words again.

const interactiveAutomationMaxMessageSpans = 200

// How a date mention shows its date (the editor's DATE_MENTION_FORMATS).
var automationMessageDateFormats = map[string]bool{"full": true, "short": true, "month_day_year": true, "day_month_year": true,
	"year_month_day": true, "relative": true}

var automationMessageDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d{1,3})?)?(?:Z|[+-]\d{2}:\d{2})?)?$`)

// normalizeAutomationMessage checks an action's words; with a formula, its
// written words go, but for the mentions it was made from.
func normalizeAutomationMessage(action *interactiveAutomationAction) bool {
	action.Message, action.MessageFormula = strings.TrimSpace(action.Message), strings.TrimSpace(action.MessageFormula)
	if len(action.MessageSpans) > 0 {
		text, ok := normalizeAutomationMessageSpans(action.MessageSpans)
		if !ok {
			return false
		}
		action.Message = text
	}
	if action.MessageFormula == "" {
		return boundedAutomationString(action.Message, interactiveAutomationMaxMessageRunes)
	}
	action.Message = ""
	formula := interactiveAutomationValueExpression{Kind: "formula", Expression: action.MessageFormula}
	return normalizeInteractiveAutomationFormula(&formula) == nil
}

// normalizeAutomationMessageSpans checks words with mentions in place and
// gives their text.
func normalizeAutomationMessageSpans(spans []interactiveAutomationMessageSpan) (string, bool) {
	if len(spans) > interactiveAutomationMaxMessageSpans {
		return "", false
	}
	var text strings.Builder
	for index := range spans {
		span := &spans[index]
		if !utf8.ValidString(span.Text) {
			return "", false
		}
		kept := interactiveAutomationMessageSpan{Text: span.Text, Mention: span.Mention, Bold: span.Bold, Italic: span.Italic,
			Underline: span.Underline, Strikethrough: span.Strikethrough, Code: span.Code, Link: span.Link}
		if kept.Link != "" && (!validGraphHeldTextLink(kept.Link) || !boundedAutomationString(kept.Link, interactiveAutomationMaxMessageRunes)) {
			return "", false
		}
		switch span.Mention {
		case "":
		case "person":
			kept.UserID = span.UserID
			if !boundedAutomationString(kept.UserID, interactiveAutomationMaxIdentifierRunes) {
				return "", false
			}
		case "page":
			kept.PageID = span.PageID
			if !boundedAutomationString(kept.PageID, interactiveAutomationMaxIdentifierRunes) {
				return "", false
			}
		case "date":
			kept.Date, kept.DateEnd, kept.DateTimeZone = span.Date, span.DateEnd, span.DateTimeZone
			if automationMessageDateFormats[span.DateFormat] {
				kept.DateFormat = span.DateFormat
			}
			if !automationMessageDatePattern.MatchString(kept.Date) || (kept.DateEnd != "" && !automationMessageDatePattern.MatchString(kept.DateEnd)) ||
				(kept.DateTimeZone != "" && !boundedAutomationString(kept.DateTimeZone, interactiveAutomationMaxIdentifierRunes)) {
				return "", false
			}
		case "automation":
			formula := interactiveAutomationValueExpression{Kind: "formula", Expression: span.Automation}
			if normalizeInteractiveAutomationFormula(&formula) != nil ||
				(span.AutomationType != "" && !boundedAutomationString(span.AutomationType, interactiveAutomationMaxIdentifierRunes)) {
				return "", false
			}
			kept.Automation, kept.AutomationType = formula.Expression, span.AutomationType
		default:
			return "", false
		}
		*span = kept
		text.WriteString(span.Text)
	}
	return strings.TrimSpace(text.String()), true
}

// normalizeAutomationTextValue checks words with mentions written as a
// value — a text variable's, as Notion's "@" puts them in (seen
// 2026-10-25) — and keeps their text as the value; words without a mention
// are their text alone.
func normalizeAutomationTextValue(value *interactiveAutomationValueExpression) error {
	var written string
	if json.Unmarshal(value.Value, &written) != nil {
		return automationRefused("value is the text its spans spell")
	}
	if _, ok := normalizeAutomationMessageSpans(value.Spans); !ok {
		return automationRefused("spans are at most %d, each text or a mention of a person, page or date", interactiveAutomationMaxMessageSpans)
	}
	var text strings.Builder
	for _, span := range value.Spans {
		text.WriteString(span.Text)
	}
	if !automationMessageSpansShaped(value.Spans) {
		value.Spans = nil
	}
	value.Value, _ = json.Marshal(text.String())
	return nil
}

// resolveTextValue computes words with mentions as the run gives them, as
// rich text: a person, a page or a date as its mention, and what the run
// gives as a message reads it. Its mentions stay mentions where the value is
// set or said, as a Notion variable's do (seen 2026-10-25).
func (formulas *interactiveAutomationFormulas) resolveTextValue(
	spans []interactiveAutomationMessageSpan, variables map[string]json.RawMessage,
) (json.RawMessage, error) {
	resolved, err := formulas.resolveMessageSpans(interactiveAutomationAction{MessageSpans: spans}, variables)
	if err != nil {
		return nil, err
	}
	text := make([]graphTextSpan, 0, len(resolved.MessageSpans))
	for _, span := range resolved.MessageSpans {
		text = append(text, graphTextSpan{Text: span.Text, Mention: span.Mention, UserID: span.UserID, PageID: span.PageID,
			Date: span.Date, DateEnd: span.DateEnd, DateTimeZone: span.DateTimeZone, Bold: span.Bold, Italic: span.Italic,
			Underline: span.Underline, Strikethrough: span.Strikethrough, Code: span.Code, Link: span.Link})
	}
	return json.Marshal(databaseFormulaOutput(databaseFormulaRich(text), true))
}

// resolveMessage computes an action's words from its formula, or its
// mentions of what the run gives, as the run plans it.
func (formulas *interactiveAutomationFormulas) resolveMessage(
	action interactiveAutomationAction, variables map[string]json.RawMessage,
) (interactiveAutomationAction, error) {
	if action.MessageFormula == "" {
		if len(action.MessageSpans) == 0 {
			return action, nil
		}
		return formulas.resolveMessageSpans(action, variables)
	}
	// A page, person or date the formula gives, alone or joined to words,
	// stays its mention, as Notion's inbox shows one (seen 2026-10-24).
	if raw, err := formulas.evaluateValue(action.MessageFormula, variables); err == nil {
		switch raw.(type) {
		case databaseFormulaRichText, databaseFormulaReference, databaseFormulaDate, []any:
			dateText := formulas.messageDateText(action.MessageFormula, raw, variables)
			if spans := automationMessageSpansOf(raw, dateText); hasAutomationMessageMentions(spans) {
				action.MessageFormula = ""
				action.Message, action.MessageSpans = boundedAutomationMessageSpans(spans)
				return action, nil
			}
		}
	}
	result, err := formulas.evaluateValue("format("+action.MessageFormula+")", variables)
	if err != nil {
		return action, err
	}
	text := ""
	switch value := result.(type) {
	case string:
		text = value
	case databaseFormulaRichText:
		text = value.shown()
	default:
		encoded, _ := json.Marshal(databaseFormulaOutput(value, true))
		text = string(encoded)
	}
	if runes := []rune(strings.TrimSpace(text)); len(runes) > interactiveAutomationMaxMessageRunes {
		text = string(runes[:interactiveAutomationMaxMessageRunes])
	}
	action.Message, action.MessageFormula, action.MessageSpans = strings.TrimSpace(text), "", nil
	return action, nil
}

// messageDateText writes a date as format() writes the formula giving it, in
// the run's time zone.
func (formulas *interactiveAutomationFormulas) messageDateText(
	expression string, value any, variables map[string]json.RawMessage,
) func(any) string {
	if _, date := value.(databaseFormulaDate); date {
		if formatted, err := formulas.evaluateValue("format("+expression+")", variables); err == nil {
			return func(any) string { return databaseFormulaFormat(formatted) }
		}
	}
	return databaseFormulaFormat
}

// automationMessageSpansOf is a value as a message's words: a person or a
// page as its mention, a date as a date's, rich text's mentions as they are,
// anything else as format() writes it.
func automationMessageSpansOf(value any, dateText func(any) string) []interactiveAutomationMessageSpan {
	switch typed := value.(type) {
	case databaseFormulaReference:
		switch typed.Kind {
		case "person":
			return []interactiveAutomationMessageSpan{{Text: "@" + typed.Name, Mention: "person", UserID: typed.ID}}
		case "page":
			return []interactiveAutomationMessageSpan{{Text: typed.Name, Mention: "page", PageID: typed.ID}}
		}
	case databaseFormulaRichText:
		spans := make([]interactiveAutomationMessageSpan, 0, len(typed))
		for _, span := range typed {
			switch span.Mention {
			case "page":
				spans = append(spans, interactiveAutomationMessageSpan{Text: span.Text, Mention: "page", PageID: span.PageID})
			case "person":
				spans = append(spans, interactiveAutomationMessageSpan{Text: span.Text, Mention: "person", UserID: span.UserID})
			case "date":
				spans = append(spans, interactiveAutomationMessageSpan{Text: span.Text, Mention: "date",
					Date: span.Date, DateEnd: span.DateEnd, DateTimeZone: span.DateTimeZone, DateFormat: "full"})
			default:
				if span.Text != "" {
					spans = append(spans, interactiveAutomationMessageSpan{Text: span.Text})
				}
			}
		}
		return spans
	case []any:
		var spans []interactiveAutomationMessageSpan
		for index, item := range typed {
			if index > 0 {
				spans = append(spans, interactiveAutomationMessageSpan{Text: ", "})
			}
			spans = append(spans, automationMessageSpansOf(item, dateText)...)
		}
		return spans
	case databaseFormulaDate:
		start, end, _ := strings.Cut(typed.Text, "/")
		if automationMessageDatePattern.MatchString(start) && (end == "" || automationMessageDatePattern.MatchString(end)) {
			// The day written out, as Notion's inbox shows the run's date (seen 2026-10-23).
			return []interactiveAutomationMessageSpan{{Text: "@" + dateText(typed), Mention: "date",
				Date: start, DateEnd: end, DateTimeZone: typed.Zone, DateFormat: "full"}}
		}
	}
	if text := databaseFormulaFormat(value); text != "" {
		return []interactiveAutomationMessageSpan{{Text: text}}
	}
	return nil
}

// boundedAutomationMessageSpans keeps the words a message holds and gives
// their text.
func boundedAutomationMessageSpans(spans []interactiveAutomationMessageSpan) (string, []interactiveAutomationMessageSpan) {
	var text strings.Builder
	runes := 0
	for index, span := range spans {
		count := utf8.RuneCountInString(span.Text)
		if runes+count > interactiveAutomationMaxMessageRunes {
			spans = spans[:index]
			break
		}
		runes += count
		text.WriteString(span.Text)
	}
	return strings.TrimSpace(text.String()), spans
}

// resolveMessageSpans puts what each mention of the run reads in its place:
// a person or a page as its mention, a date as a date's, anything else as
// format() writes it, and gives the words' text.
func (formulas *interactiveAutomationFormulas) resolveMessageSpans(
	action interactiveAutomationAction, variables map[string]json.RawMessage,
) (interactiveAutomationAction, error) {
	resolved := make([]interactiveAutomationMessageSpan, 0, len(action.MessageSpans))
	for _, span := range action.MessageSpans {
		if span.Mention != "automation" {
			resolved = append(resolved, span)
			continue
		}
		value, err := formulas.evaluateValue(span.Automation, variables)
		if err != nil {
			return action, err
		}
		// What it reads keeps the marks its mention had.
		for _, read := range automationMessageSpansOf(value, formulas.messageDateText(span.Automation, value, variables)) {
			read.Bold, read.Italic, read.Underline = read.Bold || span.Bold, read.Italic || span.Italic, read.Underline || span.Underline
			read.Strikethrough, read.Code = read.Strikethrough || span.Strikethrough, read.Code || span.Code
			if read.Link == "" && read.Mention == "" {
				read.Link = span.Link
			}
			resolved = append(resolved, read)
		}
	}
	action.Message, action.MessageSpans = boundedAutomationMessageSpans(resolved)
	return action, nil
}

// automationMessageSpansShaped is whether words hold more than their text:
// a mention or a mark, which a notification and a confirmation show.
func automationMessageSpansShaped(spans []interactiveAutomationMessageSpan) bool {
	for _, span := range spans {
		if span.Mention != "" || span.Bold || span.Italic || span.Underline || span.Strikethrough || span.Code || span.Link != "" {
			return true
		}
	}
	return false
}

// hasAutomationMessageMentions is whether words name a person, a page or a date.
func hasAutomationMessageMentions(spans []interactiveAutomationMessageSpan) bool {
	for _, span := range spans {
		if span.Mention != "" {
			return true
		}
	}
	return false
}
