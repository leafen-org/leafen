package app

// A text span's words and colors as a content change writes them into a
// block's text (content_graph_compound_mutation.go): its color one of
// Notion's, and its background one of their backgrounds, as a caption's and
// a table cell's are (validGraphContentTextSpan) and as Notion's API takes
// them (2026-11-26: a block's text took any color written, which the screen
// and Notion's API never do).

// graphTextSpanColorProblem names what is wrong with a span's colors, none
// for nothing.
func graphTextSpanColorProblem(span graphTextSpan) string {
	if span.Color != "" && !validNotionBlockColor(span.Color) {
		return "color is a Notion color (default, gray, brown, orange, yellow, green, blue, purple, pink or red, or one of them with _background), not " + span.Color
	}
	if span.Background != "" && !validNotionSpanBackground(span.Background) {
		return "background is a Notion color with _background (gray_background …), not " + span.Background
	}
	return ""
}

// graphTextSpanCharacters is how many characters a span's words and settings
// hold, as a text's bound counts them.
func graphTextSpanCharacters(span graphTextSpan) int {
	values := []string{
		span.Text, span.Color, span.Background, span.Link, span.CommentID, span.Mention,
		span.PageID, span.DatabaseID, span.CustomEmojiID, span.Date, span.DateEnd, span.DateTimeZone,
		span.DateReminder, span.DateFormat, span.DateTimeFormat, span.UserID, span.IconURL, span.Provider, span.Description, span.ImageURL,
		span.Equation, span.Template, span.Suggestion, span.SuggestionID, span.SuggestionBefore,
	}
	total := 0
	for _, value := range values {
		total += len([]rune(value))
	}
	return total
}
