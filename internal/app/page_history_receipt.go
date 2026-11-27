package app

// pageHistoryReceiptAt is a restore's or revert's receipt. Its number is
// the version history's frontier after the change, snapshotFrontier as the
// history list names it, which the next restore's or revert's baseRevision
// takes — apart from the page's revision (page.revision) the answer's page
// carries, a number of its own (2026-11-16, an agent calling every
// operation: a revert answered page.revision 0 beside receipt.revision 6).
// revision, its old name, still carries the same number for clients that
// read it.
func pageHistoryReceiptAt(requestID, versionID string, frontier int, duplicate bool) pageHistoryRestoreReceipt {
	return pageHistoryRestoreReceipt{
		RequestID: requestID, RestoredVersionID: versionID,
		SnapshotFrontier: frontier, Revision: frontier, Duplicate: duplicate,
	}
}
