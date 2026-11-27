package app

import "fmt"

// keptCauseError is a failure told in its kind's words that still carries
// what made it fail: errors.Is finds the kind, and the error writer finds the
// cause — a database that stayed busy is told busy (api_error_busy.go) — while
// whatever shows the failure's words shows the kind's alone, never the
// cause's.
type keptCauseError struct {
	kind  error
	cause error
}

func (failure *keptCauseError) Error() string { return failure.kind.Error() }

func (failure *keptCauseError) Unwrap() []error { return []error{failure.kind, failure.cause} }

// failedBecause is kind carrying cause.
func failedBecause(kind, cause error) error {
	if cause == nil {
		return kind
	}
	return &keptCauseError{kind: kind, cause: cause}
}

// diagnosticCauseClass is the class a log line names a failure's cause by:
// for one told in its kind's words, what made it fail.
func diagnosticCauseClass(cause error) string {
	for {
		kept, ok := cause.(*keptCauseError)
		if !ok {
			return fmt.Sprintf("%T", cause)
		}
		cause = kept.cause
	}
}
