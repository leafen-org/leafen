package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page made from the "@" or "+" menu in a line, a table row's cell or a
// block's caption in Notion has that block as its parent, and Notion doesn't
// list it among the block's children (seen 2026-11-12: the line's
// has_children is false; 2026-11-13: a table cell's page has the row as its
// parent, a caption's the block). The import finds it by the block's mention
// of it and brings it as a sub-page of the page holding the block, hanging
// on the block as here (mention_anchored_pages.go,
// mention_anchored_fields.go). One made in a page's title or a database
// page's cell is found by that field's mention (notion_import_field_pages.go).
//
// One Notion restored from its trash comes back as a sub-page block, the
// last inside its line, though the line still mentions it (seen 2026-11-18
// in two tabs of a Notion test page, as Leafen's restore makes it,
// restoreMentionAnchoredPage), and Notion's API lists it among the line's
// children. The line's mention is read before its children are, so the page
// is staged as hanging first; once the line's children list it, that
// listing takes its place, and it comes as the sub-page block Notion shows
// (releaseNotionImportListedLinePages). A callout's words are a line of its
// own in Leafen (notionImportCalloutFirstLine): a page made from their "@"
// hangs on that line, and one Notion restored comes back the last inside the
// callout, after its other lines (seen 2026-11-18 under a Notion test page,
// three pages restored through the API, the callout's children listing each and
// its screen showing the block there).

// notionImportBlockAnchor marks a staged page found by a block's mention
// (notionImportPresentation.Anchor).
const notionImportBlockAnchor = "block"

// notionImportLinePageSources stages the pages hanging on the blocks just
// read: those a block's words, caption or cells mention whose parent is that
// block. A page the import already has is left to where it is.
func notionImportLinePageSources(
	ctx context.Context, app core.App, client notionProviderClient, jobID string,
	results []notionProviderObject, blueprint bool,
) ([]stagedNotionSource, error) {
	if blueprint {
		return nil, nil
	}
	sources := []stagedNotionSource{}
	checked := map[string]bool{}
	for _, child := range results {
		if child.ID == "" || child.Unreadable || len(child.Payload) == 0 {
			continue
		}
		fields := struct {
			RichText []notionProviderRichText   `json:"rich_text"`
			Caption  []notionProviderRichText   `json:"caption"`
			Cells    [][]notionProviderRichText `json:"cells"`
		}{}
		if json.Unmarshal(child.Payload, &fields) != nil {
			continue
		}
		for _, parts := range append([][]notionProviderRichText{fields.RichText, fields.Caption}, fields.Cells...) {
			for _, pageID := range notionImportMentionedPageIDs(parts) {
				if checked[notionScopeID(pageID)] {
					continue
				}
				checked[notionScopeID(pageID)] = true
				page, found, err := notionImportHangingPage(ctx, app, client, jobID, pageID,
					notionProviderParent{Type: "block_id", BlockID: child.ID})
				if err != nil {
					return nil, err
				}
				if found {
					presentation := notionImportObjectPresentation(page, true)
					presentation.Anchor, presentation.History = notionImportBlockAnchor, notionImportObjectHistory(page)
					sources = append(sources, stagedNotionSource{
						SourceID: page.ID, Kind: "page", ParentSourceID: child.ID, ParentKind: "block",
						Title: notionObjectTitle(page), Text: encodeNotionImportPresentation(presentation),
						DiscoveryStatus: notionImportPageDiscoveryPending,
					})
				}
			}
		}
	}
	return sources, nil
}

// notionImportMentionedPageIDs lists the pages rich text mentions, a
// mention of a block on a page left out.
func notionImportMentionedPageIDs(parts []notionProviderRichText) []string {
	ids := []string{}
	for _, part := range parts {
		pageID := strings.TrimSpace(part.Mention.Page.ID)
		if part.Type == "mention" && part.Mention.Type == "page" && pageID != "" && notionProviderMentionedBlock(part) == "" {
			ids = append(ids, pageID)
		}
	}
	return ids
}

// notionImportHangingPage reads a mentioned page the import doesn't have
// yet, and reports whether its parent is the one named: Notion's own, not
// in its trash.
func notionImportHangingPage(
	ctx context.Context, app core.App, client notionProviderClient, jobID, pageID string, parent notionProviderParent,
) (notionProviderObject, bool, error) {
	var staged int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_notion_import_sources}} WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": pageID}).Row(&staged); err != nil || staged > 0 {
		return notionProviderObject{}, false, err
	}
	raw := json.RawMessage{}
	if err := client.request(ctx, http.MethodGet, "/v1/pages/"+url.PathEscape(pageID), nil, &raw); err != nil {
		if notionProviderRefused(err) {
			return notionProviderObject{}, false, nil
		}
		return notionProviderObject{}, false, err
	}
	page, trash := notionProviderObject{}, struct {
		InTrash bool `json:"in_trash"`
	}{}
	if json.Unmarshal(raw, &page) != nil || json.Unmarshal(raw, &trash) != nil || page.Object != "page" || trash.InTrash ||
		page.Parent.Type != parent.Type {
		return notionProviderObject{}, false, nil
	}
	switch parent.Type {
	case "block_id":
		return page, notionScopeID(page.Parent.BlockID) == notionScopeID(parent.BlockID), nil
	case "page_id":
		return page, notionScopeID(page.Parent.PageID) == notionScopeID(parent.PageID), nil
	}
	return notionProviderObject{}, false, nil
}

// notionImportLinePage is the line a staged sub-page inside a block hangs
// on, and whether it hangs there unmentioned, or "" when it takes a block
// there: one its mention found, not one the block's children list, in a
// block other than a sub-page's whose words, caption or cells mention it,
// or one found hanging there unmentioned (notion_import_unmentioned_pages.go).
// That line is the block itself, or for a callout the first line its words
// went to (notionImportCalloutFirstLine), as Leafen's own callout holds its
// words in a line inside it and a page made from its "@" hangs on that line;
// Notion gives such a page the callout as its parent (seen 2026-11-18).
func notionImportLinePage(
	app core.App, jobID string, source storedNotionSource, containerID string, mappings *notionApplyMappings,
) (string, bool, error) {
	anchor := notionImportStoredAnchor(source)
	unmentioned := anchor == notionImportUnmentionedAnchor
	if anchor != notionImportBlockAnchor && !unmentioned {
		return "", false, nil
	}
	block, found, err := loadStoredNotionBlock(app, containerID)
	if err != nil || !found || block.Type == graphBlockTypeChildPage {
		return "", false, err
	}
	wordsSourceID, text, found, err := notionImportBlockWords(app, jobID, source.ParentSourceID)
	if err != nil || !found || !unmentioned && !notionImportMentions(text, source.SourceID) {
		return "", false, err
	}
	if wordsSourceID == source.ParentSourceID {
		return containerID, unmentioned, nil
	}
	line, err := notionImportWordsLineID(app, jobID, wordsSourceID, mappings)
	return line, unmentioned && line != "", err
}

// notionImportStoredAnchor is what a staged page's presentation says it
// hangs on (notionImportPresentation.Anchor), "" for none.
func notionImportStoredAnchor(source storedNotionSource) string {
	stored := notionImportPresentation{}
	if !strings.HasPrefix(source.Text, notionPresentationSourcePrefix) ||
		json.Unmarshal([]byte(strings.TrimPrefix(source.Text, notionPresentationSourcePrefix)), &stored) != nil {
		return ""
	}
	return stored.Anchor
}

// notionImportBlockWords is the staged source holding a block's words and
// its text: a callout's first line once its words went there
// (notionImportCalloutFirstLine), any other block itself; a page's own is
// the page with its title.
func notionImportBlockWords(app core.App, jobID, blockSourceID string) (string, string, bool, error) {
	stored := struct {
		SourceID string `db:"source_id"`
		Text     string `db:"text"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[source_id]], [[text]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] IN ({:line}, {:block})
		ORDER BY [[source_id]] = {:line} DESC LIMIT 1
	`).Bind(dbx.Params{
		"jobId": jobID, "block": blockSourceID, "line": blockSourceID + notionImportCalloutFirstLineSuffix,
	}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return stored.SourceID, stored.Text, true, nil
}

// notionImportWordsLineID is the Leafen line a callout's first line became,
// made earlier in this run or a run before it; "" when there is none, so the
// page takes its block instead of hanging on a line that doesn't mention it.
func notionImportWordsLineID(app core.App, jobID, lineSourceID string, mappings *notionApplyMappings) (string, error) {
	if mappings != nil {
		if lineID, found := mappings.nativeID(lineSourceID, "block"); found {
			return lineID, nil
		}
	}
	var lineID string
	err := app.DB().NewQuery(`
		SELECT [[native_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[kind]] = 'block'
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": lineSourceID}).Row(&lineID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return lineID, err
}

// notionImportMentions reports whether a staged source's text names a page,
// its id written with dashes or without.
func notionImportMentions(text, pageSourceID string) bool {
	compact := strings.ReplaceAll(pageSourceID, "-", "")
	return strings.Contains(text, pageSourceID) || strings.Contains(strings.ReplaceAll(text, "-", ""), compact)
}

// releaseNotionImportListedLinePages takes out the staged pages a block's
// mention found that the block's children now list as sub-pages, as one
// Notion restored from its trash, so that the listing stages each in their
// place among the block's children instead: the mention's staging comes
// before the block's children are read, and nothing has read the page's own
// body yet. It returns how many it took out.
func releaseNotionImportListedLinePages(app core.App, jobID string, sources []stagedNotionSource) (int64, error) {
	listed := []stagedNotionSource{}
	for _, source := range sources {
		if source.Kind == "page" && source.ParentKind == "block" && source.Text == "" {
			listed = append(listed, stagedNotionSource{SourceID: source.SourceID, ParentSourceID: source.ParentSourceID})
		}
	}
	if len(listed) == 0 {
		return 0, nil
	}
	encoded, err := json.Marshal(listed)
	if err != nil {
		return 0, err
	}
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND [[kind]] = 'page' AND [[parent_kind]] = 'block'
			AND [[discovery_status]] = 'pending' AND [[discovery_cursor]] = '' AND [[discovery_position]] = 0
			AND substr([[text]], 1, length({:prefix})) = {:prefix}
			AND json_extract(substr([[text]], length({:prefix}) + 1), '$.anchor') = {:anchor}
			AND EXISTS (
				SELECT 1 FROM json_each({:listed}) AS [[listed]]
				WHERE json_extract([[listed]].[[value]], '$.sourceId') = [[source_id]]
					AND json_extract([[listed]].[[value]], '$.parentSourceId') = [[parent_source_id]]
			)
	`).Bind(dbx.Params{
		"jobId": jobID, "prefix": notionPresentationSourcePrefix, "anchor": notionImportBlockAnchor,
		"listed": string(encoded),
	}).Execute()
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
