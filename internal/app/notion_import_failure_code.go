package app

// A Notion import or migration turn fails with the code of what stopped it
// (errors.New(code), sometimes wrapped or joined). The job keeps that code, so
// the window and the agent can say why — Notion refused, the connection is
// gone, the plan changed — instead of the runner's general failure
// (seen 2026-10-16: every failure read LEAFEN_JOB_RUNNER_FAILED).

var notionImportFailureCodes = map[string]bool{
	errorCodeNotionImportProviderFailed: true, errorCodeNotionImportTokenRejected: true, errorCodeNotionImportConnectionFailed: true,
	errorCodeNotionImportConnectionNotFound: true, errorCodeNotionImportScopeFailed: true,
	errorCodeNotionImportCreateFailed: true, errorCodeNotionImportFileCopyRequired: true,
	errorCodeNotionImportFilePermanent: true, errorCodeNotionMigrationFailed: true,
	errorCodeNotionMigrationPlanConflict: true, errorCodeNotionMigrationPlanBlocked: true,
	errorCodeNotionMigrationInvalid: true, errorCodeNotionMigrationNotFound: true,
	errorCodeImportedPeopleMappingFailed: true,
}

// stableNotionImportFailureCode finds the first Notion import code in an
// error's chain, or nothing.
func stableNotionImportFailureCode(err error) string {
	for pending := []error{err}; len(pending) > 0; {
		current := pending[0]
		pending = pending[1:]
		if current == nil {
			continue
		}
		if notionImportFailureCodes[current.Error()] {
			return current.Error()
		}
		switch wrapped := current.(type) {
		case interface{ Unwrap() []error }:
			pending = append(pending, wrapped.Unwrap()...)
		case interface{ Unwrap() error }:
			pending = append(pending, wrapped.Unwrap())
		}
	}
	return ""
}

// notionImportFailureReason says, for an agent to tell the person, what a
// failed import's code means and what to do. Another tool's migration
// (source, the tool's name) says its own files and people where Notion's
// says Notion's, and what Notion's would blame on Notion as Leafen's own
// stop, as Leafen read the tool's bundle itself — as the import screens
// say it (notionImportFailureText).
func notionImportFailureReason(code, source string) string {
	if source != "" {
		switch code {
		case errorCodeNotionImportFileCopyRequired, errorCodeNotionImportFilePermanent:
			return "a file from " + source + " couldn't be copied"
		case errorCodeImportedPeopleMappingFailed:
			return "matching " + source + "'s people to members stopped"
		case errorCodeNotionImportProviderFailed, errorCodeNotionImportTokenRejected, errorCodeNotionImportConnectionFailed,
			errorCodeNotionImportConnectionNotFound, errorCodeNotionImportScopeFailed:
			code = ""
		}
	}
	switch code {
	case errorCodeNotionImportProviderFailed:
		return "Notion refused or didn't answer a request"
	case errorCodeNotionImportTokenRejected:
		return "Notion no longer accepts the connection's token — copy the integration's token again (or make a new one) and connect it in Leafen's import screen"
	case errorCodeNotionImportConnectionFailed, errorCodeNotionImportConnectionNotFound:
		return "the Notion connection can't be used: the integration was removed or its token changed — connect it again"
	case errorCodeNotionImportScopeFailed:
		return "Notion didn't let the connection read the chosen pages — share them with the integration again"
	case errorCodeNotionImportFileCopyRequired, errorCodeNotionImportFilePermanent:
		return "a file stored in Notion couldn't be copied"
	case errorCodeNotionMigrationPlanConflict:
		return "the plan changed while it ran — check the plan and run it again"
	case errorCodeNotionMigrationPlanBlocked, errorCodeNotionMigrationInvalid:
		return "the plan can't be carried out as written — check it again"
	case errorCodeImportedPeopleMappingFailed:
		return "matching Notion's people to members stopped"
	case errorCodeNotionImportCreateFailed, errorCodeNotionMigrationFailed:
		return "Leafen couldn't make what was imported"
	default:
		return "Leafen stopped while importing"
	}
}
