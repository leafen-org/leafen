package app

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's graph reaches its reader as they may read it, as its document
// does (collaboration_reader_mentions.go). A page or database mention they
// can't open — in the text a block keeps in its content, a caption, a
// table's cells, a database row's title and text cells, or a database's
// name and description, a linked one's own name too — keeps its place
// with marks for words, no-access or not-found (unnamedMentionWords), and
// the "unavailable" mark, as a public page's does
// (public_content_mentions.go). A page link or sub-page block naming a
// page closed to them keeps neither the page's title nor its icon, only the
// "pageLinkUnavailable" mark: a page link then reads "사용 권한 없음" by the
// crossed-out eye, and a sub-page isn't drawn, as in Notion (seen
// 2026-11-09, a guest).

// The block content fields holding spans, and those holding a table row's
// cells of spans.
var (
	graphReaderSpanFields   = []string{"rich", "caption", "notionRichTextSpans"}
	graphReaderPageLinkKeys = []string{"childPageTitle", "childPageIcon", "childPageIconType"}
)

// graphItemsAsRead puts a page's graph items as their reader reads them. The
// pages they name are worked out together first (mention_closer_learning.go).
func graphItemsAsRead(app core.App, readerID string, items []graphReadItem) []graphReadItem {
	walk := func(closer func() searchMentionCloser) {
		for _, item := range items {
			switch item.Kind {
			case graphReadKindBlock:
				graphBlockAsRead(item.Value, closer)
			case graphReadKindRow:
				values, _ := item.Value["values"].(map[string]any)
				for _, raw := range values {
					if value, ok := raw.(map[string]any); ok {
						graphCellAsRead(value, closer)
					}
				}
			case graphReadKindDataSource:
				graphNameAsRead(item.Value, "title", "titleSpans", closer)
				graphNameAsRead(item.Value, "description", "descriptionSpans", closer)
			}
		}
	}
	walk(readerCloserFor(app, readerID, walk))
	return items
}

// graphNameAsRead reads a database's name or description, or a linked
// database's own name, as its reader reads it, as Notion's database name
// reads (seen 2026-11-10: "사용 권한 없음" by the crossed-out eye to a guest
// the mentioned page is closed to): a mention in its spans they can't open
// in marks, and its words, while the spans spell them, again from the spans.
func graphNameAsRead(value map[string]any, wordsKey, spansKey string, closer func() searchMentionCloser) {
	spans, ok := value[spansKey].([]any)
	if !ok {
		return
	}
	spelled := graphSpansWords(spans)
	if !graphSpansAsRead(spans, closer) {
		return
	}
	if words, plain := value[wordsKey].(string); plain && words == spelled {
		value[wordsKey] = graphSpansWords(spans)
	}
}

// graphBlockAsRead reads a block's content as its reader reads it.
func graphBlockAsRead(block map[string]any, closer func() searchMentionCloser) {
	content, ok := block["content"].(map[string]any)
	if !ok {
		return
	}
	switch block["type"] {
	case graphBlockTypeLinkToPage, graphBlockTypeChildPage:
		target, _ := content["childPageId"].(string)
		if target == "" {
			target, _ = content["pageId"].(string)
		}
		if target != "" && target != block["pageId"] && closer()(pageBlockMention, target) != mentionOpens {
			for _, key := range graphReaderPageLinkKeys {
				delete(content, key)
			}
			content[publicPageLinkUnavailableKey] = true
		}
	}
	for _, field := range graphReaderSpanFields {
		if spans, ok := content[field].([]any); ok && graphSpansAsRead(spans, closer) && field != "caption" {
			if _, plain := content["text"].(string); plain {
				content["text"] = graphSpansWords(spans)
			}
		}
	}
	if cells, ok := content["cells"].([]any); ok {
		for _, cell := range cells {
			if spans, ok := cell.([]any); ok {
				graphSpansAsRead(spans, closer)
			}
		}
	}
	// A database's name on its block: a linked one's own, and the one its
	// page's title spells (database_page_name_spans.go), which the page's
	// title reads as (page_name_mentions.go).
	graphNameAsRead(content, "linkedTitle", "linkedTitleSpans", closer)
	if spans, ok := content["pageTitleSpans"].([]any); ok {
		graphSpansAsRead(spans, closer)
	}
}

// graphCellAsRead reads a database cell as its reader reads it: a title or
// text cell's words again from its spans when one of them was closed.
func graphCellAsRead(value map[string]any, closer func() searchMentionCloser) {
	words := ""
	switch spans := value["spans"].(type) {
	case []graphTextSpan:
		read, marked := typedSpansAsRead(spans, closer)
		if !marked {
			return
		}
		value["spans"] = read
		for _, span := range read {
			words += span.Text
		}
	case []any:
		if !graphSpansAsRead(spans, closer) {
			return
		}
		words = graphSpansWords(spans)
	default:
		return
	}
	if _, plain := value["value"].(string); plain {
		value["value"] = words
	}
}

// typedSpansAsRead is spans with each page or database mention the reader
// can't open in marks, and whether any was.
func typedSpansAsRead(spans []graphTextSpan, closer func() searchMentionCloser) ([]graphTextSpan, bool) {
	if !querySpansHoldMention(spans) {
		return spans, false
	}
	var read []graphTextSpan
	for index, span := range spans {
		reach := searchMentionReach(closer(), span.Mention, span.PageID, span.DatabaseID)
		if reach == mentionOpens {
			continue
		}
		if read == nil {
			read = slices.Clone(spans)
		}
		read[index].Text = unnamedMentionWords(reach, span.Text, true)
	}
	if read == nil {
		return spans, false
	}
	return read, true
}

// graphSpansAsRead marks, in spans, each page or database mention closed to
// the reader, and reports whether it marked any.
func graphSpansAsRead(spans []any, closer func() searchMentionCloser) bool {
	if !graphSpansHoldMention(spans) {
		return false
	}
	marked := false
	for _, raw := range spans {
		span, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		mention, _ := span["mention"].(string)
		pageID, _ := span["pageId"].(string)
		databaseID, _ := span["databaseId"].(string)
		reach := searchMentionReach(closer(), mention, pageID, databaseID)
		if reach == mentionOpens {
			continue
		}
		text, _ := span["text"].(string)
		span["text"] = unnamedMentionWords(reach, text, true)
		span[publicMentionUnavailableKey] = true
		marked = true
	}
	return marked
}

func graphSpansHoldMention(spans []any) bool {
	for _, raw := range spans {
		if span, ok := raw.(map[string]any); ok && searchMentionHasNoWords(stringField(span, "mention")) {
			return true
		}
	}
	return false
}

func graphSpansWords(spans []any) string {
	var words strings.Builder
	for _, raw := range spans {
		if span, ok := raw.(map[string]any); ok {
			words.WriteString(stringField(span, "text"))
		}
	}
	return words.String()
}

func stringField(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return text
}

// queryRowsAsRead puts the rows a view's query answers its reader with as
// they read them: in a title or text cell, a mention they can't open has
// marks for words.
func queryRowsAsRead(app core.App, readerID string, rows []databaseQueryRow) {
	closer := readerCloser(app, readerID)
	for _, row := range rows {
		for propertyID, value := range row.Values {
			if !querySpansHoldMention(value.Spans) {
				continue
			}
			spans, marked := typedSpansAsRead(value.Spans, closer)
			if !marked {
				continue
			}
			var words strings.Builder
			for _, span := range spans {
				words.WriteString(span.Text)
			}
			if encoded, err := json.Marshal(words.String()); err == nil && len(value.Value) > 0 && value.Value[0] == '"' {
				value.Value = encoded
			}
			value.Spans = spans
			row.Values[propertyID] = value
		}
	}
}

func querySpansHoldMention(spans []graphTextSpan) bool {
	for _, span := range spans {
		if searchMentionHasNoWords(span.Mention) {
			return true
		}
	}
	return false
}

// pageTitleAsRead is a page's title as its reader reads it: a database
// page's title cell's words, or a full-page database's name's, a mention in
// them they can't open in marks (page_name_mentions.go).
func pageTitleAsRead(app core.App, readerID string, page *core.Record) string {
	return rowTitleAsRead(app, readerID, page.Id, page.GetString("title"))
}

// rowTitleAsRead is a page's title, as written, as its reader reads it.
func rowTitleAsRead(app core.App, readerID, rowID, title string) string {
	return pageTitleAsReadBy(app, readerCloser(app, readerID), rowID, title)
}

// readerCloser is a person's closer for the page they read, made once it is
// first asked.
func readerCloser(app core.App, readerID string) func() searchMentionCloser {
	var closes searchMentionCloser
	return func() searchMentionCloser {
		if closes == nil {
			closes = pageReader(readerID).closes(app)
		}
		return closes
	}
}

// commentsAsRead puts the comments a page's discussions list for its reader
// as they read them: a page mention they can't open in marks.
func commentsAsRead(app core.App, readerID string, comments []commentRecord) {
	closer := readerCloser(app, readerID)
	for index := range comments {
		if !bytes.Contains(comments[index].Body, []byte(`"mention"`)) {
			continue
		}
		body := map[string]any{}
		if json.Unmarshal(comments[index].Body, &body) != nil {
			continue
		}
		if spans, ok := body["rich"].([]any); ok && graphSpansAsRead(spans, closer) {
			if encoded, err := json.Marshal(body); err == nil {
				comments[index].Body = encoded
			}
		}
	}
}

// rowPageAsRead puts a database page as its reader reads it: its cells and
// its body blocks, as its graph's are (graphItemsAsRead).
func rowPageAsRead(app core.App, readerID string, page *databaseRowPageSnapshot) {
	walk := func(closer func() searchMentionCloser) {
		if bytes.Contains(page.Row.Values, []byte(`"mention"`)) {
			values := map[string]map[string]any{}
			if json.Unmarshal(page.Row.Values, &values) == nil {
				for _, value := range values {
					graphCellAsRead(value, closer)
				}
				if encoded, err := json.Marshal(values); err == nil {
					page.Row.Values = encoded
				}
			}
		}
		for index, block := range page.Body {
			page.Body[index].Content, _, _ = rawBlockContentAsRead(block.Type, block.Content, closer)
		}
	}
	walk(readerCloserFor(app, readerID, walk))
}

// rawBlockContentAsRead is a block's stored content as its reader reads it
// (graphBlockAsRead), and whether it read it again.
func rawBlockContentAsRead(blockType string, raw json.RawMessage, closer func() searchMentionCloser) (json.RawMessage, map[string]any, bool) {
	if !bytes.Contains(raw, []byte(`"mention"`)) && !bytes.Contains(raw, []byte(`"childPageId"`)) &&
		!bytes.Contains(raw, []byte(`"pageId"`)) {
		return raw, nil, false
	}
	content := map[string]any{}
	if json.Unmarshal(raw, &content) != nil {
		return raw, nil, false
	}
	graphBlockAsRead(map[string]any{"type": blockType, "content": content}, closer)
	encoded, err := json.Marshal(content)
	if err != nil {
		return raw, nil, false
	}
	return encoded, content, true
}

// pageHistoryAsRead puts a page's past version as its reader reads it: its
// blocks and its cells, as the page's graph has them now.
func pageHistoryAsRead(app core.App, readerID string, version *pageHistorySnapshotResponse) {
	walk := func(closer func() searchMentionCloser) {
		for index, block := range version.Blocks {
			read, content, again := rawBlockContentAsRead(block.Type, block.Content, closer)
			if !again {
				continue
			}
			version.Blocks[index].Content = read
			if spans, ok := content["rich"].([]any); ok {
				version.Blocks[index].PlainText = graphSpansWords(spans)
			}
		}
		// A database page's title is its title cell's words.
		if title, marked := historyValuesAsRead(version.Values, closer); marked && title != nil {
			version.Title = *title
		}
		for _, database := range version.Databases {
			for _, row := range database.Rows {
				historyValuesAsRead(row.Values, closer)
			}
		}
	}
	walk(readerCloserFor(app, readerID, walk))
}

// historyValuesAsRead marks, in a version's cells, each mention closed to
// its reader, and gives the title cell's words when it marked any.
func historyValuesAsRead(values map[string]pageHistoryRowValue, closer func() searchMentionCloser) (*string, bool) {
	var title *string
	marked := false
	for propertyID, value := range values {
		if !bytes.Contains(value.Spans, []byte(`"mention"`)) {
			continue
		}
		spans := []any{}
		if json.Unmarshal(value.Spans, &spans) != nil || !graphSpansAsRead(spans, closer) {
			continue
		}
		marked = true
		if encoded, err := json.Marshal(spans); err == nil {
			value.Spans = encoded
		}
		words := graphSpansWords(spans)
		if len(value.Value) > 0 && value.Value[0] == '"' {
			if encoded, err := json.Marshal(words); err == nil {
				value.Value = encoded
			}
		}
		if value.Type == graphValueTypeTitle {
			title = &words
		}
		values[propertyID] = value
	}
	return title, marked
}

// backlinksAsRead puts the blocks and pages that link to a page as their
// reader reads them.
func backlinksAsRead(app core.App, readerID string, backlinks *pageBacklinksResponse) {
	closer := readerCloser(app, readerID)
	for index, block := range backlinks.Blocks {
		backlinks.Blocks[index].Content, _, _ = rawBlockContentAsRead(block.Type, block.Content, closer)
	}
	listed := make([]*pageResponse, 0, len(backlinks.Pages))
	for index, page := range backlinks.Pages {
		backlinks.Pages[index].Title = rowTitleAsRead(app, readerID, page.ID, page.Title)
		listed = append(listed, &backlinks.Pages[index])
	}
	// Their spans say their mentions where they are named (closedMentions.ts).
	pagesTitleSpansAsReadBy(app, closer, listed)
}

// historyChangesAsRead puts a page's history of changes as its reader reads
// it. A cell's change keeps its words before and after, not its spans: the
// words of a mention the reader can't open that its cell holds now are
// read in marks wherever they stand in them.
func historyChangesAsRead(app core.App, readerID string, changes []pageHistoryChangeWire) {
	closer := readerCloser(app, readerID)
	for index, change := range changes {
		if change.ItemID == "" || len(change.RowValues) == 0 {
			continue
		}
		// One that kept its spans says its mentions by them, as its reader reads them.
		for valueIndex, value := range change.RowValues {
			if len(value.FromSpans) > 0 {
				read, _ := typedSpansAsRead(value.FromSpans, closer)
				changes[index].RowValues[valueIndex].FromSpans, changes[index].RowValues[valueIndex].From = read, spelledName(read)
			}
			if len(value.ToSpans) > 0 {
				read, _ := typedSpansAsRead(value.ToSpans, closer)
				changes[index].RowValues[valueIndex].ToSpans, changes[index].RowValues[valueIndex].To = read, spelledName(read)
			}
		}
		stored := []storedGraphRowValue{}
		if app.DB().NewQuery(`SELECT [[row_id]], [[property_id]], [[spans]] FROM {{leafen_database_row_values}}
			WHERE [[row_id]] = {:rowId} AND [[spans]] LIKE '%"mention"%'`).
			Bind(dbx.Params{"rowId": change.ItemID}).All(&stored) != nil {
			continue
		}
		for _, cell := range stored {
			for _, span := range databaseStoredSpans(cell.Spans) {
				reach := searchMentionReach(closer(), span.Mention, span.PageID, span.DatabaseID)
				if span.Text == "" || reach == mentionOpens {
					continue
				}
				marks := unnamedMentionWords(reach, span.Text, true)
				for valueIndex, value := range changes[index].RowValues {
					if value.PropertyID != cell.PropertyID || len(value.FromSpans) > 0 || len(value.ToSpans) > 0 {
						continue
					}
					changes[index].RowValues[valueIndex].From = strings.ReplaceAll(value.From, span.Text, marks)
					changes[index].RowValues[valueIndex].To = strings.ReplaceAll(value.To, span.Text, marks)
				}
			}
		}
	}
}
