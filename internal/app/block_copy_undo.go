package app

import (
	"encoding/json"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A caller opting into copy Undo pins the native output before the copy fence
// opens. A later receipt read must never adopt a peer's edit as its baseline.
type nativeBlockCopyUndo struct {
	HistoryStart int `json:"historyStart"`
	HistoryEnd   int `json:"historyEnd"`
}

func updateNativeCopyPayload(app core.App, jobID string, change func(map[string]any) error) error {
	raw := ""
	if err := app.DB().NewQuery(`SELECT [[payload_json]] FROM {{leafen_jobs}} WHERE [[id]]={:id}`).Bind(dbx.Params{"id": jobID}).Row(&raw); err != nil {
		return err
	}
	value := map[string]any{}
	if json.Unmarshal([]byte(raw), &value) != nil {
		return errBlockMoveConflict
	}
	if err := change(value); err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[payload_json]]={:value} WHERE [[id]]={:id}`).Bind(dbx.Params{"id": jobID, "value": string(encoded)}).Execute()
	return err
}

func nativeBlockCopyRootSpot(app core.App, job storedBlockMoveJob, doc *crdt.Doc, placement blockMovePlacement) (string, *string, error) {
	roots := []struct {
		ID string `db:"target_block_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[target_block_id]] FROM {{leafen_block_move_items}} WHERE [[job_id]]={:id} AND [[depth]]=0 AND [[moved]]=1 ORDER BY [[position]] DESC,[[block_id]] DESC LIMIT 1`).Bind(dbx.Params{"id": job.ID}).All(&roots); err != nil {
		return "", nil, err
	}
	after := placement.CopyAfter
	if len(roots) > 0 {
		after = roots[0].ID
	}
	order, err := structuralOrder(doc, placement.CopyParent)
	if err != nil {
		return "", nil, err
	}
	if after != "" && structuralIndex(order, after) < 0 {
		return "", nil, errBlockMoveConflict
	}
	if after == "" {
		return placement.CopyParent, nil, nil
	}
	return placement.CopyParent, &after, nil
}

func captureNativeBlockCopyUndo(app core.App, job storedBlockMoveJob) error {
	placement, err := loadBlockMovePlacement(app, job.ID)
	if err != nil || placement.CopyUndo == nil {
		return err
	}
	placement.CopyUndo.HistoryEnd, err = pageHistorySnapshotFrontier(app, job.DestinationPageID)
	if err != nil {
		return err
	}
	return updateNativeCopyPayload(app, job.ID, func(value map[string]any) error { value["copyUndo"] = placement.CopyUndo; return nil })
}

func linkNativePageCopyUndo(app core.App, job storedBlockMoveJob, sourcePageID string) error {
	placement, err := loadBlockMovePlacement(app, job.ID)
	if err != nil {
		return err
	}
	pageJob, err := loadStoredJobByRequest(app, job.ActorID, "block-copy-page:"+childPageBlockDigest(job.ID, sourcePageID))
	if err != nil {
		return err
	}
	if err := updateNativeCopyPayload(app, pageJob.ID, func(value map[string]any) error { value["parentBlockCopyJobId"] = job.ID; return nil }); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_page_duplication_jobs}} SET [[parent_block_copy_job_id]]={:parent} WHERE [[job_id]]={:id}`).Bind(dbx.Params{"parent": job.ID, "id": pageJob.ID}).Execute(); err != nil {
		return err
	}
	if placement.CopyUndo == nil {
		return nil
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_block_move_items}} SET [[undo_page_job_id]]={:pageJob} WHERE [[job_id]]={:job} AND [[block_id]]={:source}`).Bind(dbx.Params{"pageJob": pageJob.ID, "job": job.ID, "source": sourcePageID}).Execute(); err != nil {
		return err
	}
	return updateNativeCopyPayload(app, pageJob.ID, func(value map[string]any) error { value["copyUndoParentJobId"] = job.ID; return nil })
}

func captureNativePageCopyUndo(app core.App, job storedPageDuplicationJob) error {
	raw := ""
	if err := app.DB().NewQuery(`SELECT [[payload_json]] FROM {{leafen_jobs}} WHERE [[id]]={:id}`).Bind(dbx.Params{"id": job.ID}).Row(&raw); err != nil {
		return err
	}
	payload := struct {
		ParentJobID string `json:"copyUndoParentJobId"`
	}{}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return errPageDuplicationConflict
	}
	if payload.ParentJobID == "" {
		return nil
	}
	doc, err := loadPageDuplicationStructuralDocument(app, job.DestinationParentID)
	if err != nil {
		return err
	}
	fingerprints, err := interactiveAutomationBlockFingerprints(app, job.DestinationParentID, doc, []string{job.TargetRootPageID})
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_block_move_items}} SET [[undo_target_id]]={:target},[[undo_fingerprint]]={:digest} WHERE [[job_id]]={:parent} AND ` + partialIndexKey(`[[undo_page_job_id]]`, `={:job}`)).Bind(dbx.Params{"target": job.TargetRootPageID, "digest": fingerprints[job.TargetRootPageID], "parent": payload.ParentJobID, "job": job.ID}).Execute()
	return err
}

// Pin each bounded block-copy window in the same transaction that writes it.
func captureNativeBlockCopyUndoWindow(app core.App, job storedBlockMoveJob, items []storedBlockMoveItem) error {
	placement, err := loadBlockMovePlacement(app, job.ID)
	if err != nil || placement.CopyUndo == nil {
		return err
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.TargetBlockID)
	}
	doc, err := loadPageDuplicationStructuralDocument(app, job.DestinationPageID)
	if err != nil {
		return err
	}
	fingerprints, err := interactiveAutomationBlockFingerprints(app, job.DestinationPageID, doc, ids)
	if err != nil {
		return err
	}
	for _, item := range items {
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_block_move_items}} SET [[undo_target_id]]={:target},[[undo_fingerprint]]={:digest} WHERE [[job_id]]={:job} AND [[block_id]]={:source}`).Bind(dbx.Params{"target": item.TargetBlockID, "digest": fingerprints[item.TargetBlockID], "job": job.ID, "source": item.BlockID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
