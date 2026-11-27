package app

import (
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A page showing rows of databases on other pages — the sources its linked
// databases show and those its relations point to — hears when those pages
// change (useExternalGraphRefresh) and reads what their changes changed, not
// its whole graph, when each of them changed only rows, its databases'
// sources, views and properties and its own blocks, and said which, as a page
// reads its own changes (content_graph_changed_rows.go): their blocks are
// nothing to read, a row put in the trash or deleted that row. Before,
// it read its whole graph again for any change there (2026-11-25: one value
// typed in a source of 5,000 rows had the page linking it read its 51 windows
// again).
//
// maxGraphExternalPages (contracts/content-graph-wire.json, which the web's
// graphWireBounds.externalPages shares) is the most other pages a read takes
// the changes of: the web reads its whole graph when more moved on, and asks
// for no more (2026-11-25: a page showing rows of 101 pages that all changed
// between two checks asked for them all, was refused, and asked the same
// every 20 seconds, showing the old values till reopened).

// graphExternalSince is another page the read's page shows rows of and the
// position of its changes the page shows.
type graphExternalSince struct {
	PageID string
	Since  int
}

// parseGraphExternalSince reads external=<page>.<position>,…: nil when the
// query has none, and false when it doesn't read.
func parseGraphExternalSince(raw string) ([]graphExternalSince, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxGraphExternalPages {
		return nil, false
	}
	external := make([]graphExternalSince, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		pageID, position, found := strings.Cut(part, ".")
		since, err := strconv.Atoi(position)
		if _, again := seen[pageID]; !found || pageID == "" || err != nil || since < 0 || again {
			return nil, false
		}
		seen[pageID] = struct{}{}
		external = append(external, graphExternalSince{PageID: pageID, Since: since})
	}
	return external, true
}

// loadContentGraphExternalChangedRows reads the page's rows the changes of
// the other pages it shows rows of changed after the positions given, as of
// the read's moment, and those of them it no longer has (graphPageChangesParts,
// readGraphChangedParts, as for its own changes), with its databases whole
// when those changes changed those; their pages' blocks it doesn't show.
// False when a page given isn't one it shows rows of, or one of those
// changes doesn't say what it changed, or that is too much, and the page is
// read whole. The read's own page changed nothing here: its changedSince is
// its own position.
func loadContentGraphExternalChangedRows(
	app core.App,
	owner, pageID string,
	linkedScope linkedDatabaseReadScope,
	external []graphExternalSince,
) (graphReadResponse, bool, error) {
	shown := map[string]struct{}{}
	for _, page := range linkedScope.sourceContainerPages {
		shown[page] = struct{}{}
	}
	for _, page := range linkedScope.relationSourcePages {
		shown[page] = struct{}{}
	}
	delete(shown, pageID)
	rows := map[string]struct{}{}
	databases := false
	for _, page := range external {
		if _, ok := shown[page.PageID]; !ok {
			return graphReadResponse{}, false, nil
		}
		parts, _, said, err := graphPageChangesParts(app, owner, page.PageID, page.Since)
		if err != nil || !said {
			return graphReadResponse{}, false, err
		}
		databases = databases || parts.Databases
		for _, id := range parts.OnlyRows {
			rows[id] = struct{}{}
		}
		if len(rows) > maxGraphChangedRows {
			return graphReadResponse{}, false, nil
		}
	}
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	response, read, err := readGraphChangedParts(app, owner, pageID, linkedScope, graphChangedParts{OnlyRows: ids, Databases: databases})
	if err != nil || !read {
		return graphReadResponse{}, false, err
	}
	since := response.SnapshotRevision
	response.ChangedSince = &since
	return response, true, nil
}
