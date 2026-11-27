package app

import (
	"regexp"
	"slices"
	"strings"
)

// Notion's Markdown names a tag's attributes so (seen 2026-11-15, the public
// API's create, replace_content and insert_content alike, each version's
// tags):
//
//   - "v2" by a letter or "_", then letters, digits, "_" and "-"
//     (notionMarkdownAttributeName: "<callout _x=\"1\">" and "<a _x=\"1\"
//     href=…>" read as their tags, the attribute left out). A name another
//     character ends — ":" or "." — is one without a value, refused as one
//     is ("<callout x:y=\"1\">" has its attribute "x" written wrong,
//     notionMarkdownRepair.malformed), and an attribute starting with
//     neither makes the tag's attributes malformed
//     (notionMarkdownVersion2OpenerFault).
//   - "v1" by any run of letters, digits, "_", ":" and "-", read from its
//     first letter on (notionMarkdownVersion1AttributeName): "_color",
//     "__color", "1color", ":color" and "-color" are a callout's or a span's
//     color, "x_color", "_x_color", "x:color", "Color" and "c_olor" none, and
//     a name holding another character ("x.y", ".color") is none either,
//     though its tag reads ("<callout x.y=\"1\">x</callout>" is a callout).
//     The last of a name it reads twice is the one it keeps
//     ("<callout color=\"red_bg\" _color=\"blue_bg\">" is blue).
//
// A tag's attributes are read whole, each name from the space before it, so
// that no name is read inside a longer one ("_href" is no href).
const notionMarkdownAttributeName = `[A-Za-z_][A-Za-z0-9_-]*`

// notionMarkdownAttributeAt is one attribute with its value in double
// quotes, its name all that stands from the space before it to its =.
var notionMarkdownAttributeAt = regexp.MustCompile(`(?:^|\s)([^\s="'<>/` + "`" + `]+)="([^"]*)"`)

// notionMarkdownAttributeNameCharacter reports whether character stands in a
// "v2" attribute's name, first when it would start it.
func notionMarkdownAttributeNameCharacter(character byte, first bool) bool {
	return notionMarkdownASCIILetter(character) || character == '_' ||
		!first && (character >= '0' && character <= '9' || character == '-')
}

// notionMarkdownVersion1AttributeName is the name "v1" reads an attribute
// of the tag named written as written by, and whether Leafen's reading of
// the tag keeps it: its letters from the first on, when the name holds only
// letters, digits, "_", ":" and "-". It keeps one named as Leafen's readings
// name (small letters and "-", and a mention's or a span's in "camelCase",
// as a date's timeZone), and one read past the characters before its first
// letter only when the tag takes it (notionMarkdownVersion1Takes): any other
// is one "v1" takes nothing from.
func notionMarkdownVersion1AttributeName(tag, written string) (string, bool) {
	for index := range len(written) {
		if character := written[index]; !notionMarkdownAttributeNameCharacter(character, false) && character != ':' {
			return "", false
		}
	}
	name := strings.TrimLeft(written, "_:-0123456789")
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return "", false
	}
	inline := !notionMarkdownVersion1BlockTags[tag] && !notionMarkdownVersion1CaptionTags[tag]
	for index := range len(name) {
		character := name[index]
		if !(character >= 'a' && character <= 'z' || character == '-' || inline && character >= 'A' && character <= 'Z') {
			return "", false
		}
	}
	return name, name == written || notionMarkdownVersion1Takes(tag, name)
}

// notionMarkdownVersion1Takes reports whether a "v1" tag takes an attribute
// of the name: one its "v2" tag takes, or a span's underline.
func notionMarkdownVersion1Takes(tag, name string) bool {
	converted := tag
	if legacy, ok := notionMarkdownLegacyTags[tag]; ok {
		converted = legacy
	}
	allowed, checked := notionMarkdownTagAttributes[converted]
	return !checked || allowed == nil || slices.Contains(allowed, name) || slices.Contains(notionMarkdownLegacyAttributes[tag], name) ||
		notionMarkdownLeafenAttributes[converted] == name || tag == "span" && name == "underline"
}
