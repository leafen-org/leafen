package app

import "strings"

// textSpanColors reads a span's text color and its background, which Notion
// keeps apart (seen 2026-10-10). An older span kept its background in its
// color; it reads as that background.
func textSpanColors(color, background string) (text, behind string) {
	if strings.HasSuffix(color, "_background") {
		if background == "" {
			background = color
		}
		color = ""
	}
	return color, background
}

// notionSpanColor is the one color Notion's public API gives a span: its
// background when it has one, else its text color (seen 2026-10-10). The
// default background gives way to a text color, and alone reads as itself,
// as Notion keeps it on text, equations and mentions (seen 2026-11-12).
func notionSpanColor(color, background string) string {
	text, behind := textSpanColors(color, background)
	switch {
	case behind != "" && behind != "default_background":
		return behind
	case text != "" && text != "default":
		return text
	case behind != "":
		return behind
	}
	return "default"
}

// splitNotionSpanColor puts a color Notion names, which may be a background,
// on the span's own field.
func splitNotionSpanColor(color string) (text, background string) {
	if color == "" || color == "default" {
		return "", ""
	}
	return textSpanColors(color, "")
}

// validNotionSpanBackground is a span's own background: one of Notion's
// background colors.
func validNotionSpanBackground(background string) bool {
	return strings.HasSuffix(background, "_background") && validNotionBlockColor(background)
}

// notionCommentSpanColor is the one color a comment's span keeps: its
// background when it has one, as Notion's API reports it.
func notionCommentSpanColor(span graphTextSpan) string {
	if span.Background != "" {
		return span.Background
	}
	return span.Color
}
