package app

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// noteBlockMoveEditedPages notes the pages a finished move or copy of blocks
// wrote as edited now, by the one who moved them, as a change to a page's
// blocks edits it (noteGraphPageEdited): where they went, and the page they
// left. Notion's block "옮기기" to another page brought both the page it
// left and the page it went into to the top of a section sorted by last
// edit (seen 2026-11-27 in Chrome); before, the move's pages kept the times
// they had. A copy reads its source and edits only where it goes, and a
// move to its own page's end edits that page once (moveBlockToOwnPageEnd).
func noteBlockMoveEditedPages(app core.App, job storedBlockMoveJob) error {
	pages := []string{job.DestinationPageID}
	if job.Operation != blockMoveOperationCopy && job.SourcePageID != job.DestinationPageID {
		pages = append(pages, job.SourcePageID)
	}
	for _, pageID := range pages {
		if err := notePageContentEdited(context.Background(), app, pageID, job.ActorID); err != nil {
			return err
		}
	}
	return nil
}
