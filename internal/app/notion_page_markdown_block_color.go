package app

import (
	"encoding/json"
	"regexp"
	"strings"
)

// A block's color as Leafen's reading takes it: a text block's line ends with
// {color="red"}, a background named "…_bg" ({color="blue_bg"}) — a
// paragraph, a heading, a list item, a to-do and a quote — and a toggle's tag
// carries it (<details color="red_bg">), as Notion's page Markdown wrote it
// on 2026-10-25. Notion's tags now carry it (<p color="red">), which reading
// puts this way first (notion_page_markdown_containers.go) and the writer
// writes (notion_page_markdown_writer.go).

var notionMarkdownBlockColorPattern = regexp.MustCompile(`^(.*?)[ \t]+\{color="([a-z_]+)"\}$`)

// notionMarkdownColorName is a block color as Markdown names it.
func notionMarkdownColorName(color string) string {
	if strings.HasSuffix(color, "_background") {
		return strings.TrimSuffix(color, "_background") + "_bg"
	}
	return color
}

// notionMarkdownColorValue is a color Markdown names as a block keeps it;
// "" for none Notion has.
func notionMarkdownColorValue(name string) string {
	color := name
	if strings.HasSuffix(color, "_bg") {
		color = strings.TrimSuffix(color, "_bg") + "_background"
	}
	if !validNotionBlockColor(color) {
		return ""
	}
	return color
}

// notionMarkdownColorAttribute is a tag's color="…" for a table's column,
// row or cell; none without one.
func notionMarkdownColorAttribute(color string) string {
	if !validGraphTableColor(color) || color == "" {
		return ""
	}
	return ` color="` + notionMarkdownColorName(color) + `"`
}

// notionMarkdownBlockColorSuffix is what ends a colored block's line; none
// for a block of the default color.
func notionMarkdownBlockColorSuffix(content map[string]any) string {
	color := notionBlockColor(content)
	if color == "default" || color == "default_background" {
		return ""
	}
	return ` {color="` + notionMarkdownColorName(color) + `"}`
}

// takeNotionMarkdownBlockColor takes a line's {color="…"} off its end and
// gives the color as a block keeps it; a color Notion has none of stays the
// line's text.
func takeNotionMarkdownBlockColor(line string) (string, string) {
	match := notionMarkdownBlockColorPattern.FindStringSubmatch(line)
	if match == nil {
		return line, ""
	}
	color := match[2]
	if strings.HasSuffix(color, "_bg") {
		color = strings.TrimSuffix(color, "_bg") + "_background"
	}
	if !validNotionBlockColor(color) {
		return line, ""
	}
	return match[1], color
}

// withNotionMarkdownBlockColor puts a color in a block's content.
func withNotionMarkdownBlockColor(content json.RawMessage, color string) json.RawMessage {
	if color == "" || color == "default" {
		return content
	}
	fields := map[string]any{}
	if len(content) > 0 {
		_ = json.Unmarshal(content, &fields)
	}
	fields["color"] = color
	encoded, err := json.Marshal(fields)
	if err != nil {
		return content
	}
	return encoded
}
