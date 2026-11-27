package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type interactiveAutomationButtonRun struct {
	event        *core.RequestEvent
	user         *core.Record
	locator      interactiveAutomationSourceLocator
	input        interactiveAutomationExecuteInput
	status       int
	response     interactiveAutomationExecuteResponse
	source       interactiveAutomationSource
	stored       storedInteractiveAutomationExecution
	found        bool
	replayed     bool
	delivered    bool
	queued       bool
	pinnedCopy   bool
	documentHash string
	progress     *interactiveAutomationProgress
	plan         interactiveAutomationPlan
	actions      []interactiveAutomationAction
	graphInput   graphMutationInput
	starts       map[string]struct{}
	copies       map[string]string
}

func (run *interactiveAutomationButtonRun) prepare(app core.App, auth pageAuthorization, doc *crdt.Doc) (graphMutationInput, error) {
	empty := graphMutationInput{}
	stored, found, err := loadInteractiveAutomationExecution(app, run.user.Id, run.input.ExecutionID)
	if err != nil {
		return empty, err
	}
	run.stored, run.found = stored, found
	if found {
		if stored.OwnerID != auth.Workspace.OwnerID || stored.WorkspaceID != auth.Workspace.WorkspaceID || stored.PageID != run.locator.PageID || stored.SourceKind != run.locator.Kind || stored.SourceID != run.locator.sourceID() {
			return empty, automationRunRefused("executionId is a run of another button or page: a new run takes a new id")
		}
		previous := storedInteractiveAutomationResult{}
		if json.Unmarshal([]byte(stored.ResultJSON), &previous) != nil {
			return empty, errInteractiveAutomationUnavailable
		}
		if _, err := interactiveAutomationResponseFromStored(stored); err != nil {
			return empty, err
		}
		run.progress = previous.Progress
		run.documentHash = stored.DocumentHash
		if run.input.Undo {
			if run.progress != nil && run.progress.Undone {
				return run.replay(stored)
			}
			run.graphInput, err = interactiveAutomationUndoInput(app, auth.Page.Id, doc, run.progress)
			run.response = interactiveAutomationExecuteResponse{Receipt: &interactiveAutomationReceipt{ExecutionID: run.input.ExecutionID}, Undone: true}
			if err != nil {
				return run.graphInput, err
			}
			rows, err := interactiveAutomationUndoRowOperations(app, auth.Workspace.OwnerID, run.progress)
			if err != nil {
				return run.graphInput, err
			}
			run.graphInput.Operations = append(run.graphInput.Operations, rows[auth.Page.Id]...)
			delete(rows, auth.Page.Id)
			run.progress.OtherPages = interactiveAutomationUndoOtherPages(run.user.Id, run.input.ExecutionID, rows)
			return run.graphInput, nil
		}
	}
	if found && run.input.Cancel {
		if run.progress == nil || run.progress.Done {
			return run.replay(stored)
		}
		token, err := run.confirmationToken(auth, run.progress.NextAction)
		if err != nil {
			return empty, err
		}
		if !interactiveAutomationTokenMatches(run.input.ConfirmationToken, token) {
			return empty, errAutomationConfirmationToken
		}
		run.progress.ConsumedTokens = append(run.progress.ConsumedTokens, token)
		run.progress.Done = true
		run.response = interactiveAutomationExecuteResponse{Receipt: &interactiveAutomationReceipt{ExecutionID: run.input.ExecutionID}, CanUndo: (len(run.progress.UndoBlocks) > 0 || len(run.progress.CopyJobs) > 0)}
		return empty, nil
	}
	if run.input.Undo || run.input.Cancel && !found {
		return empty, automationRunRefused("executionId names no run of this button to undo or cancel")
	}
	source, err := loadInteractiveAutomationSource(app, auth, run.locator)
	if err != nil {
		return empty, err
	}
	run.source = source
	normalized := normalizedInteractiveAutomation{}
	if source.Kind == interactiveAutomationSourceLegacyTemplate {
		run.plan, run.documentHash, err = planLegacyTemplateUse(app, auth, source, run.input.ExecutionID)
	} else {
		normalized, err = normalizeInteractiveAutomationDocument(source.Document)
		run.documentHash = normalized.DocumentHash
	}
	if err != nil {
		return empty, err
	}
	targetHash := automationDigest(run.locator.sourceID(), run.documentHash)
	if found && (stored.TargetHash != targetHash || stored.DocumentHash != run.documentHash) {
		return empty, automationRunRefused("the button's steps changed since this run began: run it again with a new executionId")
	}
	if found && (run.progress == nil || run.progress.Done) {
		return run.replay(stored)
	}
	if run.progress == nil {
		run.progress = &interactiveAutomationProgress{Variables: map[string]json.RawMessage{}, UndoBlocks: map[string]string{}}
	}
	if found && run.input.ContinuationToken != "" {
		replay, err := run.continuationReplay(auth)
		if err != nil {
			return empty, err
		}
		if replay {
			return run.replay(stored)
		}
	} else if found {
		if run.input.ConfirmationToken == "" {
			return run.replay(stored)
		}
		for _, consumed := range run.progress.ConsumedTokens {
			if interactiveAutomationTokenMatches(run.input.ConfirmationToken, consumed) {
				return run.replay(stored)
			}
		}
		token, err := run.confirmationToken(auth, run.progress.NextAction)
		if err != nil {
			return empty, err
		}
		if !interactiveAutomationTokenMatches(run.input.ConfirmationToken, token) {
			return empty, errAutomationConfirmationToken
		}
		run.progress.ConsumedTokens = append(run.progress.ConsumedTokens, token)

		run.progress.NextAction++
	} else if run.input.ConfirmationToken != "" || run.input.ContinuationToken != "" {
		return empty, automationRunRefused("confirmationToken and continuationToken go with a run already begun; executionId names none")
	}
	// Every phase uses the exact receipt timestamp, including the first query.
	executionTime := time.Now().UTC().Truncate(time.Millisecond)
	if found {
		executionTime = time.UnixMilli(stored.CreatedAt).UTC()
	} else {
		digest := sha256.Sum256([]byte(run.input.ConfirmationToken))
		run.stored = storedInteractiveAutomationExecution{ActorID: run.user.Id, ExecutionID: run.input.ExecutionID,
			OwnerID: auth.Workspace.OwnerID, WorkspaceID: auth.Workspace.WorkspaceID, PageID: auth.Page.Id,
			SourceKind: run.locator.Kind, SourceID: run.locator.sourceID(), TargetHash: targetHash, DocumentHash: run.documentHash,
			ConfirmationHash: hex.EncodeToString(digest[:]), CreatedAt: executionTime.UnixMilli()}
	}
	if source.Kind != interactiveAutomationSourceLegacyTemplate {
		begin := run.progress.NextAction
		if begin < 0 || begin > len(normalized.Document.Actions) {
			return empty, errInteractiveAutomationUnavailable
		}
		if begin < len(normalized.Document.Actions) && normalized.Document.Actions[begin].SourceBlockID != "" {
			return run.prepareNativeCopy(app, auth, doc, normalized.Document.Actions[begin])
		}
		run.plan, run.actions, err = planInteractiveAutomationActionTurn(app, auth, source, normalized.Document, run.input.ExecutionID, executionTime, *run.progress)
		if err != nil {
			return empty, err
		}
		next := run.plan.progress
		run.progress = &next
		end := next.NextAction
		if !next.Done && normalized.Document.Actions[end].Type == interactiveAutomationActionShowConfirmation {
			// Its words as the run gives them so far, as a notification's.
			action, err := run.plan.formulas.resolveMessage(normalized.Document.Actions[end], next.Variables)
			if err != nil {
				return empty, err
			}
			title := ""
			_ = json.Unmarshal(action.Title, &title)
			token, err := run.confirmationToken(auth, end)
			if err != nil {
				return empty, err
			}
			run.plan.Confirmation = &interactiveAutomationConfirmation{Title: title, Message: action.Message, MessageSpans: action.MessageSpans,
				ConfirmLabel: action.ConfirmLabel, CancelLabel: action.CancelLabel, Token: token}
		}
	} else {
		run.progress.Done = true
	}
	if err := enforceOrganizationDLPContent(app, auth.Workspace.Organization.OrganizationID, run.plan); err != nil {
		return empty, err
	}
	run.response = interactiveAutomationExecuteResponse{ExecutionID: run.input.ExecutionID,
		Receipt: &interactiveAutomationReceipt{ExecutionID: run.input.ExecutionID}, Effects: run.plan.Effects, Outcomes: run.plan.Outcomes, Confirmation: run.plan.Confirmation}
	if run.response.Confirmation != nil {
		run.status = http.StatusAccepted
	} else if !run.progress.Done {
		if err := run.setPending(auth); err != nil {
			return empty, err
		}
	}
	run.graphInput, run.starts, run.copies, err = buildInteractiveAutomationMutation(app, auth, run.plan)
	if err != nil {
		return run.graphInput, err
	}
	others, err := interactiveAutomationOtherPageInputs(app, auth, run.plan)
	if err != nil {
		return run.graphInput, err
	}
	if err := noteInteractiveAutomationUndoRows(app, auth, run.plan, run.progress); err != nil {
		return run.graphInput, err
	}
	phase := interactiveAutomationPhaseID(run.user.Id, run.input.ExecutionID, run.input.ConfirmationToken+run.input.ContinuationToken, run.input.Undo)
	writes := []interactiveAutomationOtherPageWrite{}
	for _, other := range others {
		writes = append(writes, interactiveAutomationOtherPageWrite{Phase: phase, PageID: other.PageID, Operations: other.Operations})
	}
	// A repeat of a phase already planned keeps that phase's writes, which
	// are made again after it (each page records them once).
	if len(writes) > 0 || len(run.progress.OtherPages) == 0 || run.progress.OtherPages[0].Phase != phase {
		run.progress.OtherPages = writes
	}
	return run.graphInput, nil
}

func (run *interactiveAutomationButtonRun) confirmationToken(auth pageAuthorization, index int) (string, error) {
	binding := strings.Join([]string{run.user.Id, run.input.ExecutionID, auth.Workspace.OwnerID, auth.Workspace.WorkspaceID,
		run.locator.PageID, run.locator.Kind, run.locator.sourceID(), automationDigest(run.locator.sourceID(), run.documentHash), run.documentHash, fmt.Sprint(index)}, "\x00")
	return interactiveAutomationConfirmationToken(run.user, binding)
}

func (run *interactiveAutomationButtonRun) replay(stored storedInteractiveAutomationExecution) (graphMutationInput, error) {
	var err error
	run.response, err = interactiveAutomationResponseFromStored(stored)
	run.replayed = true
	run.status = http.StatusOK
	return graphMutationInput{}, err
}

func (run *interactiveAutomationButtonRun) finish(app core.App, auth pageAuthorization, doc *crdt.Doc) error {
	if run.input.Undo {
		if err := restoreInteractiveAutomationArchives(app, auth, run.progress); err != nil {
			return err
		}
		if err := restoreInteractiveAutomationPageIcons(app, auth, run.progress); err != nil {
			return err
		}
		run.progress.Undone = true
		run.progress.Done = true
	} else {
		ids := make([]string, 0, len(run.plan.Effects.Blocks))
		for _, block := range run.plan.Effects.Blocks {
			ids = append(ids, block.ID)
		}
		if len(ids) > 0 && !run.pinnedCopy {
			if len(run.progress.UndoBlocks) == 0 && len(run.progress.CopyJobs) == 0 {
				frontier, err := pageHistorySnapshotFrontier(app, auth.Page.Id)
				if err != nil {
					return err
				}
				run.progress.UndoHistoryCursor = frontier
			}
			fingerprints, err := interactiveAutomationBlockFingerprints(app, auth.Page.Id, doc, ids)
			if err != nil {
				return err
			}
			if run.progress.UndoBlocks == nil {
				run.progress.UndoBlocks = map[string]string{}
			}
			for id, digest := range fingerprints {
				run.progress.UndoBlocks[id] = digest
			}
		}
		// Its pages' icons set and its pages archived after this page's change, in the same transaction.
		iconed, err := applyInteractiveAutomationPageIcons(app, auth, run.plan)
		if err != nil {
			return err
		}
		noteInteractiveAutomationUndoIcons(run.progress, iconed)
		archived, err := applyInteractiveAutomationArchives(app, auth, run.plan)
		if err != nil {
			return err
		}
		noteInteractiveAutomationUndoArchives(run.progress, archived)
		deliveryIDs, err := admitInteractiveAutomationButtonDeliveries(app, run.user.Id, auth, run.source, run.actions,
			run.input.ExecutionID, time.UnixMilli(run.stored.CreatedAt).UTC())
		if err != nil {
			return err
		}
		run.delivered = len(deliveryIDs) > 0
		run.response.CanUndo = (len(run.progress.UndoBlocks) > 0 || len(run.progress.CopyJobs) > 0 || len(run.progress.UndoRows) > 0 ||
			len(run.progress.UndoArchives) > 0 || len(run.progress.UndoIcons) > 0) && !run.progress.Undone
		run.response.Effects.Edited, run.response.Effects.Tasks, err = interactiveAutomationNoticeRows(app, &run.plan)
		if err != nil {
			return err
		}
	}
	return saveInteractiveAutomationResult(app, run.stored, run.found, run.response, run.progress)
}

// interactiveAutomationNoticeRows lists the rows a run changed but did not
// make, and tells whether every row it made or changed is a task, for the
// notice that says how many tasks or pages it made and changed, as Notion's
// (seen 2026-11-02).
func interactiveAutomationNoticeRows(app core.App, plan *interactiveAutomationPlan) ([]interactiveAutomationEffectPage, bool, error) {
	edited := []interactiveAutomationEffectPage{}
	sources := map[string]bool{}
	for id, row := range plan.Rows {
		if !row.Insert {
			edited = append(edited, interactiveAutomationEffectPage{ID: id})
		}
		sources[row.DatabaseID] = true
	}
	// A page it archived or gave an icon reads as edited too, "1개의 페이지가 편집되었습니다." (Notion's, seen
	// 2026-11-15 and 2026-11-21).
	listed := map[string]bool{}
	for _, archived := range plan.Archives {
		if _, own := plan.Rows[archived.RowID]; !own && !listed[archived.RowID] {
			edited, listed[archived.RowID] = append(edited, interactiveAutomationEffectPage{ID: archived.RowID}), true
		}
		sources[archived.DatabaseID] = true
	}
	for _, icon := range plan.PageIcons {
		if _, own := plan.Rows[icon.RowID]; !own && !listed[icon.RowID] {
			edited, listed[icon.RowID] = append(edited, interactiveAutomationEffectPage{ID: icon.RowID}), true
		}
		sources[icon.DatabaseID] = true
	}
	sort.Slice(edited, func(i, j int) bool { return edited[i].ID < edited[j].ID })
	tasks := len(sources) > 0
	for source := range sources {
		roles, err := loadDatabaseTaskRoles(app, source)
		if err != nil {
			return nil, false, err
		}
		tasks = tasks && roles.Enabled
	}
	return edited, tasks, nil
}

// interactiveAutomationUndoOtherPages are the undo's writes to other pages' rows, one phase.
func interactiveAutomationUndoOtherPages(actorID, executionID string, rows map[string][]graphOperation) []interactiveAutomationOtherPageWrite {
	phase := interactiveAutomationPhaseID(actorID, executionID, "", true)
	pages := make([]string, 0, len(rows))
	for page := range rows {
		pages = append(pages, page)
	}
	sort.Strings(pages)
	writes := make([]interactiveAutomationOtherPageWrite, 0, len(pages))
	for _, page := range pages {
		writes = append(writes, interactiveAutomationOtherPageWrite{Phase: phase, PageID: page, Operations: rows[page]})
	}
	return writes
}
