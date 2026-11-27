package app

import (
	"net/url"
	"strings"
)

// notionProviderMentionedBlock is the Notion block a page mention names, by
// its address's fragment, or "".
func notionProviderMentionedBlock(part notionProviderRichText) string {
	if part.Type != "mention" || part.Mention.Type != "page" {
		return ""
	}
	address, err := url.Parse(strings.TrimSpace(part.Href))
	if err != nil {
		return ""
	}
	blockID := notionLinkedID(address.Fragment)
	if blockID == notionLinkedID(strings.TrimSpace(part.Mention.Page.ID)) {
		return ""
	}
	return blockID
}

// notionProviderTemplateMentionReference keeps a template's "when
// duplicated" mention as one — a date ("today", "now") or a person ("me") —
// which the template the import makes of its page (notion_import_templates.go)
// gives its value as it is used; one of another value stays its text.
func notionProviderTemplateMentionReference(mention notionProviderMention) map[string]any {
	value := mention.TemplateMention.Date
	if mention.TemplateMention.Type == "template_mention_user" {
		value = mention.TemplateMention.User
	}
	if !validGraphTemplateMention(value) {
		return nil
	}
	return map[string]any{"mention": graphTemplateMention, "template": value}
}
