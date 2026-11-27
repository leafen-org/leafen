package app

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// What a workspace's trash holds, as its window lists it, for a reader
// without the screen — an agent emptying the trash or bringing something
// back: GET /workspaces/{workspaceId}/trash. Its lines are the trash
// window's (web AuthenticatedTrashView and TrashView), from the same reads
// the window makes — the pages the start-up read gives the reader
// (readReaderBootstrapPages), the database pages in the trash
// (listDatabaseRowTrash) and the databases there (readDeletedDatabaseWindow)
// — by the window's rule, before its filters and search:
//
//   - a page in the trash, and each page under one there that the reader
//     reads, which went with it (Notion's trash lists a sub-page beside the
//     page it went with, seen 2026-11-12), less a full-page database's page
//     left as a signpost;
//   - a database page in the trash, a page of a data source in the trash
//     among them; one the page list holds too (a wiki's page) once, as the
//     database page;
//   - a database in the trash, below the page holding it.
//
// They come the latest first, as the window orders them: by when each went
// there — one that went with what holds it at its own last edit — then the
// one higher up first. A line's id is what notion-restore-pages and the
// trash's restore and delete operations take; its lastEditedBy is what the
// window's "last edited by" filter, the reader's own at first, reads. Who may
// read the list reads it as the window: a reader of the workspace, each
// line one they may see (a database one whose page they may edit).
//
// Notion's MCP has no such read (its tool pages, 2026-11-26: delete and
// restore only); its public API finds what is in the trash with its search
// (filter in_trash, notion_search_api.go), the trash's own items alone. This
// one lists the window's lines, beside the trash's other routes (trash/empty,
// trash/legal-holds), in their words.

func registerPageTrashListRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+pageTrashListPath, readPageTrashList)
}

func readPageTrashList(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	after, err := decodeTimeIDCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePagePermanentDeleteInvalid, "Give the nextCursor of the last list, or none to start.")
	}
	lines, err := readTrashLines(event.Request.Context(), event.App, user.Id, workspaceID)
	switch {
	case isPermissionDenied(err):
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	case err != nil:
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePagePermanentDeleteUnavailable,
			"Couldn't read the trash.", "page_trash", "list", errorCodePagePermanentDeleteUnavailable, err)
	}
	items, next := trashLinesWindow(lines, after)
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, pageTrashListResponse{Items: items, NextCursor: next})
}

// trashLineEntry is a line of the trash with where it stands in the window's
// order: the moment it is placed at and how deep it sits.
type trashLineEntry struct {
	line   pageTrashLine
	moment int64
	depth  int
}

// readTrashLines reads a workspace's trash, every line, as its reader's
// window lists it.
func readTrashLines(ctx context.Context, app core.App, principalID, workspaceID string) ([]trashLineEntry, error) {
	if _, err := authorizeWorkspaceReader(app, principalID, workspaceID); err != nil {
		return nil, err
	}
	read, err := readReaderBootstrapPages(ctx, app, principalID)
	if err != nil {
		return nil, err
	}
	// The database pages: none for a reader the list doesn't open to (a
	// guest), whose window shows none.
	rows := []databaseRowTrashItem{}
	for cursor := ""; ; {
		window, next, err := listDatabaseRowTrashFor(ctx, app, principalID, workspaceID, cursor)
		if isPermissionDenied(err) {
			rows = nil
			break
		}
		if err != nil {
			return nil, err
		}
		rows = append(rows, window...)
		if cursor = next; next == "" {
			break
		}
	}
	// The databases, those past their days gone first, as their list's read does.
	if err := purgeExpiredDeletedDatabases(app, time.Now().UTC().UnixMilli()); err != nil {
		return nil, err
	}
	databases := []deletedDatabase{}
	for cursor := ""; ; {
		window, next, err := readDeletedDatabaseWindow(app, principalID, workspaceID, cursor)
		if err != nil {
			return nil, err
		}
		databases = append(databases, window...)
		if cursor = next; next == "" {
			break
		}
	}
	return trashLinesOf(workspaceID, read.Pages, rows, databases), nil
}

// trashLinesOf applies the window's rule to what it reads: the pages the
// reader sees, the database pages and the databases in the workspace's
// trash.
func trashLinesOf(workspaceID string, pages []pageResponse, rows []databaseRowTrashItem, databases []deletedDatabase) []trashLineEntry {
	listedAsRow := make(map[string]bool, len(rows))
	for _, row := range rows {
		listedAsRow[row.RowID] = true
	}
	byID := make(map[string]pageResponse, len(pages))
	for _, page := range pages {
		byID[page.ID] = page
	}
	// What each line sits under, for how deep it is: a database page under
	// its database's page.
	parentOf := make(map[string]string, len(pages)+len(rows))
	for _, page := range pages {
		if !listedAsRow[page.ID] {
			parentOf[page.ID] = page.ParentID
		}
	}
	for _, row := range rows {
		parentOf[row.RowID] = row.ContainerPageID
	}
	depthOf := func(id string) int {
		depth, seen := 0, map[string]bool{}
		for above := parentOf[id]; above != "" && !seen[above]; above = parentOf[above] {
			seen[above] = true
			depth++
		}
		return depth
	}
	// The page in the trash a page went there with: itself or the nearest
	// page above it there (web pageTrashRoot).
	trashRoot := func(id string) (pageResponse, bool) {
		for seen := map[string]bool{}; id != "" && !seen[id]; {
			seen[id] = true
			page, known := byID[id]
			if !known {
				break
			}
			if page.InTrash {
				return page, true
			}
			id = page.ParentID
		}
		return pageResponse{}, false
	}
	entries := []trashLineEntry{}
	for _, page := range pages {
		if page.WorkspaceID != workspaceID || page.MovedDatabase || listedAsRow[page.ID] {
			continue
		}
		root, trashed := trashRoot(page.ID)
		if !trashed {
			continue
		}
		line := pageTrashLine{ID: page.ID, Kind: pageTrashLinePage, Title: page.Title,
			TrashedAt: trashLineTime(trashLineMillis(optionalText(root.TrashedAt))),
			ParentID:  page.ParentID, LastEditedBy: cmp.Or(page.LastEditedBy, page.CreatedBy)}
		at := optionalText(page.TrashedAt)
		if !page.InTrash {
			line.TrashedWithPageID, at = root.ID, cmp.Or(page.UpdatedAt, optionalText(root.TrashedAt))
		}
		entries = append(entries, trashLineEntry{line: line, moment: trashLineMillis(at), depth: depthOf(page.ID)})
	}
	for _, row := range rows {
		entries = append(entries, trashLineEntry{line: pageTrashLine{
			ID: row.RowID, Kind: pageTrashLineDatabasePage, Title: row.Title, TrashedAt: row.TrashedAt,
			ParentID: row.ContainerPageID, LastEditedBy: row.TrashedBy,
		}, moment: trashLineMillis(cmp.Or(row.EditedAt, row.TrashedAt)), depth: depthOf(row.RowID)})
	}
	// The pages in the order the window first puts them, the latest first and
	// then by ID; the databases after them, in their list's order.
	slices.SortStableFunc(entries, func(left, right trashLineEntry) int {
		return cmp.Or(cmp.Compare(right.moment, left.moment), strings.Compare(left.line.ID, right.line.ID))
	})
	for _, item := range databases {
		line := pageTrashLine{ID: item.BlockID, Kind: pageTrashLineDatabase, Title: item.Title,
			TrashedAt: trashLineTime(item.DeletedAt), ParentID: item.PageID}
		if root, trashed := trashRoot(item.PageID); item.InTrashedPage && trashed {
			line.TrashedWithPageID = root.ID
		}
		entries = append(entries, trashLineEntry{line: line, moment: cmp.Or(item.EditedAt, item.DeletedAt), depth: depthOf(item.PageID) + 1})
	}
	slices.SortStableFunc(entries, func(left, right trashLineEntry) int {
		return cmp.Or(cmp.Compare(right.moment, left.moment), cmp.Compare(left.depth, right.depth))
	})
	return entries
}

// trashLinesWindow is one answer's lines from the cursor on, and the cursor
// of the next when more follow: after the line the last answer ended with,
// or, when that line has left the trash since, after where it stood.
func trashLinesWindow(entries []trashLineEntry, after timeIDCursor) ([]pageTrashLine, string) {
	start := 0
	if after.ID != "" {
		start = len(entries)
		for index, entry := range entries {
			if entry.line.ID == after.ID {
				start = index + 1
				break
			}
			if entry.moment < after.At {
				start = min(start, index)
			}
		}
	}
	entries = entries[start:]
	next := ""
	if len(entries) > pageTrashListWindow {
		entries = entries[:pageTrashListWindow]
		last := entries[len(entries)-1]
		next = encodeTimeIDCursor(last.moment, last.line.ID)
	}
	lines := make([]pageTrashLine, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, entry.line)
	}
	return lines, next
}

// trashLineMillis is a stored time in milliseconds, to the nearest one, as
// the window reads it (web trashLineMoment): written with a "T" or, as a
// page's last edit is, a space; 0 when it isn't one.
func trashLineMillis(at string) int64 {
	at = strings.TrimSpace(at)
	if len(at) > 10 && at[10] == ' ' {
		at = at[:10] + "T" + at[11:]
	}
	parsed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return 0
	}
	return parsed.Round(time.Millisecond).UnixMilli()
}

// trashLineTime writes a line's time as the trash's other lists do; none
// for 0.
func trashLineTime(millis int64) string {
	return databaseRowTrashTime(millis)
}

// optionalText is an optional text's value, "" for none.
func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
