package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type nativeCopyUndoItem struct {
	SourceID    string `db:"block_id"`
	TargetID    string `db:"undo_target_id"`
	Fingerprint string `db:"undo_fingerprint"`
	PageJobID   string `db:"undo_page_job_id"`
	PagePhase   string `db:"page_phase"`
}

func loadNativeCopyUndoWindow(app core.App, jobID, cursor string) ([]nativeCopyUndoItem, error) {
	items := []nativeCopyUndoItem{}
	err := app.DB().NewQuery(`SELECT [[item]].[[block_id]],[[item]].[[undo_target_id]],[[item]].[[undo_fingerprint]],[[item]].[[undo_page_job_id]],COALESCE([[page]].[[phase]],'') AS [[page_phase]]
 FROM {{leafen_block_move_items}} [[item]] LEFT JOIN {{leafen_page_duplication_jobs}} [[page]] ON [[page]].[[job_id]]=[[item]].[[undo_page_job_id]]
 WHERE [[item]].[[job_id]]={:job} AND [[item]].[[moved]]=1 AND [[item]].[[block_id]]>{:cursor}
 ORDER BY [[item]].[[block_id]] LIMIT {:limit}`).Bind(dbx.Params{"job": jobID, "cursor": cursor, "limit": maxBlockMoveItemsPerTurn}).All(&items)
	return items, err
}
