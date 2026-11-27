package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Only the execution owning this exact same-page copy may poll its receipt.
// A different copy or move continues to fence every page and row body.
func blockMovePageFenceExceptOwnedCopy(app core.App, principalID, pageID, jobID string) (bool, error) {
	if jobID == "" {
		return blockMovePageFenceActive(app, pageID)
	}
	job, err := loadBlockMoveJobByID(app, jobID, principalID)
	if err != nil {
		return false, err
	}
	if job.Operation != blockMoveOperationCopy || job.SourcePageID != pageID || job.DestinationPageID != pageID {
		return false, permissionDenied()
	}
	var other int
	err = app.DB().NewQuery(`SELECT EXISTS(SELECT 1 FROM {{leafen_block_move_jobs}} [[move]] WHERE [[move]].[[job_id]]!={:job} AND ` + blockMoveActiveFenceSQL("[[move]]") + ` AND ([[move]].[[source_page_id]]={:page} OR [[move]].[[destination_page_id]]={:page}))`).Bind(dbx.Params{"job": jobID, "page": pageID}).Row(&other)
	return other > 0, err
}
