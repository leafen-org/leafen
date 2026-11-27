package app

// What a refused wiki change or page verification says to fix
// (request_refusal.go): the field and why after "Check the wiki request.",
// and for a page in a state the change can't start from — already a wiki,
// inside one, a database, busy with another operation — that state after
// "The operation can't be applied in the current wiki state." A route keeps
// its status and code.

// wikiRefused refuses a wiki request for the reason given.
func wikiRefused(format string, args ...any) error {
	return refuse(errWikiInvalid, format, args...)
}

// wikiStateRefused refuses a wiki change for the page's state given.
func wikiStateRefused(format string, args ...any) error {
	return refuse(errWikiConflict, format, args...)
}

// errWikiPageBusy is a page a wiki change on it or a page above it holds
// until the change finishes.
var errWikiPageBusy = wikiStateRefused("a wiki change on this page or a page above it is still running; try again when it finishes")

func wikiMutationInputRefusal(input wikiMutationInput) error {
	switch {
	case input.RequestID == "" || len([]rune(input.RequestID)) > maxWikiMutationRequestIDRunes:
		return wikiRefused("requestId is a new id of yours, 1–%d characters, so the same change sent again isn't made twice",
			maxWikiMutationRequestIDRunes)
	case input.ExpectedRevision < 0:
		return wikiRefused("expectedRevision is the page's wiki revision as it was read, 0 or more")
	case !validWikiOperation(input.Operation):
		return wikiRefused("operation is %s or %s, not %q", wikiOperationConvert, wikiOperationUndo, input.Operation)
	}
	return nil
}
