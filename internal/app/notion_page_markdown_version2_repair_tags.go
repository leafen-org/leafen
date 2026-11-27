package app

import (
	"regexp"
	"strings"
)

// One tag of a "v2" line put right as Notion does (notionMarkdownRepair.tag),
// what its reading needs of it (check), and the words said of HTML and of
// tags neither reads.

// tag is one tag put right: alone is whether it is all its line, and block
// whether it stands where a block's tag is read — an opening one first on
// its line past its marks and block tags, a closing one where its line is
// a block's tags (repair.text).
func (repair *notionMarkdownRepair) tag(tag, name string, closing bool, attributes map[string]string, alone, block bool) string {
	switch {
	case name == "embed" && block && !closing:
		// One the editor makes another block is made it, and said.
		if warning, made := notionMarkdownEmbedWarning(attributes); made {
			repair.parsed = append(repair.parsed, warning)
		}
		return tag
	case notionMarkdownVersion2Tags[name]:
		return tag
	case name == "details" || name == "summary":
		// Read as v1's only standing where a block's tag is; inside text a
		// <details> is text, a <summary> a tag neither reads.
		if !block {
			if name == "summary" {
				repair.say("unsupported_tag", name, notionMarkdownUnsupportedSaid(name))
			} else {
				repair.standsInText(name, closing, strings.HasSuffix(tag, "/>"))
			}
			return `\` + tag
		}
		repair.olderDetails(name)
		return tag
	case notionMarkdownVersion1Tags[name] != "":
		repair.say("deprecated_tag", name, notionMarkdownOlderTagSaid(name, notionMarkdownVersion1Tags[name]))
		// A mention's or a date's is read as the <mention> or <date> it
		// becomes, its closing tag too (seen 2026-11-14:
		// "a<mention-page url=…>b⏎⏎c" is refused for its </mention>,
		// "a<mention-date start=…>b</date>" is the date, "a</mention-page>b"
		// the words "a</mention>b").
		if strings.HasPrefix(name, "mention-") {
			return notionMarkdownVersion1RenamedTag(tag)
		}
		// Standing in text, v1's block is named as the tag it becomes, and
		// its </table_of_contents> goes (seen 2026-11-14: "x </synced_block>
		// y" is "x </synced-block> y", "</table_of_contents> y" "y").
		if converted := notionMarkdownLegacyTags[name]; !block && converted == "table-of-contents" && closing {
			return ""
		} else if !block && strings.HasPrefix(name, "synced_") {
			return `\` + strings.Replace(tag, name, converted, 1)
		}
		return tag
	case notionMarkdownHTMLKept[name]:
		repair.say("html_tag_autofix", name, notionMarkdownHTMLTagSaid(name, "taken out and what it held kept. "+
			"Write that content without <"+name+"> next time."))
		// A block's tag leaves a space, an inline one's nothing (seen
		// 2026-11-14: "a<div>b</div>c" is "a b c", "a<sup>b</sup>c" "abc").
		if notionMarkdownHTMLSpaced[name] {
			return notionMarkdownRemovedSpace
		}
		return ""
	case name == "kbd":
		repair.say("html_tag_autofix", name, notionMarkdownHTMLSaid(name))
		if closing {
			return "</code>"
		}
		return "<code>"
	case name == "mark":
		repair.say("html_tag_autofix", name, notionMarkdownHTMLSaid(name))
		if closing {
			return "</span>"
		}
		return `<span color="yellow_bg">`
	case name == "img":
		repair.say("html_tag_autofix", name, notionMarkdownHTMLSaid(name))
		source := attributes["src"]
		if alone && (strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://")) {
			return "![](" + source + ")"
		}
		// Its alt text without the spaces at its ends, or a space (seen
		// 2026-11-14: alt=" q " is "q", none a space).
		if alt := strings.TrimSpace(attributes["alt"]); alt != "" {
			return alt
		}
		return notionMarkdownRemovedSpace
	case name == "hr":
		repair.say("html_tag_autofix", name, notionMarkdownHTMLSaid(name))
		if alone && !closing {
			return "---"
		}
		return notionMarkdownRemovedSpace
	case name == "blockquote":
		repair.say("html_tag_autofix", name, notionMarkdownHTMLSaid(name))
		return notionMarkdownRemovedSpace
	}
	repair.say("unsupported_tag", name, notionMarkdownUnsupportedSaid(name))
	return `\` + tag
}

func (repair *notionMarkdownRepair) refuse(words string) {
	if repair.refused == "" {
		repair.refused = words
	}
}

// check refuses an opening tag without what Notion's reading needs of it,
// each said after what is said of the blocks' unclosed tags (seen
// 2026-11-10).
func (repair *notionMarkdownRepair) check(name, tag string, attributes map[string]string) {
	address, addressed := attributes["url"]
	if converted := notionMarkdownLegacyTags[name]; strings.HasPrefix(name, "synced_") {
		name = converted
	}
	selfClosed := strings.HasSuffix(tag, "/>")
	// A mention's address is checked where a comment's is
	// (notionMarkdownMentionSaid).
	if said := notionMarkdownMentionSaid(name, attributes); said != "" {
		repair.missing = append(repair.missing, said)
		return
	}
	switch {
	case (name == "date" || name == "mention-date") && !notionMarkdownNamed(attributes, "start"):
		// An empty start is one, kept as written (notionMarkdownDateSpan).
		repair.missing = append(repair.missing, `<date> needs a start, as in <date start="...">...</date>.`)
	case name == "a":
		// One without an href, or with one naming an address Notion's links
		// don't open, is refused (notionMarkdownVersion2HrefRefused).
		if href, named := attributes["href"]; !named {
			repair.missing = append(repair.missing, "<a> has no href, so it stays as text.")
		} else if notionMarkdownVersion2HrefRefused(href) {
			repair.missing = append(repair.missing, `<a> can't link to "`+href+`", so it stays as text.`)
		}
	case (name == "link-preview" || name == "unknown_mention" || name == "folder" || name == "synced-block-reference") && !addressed:
		repair.missing = append(repair.missing, "<"+name+`> needs a url, as in <`+name+` url="...">...</`+name+">.")
	case name == "synced-block-reference" && address == "":
		repair.missing = append(repair.missing, `<synced-block-reference> needs a url, as in <synced-block-reference url="..."/>.`)
	case name == "link-to-page" && strings.TrimSpace(address) == "":
		// A link leads to a page (notion_page_markdown_link_to_page.go).
		repair.missing = append(repair.missing, `<link-to-page> needs the url of the page it leads to, as in <link-to-page url="..." />.`)
	case (name == "link-preview" || name == "unknown_mention" || name == "custom-block") && !selfClosed:
		repair.missing = append(repair.missing, notionMarkdownSelfClosingOnly(name))
	case name == "custom-block":
		// No definition it names is one Leafen has.
		repair.missing = append(repair.missing, "This custom-block tag can't be used: it takes one definition reference, a url if wanted, and closes itself.")
	case name == "external_object_instance":
		if integration, named := attributes["integration"]; !named {
			repair.missing = append(repair.missing, "<external_object_instance> needs an integration, "+
				`as in <external_object_instance integration="...">...</external_object_instance>.`)
		} else if integration != "figma" && integration != "github" && integration != "google_drive" {
			repair.missing = append(repair.missing, `<external_object_instance> takes integration figma, github or google_drive, not "`+
				integration+`".`)
		}
	case name == "folder" && repair.unmade == "":
		// One Notion's reading takes it then refuses to make.
		repair.unmade = "Folder blocks cannot be created."
		if notionMarkdownAddressHex(notionMarkdownAddressID(address)) == "" {
			repair.unmade = "Not a page or database URL: " + address
		}
	}
}

// notionMarkdownVersion2HrefScheme is the scheme an address starts with,
// as a browser reads one: a letter, then letters, digits, +, - and ., then a
// colon ("1:2" and "+:y" have none).
var notionMarkdownVersion2HrefScheme = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*):`)

// notionMarkdownVersion2HrefRefused reports an <a>'s href that Notion's "v2"
// refuses (seen 2026-11-15, create and replace alike, in a paragraph, a
// heading, an item, a quote and a table's cell, the tag closed in itself or
// left open too): one of nothing but spaces, or one naming a scheme other
// than http, https, mailto, tel, notion, slack and itms-apps, in any case,
// read as a browser reads an address — its tabs and line breaks left out and
// the spaces at its ends ("  javascript:x", "java⏎script:x", "ftp://a.com",
// "sms:1", "data:…", "file:///x", "x:", "a.com:80", "localhost:3000",
// "c:\x"). Any other is taken: one without a scheme ("a.com", "./x", "#x",
// "1:2", "javascript", "%20javascript:x", "javascript :x") too.
func notionMarkdownVersion2HrefRefused(href string) bool {
	address := strings.TrimFunc(strings.Map(func(character rune) rune {
		if character == '\t' || character == '\n' || character == '\r' {
			return -1
		}
		return character
	}, href), func(character rune) bool { return character <= ' ' })
	if address == "" {
		return true
	}
	scheme := notionMarkdownVersion2HrefScheme.FindStringSubmatch(address)
	if scheme == nil {
		return false
	}
	switch strings.ToLower(scheme[1]) {
	case "http", "https", "mailto", "tel", "notion", "slack", "itms-apps":
		return false
	}
	return true
}

// notionMarkdownMentionTypeClash reports whether a <mention>'s type names a
// kind its address can't (seen 2026-11-11): a person's (user://) is of type
// user, a data source's (collection://) data-source, an agent's agent, a
// teamspace's team and a group's group, and a web address page or
// database alike, which its target tells apart. No type clashes with none.
func notionMarkdownMentionTypeClash(kind, address string) bool {
	if kind == "" {
		return false
	}
	scheme, _, _ := strings.Cut(strings.TrimSpace(address), ":")
	switch strings.ToLower(scheme) {
	case "user":
		return kind != "user"
	case "collection":
		return kind != "data-source"
	case "agent":
		return kind != "agent"
	case "teamspace":
		return kind != "team"
	case "group":
		return kind != "group"
	}
	return kind != "page" && kind != "database"
}

// notionMarkdownMentionScheme reports whether a <mention>'s address is one
// "v2" mentions by: a web page's, a person's, an agent's, a data source's, a
// teamspace's or a group's.
func notionMarkdownMentionScheme(address string) bool {
	scheme, _, found := strings.Cut(strings.TrimSpace(address), ":")
	switch strings.ToLower(scheme) {
	case "http", "https", "user", "agent", "collection", "teamspace", "group", "pageorcollectionviewblock":
		return found
	}
	return false
}

// notionMarkdownHTMLTag tells HTML that Notion puts right, as Markdown or
// without its tags.
func notionMarkdownHTMLTag(name string) bool {
	_, dropped := notionMarkdownHTMLDropped[name]
	return notionMarkdownHTMLKept[name] || dropped || notionMarkdownHTMLWords[name] != ""
}

func notionMarkdownHTMLSaid(name string) string {
	return notionMarkdownHTMLTagSaid(name, notionMarkdownHTMLWords[name])
}

func notionMarkdownUnsupportedSaid(name string) string {
	return "<" + name + "> isn't a tag this Markdown knows, so it stays as text; write \\<" + name + "> when the text itself is meant."
}
