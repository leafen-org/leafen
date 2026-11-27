package app

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A failed or canceled import's staging pages, never shown and kept only so
// the import could be retried, go first when retention retires it
// (2026-10-16: three failed imports left 53 staging pages for good, and the
// ID runner retried one of their databases every few minutes). A page goes
// as one emptied from the trash does: its residue lane by lane, then the
// page. Only pages the import made after it started go; one older than the
// import that it staged again stays.

const notionImportRetentionPhaseStaging = "staging"

// advanceNotionImportRetentionStaging takes one step: a lane of the current
// page, the page itself, or, with none left, the next phase.
func advanceNotionImportRetentionStaging(
	ctx context.Context,
	app core.App,
	run storedNotionImportRetentionRun,
) (string, string, int, error) {
	pageID, lane, err := notionImportStagingCursor(run.AfterKey)
	if err != nil {
		return "", "", 0, err
	}
	if pageID == "" {
		if pageID, err = nextNotionImportStagingPage(ctx, app, run.JobID); err != nil {
			return "", "", 0, err
		}
		if pageID == "" {
			return notionImportRetentionNextPhase(notionImportRetentionPhaseStaging), "", 0, nil
		}
		lane = 0
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return notionImportRetentionPhaseStaging, "", 0, nil
	}
	if err != nil {
		return "", "", 0, err
	}
	// A page shown since goes on being shown: nothing more of it is taken.
	if !page.GetBool("notion_import_staging") {
		return notionImportRetentionPhaseStaging, "", 0, nil
	}
	if lane < pagePermanentDeleteCleanupLaneCount {
		more, err := cleanPagePermanentDeleteResidueLane(app, pageID, lane)
		if err != nil {
			return "", "", 0, err
		}
		if !more {
			lane++
		}
		return notionImportRetentionPhaseStaging, pageID + ":" + strconv.Itoa(lane), 0, nil
	}
	if err := app.Delete(page); err != nil {
		return "", "", 0, err
	}
	return notionImportRetentionPhaseStaging, "", 1, nil
}

func notionImportStagingCursor(afterKey string) (string, int, error) {
	if afterKey == "" {
		return "", 0, nil
	}
	pageID, rawLane, found := strings.Cut(afterKey, ":")
	lane, err := strconv.Atoi(rawLane)
	if !found || pageID == "" || err != nil || lane < 0 || lane > pagePermanentDeleteCleanupLaneCount {
		return "", 0, failedBecause(errNotionImportRetentionUnavailable, err)
	}
	return pageID, lane, nil
}

// nextNotionImportStagingPage names the next staging page the import made
// after it started, or "" when none is left.
func nextNotionImportStagingPage(ctx context.Context, app core.App, jobID string) (string, error) {
	var created int64
	if err := app.DB().NewQuery(`SELECT [[created]] FROM {{leafen_jobs}} WHERE [[id]]={:jobId}`).
		WithContext(ctx).Bind(dbx.Params{"jobId": jobID}).Row(&created); err != nil {
		return "", err
	}
	var pageID string
	err := app.DB().NewQuery(`
		SELECT [[mapping]].[[native_id]] FROM {{leafen_notion_import_mappings}} [[mapping]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[mapping]].[[native_id]]
		WHERE [[mapping]].[[job_id]] = {:jobId} AND [[mapping]].[[kind]] IN ('page','row')
			AND [[page]].[[notion_import_staging]] = TRUE AND [[page]].[[created]] >= {:started}
		ORDER BY [[mapping]].[[native_id]] LIMIT 1
	`).WithContext(ctx).Bind(dbx.Params{
		"jobId": jobID, "started": time.UnixMilli(created).UTC().Format("2006-01-02 15:04:05.000Z"),
	}).Row(&pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return pageID, err
}
