package app

import "github.com/pocketbase/pocketbase/core"

// placePageDuplicationCopy shows a copy's top in its parent's body once it is
// ready: right after the page it copies when that page's block is in the same
// body, as in Notion, and at the end otherwise, where the sidebar puts it. A
// copy asked for by a copied block takes the place of the link that block
// kept while the copy was made. A copy holding a database edits the page
// above with no line (pageDuplicationQuietActivity). A copy at the top has no
// page above: it says it was made, now that it is done
// (workspace_activity_copy.go).
func placePageDuplicationCopy(app core.App, job storedPageDuplicationJob, item storedPageDuplicationItem) error {
	target, err := app.FindRecordById(pagesCollection, item.TargetPageID)
	if err != nil {
		return err
	}
	if pageDuplicationTopCopy(job, item.SourcePageID, target) {
		return recordPageDuplicationMade(app, job, target)
	}
	placeholder, err := pageDuplicationPlaceholder(app, job.ID)
	if err != nil {
		return err
	}
	activity, err := pageDuplicationQuietActivity(app, job.ID)
	if err != nil {
		return err
	}
	placed := false
	if placeholder != "" {
		if placed, err = replaceChildPageCopyPlaceholder(
			app, job.OwnerID, job.WorkspaceID, target.GetString("parent_id"), placeholder, item.SourcePageID, target.Id, job.ActorID, activity...,
		); err != nil {
			return err
		}
	}
	if placed {
		return nil
	}
	return placeChildPageCopy(
		app, job.OwnerID, job.WorkspaceID, target.GetString("parent_id"), item.SourcePageID, target.Id, job.ActorID, activity...,
	)
}
