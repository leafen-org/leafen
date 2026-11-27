package app

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// One document imported under an empty page fills that page, as Notion's
// import does (seen 2026-11-10, Markdown and HTML, from the page's "…" ›
// 가져오기 and from the Text & Markdown and HTML windows): the page keeps its
// address, its body becomes the document's and its title the file's name, an
// old title included. A page with anything in it — a block, an empty line
// too, or a sub-page — and an import of several files get a sub-page for each
// file instead. It fills the page only when the request asks it to
// (fillEmptyPage), as the import windows opened on that page do; an import
// given a parent page and no more makes a new page under it, as a page made
// under a parent is (2026-11-16: an agent's import under its own empty test
// page renamed and filled that page, with nothing said). Every way in comes
// here: the one-file import (createWorkspaceImport) and a batch of one file
// (fillWorkspaceImportBatchPlace), whose file's import then adds to the page
// (admitWorkspaceImportBatchDocument). Either names the page when it is in
// (publishWorkspaceImport), and the import's answer says what it did to the
// page (workspaceImportPlaceWarning). A CSV makes its database page as before.

// workspaceImportPlaceIsEmpty reports whether the page an import goes under
// is an empty page it fills: a page of its own — not a database, a
// database's row or its template, which their database names — with no
// blocks, sub-pages or databases in it.
func workspaceImportPlaceIsEmpty(app core.App, pageID string) (bool, error) {
	if pageID == "" {
		return false, nil
	}
	var empty bool
	err := app.DB().NewQuery(`
		SELECT
			[[page]].[[data_source_id]] = '' AND NOT [[page]].[[database_template]]
			AND [[page]].[[kind]] <> {:database} AND [[page]].[[content]] = ''
			AND NOT EXISTS (SELECT 1 FROM {{leafen_blocks}} WHERE [[page_id]] = [[page]].[[id]])
			AND NOT EXISTS (SELECT 1 FROM {{leafen_pages}} WHERE [[parent_id]] = [[page]].[[id]] AND NOT [[in_trash]])
			AND NOT EXISTS (SELECT 1 FROM {{leafen_data_sources}} WHERE [[page_id]] = [[page]].[[id]])
		FROM {{leafen_pages}} AS [[page]] WHERE [[page]].[[id]] = {:pageId}
	`).Bind(dbx.Params{"pageId": pageID, "database": pageKindDatabase}).Row(&empty)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return empty, err
}

// fillWorkspaceImportBatchPlace gives a batch of one Markdown file asked to
// fill the empty page it went under that page as the file's page, once all
// its text is in and before any page is made: its item takes the page, made
// already.
func fillWorkspaceImportBatchPlace(txApp core.App, jobID string, batch workspaceImportJobPayload) error {
	if !batch.FillEmptyPage {
		return nil
	}
	placeID := batch.ParentPageID
	empty, err := workspaceImportPlaceIsEmpty(txApp, placeID)
	if err != nil || !empty {
		return err
	}
	_, err = txApp.DB().NewQuery(`
		UPDATE {{leafen_workspace_import_batch_items}} SET [[page_id]] = {:placeId}, [[page_ready]] = 1
		WHERE [[job_id]] = {:jobId} AND [[parent_position]] = -1 AND [[kind]] = {:file}
			AND [[format]] = {:markdown} AND [[state]] = {:received}
			AND (SELECT COUNT(*) FROM {{leafen_workspace_import_batch_items}}
				WHERE [[job_id]] = {:jobId} AND [[kind]] <> {:skipped}) = 1
	`).Bind(dbx.Params{
		"jobId": jobID, "placeId": placeID, "file": workspaceImportBatchItemKindFile,
		"markdown": workspaceImportFormatMarkdown, "received": workspaceImportBatchItemReceived,
		"skipped": workspaceImportBatchItemKindSkipped,
	}).Execute()
	return err
}

// workspaceImportPlaceTitle is the title of the page an import fills when it
// is admitted: the page takes the file's name only while it still has it.
func workspaceImportPlaceTitle(app core.App, pageID string) (string, error) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return "", err
	}
	return page.GetString("title"), nil
}

// nameWorkspaceImportFilledPage gives the page an import filled the file's
// name, as the import publishes, unless the page was renamed meanwhile. The
// title goes as any rename of the importer's does.
func nameWorkspaceImportFilledPage(txApp core.App, job storedJob, payload workspaceImportJobPayload) error {
	pageID := workspaceImportPrimaryRootPageID(payload)
	page, err := txApp.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if page.GetBool("in_trash") || page.GetString("title") != payload.PlaceTitle || page.GetString("title") == payload.Title {
		return nil
	}
	input := normalizePageMetadataInput(pageMetadataInput{
		Title: payload.Title, ParentID: page.GetString("parent_id"), InTrash: false,
		MutationID: fmt.Sprintf("workspace-import-title-%s", job.ID), BaseCursor: page.GetInt("cursor"),
	})
	if !validPageMetadataMutationInput(pageID, input) {
		return nil
	}
	_, _, err = applyPageMetadataTransaction(txApp, job.Owner, pageID, input, pageMetadataRequestHash(pageID, input))
	return err
}

// workspaceImportPlaceWarning says, in an import's answer, what it did to a
// page that was already there: filled the empty page it went under, or put
// its text in place of a page's body.
func workspaceImportPlaceWarning(payload workspaceImportJobPayload) string {
	switch {
	case payload.FillsPlace || payload.FillEmptyPage && workspaceImportPrimaryRootPageID(payload) == payload.ParentPageID:
		return "This import filled the empty page it went under (fillEmptyPage): the page took the document's body and the file's name as its title."
	case payload.ReplaceExistingPage:
		return "This import put the document in place of the page's body."
	}
	return ""
}
