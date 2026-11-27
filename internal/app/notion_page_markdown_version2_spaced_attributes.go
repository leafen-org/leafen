package app

import "strings"

// A "v2" write's tag Notion reads takes spaces around an attribute's =, an
// attribute right after a quoted value, and spaces alone before its ">"
// (seen 2026-11-15: "<unknown url = \"…\"/>", "url= \"…\"", "url =\"…\"",
// "url=\t\"…\"" and "url=\"…\"alt=\"a\"" read as url="…", in a callout, a
// paragraph, a toggle, a column, an image, an item, a span and a link too;
// "url = '…'" and "url = …" are refused as "url='…'" and "url=…" are; "<b
// >y</b>" is "y" in bold). Such a tag is put as it reads before anything
// else reads the Markdown. One the reading leaves as words is the words as
// written (seen 2026-11-15: "a <p color = \"red\">x</p>", "<p color =
// \"red\">x</p> y" and "a <unknown url = \"…\"/>" keep their spaces). A tag
// it doesn't read — another name, one in capitals, HTML — code, and a tag
// whose opening doesn't close on its line are left as they are.

// notionMarkdownVersion2SpacedTag is a tag as written and as put.
type notionMarkdownVersion2SpacedTag struct{ written, put string }

// notionMarkdownVersion2SpacedAttributes is markdown with its tags put as
// Notion reads their attributes, those over lines on one
// (notionMarkdownVersion2OverLines), the tags it put, in order, and what
// Notion refuses of those over lines.
func notionMarkdownVersion2SpacedAttributes(markdown string) (string, []notionMarkdownVersion2SpacedTag, string) {
	if !strings.Contains(markdown, "<") {
		return markdown, nil, ""
	}
	lines, over, refused := notionMarkdownVersion2OverLines(strings.Split(markdown, "\n"))
	spaced, fence := []notionMarkdownVersion2SpacedTag{}, ""
	for index, line := range lines {
		marks := notionMarkdownVersion2QuoteOpening.FindString(line)
		trimmed := strings.TrimSpace(line[len(marks):])
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		body := strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):])
		if notionMarkdownVersion2Fence(body) || strings.HasPrefix(body, "~~~") {
			fence = body[:len(body)-len(strings.TrimLeft(body, body[:1]))]
			continue
		}
		if !strings.Contains(line, "<") {
			continue
		}
		code := notionMarkdownInlineCodeRanges(line)
		var out strings.Builder
		at := 0
		for from := 0; from < len(line); from++ {
			if line[from] != '<' || from < at || notionMarkdownEscapedAt(line, from) || notionMarkdownInRanges(code, from) {
				continue
			}
			put, end, ok := notionMarkdownVersion2SpacedOpener(line, from)
			written := ""
			if ok {
				written = line[from:end]
			}
			if tag, joined := over[index]; joined && from == tag.at {
				// Its words are as written over its lines.
				if !ok {
					put, end = tag.tag.put, from+len(tag.tag.put)
				}
				written, ok = tag.tag.written, true
			}
			if !ok || put == written {
				continue
			}
			out.WriteString(line[at:from] + put)
			spaced = append(spaced, notionMarkdownVersion2SpacedTag{written: written, put: put})
			at = end
		}
		if at > 0 {
			lines[index] = out.String() + line[at:]
		}
	}
	return strings.Join(lines, "\n"), spaced, refused
}

// notionMarkdownVersion2SpacedWords is the Markdown a "v2" write's reading
// put, with each tag it left as words, escaped, as written.
func notionMarkdownVersion2SpacedWords(markdown string, spaced []notionMarkdownVersion2SpacedTag) string {
	at := 0
	for _, tag := range spaced {
		found := strings.Index(markdown[at:], `\`+tag.put)
		if found < 0 {
			continue
		}
		found += at + 1
		markdown = markdown[:found] + tag.written + markdown[found+len(tag.put):]
		at = found + len(tag.written)
	}
	return markdown
}

// notionMarkdownVersion2SpacedOpener reads the opening tag starting at
// line[at] as Notion's "v2" does: the tag put with each attribute as
// name="value" (or as its value was written) after a space, and where it
// ends; not ok for a tag Notion doesn't read, an attribute without a name
// before its = ("<b = \"c\">"), a quote that doesn't close or no ">" on the
// line.
func notionMarkdownVersion2SpacedOpener(line string, at int) (string, int, bool) {
	end := at + 1
	for end < len(line) && (line[end] >= 'a' && line[end] <= 'z' ||
		end > at+1 && (line[end] >= '0' && line[end] <= '9' || line[end] == '-' || line[end] == '_')) {
		end++
	}
	name := line[at+1 : end]
	if name == "" || !notionMarkdownVersion2Tags[name] && notionMarkdownVersion1Tags[name] == "" && name != "details" {
		return "", 0, false
	}
	var out strings.Builder
	out.WriteString(line[at:end])
	for position := end; position < len(line); {
		space := position
		position = notionMarkdownSkipSpaces(line, position)
		switch {
		case position == len(line):
			return "", 0, false
		case line[position] == '>' && out.Len() == end-at:
			// Spaces alone before it go too (seen 2026-11-15: "<b  >y</b>" is "y" in bold).
			return out.String() + ">", position + 1, true
		case line[position] == '>':
			out.WriteString(line[space : position+1])
			return out.String(), position + 1, true
		case strings.HasPrefix(line[position:], "/>"):
			out.WriteString(line[space : position+2])
			return out.String(), position + 2, true
		}
		// A name is as any attribute's (notionMarkdownAttributeName; seen
		// 2026-11-15: "color=\"red\"_x=\"1\"").
		start := position
		for position < len(line) && notionMarkdownAttributeNameCharacter(line[position], position == start) {
			position++
		}
		if position == start {
			return "", 0, false
		}
		lead := line[space:start]
		if lead == "" {
			lead = " "
		}
		equals := notionMarkdownSkipSpaces(line, position)
		if equals == len(line) || line[equals] != '=' {
			out.WriteString(lead + line[start:position])
			continue
		}
		value := notionMarkdownSkipSpaces(line, equals+1)
		stop := value
		switch {
		case value < len(line) && (line[value] == '"' || line[value] == '\''):
			closing := strings.IndexByte(line[value+1:], line[value])
			if closing < 0 {
				return "", 0, false
			}
			stop = value + closing + 2
		default:
			for stop < len(line) && line[stop] != ' ' && line[stop] != '\t' && line[stop] != '>' && !strings.HasPrefix(line[stop:], "/>") {
				stop++
			}
		}
		out.WriteString(lead + line[start:position] + "=" + line[value:stop])
		position = stop
	}
	return "", 0, false
}

// notionMarkdownSkipSpaces is where the spaces and tabs from text[at] end.
func notionMarkdownSkipSpaces(text string, at int) int {
	for at < len(text) && (text[at] == ' ' || text[at] == '\t') {
		at++
	}
	return at
}

func notionMarkdownASCIILetter(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}
