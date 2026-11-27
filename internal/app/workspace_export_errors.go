package app

import "errors"

type workspaceExportFailureError struct {
	code  string
	cause error
}

func (err *workspaceExportFailureError) Error() string {
	return err.code
}

func (err *workspaceExportFailureError) Unwrap() error {
	return err.cause
}

func newWorkspaceExportFailure(code string) error {
	return &workspaceExportFailureError{code: code}
}

func newWorkspaceExportFailureWithCause(code string, cause error) error {
	return &workspaceExportFailureError{code: code, cause: cause}
}

func stableWorkspaceExportFailureCode(err error) string {
	var failure *workspaceExportFailureError
	if !errors.As(err, &failure) {
		return ""
	}
	switch failure.code {
	case errorCodeWorkspaceExportSourceChanged,
		errorCodeWorkspaceExportArtifactInvalid,
		errorCodeWorkspaceExportStorageUnavailable,
		errorCodeWorkspaceExportDatabaseBusy,
		errorCodeWorkspaceExportFormatUnsupported,
		errorCodeWorkspaceExportCreateFailed,
		errorCodeWorkspaceExportLinkOutside:
		return failure.code
	default:
		return ""
	}
}
