package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxNotionInlineEquationRunes = 1000

type notionMentionRequest struct {
	Type string `json:"type"`
	Page *struct {
		ID string `json:"id"`
	} `json:"page,omitempty"`
	Database *struct {
		ID string `json:"id"`
	} `json:"database,omitempty"`
	CustomEmoji *struct {
		ID   string `json:"id"`
		Name string `json:"name,omitempty"`
		URL  string `json:"url,omitempty"`
	} `json:"custom_emoji,omitempty"`
	// A person may say it is one ("object": "user"), as Notion's takes it.
	User *struct {
		ID     string `json:"id"`
		Object string `json:"object,omitempty"`
	} `json:"user,omitempty"`
	Date *struct {
		Start    string  `json:"start"`
		End      *string `json:"end,omitempty"`
		TimeZone *string `json:"time_zone,omitempty"`
	} `json:"date,omitempty"`
	// A template's mention is refused outside a template, which the API
	// writes none of (notionMentionRefusal).
	TemplateMention json.RawMessage `json:"template_mention,omitempty"`
}

type notionBlockRichTextRequest struct {
	Type string `json:"type,omitempty"`
	Text *struct {
		Content string `json:"content"`
		Link    *struct {
			URL string `json:"url"`
		} `json:"link,omitempty"`
	} `json:"text,omitempty"`
	Equation *struct {
		Expression string `json:"expression"`
	} `json:"equation,omitempty"`
	Mention *notionMentionRequest `json:"mention,omitempty"`
	// What an item says it reads as and links to is left, as Notion's
	// leaves it (seen 2026-11-10).
	PlainText   *string `json:"plain_text,omitempty"`
	Href        *string `json:"href,omitempty"`
	Annotations *struct {
		Bold          bool   `json:"bold,omitempty"`
		Italic        bool   `json:"italic,omitempty"`
		Strikethrough bool   `json:"strikethrough,omitempty"`
		Underline     bool   `json:"underline,omitempty"`
		Code          bool   `json:"code,omitempty"`
		Color         string `json:"color,omitempty"`
	} `json:"annotations,omitempty"`
}

// notionNativeCaptionRequest reads a caption as the common rich-text union.
// Its mentions are checked and titled when the mutation is prepared; one
// Notion refuses for what it names is said so (notionMentionRefusal).
func notionNativeCaptionRequest(raw any) ([]map[string]any, error) {
	spans, err := notionTextRequestGraphSpans(raw)
	if refusal := (&notionMentionRefusal{}); errors.As(err, &refusal) {
		return nil, refusal
	}
	if err != nil || !validGraphBlockCaption(spans) {
		return nil, errors.New("block caption is invalid")
	}
	encoded, err := json.Marshal(spans)
	if err != nil {
		return nil, errors.New("block caption is invalid")
	}
	caption := []map[string]any{}
	if json.Unmarshal(encoded, &caption) != nil {
		return nil, errors.New("block caption is invalid")
	}
	return caption, nil
}

func notionTextRequestGraphSpans(raw any) ([]graphTextSpan, error) {
	return notionTextRequestSpans(raw, false)
}

// notionTextRequestSpans reads rich text as notionTextRequestGraphSpans
// does; keepPeople, for a value the MCP read from Markdown, keeps a mention
// of a UUID no person here has, by that UUID, for the check that follows to
// keep or refuse (notionMentionPreparation), and a link the reading made to
// a path in the app or an anchor, which Leafen's links take where Notion's
// JSON takes none.
func notionTextRequestSpans(raw any, keepPeople bool) ([]graphTextSpan, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, errors.New("block rich_text is invalid")
	}
	items := []notionBlockRichTextRequest{}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&items) != nil || len(items) > 100 {
		return nil, errors.New("block rich_text is invalid")
	}
	// As Notion's: up to 100 pieces, however long the block's text.
	spans := make([]graphTextSpan, 0, len(items))
	for _, item := range items {
		span, err := notionTextRequestGraphSpan(item, keepPeople)
		if err != nil {
			return nil, err
		}
		spans = append(spans, span)
	}
	return spans, nil
}

func notionTextRequestGraphSpan(item notionBlockRichTextRequest, keepPeople bool) (graphTextSpan, error) {
	span := graphTextSpan{}
	// An item without its type is the kind its one object names, as Notion
	// takes {"mention": {"page": …}} (seen 2026-11-10).
	if item.Type == "" {
		switch {
		case item.Mention != nil && item.Text == nil && item.Equation == nil:
			item.Type = "mention"
		case item.Equation != nil && item.Text == nil && item.Mention == nil:
			item.Type = "equation"
		}
	}
	switch item.Type {
	case "", "text":
		if item.Text == nil || item.Equation != nil || item.Mention != nil {
			return graphTextSpan{}, errors.New("text rich_text content is invalid")
		}
		span.Text = item.Text.Content
		if item.Text.Link != nil {
			link, err := notionTextLinkRequest(item.Text.Link.URL)
			if err != nil && keepPeople && validGraphTextLink(item.Text.Link.URL) {
				link, err = item.Text.Link.URL, nil
			}
			if err != nil {
				return graphTextSpan{}, err
			}
			span.Link = link
		}
	case "mention":
		if item.Text != nil || item.Equation != nil || item.Mention == nil {
			return graphTextSpan{}, errors.New("mention rich_text content is invalid")
		}
		var err error
		span, err = notionMentionRequestGraphSpan(*item.Mention)
		if refusal := (&notionMentionRefusal{}); keepPeople && errors.As(err, &refusal) && refusal.missingUser != "" {
			kept, _ := notionKeptUUID(refusal.missingUser)
			span, err = graphTextSpan{Text: notionUnknownPersonMentionText, Mention: "person", UserID: kept}, nil
		}
		if err != nil {
			return graphTextSpan{}, err
		}
	case "equation":
		if item.Text != nil || item.Equation == nil || item.Mention != nil ||
			item.Equation.Expression == "" ||
			len([]rune(item.Equation.Expression)) > maxNotionInlineEquationRunes {
			return graphTextSpan{}, errors.New("equation rich_text content is invalid")
		}
		span.Text = item.Equation.Expression
		span.Equation = item.Equation.Expression
	default:
		return graphTextSpan{}, errors.New("only text, equation, and supported mention rich_text items are available")
	}
	if annotations := item.Annotations; annotations != nil {
		if annotations.Color != "" && annotations.Color != "default" &&
			!validNotionBlockColor(annotations.Color) {
			return graphTextSpan{}, errors.New("rich_text annotation color is invalid")
		}
		span.Bold = annotations.Bold
		span.Italic = annotations.Italic
		span.Strikethrough = annotations.Strikethrough
		span.Underline = annotations.Underline
		span.Code = annotations.Code
		// A background color is the span's background, apart from its text color.
		span.Color, span.Background = splitNotionSpanColor(annotations.Color)
	}
	return span, nil
}

func notionMentionRequestGraphSpan(mention notionMentionRequest) (graphTextSpan, error) {
	if mention.Type == "" {
		mention.Type = notionMentionRequestKind(mention)
	}
	switch mention.Type {
	case "page":
		if mention.Page == nil || mention.Database != nil || mention.CustomEmoji != nil || mention.User != nil || mention.Date != nil {
			return graphTextSpan{}, errors.New("page mention is invalid")
		}
		publicID := strings.TrimSpace(mention.Page.ID)
		pageID, err := resolveNotionCompatiblePageID(publicID)
		if err != nil {
			// A database row page keeps a stored public ID; the mention
			// owner resolves it with the target's authorization.
			rowPublicID, ok := notionKeptUUID(publicID)
			if !ok {
				return graphTextSpan{}, errors.New("page mention is invalid")
			}
			pageID = notionPageMentionRowPrefix + rowPublicID
		}
		return graphTextSpan{Text: publicID, Mention: "page", PageID: pageID}, nil
	case "database":
		if mention.Database == nil || mention.Page != nil || mention.CustomEmoji != nil || mention.User != nil || mention.Date != nil {
			return graphTextSpan{}, errors.New("database mention is invalid")
		}
		publicID := strings.TrimSpace(mention.Database.ID)
		databaseID, ok := normalizeNotionUUIDRequest(publicID)
		if !ok {
			return graphTextSpan{}, errors.New("database mention is invalid")
		}
		return graphTextSpan{
			Text: databaseID, Mention: "database", DatabaseID: databaseID,
		}, nil
	case "custom_emoji":
		if mention.CustomEmoji == nil || mention.Page != nil || mention.Database != nil || mention.User != nil || mention.Date != nil {
			return graphTextSpan{}, errors.New("custom emoji mention is invalid")
		}
		// Kept as written, an emoji the workspace hasn't keeping its
		// capitals as Notion does (notionMissingCustomEmojiKept).
		customEmojiID, ok := notionCustomEmojiMentionWritten(strings.TrimSpace(mention.CustomEmoji.ID))
		if !ok {
			return graphTextSpan{}, errors.New("custom emoji mention is invalid")
		}
		return graphTextSpan{
			Text: customEmojiID, Mention: "custom_emoji", CustomEmojiID: customEmojiID,
		}, nil
	case "user":
		if mention.User == nil || mention.Page != nil || mention.Database != nil || mention.CustomEmoji != nil || mention.Date != nil {
			return graphTextSpan{}, errors.New("user mention is invalid")
		}
		// Its ID in either case names the person (notionKeptUUID).
		publicID := strings.TrimSpace(mention.User.ID)
		normalized, isUUID := normalizeNotionUUIDRequest(publicID)
		resolvable := publicID
		if isUUID {
			resolvable = normalized
		}
		userID, err := resolveNotionCompatibleUserID(resolvable)
		// A connection's bot is a user Notion's text mentions too; whether it
		// is one of the workspace's the mention's check finds.
		if err != nil && isUUID && notionBotUserIDShape(normalized) {
			return graphTextSpan{Text: "@" + publicID, Mention: "person", UserID: normalized}, nil
		}
		if err != nil && isUUID {
			return graphTextSpan{}, &notionMentionRefusal{missingUser: publicID}
		}
		if err != nil {
			return graphTextSpan{}, errors.New("user mention is invalid")
		}
		return graphTextSpan{Text: "@" + publicID, Mention: "person", UserID: userID}, nil
	case "date":
		if mention.Date == nil || mention.Page != nil || mention.Database != nil || mention.CustomEmoji != nil || mention.User != nil {
			return graphTextSpan{}, errors.New("a date mention has mention.date and nothing else")
		}
		start := strings.TrimSpace(mention.Date.Start)
		end := ""
		if mention.Date.End != nil {
			end = strings.TrimSpace(*mention.Date.End)
			if end == "" {
				return graphTextSpan{}, errors.New("mention.date.end takes a date or null, not an empty string.")
			}
		}
		timeZone := ""
		if mention.Date.TimeZone != nil {
			timeZone = strings.TrimSpace(*mention.Date.TimeZone)
			if timeZone == "" {
				return graphTextSpan{}, errors.New("mention.date.time_zone takes a time zone or null, not an empty string.")
			}
		}
		start, end, timeZone = notionMentionDateKept(start, end, timeZone)
		if !validNotionMentionDateRange(start, end, timeZone) {
			return graphTextSpan{}, notionMentionDateFault(start, end, timeZone)
		}
		// A date mention made through the API shows its full date, as Notion's does.
		return graphTextSpan{
			Text: notionMentionDatePlainText(start, end), Mention: "date",
			Date: start, DateEnd: end, DateTimeZone: timeZone, DateFormat: dateMentionFormatFull,
		}, nil
	case "template_mention":
		return graphTextSpan{}, &notionMentionRefusal{}
	default:
		return graphTextSpan{}, errors.New("mention type is not supported")
	}
}

func notionMentionDatePlainText(start, end string) string {
	if end == "" {
		return start
	}
	return start + " → " + end
}

// notionMentionDateFault says which part of a date mention isn't a date, as
// Notion's API says it of a date (seen 2026-10-29).
func notionMentionDateFault(start, end, zone string) error {
	if zone != "" {
		if _, err := time.LoadLocation(zone); err != nil || zone == "Local" {
			return fmt.Errorf("mention.date.time_zone takes a time zone, not `%q`.", zone)
		}
	}
	if _, ok := parseNotionMentionMoment(start, zone); !ok {
		return fmt.Errorf("mention.date.start takes a valid ISO 8601 date string, not `%q`.", start)
	}
	if _, ok := parseNotionMentionMoment(end, zone); !ok {
		return fmt.Errorf("mention.date.end takes a valid ISO 8601 date string, not `%q`.", end)
	}
	return errors.New("mention.date has an invalid date range: both ends are days or both are times, and the end is not before the start.")
}

// notionMentionRequestKind is the kind of a mention written without its
// type: the one object it names.
func notionMentionRequestKind(mention notionMentionRequest) string {
	kinds := []string{}
	for kind, named := range map[string]bool{
		"page": mention.Page != nil, "database": mention.Database != nil, "custom_emoji": mention.CustomEmoji != nil,
		"user": mention.User != nil, "date": mention.Date != nil, "template_mention": mention.TemplateMention != nil,
	} {
		if named {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) != 1 {
		return ""
	}
	return kinds[0]
}
