package app

// The pages by who made them and by their data source (a query-plan
// follow-up, 2026-11-10). A person's daily page update digest
// (collectPageUpdateDigest) reads the pages they made and the pages they
// follow; with no index led by the maker it read every page of the server
// for each person. A database's rows sort apart from the pages under their
// data source, so the pages a person made are one range even beside the
// thousands of rows they made. The index is on every page, not on the pages
// that are not rows alone: such a partial index is smaller than the table,
// and the planner read it whole in place of the table for statements that
// ask for those pages, slower than the table.

const pageCreatorIndex = "idx_leafen_pages_created_by_source"
