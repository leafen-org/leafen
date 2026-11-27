package app

import "github.com/pocketbase/pocketbase/core"

// applyNotionPageMove moves a page under another for the public API's page
// move, as applyPageMetadata does, the page keeping who edited it last and
// when: Notion's move through its API edits no page (seen 2026-11-27 twice,
// ten minutes apart: the page moved, the page it left and the page it went
// into all kept their last_edited_time), as the app's move does
// (page_place_parent_edits.go). The move is still a change of the page —
// its cursor and its readers hear it.
func applyNotionPageMove(
	app core.App, actorID, pageID string, input pageMetadataInput, requestHash string,
) (pageMetadataResponse, pageMetadataMutationStatus, error) {
	var response pageMetadataResponse
	status := pageMetadataApplied
	err := app.RunInTransaction(func(txApp core.App) error {
		page, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		editedAt, editedBy := page.GetString("updated"), page.GetString("last_edited_by")
		response, status, err = applyPageMetadataTransaction(txApp, actorID, pageID, input, requestHash)
		if err != nil || status != pageMetadataApplied || response.Receipt.Duplicate {
			return err
		}
		moved, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		// The answer is read again from the page (serveNotionPageMove).
		return restorePageLastEdit(txApp, moved, editedAt, editedBy)
	})
	return response, status, err
}
