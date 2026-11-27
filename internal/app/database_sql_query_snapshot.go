package app

import "github.com/pocketbase/pocketbase/core"

// runDatabaseSQLQueryStep runs one step of a SQL query's snapshot in its own
// transaction, keeping its access decisions as a rows query's steps do
// (advanceDatabaseQueryJobTurns): a step writes only its job's own progress —
// the rows it takes in, its sources' cursors, its results — nothing a decision
// reads, and the transaction holds the database's one writer meanwhile. Each
// related row its rows' relation cells name is then judged once a step, and
// the page holding that row's database once for them all (2026-11-26: judged
// afresh for every cell, three 300-row sources with a relation each took
// 4.0 s of a NAS-class server's time to snapshot, 0.9 s kept).
func runDatabaseSQLQueryStep(app core.App, step func(core.App) error) error {
	return app.RunInTransaction(func(txApp core.App) error { return step(withAccessMemo(txApp)) })
}

// A SQL query's table holds no column for a formula or a rollup
// (databaseSQLLogicalColumns, as Notion's SQL gives none), so its snapshot
// reads each row's own values and works out none of them: a source with a
// rollup over a relation was taken a few dozen rows a step, each step
// working out the related rows' values the query then threw away
// (2026-11-26: three 300-row sources with a rollup and a formula, 1.2 s of a
// NAS-class server's time to snapshot with the decisions kept, 0.8 s).

// mcpSQLSnapshotMentions is an agent's reading of the rows a SQL query's
// snapshot takes in: its own (mcpQueryMentions) without the rows' formulas
// and rollups, named apart so no query's rows kept for one are the other's.
func mcpSQLSnapshotMentions(authority mcpAuthority) databaseQueryMentions {
	mentions := mcpQueryMentions(authority)
	mentions.key += ":uncomputed"
	mentions.uncomputed = true
	return mentions
}

// computedPropertiesSQL leaves a source's formulas and rollups out of the
// properties of a reading that works out none of them, as a condition on
// leafen_database_properties (loadDatabaseQueryContext).
func (mentions databaseQueryMentions) computedPropertiesSQL() string {
	if !mentions.uncomputed {
		return ""
	}
	return " AND [[type]] NOT IN ('" + graphValueTypeFormula + "', '" + graphValueTypeRollup + "')"
}
