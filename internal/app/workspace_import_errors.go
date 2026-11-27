package app

import (
	"errors"
	"net/http"
	"os"
	"strings"
)

type workspaceImportFailureError struct {
	code           string
	cause          error
	stage          string
	sourcePosition string
	// detail says where and what a refused import found wrong — the entry,
	// its line, the field — never the value it found.
	detail string
}

type workspaceImportPublicationRetryError struct{ cause error }

func (err *workspaceImportPublicationRetryError) Error() string { return err.cause.Error() }

func (err *workspaceImportPublicationRetryError) Unwrap() error { return err.cause }

func retryWorkspaceImportPublication(err error) bool {
	retry := &workspaceImportPublicationRetryError{}
	return errors.As(err, &retry)
}

func workspaceImportPublicationRetry(cause error) error {
	if cause == nil {
		return nil
	}
	return &workspaceImportPublicationRetryError{cause: cause}
}

func (err *workspaceImportFailureError) Error() string { return err.code }

func (err *workspaceImportFailureError) Unwrap() error { return err.cause }

func (err *workspaceImportFailureError) DiagnosticStage() string { return err.stage }

func (err *workspaceImportFailureError) DiagnosticSourcePosition() string {
	return err.sourcePosition
}

func workspaceImportInputError() error {
	return &workspaceImportFailureError{
		code: errorCodeWorkspaceImportInvalid, sourcePosition: diagnosticSourcePosition(1),
	}
}

// workspaceImportInvalid is a refused import that says what was wrong.
func workspaceImportInvalid(detail string) error {
	return &workspaceImportFailureError{
		code: errorCodeWorkspaceImportInvalid, detail: detail, sourcePosition: diagnosticSourcePosition(1),
	}
}

// workspaceImportRefusal is the refusal in err that says what was wrong —
// a failure may wrap the one that does — or else its outermost refusal.
func workspaceImportRefusal(err error) *workspaceImportFailureError {
	var found *workspaceImportFailureError
	for err != nil {
		failure := &workspaceImportFailureError{}
		if !errors.As(err, &failure) {
			break
		}
		if failure.code == errorCodeWorkspaceImportInvalid && (found == nil || failure.detail != "") {
			found = failure
			if failure.detail != "" {
				break
			}
		}
		err = failure.cause
	}
	return found
}

// workspaceImportAt says where a refusal was found: an entry of the
// bundle and its line.
func workspaceImportAt(err error, place string) error {
	failure := workspaceImportRefusal(err)
	if failure == nil || strings.HasPrefix(failure.detail, place) {
		return err
	}
	if failure.detail == "" {
		failure.detail = place + ": doesn't fit the bundle's format"
	} else {
		failure.detail = place + ": " + failure.detail
	}
	return err
}

// workspaceImportFailureDetail is what a refused import says about where
// and what was wrong, if it says: its own words, or the content graph's
// when the content it wrote was refused there.
func workspaceImportFailureDetail(err error) string {
	if failure := workspaceImportRefusal(err); failure != nil && failure.detail != "" {
		return failure.detail
	}
	graphFailure := &graphRequestError{}
	if errors.As(err, &graphFailure) && graphFailure.status == http.StatusBadRequest && graphFailure.message != "" {
		return graphFailure.message
	}
	return ""
}

func workspaceImportStateError() error {
	return &workspaceImportFailureError{
		code: errorCodeWorkspaceImportStateInvalid, stage: "workspace-import-state",
		sourcePosition: diagnosticSourcePosition(1),
	}
}

func workspaceImportStateErrorAt(stage string) error {
	return &workspaceImportFailureError{
		code: errorCodeWorkspaceImportStateInvalid, stage: stage,
		sourcePosition: diagnosticSourcePosition(1),
	}
}

func workspaceImportTargetNotFreshError(cause error, stage string) error {
	return &workspaceImportFailureError{
		code: errorCodeWorkspaceImportTargetNotFresh, cause: cause, stage: stage,
		sourcePosition: diagnosticSourcePosition(1),
	}
}

func workspaceImportFailureAt(err error, stage string) error {
	return &workspaceImportFailureError{
		code: stableWorkspaceImportFailureCode(err), cause: err, stage: stage,
		sourcePosition: diagnosticSourcePosition(1),
	}
}

func workspaceImportStorageError(cause error) error {
	return workspaceImportStorageErrorAt(cause, "workspace-import-storage")
}

func workspaceImportStorageErrorAt(cause error, stage string) error {
	if cause == nil {
		cause = errors.New(errorCodeWorkspaceImportStorageUnavailable)
	}
	return &workspaceImportFailureError{
		code: errorCodeWorkspaceImportStorageUnavailable, cause: cause, stage: stage,
		sourcePosition: diagnosticSourcePosition(1),
	}
}

func stableWorkspaceImportFailureCode(err error) string {
	if isSQLiteBusyError(err) {
		return errorCodeWorkspaceImportDatabaseBusy
	}
	if errors.Is(err, errDLPPolicyBlocked) {
		return errorCodeDLPPolicyBlocked
	}
	if errors.Is(err, errDLPPolicyUnavailable) {
		return errorCodeDLPUnavailable
	}
	failure := &workspaceImportFailureError{}
	if errors.As(err, &failure) {
		return failure.code
	}
	// The page it goes into no longer the importer's to change, said so
	// rather than as the service failing (workspaceImportRefusedForGood).
	if isPermissionDenied(err) {
		return errorCodePermissionDenied
	}
	graphFailure := &graphRequestError{}
	if errors.As(err, &graphFailure) && graphFailure.code != "" {
		return graphFailure.code
	}
	pathError := &os.PathError{}
	if errors.As(err, &pathError) {
		return errorCodeWorkspaceImportStorageUnavailable
	}
	return errorCodeWorkspaceImportUnavailable
}
