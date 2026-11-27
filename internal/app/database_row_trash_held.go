package app

// databaseRowTrashHeldSQL is true while the trash event of rootRowID is kept
// in the trash: a permanent deletion of its page waits (page_deletion_hold.go),
// or a legal hold keeps the page of one of its rows. It judges that one
// event, its rows read by the event's index. As the set of every held event
// it judged a hold for every row of the trash whenever it was asked, and the
// trash runner asked each time it looked for its next end, inside its write
// (2026-11-18: 8,000 rows in the trash and a legal hold of 500 pages, 0.59 s
// of CPU on the Mac each time a row went to the trash; on a NAS-class
// server, sending 20 rows to the trash one after another took 4.5–6.2 s a
// row, up to 10 s, and some were refused, against 0.21–0.26 s after).
func databaseRowTrashHeldSQL(rootRowID string) string {
	return `(EXISTS (
		SELECT 1 FROM {{leafen_page_deletion_holds}} AS [[deletion_hold]]
		WHERE [[deletion_hold]].[[page_id]] = ` + rootRowID + `
	) OR EXISTS (
		SELECT 1 FROM {{leafen_database_row_trash}} AS [[held_trash]]
		INDEXED BY [[idx_leafen_database_row_trash_root]]
		WHERE [[held_trash]].[[root_row_id]] = ` + rootRowID + `
			AND ` + legalHoldRetainsPageSQL("[[held_trash]].[[row_id]]", "[[held_trash]].[[workspace_id]]") + `
	))`
}
