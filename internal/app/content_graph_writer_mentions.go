package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a writer sends back of what they read: a page's graph reaches its
// reader with the words of a mention they can't open in marks and a closed
// page link without its title (content_graph_reader_mentions.go). A change
// they write keeps what the page holds for everyone else: a mention written
// with marks for words takes its target's name again ("Untitled" for one
// nothing answers to), the marks aren't stored, and a page link takes back
// the title and icon its block keeps. A database's name and description, a
// linked one's own name, and the name a page-named database's block keeps
// are put back as a block's text is.

// restoreGraphWriteAsStored puts back, in a change written through the app,
// what its writer's reading left out. A page title written as a database's
// name read with marks — the name its block or its source spells — takes
// that name's words again.
func restoreGraphWriteAsStored(app core.App, input *graphMutationInput) {
	names := map[string]string{}
	respelled := map[string]string{}
	for index := range input.TextUpdates {
		restoreClosedMentionSpans(app, names, input.TextUpdates[index].Spans)
	}
	for _, operation := range input.Operations {
		if operation.Row != nil {
			for propertyID, value := range operation.Row.Values {
				if !restoreClosedMentionSpans(app, names, value.Spans) {
					continue
				}
				var words strings.Builder
				for _, span := range value.Spans {
					words.WriteString(span.Text)
				}
				if encoded, err := json.Marshal(words.String()); err == nil && len(value.Value) > 0 && value.Value[0] == '"' {
					value.Value = encoded
				}
				operation.Row.Values[propertyID] = value
			}
		}
		if operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil {
			operation.Block.Content = restoreBlockContentAsStored(app, names, respelled, operation.Block.ID, operation.Block.Content)
		}
		if source := operation.DataSource; source != nil {
			if !source.TitleOmitted {
				source.Title, source.TitleSpans = restoreNameAsStored(app, names, respelled, source.Title, source.TitleSpans)
			}
			if source.Description != nil {
				description := ""
				description, source.DescriptionSpans = restoreNameAsStored(app, names, nil, *source.Description, source.DescriptionSpans)
				source.Description = &description
			}
		}
	}
	if input.PageTitle != nil {
		if title, marked := respelled[*input.PageTitle]; marked {
			input.PageTitle = &title
		} else if holdsMentionMarks(*input.PageTitle) {
			title := withoutMentionMarks(*input.PageTitle)
			input.PageTitle = &title
		}
	}
}

// restoreNameAsStored puts back the mentions of a database's name or
// description written with marks for words, and gives the words, while the
// spans spelled them, again from the spans; a name's are kept in respelled,
// by the words it was written with. Other words with marks — a rename's,
// whose formatting goes — keep each mention whose marks they still hold, in
// its order, and none of the others' marks (renamedWithMentions).
func restoreNameAsStored(app core.App, names, respelled map[string]string, words string, spans []graphTextSpan) (string, []graphTextSpan) {
	spelled := spelledName(spans)
	marked := append([]graphTextSpan(nil), spans...)
	restored := restoreClosedMentionSpans(app, names, spans)
	if words != spelled {
		if restored || holdsMentionMarks(words) {
			return renamedWithMentions(words, marked, spans)
		}
		return words, spans
	}
	if restored && respelled != nil {
		respelled[spelled] = spelledName(spans)
	}
	return spelledName(spans), spans
}

// renamedWithMentions is a renamed name's words and spans: each run of marks
// in them that a mention of the name read before (marked, the same mention
// as named again in restored) stands for, in order, is that mention again;
// a run none stands for goes; the other words are plain.
func renamedWithMentions(words string, marked, restored []graphTextSpan) (string, []graphTextSpan) {
	spans := []graphTextSpan{}
	next := 0
	plain := func(text string) {
		if text != "" {
			spans = append(spans, graphTextSpan{Text: text})
		}
	}
	runes := []rune(words)
	for start := 0; start < len(runes); {
		end := start
		isMark := runes[start] == closedMentionMark || runes[start] == missingMentionMark
		for end < len(runes) && (runes[end] == closedMentionMark || runes[end] == missingMentionMark) == isMark {
			end++
		}
		segment := string(runes[start:end])
		start = end
		if !isMark {
			plain(segment)
			continue
		}
		for next < len(marked) && segment != "" {
			index := next
			next++
			if searchMentionHasNoWords(marked[index].Mention) && strings.HasPrefix(segment, marked[index].Text) && marked[index].Text != "" {
				spans = append(spans, restored[index])
				segment = strings.TrimPrefix(segment, marked[index].Text)
			}
		}
	}
	return spelledName(spans), spans
}

// restoreClosedMentionSpans gives each mention written with marks for words
// its target's name, and reports whether it gave any.
func restoreClosedMentionSpans(app core.App, names map[string]string, spans []graphTextSpan) bool {
	restored := false
	for index, span := range spans {
		if !searchMentionHasNoWords(span.Mention) || !closedMentionText(span.Text) {
			continue
		}
		spans[index].Text = closedMentionTargetName(app, names, span.Mention, span.PageID, span.BlockID, span.DatabaseID)
		restored = true
	}
	return restored
}

// restoreBlockContentAsStored is a block's content as written, less the
// marks its writer's reading carried and with a page link's title and icon
// as its block keeps them.
func restoreBlockContentAsStored(app core.App, names, respelled map[string]string, blockID string, raw json.RawMessage) json.RawMessage {
	text := string(raw)
	if !strings.Contains(text, string(closedMentionMark)) && !strings.Contains(text, string(missingMentionMark)) &&
		!strings.Contains(text, `"`+publicPageLinkUnavailableKey+`"`) &&
		!strings.Contains(text, `"`+publicMentionUnavailableKey+`"`) {
		return raw
	}
	content := map[string]any{}
	if json.Unmarshal(raw, &content) != nil {
		return raw
	}
	if _, closed := content[publicPageLinkUnavailableKey]; closed {
		delete(content, publicPageLinkUnavailableKey)
		stored := ""
		_ = app.DB().NewQuery(`SELECT [[content]] FROM {{leafen_blocks}} WHERE [[id]] = {:id} LIMIT 1`).
			Bind(dbx.Params{"id": blockID}).Row(&stored)
		kept := map[string]any{}
		_ = json.Unmarshal([]byte(stored), &kept)
		for _, key := range graphReaderPageLinkKeys {
			if value, ok := kept[key]; ok {
				content[key] = value
			}
		}
	}
	restore := func(spans []any) {
		for _, entry := range spans {
			span, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			delete(span, publicMentionUnavailableKey)
			mention := stringField(span, "mention")
			if searchMentionHasNoWords(mention) && closedMentionText(stringField(span, "text")) {
				span["text"] = closedMentionTargetName(app, names, mention, stringField(span, "pageId"),
					stringField(span, "blockId"), stringField(span, "databaseId"))
			}
		}
	}
	for _, field := range graphReaderSpanFields {
		if spans, ok := content[field].([]any); ok {
			restore(spans)
			if _, plain := content["text"].(string); plain && field != "caption" {
				content["text"] = graphSpansWords(spans)
			}
		}
	}
	if cells, ok := content["cells"].([]any); ok {
		for _, cell := range cells {
			if spans, ok := cell.([]any); ok {
				restore(spans)
			}
		}
	}
	// A database's name on its block, a linked one's own words with it.
	if spans, ok := content["linkedTitleSpans"].([]any); ok {
		spelled := graphSpansWords(spans)
		restore(spans)
		if title, plain := content["linkedTitle"].(string); plain && title == spelled {
			content["linkedTitle"] = graphSpansWords(spans)
		} else if plain {
			content["linkedTitle"] = withoutMentionMarks(title)
		}
	}
	if spans, ok := content["pageTitleSpans"].([]any); ok {
		spelled := graphSpansWords(spans)
		restore(spans)
		respelled[spelled] = graphSpansWords(spans)
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return raw
	}
	return encoded
}

// closedMentionText reports words that are only a mention's marks of one
// kind: no-access or not-found.
func closedMentionText(text string) bool {
	return text != "" && (strings.Trim(text, string(closedMentionMark)) == "" ||
		strings.Trim(text, string(missingMentionMark)) == "")
}

// closedMentionTargetName is a mention's target's name now: a page's title,
// or a database's, read from the page holding it; a block's words for a
// mention of a block on a page (page_mention_block_anchor.go).
func closedMentionTargetName(app core.App, names map[string]string, mention, pageID, blockID, databaseID string) string {
	key := mention + ":" + pageID + "#" + blockID + databaseID
	if name, known := names[key]; known {
		return name
	}
	if mention == "page" && blockID != "" {
		name := ""
		if block, there := pageMentionAnchorBlock(app, pageID, blockID); there {
			name = pageMentionBlockWords(app, block, pageMentionBlockSayer(app, "", block.WorkspaceID))
		}
		if name == "" {
			name = notionUnresolvedMentionTitle
		}
		names[key] = name
		return name
	}
	target := pageID
	if mention == "database" {
		// A data source's mention is named by the source's name (data_source_mentions.go).
		if dataSourceID, _, _, found, err := mentionedDataSource(app, databaseID); err == nil && found {
			name, _ := mentionedDataSourceName(app, dataSourceID)
			names[key] = name
			return name
		}
		target = mcpDatabaseHolderPageID(app, databaseID)
	}
	name := ""
	_ = app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_pages}} WHERE [[id]] = {:id} LIMIT 1`).
		Bind(dbx.Params{"id": target}).Row(&name)
	if name == "" {
		name = notionUnresolvedMentionTitle
	}
	names[key] = name
	return name
}
