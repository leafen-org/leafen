package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// noticeNow is one notice to read as it stands now: what it is about, and
// its title, line and spans to put in their place, with the database item
// it names when it names one (notification_item_now.go).
type noticeNow struct {
	Kind, ActivityKey, PageID, BlockID, CommentID, Target string
	Title, Preview, Spans, ItemID                         *string
}

// notificationInboxItemsNow puts each of the inbox's notices as it stands
// now (noticesNow), with a connection's comment's notice naming the
// connection (notification_connection_authors.go), then as the reader reads
// it (notification_reader_mentions.go). The rows are ones the reader may see
// (visibleNotificationInboxRows).
func notificationInboxItemsNow(app core.App, readerID string, items []notificationInboxRecord) {
	notificationInboxConnectionAuthors(app, items)
	notices := make([]noticeNow, len(items))
	spans := make([]string, len(items))
	for index := range items {
		spans[index] = string(items[index].PreviewSpans)
		item := &items[index]
		notices[index] = noticeNow{Kind: item.Kind, ActivityKey: item.ActivityKey, PageID: item.PageID, BlockID: item.BlockID,
			CommentID: item.CommentID, Target: item.Target, Title: &item.Title, Preview: &item.Preview, Spans: &spans[index], ItemID: &item.ItemID}
	}
	noticesNow(app, notices)
	for index := range items {
		if spans[index] != "" {
			items[index].PreviewSpans = json.RawMessage(spans[index])
		}
	}
	notificationInboxItemsAsRead(app, readerID, items)
	notificationInboxItemKinds(app, items)
}

// notificationInboxItemKinds says the kind of the page each notice names —
// its database item, else its page — which names it while untitled where
// the reader's list doesn't hold it (page_name_kind.go): a full-page
// database "새 데이터베이스".
func notificationInboxItemKinds(app core.App, items []notificationInboxRecord) {
	named := func(item notificationInboxRecord) string {
		if item.ItemID != "" {
			return item.ItemID
		}
		return item.PageID
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if id := named(item); id != "" {
			ids = append(ids, id)
		}
	}
	kinds, err := loadPageNameKinds(app, ids)
	if err != nil {
		return
	}
	for index := range items {
		if kind := kinds[named(items[index])]; kind.Kind == pageKindDatabase {
			items[index].PageKind, items[index].ViewBlock, items[index].SourceTitles = kind.Kind, kind.ViewBlock, kind.SourceTitles
		}
	}
}

// mcpInboxNoticesNow puts an agent's notices as the app's inbox puts them
// now (noticesNow); the agent's reader rule follows (mcpInboxNoticesAsRead).
func mcpInboxNoticesNow(app core.App, items []mcpInboxNotice) {
	notices := make([]noticeNow, len(items))
	for index := range items {
		item := &items[index]
		notices[index] = noticeNow{Kind: item.Kind, ActivityKey: item.ActivityKey, PageID: item.PageID, BlockID: item.BlockID,
			CommentID: item.CommentID, Target: item.Target, Title: &item.Title, Preview: &item.Preview, Spans: &item.PreviewSpans, ItemID: &item.ItemID}
	}
	noticesNow(app, notices)
}

// noticesNow names each notice's database item as it is called now
// (noticeItemsNow) and reads its line as it stands now (noticeLinesNow).
func noticesNow(app core.App, notices []noticeNow) {
	noticeItemsNow(app, notices)
	noticeLinesNow(app, notices)
}

// noticeLinesNow puts the line a reminder or a mention in a page's text
// stands on now in place of the words it was sent with: Notion's inbox
// shows that block as it is (seen 2026-11-06: a reminder taken out of its
// line reads "비어 있음"). A block gone from its page, or a page that can't
// be read, keeps the words; each page is read once.
func noticeLinesNow(app core.App, lines []noticeNow) {
	wanted := map[string][]int{}
	ids := []any{}
	for index, line := range lines {
		if line.CommentID != "" || line.BlockID == "" || line.PageID == "" || (line.Kind != "reminder" && line.Kind != "mention") {
			continue
		}
		if _, seen := wanted[line.BlockID]; !seen {
			ids = append(ids, line.BlockID)
		}
		wanted[line.BlockID] = append(wanted[line.BlockID], index)
	}
	if len(ids) == 0 {
		return
	}
	blocks := []struct {
		ID           string `db:"id"`
		PageID       string `db:"page_id"`
		LastEditedBy string `db:"last_edited_by"`
		Content      string `db:"mention_content"`
	}{}
	if err := app.DB().Select("id", "page_id", "last_edited_by").
		AndSelect(`CASE WHEN instr([[content]], '"mention"') > 0 OR instr([[content]], '"link"') > 0
			THEN [[content]] ELSE '' END AS [[mention_content]]`).
		From("leafen_blocks").Where(dbx.In("id", ids...)).All(&blocks); err != nil {
		return
	}
	docs := map[string]*crdt.Doc{}
	for _, block := range blocks {
		doc, read := docs[block.PageID]
		if !read {
			loaded, found, err := loadYgoDoc(app, block.PageID)
			if err != nil || !found {
				loaded = nil
			}
			docs[block.PageID], doc = loaded, loaded
		}
		if doc == nil {
			continue
		}
		spans := graphTextSpansFromDelta(textDeltaAsItStands(blockMentionText(doc, storedReminderBlock{ID: block.ID, LastEditedBy: block.LastEditedBy, Content: block.Content}).ToDelta(), false))
		encoded := encodeNotificationLineSpans(spans)
		words := strings.Builder{}
		for _, span := range spans {
			words.WriteString(span.Text)
		}
		for _, index := range wanted[block.ID] {
			if lines[index].PageID != block.PageID {
				continue
			}
			// An emptied line is one empty span — an empty list is left out of
			// the answer — which the inbox says as Notion's "비어 있음"; no
			// spans at all is a line not read.
			*lines[index].Preview = notificationLinePlain(words.String())
			*lines[index].Spans = `[{"text":""}]`
			if encoded != "" && *lines[index].Preview != "" {
				*lines[index].Spans = encoded
			}
		}
	}
}

// notificationLinePlain is a line's words to a preview's length.
func notificationLinePlain(words string) string {
	runes := []rune(strings.TrimSpace(words))
	if len(runes) > notificationEmailPreviewRunes {
		runes = runes[:notificationEmailPreviewRunes]
	}
	return string(runes)
}
