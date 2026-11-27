package app

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where a highlight is cut it says "...", three full stops, as Notion's do
// (seen 2026-11-11); how much of its line it shows is
// workspaceSearchHighlightWindow's.
const (
	workspaceSearchHighlightCut = "..."
	// workspaceSearchHighlightBesideRunes bounds the row's line a read of
	// highlights names, longer than any row's line (workspace_search_passage.go).
	workspaceSearchHighlightBesideRunes = 1024
)

// readWorkspaceSearchHighlights lists the lines of a found page that hold its
// keywords, for the highlights shown beside the results, as Notion's search
// does (seen 2026-09-30): up to 14, the heaviest first
// (workspace_search_weight.go) and the rest in reading order, leaving out
// the line the result's row shows, given as "line" (seen 2026-11-11: beside
// "w398 many 01 zintharo" its lines 02 to 15 of 40; beside line 30 of
// another, lines 01 to 14; 2026-11-14: for "korvan delmire", "korvan
// delmire together" before "korvan alone first", which comes first in the
// page, and for "lumbrask pevrillo", "lumbrask then pevrillo apart" before
// "lumbrask pevrillo joined", which comes first). A page search
// wouldn't show the reader has none, and a sub-page's line closed to the
// reader is none of them (workspace_search_reader.go).
func readWorkspaceSearchHighlights(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	pageID := strings.TrimSpace(event.Request.URL.Query().Get("pageId"))
	query := strings.TrimSpace(event.Request.URL.Query().Get("q"))
	beside := strings.TrimSpace(event.Request.URL.Query().Get("line"))
	if reason := workspaceSearchTextsRefusal(workspaceID != "" && pageID != "", query, true, "line", beside, workspaceSearchHighlightBesideRunes); reason != "" {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSearchInvalid, refusalMessage("Check the search query.", reason))
	}
	authorization, err := authorizeWorkspaceReader(event.App, user.Id, workspaceID)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "highlights-authorize",
			errorCodeWorkspaceSearchFailed, "Couldn't search.",
		)
	}
	visible, err := filterWorkspaceSearchCandidates(event.App, user.Id, []workspaceSearchCandidate{{PageID: pageID}})
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "highlights-access",
			errorCodeWorkspaceSearchFailed, "Couldn't search.",
		)
	}
	response := workspaceSearchHighlightsResponse{Highlights: []string{}}
	if len(visible) == 0 {
		return event.JSON(http.StatusOK, response)
	}
	entry := struct {
		Body  string `db:"body"`
		Title string `db:"title"`
		// Named is whether the page is a wiki or a full-page database, found
		// by its database's name.
		Named bool `db:"named"`
	}{}
	err = event.App.DB().NewQuery(`
		SELECT [[entry]].[[body]], [[entry]].[[title]], IFNULL([[page]].[[kind]], '') = {:database} OR EXISTS (
			SELECT 1 FROM {{leafen_data_sources}} [[source]] WHERE [[source]].[[page_id]] = [[entry]].[[page_id]] AND [[source]].[[wiki]] = 1
		) AS [[named]]
		FROM {{leafen_search_pages}} [[entry]] LEFT JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[entry]].[[page_id]]
		WHERE [[entry]].[[page_id]] = {:pageId} AND [[entry]].[[workspace_id]] = {:workspaceId} AND [[entry]].[[owner_id]] = {:ownerId}
	`).Bind(dbx.Params{
		"pageId": pageID, "workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID,
		"database": pageKindDatabase,
	}).One(&entry)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "highlights-body",
			errorCodeWorkspaceSearchFailed, "Couldn't search.",
		)
	}
	body := entry.Body
	// A wiki or a full-page database whose name holds every keyword has no
	// line in its row (searchWorkspace), nor highlights: its preview shows
	// the page itself, as Notion's does (seen 2026-11-11: "vorquen" beside
	// the wiki "W383 Vorquen wikitwo" shows its home, not its lines holding
	// "vorquen").
	titleAll, _ := workspaceSearchMatchFor(foldSearchText(query)).Keywords.match(entry.Title)
	if err == nil && entry.Named && titleAll {
		return event.JSON(http.StatusOK, response)
	}
	plainQuery := workspaceSearchPlainText(foldSearchText(query))
	// Nor, beside a page whose title holds every keyword, its sub-pages'
	// titles: as its row shows none (searchPassageWithoutSubPages), its
	// preview shows the page itself where they were all it had, as Notion's
	// (seen 2026-11-14: "w925qx" beside "w925qx 부모 가" shows its
	// paragraph "평범한 문단", not its sub-page "w925qx 자식 가").
	answer := func(highlights []string) error {
		if titleAll && len(highlights) > 0 {
			sub, err := searchSubPageTitles(event.App, pageID)
			if err != nil {
				return workspaceSearchInternalError(
					event, err, persistentReadWorkspaceSearch, "highlights-sub-pages",
					errorCodeWorkspaceSearchFailed, "Couldn't search.",
				)
			}
			words, shown := workspaceSearchHighlightWords(plainQuery), map[string]bool{}
			for title := range sub {
				if highlight, weight, _ := workspaceSearchHighlight(title, words); weight.keywords > 0 {
					shown[highlight.shown(false)], shown[highlight.shown(true)] = true, true
				}
			}
			highlights = slices.DeleteFunc(highlights, func(line string) bool { return shown[line] })
		}
		response.Highlights = highlights
		return event.JSON(http.StatusOK, response)
	}
	if err == nil {
		closed, err := workspaceSearchClosedLines(event.App, personReader(user.Id), authorization.WorkspaceID, []string{pageID})
		if err != nil {
			return workspaceSearchInternalError(
				event, err, persistentReadWorkspaceSearch, "highlights-lines",
				errorCodeWorkspaceSearchFailed, "Couldn't search.",
			)
		}
		if lines := closed[pageID]; len(lines) > 0 {
			read, err := workspaceSearchReadAs(event.App, authorization.WorkspaceID, pageID, lines)
			if err != nil {
				return workspaceSearchInternalError(
					event, err, persistentReadWorkspaceSearch, "highlights-read",
					errorCodeWorkspaceSearchFailed, "Couldn't search.",
				)
			}
			return answer(workspaceSearchHighlights(read.Body, plainQuery, beside))
		}
		highlights, chunked, err := workspaceSearchChunkHighlights(event.App, pageID, plainQuery, beside)
		if err != nil {
			return workspaceSearchInternalError(
				event, err, persistentReadWorkspaceSearch, "highlights-body",
				errorCodeWorkspaceSearchFailed, "Couldn't search.",
			)
		}
		if chunked {
			return answer(highlights)
		}
	}
	return answer(workspaceSearchHighlights(body, plainQuery, beside))
}

// workspaceSearchHighlightChunks is how many of a page's chunks a highlight
// read takes at a time.
const workspaceSearchHighlightChunks = 16

// workspaceSearchChunkHighlights picks the highlights of a page indexed in
// chunks as workspaceSearchHighlights does from its whole body, reading its
// chunks in order only until it has them all (2026-10-19: it read every chunk
// of the page, 468 on one typed into for hours, for the first six lines).
// It says whether the page has chunks; one without keeps its body on its row.
func workspaceSearchChunkHighlights(app core.App, pageID, plainQuery, beside string) ([]string, bool, error) {
	picker := newWorkspaceSearchHighlightPicker(plainQuery, beside)
	pending, after, chunked := "", -1, false
	for {
		chunks := []struct {
			No      int    `db:"chunk_no"`
			Body    string `db:"body"`
			MidLine bool   `db:"mid_line"`
		}{}
		if err := app.DB().NewQuery(`SELECT [[chunk_no]], [[body]], [[mid_line]] FROM {{leafen_search_chunks}}
			WHERE [[page_id]] = {:pageId} AND [[chunk_no]] > {:after} ORDER BY [[chunk_no]] LIMIT {:limit}`).
			Bind(dbx.Params{"pageId": pageID, "after": after, "limit": workspaceSearchHighlightChunks}).All(&chunks); err != nil {
			return nil, false, err
		}
		if len(chunks) == 0 {
			break
		}
		chunked = true
		for _, chunk := range chunks {
			// The chunks joined, a line break where a line ended: each line
			// as the whole body splits it.
			lines := strings.Split(pending+chunk.Body, "\n")
			for _, line := range lines[:len(lines)-1] {
				if picker.take(line) {
					return picker.picked(), true, nil
				}
			}
			pending = lines[len(lines)-1]
			if !chunk.MidLine {
				if picker.take(pending) {
					return picker.picked(), true, nil
				}
				pending = ""
			}
			after = chunk.No
		}
	}
	if pending != "" {
		picker.take(pending)
	}
	return picker.picked(), chunked, nil
}

// workspaceSearchHighlightPicker picks a page's highlights a line at a time.
type workspaceSearchHighlightPicker struct {
	terms []string
	// beside is the line the result's row shows, whose own line is left
	// out once: the first holding it, as its row's line is a part of its
	// line (workspace_search_passage.go).
	beside string
	// lines are the highlights found, in reading order, each with its
	// line's weight; whole is how many hold all the keywords.
	lines []workspaceSearchHighlightLine
	whole int
}

type workspaceSearchHighlightLine struct {
	highlight workspaceSearchHighlightText
	weight    workspaceSearchWeight
	// outweighed is whether a later line of the page weighs more.
	outweighed bool
}

func newWorkspaceSearchHighlightPicker(plainQuery, beside string) *workspaceSearchHighlightPicker {
	return &workspaceSearchHighlightPicker{terms: workspaceSearchHighlightWords(plainQuery), beside: beside}
}

// take adds a line's highlight if it holds a keyword, and says whether the
// page is read far enough: as many lines holding every keyword as are
// shown. A later line may weigh more yet, by keywords held again; a long
// page is not read past them all the same (2026-10-19: one typed into for
// hours had 468 chunks).
func (picker *workspaceSearchHighlightPicker) take(line string) bool {
	if picker.whole == maxWorkspaceSearchHighlights {
		return true
	}
	line = searchLineShown(line)
	highlight, weight, held := workspaceSearchHighlight(line, picker.terms)
	if held == 0 {
		return false
	}
	// The lines before it that weigh less, the row's own line's too, are
	// outweighed by a later line.
	for index := range picker.lines {
		if weight.compare(picker.lines[index].weight) < 0 {
			picker.lines[index].outweighed = true
		}
	}
	if picker.beside != "" && strings.Contains(line, picker.beside) {
		picker.beside = ""
		return false
	}
	picker.lines = append(picker.lines, workspaceSearchHighlightLine{highlight: highlight, weight: weight})
	if held == len(picker.terms) {
		picker.whole++
	}
	// Only the lines that may yet be shown are kept.
	if len(picker.lines) > 2*maxWorkspaceSearchHighlights {
		picker.lines = picker.ranked()
	}
	return picker.whole == maxWorkspaceSearchHighlights
}

// ranked is the lines that are shown: the heaviest first, in reading order
// among those of a weight.
func (picker *workspaceSearchHighlightPicker) ranked() []workspaceSearchHighlightLine {
	lines := slices.Clone(picker.lines)
	slices.SortStableFunc(lines, func(a, b workspaceSearchHighlightLine) int { return a.weight.compare(b.weight) })
	return lines[:min(len(lines), maxWorkspaceSearchHighlights)]
}

// picked is the highlights shown.
func (picker *workspaceSearchHighlightPicker) picked() []string {
	highlights := []string{}
	for _, line := range picker.ranked() {
		highlights = append(highlights, line.highlight.shown(line.outweighed))
	}
	return highlights
}

// workspaceSearchHighlights picks the lines holding a keyword — a block's
// own lines joined, as a result's line joins them
// (workspace_search_passage.go) — each the part of it its first keyword
// shows (workspaceSearchHighlightWindow). As Notion's do, a highlight starts
// with "..." unless a keyword starts it, and ends with them unless a keyword
// ends it, of a text line, a sub-page's or a wiki page's alike (seen
// 2026-11-11: "...w388 tail brindavo", "...W388 sub Brindavo", "brindavo"
// and "talquiren c001 … c034...", but "...w398 line 02 zintharo" and
// "...w388 dot brindavo...."); a line shown to its end has them a space
// after it when a later line of the page weighs more, the row's own line
// too, whatever it ends with (seen 2026-11-14 on fourteen synthetic pages:
// "...a1 first qeltharn line before ..." and "...p1 qeltharn ends with
// dot. ..." before the row's heavier line, "...e3 qeltharn light after
// ..." after it but before "e4 qeltharn and qeltharn medium after", which
// shows "...e4 qeltharn and qeltharn medium after..." with none heavier
// after it, as "...a4 third qeltharn line after..." and "...p5 qeltharn
// after dot...."; for "qeltharn vossimer", "...q1 qeltharn only here ..."
// before "q2 qeltharn vossimer joined", heavier by its keywords); a cut
// one has none ("... n018...").
func workspaceSearchHighlights(body, plainQuery, beside string) []string {
	picker := newWorkspaceSearchHighlightPicker(plainQuery, beside)
	for _, line := range strings.Split(body, "\n") {
		if picker.take(line) {
			break
		}
	}
	return picker.picked()
}

// workspaceSearchHighlightText is a highlight but for the "..." after it,
// which may depend on the lines after its own.
type workspaceSearchHighlightText struct {
	text string
	// open is whether "..." follow it: it ends where no keyword does; atEnd
	// whether at its line's end.
	open, atEnd bool
}

// shown is the highlight as shown, its line outweighed by a later one or
// not.
func (highlight workspaceSearchHighlightText) shown(outweighed bool) string {
	switch {
	case !highlight.open:
		return highlight.text
	case highlight.atEnd && outweighed:
		return highlight.text + " " + workspaceSearchHighlightCut
	default:
		return highlight.text + workspaceSearchHighlightCut
	}
}

// workspaceSearchHighlight is a line's highlight, its weight
// (workspace_search_weight.go) and how many of the keywords it holds, none
// when it holds none (workspaceSearchKeywordsFound).
func workspaceSearchHighlight(line string, words []string) (workspaceSearchHighlightText, workspaceSearchWeight, int) {
	runes := []rune(line)
	found := workspaceSearchKeywordsFound(runes, words)
	if len(found) == 0 {
		return workspaceSearchHighlightText{}, workspaceSearchWeight{}, 0
	}
	places := make([][2]int, 0, len(found))
	seen := map[int]bool{}
	for _, at := range found {
		places = append(places, at.place)
		seen[at.word] = true
	}
	held := len(seen)
	start, end := workspaceSearchHighlightWindow(runes, places[0])
	highlight := workspaceSearchHighlightText{text: string(runes[start:end])}
	if !workspaceSearchAtKeyword(places, start, 0) {
		highlight.text = workspaceSearchHighlightCut + highlight.text
	}
	if !workspaceSearchAtKeyword(places, end, 1) {
		highlight.open = true
		highlight.atEnd = strings.TrimSpace(string(runes[end:])) == ""
	}
	return highlight, workspaceSearchWeigh(runes, found, len(words)), held
}

// workspaceSearchAtKeyword is whether a highlight's edge, its start (0) or
// its end (1), is a keyword's: "Morvenal4291" for "morvenal" ends with none
// (seen 2026-11-11: "...w398 e Morvenal4291...").
func workspaceSearchAtKeyword(places [][2]int, at, edge int) bool {
	for _, place := range places {
		if place[edge] == at {
			return true
		}
	}
	return false
}
