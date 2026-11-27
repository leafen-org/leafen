package app

import "strings"

// A text block's tag — <p>, <h1>…<h4>, <quote>, <toggle>, <callout> — or
// v1's <details> holding text on its opening line reads as the tag with that
// text on the line under it, as Notion's "v2" reads it (seen 2026-11-21, its
// API's page Markdown replace, each case written after a plain line, two
// ways: as written and with the text under the tag): "<toggle>x⏎⏎y⏎
// </toggle>" is "<toggle>⏎x⏎⏎y⏎</toggle>", the toggle "x" holding the
// paragraph "y", and so are "<toggle>x⏎⏎y</toggle>", its closing tag ending
// the last line, a line starting a block before it ("<toggle>x⏎- y⏎
// </toggle>", "<toggle>x⏎```…"), blank lines past one, a closing tag further
// in, an opening tag further in under a list item ("- a⏎  <toggle>x⏎⏎  y⏎
// </toggle>") and one in a quote ("> <toggle>x⏎>⏎> y⏎> </toggle>"; "> <toggle>
// x⏎> y⏎> </toggle>" holds the toggle "x⏎y"). Code its text opens leaves it
// open ("<toggle>```⏎code⏎```⏎</toggle>" is refused,
// notionMarkdownVersion2TagFences); text starting a block is that block
// ("<toggle>- a⏎⏎b⏎</toggle>" is an untitled toggle holding the list "a" and
// the paragraph "b", "<toggle>- a⏎y⏎</toggle>" the list "a⏎y"); "$$" is its
// words. Its closing tag is the one Notion pairs with it
// (notionMarkdownVersion2PairOf); one that text follows on its line leaves
// both as text (notionMarkdownVersion2BrokenPairs, which reads the lines
// after this, so the closing tags of the blocks around it after it are no
// text: "<toggle>x⏎⏎<toggle>y⏎⏎z</toggle></toggle>" is a toggle in a
// toggle), and a tag Notion closes nowhere stays as written. Lines of text
// alone after it are its text on its line (notionMarkdownVersion2TagJoined).
// A <details> so is the <toggle> it is converted to, said so; one Notion
// closes nowhere stands alone, an untitled one left open, refused at the end
// as Notion refuses it ("<details>x", "<details>x⏎y", "<details>x⏎⏎y⏎
// </toggle>").

// notionMarkdownVersion2SpreadTags puts the text on a text block's opening
// line on a line of its own under it, and the closing tag Notion pairs with
// it on one too; toggled tells whether a <details> so read as a <toggle>.
func notionMarkdownVersion2SpreadTags(lines []string) ([]string, bool) {
	out, toggled, fence := make([]string, 0, len(lines)), false, ""
	// closings are the closing tags put on lines of their own under a line,
	// by that line, in order.
	closings := map[int][]string{}
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
		default:
			if spread, details := notionMarkdownVersion2SpreadTag(lines, index, closings); spread != nil {
				out, toggled = append(out, spread...), toggled || details
				continue
			}
		}
		out = append(out, line)
		out = append(out, closings[index]...)
	}
	return out, toggled
}

// notionMarkdownVersion2SpreadTag is the tag opening lines[start] with its
// text under it, its closing tag's line put right in lines and closings;
// none where it stays as written. details tells a <details> read as a
// <toggle>.
func notionMarkdownVersion2SpreadTag(lines []string, start int, closings map[int][]string) ([]string, bool) {
	// Under a quote's marks too, which its lines keep.
	line := lines[start]
	lead := line[:len(line)-len(strings.TrimLeft(line, " \t>"))]
	opened := strings.TrimRight(line[len(lead):], " \t")
	match := notionMarkdownVersion2TextTagOpening.FindStringSubmatch(opened)
	if match == nil {
		match = notionMarkdownVersion2TitledDetails.FindStringSubmatch(opened)
	}
	// Code its text opens leaves it open (notionMarkdownVersion2TagFences).
	if match == nil || strings.TrimSpace(match[3]) == "" || !notionMarkdownVersion2DetailsTitleStarts(match[3]) ||
		notionMarkdownVersion2ClosingOutsideCode(match[3], "</"+match[1]+">") || strings.Contains(match[3], "<"+match[1]) ||
		notionMarkdownVersion2Fence(strings.TrimSpace(match[3])) || strings.HasPrefix(strings.TrimSpace(match[3]), "~~~") {
		return nil, false
	}
	// Lines of text alone after it join its line instead.
	if _, used := notionMarkdownVersion2TagJoined(lines, start); used > 0 {
		return nil, false
	}
	opening, name := opened[:len(opened)-len(match[3])], match[1]
	closer, at, end := notionMarkdownVersion2PairOf(lines, start, len(lead)+len(opening), name)
	if closer < 0 || strings.TrimSpace(lines[closer][end:]) != "" {
		// A <details> Notion closes nowhere stands alone, so is left open.
		if closer < 0 && name == "details" {
			return []string{lead + opening, lead + notionMarkdownVersion2SpreadText(match[3])}, false
		}
		return nil, false
	}
	closing := lines[closer][at:end]
	if name == "details" {
		opening, closing = "<toggle"+opening[len("<details"):], "</toggle>"
	}
	if before := strings.TrimRight(lines[closer][:at], " \t"); strings.Trim(before, " \t>") != "" {
		// What it holds after the tags paired inside it closes first.
		lines[closer] = before
		closings[closer] = append([]string{lead + closing}, closings[closer]...)
	} else {
		lines[closer] = lines[closer][:at] + closing
	}
	return []string{lead + opening, lead + notionMarkdownVersion2SpreadText(match[3])}, name == "details"
}

// notionMarkdownVersion2SpreadText is the text on a text block's opening
// line on a line of its own: "$$" there opens no equation, it is its words
// (seen 2026-11-21: "<toggle>$$⏎⏎b⏎</toggle>" is the toggle "$$").
func notionMarkdownVersion2SpreadText(text string) string {
	if strings.TrimSpace(text) == "$$" {
		return strings.Replace(text, "$$", `\$\$`, 1)
	}
	return text
}

// notionMarkdownVersion2SpreadStarts tells text on a text block's opening
// line that starts a block of its own, which the block's tag then holds
// (notionMarkdownVersion2SpreadTags).
func notionMarkdownVersion2SpreadStarts(lines []string, start int, text string) bool {
	next := ""
	if start+1 < len(lines) {
		next = strings.TrimSpace(lines[start+1])
	}
	return notionMarkdownVersion2Starts(strings.TrimSpace(text), next, false)
}

// notionMarkdownVersion2Tagged is a "v2" write's lines with its text
// blocks' tags put right (notionMarkdownVersion2TagLines) and those closed in
// themselves (notionMarkdownVersion2SelfClosedBlocks), and the repair that
// reads them, which says a <details> read as a <toggle> there.
func notionMarkdownVersion2Tagged(paired []string, reads *notionMarkdownVersion2GroupReads) ([]string, notionMarkdownRepair) {
	lines, toggled := notionMarkdownVersion2TagLines(paired)
	repair := notionMarkdownRepair{groups: reads}
	if toggled {
		repair.olderDetails("details")
	}
	return notionMarkdownVersion2SelfClosedBlocks(lines), repair
}
