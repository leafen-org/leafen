package app

// A row brought back from trash keeps the time it spent there: a page version
// taken while it was in trash shows it away, marks its return as a change and
// takes it away again when restored, as Notion's page history does. The trash
// marker itself goes when the row comes back, so each stay ends here as a
// span from its trash time to its restore time.

// databaseRowAwayAtSQL holds for a row that was in trash at {:at} and has come
// back since.
func databaseRowAwayAtSQL(rowColumn string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_database_row_trash_spans}} AS [[stay]]
		WHERE [[stay]].[[row_id]] = ` + rowColumn + ` AND [[stay]].[[trashed_at]] <= {:at} AND [[stay]].[[restored_at]] > {:at})`
}
