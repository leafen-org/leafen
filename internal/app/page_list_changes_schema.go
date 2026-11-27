package app

// What a workspace's sidebar lists changes page by page, and each change is
// kept here by the page it touches: its record, the person's favorite of it,
// its inline databases, its wiki state, its body's first block or last (its
// icon), the copy it belongs to finishing. An
// open screen then reads only the pages changed since its list
// (bootstrap_page_changes.go) rather than the whole list again: in a
// workspace of 47,000 pages that was 48 windows and 17–25 s on a NAS for
// another person's rename, move or trash (2026-11-15), where Notion's
// sidebar follows within a second. A change that may reach pages it doesn't
// name — sharing, teamspaces and their access, groups, a wiki made or
// unmade — is kept as one of the whole list (page '').
//
// Each change takes the next number of one sequence, which starts at 1 (a
// list read before any change holds them all from there); a page changed
// again keeps its last. One whose place or fence changed (moved, deleted for good
// or brought back) says so in subtree_seq: the pages under it are read too,
// as their access follows it.

const (
	pageListChangesTable        = "leafen_page_list_changes"
	pageListChangeSequenceTable = "leafen_page_list_change_sequence"
)
