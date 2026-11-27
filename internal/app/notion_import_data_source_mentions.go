package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf16"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's block API leaves a data source's mention out of rich text: the
// words around it come as two pieces, one at a line's end or alone as none
// (seen 2026-11-22, API versions 2022-06-28, 2025-09-03 and 2026-03-11, a
// page's body, its title, a row's text property, a comment), while the page's
// "v2" Markdown keeps it as <mention url="collection://ID" /> in the page's
// lines and in each sub-page's <page> tag. A move reads that Markdown
// (notionImportPageMarkdown) where a listing's text may have lost one — a
// line or a sub-page's title that is empty, starts or ends with a space, or
// has two pieces formatted alike side by side, which Notion's own text never
// has — and puts each mention back where the line's words with the same
// formatting stand around it: a line of the same words among the lines its
// parent holds, the mention between the same words. It is staged as the
// mention of the Notion data source by its ID until the move is published,
// then is the mention of the data source a move made of it, by its address
// and its name (data_source_mentions.go), or of one not there, "Untitled"
// (keepUnresolvedDataSourceMention). A mention Notion's Markdown places
// between words formatted alike takes their formatting, as Notion's export
// draws it (seen 2026-11-22: **x [source](…) y** in Markdown, <strong>
// around its link in HTML); the Markdown writes it unformatted. Notion's
// API gives no Markdown of a row's title or properties or of a comment, so
// theirs stay as the words around them.

// notionImportMarkdownMentions are the lines of a page's Markdown, for
// placing the data source mentions they hold.
type notionImportMarkdownMentions struct {
	Lines []notionImportMarkdownLine
}

// notionImportMarkdownLine is one line of text, or a sub-page's <page> tag:
// the line it sits under (-1 for the page), its words without its mentions,
// and its data source mentions.
type notionImportMarkdownLine struct {
	Parent  int
	Text    bool
	Page    string
	Key     string
	Sources []notionImportSourcePlacement
}

// notionImportSourcePlacement is a data source mention among a line's words:
// at a UTF-16 offset of its words without mentions, after as many mentions
// standing there, with the formatting it is written with.
type notionImportSourcePlacement struct {
	At, After int
	ID        string
	Span      graphTextSpan
}

// notionImportTextLineTypes are the blocks whose words Notion's block API
// gives as rich text, which a mention may have left.
var notionImportTextLineTypes = map[string]bool{
	graphBlockTypeParagraph: true, graphBlockTypeHeading1: true, graphBlockTypeHeading2: true,
	graphBlockTypeHeading3: true, graphBlockTypeHeading4: true, graphBlockTypeToggleHeading1: true,
	graphBlockTypeToggleHeading2: true, graphBlockTypeToggleHeading3: true, graphBlockTypeToggleHeading4: true,
	graphBlockTypeBulletedListItem: true, graphBlockTypeNumberedListItem: true, graphBlockTypeToDo: true,
	graphBlockTypeToggle: true, graphBlockTypeQuote: true, graphBlockTypeCallout: true,
}

// readNotionImportMarkdownMentions reads a page's "v2" Markdown into its
// lines, as a "v2" write reads it (notionMarkdownVersion2Input); nil when it
// reads none or names no data source.
func readNotionImportMarkdownMentions(markdown string) *notionImportMarkdownMentions {
	read, _, err := notionMarkdownVersion2Input(markdown)
	if err != nil {
		return nil
	}
	items, err := parseNotionMarkdown(read, false, nil)
	if err != nil {
		return nil
	}
	index, mentions, named := map[string]int{}, &notionImportMarkdownMentions{}, false
	for position, item := range items {
		index[item.ID] = position
		line := notionImportMarkdownLine{Parent: -1, Text: notionImportTextLineTypes[item.Type]}
		if parent, found := index[item.ParentID]; found && item.ParentID != "" {
			line.Parent = parent
		}
		spans := item.Spans
		if item.Type == notionMarkdownReferenceBlock {
			tag := notionMarkdownTag{}
			if json.Unmarshal(item.Content, &tag) == nil && tag.Tag == "page" && tag.URL != "" {
				line.Page, spans = notionMarkdownKey(tag.URL), tag.Spans
				if len(spans) == 0 {
					spans = []graphTextSpan{{Text: tag.Title}}
				}
			}
		} else if len(spans) == 0 && item.Text != "" {
			spans = []graphTextSpan{{Text: item.Text}}
		}
		line.Key, line.Sources = notionImportMarkdownLineSources(spans)
		named = named || len(line.Sources) > 0
		mentions.Lines = append(mentions.Lines, line)
	}
	if !named {
		return nil
	}
	return mentions
}

// notionImportMarkdownLineSources is a line's words without its mentions and
// its data source mentions among them.
func notionImportMarkdownLineSources(spans []graphTextSpan) (string, []notionImportSourcePlacement) {
	var key strings.Builder
	offset, standing, last := 0, 0, -1
	sources := []notionImportSourcePlacement{}
	for _, span := range spans {
		if span.Mention == "" {
			key.WriteString(span.Text)
			offset += utf16TextLength(span.Text)
			continue
		}
		if last != offset {
			standing, last = 0, offset
		}
		if id, named := strings.CutPrefix(span.DatabaseID, notionMarkdownDataSourceAddress); span.Mention == "database" && named {
			if dashed, isUUID := normalizeNotionUUIDRequest(strings.TrimSpace(id)); isUUID {
				sources = append(sources, notionImportSourcePlacement{At: offset, After: standing, ID: dashed, Span: span})
			}
		}
		standing++
	}
	return key.String(), sources
}

// notionProviderTextMayLoseSource tells whether rich text may have lost a
// data source's mention: none at all, words beginning or ending with a space,
// or two pieces of words formatted alike side by side.
func notionProviderTextMayLoseSource(parts []notionProviderRichText) bool {
	if len(parts) == 0 {
		return true
	}
	first, last := parts[0], parts[len(parts)-1]
	if first.Type == "text" && strings.HasPrefix(first.PlainText, " ") ||
		last.Type == "text" && strings.HasSuffix(last.PlainText, " ") {
		return true
	}
	for index := 1; index < len(parts); index++ {
		before, after := parts[index-1], parts[index]
		if before.Type == "text" && after.Type == "text" && before.Annotations == after.Annotations &&
			before.Href == after.Href && (before.Text.Link == nil) == (after.Text.Link == nil) {
			return true
		}
	}
	return false
}

// notionProviderBlocksMayLoseSource tells whether a listing's lines or its
// sub-pages' titles may have lost a data source's mention, for the move to
// read their page's Markdown: the run (notionImportDataSourceMentionReads)
// and the scan reckoning its reads alike.
func notionProviderBlocksMayLoseSource(blocks []notionProviderObject) bool {
	for _, block := range blocks {
		if block.Type == "child_page" {
			title := block.ChildPage.Title
			if title == "" || strings.Contains(title, "  ") || strings.HasPrefix(title, " ") || strings.HasSuffix(title, " ") {
				return true
			}
			continue
		}
		if !notionImportTextLineTypes[block.Type] {
			continue
		}
		fields := struct {
			RichText []notionProviderRichText `json:"rich_text"`
		}{}
		if len(block.Payload) > 0 && string(block.Payload) != "null" && json.Unmarshal(block.Payload, &fields) != nil {
			continue
		}
		if block.Type == graphBlockTypeParagraph && len(fields.RichText) == 0 {
			fields.RichText = block.Paragraph.RichText
		}
		if notionProviderTextMayLoseSource(fields.RichText) {
			return true
		}
	}
	return false
}

// notionImportDataSourceMentionReads puts back the data source mentions a
// listing's staged lines lost, from their page's Markdown: read already, or
// read now when its blocks say they may have lost one
// (notionProviderBlocksMayLoseSource). A template's lines are read as Notion
// names them, as its headings are.
func notionImportDataSourceMentionReads(
	ctx context.Context, app core.App, client notionProviderClient, jobID, parentSourceID, parentKind string,
	blueprint bool, blocks []notionProviderObject, sources []stagedNotionSource,
) error {
	if blueprint {
		return nil
	}
	pageID, err := notionImportBlockPage(app, jobID, parentSourceID, parentKind)
	if err != nil || pageID == "" {
		return err
	}
	// A page read already tells its every listing, at no cost.
	reading, known := notionImportPageReadings.get(jobID + "\x00" + pageID)
	if !known && notionProviderBlocksMayLoseSource(blocks) {
		reading, _, err = notionImportPageMarkdown(ctx, client, jobID, pageID)
	}
	if err != nil || reading.Mentions == nil {
		return err
	}
	parents := map[int]bool{-1: true}
	if parentKind != "page" && parentKind != "row" {
		parentKey, err := notionImportStagedLineKey(app, jobID, parentSourceID)
		if err != nil {
			return err
		}
		parents = map[int]bool{}
		for index, line := range reading.Mentions.Lines {
			if line.Key == parentKey && line.Page == "" {
				parents[index] = true
			}
		}
	}
	// The listing's lines, and those of the Markdown's lines, by their words.
	staged, plans := map[string][]int{}, map[int]*notionParagraphSourcePlan{}
	for index := range sources {
		if sources[index].Kind != graphBlockTypeParagraph && sources[index].Kind != "block" {
			continue
		}
		plan, err := decodeNotionParagraphSourceText(sources[index].Text)
		if err != nil || plan.BlockType != "" && !notionImportTextLineTypes[plan.BlockType] {
			continue
		}
		plans[index] = &plan
		key := notionImportPlanKey(plan.Spans)
		staged[key] = append(staged[key], index)
	}
	for key, indexes := range staged {
		placements := reading.Mentions.placements(key, parents, len(indexes))
		for order, index := range indexes {
			if placements == nil || len(placements[order]) == 0 {
				continue
			}
			plan := plans[index]
			for _, placement := range placements[order] {
				plan.Spans = notionImportPlanWithSource(plan.Spans, placement)
			}
			plan.Text = notionImportPlanWords(plan.Spans)
			if sources[index].Text, err = encodeNotionParagraphSourcePlan(*plan); err != nil {
				return err
			}
		}
	}
	return nil
}

// placements are the data source mentions of the lines with words key, for
// count lines of a listing with those words: the same for each when every
// such line of the page has the same, else those of the lines under parents
// in order, when there are as many of them; nil when none tells.
func (mentions *notionImportMarkdownMentions) placements(
	key string, parents map[int]bool, count int,
) [][]notionImportSourcePlacement {
	every, under := [][]notionImportSourcePlacement{}, [][]notionImportSourcePlacement{}
	for _, line := range mentions.Lines {
		if !line.Text || line.Key != key {
			continue
		}
		every = append(every, line.Sources)
		if parents[line.Parent] {
			under = append(under, line.Sources)
		}
	}
	same := func(lines [][]notionImportSourcePlacement) bool {
		for _, sources := range lines[1:] {
			if !notionImportSamePlacements(sources, lines[0]) {
				return false
			}
		}
		return true
	}
	repeated := func(sources []notionImportSourcePlacement) [][]notionImportSourcePlacement {
		each := make([][]notionImportSourcePlacement, count)
		for index := range each {
			each[index] = sources
		}
		return each
	}
	switch {
	case len(every) > 0 && same(every):
		return repeated(every[0])
	case len(under) > 0 && same(under):
		return repeated(under[0])
	case len(under) == count:
		return under
	}
	return nil
}

func notionImportSamePlacements(left, right []notionImportSourcePlacement) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].At != right[index].At || left[index].After != right[index].After || left[index].ID != right[index].ID {
			return false
		}
	}
	return true
}

// notionImportStagedLineKey is the words without mentions of a staged block,
// "" for one without words.
func notionImportStagedLineKey(app core.App, jobID, sourceID string) (string, error) {
	text := ""
	err := app.DB().NewQuery(`
		SELECT [[text]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[kind]] IN ('block', 'paragraph')
		LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": sourceID}).Row(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	plan, err := decodeNotionParagraphSourceText(text)
	if err != nil {
		return "", nil
	}
	return notionImportPlanKey(plan.Spans), nil
}

// notionImportPlanKey is a staged line's words without its mentions.
func notionImportPlanKey(spans []notionParagraphSpan) string {
	var key strings.Builder
	for _, span := range spans {
		if span.Attributes["mention"] == nil {
			key.WriteString(span.Text)
		}
	}
	return key.String()
}

func notionImportPlanWords(spans []notionParagraphSpan) string {
	var words strings.Builder
	for _, span := range spans {
		words.WriteString(span.Text)
	}
	return words.String()
}

// notionImportPendingSource is the pending mention of a Notion data source,
// which the move's publication names (notionImportReferenceResolver.dataSource).
func notionImportPendingSource(id string) string {
	return notionImportPendingDatabasePrefix + notionMarkdownDataSourceAddress + id
}

// notionImportSourceMarks are the formatting a mention keeps.
var notionImportSourceMarks = []string{"bold", "italic", "strikethrough", "underline", "code", "color", "background"}

// notionImportPlanWithSource puts a data source's pending mention among a
// staged line's pieces where its placement says.
func notionImportPlanWithSource(spans []notionParagraphSpan, placement notionImportSourcePlacement) []notionParagraphSpan {
	texts, mentions := make([]string, len(spans)), make([]bool, len(spans))
	for index, span := range spans {
		texts[index], mentions[index] = span.Text, span.Attributes["mention"] != nil
	}
	index, cut, found := notionImportMentionSlot(texts, mentions, placement.At, placement.After)
	if !found {
		return spans
	}
	spans = append([]notionParagraphSpan(nil), spans...)
	if cut > 0 {
		left, right := notionImportCutWords(spans[index].Text, cut)
		before, after := spans[index], spans[index]
		before.Text, after.Text = left, right
		before.Attributes, after.Attributes = cloneNotionSpanAttributes(spans[index].Attributes), cloneNotionSpanAttributes(spans[index].Attributes)
		spans = append(spans[:index], append([]notionParagraphSpan{before, after}, spans[index+1:]...)...)
		index++
	}
	attributes := notionImportSourceMarksOf(placement.Span)
	attributes["mention"], attributes["databaseId"] = "database", notionImportPendingSource(placement.ID)
	// Words formatted alike on both sides lend it their formatting.
	left, right := notionImportNeighbourWords(spans, index)
	if left != nil && right != nil {
		for _, mark := range notionImportSourceMarks {
			if value, set := left.Attributes[mark]; set && right.Attributes[mark] == value {
				attributes[mark] = value
			}
		}
	}
	mention := notionParagraphSpan{Text: notionUnresolvedMentionTitle, Attributes: attributes}
	return append(spans[:index], append([]notionParagraphSpan{mention}, spans[index:]...)...)
}

// notionImportNeighbourWords are the pieces of words just before and after
// a place among a line's pieces, nil where there are none.
func notionImportNeighbourWords(spans []notionParagraphSpan, index int) (*notionParagraphSpan, *notionParagraphSpan) {
	var left, right *notionParagraphSpan
	if index > 0 && spans[index-1].Attributes["mention"] == nil && spans[index-1].Text != "" {
		left = &spans[index-1]
	}
	if index < len(spans) && spans[index].Attributes["mention"] == nil && spans[index].Text != "" {
		right = &spans[index]
	}
	return left, right
}

func cloneNotionSpanAttributes(attributes map[string]any) map[string]any {
	if attributes == nil {
		return nil
	}
	cloned := make(map[string]any, len(attributes))
	for key, value := range attributes {
		cloned[key] = value
	}
	return cloned
}

// notionImportMentionSlot finds where a mention placed at a UTF-16 offset of
// a line's words, after as many mentions there, goes among its pieces: before
// piece index, or cut pieces of words into it (cut > 0). Fewer mentions there
// than it follows puts it after them.
func notionImportMentionSlot(texts []string, mentions []bool, at, after int) (int, int, bool) {
	offset, seen := 0, 0
	for index := 0; index <= len(texts); index++ {
		if offset == at {
			if seen == after || index == len(texts) || !mentions[index] {
				return index, 0, true
			}
			seen++
			continue
		}
		if index == len(texts) {
			break
		}
		if mentions[index] {
			continue
		}
		length := utf16TextLength(texts[index])
		if offset < at && at < offset+length {
			return index, at - offset, true
		}
		offset += length
	}
	return 0, 0, false
}

// notionImportCutWords cuts words at a UTF-16 offset.
func notionImportCutWords(text string, cut int) (string, string) {
	units := utf16.Encode([]rune(text))
	if cut <= 0 || cut >= len(units) {
		return text, ""
	}
	return string(utf16.Decode(units[:cut])), string(utf16.Decode(units[cut:]))
}

// notionImportSourceMarksOf are the marks a mention is written with in
// Markdown, as a staged line's piece keeps them; its colors are left to the
// words around it, which name them as Notion's API does.
func notionImportSourceMarksOf(span graphTextSpan) map[string]any {
	attributes := map[string]any{}
	for key, set := range map[string]bool{
		"bold": span.Bold, "italic": span.Italic, "strikethrough": span.Strikethrough,
		"underline": span.Underline, "code": span.Code,
	} {
		if set {
			attributes[key] = true
		}
	}
	return attributes
}
