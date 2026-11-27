package main

// neededUnseen are the fields, by record and name, whose handler refuses a
// request without them where reading its steps can't see the check, each
// with where it is (2026-11-26, the wider scorecard: one left out was told
// the state had changed, a 409, while the operation list showed it as
// optional). Each holds the revision or cursor the caller read: one left out
// is refused whenever the server's has moved from 0.
var neededUnseen = map[string]string{
	// applyPageMutation sets pageMutationCursorConflict unless the page's
	// cursor is this one, and updatePage refuses that status.
	"pageMutationInput.baseCursor": "page_mutation.go",
	// keepSidebarLayout replaces the layout only where its updated_at is
	// this one, in its UPDATE's WHERE (0 while the person keeps none).
	"sidebarLayoutWire.revision": "sidebar_layout.go",
}
