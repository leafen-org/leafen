package app

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A notice keeps the words its page's title and its line had when it was
// sent, a page or database mention among them in its target's title as
// written. Each reader reads a notice as they read the page it is about
// (content_graph_reader_mentions.go): a mention they can't open — in the
// line's spans, in the comment the notice came from or the title of the
// database page or full-page database it names, as they stand now — keeps marks in place of its words
// (unnamedMentionWords), wherever those stand in the title and the line.
// Notion's inbox says them so (seen 2026-11-09, a guest): in a comment's or
// a page's line the crossed-out eye and "사용 권한 없음" ("찾을 수 없음" for
// one nothing answers to), in a database page's title "@찾을 수 없음". A notice's email, push, Slack message and desktop notice
// say them in those words (notificationLineAsItStands,
// notificationEmailHeadline), and an agent's inbox "Untitled", as all it
// reads does (mcp_reader_mentions.go).

// noticeWords is what of one notice its reader reads in words: the title,
// the line, and the line's spans when the notice keeps them. ItemID is the
// database item it names, when one (notification_item_now.go).
type noticeWords struct {
	PageID, BlockID, CommentID, ItemID string
	Title, Preview, Spans              *string
}

// noticesAsRead puts notices as their reader reads them. said, when not "",
// stands for a closed mention in place of its marks.
func noticesAsRead(app core.App, closer func() searchMentionCloser, notices []noticeWords, said string) {
	rows, comments := []any{}, []any{}
	for _, notice := range notices {
		for _, id := range []string{notice.PageID, notice.BlockID, notice.ItemID} {
			if id != "" {
				rows = append(rows, id)
			}
		}
		if notice.CommentID != "" {
			comments = append(comments, notice.CommentID)
		}
	}
	titles := noticeRowTitleSpans(app, rows)
	bodies := noticeCommentSpans(app, comments)
	words := func(text string, reach mentionReach) string {
		if said != "" {
			return said
		}
		return unnamedMentionWords(reach, text, true)
	}
	for _, notice := range notices {
		closed := map[string]mentionReach{}
		for _, id := range []string{notice.PageID, notice.BlockID, notice.ItemID} {
			noticeClosedTexts(titles[id], closer, closed)
		}
		if notice.Title != nil {
			*notice.Title = noticeWordsAsRead(*notice.Title, closed, words)
		}
		noticeClosedTexts(bodies[notice.CommentID], closer, closed)
		if notice.Spans != nil && strings.Contains(*notice.Spans, `"mention"`) {
			spans := []graphTextSpan{}
			if json.Unmarshal([]byte(*notice.Spans), &spans) == nil {
				marked := false
				for index, span := range spans {
					reach := searchMentionReach(closer(), span.Mention, span.PageID, span.DatabaseID)
					if span.Text == "" || reach == mentionOpens {
						continue
					}
					closed[span.Text] = reach
					spans[index].Text, marked = words(span.Text, reach), true
				}
				if encoded, err := json.Marshal(spans); err == nil && marked {
					*notice.Spans = string(encoded)
				}
			}
		}
		if notice.Preview != nil {
			*notice.Preview = noticeWordsAsRead(*notice.Preview, closed, words)
		}
	}
}

// noticeClosedTexts adds the words of each mention in spans the reader
// can't open, with how it stands to them.
func noticeClosedTexts(spans []graphTextSpan, closer func() searchMentionCloser, closed map[string]mentionReach) {
	for _, span := range spans {
		if reach := searchMentionReach(closer(), span.Mention, span.PageID, span.DatabaseID); span.Text != "" && reach != mentionOpens {
			closed[span.Text] = reach
		}
	}
}

// noticeWordsAsRead is words with each unnamed mention's words in them read
// as words says them, the longest first so a shorter one inside it doesn't
// break it.
func noticeWordsAsRead(text string, closed map[string]mentionReach, words func(string, mentionReach) string) string {
	if len(closed) == 0 || text == "" {
		return text
	}
	texts := make([]string, 0, len(closed))
	for written := range closed {
		texts = append(texts, written)
	}
	sort.Slice(texts, func(left, right int) bool { return len(texts[left]) > len(texts[right]) })
	for _, written := range texts {
		text = strings.ReplaceAll(text, written, words(written, closed[written]))
	}
	return text
}

// noticeRowTitleSpans is, of the pages named, each database page's title
// cell's spans that hold a mention.
func noticeRowTitleSpans(app core.App, ids []any) map[string][]graphTextSpan {
	found := map[string][]graphTextSpan{}
	if len(ids) == 0 {
		return found
	}
	rows := []struct {
		RowID string `db:"row_id"`
		Spans string `db:"spans"`
	}{}
	if app.DB().Select("value.row_id", "value.spans").From("leafen_database_row_values value").
		InnerJoin("leafen_database_properties property", dbx.NewExp("[[property.id]] = [[value.property_id]]")).
		Where(dbx.In("value.row_id", ids...)).AndWhere(dbx.HashExp{"property.type": graphValueTypeTitle}).
		AndWhere(dbx.NewExp(`instr([[value.spans]], '"mention"') > 0`)).All(&rows) != nil {
		return found
	}
	for _, row := range rows {
		found[row.RowID] = databaseStoredSpans(row.Spans)
	}
	// A full-page database's title is its name's (page_name_mentions.go).
	pages := make([]string, 0, len(ids))
	for _, id := range ids {
		if pageID, _ := id.(string); pageID != "" && found[pageID] == nil {
			pages = append(pages, pageID)
		}
	}
	if named, err := pagesNamedWithMentions(app, pages); err == nil {
		for _, pageID := range named {
			if spans, mentions := pageNameSpans(app, pageID); mentions {
				found[pageID] = spans
			}
		}
	}
	return found
}

// noticeCommentSpans is, of the comments named, each one's text that holds
// a mention, as it stands.
func noticeCommentSpans(app core.App, ids []any) map[string][]graphTextSpan {
	found := map[string][]graphTextSpan{}
	if len(ids) == 0 {
		return found
	}
	rows := []struct {
		ID   string `db:"id"`
		Body string `db:"body_json"`
	}{}
	if app.DB().Select("id", "body_json").From(commentsTable).Where(dbx.In("id", ids...)).
		AndWhere(dbx.NewExp(`instr([[body_json]], '"mention"') > 0`)).All(&rows) != nil {
		return found
	}
	for _, row := range rows {
		body := struct {
			Rich []graphTextSpan `json:"rich"`
		}{}
		if json.Unmarshal([]byte(row.Body), &body) == nil {
			found[row.ID] = body.Rich
		}
	}
	return found
}

// notificationInboxItemsAsRead puts the inbox's notices as their reader
// reads them, the marks left for the app to say, each with its page's or
// item's title spans while they spell its title, for the app to say its
// mentions as a plain title does (titleSpansAsReadFor).
func notificationInboxItemsAsRead(app core.App, readerID string, items []notificationInboxRecord) {
	notices := make([]noticeWords, len(items))
	spans := make([]string, len(items))
	for index := range items {
		spans[index] = string(items[index].PreviewSpans)
		notices[index] = noticeWords{PageID: items[index].PageID, BlockID: items[index].BlockID, CommentID: items[index].CommentID,
			ItemID: items[index].ItemID, Title: &items[index].Title, Preview: &items[index].Preview, Spans: &spans[index]}
	}
	closer := readerCloser(app, readerID)
	noticesAsRead(app, closer, notices, "")
	named := func(item notificationInboxRecord) string {
		if item.ItemID != "" {
			return item.ItemID
		}
		return item.PageID
	}
	titles := map[string]string{}
	for index := range items {
		if spans[index] != "" {
			items[index].PreviewSpans = json.RawMessage(spans[index])
		}
		if noticeNamesItsItem(items[index].ActivityKey) {
			titles[named(items[index])] = items[index].Title
		}
	}
	read := titleSpansAsReadFor(app, closer, titles)
	for index := range items {
		if found := read[named(items[index])]; noticeNamesItsItem(items[index].ActivityKey) && spelledName(found) == items[index].Title {
			items[index].TitleSpans = found
		}
	}
}

// notificationEmailItemsAsRead puts notices sent as words — an email, a
// push, a Slack message, a desktop notice — as their reader reads them,
// the marks left for notificationLineAsItStands and
// notificationEmailHeadline to say.
func notificationEmailItemsAsRead(app core.App, readerID string, items []storedNotificationEmailItem) {
	notices := make([]noticeWords, len(items))
	for index := range items {
		notices[index] = noticeWords{PageID: items[index].PageID, BlockID: items[index].BlockID, CommentID: items[index].CommentID,
			Title: &items[index].Title, Preview: &items[index].Preview, Spans: &items[index].PreviewSpans}
	}
	noticesAsRead(app, readerCloser(app, readerID), notices, "")
	// What it names — its database item (notificationItemOf), else its page —
	// is named while untitled by its kind (page_name_kind.go): a full-page
	// database "새 데이터베이스".
	named := func(item storedNotificationEmailItem) string {
		if id := notificationItemOf(item.Target); id != "" {
			return id
		}
		return item.PageID
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, named(item))
	}
	kinds, err := loadPageNameKinds(app, ids)
	if err != nil {
		return
	}
	for index := range items {
		items[index].NameKind = kinds[named(items[index])]
	}
}

// mcpInboxNoticesAsRead puts notices as an agent reads them: a closed
// mention "Untitled".
func mcpInboxNoticesAsRead(app core.App, authority mcpAuthority, items []mcpInboxNotice) {
	closes := mcpReaderCloses(app, authority)
	notices := make([]noticeWords, len(items))
	for index := range items {
		notices[index] = noticeWords{PageID: items[index].PageID, BlockID: items[index].BlockID, CommentID: items[index].CommentID,
			ItemID: items[index].ItemID, Title: &items[index].Title, Preview: &items[index].Preview, Spans: &items[index].PreviewSpans}
	}
	noticesAsRead(app, func() searchMentionCloser { return closes }, notices, notionUnresolvedMentionTitle)
}
