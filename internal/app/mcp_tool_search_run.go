package app

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// One step of an MCP search over a workspace: it checks matches from a
// position for up to mcpSearchCheckBudget (mcp_tool_search.go), and its
// results, cursors and highlights are what the search tools answer with.

// runMCPSearch searches a workspace under a filter from a position.
func runMCPSearch(
	app core.App, authority mcpAuthority, authorization workspaceAuthorization,
	query string, filter workspaceSearchFilter, startCursor string, limit int,
) (mcpSearchOutput, error) {
	run, err := advanceMCPSearch(app, authority, authorization, query, filter, startCursor, limit, mcpSearchCheckBudget)
	if err != nil {
		return mcpSearchOutput{}, err
	}
	if run.step.Indexing {
		next, err := run.goesOn()
		if err != nil {
			return mcpSearchOutput{}, err
		}
		return mcpSearchOutput{
			Results: []mcpSearchResult{}, HasMore: true, NextCursor: &next, Indexing: true,
		}, nil
	}
	output := mcpSearchOutput{Results: []mcpSearchResult{}, HasMore: run.step.HasMore}
	if workspaceSearchBacklog(app, authorization.WorkspaceID) {
		output.Backlog = mcpSearchBacklogNotice
	}
	rows, err := run.shown(app, authority)
	if err != nil {
		return mcpSearchOutput{}, err
	}
	found := []contentRead{}
	if output.Results, found, err = run.results(app, authority, rows); err != nil {
		return mcpSearchOutput{}, err
	}
	if run.step.HasMore && len(run.step.Rows) > 0 {
		next, err := run.after(run.step.Last)
		if err != nil {
			return mcpSearchOutput{}, err
		}
		output.NextCursor = &next
	}
	recordMCPReads(app, authority, authorization.WorkspaceID, found)
	return output, nil
}

const mcpSearchBacklogNotice = "Some recently changed pages are still being indexed and may be missing from these results; " +
	"if what the person wants isn't here, say so and search again in a little while."

// mcpSearchRun is one step of a workspace's search: the matches it checked
// from a position, under the filter it was asked with.
type mcpSearchRun struct {
	authorization workspaceAuthorization
	filter        workspaceSearchFilter
	query         string
	plainQuery    string
	queryHash     string
	match         workspaceSearchMatch
	// highlight is the keywords as an agent's highlight marks them.
	highlight mcpSearchHighlightQuery
	// from is where the step's page of results starts.
	from workspaceSearchCursor
	step workspaceSearchQueryStep
}

// advanceMCPSearch checks a workspace's matches from a position (a
// start_cursor its search gave) for a budget at most.
func advanceMCPSearch(
	app core.App, authority mcpAuthority, authorization workspaceAuthorization,
	query string, filter workspaceSearchFilter, startCursor string, limit int, budget time.Duration,
) (mcpSearchRun, error) {
	// Without keywords, search browses by its filters or sort.
	if query == "" {
		browse := false
		if filter, browse = filter.forBrowse(); !browse {
			return mcpSearchRun{}, mcpInvalid("an empty query browses: name a team with teamspace_id or a page with page_url, add a filter, or sort by last_edited or created — or give keywords")
		}
	}
	var err error
	queryFolded := foldSearchText(query)
	run := mcpSearchRun{
		authorization: authorization, filter: filter, query: query,
		plainQuery: workspaceSearchPlainText(queryFolded),
		queryHash:  workspaceSearchQueryHash(queryFolded + filter.hashKey()),
		highlight:  newMCPSearchHighlightQuery(query),
	}
	run.from = workspaceSearchCursor{
		PrincipalID: authority.PrincipalID,
		WorkspaceID: authorization.WorkspaceID,
		QueryHash:   run.queryHash,
	}
	indexCursor := workspaceSearchIndexCursor{}
	hasIndexCursor := false
	if startCursor != "" {
		switch {
		case strings.HasPrefix(startCursor, "i."):
			indexCursor, hasIndexCursor, err = decodeWorkspaceSearchIndexCursor(
				strings.TrimPrefix(startCursor, "i."), authority.PrincipalID,
				authorization.WorkspaceID, run.queryHash, filter.bestMatch(),
			)
			if err != nil {
				return mcpSearchRun{}, mcpInvalid("start_cursor isn't one this search gave, or the search changed since: search again without it")
			}
			run.from.Score, run.from.PageID, run.from.Row = indexCursor.AfterScore, indexCursor.AfterPageID, indexCursor.AfterRow
		case strings.HasPrefix(startCursor, "r."):
			run.from, err = decodeWorkspaceSearchCursor(
				strings.TrimPrefix(startCursor, "r."), authority.PrincipalID,
				authorization.WorkspaceID, run.queryHash, filter.bestMatch(),
			)
			if err != nil {
				return mcpSearchRun{}, mcpInvalid("start_cursor isn't one this search gave, or the search changed since: search again without it")
			}
		default:
			return mcpSearchRun{}, mcpInvalid("start_cursor is the next_cursor a search gave, as it gave it")
		}
	}

	run.match = workspaceSearchMatchFor(queryFolded)
	if run.match.Exact == "" && query != "" {
		return mcpSearchRun{}, mcpInvalid("the query has no word to search for: give letters or numbers")
	}
	run.step, err = advanceWorkspaceSearchWithin(
		budget, app, authorization, authority.PrincipalID, run.plainQuery, run.match,
		run.from, indexCursor, hasIndexCursor, limit, filter,
	)
	if err != nil {
		if errors.Is(err, errWorkspaceSearchRevisionChanged) {
			return mcpSearchRun{}, mcpInvalid("the pages changed while this search went on: search again without start_cursor")
		}
		return mcpSearchRun{}, failedBecause(errMCPToolFailed, err)
	}
	return run, nil
}

// goesOn is where a step still checking goes on from.
func (run mcpSearchRun) goesOn() (string, error) {
	next, err := encodeWorkspaceSearchIndexCursor(run.step.IndexCursor)
	if err != nil {
		return "", failedBecause(errMCPToolFailed, err)
	}
	return "i." + next, nil
}

// after is where a later page starts, after a result of the step.
func (run mcpSearchRun) after(last workspaceSearchCandidate) (string, error) {
	next, err := encodeWorkspaceSearchCursor(workspaceSearchCursor{
		PrincipalID: run.from.PrincipalID, WorkspaceID: run.authorization.WorkspaceID,
		QueryHash: run.queryHash, Revision: run.step.Revision, Score: last.Score, PageID: last.PageID, Row: last.Row,
	})
	if err != nil {
		return "", failedBecause(errMCPToolFailed, err)
	}
	return "r." + next, nil
}

// shown is the step's results an answer lists: a token narrowed to chosen
// pages finds only what lies under them, as it reads them — its person found
// them as they read them (workspace_search_reader.go); a template is found
// in the app alone.
func (run mcpSearchRun) shown(app core.App, authority mcpAuthority) ([]storedWorkspaceSearchResult, error) {
	rows := run.step.Rows
	if authority.PagesNarrowed {
		rows = slices.DeleteFunc(slices.Clone(rows), func(row storedWorkspaceSearchResult) bool {
			return !mcpAuthorityReachesPageID(app, authority, row.PageID)
		})
		reading := newWorkspaceSearchReading(mcpReader(authority), run.authorization.WorkspaceID, run.match,
			run.plainQuery, run.filter.bestMatch())
		var err error
		if rows, err = reading.keepRows(app, rows); err != nil {
			return nil, failedBecause(errMCPToolFailed, err)
		}
	}
	return slices.DeleteFunc(slices.Clone(rows), func(row storedWorkspaceSearchResult) bool { return row.Template }), nil
}

// results answers rows the step found, with the found pages' passages as
// reads of them.
func (run mcpSearchRun) results(
	app core.App, authority mcpAuthority, rows []storedWorkspaceSearchResult,
) ([]mcpSearchResult, []contentRead, error) {
	results, found := []mcpSearchResult{}, []contentRead{}
	for _, row := range rows {
		// A database held in a page's body is a database, at its address on
		// that page, placed by the page.
		if row.InlineDatabase {
			result := mcpSearchResult{
				ID: row.PageID, WorkspaceID: row.WorkspaceID, Title: row.Title, Type: "database",
				URL: mcpDatabaseAddress(authority, row.PageID), Highlight: run.titleHighlight(row.Title), indexed: row.Title,
			}
			// Its title as it shows, as Notion's search keeps it and as its
			// highlight says it (mcp_plain_titles.go), by the name it goes by
			// everywhere (databaseNameFrom): one of several without a name of
			// its own by its sources' names listed.
			if names, err := databaseContainerNames(app, []string{row.PageID}); err == nil && names[row.PageID].Sourced {
				name := names[row.PageID]
				result.Title = mcpDatabaseNameShown(app, authority, row.WorkspaceID, name)
				result.indexed = mcpDatabaseNameAsIndexed(app, authority.PrincipalID, name)
				result.Highlight = run.titleHighlight(mcpDatabaseNameAsSearched(app, authority.PrincipalID, name))
			}
			if ancestors, err := mcpDatabaseAncestors(app, authority, row.ParentID, row.PageID); err == nil {
				if path := mcpAncestorPath(app, ancestors); path != nil {
					result.Path = *path
				}
			}
			// An untitled one is called a new database (mcp_untitled_names.go);
			// Notion's search lists none, by the title it keeps (indexed).
			result.Title = mcpNamedOr(result.Title, mcpUntitledDatabaseName(app, authority.PrincipalID, nil, true))
			results = append(results, result)
			continue
		}
		// A found page's highlight is its passage in Notion's form
		// (workspace_search_passage.go), read as the app's results read it:
		// its lines holding the keywords or their parts
		// (mcp_search_highlight_marks.go).
		highlight := ""
		if run.query != "" && row.Preview != "" {
			passage := row.passage
			if !row.readAs {
				var err error
				if passage, err = workspaceSearchOriginalPassage(app, row.PageID, run.highlight.plain()); err != nil {
					return nil, nil, failedBecause(errMCPToolFailed, err)
				}
			}
			// Said as an agent's highlight says a page's words (mcp_search_highlight.go).
			highlight = mcpSearchHighlightPassage(passage, run.highlight).highlight()
		}
		// A found page's passage is a read of it (read_log.go).
		byTitle := highlight == ""
		if !byTitle {
			found = append(found, contentRead{Kind: "page", ID: row.PageID, How: contentReadList})
		} else {
			highlight = run.titleHighlight(mcpTitleAsSearched(app, row.PageID, row.Title))
		}
		// Its title as it shows and as Notion's search keeps it (mcp_plain_titles.go).
		result := mcpSearchResult{
			ID: row.PageID, WorkspaceID: row.WorkspaceID, Title: mcpTitleAsShown(app, authority, row.WorkspaceID, row.PageID, row.Title),
			URL: mcpPageURL(authority, row.PageID), Highlight: highlight, indexed: mcpTitleAsIndexed(app, row.PageID, row.Title),
		}
		if record, err := app.FindRecordById(pagesCollection, row.PageID); err == nil {
			if ancestors, err := mcpPageAncestors(app, authority, record); err == nil {
				if path := mcpAncestorPath(app, ancestors); path != nil {
					result.Path = *path
				}
				// A row names the data source it stands in, as its fetch's
				// <parent-data-source> does.
				if len(ancestors) > 0 && ancestors[0].Kind == mcpFetchAncestorDataSource {
					result.ParentDataSourceURL = ancestors[0].URL
				}
			}
			result.Type = "page"
			// A full-page database or a wiki is found as the database, by its
			// own ID and address, as Notion's search names it (seen
			// 2026-11-14: id and url both the database's), not by its page's.
			if container := mcpFullPageDatabaseContainer(app, row.PageID); container != "" {
				result.Type, result.ID, result.URL = "database", container, mcpDatabaseAddress(authority, container)
				// An untitled one goes by the name the one rule gives it, its
				// sources' names listed (databaseNameFrom), as it is found by
				// them (searchUntitledDatabaseWords).
				if isFullPageDatabase(record) && strings.TrimSpace(row.Title) == "" {
					if names, err := databaseContainerNames(app, []string{container}); err == nil && len(names[container].Sources) > 0 {
						name := names[container]
						result.Title = mcpDatabaseNameShown(app, authority, row.WorkspaceID, name)
						result.indexed = mcpDatabaseNameAsIndexed(app, authority.PrincipalID, name)
						if byTitle {
							result.Highlight = run.titleHighlight(mcpDatabaseNameAsSearched(app, authority.PrincipalID, name))
						}
					}
				}
			} else if isFullPageDatabase(record) {
				result.Type, result.URL = "database", mcpPageAddress(app, authority, row.PageID)
			}
			if facts, err := loadMCPPageFacts(app, record, time.Now(), authority.clock()); err == nil {
				result.Timestamp, result.Verification = mcpSearchTimestamp(facts.LastEditedAt, authority.clock()), &facts.Verification
			}
			// An untitled one is called what the screen calls it, as its
			// fetch heads it (mcp_untitled_names.go), notion-search's too
			// (notionMCPSearchTitle).
			if strings.TrimSpace(result.Title) == "" {
				result.Title = mcpUntitledPageName(app, authority.PrincipalID, record)
			}
		}
		results = append(results, result)
	}
	return results, found, nil
}

// titleHighlight is the highlight of a result whose body holds none of the
// keywords — a database, a row or page with nothing written that matches —
// as Notion's search gives it: its title, the keywords in it marked (seen
// 2026-11-14: "w1018 **qmzk** 제목만" for a page whose body doesn't hold
// "qmzk"), and its title as it is when that holds none either, as for a page
// found by its path (seen 2026-11-22 twice: "w2335 경로 아래" under "w2335
// 경로 tolvrex" for "tolvrex" and for "tolvrex brask").
func (run mcpSearchRun) titleHighlight(title string) string {
	if run.query == "" {
		return ""
	}
	if block, held := run.highlight.blockOf(searchBlockText(title)); held {
		return workspaceSearchPassage{block}.highlight()
	}
	return searchLineShown(searchBlockText(title))
}

// mcpSearchTimestamp is when a found page was last edited as Notion's search
// gives it, to the minute (seen 2026-11-14: "2026-11-14T01:53:00.000Z" for a
// page fetch says was last edited at 01:53:37.451), on the clock
// (person_clock.go).
func mcpSearchTimestamp(value string, clock *time.Location) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return personClockMillis(parsed.Truncate(time.Minute), clock)
}
