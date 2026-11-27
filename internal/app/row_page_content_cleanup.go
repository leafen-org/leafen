package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database page can hold databases of its own. When its row goes, the page
// content kept under the row's ID (its databases, their rows and views, its
// documents and history) goes too, as a permanently deleted page's does: the
// row's deletion queues the page, and the trash retention runner clears it
// with the permanent deletion lanes. The rows of the databases it held queue
// their own pages in turn.

const (
	rowPageContentCleanupTable = "leafen_row_page_content_cleanup"
	// rowPageContentCleanupTurn bounds the lane runs of one runner turn.
	rowPageContentCleanupTurn = 90
)

// wakeRowPageContentCleanup wakes the trash retention runner once the
// change it is part of commits, while the queue holds a page: every deletion
// that can queue one — a row's or a template's, by its trigger — calls it, so
// the page's content goes at once rather than within
// rowPageContentCleanupWait.
func wakeRowPageContentCleanup(app core.App) error {
	var queued bool
	if err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_row_page_content_cleanup}})`).Row(&queued); err != nil {
		return err
	}
	if queued {
		notifyRecoverableBlockTrashRetentionAfterCommit(app)
	}
	return nil
}

// cleanRowPageContentSlice clears a bounded part of one queued database
// page's content and reports whether more is queued.
func cleanRowPageContentSlice(app core.App) (bool, error) {
	return cleanRowPageContentSliceIn(app, "")
}

// cleanRowPageContentSliceIn is cleanRowPageContentSlice for one workspace's
// queue, or all with none: a workspace being deleted clears its own before
// its queue goes (workspace_deletion_cleanup.go).
func cleanRowPageContentSliceIn(app core.App, workspaceID string) (bool, error) {
	queued := struct {
		PageID string `db:"page_id"`
		Lane   int    `db:"lane"`
	}{}
	// The queue's first entry by its queued order, or the workspace's first by
	// its own (idx_leafen_row_page_content_cleanup_workspace_queue), not a walk
	// of the whole queue past other workspaces' entries; and whether any is
	// left.
	pick := `SELECT [[page_id]], [[lane]] FROM {{leafen_row_page_content_cleanup}} ORDER BY [[queued_at]], [[page_id]] LIMIT 1`
	left := `SELECT EXISTS (SELECT 1 FROM {{leafen_row_page_content_cleanup}})`
	if workspaceID != "" {
		pick = `SELECT [[page_id]], [[lane]] FROM {{leafen_row_page_content_cleanup}} WHERE [[workspace_id]] = {:workspace}
			ORDER BY [[queued_at]], [[page_id]] LIMIT 1`
		left = `SELECT EXISTS (SELECT 1 FROM {{leafen_row_page_content_cleanup}} WHERE [[workspace_id]] = {:workspace})`
	}
	err := app.DB().NewQuery(pick).Bind(dbx.Params{"workspace": workspaceID}).One(&queued)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// The row page is gone; a page of the same ID again would not be its.
	var page int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": queued.PageID}).Row(&page); err != nil {
		return false, err
	}
	lane := queued.Lane
	for runs := 0; page == 0 && lane < pagePermanentDeleteCleanupLaneCount && runs < rowPageContentCleanupTurn; runs++ {
		more, err := cleanPagePermanentDeleteResidueLane(app, queued.PageID, lane)
		if err != nil {
			return false, err
		}
		if !more {
			lane++
		}
	}
	if page != 0 || lane >= pagePermanentDeleteCleanupLaneCount {
		_, err = app.DB().NewQuery(`DELETE FROM {{leafen_row_page_content_cleanup}} WHERE [[page_id]] = {:id}`).
			Bind(dbx.Params{"id": queued.PageID}).Execute()
	} else {
		_, err = app.DB().NewQuery(`UPDATE {{leafen_row_page_content_cleanup}} SET [[lane]] = {:lane} WHERE [[page_id]] = {:id}`).
			Bind(dbx.Params{"id": queued.PageID, "lane": lane}).Execute()
	}
	if err != nil {
		return false, err
	}
	var more bool
	err = app.DB().NewQuery(left).Bind(dbx.Params{"workspace": workspaceID}).Row(&more)
	return more, err
}
