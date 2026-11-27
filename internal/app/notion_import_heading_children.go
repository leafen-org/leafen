package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's block API calls a plain heading (is_toggleable false) that holds
// blocks has_children false, while listing its children lists them (seen
// 2026-11-21 through API versions 2022-06-28 and 2026-03-11). A page
// Markdown replace writing a heading tag over a block that was there makes
// one (notion_page_markdown_heading_holds.go): paragraphs, lists, toggles and
// sub-pages alike. Notion's screen draws the heading alone; making it a
// toggle heading shows them as its children, has_children true. So a plain
// heading may hold blocks whatever has_children says. Reading every plain
// heading's children costs a Notion read for each, nearly always empty (a
// page of 100 headings and 200 lines read in 59 s instead of 8); the page's
// v2 Markdown, one read, shows a heading holding blocks as its tag on a line
// of its own ("<h2>⏎x⏎⏎y⏎</h2>") and one holding none as "## x". So the
// headings of a page are read only when its Markdown shows such a tag, is cut
// short, or cannot be had.

// notionProviderBlockMayHold tells whether a block's children are to be read
// (notionImportReadsChildren, the scan's rule too).
func notionProviderBlockMayHold(block notionProviderObject) bool {
	return notionImportReadsChildren(block) || notionProviderPlainHeadingNamedEmpty(block)
}

// notionProviderPlainHeadingNamedEmpty tells a plain heading Notion calls
// childless, which may hold blocks all the same.
func notionProviderPlainHeadingNamedEmpty(block notionProviderObject) bool {
	if block.HasChildren || !notionMarkdownPlainHeadings[block.Type] {
		return false
	}
	// A toggle heading tells what it holds.
	body := struct {
		IsToggleable bool `json:"is_toggleable"`
	}{}
	return json.Unmarshal(block.Payload, &body) != nil || !body.IsToggleable
}

// notionImportHeadingReads leaves the children of the plain headings Notion
// calls childless among one listing's blocks unread, staged as read, when
// the page they sit on holds no blocks under a plain heading.
func notionImportHeadingReads(
	ctx context.Context, app core.App, client notionProviderClient, jobID string,
	parentSourceID, parentKind string, blueprint bool, results []notionProviderObject, sources []stagedNotionSource,
) error {
	headings := map[string]bool{}
	for _, block := range results {
		if notionProviderPlainHeadingNamedEmpty(block) {
			headings[block.ID] = true
		}
	}
	// A template's blocks are read as Notion names them.
	if len(headings) == 0 || blueprint {
		return nil
	}
	pageID, err := notionImportBlockPage(app, jobID, parentSourceID, parentKind)
	if err != nil || pageID == "" {
		return err
	}
	holds, _, err := notionPageHoldsUnderHeadings(ctx, client, jobID, pageID)
	if err != nil || holds {
		return err
	}
	for index := range sources {
		if headings[sources[index].SourceID] && sources[index].DiscoveryStatus == notionImportPageDiscoveryPending {
			sources[index].DiscoveryStatus = notionImportPageDiscoveryComplete
		}
	}
	return nil
}

// notionImportBlockPage names the page or row a listing's blocks sit on: the
// one listed, or the one its block sits on; none when it is not staged.
func notionImportBlockPage(app core.App, jobID, parentSourceID, parentKind string) (string, error) {
	if parentKind == "page" || parentKind == "row" {
		return parentSourceID, nil
	}
	var pageID string
	err := app.DB().NewQuery(`
		WITH RECURSIVE up([[source_id]], [[parent_source_id]], [[parent_kind]], [[depth]]) AS (
			SELECT [[source_id]], [[parent_source_id]], [[parent_kind]], 0
			FROM {{leafen_notion_import_sources}} WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
			UNION ALL
			SELECT s.[[source_id]], s.[[parent_source_id]], s.[[parent_kind]], up.[[depth]] + 1
			FROM {{leafen_notion_import_sources}} s JOIN up ON s.[[source_id]] = up.[[parent_source_id]]
			WHERE s.[[job_id]] = {:jobId} AND up.[[parent_kind]] = 'block' AND up.[[depth]] < 64
		)
		SELECT [[parent_source_id]] FROM up WHERE [[parent_kind]] IN ('page', 'row') LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": parentSourceID}).Row(&pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return pageID, err
}

// notionMarkdownHeadingHoldsTag finds a heading tag that holds blocks: one
// ending its line, its words and blocks on the lines after it.
var notionMarkdownHeadingHoldsTag = regexp.MustCompile(`(?m)<h[1-4](?:\s[^>\n]*)?>[ \t]*$`)

// notionPageHoldsUnderHeadings tells whether a page may hold blocks under a
// plain heading, from its v2 Markdown (notionImportPageMarkdown), and
// whether it read Notion to tell. Markdown Notion will not give, or cuts
// short, may.
func notionPageHoldsUnderHeadings(
	ctx context.Context, client notionProviderClient, scope, pageID string,
) (bool, bool, error) {
	reading, read, err := notionImportPageMarkdown(ctx, client, scope, pageID)
	return reading.Holds, read, err
}

// notionProviderMarkdownAPIVersion is the first API version giving a page's
// v2 Markdown.
const notionProviderMarkdownAPIVersion = "2026-03-11"

// notionImportPlainHeadingSource tells whether a staged block is a plain
// heading, whose children are read in case it holds any: Notion giving none
// for it is nothing lost to report.
func notionImportPlainHeadingSource(app core.App, jobID string, discovery notionImportPageDiscovery) (bool, error) {
	if discovery.Kind != "block" {
		return false, nil
	}
	var text string
	err := app.DB().NewQuery(`
		SELECT [[text]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[kind]] = 'block'
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": discovery.SourceID}).Row(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	plan, err := decodeNotionParagraphSourceText(text)
	return err == nil && notionMarkdownPlainHeadings[plan.BlockType], nil
}
