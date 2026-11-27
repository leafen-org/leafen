package app

// partialIndexKey is a lookup of column by value, written so SQLite can
// answer it from the column's partial index, one made only for the rows where
// the column is not the empty string. The planner uses a partial index only
// when the statement's own WHERE implies the index's, and "column = value"
// never does, even for a bound value that is never empty; without the second
// term a write or a repeated read goes through every row of the table or the
// workspace instead.
//
// comparison is "=" followed by a bound value or another column. A fence,
// job, rule, token or container is looked up by its own id, never by the
// empty string, so the second term changes no result; an empty value finds
// nothing.
func partialIndexKey(column, comparison string) string {
	return "(" + column + comparison + " AND " + column + "!='')"
}
