package app

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Whether a page an app lists (page_children_routes.go) has pages under it
// is whether the person's sidebar lists one there (sidebarLeavesOut). Its
// sub-pages are read in the sidebar's order a window at a time, and further
// only while every one read is left out, so a listing reads a few a page
// however many are under it: reading every one made a request's reads,
// memory and JSON grow with the sum of the listed pages' sub-pages
// (2026-11-18: listing a workspace's top of eleven pages, two of them
// holding 5,000, handed the sidebar's rule 5,217 pages; a window of four
// hands it 27).

const (
	// pageChildWindow is how many of each listed page's sub-pages its list's
	// query reads.
	pageChildWindow = 4
	// pageChildWindowFurther is how many more of a page whose first window
	// was all read are read next, four times as many each time after: the
	// pages left out mostly come many together (a table's row pages, a
	// button's templates).
	pageChildWindowFurther = 64
	// pageChildRoundBudget is the most sub-pages one read further takes for
	// all the pages it looks under.
	pageChildRoundBudget = 4 * pageChildrenMax
)

// pageChildWindowSQL selects, in the sidebar's order, the IDs of up to
// size (an SQL expression) pages right under a page of workspace
// {:workspaceId} (parent, an SQL expression) that aren't in the trash or
// archived and that kept, a condition on [[child]], keeps — those after the
// page whose ID after, an SQL expression, gives, when it isn't empty. The
// index of each parent's pages by position reads them in that order and
// stops at the window.
func pageChildWindowSQL(parent, after, kept, size string) string {
	cursor := ""
	if after != "" {
		cursor = `
			AND ([[child]].[[position]], [[child]].[[id]]) > (
				SELECT [[after_child]].[[position]], [[after_child]].[[id]] FROM {{leafen_pages}} [[after_child]]
				WHERE [[after_child]].[[id]] = ` + after + `)`
	}
	return `
		SELECT [[child]].[[id]] FROM {{leafen_pages}} [[child]] INDEXED BY [[` + pagePositionIndex + `]]
		WHERE [[child]].[[workspace]] = {:workspaceId} AND [[child]].[[parent_id]] = ` + parent + cursor + `
			AND [[child]].[[in_trash]] = FALSE AND [[child]].[[archived_at]] = ''
			AND ` + kept + `
		ORDER BY [[child]].[[position]], [[child]].[[id]]
		LIMIT ` + size
}

// pageChildWindowListSQL is the first window of the pages under each page a
// list reads (parent, its ID as an SQL expression), as a JSON list in a
// column of its query.
func pageChildWindowListSQL(parent, kept string) string {
	return `(SELECT json_group_array([[child_window]].[[id]]) FROM (` +
		pageChildWindowSQL(parent, "", kept, "{:childWindow}") + `) [[child_window]])`
}

// pageChildWindowsAfterSQL selects, for each [page, after] pair in the JSON
// list {:pending}, by its place there, the window of up to {:childRead}
// pages under that page after that one, as a JSON list.
func pageChildWindowsAfterSQL(kept string) string {
	return `
		SELECT [[pending]].[[key]] AS [[slot]], (SELECT json_group_array([[child_window]].[[id]]) FROM (` +
		pageChildWindowSQL(`json_extract([[pending]].[[value]], '$[0]')`, `json_extract([[pending]].[[value]], '$[1]')`, kept, "{:childRead}") +
		`) [[child_window]]) AS [[child_ids]]
		FROM json_each({:pending}) [[pending]]`
}

// ownPageChildKeptSQL keeps of the pages under one of the person's own those
// their sidebar lists by where they are (bootstrapPageListedSQL), the
// workspace owner's and not being imported, as the list keeps its pages.
var ownPageChildKeptSQL = `[[child]].[[notion_import_staging]] = FALSE
			AND [[child]].[[owner]] = (SELECT [[child_workspace]].[[owner]] FROM {{leafen_workspaces}} [[child_workspace]]
				WHERE [[child_workspace]].[[id]] = {:workspaceId})
			AND ` + bootstrapPageListedSQL("[[child]]", "{:principalId}")

// sharedPageChildKeptSQL keeps, after bootstrapSharedBasesSQL, of the pages
// under one the person opens only through shares those they open so too,
// as the walk of bootstrapSharedWalkSQL reaches them without walking every
// page shared with the person: a page the walk starts from, or one it
// reaches from the page above, which it reaches when inShared (an SQL
// expression) holds.
func sharedPageChildKeptSQL(inShared string) string {
	return `([[child]].[[id]] IN (SELECT [[id]] FROM [[shared_base]])
			OR (` + inShared + ` AND ` + bootstrapSharedStepSQL("[[child]]") + `))`
}

// pageChildKeptSQL is kept, a condition on the pages under a page
// ([[child]]), keeping only those the sidebar's rule doesn't leave out for
// where they hang — unmentioned (loadUnmentionedAnchoredPageSet), or on a
// table row's cells or a caption (loadFieldAnchoredPageSet) — read from the
// page's own columns as the rule reads them, so that a window holds pages
// the rule may still list: a table's row pages before a page's other
// sub-pages filled its windows with pages left out, each costing a read
// further and the rule again (2026-11-18). The rule still decides.
func pageChildKeptSQL(app core.App, kept string) (string, error) {
	column, err := pageAnchorUnmentionedColumn(app)
	if err != nil {
		return "", err
	}
	if column != "FALSE" {
		kept += `
			AND [[child]].` + column + ` = FALSE`
	}
	known, err := pageAnchorsKnown(app)
	if err != nil || !known {
		return kept, err
	}
	return kept + `
			AND NOT EXISTS (SELECT 1 FROM {{leafen_blocks}} [[anchor_block]]
				WHERE [[child]].[[anchor_block_id]] != '' AND [[anchor_block]].[[id]] = [[child]].[[anchor_block_id]]
					AND [[anchor_block]].[[page_id]] = [[child]].[[parent_id]]
					AND ([[anchor_block]].[[type]] NOT IN (` + pageChildTextBlockTypesSQL + `)
						OR [[anchor_block]].[[type]] = '` + graphBlockTypeCode + `'))`, nil
}

// pageChildTextBlockTypesSQL lists the block types whose lines a page hangs
// on (graphTextBlockTypes) as SQL strings.
var pageChildTextBlockTypesSQL = func() string {
	types := make([]string, 0, len(graphTextBlockTypes))
	for kind := range graphTextBlockTypes {
		types = append(types, "'"+strings.ReplaceAll(kind, "'", "''")+"'")
	}
	sort.Strings(types)
	return strings.Join(types, ", ")
}()

// pageChildWaiting is a listed page whose pages read so far may go on past
// them: its place in its list and the last one read.
type pageChildWaiting struct {
	row   int
	after string
}

// keepListedPageChildren keeps of the pages read those the person's sidebar
// lists, each said to have pages under it when the sidebar lists one there:
// one of its first window (ChildIDs) or, while all of those are left out,
// of the windows after (lookFurtherForPageChildren).
func keepListedPageChildren(app core.App, principalID, workspaceID string, rows []pageChildWire) ([]pageChildWire, error) {
	windows := make([][]string, len(rows))
	full := make([]bool, len(rows))
	for index, row := range rows {
		if row.ChildIDs != "" {
			if err := json.Unmarshal([]byte(row.ChildIDs), &windows[index]); err != nil {
				return nil, err
			}
		}
		full[index] = len(windows[index]) == pageChildWindow
	}
	ids := make([]string, 0, len(rows)*(1+pageChildWindow))
	for index, row := range rows {
		ids = append(append(ids, row.ID), windows[index]...)
	}
	leftOut, err := sidebarLeavesOut(app, principalID, ids)
	if err != nil {
		return nil, err
	}
	listed := func(id string) bool { return !leftOut[id] }
	kept := rows[:0]
	waiting := map[bool][]pageChildWaiting{}
	for index, row := range rows {
		if !listed(row.ID) {
			continue
		}
		window := windows[index]
		row.HasChildren = slices.ContainsFunc(window, listed)
		if !row.HasChildren && full[index] {
			waiting[row.Shared] = append(waiting[row.Shared], pageChildWaiting{row: len(kept), after: window[len(window)-1]})
		}
		kept = append(kept, row)
	}
	for _, shared := range []bool{false, true} {
		if err := lookFurtherForPageChildren(app, principalID, workspaceID, shared, kept, waiting[shared]); err != nil {
			return nil, err
		}
	}
	return kept, nil
}

// lookFurtherForPageChildren reads the windows after those read of listed
// pages whose pages read so far are all left out — every such page of the
// person's own, or every one opened through shares, in one read each time —
// until each has one the sidebar lists or none is left.
func lookFurtherForPageChildren(app core.App, principalID, workspaceID string, shared bool, rows []pageChildWire, waiting []pageChildWaiting) error {
	for size := pageChildWindowFurther; len(waiting) > 0; size = min(4*size, pageChildrenMax) {
		read := max(2*pageChildWindow, min(size, pageChildRoundBudget/len(waiting)))
		windows, err := readPageChildWindowsAfter(app, principalID, workspaceID, shared, rows, waiting, read)
		if err != nil {
			return err
		}
		leftOut, err := sidebarLeavesOut(app, principalID, slices.Concat(windows...))
		if err != nil {
			return err
		}
		next := []pageChildWaiting{}
		for slot, page := range waiting {
			window := windows[slot]
			if slices.ContainsFunc(window, func(id string) bool { return !leftOut[id] }) {
				rows[page.row].HasChildren = true
			} else if len(window) == read {
				next = append(next, pageChildWaiting{row: page.row, after: window[len(window)-1]})
			}
		}
		waiting = next
	}
	return nil
}

// readPageChildWindowsAfter reads, for each waiting page, the window of the
// pages under it after the last one read, as its list read its first: under
// the person's own pages, or among those they open only through shares —
// climbing from each page to know whether the walk reaches it.
func readPageChildWindowsAfter(app core.App, principalID, workspaceID string, shared bool, rows []pageChildWire, waiting []pageChildWaiting, size int) ([][]string, error) {
	pending := make([][2]string, len(waiting))
	for slot, page := range waiting {
		pending[slot] = [2]string{rows[page.row].ID, page.after}
	}
	encoded, err := json.Marshal(pending)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{"principalId": principalID, "workspaceId": workspaceID, "pending": string(encoded), "childRead": size}
	kept, prefix := ownPageChildKeptSQL, ""
	if shared {
		kept = sharedPageChildKeptSQL(bootstrapSharedReachedSQL("shared_up", "shared_base", "[[pending]].[[key]]"))
		prefix = bootstrapSharedBasesSQL +
			bootstrapSharedClimbSQL(`SELECT [[key]], json_extract([[value]], '$[0]') FROM json_each({:pending})`)
		params = bootstrapSharedParams(params)
	}
	kept, err = pageChildKeptSQL(app, kept)
	if err != nil {
		return nil, err
	}
	query := prefix + pageChildWindowsAfterSQL(kept)
	read := []struct {
		Slot     int    `db:"slot"`
		ChildIDs string `db:"child_ids"`
	}{}
	if err := app.DB().NewQuery(query).Bind(params).All(&read); err != nil {
		return nil, err
	}
	windows := make([][]string, len(waiting))
	for _, entry := range read {
		if entry.Slot < 0 || entry.Slot >= len(windows) {
			continue
		}
		if err := json.Unmarshal([]byte(entry.ChildIDs), &windows[entry.Slot]); err != nil {
			return nil, err
		}
	}
	return windows, nil
}

// sidebarLeavesOut says which of some pages, read by where they are, the
// person's sidebar leaves out: those they may not open there
// (listedPagesHidden) and those its tree leaves out where they stand
// (sidebarTreeLeftOut).
func sidebarLeavesOut(app core.App, principalID string, pageIDs []string) (map[string]bool, error) {
	hidden, err := listedPagesHidden(app, principalID, pageIDs)
	if err != nil {
		return nil, err
	}
	leftOut, err := sidebarTreeLeftOut(app, pageIDs)
	if err != nil {
		return nil, err
	}
	for id := range hidden {
		leftOut[id] = true
	}
	return leftOut, nil
}
