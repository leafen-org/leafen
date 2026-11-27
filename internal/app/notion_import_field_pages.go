package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page made from "@" in a page's title in Notion has that page as its
// parent, and one made in a database page's title or text cell that
// database page, with no block in its body (seen 2026-11-13 through
// Notion's API). The import finds it by the field's mention of it once the
// body was read, and brings it as a sub-page hanging on that field, as
// Leafen's are (mention_anchored_fields.go): on the page's "title", or on
// "cell:" and the imported property. A page its parent's body lists as a
// sub-page stays a sub-page with its block, though the title mentions it.

// notionImportCellAnchor marks a staged page found by a database page's
// cell (notionImportPresentation.Anchor).
const notionImportCellAnchor = "cell"

// notionImportFieldPageSources stages the pages hanging on the fields of a
// page or database page whose body was just read in full: those its title,
// or a database page's title and text cells, mention whose parent is the
// page, none its body listed.
func notionImportFieldPageSources(
	ctx context.Context, app core.App, client notionProviderClient, jobID string,
	discovery notionImportPageDiscovery, listed []stagedNotionSource,
) ([]stagedNotionSource, error) {
	if discovery.Blueprint || discovery.Kind != "page" && discovery.Kind != "row" {
		return nil, nil
	}
	var text string
	err := app.DB().NewQuery(`
		SELECT [[text]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[kind]] = {:kind}
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": discovery.SourceID, "kind": discovery.Kind}).Row(&text)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !strings.Contains(text, notionImportPendingPagePrefix) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	anchor, fields := pageAnchorTitle, notionImportTitleFieldSpans(text)
	if discovery.Kind == "row" {
		anchor, fields = notionImportCellAnchor, notionImportRowFieldSpans(text)
	}
	seen := map[string]bool{}
	for _, source := range listed {
		seen[notionScopeID(source.SourceID)] = true
	}
	sources := []stagedNotionSource{}
	for _, spans := range fields {
		for _, span := range spans {
			pageID, pending := strings.CutPrefix(span.PageID, notionImportPendingPagePrefix)
			if span.Mention != "page" || !pending || span.BlockID != "" || seen[notionScopeID(pageID)] {
				continue
			}
			seen[notionScopeID(pageID)] = true
			page, found, err := notionImportHangingPage(ctx, app, client, jobID, pageID,
				notionProviderParent{Type: "page_id", PageID: discovery.SourceID})
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			presentation := notionImportObjectPresentation(page, true)
			presentation.Anchor, presentation.History = anchor, notionImportObjectHistory(page)
			sources = append(sources, stagedNotionSource{
				SourceID: page.ID, Kind: "page", ParentSourceID: discovery.SourceID, ParentKind: discovery.Kind,
				Title: notionObjectTitle(page), Text: encodeNotionImportPresentation(presentation),
				DiscoveryStatus: notionImportPageDiscoveryPending,
			})
		}
	}
	return sources, nil
}

// notionImportFieldPagePosition is where an imported sub-page goes among its
// parent's: one hanging on a field where its mention stands among those
// hanging there (notionImportAnchoredPagePosition); the first on the title
// before those already made, as the title stands above the body
// (childPageBodyOrder); any other where its source stands. That is only its
// first place: once published it takes its place among all its parent's
// published sub-pages where its block or line stands
// (alignNotionImportSubPages), those an earlier run of a migration made
// included, though they kept that run's numbers.
func notionImportFieldPagePosition(
	app core.App, jobID string, source storedNotionSource, mappings *notionApplyMappings, parentPageID, anchor string,
) (float64, error) {
	if anchor != "" {
		if position, placed, err := notionImportAnchoredPagePosition(app, jobID, source, mappings, parentPageID, anchor); err != nil || placed {
			return position, err
		}
	}
	if anchor != pageAnchorTitle {
		return float64(source.Position), nil
	}
	var first sql.NullFloat64
	if err := app.DB().NewQuery(`
		SELECT MIN([[position]]) FROM {{leafen_pages}} WHERE [[parent_id]] = {:parent} AND [[in_trash]] = FALSE
	`).Bind(dbx.Params{"parent": parentPageID}).Row(&first); err != nil {
		return 0, err
	}
	if !first.Valid {
		return float64(source.Position), nil
	}
	return first.Float64 - 1, nil
}

// notionImportTitleFieldSpans is a page source's title as its presentation
// keeps it.
func notionImportTitleFieldSpans(text string) [][]graphTextSpan {
	stored := notionImportPresentation{}
	if !strings.HasPrefix(text, notionPresentationSourcePrefix) ||
		json.Unmarshal([]byte(strings.TrimPrefix(text, notionPresentationSourcePrefix)), &stored) != nil {
		return nil
	}
	return [][]graphTextSpan{stored.TitleSpans}
}

// notionImportRowFieldSpans is a row source's title and text cells, in its
// properties' order.
func notionImportRowFieldSpans(text string) [][]graphTextSpan {
	values := map[string]notionDatabaseRowValuePlan{}
	if json.Unmarshal([]byte(text), &values) != nil {
		return nil
	}
	properties := make([]string, 0, len(values))
	for property, value := range values {
		if value.Type == graphValueTypeTitle || value.Type == graphValueTypeRichText {
			properties = append(properties, property)
		}
	}
	sort.Strings(properties)
	fields := make([][]graphTextSpan, 0, len(properties))
	for _, property := range properties {
		fields = append(fields, values[property].Spans)
	}
	return fields
}

// notionImportFieldAnchor is the field a staged sub-page found by its
// parent's mention hangs on once imported: "title" while the parent's title
// mentions it, or the cell of the database page that does; "" for any other
// page, which takes a block in its parent's body. One found hanging on its
// parent unmentioned (notion_import_unmentioned_pages.go) hangs there so,
// on a page's title or a database page's title cell: Notion doesn't tell
// which field it was made from, and its sidebar lists none hanging there
// unmentioned.
func notionImportFieldAnchor(app core.App, source storedNotionSource, parent *core.Record) (string, bool, error) {
	stored := notionImportPresentation{}
	if !strings.HasPrefix(source.Text, notionPresentationSourcePrefix) ||
		json.Unmarshal([]byte(strings.TrimPrefix(source.Text, notionPresentationSourcePrefix)), &stored) != nil ||
		stored.Anchor == "" || stored.Anchor == notionImportBlockAnchor {
		return "", false, nil
	}
	if stored.Anchor == notionImportUnmentionedAnchor {
		anchor, err := notionImportUnmentionedFieldAnchor(app, source, parent)
		return anchor, anchor != "", err
	}
	pending := notionImportPendingPagePrefix + source.SourceID
	if stored.Anchor == pageAnchorTitle {
		if spansMentionPage(ordinaryPageTitleSpans(parent), pending) {
			return pageAnchorTitle, false, nil
		}
		return "", false, nil
	}
	cells := []struct {
		Property string `db:"property_id"`
		Spans    string `db:"spans"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[property_id]], [[spans]] FROM {{leafen_database_row_values}}
		WHERE [[row_id]] = {:row} AND [[type]] IN ('title', 'rich_text') AND instr([[spans]], {:pending}) > 0
		ORDER BY [[property_id]]
	`).Bind(dbx.Params{"row": parent.Id, "pending": pending}).All(&cells); err != nil {
		return "", false, err
	}
	for _, cell := range cells {
		if spansMentionPage(databaseStoredSpans(cell.Spans), pending) {
			return pageAnchorCellPrefix + cell.Property, false, nil
		}
	}
	return "", false, nil
}

// notionImportUnmentionedFieldAnchor is the field a page found hanging on
// its parent unmentioned hangs on: a page's title, or a database page's
// title cell; "" when the parent is neither.
func notionImportUnmentionedFieldAnchor(app core.App, source storedNotionSource, parent *core.Record) (string, error) {
	switch {
	case source.ParentKind == "page" && parent.GetString("data_source_id") == "":
		return pageAnchorTitle, nil
	case source.ParentKind != "row" && source.ParentKind != "page":
		return "", nil
	}
	properties := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:dataSource} AND [[type]] = 'title' ORDER BY [[id]] LIMIT 1
	`).Bind(dbx.Params{"dataSource": parent.GetString("data_source_id")}).Column(&properties); err != nil || len(properties) == 0 {
		return "", err
	}
	return pageAnchorCellPrefix + properties[0], nil
}

// resolveNotionImportTitleSpans points the pending mentions and page links
// in an imported page's title at what the import brought in, before the
// page is published; one naming what it did not bring keeps its words,
// linked to it in Notion.
func resolveNotionImportTitleSpans(page *core.Record, resolver *notionImportReferenceResolver) error {
	raw := page.GetString("title_spans")
	if !strings.Contains(raw, `"`+notionImportPendingPagePrefix) && !strings.Contains(raw, `"`+notionImportPendingDatabasePrefix) &&
		!strings.Contains(raw, `"`+notionImportPendingPageLinkPrefix) {
		return nil
	}
	spans := decodeStoredTextSpans(raw)
	if _, err := resolveNotionImportSpans(spans, resolver); err != nil {
		return err
	}
	// A data source's mention named spells the title anew
	// (notion_import_data_source_titles.go).
	if spelled := spelledName(spans); spelled != page.GetString("title") && strings.Contains(raw, notionMarkdownDataSourceAddress) {
		if err := spellNotionImportTitle(resolver, page, spelled); err != nil {
			return err
		}
	}
	encoded, err := encodedPageTitleSpans(page.GetString("title"), spans)
	if err != nil {
		encoded = ""
	}
	page.Set("title_spans", encoded)
	return nil
}
