package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The workspace owner's list of the pages in their 30 days after a permanent
// deletion (보안 › 데이터 보존), and bringing one back or deleting it at once,
// name and reach only the pages the owner reads as a person
// (owner_listed_pages.go).

// deletedPageLinesReadPerRequest is how many lines one request of the list
// reads at most, past those the reader does not read: a workspace whose held
// pages are mostly a private teamspace's its owner has not joined answers
// each request at once and goes on with the next, where reading on to a full
// window read every held line in one request.
const deletedPageLinesReadPerRequest = 5 * pageDeletionHoldListWindow

type storedDeletedPageLine struct {
	PageID    string `db:"page_id"`
	Title     string `db:"title"`
	Icon      string `db:"icon"`
	Row       bool   `db:"row"`
	ActorName string `db:"actor_name"`
	RequestID string `db:"request_id"`
	HeldAt    int64  `db:"held_at"`
	PurgeAt   int64  `db:"purge_at"`
	LegalHold bool   `db:"legal_hold"`
}

// readDeletedPages gives the owner a window of the workspace's pages in their
// 30 days that they read, newest first; nextCursor goes on from the last, ""
// once all are read.
func readDeletedPages(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner); err != nil {
		return pagePermanentDeleteError(event, err, "deleted-pages-authorize")
	}
	after, err := decodeTimeIDCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePagePermanentDeleteInvalid, "Give the nextCursor of the last list, or none to start.")
	}
	rows, next, err := readDeletedPageLines(event.App, user.Id, workspaceID, after)
	if err != nil {
		return pagePermanentDeleteError(event, errors.Join(errPagePermanentDeleteUnavailable, err), "deleted-pages")
	}
	response := pageDeletedPagesResponse{Pages: make([]pageDeletedPageRecord, 0, len(rows)), NextCursor: next}
	for _, row := range rows {
		title := row.Title
		if row.Row {
			title = databaseRowTitleText(row.Title)
		}
		actorName := row.ActorName
		// The trash's own 30 days deleted it, not the person it ran as.
		if strings.HasPrefix(row.RequestID, pageTrashRetentionRequestPrefix) {
			actorName = ""
		}
		response.Pages = append(response.Pages, pageDeletedPageRecord{
			PageID: row.PageID, Title: title, Icon: row.Icon, Row: row.Row, DeletedByName: actorName,
			DeletedAt: row.HeldAt, PurgeAt: row.PurgeAt, LegalHold: row.LegalHold,
		})
	}
	// Each title as its reader reads it, and its spans while it holds a
	// mention, for a plain title to say each one (page_title_spans.go).
	titles := make(map[string]*string, len(response.Pages))
	for index := range response.Pages {
		if !response.Pages[index].Row {
			titles[response.Pages[index].PageID] = &response.Pages[index].Title
		}
	}
	pageTitlesAsRead(event.App, user.Id, titles, true)
	spans := pageTitlesSpansAsRead(event.App, user.Id, titles)
	for index := range response.Pages {
		response.Pages[index].TitleSpans = spans[response.Pages[index].PageID]
	}
	return event.JSON(http.StatusOK, response)
}

// readDeletedPageLines reads a window of the pages in their 30 days that the
// reader reads, from the cursor on, reading on past those they don't until
// the window is full, none are left or the request has read
// deletedPageLinesReadPerRequest lines; next goes on from the last line read,
// so a window may come short, or empty, with more to come. Each window's
// lines are judged together (owner_listed_pages.go).
func readDeletedPageLines(
	app core.App, readerID, workspaceID string, after timeIDCursor,
) ([]storedDeletedPageLine, string, error) {
	app = withAccessMemo(app)
	lines := []storedDeletedPageLine{}
	for read := 0; ; {
		rows := []storedDeletedPageLine{}
		if err := app.DB().NewQuery(`
			SELECT [[kept]].[[page_id]], [[page]].[[icon]], COALESCE([[actor]].[[name]], '') AS [[actor_name]],
				[[page]].[[data_source_id]] != '' AS [[row]],
				CASE WHEN [[page]].[[data_source_id]] != '' THEN COALESCE((
					SELECT [[value]].[[value]] FROM {{leafen_database_row_values}} AS [[value]]
					JOIN {{leafen_database_properties}} AS [[property]]
						ON [[property]].[[id]] = [[value]].[[property_id]] AND [[property]].[[type]] = 'title'
					WHERE [[value]].[[row_id]] = [[kept]].[[page_id]] LIMIT 1), '') ELSE [[page]].[[title]] END AS [[title]],
				[[kept]].[[request_id]], [[kept]].[[held_at]], [[kept]].[[purge_at]],
				` + legalHoldRetainsPageSQL("[[kept]].[[page_id]]", "[[kept]].[[workspace_id]]") + ` AS [[legal_hold]]
			FROM {{leafen_page_deletion_holds}} AS [[kept]]
			JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[kept]].[[page_id]]
			LEFT JOIN {{users}} AS [[actor]] ON [[actor]].[[id]] = [[kept]].[[actor_id]]
			WHERE [[kept]].[[workspace_id]] = {:workspaceId}
				AND ({:afterId} = '' OR [[kept]].[[held_at]] < {:afterAt}
					OR ([[kept]].[[held_at]] = {:afterAt} AND [[kept]].[[page_id]] > {:afterId}))
			ORDER BY [[kept]].[[held_at]] DESC, [[kept]].[[page_id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"workspaceId": workspaceID, "afterAt": after.At, "afterId": after.ID, "limit": pageDeletionHoldListWindow,
		}).All(&rows); err != nil {
			return nil, "", err
		}
		pageIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			pageIDs = append(pageIDs, row.PageID)
		}
		reads, err := listedPagesPersonReads(app, readerID, pageIDs)
		if err != nil {
			return nil, "", err
		}
		for _, row := range rows {
			if reads[row.PageID] && len(lines) == pageDeletionHoldListWindow {
				// A line past the full window: the next window starts at it.
				return lines, encodeTimeIDCursor(after.At, after.ID), nil
			}
			if reads[row.PageID] {
				lines = append(lines, row)
			}
			after = timeIDCursor{At: row.HeldAt, ID: row.PageID}
		}
		if len(rows) < pageDeletionHoldListWindow {
			return lines, "", nil
		}
		if read += len(rows); read >= deletedPageLinesReadPerRequest {
			return lines, encodeTimeIDCursor(after.At, after.ID), nil
		}
	}
}

// deletedPageRequest authorizes the workspace's owner over one page in its 30
// days that they read and answers with the page.
func deletedPageRequest(txApp core.App, actorID, workspaceID, pageID string) (storedPageDeletionHold, error) {
	if _, err := authorizeWorkspace(txApp, actorID, workspaceID, permissionWorkspaceOwner); err != nil {
		return storedPageDeletionHold{}, err
	}
	hold, err := loadPageDeletionHold(txApp, pageID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && hold.WorkspaceID != workspaceID) {
		return storedPageDeletionHold{}, deletedPageNotHeld(txApp, workspaceID, pageID)
	}
	if err != nil {
		return storedPageDeletionHold{}, err
	}
	if reads, err := personReadsListedPage(withAccessMemo(txApp), actorID, pageID); err != nil || !reads {
		return storedPageDeletionHold{}, deniedUnless(err)
	}
	return hold, nil
}
