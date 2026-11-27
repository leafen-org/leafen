package app

import (
	"slices"
	"strings"
)

// A "v2" tag whose text stands in a <p> of its own on lines of its own may
// hold blocks in that <p> after its text and a blank line, as Notion writes a
// callout whose first line holds blocks (seen 2026-11-14:
// "<callout>\n<p>\na\n\nb\n</p>\n</callout>"). Notion reads them so: in a
// callout under its first line, the line its block list gives as the
// callout's text with them left out (notionCalloutFirstLine); in a toggle, a
// quote or a list item as that block's first blocks, before those after the
// </p> ("<toggle>\n<p>\na\n\nb\n</p>\n</toggle>" is the toggle "a" holding
// "b").

// notionMarkdownTitleParagraphBlocks are the lines of the blocks a <p>
// standing first in a tag's lines holds after its text; none for a <p> on one
// line, closed in itself or holding text alone.
func notionMarkdownTitleParagraphBlocks(body []string) []string {
	start := 0
	for start < len(body) && strings.TrimSpace(body[start]) == "" {
		start++
	}
	if start == len(body) {
		return nil
	}
	match := notionMarkdownParagraphOpening.FindStringSubmatch(strings.TrimSpace(body[start]))
	if match == nil || match[2] == "/" || match[3] != "" {
		return nil
	}
	end := notionMarkdownClosing(body, start, "p")
	if end < 0 {
		return nil
	}
	_, held := notionMarkdownTitle(body[start+1 : end])
	return held
}

// notionMarkdownHeldTitleBlocks puts the blocks a tag's <p> holds where they
// go: a callout's under its first line, an empty one too (seen 2026-11-14:
// "<callout>\n<p>\n\n- b\n</p>\n</callout>" holds the item under an empty
// first line), written at once after that line one level further in than its
// other blocks; another tag's before its blocks.
// lines are the tag's lines as notionMarkdownContainerBlock wrote them.
func notionMarkdownHeldTitleBlocks(name, title string, held, children []string, level int) ([]string, []string) {
	if len(held) == 0 {
		return nil, children
	}
	if name == "callout" {
		return notionMarkdownFlow(held, level+2), children
	}
	if len(children) == 0 {
		return nil, held
	}
	return nil, slices.Concat(held, []string{""}, children)
}

// notionMarkdownCalloutWithHeld places a callout's first line's blocks right
// after that line, the callout's opening and text the first two of its lines.
func notionMarkdownCalloutWithHeld(lines, under []string) []string {
	if len(under) == 0 || len(lines) < 2 {
		return lines
	}
	return slices.Concat(lines[:2], under, lines[2:])
}

// notionMarkdownCalloutTitleLine is a callout's first line read from the <p>
// standing first in its lines, body: an empty one stays an empty line (seen
// 2026-11-14: "<callout>\n<p>\n\n</p>\n</callout>" and "<p />" hold one),
// and the <p>'s color stays the line's ("<p color=\"red\">", read back as
// the callout's text, notionCalloutFirstLine).
func notionMarkdownCalloutTitleLine(title string, body []string) string {
	if title == "" {
		title = "<empty-block/>"
	}
	for _, line := range body {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			if match := notionMarkdownParagraphOpening.FindStringSubmatch(trimmed); match != nil {
				if color := notionMarkdownAttributes(match[1])["color"]; color != "" && color != "default" {
					title += ` {color="` + color + `"}`
				}
			}
			break
		}
	}
	return title
}
