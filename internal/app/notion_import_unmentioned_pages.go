package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page made from a line's, a table row's or a caption's "@" whose mention
// an integration took out of that block stays in Notion with the block as
// its parent, out of its trash and out of its sidebar
// (mention_anchored_unmentioned.go), and nothing in the page leads to it
// (seen 2026-11-18 under 한지테스트페이지: the line's has_children false, its
// children and words naming no page); only Notion's search finds it, the
// block as its parent. One made from a page's title or a database page's
// cell stays so with that page as its parent, its body listing no block of
// it (seen 2026-11-18, a title's and a text cell's). Once all under the
// import's roots is staged the import reads Notion's search and stages each
// page whose parent is a block, a page or a database page it staged and
// which it doesn't have yet — none listed in its parent's body is — hanging
// unmentioned on that block, on that page's title or on that database
// page's title cell, as Leafen keeps such a page; its body is read as any
// page's. A pass that staged one reads the search again, for the pages
// hanging unmentioned on it and its blocks.
//
// Such a page was made on its block or page, so after it (seen 2026-11-18:
// the page a minute after its line, both orphans of 'W1846 orphan check'; a
// title's and a cell's after their page): no page last edited before the
// oldest block or page a pass looks for can hang on one. The search lists
// the most recently edited first, so a pass reads only down to the oldest
// block or page staged since the last pass began, less a margin — a small
// new import reads a page of the search, not every page the connection
// reaches; an import of old pages reads back to when they were made.

// notionImportUnmentionedAnchor marks a staged page its block's parenthood
// found though the block doesn't mention it (notionImportPresentation.Anchor).
const notionImportUnmentionedAnchor = "unmentioned"

// notionImportNonBlockSourceKinds are the kinds of staged source that are
// no block of a page's body: every other kind leafen_notion_import_sources
// takes is one, staged by its type, which a page can hang on.
var notionImportNonBlockSourceKinds = map[string]bool{
	"page": true, "template": true, "database": true, "property": true, "row": true, "issue": true, "view": true,
}

// notionImportUnmentionedMargin is how much older than a pass's oldest block
// the search may go before the pass stops: Notion gives its times to the
// minute, and a page and its block made or copied together can fall either
// side of one.
const notionImportUnmentionedMargin = time.Hour

// notionImportUnmentionedSweep is where the import's reading of Notion's
// search for such pages is: the next search page, whether the pass so far
// staged one, and whether a pass found none.
type notionImportUnmentionedSweep struct {
	UnmentionedCursor string `json:"unmentionedCursor,omitempty"`
	UnmentionedFound  bool   `json:"unmentionedFound,omitzero"`
	UnmentionedSwept  bool   `json:"unmentionedSwept,omitzero"`
	// UnmentionedDated holds while the job has noted, from its start, when
	// each block and page it staged was made; a job begun before it noted
	// them — or noted blocks alone, under the name unmentionedDated — or one
	// with a block Notion gave no time, reads the search through every pass.
	UnmentionedDated bool `json:"unmentionedHoldersDated,omitzero"`
	// UnmentionedSince is the oldest of those times since the last pass
	// began, and UnmentionedBound the one the pass under way reads down to.
	UnmentionedSince string `json:"unmentionedSince,omitempty"`
	UnmentionedBound string `json:"unmentionedBound,omitempty"`
}

// notionImportUnmentionedSweepFromStart is a new job's: it notes when the
// blocks and pages it stages were made from its first turn.
func notionImportUnmentionedSweepFromStart() notionImportUnmentionedSweep {
	return notionImportUnmentionedSweep{UnmentionedDated: true}
}

// noteUnmentionedParents keeps the oldest time one of the blocks or pages a
// turn stages was made, of those a page can hang on unmentioned
// (notionHangingParentMade), none in a template.
func (sweep *notionImportUnmentionedSweep) noteUnmentionedParents(objects []notionProviderObject, blueprint bool) {
	if !sweep.UnmentionedDated || blueprint {
		return
	}
	for _, object := range objects {
		made, holds, dated := notionHangingParentMade(object)
		if !holds {
			continue
		}
		if !dated {
			sweep.UnmentionedDated, sweep.UnmentionedSince, sweep.UnmentionedBound = false, "", ""
			return
		}
		if since, err := time.Parse(time.RFC3339Nano, sweep.UnmentionedSince); err != nil || made.Before(since) {
			sweep.UnmentionedSince = made.UTC().Format(time.RFC3339Nano)
		}
	}
}

// notionHangingParentMade is when a Notion object a page can hang on
// unmentioned was made: a block — a sub-page's block made with its page,
// whose title can hold one — or a page or database page; a database's own
// block and a template hold none (holds false). dated is false for one
// Notion gave no time.
func notionHangingParentMade(object notionProviderObject) (made time.Time, holds, dated bool) {
	if object.Object != "block" && object.Object != "page" || object.Type == "child_database" || object.Type == "template" {
		return time.Time{}, false, false
	}
	made, err := time.Parse(time.RFC3339, strings.TrimSpace(object.CreatedTime))
	return made, true, err == nil
}

// pastBound tells whether a search page, newest first, reached a page last
// edited before the pass's oldest block or page less the margin: none after
// it can hang on one the pass looks for.
func (sweep notionImportUnmentionedSweep) pastBound(results []json.RawMessage) bool {
	if !sweep.UnmentionedDated || len(results) == 0 {
		return false
	}
	bound, err := time.Parse(time.RFC3339Nano, sweep.UnmentionedBound)
	if err != nil {
		return false
	}
	last := struct {
		LastEditedTime string `json:"last_edited_time"`
	}{}
	if json.Unmarshal(results[len(results)-1], &last) != nil {
		return false
	}
	edited, err := time.Parse(time.RFC3339, strings.TrimSpace(last.LastEditedTime))
	return err == nil && edited.Before(bound.Add(-notionImportUnmentionedMargin))
}

// sweepNotionImportUnmentionedPages reads one search page and stages the
// pages hanging unmentioned on the import's blocks. Search Notion refuses
// leaves the import as it is.
func (r *durableJobRunner) sweepNotionImportUnmentionedPages(
	ctx context.Context, job storedJob, payload notionImportJobPayload, client notionProviderClient,
) error {
	sweep := &payload.notionImportUnmentionedSweep
	if sweep.UnmentionedCursor == "" {
		// A pass begins, reading down to the oldest block or page staged
		// since the last one began; with none staged no page can newly hang
		// on one.
		sweep.UnmentionedBound, sweep.UnmentionedSince = sweep.UnmentionedSince, ""
		if sweep.UnmentionedDated && sweep.UnmentionedBound == "" {
			sweep.UnmentionedFound, sweep.UnmentionedSwept = false, true
			return r.commitNotionDiscovery(job, payload, nil)
		}
	}
	request := map[string]any{
		"page_size": notionImportProviderPageItems, "filter": map[string]any{"property": "object", "value": "page"},
		"sort": map[string]any{"direction": "descending", "timestamp": "last_edited_time"},
	}
	if sweep.UnmentionedCursor != "" {
		request["start_cursor"] = sweep.UnmentionedCursor
	}
	found := struct {
		Results    []json.RawMessage `json:"results"`
		HasMore    bool              `json:"has_more"`
		NextCursor *string           `json:"next_cursor"`
	}{}
	if err := client.request(ctx, http.MethodPost, "/v1/search", request, &found); err != nil {
		if !notionProviderRefused(err) {
			return err
		}
		found.Results, found.HasMore = nil, false
	}
	if len(found.Results) > notionImportProviderPageItems {
		return errors.New(errorCodeNotionImportProviderFailed)
	}
	runPlan, err := loadNotionMigrationRunPlan(r.app, job.Owner, payload)
	if err != nil {
		return err
	}
	sources, staged, err := notionImportUnmentionedPageSources(r.app, job.ID, runPlan, found.Results)
	if err != nil {
		return err
	}
	// Those it stages can hold more, the next pass's to find.
	sweep.noteUnmentionedParents(staged, false)
	sweep.UnmentionedFound = sweep.UnmentionedFound || len(sources) > 0
	if found.HasMore && found.NextCursor != nil && *found.NextCursor != "" && *found.NextCursor != sweep.UnmentionedCursor &&
		!sweep.pastBound(found.Results) {
		sweep.UnmentionedCursor = *found.NextCursor
	} else {
		*sweep = notionImportUnmentionedSweep{
			UnmentionedSwept: !sweep.UnmentionedFound, UnmentionedDated: sweep.UnmentionedDated,
			UnmentionedSince: sweep.UnmentionedSince,
		}
	}
	return r.commitNotionDiscovery(job, payload, sources)
}

// notionImportUnmentionedPageSources stages, of a search page's results, the
// live pages whose parent is a block, a page or a database page the import
// staged, not in a template, which it doesn't have yet and the plan doesn't
// leave out; staged are their Notion pages.
func notionImportUnmentionedPageSources(
	app core.App, jobID string, runPlan *notionMigrationRunPlan, results []json.RawMessage,
) (sources []stagedNotionSource, staged []notionProviderObject, err error) {
	pages, holders, ids := []notionProviderObject{}, []string{}, []string{}
	for _, raw := range results {
		page, state := notionProviderObject{}, struct {
			InTrash  bool `json:"in_trash"`
			Archived bool `json:"archived"`
		}{}
		if json.Unmarshal(raw, &page) != nil || json.Unmarshal(raw, &state) != nil || page.Object != "page" ||
			page.ID == "" || state.InTrash || state.Archived || notionImportUnmentionedHolder(page) == "" ||
			runPlan.skips(page.ID) {
			continue
		}
		pages, holders, ids = append(pages, page), append(holders, notionImportUnmentionedHolder(page)), append(ids, page.ID)
	}
	if len(pages) == 0 {
		return nil, nil, nil
	}
	stored := []struct {
		SourceID  string `db:"source_id"`
		Kind      string `db:"kind"`
		Blueprint bool   `db:"blueprint"`
	}{}
	encoded, _ := json.Marshal(append(holders, ids...))
	if err := app.DB().NewQuery(`
		SELECT [[source_id]], [[kind]], [[blueprint]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"jobId": jobID, "ids": string(encoded)}).All(&stored); err != nil {
		return nil, nil, err
	}
	// What each staged holder is to a page hanging on it: a block, or a page
	// or database page by its title or cells.
	holds, have := map[string]string{}, map[string]bool{}
	for _, source := range stored {
		have[source.SourceID] = true
		switch {
		case source.Blueprint:
		case source.Kind == "page" || source.Kind == "row":
			holds[source.SourceID] = source.Kind
		case !notionImportNonBlockSourceKinds[source.Kind]:
			holds[source.SourceID] = "block"
		}
	}
	for index, page := range pages {
		holder := holders[index]
		kind := holds[holder]
		if have[page.ID] || kind == "" || (kind == "block") != (page.Parent.Type == "block_id") {
			continue
		}
		have[page.ID] = true
		presentation := notionImportObjectPresentation(page, true)
		presentation.Anchor, presentation.History = notionImportUnmentionedAnchor, notionImportObjectHistory(page)
		sources = append(sources, stagedNotionSource{
			SourceID: page.ID, Kind: "page", ParentSourceID: holder, ParentKind: kind,
			Title: notionObjectTitle(page), Text: encodeNotionImportPresentation(presentation),
			DiscoveryStatus: notionImportPageDiscoveryPending,
		})
		staged = append(staged, page)
	}
	return sources, staged, nil
}

// notionImportUnmentionedHolder is the block or page a page found by the
// search has as its parent, "" for one in a database or the workspace.
func notionImportUnmentionedHolder(page notionProviderObject) string {
	switch page.Parent.Type {
	case "block_id":
		return page.Parent.BlockID
	case "page_id":
		return page.Parent.PageID
	}
	return ""
}

// syncNotionImportPageMention keeps a page an earlier run of a migration hung
// on a block, a title or a cell mentioned there or not as Notion now has it:
// listed again once it mentions the page, left out while the page hangs
// there unmentioned (anchoredPageStepOf).
func syncNotionImportPageMention(app core.App, source storedNotionSource, pageID string) error {
	anchor := notionImportStoredAnchor(source)
	switch anchor {
	case notionImportBlockAnchor, notionImportUnmentionedAnchor, pageAnchorTitle, notionImportCellAnchor:
	default:
		return nil
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil || page.GetString(pageAnchorBlockField) == "" || page.GetBool("in_trash") {
		return err
	}
	return setPageAnchorUnmentioned(app, pageID, anchor == notionImportUnmentionedAnchor)
}
