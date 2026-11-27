package app

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database template's body holds sub-pages as a page's does, and a page
// made from the template, or the template's copy, takes a copy of each, as
// Notion's does (seen 2026-11-16, its app and its API alike): the copied body
// first holds a link to the sub-page where its block stood
// (readDatabaseRowPageBodyCopy), and the job that copies the body's databases
// (database_row_page_database_copies.go) begins, before them, the sub-pages'
// copies, each a page's copy whose block takes the link's place once it is
// ready (child_page_block_copies.go), and waits for them to be done before it
// goes on. So does a template made from a page's body (an import's template,
// notion_import_templates.go, whose row goes once the job is done). A
// sub-page being copied elsewhere — the same template used a moment before —
// is waited for, as a database's copy is; one that can't be copied keeps the
// link. A database page's own copy still links to its sub-pages.

// rowPageSubPageCopy is a sub-page a body's copy takes as a copy of it: the
// page, and the link to it the copy holds until then.
type rowPageSubPageCopy struct {
	SourcePageID  string `json:"sourcePageId"`
	PlaceholderID string `json:"placeholderId"`
}

// rowPageBodySubPageCopies finds the sub-pages a body's copy takes as
// copies: the links that stand for the source's sub-page blocks, each under
// the ID copied from the page's own (a sub-page's block has its page's ID),
// when the source or the copy is a template and the copy shows
// (rowPageOwnDatabasesCopied).
func rowPageBodySubPageCopies(
	app core.App, sourcePageID string, target *core.Record, body []newRowPageBlock, sourceOf func(string) string,
) ([]rowPageSubPageCopy, error) {
	if !rowPageOwnDatabasesCopied(target) {
		return nil, nil
	}
	if template, err := isDatabaseTemplatePageID(app, sourcePageID); err != nil || !template && !isDatabaseTemplatePageRecord(target) {
		return nil, err
	}
	copies := []rowPageSubPageCopy{}
	for _, block := range body {
		if block.Type != graphBlockTypeLinkToPage {
			continue
		}
		if page, _ := block.Content["childPageId"].(string); page != "" && page == sourceOf(block.ID) {
			copies = append(copies, rowPageSubPageCopy{SourcePageID: page, PlaceholderID: block.ID})
		}
	}
	return copies, nil
}

// rowPageSubPageCopySourceBusy says whether a sub-page is being copied
// elsewhere, which its next copy waits for (admitPageDuplicationWith).
func rowPageSubPageCopySourceBusy(app core.App, pageID string) (bool, error) {
	var busy int
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_pages}} AS [[page]]
			WHERE [[page]].[[id]] = {:pageId} AND ([[page]].[[duplication_source_job_id]] != ''
				OR EXISTS(SELECT 1 FROM {{leafen_page_duplication_jobs}} AS [[copy]]
					WHERE [[copy]].[[owner_id]] = [[page]].[[owner]] AND [[copy]].[[source_root_page_id]] = [[page]].[[id]]
						AND [[copy]].[[phase]] NOT IN ('completed','failed'))))
	`).Bind(dbx.Params{"pageId": pageID}).Row(&busy)
	return busy != 0, err
}

// beginRowPageSubPageCopy begins the next sub-page's copy into the job's
// page; one whose page, or the page it goes into, is held a moment waits its
// turn, and one that can't be copied keeps its link.
func (r *durableJobRunner) beginRowPageSubPageCopy(job storedJob, payload rowPageDatabaseCopyPayload) error {
	app := r.app
	next := payload.SubPages[payload.SubPageNext]
	wait := func() error {
		delay := rowPageDatabaseCopyWait
		if payload.Waits >= rowPageDatabaseCopyQuickWaits {
			delay = rowPageDatabaseCopySlowWait
		}
		payload.Waits++
		return saveRowPageDatabaseCopyJob(app, job, payload, delay, false)
	}
	busy, err := rowPageSubPageCopySourceBusy(app, next.SourcePageID)
	if err != nil {
		return err
	}
	if busy && payload.Waits < rowPageDatabaseCopyMaxWaits {
		return wait()
	}
	copies, err := blockCopyCopiesSubPages(app, job.PageID)
	if err != nil {
		return err
	}
	if copies {
		var admission pageDuplicationAdmission
		admission, err = admitPageDuplicationInPlace(app, payload.ActorID, next.SourcePageID, pageDuplicationInput{
			// Named by the job, so a turn after a stop finds the copy it began.
			RequestID: job.ID + ":page:" + strconv.Itoa(payload.SubPageNext), DestinationParentID: job.PageID,
		}, next.PlaceholderID, "")
		var failure *pageDuplicationFailure
		if (isPermissionDenied(err) || errors.As(err, &failure)) && payload.Waits < rowPageDatabaseCopyMaxWaits {
			// Held by an operation under way (its page's copy), not out of reach.
			if reason, reasonErr := pageBusyReasonOf(app, job.PageID, false); reasonErr == nil && reason != "" {
				return wait()
			}
		}
		if err != nil && !isPermissionDenied(err) && !errors.As(err, &failure) {
			return err
		}
		if err != nil {
			app.Logger().Warn("row page sub-page copy refused", "page", job.PageID, "subPage", next.SourcePageID, "error", err)
		} else {
			payload.SubPageJobs = append(payload.SubPageJobs, admission.Job.ID)
		}
	}
	payload.SubPageNext, payload.Waits = payload.SubPageNext+1, 0
	return saveRowPageDatabaseCopyJob(app, job, payload, 0, false)
}

// rowPageSubPageCopiesDone says whether the sub-pages' copies the job began
// have ended, done or failed.
func rowPageSubPageCopiesDone(app core.App, jobIDs []string) (bool, error) {
	if len(jobIDs) == 0 {
		return true, nil
	}
	encoded, err := json.Marshal(jobIDs)
	if err != nil {
		return false, err
	}
	var pending int
	err = app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_page_duplication_jobs}}
		WHERE [[job_id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[phase]] NOT IN ('completed','failed')
	`).Bind(dbx.Params{"ids": string(encoded)}).Row(&pending)
	return pending == 0, err
}
