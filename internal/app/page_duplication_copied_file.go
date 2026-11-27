package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A copy's files are copied in turns of their own: the top's first, while the
// tree is read, when the top goes first (page_duplication_top_first.go), the
// rest once the tree is read (advancePageDuplicationFiles). What waits on a
// file comes as soon as its copy is in, whichever turn copies it: an emoji the
// copy brings (page_duplication_custom_emoji.go), and the uploaded icon and
// cover of the copy's top, which was made when the copy was asked for, before
// any file was copied. So every way a copy goes gives its top the icon and
// cover the original shows, as Notion's copy does (2026-11-16: a top that went
// first while the rest of a small tree was read in the same turn stayed held
// into the copy's own turns, which take what the top could not have then only
// as they hold it, and the copy ended with no icon or cover).

// settlePageDuplicationCopiedFile brings what the copied file task waits on.
func settlePageDuplicationCopiedFile(app core.App, job storedPageDuplicationJob, task storedPageDuplicationFile) error {
	if err := settlePageDuplicationCarriedCustomEmojis(app, job); err != nil {
		return err
	}
	return settlePageDuplicationTopPresentation(app, job, task.SourceFileID)
}

// settlePageDuplicationTopPresentation gives the copy's top its icon and cover
// once the file just copied is one of them and neither is still to copy.
func settlePageDuplicationTopPresentation(app core.App, job storedPageDuplicationJob, copiedFileID string) error {
	source, err := app.FindRecordById(pagesCollection, job.SourceRootPageID)
	if err != nil {
		return err
	}
	ids := []string{}
	if id, managed := productFilePresentationID(source.GetString("icon")); managed && source.GetString("icon_type") == "image" {
		ids = append(ids, id)
	}
	if id, managed := productFilePresentationID(source.GetString("cover")); managed {
		ids = append(ids, id)
	}
	shown := false
	for _, id := range ids {
		shown = shown || id == copiedFileID
	}
	if !shown {
		return nil
	}
	raw, _ := json.Marshal(ids)
	var waiting int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_page_duplication_files}}
		WHERE [[job_id]]={:jobId} AND [[status]]!='copied'
			AND [[source_file_id]] IN (SELECT [[value]] FROM json_each({:fileIds}))
	`).Bind(dbx.Params{"jobId": job.ID, "fileIds": string(raw)}).Row(&waiting); err != nil || waiting > 0 {
		return err
	}
	return refreshPageDuplicationCopyTop(app, job, storedPageDuplicationItem{
		SourcePageID: job.SourceRootPageID, TargetPageID: job.TargetRootPageID,
	})
}
