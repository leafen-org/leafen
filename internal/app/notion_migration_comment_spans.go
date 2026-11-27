package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A moved comment keeps its formatting, links and mentions (2026-10-14: a
// comment "Looks **good** to me." came over as plain words): each piece of
// the source's rich text becomes a run of the comment's text with its marks
// and color, a link, an equation, a date or link mention as it is, a page or
// database mention of what the migration brought pointing at that (at the
// block a mention of one names), and a
// person the plan maps to a member a mention of them. Anything else stays
// the words the source showed for it.

// notionMigrationCommentSpans is a comment's rich text as a Leafen
// comment's runs, after a first line of its own when there is one; the
// runs past the most a comment holds join the last one as words.
func notionMigrationCommentSpans(
	resolver *notionImportReferenceResolver, members map[string]string, first string, parts []notionProviderRichText,
) ([]mcpCommentSpan, error) {
	spans := []mcpCommentSpan{}
	if first != "" {
		spans = append(spans, mcpCommentSpan{Text: first})
	}
	for _, part := range parts {
		span, err := notionMigrationCommentSpan(resolver, members, part)
		if err != nil {
			return nil, err
		}
		if span.Text != "" {
			spans = append(spans, span)
		}
	}
	if len(spans) > mcpCommentMaximumRichTextItems {
		rest := []string{}
		for _, span := range spans[mcpCommentMaximumRichTextItems-1:] {
			rest = append(rest, span.Text)
		}
		spans = append(spans[:mcpCommentMaximumRichTextItems-1], mcpCommentSpan{Text: strings.Join(rest, "")})
	}
	if len(spans) == 0 {
		spans = append(spans, mcpCommentSpan{Text: ""})
	}
	return spans, nil
}

func notionMigrationCommentSpan(
	resolver *notionImportReferenceResolver, members map[string]string, part notionProviderRichText,
) (mcpCommentSpan, error) {
	span := mcpCommentSpan{
		Text: part.PlainText, Bold: part.Annotations.Bold, Italic: part.Annotations.Italic, Underline: part.Annotations.Underline,
		Strikethrough: part.Annotations.Strikethrough, Code: part.Annotations.Code,
	}
	if color := strings.TrimSpace(part.Annotations.Color); color != "default" && len(color) <= 64 {
		span.Color = color
	}
	switch part.Type {
	case "", "text":
		if part.Text.Content != "" {
			span.Text = part.Text.Content
		}
		if part.Text.Link != nil && part.Text.Link.URL != "" && validMCPCommentLink(part.Text.Link.URL) {
			span.Link = part.Text.Link.URL
		}
	case "equation":
		if expression := part.Equation.Expression; expression != "" {
			span.Equation, span.Text = expression, expression
		}
	case "mention":
		mention := part.Mention
		switch mention.Type {
		case "page", "database":
			attribute, value := "pageId", notionImportPendingPagePrefix+strings.TrimSpace(mention.Page.ID)
			if mention.Type == "database" {
				attribute, value = "databaseId", notionImportPendingDatabasePrefix+strings.TrimSpace(mention.Database.ID)
			}
			// One of a block keeps the block, as the page's text does
			// (notion_import_references.go).
			anchor := ""
			if block := notionProviderMentionedBlock(part); block != "" {
				anchor = notionImportPendingBlockPrefix + block
			}
			nativeID, blockID, err := resolver.resolveMention(attribute, value, anchor)
			if err != nil {
				return mcpCommentSpan{}, err
			}
			if nativeID != "" && mention.Type == "page" {
				span.Mention, span.PageID, span.BlockID = "page", nativeID, blockID
			} else if nativeID != "" {
				span.Mention, span.DatabaseID = "database", nativeID
			}
		case "user":
			if member := members[canonicalNotionID(mention.User.ID)]; member != "" {
				span.Mention, span.UserID = "person", member
				if !strings.HasPrefix(span.Text, "@") {
					span.Text = "@" + span.Text
				}
			}
		case "date":
			if reference := notionProviderMentionReference(mention); reference != nil {
				span.Mention = "date"
				if !strings.HasPrefix(span.Text, "@") {
					span.Text = "@" + span.Text
				}
				span.Date, _ = reference["date"].(string)
				span.DateEnd, _ = reference["dateEnd"].(string)
				span.DateTimeZone, _ = reference["dateTimeZone"].(string)
			}
		case "link_mention":
			if reference := notionProviderMentionReference(mention); reference != nil {
				span.Mention = "external"
				span.Link, _ = reference["link"].(string)
			}
		}
	}
	return span, nil
}

// notionMigrationPlanMembers is the plan's people: each Notion person's
// member, by the person's ID.
func notionMigrationPlanMembers(app core.App, migrationID string) (map[string]string, error) {
	var stored string
	if err := app.DB().NewQuery(`SELECT [[plan_json]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": migrationID}).Row(&stored); err != nil {
		return nil, err
	}
	plan := notionMigrationPlan{}
	members := map[string]string{}
	if json.Unmarshal([]byte(stored), &plan) != nil {
		return members, nil
	}
	for _, person := range plan.People {
		if member := strings.TrimSpace(person.MemberID); member != "" {
			members[canonicalNotionID(person.NotionUserID)] = member
		}
	}
	return members, nil
}
