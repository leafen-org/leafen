package app

// A data source's rows sit on its database's page under the page's owner:
// a database's rows are read, moved and deleted by that owner, page and
// source, and every index of the row table begins with the owner and the
// page. A statement that finds a source's rows by the source alone reads
// every row of every database, so it names the source's owner and page too.

// databaseSourceRowsSQL narrows row, an alias of leafen_database_rows, to the
// rows of the data source whose id the SQL expression source gives, read
// from the row indexes by the source's owner and page.
func databaseSourceRowsSQL(row, source string) string {
	return row + `.[[owner]] = (SELECT [[owner]] FROM {{leafen_data_sources}} WHERE [[id]] = ` + source + `)
		AND ` + row + `.[[page_id]] = (SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = ` + source + `)
		AND ` + row + `.[[data_source_id]] = ` + source
}
