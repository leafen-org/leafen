package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person mention a page holds already stays when its text is written
// back, whoever writes it, as a person value keeps whom it holds
// (notion_people_value_held.go): a personal access token's bot, which only
// that token may mention anew, a connection or a token since
// stopped, someone who has left the workspace or whose invitation lapsed.
// Each was refused as naming no one, or, written as Markdown, kept as
// "@Anonymous", so text one road read and another wrote back failed whole
// or lost the mention (2026-11-26: a row titled "r @<a token's bot>" read
// by another token and written back, 404; read by the agent's fetch, which
// left the bot out, and written back, "r "). A mention keeps the ID and the
// words it is held with; one the page doesn't hold yet is checked as
// before (notionMentionPreparation.personMention).

// notionHeldMentions is whom a page's text mentions — its blocks' text,
// captions and table cells, its title, a database page's values — by the ID
// each mention keeps, with its words; read the first time a write's mention
// names no one its writer may name.
type notionHeldMentions struct {
	app             core.App
	ownerID, pageID string
	read            bool
	held            map[string]notionHeldMention
}

type notionHeldMention struct{ userID, words string }

// newNotionHeldMentions is what the page holds; none for a page not made yet.
func newNotionHeldMentions(app core.App, ownerID, pageID string) *notionHeldMentions {
	return &notionHeldMentions{app: app, ownerID: ownerID, pageID: pageID}
}

// mention is the mention the page holds of the person an ID written names —
// in either case, by a public or a Leafen ID — by the ID it keeps and its
// words; false for one it doesn't hold.
func (held *notionHeldMentions) mention(written string) (string, string, bool, error) {
	if held == nil || held.pageID == "" {
		return "", "", false, nil
	}
	if !held.read {
		if err := held.load(); err != nil {
			return "", "", false, err
		}
		held.read = true
	}
	written = strings.TrimSpace(written)
	candidates := []string{written, strings.ToLower(written)}
	if normalized, isUUID := normalizeNotionUUIDRequest(written); isUUID {
		candidates = append(candidates, strings.ToLower(normalized))
		if internal, err := resolveNotionCompatibleUserID(strings.ToLower(normalized)); err == nil {
			candidates = append(candidates, internal)
		}
	}
	if public, err := notionCompatibleUserID(written); err == nil {
		candidates = append(candidates, public)
	}
	for _, candidate := range candidates {
		if mention, found := held.held[candidate]; found {
			return mention.userID, mention.words, true, nil
		}
	}
	return "", "", false, nil
}

func (held *notionHeldMentions) add(spans []graphTextSpan) {
	for _, span := range spans {
		userID := strings.TrimSpace(span.UserID)
		if span.Mention != "person" || userID == "" {
			continue
		}
		mention := notionHeldMention{userID: span.UserID, words: span.Text}
		for _, key := range []string{userID, strings.ToLower(userID)} {
			if _, known := held.held[key]; !known {
				held.held[key] = mention
			}
		}
		if normalized, isUUID := normalizeNotionUUIDRequest(userID); isUUID {
			if _, known := held.held[strings.ToLower(normalized)]; !known {
				held.held[strings.ToLower(normalized)] = mention
			}
		}
	}
}

func (held *notionHeldMentions) load() error {
	held.held = map[string]notionHeldMention{}
	if record, err := held.app.FindRecordById(pagesCollection, held.pageID); err == nil {
		held.add(decodeStoredTextSpans(record.GetString("title_spans")))
	}
	values := []string{}
	if err := held.app.DB().NewQuery(`
		SELECT [[spans]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:pageId} AND [[spans]] != ''
	`).Bind(dbx.Params{"pageId": held.pageID}).Column(&values); err != nil {
		return err
	}
	for _, spans := range values {
		held.add(decodeStoredTextSpans(spans))
	}
	blocks := []struct {
		ID      string `db:"id"`
		Content string `db:"content"`
	}{}
	if err := held.app.DB().NewQuery(`
		SELECT [[id]], [[content]] FROM {{leafen_blocks}}
		WHERE [[owner]] = {:ownerId} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"ownerId": held.ownerID, "pageId": held.pageID}).All(&blocks); err != nil || len(blocks) == 0 {
		return err
	}
	// A caption's and a table cell's spans are in the block's content, its
	// text in the page's document.
	for _, block := range blocks {
		var content any
		if json.Unmarshal([]byte(block.Content), &content) == nil {
			held.add(notionHeldContentMentions(content))
		}
	}
	doc, err := loadRowPageTextDocument(held.app, held.pageID)
	if err != nil || doc == nil {
		return err
	}
	for _, block := range blocks {
		held.add(graphTextSpansFromYText(doc.GetText(ygoBlockTextRootPrefix + block.ID)))
	}
	return nil
}

// notionHeldContentMentions is the person mentions a block's content keeps,
// wherever in it a span stands.
func notionHeldContentMentions(content any) []graphTextSpan {
	spans := []graphTextSpan{}
	switch value := content.(type) {
	case map[string]any:
		if mention, _ := value["mention"].(string); mention == "person" {
			userID, _ := value["userId"].(string)
			text, _ := value["text"].(string)
			return append(spans, graphTextSpan{Mention: mention, UserID: userID, Text: text})
		}
		for _, nested := range value {
			spans = append(spans, notionHeldContentMentions(nested)...)
		}
	case []any:
		for _, nested := range value {
			spans = append(spans, notionHeldContentMentions(nested)...)
		}
	}
	return spans
}
