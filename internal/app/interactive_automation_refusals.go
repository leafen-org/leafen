package app

import "fmt"

// What a refused button, template or automation run says to fix
// (request_refusal.go): the step and field of its actions as the document
// names them (actions[2].url …) and why, after "Check what to run.", and a
// run in a state it can't go on from says that state instead of "This ID or
// checksum was used for a different run." Status and code stay the route's.

// automationRefused refuses an automation document or run for the reason
// given.
func automationRefused(format string, args ...any) error {
	return refuse(errInteractiveAutomationInvalid, format, args...)
}

// automationRunRefused refuses a run for the state given.
func automationRunRefused(format string, args ...any) error {
	return refuse(errInteractiveAutomationConflict, format, args...)
}

// automationActionAt places a step's refusal at its place in the actions.
func automationActionAt(err error, index int) error {
	return refusalAt(err, fmt.Sprintf("actions[%d].", index))
}

// interactiveAutomationRefusalMessage is a refused run's words.
func interactiveAutomationRefusalMessage(base string, err error) string {
	return refusalMessage(base, refusalReason(err))
}

// interactiveAutomationConflictMessage is a run refused for its state.
func interactiveAutomationConflictMessage(err error) string {
	if reason := refusalReason(err); reason != "" {
		return refusalMessage("This run can't go on this way.", reason)
	}
	return "This ID or checksum was used for a different run."
}

// interactiveAutomationRunInputRefusal is why a run's own fields can't be
// used, or nil.
func interactiveAutomationRunInputRefusal(input interactiveAutomationExecuteInput) error {
	switch {
	case !boundedAutomationString(input.ExecutionID, interactiveAutomationMaxIdentifierRunes):
		return automationRefused("executionId is a new id of yours for the run, 1–%d characters", interactiveAutomationMaxIdentifierRunes)
	case len(input.ConfirmationToken) > 1024 || len(input.ContinuationToken) > 1024:
		return automationRefused("confirmationToken and continuationToken are the tokens a run gave back, at most 1024 characters")
	case input.ContinuationToken != "" && (input.ConfirmationToken != "" || input.Cancel || input.Undo):
		return automationRefused("continuationToken goes alone: not with confirmationToken, cancel or undo")
	case input.Undo && (input.Cancel || input.ConfirmationToken != ""):
		return automationRefused("undo goes alone: not with cancel or confirmationToken")
	}
	return nil
}

// errAutomationConfirmationToken is a confirmation that isn't the one the
// run is waiting on.
var errAutomationConfirmationToken = automationRunRefused("confirmationToken isn't the one the run gave for the step it waits on")

// Refusals more than one step says.
var (
	errAutomationNoTriggerRow     = automationRefused("the step edits the triggering page, which a button on a page has none of: it runs from a database's button property")
	errAutomationTargetNotTrigger = automationRefused("target is trigger_page: the step acts on the page whose button was pressed")
	errAutomationOpenIn           = automationRefused("openIn is left out, or side_peek, center_peek or full_page")
	errAutomationMessage          = automationRefused("message is at most %d characters, its messageSpans spell it, or messageFormula is one formula", interactiveAutomationMaxMessageRunes)
)
