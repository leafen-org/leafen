package app

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A confirmation pauses at this action. Earlier effects stay committed. The
// variables and execution time belong to the run, not each confirmation retry.
type interactiveAutomationProgress struct {
	NextAction     int                        `json:"nextAction"`
	Done           bool                       `json:"done"`
	Variables      map[string]json.RawMessage `json:"variables,omitempty"`
	BlockIndex     int                        `json:"blockIndex"`
	PageIndex      int                        `json:"pageIndex"`
	ConsumedTokens []string                   `json:"consumedTokens,omitempty"`
	UndoBlocks     map[string]string          `json:"undoBlocks,omitempty"`
	// UndoRows are the rows the run made and changed, for "실행 취소".
	UndoRows map[string]interactiveAutomationUndoRow `json:"undoRows,omitempty"`
	// UndoArchives are the rows the run archived, each with the page
	// holding its database, for "실행 취소" (interactive_automation_archive.go).
	UndoArchives      map[string]string                   `json:"undoArchives,omitempty"`
	UndoHistoryCursor int                                 `json:"undoHistoryCursor"`
	Undone            bool                                `json:"undone,omitzero"`
	PendingCopyJob    string                              `json:"pendingCopyJob,omitempty"`
	CopyCursor        string                              `json:"copyCursor,omitempty"`
	CopyJobs          []string                            `json:"copyJobs,omitempty"`
	CopyHistoryRanges []interactiveAutomationHistoryRange `json:"copyHistoryRanges,omitempty"`
	ContinuationStep  int                                 `json:"continuationStep,omitzero"`
	FilterCursor      string                              `json:"filterCursor,omitempty"`
	// OtherPages are the last phase's writes to other pages' databases,
	// made after the run's own page's change, each once.
	OtherPages []interactiveAutomationOtherPageWrite `json:"otherPages,omitempty"`
	// UndoIcons are the row pages whose icon the run set, with the icon each had
	// (interactive_automation_page_icon.go).
	UndoIcons map[string]interactiveAutomationUndoIcon `json:"undoIcons,omitempty"`
}

// interactiveAutomationOtherPageWrite is a phase's change to one other page.
type interactiveAutomationOtherPageWrite struct {
	Phase      string           `json:"phase"`
	PageID     string           `json:"pageId"`
	Operations []graphOperation `json:"operations"`
}

// mutationID names the write on its page, the same for every retry of the
// phase, so a page records it once.
func (write interactiveAutomationOtherPageWrite) mutationID() string {
	return automationStableID("hbo", write.Phase, write.PageID)
}

// applyInteractiveAutomationOtherPages writes a run's rows on other pages'
// databases, each as its own page's change by the person running — that
// page's access, lock, history and notifications apply, and its automations
// for a button's run, not a database automation's
// (contentGraphMutationHooks.AutomationWrite).
func applyInteractiveAutomationOtherPages(
	ygo *pocketBaseYgoPersistence, principalID string, writes []interactiveAutomationOtherPageWrite, automation bool,
) error {
	for _, write := range writes {
		if len(write.Operations) == 0 {
			continue
		}
		if _, err := ygo.applyContentGraphMutation(principalID, write.PageID,
			graphMutationInput{MutationID: write.mutationID(), Operations: write.Operations},
			contentGraphMutationHooks{PersonChange: true, AutomationWrite: automation}); err != nil {
			return err
		}
	}
	return nil
}

type interactiveAutomationHistoryRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type storedInteractiveAutomationResult struct {
	interactiveAutomationExecuteResponse
	Progress *interactiveAutomationProgress `json:"progress,omitempty"`
}

func saveInteractiveAutomationResult(app core.App, row storedInteractiveAutomationExecution, found bool,
	response interactiveAutomationExecuteResponse, progress *interactiveAutomationProgress) error {
	encoded, err := json.Marshal(storedInteractiveAutomationResult{response, progress})
	if err != nil || len(encoded) > interactiveAutomationMaxResultBytes {
		return failedBecause(errInteractiveAutomationUnavailable, err)
	}
	row.ResultJSON = string(encoded)
	if !found {
		return insertInteractiveAutomationExecution(app, row)
	}
	_, err = app.DB().Update(interactiveAutomationExecutionsTable, dbx.Params{
		"result_json": row.ResultJSON,
	}, dbx.HashExp{"actor_id": row.ActorID, "execution_id": row.ExecutionID}).Execute()
	return err
}

func interactiveAutomationBlockFingerprints(app core.App, pageID string, doc *crdt.Doc, ids []string) (map[string]string, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return nil, err
	}
	textDoc, _, err := loadYgoDoc(app, pageID)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, id := range ids {
		block, exists := metadata[id]
		if !exists {
			return nil, errInteractiveAutomationConflict
		}
		value, err := json.Marshal(struct {
			Block structuralBlockMetadata
			Text  []crdt.Delta
		}{block, textDoc.GetText(ygoBlockTextRootPrefix + id).ToDelta()})
		if err != nil {
			return nil, err
		}
		result[id] = automationDigest(string(value))
	}
	return result, nil
}

func interactiveAutomationUndoInput(app core.App, pageID string, doc *crdt.Doc,
	progress *interactiveAutomationProgress) (graphMutationInput, error) {
	input := graphMutationInput{}
	if progress == nil || (len(progress.UndoBlocks) == 0 && len(progress.CopyJobs) == 0 && len(progress.UndoRows) == 0 && len(progress.UndoArchives) == 0 &&
		len(progress.UndoIcons) == 0) ||
		progress.Undone || progress.PendingCopyJob != "" {
		return input, errInteractiveAutomationConflict
	}
	if len(progress.UndoBlocks) == 0 && len(progress.CopyJobs) == 0 {
		return input, nil
	}
	undoBlocks := make(map[string]string, len(progress.UndoBlocks))
	for id, digest := range progress.UndoBlocks {
		undoBlocks[id] = digest
	}
	for _, jobID := range progress.CopyJobs {
		cursor := ""
		for {
			items, err := loadNativeCopyUndoWindow(app, jobID, cursor)
			if err != nil {
				return input, err
			}
			if len(items) == 0 {
				break
			}
			for _, item := range items {
				if item.Fingerprint == "" || item.TargetID == "" {
					return input, errInteractiveAutomationConflict
				}
				undoBlocks[item.TargetID] = item.Fingerprint
			}
			cursor = items[len(items)-1].SourceID
		}
	}
	if err := protectInteractiveAutomationCopiedPages(app, progress.CopyJobs); err != nil {
		return input, err
	}
	frontier, err := pageHistorySnapshotFrontier(app, pageID)
	if err != nil {
		return input, err
	}
	events, err := loadPageHistoryEvents(app, pageID, progress.UndoHistoryCursor, frontier)
	if err != nil {
		return input, err
	}
	if len(events) > pageHistoryMaxReplayEvents {
		return input, errInteractiveAutomationConflict
	}
	for _, event := range events {
		ownCopy := false
		for _, interval := range progress.CopyHistoryRanges {
			if event.VersionCursor > interval.Start && event.VersionCursor <= interval.End {
				ownCopy = true
				break
			}
		}
		if ownCopy {
			continue
		}
		if event.Kind == pageHistoryEventRewrite {
			return input, errInteractiveAutomationConflict
		}
		if event.Kind != pageHistoryEventGraph {
			continue
		}
		payload := pageHistoryEventEnvelope{}
		if json.Unmarshal(event.Payload, &payload) != nil {
			return input, errInteractiveAutomationUnavailable
		}
		// A later database change must not disappear with a copied database.
		if len(progress.CopyJobs) > 0 && (len(payload.Databases) > 0 || len(payload.Rows) > 0) {
			return input, errInteractiveAutomationConflict
		}
		for _, delta := range payload.GraphDeltas {
			if _, inserted := undoBlocks[delta.BlockID]; inserted && delta.Moved {
				return input, errInteractiveAutomationConflict
			}
		}
	}
	ids := make([]string, 0, len(undoBlocks))
	for id := range undoBlocks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fingerprints, err := interactiveAutomationBlockFingerprints(app, pageID, doc, ids)
	if err != nil {
		return input, err
	}
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return input, err
	}
	for id, block := range metadata {
		if _, ownedParent := undoBlocks[block.ParentID]; ownedParent {
			if _, ownedChild := undoBlocks[id]; !ownedChild {
				return input, errInteractiveAutomationConflict
			}
		}
	}

	for _, id := range ids {
		if fingerprints[id] != undoBlocks[id] {
			return input, errInteractiveAutomationConflict
		}

		if _, insertedParent := undoBlocks[metadata[id].ParentID]; !insertedParent {
			input.Operations = append(input.Operations, graphOperation{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: id}})
		}
	}
	return input, nil
}

func interactiveAutomationPhaseID(actorID, executionID, token string, undo bool) string {
	return automationStableID("hbp", actorID, executionID, token, fmt.Sprint(undo))
}
