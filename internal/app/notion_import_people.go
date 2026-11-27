package app

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

const notionParagraphSourcePrefix = "leafen-notion-rich-text-v1:"

type notionParagraphSpan struct {
	Text       string         `json:"text"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// notionParagraphSourcePlan is a staged block's rich text. A converted block
// also names its native type and content; a paragraph keeps its content.
type notionParagraphSourcePlan struct {
	Text      string                `json:"text"`
	Spans     []notionParagraphSpan `json:"spans"`
	BlockType string                `json:"blockType,omitempty"`
	Content   map[string]any        `json:"content,omitempty"`
	// When Notion last changed the block, for a migration run again.
	EditedAt string `json:"editedAt,omitempty"`
	// Why the text kept its words but lost something, for the import report.
	Omitted []string `json:"-"`
	// It holds a linked database's place until the link phase makes it one.
	LinkedDatabase bool `json:"linkedDatabase,omitzero"`
}

func notionProviderPeopleValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []any{}, nil
	}
	var users []notionProviderUser
	if err := json.Unmarshal(raw, &users); err != nil {
		return nil, err
	}
	people := make([]any, 0, len(users))
	for _, user := range users {
		reference, err := notionProviderUserReference(user)
		if err != nil {
			return nil, err
		}
		people = append(people, reference)
	}
	return people, nil
}

func notionProviderUserReference(user notionProviderUser) (map[string]any, error) {
	id := strings.TrimSpace(user.ID)
	if id == "" {
		return nil, errors.New(errorCodeNotionImportProviderFailed)
	}
	sourceID := "notion-user:" + id
	displayName := optionalNotionString(user.Name)
	email := normalizedNotionEmail(user.Person.Email)
	if displayName == nil {
		displayName = email
	}
	if displayName == nil {
		displayName = optionalNotionString(user.Bot.WorkspaceName)
	}
	return map[string]any{
		"id": sourceID, "userId": sourceID, "notionUserId": id,
		"displayName": displayName, "email": email,
		"avatarUrl":      optionalNotionString(user.AvatarURL),
		"notionUserType": strings.TrimSpace(user.Type),
	}, nil
}

func optionalNotionString(value *string) any {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func normalizedNotionEmail(value *string) any {
	normalized := optionalNotionString(value)
	if normalized == nil {
		return nil
	}
	return strings.ToLower(normalized.(string))
}

func notionParagraphSourceText(object notionProviderObject) (string, error) {
	return notionProviderRichTextSourceText(object.Paragraph.RichText, false)
}

func notionProviderRichTextSourceText(parts []notionProviderRichText, strict bool) (string, error) {
	plan, err := notionProviderRichTextSourcePlan(parts, strict)
	if err != nil {
		return "", err
	}
	return encodeNotionParagraphSourcePlan(plan)
}

func encodeNotionParagraphSourcePlan(plan notionParagraphSourcePlan) (string, error) {
	encoded, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	return notionParagraphSourcePrefix + string(encoded), nil
}

// notionProviderRichTextSourcePlan keeps the words of all it is given: what
// they can't keep is left off and named in the plan, and strict takes no such
// loss, nor a mention.
func notionProviderRichTextSourcePlan(parts []notionProviderRichText, strict bool) (notionParagraphSourcePlan, error) {
	plan := notionParagraphSourcePlan{Spans: make([]notionParagraphSpan, 0, len(parts))}
	omit := func(reason string) {
		if !slices.Contains(plan.Omitted, reason) {
			plan.Omitted = append(plan.Omitted, reason)
		}
	}
	for _, part := range parts {
		text := part.PlainText
		if part.Type == "equation" && text == "" {
			text = part.Equation.Expression
		}
		// A link mention shows its page's title, as in Notion, where its plain
		// text is its address.
		if title := strings.TrimSpace(part.Mention.LinkMention.Title); part.Type == "mention" &&
			part.Mention.Type == "link_mention" && title != "" && notionProviderMentionReference(part.Mention) != nil {
			text = title
		}
		// An empty equation, and a text that is one empty piece (code page
		// Markdown made with nothing in it), are kept as Notion reads them
		// (graph_text_empty_pieces.go).
		empty := ""
		switch {
		case text != "":
		case part.Type == "equation" && part.Equation.Expression == "":
			empty = graphTextEmptyEquation
		case (part.Type == "" || part.Type == "text") && len(parts) == 1:
			empty = graphTextEmptyText
		default:
			continue
		}
		// A date mention reads "@…", as one made in Leafen does; Notion's
		// plain text for it is the bare date.
		if part.Type == "mention" && part.Mention.Type == "date" && !strings.HasPrefix(text, "@") &&
			notionProviderMentionReference(part.Mention) != nil {
			text = "@" + text
		}
		span := notionParagraphSpan{Text: text, Attributes: map[string]any{}}
		for key, enabled := range map[string]bool{
			"bold": part.Annotations.Bold, "italic": part.Annotations.Italic,
			"strikethrough": part.Annotations.Strikethrough,
			"underline":     part.Annotations.Underline, "code": part.Annotations.Code,
		} {
			if enabled {
				span.Attributes[key] = true
			}
		}
		if color := strings.TrimSpace(part.Annotations.Color); color != "" && color != "default" {
			if validNotionBlockColor(color) {
				// A background is the span's background, apart from its text color.
				key := "color"
				if strings.HasSuffix(color, "_background") {
					key = "background"
				}
				span.Attributes[key] = color
			} else {
				omit(notionImportIssueSettingOmitted)
			}
		}
		switch part.Type {
		case "", "text":
			if part.Text.Link != nil {
				if link := notionImportTextLink(strings.TrimSpace(part.Text.Link.URL)); link != "" {
					span.Attributes["link"] = link
				} else {
					omit(notionImportIssueTextLinkOmitted)
				}
			}
		case "equation":
			expression := strings.TrimSpace(part.Equation.Expression)
			if expression != "" && expression == text && len([]rune(expression)) <= maxNotionInlineEquationRunes {
				span.Attributes["equation"] = expression
			} else if empty == "" {
				omit(notionImportIssueSettingOmitted)
			}
		case "mention":
			if part.Mention.Type == "user" {
				break
			}
			if strict {
				return notionParagraphSourcePlan{}, errors.New(errorCodeNotionImportProviderFailed)
			}
			for key, value := range notionProviderMentionReference(part.Mention) {
				span.Attributes[key] = value
			}
			// One of a block names the block until the import is published
			// (notion_import_references.go).
			if block := notionProviderMentionedBlock(part); block != "" && span.Attributes["pageId"] != nil {
				span.Attributes["blockId"] = notionImportPendingBlockPrefix + block
			}
		default:
			if strict {
				return notionParagraphSourcePlan{}, errors.New(errorCodeNotionImportProviderFailed)
			}
		}
		if part.Mention.Type == "user" {
			if user, err := notionProviderUserReference(part.Mention.User); err == nil {
				span.Attributes["mention"] = "person"
				span.Attributes["userId"] = user["userId"]
				span.Attributes["notionUser"] = user
			} else {
				omit(notionImportIssueSettingOmitted)
			}
		}
		if empty != "" {
			span.Attributes["emptyPiece"] = empty
		}
		if len(span.Attributes) == 0 {
			span.Attributes = nil
		}
		plan.Text += text
		plan.Spans = append(plan.Spans, span)
	}
	if strict && len(plan.Omitted) > 0 {
		return notionParagraphSourcePlan{}, errors.New(errorCodeNotionImportProviderFailed)
	}
	return plan, nil
}

func decodeNotionParagraphSourceText(value string) (notionParagraphSourcePlan, error) {
	if !strings.HasPrefix(value, notionParagraphSourcePrefix) {
		spans := []notionParagraphSpan{}
		if value != "" {
			spans = append(spans, notionParagraphSpan{Text: value})
		}
		return notionParagraphSourcePlan{Text: value, Spans: spans}, nil
	}
	var plan notionParagraphSourcePlan
	if err := json.Unmarshal([]byte(strings.TrimPrefix(value, notionParagraphSourcePrefix)), &plan); err != nil {
		return notionParagraphSourcePlan{}, errors.New(errorCodeNotionImportCreateFailed)
	}
	text := ""
	for _, span := range plan.Spans {
		text += span.Text
	}
	if text != plan.Text {
		return notionParagraphSourcePlan{}, errors.New(errorCodeNotionImportCreateFailed)
	}
	return plan, nil
}

func notionSourceTextGraphSpans(value string) ([]graphTextSpan, error) {
	plan, err := decodeNotionParagraphSourceText(value)
	if err != nil {
		return nil, err
	}
	spans := make([]graphTextSpan, 0, len(plan.Spans))
	for _, source := range plan.Spans {
		span, err := notionSourceGraphSpan(source)
		if err != nil {
			return nil, err
		}
		spans = append(spans, span)
	}
	if !validGraphBlockCaption(spans) {
		return nil, errors.New(errorCodeNotionImportCreateFailed)
	}
	return spans, nil
}

// notionSourceGraphSpan reads one imported rich-text span for block content.
// A person mention keeps the imported person's ID until imported people
// mapping chooses a native directory identity for it.
func notionSourceGraphSpan(source notionParagraphSpan) (graphTextSpan, error) {
	span := graphTextSpan{Text: source.Text}
	for key, raw := range source.Attributes {
		switch key {
		case "bold":
			span.Bold, _ = raw.(bool)
		case "italic":
			span.Italic, _ = raw.(bool)
		case "underline":
			span.Underline, _ = raw.(bool)
		case "strikethrough":
			span.Strikethrough, _ = raw.(bool)
		case "code":
			span.Code, _ = raw.(bool)
		case "color":
			color, _ := raw.(string)
			span.Color, span.Background = splitNotionSpanColor(color)
		case "background":
			span.Background, _ = raw.(string)
		case "link":
			span.Link, _ = raw.(string)
		case "equation":
			span.Equation, _ = raw.(string)
		case "emptyPiece":
			span.EmptyPiece, _ = raw.(string)
		case "mention":
			span.Mention, _ = raw.(string)
		case "template":
			span.Template, _ = raw.(string)
		case "userId":
			span.UserID, _ = raw.(string)
		case "pageId":
			span.PageID, _ = raw.(string)
		case "blockId":
			span.BlockID, _ = raw.(string)
		case "databaseId":
			span.DatabaseID, _ = raw.(string)
		case "date":
			span.Date, _ = raw.(string)
		case "dateStartUnset":
			span.DateStartUnset, _ = raw.(bool)
		case "dateEnd":
			span.DateEnd, _ = raw.(string)
		case "dateTimeZone":
			span.DateTimeZone, _ = raw.(string)
		case "iconUrl":
			span.IconURL, _ = raw.(string)
		case "provider":
			span.Provider, _ = raw.(string)
		case "description":
			span.Description, _ = raw.(string)
		case "imageUrl":
			span.ImageURL, _ = raw.(string)
		case "notionUser":
		default:
			return graphTextSpan{}, errors.New(errorCodeNotionImportCreateFailed)
		}
	}
	return span, nil
}
