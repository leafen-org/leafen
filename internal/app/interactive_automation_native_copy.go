package app

import (
	"encoding/json"
	"fmt"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
	"net/http"
	"strconv"
	"strings"
)

func (run *interactiveAutomationButtonRun) continuationToken(auth pageAuthorization, index int) (string, error) {
	token, err := run.confirmationToken(auth, index)
	if err != nil {
		return "", err
	}
	prefix := fmt.Sprintf("licn_%d_%d_", index, run.progress.ContinuationStep)
	signed, err := interactiveAutomationConfirmationToken(run.user, "continue\x00"+token+"\x00"+strconv.Itoa(run.progress.ContinuationStep))
	return prefix + signed, err
}

func (run *interactiveAutomationButtonRun) continuationReplay(auth pageAuthorization) (bool, error) {
	parts := strings.SplitN(run.input.ContinuationToken, "_", 4)
	if len(parts) != 4 || parts[0] != "licn" {
		return false, errInteractiveAutomationConflict
	}
	index, indexErr := strconv.Atoi(parts[1])
	step, stepErr := strconv.Atoi(parts[2])
	if indexErr != nil || stepErr != nil || index < 0 || step < 0 || step > run.progress.ContinuationStep || index > run.progress.NextAction {
		return false, errInteractiveAutomationConflict
	}
	currentStep := run.progress.ContinuationStep
	run.progress.ContinuationStep = step
	expected, err := run.continuationToken(auth, index)
	run.progress.ContinuationStep = currentStep
	if err != nil {
		return false, err
	}
	if !interactiveAutomationTokenMatches(run.input.ContinuationToken, expected) {
		return false, errInteractiveAutomationConflict
	}
	return index < run.progress.NextAction || step < currentStep, nil
}

func (run *interactiveAutomationButtonRun) setPending(auth pageAuthorization) error {
	run.progress.ContinuationStep++
	token, err := run.continuationToken(auth, run.progress.NextAction)
	if err != nil {
		return err
	}
	run.response.Pending = true
	run.response.ContinuationToken = token
	run.status = http.StatusAccepted
	return nil
}

// A native draft is a container owned by this button, not an arbitrary block
// reference. The durable copy owner remaps databases, pages, text and files.
func (run *interactiveAutomationButtonRun) prepareNativeCopy(app core.App, auth pageAuthorization, doc *crdt.Doc, action interactiveAutomationAction) (graphMutationInput, error) {
	empty := graphMutationInput{}
	if run.locator.Kind != interactiveAutomationSourcePageButton || action.Type != interactiveAutomationActionInsertBlocks {
		return empty, errInteractiveAutomationInvalid
	}
	run.response = interactiveAutomationExecuteResponse{ExecutionID: run.input.ExecutionID, Receipt: &interactiveAutomationReceipt{ExecutionID: run.input.ExecutionID}}
	if run.progress.PendingCopyJob == "" {
		metadata, err := structuralMetadata(doc)
		if err != nil {
			return empty, err
		}
		root, found := metadata[action.SourceBlockID]
		if !found || root.Type != graphBlockTypeTemplate || root.ParentID != run.source.ID || root.Content["buttonDraft"] != true {
			return empty, errInteractiveAutomationConflict
		}
		hasChildren := false
		for _, block := range metadata {
			if block.ParentID == action.SourceBlockID {
				hasChildren = true
				break
			}
		}
		if hasChildren {
			if locked, err := pageLockedFor(app, auth.Page, run.user.Id); err != nil {
				return empty, err
			} else if locked {
				return empty, pageLockedError()
			}
			spot, err := interactiveAutomationBlockPlacement(app, auth, run.source, action.Placement)
			if err != nil {
				return empty, err
			}
			frontier, err := pageHistorySnapshotFrontier(app, auth.Page.Id)
			if err != nil {
				return empty, err
			}
			admission, err := admitBlockMoveOf(app, run.user.Id, auth.Page.Id, action.SourceBlockID, blockMoveInput{
				RequestID: automationStableID("hbc", run.user.Id, run.input.ExecutionID, action.ID), DestinationPageID: auth.Page.Id, Operation: blockMoveOperationCopy,
			}, true, false, false)
			if err != nil {
				return empty, err
			}
			if admission.Admitted {
				if err := updateNativeCopyPayload(app, admission.Job.ID, func(value map[string]any) error {
					value["copyParent"] = spot.ParentID
					value["copyAfter"] = spot.AfterID
					value["copyUndo"] = &nativeBlockCopyUndo{HistoryStart: frontier}
					return nil
				}); err != nil {
					return empty, err
				}
			}
			run.progress.PendingCopyJob = admission.Job.ID
			run.queued = true
			return empty, run.setPending(auth)
		}
	} else {
		job, err := loadBlockMoveJobByID(app, run.progress.PendingCopyJob, run.user.Id)
		if err != nil {
			return empty, err
		}
		if job.Phase == blockMovePhaseFailed {
			return empty, errInteractiveAutomationConflict
		}
		if job.Phase != blockMovePhaseCompleted {
			return empty, run.setPending(auth)
		}
		placement, err := loadBlockMovePlacement(app, job.ID)
		if err != nil {
			return empty, err
		}
		if placement.CopyUndo == nil {
			return empty, errInteractiveAutomationUnavailable
		}
		// The durable item ledger, including child-page replacement outputs,
		// is consumed in keyset windows. Neither the definition nor a response
		// carries the whole native template.
		items, err := loadNativeCopyUndoWindow(app, job.ID, run.progress.CopyCursor)
		if err != nil {
			return empty, err
		}
		if len(items) > 0 {
			for _, item := range items {
				if item.PageJobID != "" {
					if item.PagePhase == pageDuplicationPhaseFailed {
						return empty, errInteractiveAutomationConflict
					}
					if item.PagePhase != pageDuplicationPhaseCompleted {
						return empty, run.setPending(auth)
					}
				}
				if item.Fingerprint == "" || item.TargetID == "" {
					return empty, errInteractiveAutomationUnavailable
				}
			}
			// A template's sub-page copied in is one of the blocks it inserted,
			// told as a block, not as a page the run made: Notion's press tells of
			// two blocks made on the page, with no page to open (seen 2026-11-13).
			for _, item := range items {
				run.response.Effects.Blocks = append(run.response.Effects.Blocks, interactiveAutomationEffectBlock{ID: item.TargetID})
			}
			run.progress.CopyCursor = items[len(items)-1].SourceID
			run.pinnedCopy = true
			return empty, run.setPending(auth)
		}
		if len(run.progress.UndoBlocks) == 0 && len(run.progress.CopyJobs) == 0 {
			run.progress.UndoHistoryCursor = placement.CopyUndo.HistoryStart
		}
		run.progress.CopyJobs = append(run.progress.CopyJobs, job.ID)
		run.progress.CopyHistoryRanges = append(run.progress.CopyHistoryRanges, interactiveAutomationHistoryRange{Start: placement.CopyUndo.HistoryStart, End: placement.CopyUndo.HistoryEnd})
		run.progress.CopyCursor = ""
		run.progress.PendingCopyJob = ""
		run.pinnedCopy = true
	}
	run.progress.NextAction++
	// The next bounded turn handles the next action, including its confirmation.
	return empty, run.setPending(auth)
}

// Page copies are born at revision zero. What a copy writes to its own pages
// until it is done is its own — a copied database's rows record a server
// change on its page, as every structural batch does (2026-10-14) — and only
// a history event or graph change after it was done is someone's edit (2026-11-11:
// a button whose draft held a full-page database refused its own Undo).
// Any such later edit is refused by Undo, even if it happened before the
// client read the completed copy receipt.
func protectInteractiveAutomationCopiedPages(app core.App, jobIDs []string) error {
	if len(jobIDs) == 0 {
		return nil
	}
	encoded, err := json.Marshal(jobIDs)
	if err != nil {
		return err
	}
	var changed int
	err = app.DB().NewQuery(`
 WITH page_jobs AS (
  SELECT [[undo_page_job_id]] AS [[id]] FROM {{leafen_block_move_items}} WHERE [[job_id]] IN (SELECT [[value]] FROM json_each({:jobs})) AND [[undo_page_job_id]]!=''
 ), copied_pages AS (
  SELECT [[mapping]].[[target_id]] AS [[id]], [[mapping]].[[job_id]] AS [[job_id]] FROM {{leafen_page_duplication_identity_map}} [[mapping]]
  WHERE [[mapping]].[[kind]] IN ('page','row') AND [[mapping]].[[job_id]] IN (SELECT [[id]] FROM page_jobs)
  UNION
  SELECT [[item]].[[target_page_id]] AS [[id]], [[item]].[[job_id]] AS [[job_id]] FROM {{leafen_page_duplication_items}} [[item]]
  WHERE [[item]].[[job_id]] IN (SELECT [[id]] FROM page_jobs)
 ), copied AS (
  SELECT [[copy]].[[id]], COALESCE([[job]].[[completed_at]], 0) AS [[done_at]] FROM copied_pages [[copy]]
  LEFT JOIN {{leafen_page_duplication_jobs}} [[job]] ON [[job]].[[job_id]]=[[copy]].[[job_id]] AND [[job]].[[phase]]='completed'
 ) SELECT EXISTS(SELECT 1 FROM copied [[copy]] LEFT JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]]=[[copy]].[[id]]
 WHERE [[page]].[[id]] IS NULL OR [[page]].[[revision]]!=0 OR [[page]].[[in_trash]]=1
 OR EXISTS(SELECT 1 FROM {{leafen_page_history_events}} [[event]] WHERE [[event]].[[page_id]]=[[copy]].[[id]] AND [[event]].[[created_at]]>[[copy]].[[done_at]])
 OR EXISTS(SELECT 1 FROM {{leafen_graph_mutations}} [[mutation]] WHERE [[mutation]].[[page_id]]=[[copy]].[[id]] AND [[mutation]].[[created]]>[[copy]].[[done_at]]))
 `).Bind(dbx.Params{"jobs": string(encoded)}).Row(&changed)
	if err != nil {
		return err
	}
	if changed > 0 {
		return errInteractiveAutomationConflict
	}
	return nil
}
