package app

// What the operations list says beside a page's lock and a database row's
// page's changes (page_metadata_row_page_paths.go): locking any page, a row's
// too, is GET /pages/{id}/lock and one PATCH /pages/{id}/metadata, and the
// row's own routes name the same by the page's id.

func init() {
	const byPageID = "The same by the page's id: PATCH /pages/{id}/metadata with expectedAuthorityRevision, " +
		"which GET /pages/{id} or GET /pages/{id}/lock reads (authorityRevision), without the row's whole snapshot."
	for id, note := range map[string]string{
		"GET /pages/{id}/lock": "With the lock it reads what changing it sends: cursor and, for a database row's page, authorityRevision. " +
			"Lock or unlock any page, a database row's too, with PATCH /pages/{id}/metadata " +
			`{"isLocked": true or false, "mutationId": a new ID, "baseCursor": cursor, "expectedAuthorityRevision": authorityRevision for a row's page}.`,
		"PATCH /pages/{id}/metadata": "A database row's page changes here too, one change at a time: its lock, archive, trash, " +
			`restore and move (parentId: a database, or a page or "" for the top with leaveDatabase true, which takes it out of its database), with expectedAuthorityRevision ` +
			"as GET /pages/{id} or GET /pages/{id}/lock reads it (authorityRevision), and its icon, cover and style.",
		"PUT /database-rows/{rowId}/page/lock":     byPageID,
		"PUT /database-rows/{rowId}/page/archive":  byPageID,
		"POST /database-rows/{rowId}/page/trash":   byPageID,
		"POST /database-rows/{rowId}/page/restore": byPageID,
		"POST /database-rows/{rowId}/page/move":    byPageID,
	} {
		nativeOperationNotes[nativeOperationShape(id)] = note
	}
}
