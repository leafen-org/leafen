package app

import (
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A search with no query finds every title, in the order of its results:
// last edited first (or last, when asked), then data sources before pages
// before database pages, then by id. So it need not read the workspace's
// every page title to answer one page of results: it reads the page titles
// from where its page of results starts, as many as that page and the trash
// reading after it take (notion_search_trash.go), through the index in that
// order (schema_baseline.json), and four times as many again
// only when those ran out before the page filled — titles the connection
// can't open, or under a page in the trash. Reading every title took most of
// such a search (2026-11-18, 20,000 pages). Database pages and data sources
// are still read whole: a database page's edit time is its row's or its
// body's, which no one index holds. A search with a query scores every title
// and reads them all.
//
// A page's edit time is its record's text, which sorts as its time does
// (PocketBase writes "2006-01-02 15:04:05.000Z"); a cursor that names a
// database page or a data source is turned into that text to place the
// pages after it.

// notionSearchWindow is the page titles one read of a search takes.
type notionSearchWindow struct {
	// pages is how many page titles a read takes.
	pages int
	// cursorKind and cursorID name the result the page of results starts
	// at, none for a search's first page.
	cursorKind, cursorID string
}

// notionSearchPageTitlesSQL holds for a page, aliased page, a search lists
// under its own title. A full-page database is its database, and a wiki's
// page is its wiki's database; each is found through its data source.
const notionSearchPageTitlesSQL = `[[page]].[[workspace]] = {:workspaceId} AND [[page]].[[in_trash]] = {:inTrash}
				AND [[page]].[[data_source_id]] = ''
				AND ({:inTrash} OR NOT (
					[[page]].[[kind]] = 'database'
					OR EXISTS (SELECT 1 FROM {{leafen_data_sources}} AS [[wiki_source]]
						WHERE [[wiki_source]].[[page_id]] = [[page]].[[id]] AND [[wiki_source]].[[wiki]] = 1)))`

const notionSearchPageTimeLayout = "2006-01-02 15:04:05.000Z"

// loadNotionSearchPageWindow reads a window's page titles in result order,
// from the window's cursor on. others are the search's database pages and
// data sources. It reports whether more page titles follow those read, and
// whether the cursor names a result of the search.
func loadNotionSearchPageWindow(
	app core.App, workspaceID string, options notionSearchOptions, window *notionSearchWindow, others []notionSearchCandidate,
) (pages []notionSearchCandidate, cut, found bool, err error) {
	params := dbx.Params{"workspaceId": workspaceID, "inTrash": options.inTrash, "limit": window.pages + 1}
	from := ""
	if window.cursorKind != "" {
		at, tie := "", "FALSE"
		if window.cursorKind == notionSearchPage {
			err := app.DB().NewQuery(`
				SELECT [[page]].[[updated]] FROM {{leafen_pages}} AS [[page]]
				WHERE [[page]].[[id]] = {:cursorId} AND ` + notionSearchPageTitlesSQL + `
			`).Bind(dbx.Params{"cursorId": window.cursorID, "workspaceId": workspaceID, "inTrash": options.inTrash}).Row(&at)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, false, false, nil
			}
			if err != nil {
				return nil, false, false, err
			}
			tie = `[[page]].[[id]] >= {:cursorId}`
		} else {
			index := slices.IndexFunc(others, func(candidate notionSearchCandidate) bool {
				return candidate.Kind == window.cursorKind && candidate.ID == window.cursorID
			})
			if index < 0 {
				return nil, false, false, nil
			}
			at = time.UnixMilli(others[index].Edited).UTC().Format(notionSearchPageTimeLayout)
			// Results edited in the same millisecond go by kind: the
			// pages come after a data source and before a database page.
			if notionSearchPage > window.cursorKind {
				tie = "TRUE"
			}
		}
		params["at"], params["cursorId"] = at, window.cursorID
		if options.ascending {
			from = ` AND [[page]].[[updated]] >= {:at} AND ([[page]].[[updated]] > {:at} OR ` + tie + `)`
		} else {
			from = ` AND [[page]].[[updated]] <= {:at} AND ([[page]].[[updated]] < {:at} OR ` + tie + `)`
		}
	}
	order := "DESC"
	if options.ascending {
		order = "ASC"
	}
	rows := []struct {
		ID      string `db:"id"`
		Title   string `db:"title"`
		Updated string `db:"updated"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page]].[[id]], [[page]].[[title]], [[page]].[[updated]]
		FROM {{leafen_pages}} AS [[page]] INDEXED BY [[idx_leafen_pages_search_edited]]
		WHERE ` + notionSearchPageTitlesSQL + from + `
		ORDER BY [[page]].[[updated]] ` + order + `, [[page]].[[id]] LIMIT {:limit}
	`).Bind(params).All(&rows); err != nil {
		return nil, false, false, err
	}
	if len(rows) > window.pages {
		rows, cut = rows[:window.pages], true
	}
	pages = make([]notionSearchCandidate, 0, len(rows))
	for _, row := range rows {
		pages = append(pages, notionSearchCandidate{
			Kind: notionSearchPage, ID: row.ID, Title: row.Title, Edited: notionSearchPageMillis(row.Updated),
			trashHolder: notionSearchTrashHolder(notionSearchPage, row.ID, ""),
		})
	}
	return pages, cut, true, nil
}

// notionSearchVisible reads a search's page of results and the result after
// it, which its next cursor names, from the cursor on. It reports whether a
// kind had more titles than one search reads, and whether the cursor names
// no result of the search.
func notionSearchVisible(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, options notionSearchOptions, startCursor *string,
) (visible []notionSearchCandidate, incomplete, refused bool, err error) {
	cursorKind, cursorID := "", ""
	if startCursor != nil {
		if cursorKind, cursorID = notionSearchCursorResult(event.App, identity.WorkspaceID, *startCursor); cursorKind == "" {
			return nil, false, true, nil
		}
	}
	var window *notionSearchWindow
	if options.query == "" && options.object != "data_source" {
		window = &notionSearchWindow{
			pages:      min(options.pageSize+1+notionSearchTrashWindow, notionSearchCandidateLimit),
			cursorKind: cursorKind, cursorID: cursorID,
		}
	}
	sources := map[string]bool{}
	rows := map[string]*databaseRowAccess{}
	closed := notionIdentityCloses(event.App, identity)
	for {
		candidates, more, cut, found, err := loadNotionSearchCandidates(event.App, identity.WorkspaceID, options, window)
		if err != nil || !found {
			return nil, false, !found, err
		}
		incomplete = more
		trash := newNotionSearchTrash(event.App, candidates, options.inTrash)
		offset := 0
		if startCursor != nil {
			offset = slices.IndexFunc(candidates, func(candidate notionSearchCandidate) bool {
				return candidate.Kind == cursorKind && candidate.ID == cursorID
			})
			if offset >= 0 {
				left, err := trash.leftOut(offset)
				if err != nil {
					return nil, false, false, err
				}
				if left {
					offset = -1
				}
			}
			if offset < 0 {
				return nil, false, true, nil
			}
		}
		visible = make([]notionSearchCandidate, 0, options.pageSize+1)
		for index := offset; index < len(candidates); index++ {
			candidate := candidates[index]
			if len(visible) > options.pageSize {
				break
			}
			if left, err := trash.leftOut(index); err != nil {
				return nil, false, false, err
			} else if left {
				continue
			}
			allowed, err := notionSearchCandidateVisible(event.App, identity, candidate, sources, rows)
			if err != nil {
				return nil, false, false, err
			}
			// A title is matched as the connection reads it — a page's, a
			// database page's or a data source's name: a mention in it of a page
			// it can't reach reads "Untitled", so the mentioned page's title
			// finds nothing (mcp_reader_mentions.go, page_name_mentions.go).
			if allowed && options.query != "" {
				read := ""
				if candidate.Kind == notionSearchDataSource {
					read = sourceNameAsClosed(event.App, closed, candidate.ID, candidate.Title)
				} else {
					read = mcpRowTitleAsRead(event.App, closed, candidate.ID, candidate.Title)
				}
				_, allowed = options.keywords.match(read)
			}
			if allowed {
				visible = append(visible, candidate)
			}
		}
		if len(visible) > options.pageSize || !cut {
			return visible, incomplete, false, nil
		}
		window.pages = min(window.pages*4, notionSearchCandidateLimit)
	}
}
