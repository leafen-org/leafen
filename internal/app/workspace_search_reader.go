package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A search shows its reader a found page as they read it. The index keeps
// a page's sub-pages' titles as lines of it, as Notion's search finds a page
// by a sub-page's title (seen 2026-09-30); a reader who may not open a
// sub-page doesn't see it on the page, and Notion's search finds the page by
// that title for no such reader (seen 2026-11-09). Such a page is read again
// without the line: kept while what the reader reads of it still answers the
// keywords, its line and highlights from that. A database page's title, and
// a full-page database's, holds its mentions' words only as it is shown
// (database_row_search.go, page_name_mentions.go): shown to a reader, a
// mention in it of a page closed to them reads in marks, as their sidebar
// shows it, "Untitled" to an agent (mcp_reader_mentions.go). Search keeps no
// mention's words, so nothing else of a found page names a page closed to
// its reader.

// workspaceSearchClosedLines is, of found pages, those holding the line of a
// sub-page closed to the reader, with those sub-pages. A sub-page living
// under the page and following its sharing opens wherever the page does;
// only the others are asked: a sub-page whose sharing was unlinked from the
// page's (schema_baseline.json), and the sub-page block of a page
// living elsewhere, which the page's references keep (schema_baseline.json).
// Neither reads a page's every sub-page block: a page of thousands of them
// took its own time to read its blocks' contents (2026-11-09). A wiki home's
// links to its pages, lines of it too (searchPageChildLines), are all asked, from
// its references: a wiki's page is its database's, not under the home.
func workspaceSearchClosedLines(app core.App, reader wordsReader, workspaceID string, pageIDs []string) (map[string]map[string]bool, error) {
	closed := map[string]map[string]bool{}
	if len(pageIDs) == 0 {
		return closed, nil
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	lines := []struct {
		PageID  string `db:"page_id"`
		ChildID string `db:"child_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[child]].[[parent_id]] AS [[page_id]], [[child]].[[id]] AS [[child_id]]
		FROM {{leafen_pages}} [[child]] INDEXED BY [[idx_leafen_pages_workspace_parent]]
		WHERE [[child]].[[workspace]] = {:workspaceId} AND [[child]].[[parent_id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND EXISTS (SELECT 1 FROM {{leafen_page_share_unlinks}} [[unlink]] WHERE [[unlink]].[[page_id]] = [[child]].[[id]])
		UNION
		SELECT [[reference]].[[source_page_id]], [[reference]].[[target_page_id]]
		FROM {{leafen_block_page_references}} [[reference]] INDEXED BY [[idx_leafen_block_page_references_source]]
		JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[reference]].[[block_id]]
		WHERE [[reference]].[[source_page_id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND ([[block]].[[type]] = {:type} OR ` + wikiPageLinkSQL("[[block]]") + `)
	`).Bind(dbx.Params{"ids": string(encoded), "workspaceId": workspaceID, "type": graphBlockTypeChildPage}).All(&lines); err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return closed, nil
	}
	children := make([]string, 0, len(lines))
	for _, line := range lines {
		children = append(children, line.ChildID)
	}
	open, err := reader.opens(app, children)
	if err != nil {
		return nil, err
	}
	for _, line := range lines {
		if open[line.ChildID] {
			continue
		}
		if closed[line.PageID] == nil {
			closed[line.PageID] = map[string]bool{}
		}
		closed[line.PageID][line.ChildID] = true
	}
	return closed, nil
}

// workspaceSearchReadAs reads a page's search words again as a reader some
// of its sub-pages are closed to reads them.
func workspaceSearchReadAs(app core.App, workspaceID, pageID string, lines map[string]bool) (searchPageProjection, error) {
	return buildSearchPageAs(app, storedSearchDirty{PageID: pageID, WorkspaceID: workspaceID, Version: 1},
		subPageLinesCloser(lines))
}

// subPageLinesCloser closes to a reader the lines of the sub-pages named.
func subPageLinesCloser(lines map[string]bool) searchMentionCloser {
	return func(mention, targetID string) mentionReach {
		if mention == "page" && lines[targetID] {
			return mentionClosed
		}
		return mentionOpens
	}
}

// workspaceSearchReading is one search's reading of its found pages for its
// reader: the pages it read again, by page.
type workspaceSearchReading struct {
	reader      wordsReader
	workspaceID string
	plainQuery  string
	keywords    workspaceSearchKeywords
	tiered      bool
	reads       map[string]searchPageProjection
}

func newWorkspaceSearchReading(reader wordsReader, workspaceID string, match workspaceSearchMatch, plainQuery string, tiered bool) *workspaceSearchReading {
	return &workspaceSearchReading{reader: reader, workspaceID: workspaceID, plainQuery: plainQuery, keywords: match.Keywords,
		tiered: tiered, reads: map[string]searchPageProjection{}}
}

// keep leaves out, of found pages the reader may read, those a line closed
// to them found, and reads again those it keeps that hold such a line, until
// it keeps as many as are wanted.
func (reading *workspaceSearchReading) keep(app core.App, candidates []workspaceSearchCandidate, want int) ([]workspaceSearchCandidate, error) {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.PageID)
	}
	closed, err := workspaceSearchClosedLines(app, reading.reader, reading.workspaceID, ids)
	if err != nil || len(closed) == 0 {
		return candidates, err
	}
	kept := make([]workspaceSearchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if len(kept) == want {
			break
		}
		lines, holds := closed[candidate.PageID]
		if !holds {
			kept = append(kept, candidate)
			continue
		}
		read, err := workspaceSearchReadAs(app, reading.workspaceID, candidate.PageID, lines)
		if err != nil {
			return nil, err
		}
		if read.Deleted || !reading.answers(candidate, read) {
			continue
		}
		reading.reads[candidate.PageID] = read
		kept = append(kept, candidate)
	}
	return kept, nil
}

// keepRows is keep for results already made, which an agent's narrowed
// token reads after its person, and shows what it keeps as read.
func (reading *workspaceSearchReading) keepRows(app core.App, rows []storedWorkspaceSearchResult) ([]storedWorkspaceSearchResult, error) {
	candidates := make([]workspaceSearchCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, workspaceSearchCandidate{PageID: row.PageID, Score: row.Score})
	}
	kept, err := reading.keep(app, candidates, len(candidates))
	if err != nil {
		return nil, err
	}
	keeps := make(map[string]bool, len(kept))
	for _, candidate := range kept {
		keeps[candidate.PageID] = true
	}
	rows = slices.DeleteFunc(rows, func(row storedWorkspaceSearchResult) bool { return !keeps[row.PageID] })
	reading.show(app, rows)
	return rows, nil
}

// answers is whether a page read again still answers the keywords as it
// was found (workspace_search_keywords.go): by its title or a page above it
// as it was; by all of them, in a body tier or by a time sort through its own
// words; by its phrases and some words, among the pages matching some.
func (reading *workspaceSearchReading) answers(candidate workspaceSearchCandidate, read searchPageProjection) bool {
	if reading.keywords.empty() {
		return true
	}
	all, some := reading.keywords.matchPage(read.Title, read.Body)
	switch tier := candidate.tier(reading.tiered); {
	case tier == workspaceSearchTierPath || tier == workspaceSearchTierSomePath:
		return true
	case workspaceSearchSomeTier(tier):
		return some
	case reading.tiered && workspaceSearchTitleTier(tier):
		return true
	}
	return all
}

// show puts found pages as their reader reads them: a page read again shows
// the passage of what they read (workspace_search_passage.go), and a title
// holding a mention's words — a database page's, a database's
// (page_name_mentions.go) — reads a mention closed to them in marks, as
// their sidebar does, or "Untitled" to an agent.
func (reading *workspaceSearchReading) show(app core.App, rows []storedWorkspaceSearchResult) {
	var closes searchMentionCloser
	var screen func() searchMentionCloser
	for index := range rows {
		row := &rows[index]
		if read, again := reading.reads[row.PageID]; again {
			row.passage = workspaceSearchPassageOf(read.Body, reading.plainQuery)
			row.Preview, row.readAs = row.passage.line(), true
		}
		if foldSearchText(row.Title) == row.TitleFolded {
			continue
		}
		// A database held in a page's body is named by its first source
		// (workspace_search_databases.go).
		if row.InlineDatabase {
			spans := inlineDatabaseNameSpans(app, row.PageID, row.Title)
			if reading.reader.token == nil {
				if screen == nil {
					screen = readerCloser(app, reading.reader.principalID)
				}
				row.Title = databaseNameAsRead(screen, row.Title, spans)
				continue
			}
			if closes == nil {
				closes = reading.reader.closes(app)
			}
			if words, hid := spansAsRead(closes, spans); hid {
				row.Title = words
			}
			continue
		}
		if reading.reader.token == nil {
			if screen == nil {
				screen = readerCloser(app, reading.reader.principalID)
			}
			row.Title = pageTitleAsReadBy(app, screen, row.PageID, row.Title)
			continue
		}
		if closes == nil {
			closes = reading.reader.closes(app)
		}
		row.Title = mcpRowTitleAsRead(app, closes, row.PageID, row.Title)
	}
}
