package app

import (
	"errors"
	"net/http"
	"strings"
)

// notionCommentMarkdownMentionsSaid is what refuses a comment's "v2" or MCP
// Markdown for its <mention> tags, "" when none is refused, as Notion's
// comment reads them (seen 2026-11-22, its public API's "v2" and its MCP
// alike; its "v1" keeps every one as words). Its reading refuses first, a
// line each after notionMarkdownRefusedHead, each tag a page's "v2" body
// refuses (notionMarkdownMentionSaid: no url, an address mentioning
// nothing, a type its address can't be). Then the first tag a comment
// can't hold: a teamspace's or a group's mention (Notion says the type,
// "team" or "group", then the types a comment's mention takes), or an
// address on the web that names no page — another site's, or one of
// Notion's (or this server's) whose path names no page by an ID a page
// could have — which a page's body links (notionMarkdownAddressMention)
// and a comment refuses as no page's address. One of a page's form that
// nothing here or in Notion answers to is taken, as Notion's is.
func notionCommentMarkdownMentionsSaid(markdown string, site *notionMarkdownSitePages) string {
	said, held := []string{}, ""
	for _, line := range strings.Split(markdown, "\n") {
		at := 0
		for _, code := range append(notionMarkdownCodeSpan.FindAllStringIndex(line, -1), []int{len(line), len(line)}) {
			text := line[at:code[0]]
			at = code[1]
			for _, found := range notionMarkdownRepairTag.FindAllStringSubmatchIndex(text, -1) {
				if found[3] != found[2] || text[found[4]:found[5]] != "mention" || notionMarkdownEscapedAt(text, found[0]) {
					continue
				}
				attributes := notionMarkdownAttributes(text[found[6]:found[7]])
				if refused := notionMarkdownMentionSaid("mention", attributes); refused != "" {
					said = append(said, refused)
				} else if held == "" {
					held = notionCommentMentionHeld(attributes, site)
				}
			}
		}
	}
	if len(said) > 0 {
		return notionMarkdownRefusedHead + strings.Join(said, "\n")
	}
	return held
}

// notionCommentNoPageAddress refuses a comment's mention of an address on
// the web that names no page (notionCommentMarkdownMentionsSaid), where
// Notion says "Invalid page mention URL. Expected a Notion page URL.", in
// Leafen's words.
const notionCommentNoPageAddress = "This mention's URL isn't a page's URL: give it the URL of a page."

// notionCommentMentionHeld is what refuses a <mention> a page's body takes
// but a comment can't hold, "" for one it holds.
func notionCommentMentionHeld(attributes map[string]string, site *notionMarkdownSitePages) string {
	address := strings.TrimSpace(attributes["url"])
	scheme, _, _ := strings.Cut(address, ":")
	switch strings.ToLower(scheme) {
	case "teamspace":
		return notionCommentMentionTypeSaid("team")
	case "group":
		return notionCommentMentionTypeSaid("group")
	}
	if span, ok := notionMarkdownAddressMention(address, "", attributes["type"] == "database", site); ok && span.Mention == "" && span.Link != "" {
		return notionCommentNoPageAddress
	}
	return ""
}

// notionCommentMarkdownSourceNotHere tells a write's refusal of a data
// source's mention naming none here of no ID's form
// (keepOrRefuseUnresolvedDataSource) from its failures.
func notionCommentMarkdownSourceNotHere(err error) bool {
	refusal := &graphRequestError{}
	return errors.As(err, &refusal) && refusal.status == http.StatusBadRequest
}
