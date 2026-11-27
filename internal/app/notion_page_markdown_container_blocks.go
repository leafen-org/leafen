package app

import "strings"

// Writing each container Notion's page Markdown holds back as the block
// Leafen reads (notion_page_markdown_containers.go reads them): the block,
// a list, columns or tabs, a file's source and a tag's attributes.

// notionMarkdownContainerBlock writes a held block as Leafen reads it: its
// line, and its blocks one level in. A heading's stay under it only where
// the heading keeps a block already there (notionMarkdownHeadingHolds).
func notionMarkdownContainerBlock(name string, attributes map[string]string, title string, children []string, level int, tabs string) []string {
	if notionMarkdownEmptyParagraph.MatchString(title) {
		title = ""
	}
	color := ""
	if value := attributes["color"]; value != "" && value != "default" {
		color = ` {color="` + value + `"}`
	}
	inner := notionMarkdownFlow(children, level+1)
	text := func(line string) []string { return append([]string{tabs + line}, inner...) }
	switch name {
	case "p":
		if title == "" {
			return text("<empty-block/>" + notionMarkdownEmptyBlockColor(attributes))
		}
		return text(notionMarkdownParagraph(notionMarkdownColoredMarks(title, color)) + color)
	case "h1", "h2", "h3", "h4":
		return text(strings.Repeat("#", int(name[1]-'0')) + " " + title + color)
	case "quote":
		return text("> " + title + color)
	case "li":
		return text("- " + title + color)
	case "todo":
		mark := " "
		if attributes["checked"] == "true" {
			mark = "x"
		}
		return text("- [" + mark + "] " + title + color)
	case "toggle":
		if heading := attributes["heading"]; len(heading) == 2 && heading[0] == 'h' && heading[1] >= '1' && heading[1] <= '4' {
			return text(strings.Repeat("#", int(heading[1]-'0')) + " " + title + ` {toggle="true"}` + color)
		}
		opening := "<details>"
		if color != "" {
			opening = `<details color="` + attributes["color"] + `">`
		}
		out := []string{tabs + opening, tabs + "<summary>" + title + "</summary>"}
		return append(append(out, inner...), tabs+"</details>")
	case "callout":
		opening := "<callout"
		for _, key := range []string{"icon", "color"} {
			if value, ok := attributes[key]; ok {
				opening += " " + key + `="` + value + `"`
			}
		}
		// Its text is its first line; with none its first block is its first
		// line, as Notion reads "<callout>\n$$\nx\n$$\n</callout>" (seen
		// 2026-11-14: the equation alone, no empty line above it), where a
		// <p> standing first is that line, empty too
		// (notionMarkdownCalloutTitleLine).
		out := []string{tabs + opening + ">"}
		if title != "" {
			out = append(out, tabs+"\t"+title)
		}
		return append(append(out, inner...), tabs+notionMarkdownCalloutClose)
	case "equation":
		out := []string{tabs + "$$" + color}
		for _, line := range strings.Split(strings.ReplaceAll(title, "<br>", "\n"), "\n") {
			out = append(out, tabs+line)
		}
		return append(out, tabs+"$$")
	case "bookmark", "embed":
		// A bookmark's url is its address in "v1", an embed's none (seen
		// 2026-11-10); "v2" leaves both out first (notionMarkdownTagAttributes).
		source := notionMarkdownSource(attributes)
		if source == "" && name == "bookmark" {
			source = attributes["url"]
		}
		tag := "<" + name
		if source != "" {
			tag += ` source="` + source + `"`
		}
		if service := attributes["service"]; service != "" {
			tag += ` service="` + service + `"`
		}
		return []string{tabs + tag + ">" + title + "</" + name + ">"}
	case "image":
		// Its tag, not Markdown's ![…](…), whose address reads otherwise
		// (notionMarkdownImageTaken).
		tag := `<image source="` + notionMarkdownSource(attributes) + `"`
		if link := attributes["link"]; link != "" {
			tag += ` link="` + link + `"`
		}
		return []string{tabs + tag + ">" + title + "</image>"}
	case "file", "pdf", "video", "audio":
		return []string{tabs + "<" + name + ` src="` + notionMarkdownSource(attributes) + `">` + title + "</" + name + ">"}
	case "column":
		// One out of its columns too, as "v1" reads it, empty or not (seen
		// 2026-11-13: "<column>\n</column>" is an empty column); "v2" refuses it first.
		opening := "<column>"
		if ratio := attributes["ratio"]; ratio != "" {
			opening = `<column ratio="` + ratio + `">`
		}
		return append(append([]string{tabs + opening}, inner...), tabs+"</column>")
	case "synced-block":
		return text("<synced-block />")
	case "synced-block-reference":
		return []string{tabs + `<synced-block-reference url="` + attributes["url"] + `" />`}
	}
	return nil
}

func notionMarkdownSource(attributes map[string]string) string {
	if source := attributes["source"]; source != "" {
		return source
	}
	return attributes["src"]
}

// notionMarkdownGroup writes a list, columns or tabs: the tags each item
// stands in, read one by one.
func notionMarkdownGroup(name string, attributes map[string]string, body []string, level int, tabs string) []string {
	out := []string{}
	if name == "columns" || name == "tabs" {
		out = append(out, tabs+"<"+name+">")
	}
	item := map[string]string{"ul": "li", "ol": "li", "columns": "column", "tabs": "tab"}[name]
	// An <ol>'s items carry its type, and its first item its start.
	listAttributes, first := attributes, true
	// A line in tabs that no <tab> holds is a tab of its own, as "v1" reads
	// it (seen 2026-11-13: "<tabs>\na\n</tabs>" holds the tab "a"); "v2"
	// refuses it first.
	beside := level
	if name == "tabs" {
		beside = level + 1
	}
	for index := 0; index < len(body); index++ {
		trimmed := strings.TrimSpace(body[index])
		match := notionMarkdownContainerOpen.FindStringSubmatch(trimmed)
		if match == nil || match[1] != item {
			run := notionMarkdownGroupRun(body, index, item)
			blocks := notionMarkdownGroupBeside(body[index:index+run], beside)
			if item == "li" {
				blocks, first = notionMarkdownListKindItems(blocks, beside, name, listAttributes, first)
			}
			out = append(out, blocks...)
			index += run - 1
			continue
		}
		end := notionMarkdownClosing(body, index, item)
		// An item closed on its own line, <li>a</li>, ends there.
		if item == "li" && strings.HasSuffix(strings.TrimSpace(match[3]), "</li>") {
			end = index
		}
		if end < 0 {
			end = len(body)
		}
		inner := body[min(index+1, end):min(end, len(body))]
		attributes := notionMarkdownAttributes(match[2])
		switch name {
		case "ul", "ol":
			title, children := notionMarkdownTitle(inner)
			title = notionMarkdownTitleEdges(inner, title)
			if text, rest, held := notionMarkdownTitleParagraph(inner); held {
				title, children = text, rest
				_, children = notionMarkdownHeldTitleBlocks(item, title, notionMarkdownTitleParagraphBlocks(inner), children, level)
			}
			if rest := strings.TrimSpace(match[3]); rest != "" {
				title, children = notionMarkdownTextEdges(strings.TrimSuffix(strings.TrimRight(match[3], " \t"), "</li>")), nil
			}
			// An empty item's text is <p />, as Notion writes one holding blocks.
			if notionMarkdownEmptyParagraph.MatchString(title) {
				title = ""
			}
			color := ""
			if value := attributes["color"]; value != "" && value != "default" {
				color = ` {color="` + value + `"}`
			}
			marker, mark := "- ", ""
			if name == "ol" {
				marker, mark = "1. ", notionMarkdownOrderedMark(listAttributes, first)
			}
			first = false
			out = append(out, tabs+marker+title+mark+color)
			out = append(out, notionMarkdownFlow(children, level+1)...)
		case "columns":
			if ratio := attributes["ratio"]; ratio != "" {
				out = append(out, tabs+"\t"+`<column ratio="`+ratio+`">`)
			} else {
				out = append(out, tabs+"\t<column>")
			}
			out = append(out, notionMarkdownFlow(inner, level+2)...)
			out = append(out, tabs+"\t</column>")
		case "tabs":
			title, children := notionMarkdownTabItem(inner)
			if notionMarkdownEmptyParagraph.MatchString(title) || title == "" {
				title = "<empty-block/>"
			}
			if icon := attributes["icon"]; icon != "" {
				title = `<tab-name icon="` + icon + `">` + title
			}
			out = append(out, tabs+"\t"+title)
			out = append(out, notionMarkdownFlow(children, level+2)...)
		}
		index = end
	}
	if name == "columns" || name == "tabs" {
		out = append(out, tabs+"</"+name+">")
	}
	return out
}

func notionMarkdownAttributes(value string) map[string]string {
	attributes := map[string]string{}
	for _, match := range notionMarkdownAttributeValue.FindAllStringSubmatch(value, -1) {
		attributes[match[1]] = match[2]
	}
	return attributes
}
