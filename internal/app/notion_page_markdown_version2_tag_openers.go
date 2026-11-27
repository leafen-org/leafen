package app

import (
	"regexp"
	"strings"
)

// Notion's "v2" reads a block's tag only standing first on its line, past
// its blocks' marks, and a tag that stands in text anywhere (seen
// 2026-11-15): a block's tag after words is words whatever its attributes
// ("a <p color='red'>x</p>", "a <p color>x</p>", "a <callout icon>x</callout>",
// "a <unknown url=\"…>x"; one ending its line is left open, unsaid,
// "x <callout icon>"). One it reads says first what its opening can't be
// read as:
//   - a quote that doesn't close before its line ends: a block's tag is
//     refused ("<unknown url=\"…'>x", "<unknown url='a\"b'>x",
//     "<p color=\"red>x</p>", "- <p color=\"red>x</p>", "<callout
//     icon=\"🙂>⏎x⏎</callout>", "<details color=\"red>…"), a tag in text is
//     words, its closing tag too ("<span color=\"red>x</span>", "a <span
//     color=\"red>x", "<p color=\"red\">x <span color=\"blue>y</span></p>");
//   - an = without a name before it, or an attribute whose name starts with
//     no letter: refused, either kind ("<p = \"c\">x</p>", "<callout
//     =\"a\">…", "a <b =\"c\">x</b>", "a <b=\"c\">x</b>"; seen 2026-11-15:
//     "<callout - color=\"red\">x</callout>", "<callout -color=…>", "<callout
//     -=…>", "<callout 1color=…>", "<callout :x=…>", "<callout ! …>",
//     "<callout color=\"red\" ->", "<image - src=… />", "a <span - color=…>x
//     </span>", over lines too; "<callout _x=…>" is read, "<empty-block / >"
//     closes in itself);
//   - a name a ":" or a "." ends: refused for that name, as an attribute
//     without a value, either kind, v1's tags by the tag each becomes (seen
//     2026-11-15: "<callout x:y=\"1\">x</callout>", "<callout x.y=…>",
//     "a <span x:y=…>", "a <a x:y=… href=…>", "<mention-date x:y=… />" is
//     <date>'s attribute "x" written wrong, notionMarkdownRepair.malformed);
//   - an attribute without a value, a block's tag whose line holds more
//     than its tag: refused for that attribute ("<unknown url=\"…\" alt>x",
//     "- <unknown url=\"…\" alt>", "> <unknown … alt>x", "<empty-block
//     alt>x", "<callout icon>x" and "<p color>x" after the tag left open);
//     one that is all its line, or a tag in text, its attributes say
//     (notionMarkdownRepair.attributes).

// notionMarkdownVersion2TagName is a tag Notion's "v2" reads by its name,
// in small letters, at the start of a stretch.
var notionMarkdownVersion2TagName = regexp.MustCompile(`^<([a-z][a-z0-9_-]*)`)

// openers says what Notion says of the tags opening in text, a stretch of a
// line (first: the stretch starts the line), that its reading can't read as
// tags, and gives where each stands, as words.
func (repair *notionMarkdownRepair) openers(text string, first bool) map[int]bool {
	words := map[int]bool{}
	for at := strings.IndexByte(text, '<'); at >= 0; {
		match := notionMarkdownVersion2TagName.FindStringSubmatch(text[at:])
		var read string
		if match != nil && !notionMarkdownEscapedAt(text, at) {
			read = match[1]
			if converted, legacy := notionMarkdownLegacyTags[read]; legacy {
				read = converted
			}
		}
		if read != "" && (notionMarkdownVersion2Tags[read] || read == "details") &&
			(notionMarkdownVersion2InlineTags[read] || notionMarkdownVersion2Leading(text, at, first)) {
			name, block := match[1], !notionMarkdownVersion2InlineTags[read]
			fault, attribute := notionMarkdownVersion2OpenerFault(text[at+len(match[0]):])
			mark := len(repair.missing)
			switch {
			// A v1 tag whose attributes are malformed is words, unsaid (seen
			// 2026-11-15: "<table_of_contents -x=\"1\"/>", "a <mention-date
			// 1x=\"1\" …/> c").
			case name != read && fault == "nameless":
				words[at] = true
			case name != read && fault != "valueless":
			case fault == "valueless":
				repair.malformed(read, attribute, notionMarkdownQuoteValueSaid)
				words[at] = true
			case fault == "unclosed":
				if block {
					repair.missing = append(repair.missing, notionMarkdownMalformedTag(name))
				}
				words[at] = true
			case fault == "nameless":
				repair.nameless(name)
				words[at] = true
			}
			if block {
				repair.openingSaid(mark, at)
			} else {
				repair.inlineAt(at, mark)
			}
		}
		next := strings.IndexByte(text[at+1:], '<')
		if next < 0 {
			break
		}
		at += next + 1
	}
	return words
}

// notionMarkdownVersion2Leading tells a tag at text[at] that stands first
// on its line, past its blocks' marks.
func notionMarkdownVersion2Leading(text string, at int, first bool) bool {
	return first && at == len(notionMarkdownVersion2MarkerRun.FindString(text))
}

// notionMarkdownVersion2OpenerFault is what keeps the rest of an opening
// tag past its name from being read: "unclosed" for a quote its line ends
// in, "nameless" for an = no attribute's name stands right before (its
// spaces taken out first, notionMarkdownVersion2SpacedAttributes) or a name
// that starts with neither a letter nor "_", "valueless" with the name for
// one a ":" or a "." ends; none else.
func notionMarkdownVersion2OpenerFault(rest string) (string, string) {
	if rest != "" && strings.IndexByte(" \t=/>", rest[0]) < 0 {
		return "", ""
	}
	quoted, end := false, -1
	for at := 0; at < len(rest) && end < 0; at++ {
		if rest[at] == '"' {
			quoted = !quoted
		} else if rest[at] == '>' && !quoted {
			end = at
		}
	}
	if end < 0 {
		if quoted {
			return "unclosed", ""
		}
		return "", ""
	}
	quoted = false
	for at := 0; at < end; at++ {
		starts := !quoted && (at == 0 || rest[at-1] == ' ' || rest[at-1] == '\t')
		switch {
		case rest[at] == '"':
			quoted = !quoted
		case starts && rest[at] == '=':
			return "nameless", ""
		// A name starts with a letter or "_"; a slash closes the tag in itself.
		case starts && strings.IndexByte(" \t/'_", rest[at]) < 0 && !('a' <= rest[at]|0x20 && rest[at]|0x20 <= 'z'):
			return "nameless", ""
		case starts && notionMarkdownAttributeNameCharacter(rest[at], true):
			name := at
			for name < end && notionMarkdownAttributeNameCharacter(rest[name], false) {
				name++
			}
			if rest[name] == ':' || rest[name] == '.' {
				return "valueless", rest[at:name]
			}
		}
	}
	return "", ""
}

// valueless refuses an opening tag of a block standing first on its line,
// its line holding more than the tag, for its first attribute without a
// value; it says whether it did.
func (repair *notionMarkdownRepair) valueless(name, written string) bool {
	// One whose value isn't in double quotes is said so (notionMarkdownRepair.loose).
	for _, found := range notionMarkdownAttributeToken.FindAllStringSubmatchIndex(written, -1) {
		if found[4] < 0 && (found[1] == len(written) || written[found[1]] != '=') {
			repair.malformed(name, written[found[2]:found[3]], notionMarkdownQuoteValueSaid)
			return true
		}
	}
	return false
}
