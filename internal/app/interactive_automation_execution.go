package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	interactiveAutomationSourcePageButton     = "page_button"
	interactiveAutomationSourceDatabaseButton = "database_button"
	interactiveAutomationSourceLegacyTemplate = "legacy_template"
)

type interactiveAutomationSourceLocator struct {
	Kind       string
	PageID     string
	BlockID    string
	DatabaseID string
	RowID      string
	PropertyID string
}

func (locator interactiveAutomationSourceLocator) sourceID() string {
	if locator.Kind == interactiveAutomationSourcePageButton || locator.Kind == interactiveAutomationSourceLegacyTemplate {
		return locator.BlockID
	}
	return strings.Join([]string{locator.DatabaseID, locator.RowID, locator.PropertyID}, ":")
}

func (locator interactiveAutomationSourceLocator) valid() bool {
	if !boundedAutomationString(locator.PageID, interactiveAutomationMaxIdentifierRunes) {
		return false
	}
	if locator.Kind == interactiveAutomationSourcePageButton || locator.Kind == interactiveAutomationSourceLegacyTemplate {
		return boundedAutomationString(locator.BlockID, interactiveAutomationMaxIdentifierRunes)
	}
	return locator.Kind == interactiveAutomationSourceDatabaseButton &&
		boundedAutomationString(locator.DatabaseID, interactiveAutomationMaxIdentifierRunes) &&
		boundedAutomationString(locator.RowID, interactiveAutomationMaxIdentifierRunes) &&
		boundedAutomationString(locator.PropertyID, interactiveAutomationMaxIdentifierRunes)
}

func executeInteractiveAutomationButton(event *core.RequestEvent, ygo *pocketBaseYgoPersistence,
	locator interactiveAutomationSourceLocator) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := interactiveAutomationExecuteInput{}
	if err := event.BindBody(&input); err != nil {
		return interactiveAutomationError(event, bodyRefusal(errInteractiveAutomationInvalid, "run", err), "input")
	}
	if !locator.valid() {
		return interactiveAutomationError(event, automationRefused("the page, block and button IDs in the path are 1–%d characters each", interactiveAutomationMaxIdentifierRunes), "input")
	}
	input.ExecutionID = strings.TrimSpace(input.ExecutionID)
	input.ConfirmationToken = strings.TrimSpace(input.ConfirmationToken)
	input.ContinuationToken = strings.TrimSpace(input.ContinuationToken)
	if err := interactiveAutomationRunInputRefusal(input); err != nil {
		return interactiveAutomationError(event, err, "input")
	}
	run := interactiveAutomationButtonRun{event: event, user: user, locator: locator, input: input, status: http.StatusCreated}
	starts := map[string]struct{}{}
	copies := map[string]string{}
	hooks := contentGraphMutationHooks{EnforcePageLock: true, PersonChange: true, ExplicitStartBlockIDs: starts, TextSourceCopies: copies}
	hooks.Authorize = func(app core.App) (pageAuthorization, error) {
		stored, found, err := loadInteractiveAutomationExecution(app, user.Id, input.ExecutionID)
		if err != nil {
			return pageAuthorization{}, err
		}
		previous := storedInteractiveAutomationResult{}
		if found && stored.PageID == locator.PageID && stored.SourceKind == locator.Kind && stored.SourceID == locator.sourceID() && json.Unmarshal([]byte(stored.ResultJSON), &previous) == nil && previous.Progress != nil && previous.Progress.PendingCopyJob != "" {
			return authorizePageBodyWithOwnedCopy(app, user.Id, locator.PageID, permissionWorkspaceWrite, previous.Progress.PendingCopyJob)
		}
		return authorizePageBody(app, user.Id, locator.PageID, permissionWorkspaceWrite)
	}
	hooks.PrepareInput = func(app core.App, auth pageAuthorization, doc *crdt.Doc, graphInput graphMutationInput) (graphMutationInput, error) {
		prepared, err := run.prepare(app, auth, doc)
		if err != nil {
			return graphInput, err
		}
		prepared.MutationID = graphInput.MutationID
		for id := range run.starts {
			starts[id] = struct{}{}
		}
		for id, source := range run.copies {
			copies[id] = source
		}
		return prepared, nil
	}
	hooks.ResolveRequestHash = func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
		return automationDigest(user.Id, input.ExecutionID, input.ConfirmationToken, input.ContinuationToken, fmt.Sprint(input.Undo), run.documentHash), nil
	}
	hooks.Prepare = func(app core.App, auth pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
		if run.replayed {
			return contentGraphMutationPreparation{Deferred: true}, nil
		}
		if len(run.graphInput.Operations) == 0 {
			if err := run.finish(app, auth, doc); err != nil {
				return contentGraphMutationPreparation{}, err
			}
			return contentGraphMutationPreparation{Deferred: true}, nil
		}
		return contentGraphMutationPreparation{Operations: run.graphInput.Operations}, nil
	}
	hooks.AfterApply = run.finish
	// The run's own steps keep its record, its undo and its deliveries and
	// set its rows' icons and archives, which record their own changes: its
	// change says the rows and blocks it wrote, as a person's change of the
	// same operations says them. Before, it said nothing, and on a page
	// holding a database of 5,000 rows each press had the page's other tabs
	// and every page linking the database read their 51 windows again, some
	// 7 seconds on a NAS-class server (2026-11-25).
	hooks.StepsKeepGraph = true
	_, err := ygo.applyContentGraphMutation(user.Id, locator.PageID,
		graphMutationInput{MutationID: interactiveAutomationPhaseID(user.Id, input.ExecutionID, input.ConfirmationToken+input.ContinuationToken, input.Undo)}, hooks)
	if err != nil {
		var graphError *graphRequestError
		if errors.As(err, &graphError) {
			return writeGraphError(event, err, "automation", errorCodeInteractiveAutomationUnavailable, "Couldn't run the action.")
		}
		return interactiveAutomationError(event, err, "execute")
	}
	// Rows of other pages' databases, after this page's change; a retry of
	// the phase writes them again, which each page records once.
	if run.progress != nil && !run.input.Cancel {
		if err := applyInteractiveAutomationOtherPages(ygo, user.Id, run.progress.OtherPages, false); err != nil {
			var graphError *graphRequestError
			if errors.As(err, &graphError) {
				return writeGraphError(event, err, "automation", errorCodeInteractiveAutomationUnavailable, "Couldn't run the action.")
			}
			return interactiveAutomationError(event, err, "execute")
		}
	}
	if !run.replayed {
		for _, row := range run.plan.TemplateRows {
			if err := ygo.fillDatabaseRowFromTemplate(event.Request.Context(), event.App, user.Id, row.RowID, row.TemplateID, "", false); err != nil {
				return interactiveAutomationError(event, err, "execute")
			}
		}
	}
	if run.progress != nil && run.input.Undo && !run.replayed {
		if err := trashInteractiveAutomationUndoRows(event.Request.Context(), ygo, user.Id, input.ExecutionID, run.progress); err != nil {
			return interactiveAutomationError(event, err, "execute")
		}
	}
	if runner, ok := event.App.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok && (run.delivered || run.queued) {
		runner.notify()
	}
	run.response.Outcomes = withAnswerableFormTokens(event.App, user.Id, run.response.Outcomes)
	return event.JSON(run.status, run.response)
}

// withAnswerableFormTokens gives each form a run opens its link for whoever
// pressed, if they may answer it, when the run answers them — never kept
// with the run (a replay gives it again). The form's link is what opens it;
// the page holding its database, which they may not read, is not said.
func withAnswerableFormTokens(app core.App, principalID string, outcomes []interactiveAutomationOutcome) []interactiveAutomationOutcome {
	result := make([]interactiveAutomationOutcome, len(outcomes))
	copy(result, outcomes)
	for index, outcome := range result {
		if outcome.Type == "openForm" {
			if outcome.PageID != "" {
				result[index].FormToken = answerableFormToken(app, principalID, outcome.PageID, outcome.ViewID)
			}
			result[index].PageID, result[index].ContainerID = "", ""
		}
	}
	return result
}

func interactiveAutomationConfirmationToken(user *core.Record, binding string) (string, error) {
	key := user.TokenKey() + user.Collection().AuthToken.Secret
	if key == "" {
		return "", errInteractiveAutomationUnavailable
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte("interactive-automation-confirmation\x00" + binding))
	return "liac_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func interactiveAutomationTokenMatches(received string, expected string) bool {
	if received == "" || len(received) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(received), []byte(expected)) == 1
}

func interactiveAutomationError(event *core.RequestEvent, err error, stage string) error {
	if storageErr, ok := persistentWriteStorageAPIError(
		event, err, persistentWriteInteractiveAutomation, "transaction",
	); ok {
		return storageErr
	}
	status, code, message := http.StatusServiceUnavailable,
		errorCodeInteractiveAutomationUnavailable, "Couldn't run the action."
	var graphErr *graphRequestError
	switch {
	case errors.As(err, &graphErr) && graphErr.code == errorCodeGraphChangeTooLarge:
		status, code, message = graphErr.status, graphErr.code, graphErr.message
	case isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodeInteractiveAutomationNotFound, "Button, template or target content not found."
	case errors.Is(err, errInteractiveAutomationInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodeInteractiveAutomationInvalid, interactiveAutomationRefusalMessage("Check what to run.", err)
	case errors.Is(err, errInteractiveAutomationNotFound):
		status, code, message = http.StatusNotFound,
			errorCodeInteractiveAutomationNotFound, "Button, template or target content not found."
	case errors.Is(err, errInteractiveAutomationUnsupported):
		status, code, message = http.StatusConflict,
			errorCodeInteractiveAutomationUnsupported, "This action can't run yet."
	case errors.Is(err, errInteractiveAutomationConflict):
		status, code, message = http.StatusConflict,
			errorCodeInteractiveAutomationConflict, interactiveAutomationConflictMessage(err)
	}
	return apiErrorWithCause(
		event, status, code, message, "interactive_automation", stage, code, err,
	)
}
