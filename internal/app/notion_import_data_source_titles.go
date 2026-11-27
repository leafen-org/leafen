package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A sub-page's title loses a data source's mention in Notion's API as a line
// does (notion_import_data_source_mentions.go); its parent's Markdown keeps it
// in the sub-page's <page> tag. A move reading the sub-page puts it back in
// the title it stages, its words and its formatting, when its title may have
// lost one and the parent page's Markdown, which reading the parent's lines
// most likely read already, names it. A page the move starts from, and a
// database's row, have no parent's Markdown to tell.

// recordNotionImportPagePresentationText keeps a sub-page's presentation
// (notionImportPageSourceText), its title with the data source mentions its
// parent's Markdown names in it.
func recordNotionImportPagePresentationText(
	ctx context.Context, app core.App, client notionProviderClient, jobID, sourceID string, page notionProviderObject,
) error {
	presentation := notionImportObjectPresentation(page, true)
	presentation.History = notionImportObjectHistory(page)
	if err := notionImportTitleSources(ctx, app, client, jobID, sourceID, page, &presentation); err != nil {
		return err
	}
	return recordNotionImportPresentation(app, jobID, sourceID, "page", encodeNotionImportPresentation(presentation))
}

// notionImportTitleSources puts in a sub-page's title the data source
// mentions its parent's Markdown names in its <page> tag, and stages the
// title it then spells.
func notionImportTitleSources(
	ctx context.Context, app core.App, client notionProviderClient, jobID, sourceID string,
	page notionProviderObject, presentation *notionImportPresentation,
) error {
	if page.Parent.Type == "database_id" || page.Parent.Type == "data_source_id" {
		return nil
	}
	parts := []notionProviderRichText(nil)
	for _, property := range page.Properties {
		if found, err := notionProviderRichTextParts(property.Title); err == nil && len(found) > 0 {
			parts = found
			break
		}
	}
	if !notionProviderTextMayLoseSource(parts) {
		return nil
	}
	stored := storedNotionSource{}
	err := app.DB().NewQuery(`
		SELECT [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[title]]
		FROM {{leafen_notion_import_sources}} WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[kind]] = 'page'
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": sourceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) || err == nil && stored.ParentSourceID == "" {
		return nil
	}
	if err != nil {
		return err
	}
	parentPage, err := notionImportBlockPage(app, jobID, stored.ParentSourceID, stored.ParentKind)
	if err != nil || parentPage == "" {
		return err
	}
	reading, _, err := notionImportPageMarkdown(ctx, client, jobID, parentPage)
	if err != nil || reading.Mentions == nil {
		return err
	}
	tag := notionMarkdownKey(sourceID)
	for _, line := range reading.Mentions.Lines {
		if line.Page != tag || len(line.Sources) == 0 {
			continue
		}
		spans := presentation.TitleSpans
		if len(spans) == 0 {
			spans = []graphTextSpan{{Text: stored.Title}}
		}
		if notionImportSpansKey(spans) != line.Key || spelledName(spans) != stored.Title {
			return nil
		}
		for _, placement := range line.Sources {
			spans = notionImportSpansWithSource(spans, placement)
		}
		title := spelledName(spans)
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_notion_import_sources}} SET [[title]] = {:title}
			WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[kind]] = 'page'
		`).Bind(dbx.Params{"title": title, "jobId": jobID, "sourceId": sourceID}).Execute(); err != nil {
			return err
		}
		presentation.TitleSpans = spans
		return nil
	}
	return nil
}

// notionImportSpansKey is a title's words without its mentions.
func notionImportSpansKey(spans []graphTextSpan) string {
	key, _ := notionImportMarkdownLineSources(spans)
	return key
}

// notionImportSpansWithSource puts a data source's pending mention among a
// title's pieces where its placement says, as a line's
// (notionImportPlanWithSource).
func notionImportSpansWithSource(spans []graphTextSpan, placement notionImportSourcePlacement) []graphTextSpan {
	texts, mentions := make([]string, len(spans)), make([]bool, len(spans))
	for index, span := range spans {
		texts[index], mentions[index] = span.Text, span.Mention != ""
	}
	index, cut, found := notionImportMentionSlot(texts, mentions, placement.At, placement.After)
	if !found {
		return spans
	}
	spans = append([]graphTextSpan(nil), spans...)
	if cut > 0 {
		before, after := spans[index], spans[index]
		before.Text, after.Text = notionImportCutWords(spans[index].Text, cut)
		spans = append(spans[:index], append([]graphTextSpan{before, after}, spans[index+1:]...)...)
		index++
	}
	mention := graphTextSpan{
		Text: notionUnresolvedMentionTitle, Mention: "database", DatabaseID: notionImportPendingSource(placement.ID),
		Bold: placement.Span.Bold, Italic: placement.Span.Italic, Strikethrough: placement.Span.Strikethrough,
		Underline: placement.Span.Underline, Code: placement.Span.Code,
	}
	// Words formatted alike on both sides lend it their formatting.
	if index > 0 && index < len(spans) && spans[index-1].Mention == "" && spans[index].Mention == "" {
		left, right := spans[index-1], spans[index]
		mention.Bold = mention.Bold || left.Bold && right.Bold
		mention.Italic = mention.Italic || left.Italic && right.Italic
		mention.Strikethrough = mention.Strikethrough || left.Strikethrough && right.Strikethrough
		mention.Underline = mention.Underline || left.Underline && right.Underline
		mention.Code = mention.Code || left.Code && right.Code
		if left.Color == right.Color {
			mention.Color = left.Color
		}
		if left.Background == right.Background {
			mention.Background = left.Background
		}
	}
	return append(spans[:index], append([]graphTextSpan{mention}, spans[index:]...)...)
}

// spellNotionImportTitle gives a published page the title its data source
// mentions named spell, and the source it came from the same, which a run
// again knows its title by (recordNotionMigrationLedger).
func spellNotionImportTitle(resolver *notionImportReferenceResolver, page *core.Record, title string) error {
	page.Set("title", title)
	_, err := resolver.app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_sources}} SET [[title]] = {:title}
		WHERE [[job_id]] = {:jobId} AND [[kind]] = 'page' AND [[source_id]] IN (
			SELECT [[source_id]] FROM {{leafen_notion_import_mappings}}
			WHERE [[job_id]] = {:jobId} AND [[kind]] = 'page' AND [[native_id]] = {:pageId})
	`).Bind(dbx.Params{"title": title, "jobId": resolver.jobID, "pageId": page.Id}).Execute()
	return err
}
