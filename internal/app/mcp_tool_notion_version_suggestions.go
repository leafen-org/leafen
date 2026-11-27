package app

import "encoding/json"

// The version tools read a page's version with the suggested edits not yet
// decided written into it, as Notion's get-version-changes and view-version
// read a page holding suggestions made on its screen (seen 2026-11-22
// through Notion's MCP, worker w2316): a suggested block is there as any
// block is, words suggested in and words suggested away are both in the
// text, a replacement's new words before the words it replaces as its text
// holds them ("## 구역 둘 — 새로운합성 기록", "제안 앞말 새말바꿀말
// 뒷말."; page_suggestion_text.go), and nothing marks them
// as suggested. Fetch reads the page as it stands, without them
// (notion_page_markdown.go), as Notion's fetch does. So get-version-changes
// lists a suggestion as an edit once it is made, and its decision as another.

// mcpHistoryWithSuggestions writes a version's suggestions into its blocks'
// text.
func mcpHistoryWithSuggestions(version *pageHistorySnapshotResponse) {
	for index := range version.Blocks {
		block := &version.Blocks[index]
		block.Content, block.PlainText = mcpHistoryTextWithSuggestions(block.Content, block.PlainText)
	}
}

// mcpHistoryTextWithSuggestions is a version's block content and words with
// the suggestions marked in its text's spans (content.rich) written in,
// unmarked, formatting as it was before a suggestion to change it (Notion's
// form for that isn't seen); as it is for a block whose text has none.
func mcpHistoryTextWithSuggestions(content json.RawMessage, words string) (json.RawMessage, string) {
	object := map[string]json.RawMessage{}
	spans := []graphTextSpan{}
	if json.Unmarshal(content, &object) != nil || object["rich"] == nil || json.Unmarshal(object["rich"], &spans) != nil {
		return content, words
	}
	marked := false
	for _, span := range spans {
		marked = marked || span.Suggestion != "" || span.SuggestionID != "" || span.SuggestionBefore != ""
	}
	if !marked {
		return content, words
	}
	// Its spans joined where they are alike, as one text with no
	// suggestion splitting it.
	written, text := graphTextSpansAndText(notionMarkdownJoinedSpans(graphTextSpansAsTheyStand(spans, true)))
	rich, err := json.Marshal(written)
	if err != nil {
		return content, words
	}
	object["rich"] = rich
	if _, has := object["text"]; has {
		object["text"], _ = json.Marshal(text)
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return content, words
	}
	return encoded, text
}
