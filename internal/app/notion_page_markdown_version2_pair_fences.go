package app

import (
	"regexp"
	"strings"
)

// Notion's "v2" pairs a block's tags past code it finds by two reads of the
// lines as written, apart from how it reads their blocks (seen 2026-11-14):
// a fence of ``` or ~~~ that stands three spaces in at most and no list
// item's or quote's mark before it opens code that its like closes — as
// many of its marks or more, nothing after them — or that runs to the end;
// and a ``` fence after a list item's or a quote's mark opens code the next
// line of ``` closes, whatever stands before it, which is none when there's
// no such line. A closing tag in code either read sees, which no closing
// tag of its kind after it outside both makes up for, leaves its tag open,
// refused as left open at the end: "<callout>⏎- ```⏎  a⏎  ```⏎</callout>",
// "<callout>⏎- ```⏎  a⏎</callout>⏎⏎```⏎z⏎```" and "<callout>⏎- a⏎⏎  ```⏎
// b⏎</callout>" are refused, "<callout>⏎- ```⏎  a⏎</callout>" and
// "<callout>⏎- ```⏎  a⏎  ```⏎```⏎```⏎</callout>" not. The block a tag
// holds ends at the first closing tag of its kind the second read leaves,
// code it holds too: "<callout>⏎- ```⏎  a⏎  ```⏎```⏎q⏎</callout>" is a
// callout holding the item's code "a" and the code "q", "<callout>⏎-
// ```⏎  a⏎  ```⏎</callout>⏎```⏎</callout>" one holding the item's code,
// and the code "</callout>" after it.

// notionMarkdownVersion2Pairing is what each line is to Notion pairing tags:
// code to the first read or the second (either), code to the second
// (second), a fence put in that it doesn't read (put).
type notionMarkdownVersion2Pairing struct {
	either, second, put []bool
}

var (
	notionMarkdownVersion2PairTag = regexp.MustCompile(`^<(/?)(p|h[1-4]|quote|toggle|callout|details|columns?|tabs?|ul|ol|li|todo|synced-block|table|tr|td|th)(?:[ \t]+[^<>]*)?>$`)
	notionMarkdownBacktickRuns    = regexp.MustCompile("`+")
)

// notionMarkdownVersion2PairingMasks reads lines, as HeldFences gave them
// with the fences it put in (put), as Notion does pairing tags.
func notionMarkdownVersion2PairingMasks(lines []string, put []bool) notionMarkdownVersion2Pairing {
	pairing := notionMarkdownVersion2Pairing{either: make([]bool, len(lines)), second: make([]bool, len(lines)), put: put}
	opened, mark, run := -1, byte(0), 0
	for index, line := range lines {
		if put[index] {
			continue
		}
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		body := strings.TrimSpace(line[len(prefix):])
		fenced := notionMarkdownVersion2Fence(body) || strings.HasPrefix(body, "~~~")
		plain := fenced && strings.Trim(prefix, " \t") == "" && notionMarkdownVersion2Indent(prefix, 0) <= 3
		if opened >= 0 {
			pairing.either[index] = true
			if plain && body[0] == mark && strings.Trim(body, body[:1]) == "" && len(body) >= run {
				opened = -1
			}
			continue
		}
		if plain {
			opened, mark = index, body[0]
			run = len(body) - len(strings.TrimLeft(body, body[:1]))
			pairing.either[index] = true
		}
	}
	for index := 0; index < len(lines); index++ {
		prefix := notionMarkdownVersion2MarkerRun.FindString(lines[index])
		if put[index] || strings.Trim(prefix, " \t") == "" || !notionMarkdownVersion2Fence(strings.TrimSpace(lines[index][len(prefix):])) {
			continue
		}
		for end := index + 1; end < len(lines); end++ {
			if after := lines[end]; !put[end] &&
				notionMarkdownVersion2Fence(strings.TrimSpace(after[len(notionMarkdownVersion2MarkerRun.FindString(after)):])) {
				for at := index; at <= end; at++ {
					pairing.either[at], pairing.second[at] = true, true
				}
				index = end
				break
			}
		}
	}
	return pairing
}

// notionMarkdownVersion2PairFences puts lines right where Notion pairs a
// block's tags past code otherwise than Leafen's reading would: code a block
// holds closes at its closing tag, and code from a closing tag on to the
// end leaves its tag open where Notion would — left is the line of the
// fence put in for that, or -1 (notionMarkdownVersion2LeftItems).
func notionMarkdownVersion2PairFences(lines []string, pairing notionMarkdownVersion2Pairing) (_ []string, left int) {
	masked := func(mask []bool, index int) bool { return index < len(mask) && mask[index] }
	open, counted, fence, quotes, top := []string{}, []bool{}, "", 0, false
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		body := strings.TrimSpace(line[len(prefix):])
		tag := notionMarkdownVersion2PairTag.FindStringSubmatch(strings.TrimSpace(line))
		if fence != "" {
			if inside := notionMarkdownVersion2Unquoted(line, quotes); strings.HasPrefix(inside, fence) && strings.Trim(inside, "`") == "" {
				fence = ""
				continue
			}
			if tag == nil || tag[1] == "" || !top || masked(pairing.second, index) || !notionMarkdownOpenHolds(open, tag[2]) {
				continue
			}
			lines = append(lines[:index], append([]string{fence}, lines[index:]...)...)
			pairing = pairing.shifted(index)
			fence, index = "", index+1
		} else if notionMarkdownVersion2Fence(body) {
			fence, quotes, top = notionMarkdownFenceOf(body), strings.Count(prefix, ">"), strings.Trim(prefix, " \t") == ""
			continue
		}
		switch {
		case tag == nil || strings.HasSuffix(tag[0], "/>"):
		case tag[1] == "":
			// One in code to Notion pairing tags it doesn't pair, though it
			// reads its block ("- ```⏎  a⏎  ```⏎⏎<callout>⏎b⏎</callout>").
			open, counted = append(open, tag[2]), append(counted, !masked(pairing.either, index))
		case notionMarkdownOpenHolds(open, tag[2]):
			at := len(open) - 1
			for open[at] != tag[2] {
				at--
			}
			paired := counted[at]
			open, counted = open[:at], counted[:at]
			if paired && masked(pairing.either, index) && !notionMarkdownVersion2PairLater(lines, index, tag[2], pairing) {
				longest := "``"
				for _, run := range notionMarkdownBacktickRuns.FindAllString(strings.Join(lines[index:], "\n"), -1) {
					if len(run) > len(longest) {
						longest = run
					}
				}
				return append(lines[:index], append([]string{longest + "`"}, lines[index:]...)...), index
			}
		}
	}
	return lines, -1
}

// notionMarkdownOpenHolds tells tags open that hold one of the name.
func notionMarkdownOpenHolds(open []string, name string) bool {
	for _, opened := range open {
		if opened == name {
			return true
		}
	}
	return false
}

// notionMarkdownVersion2PairLater tells a closing tag of the name after
// lines[index], no code to Notion pairing tags, that closes as many as open
// after it.
func notionMarkdownVersion2PairLater(lines []string, index int, name string, pairing notionMarkdownVersion2Pairing) bool {
	depth := 0
	for at := index + 1; at < len(lines); at++ {
		if at < len(pairing.either) && (pairing.either[at] || pairing.put[at]) {
			continue
		}
		tag := notionMarkdownVersion2PairTag.FindStringSubmatch(strings.TrimSpace(lines[at]))
		switch {
		case tag == nil || tag[2] != name || strings.HasSuffix(tag[0], "/>"):
		case tag[1] == "":
			depth++
		case depth == 0:
			return true
		default:
			depth--
		}
	}
	return false
}

// shifted is the pairing of lines with one put in before index.
func (pairing notionMarkdownVersion2Pairing) shifted(index int) notionMarkdownVersion2Pairing {
	insert := func(mask []bool, value bool) []bool {
		if index > len(mask) {
			return mask
		}
		return append(mask[:index:index], append([]bool{value}, mask[index:]...)...)
	}
	return notionMarkdownVersion2Pairing{either: insert(pairing.either, false), second: insert(pairing.second, false), put: insert(pairing.put, true)}
}

// notionMarkdownItemFence is the fence a list item's line opens code with,
// with the items it stands in, as Leafen's reader closes it with the item
// (notionMarkdownVersion2HeldFences); "" for none.
func notionMarkdownItemFence(line string) (string, []notionMarkdownVersion2Held) {
	opened, body := notionMarkdownVersion2Opens(line, 0)
	if body = strings.TrimSpace(body); len(opened) == 0 || !notionMarkdownVersion2Fence(body) {
		return "", nil
	}
	for _, held := range opened {
		if held.quote {
			return "", nil
		}
	}
	return notionMarkdownFenceOf(body), opened
}

// notionMarkdownItemFenceLine tells whether a line stands in the items code
// of the fence is in, and whether it closes the code.
func notionMarkdownItemFenceLine(line, fence string, items []notionMarkdownVersion2Held) (in, closed bool) {
	inside, _, count := notionMarkdownVersion2Within(line, items)
	if count < len(items) {
		return false, false
	}
	closing := strings.TrimSpace(inside)
	return true, strings.HasPrefix(closing, fence) && strings.Trim(closing, "`") == ""
}
