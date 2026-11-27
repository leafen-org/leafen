package app

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// databaseSQLQuerySourcesUnchanged is whether no data source a query's job
// read has changed since it read them.
func databaseSQLQuerySourcesUnchanged(app core.App, jobID string, authorized databaseSQLAuthorizedPlan) (bool, error) {
	stored, err := loadDatabaseSQLQuerySources(app, jobID)
	if err != nil {
		return false, err
	}
	if len(stored) != len(authorized.Sources) {
		return false, nil
	}
	for ordinal, source := range stored {
		if source.SourceRevision != authorized.Sources[ordinal].Revision {
			return false, nil
		}
	}
	return true, nil
}

// A query a call's budget leaves unfinished answers no rows yet, and says
// so: its status pending, to be called again at once (nothing goes on
// between calls), and how far it got — never an empty answer that looks like
// a page with no way on (2026-11-16: an agent read "results: [], has_more:
// true" with no next_cursor seven times as a page). The call that finishes
// it answers with its results.
const mcpQueryStatusPending = "pending"

// markPending makes an answer the pending one action describes.
func (output *notionMCPQueryDataSourcesOutput) markPending(action string) {
	output.Results, output.HasMore, output.NextCursor = []any{}, true, ""
	output.Status, output.NextAction = mcpQueryStatusPending, action
	retry := 0
	output.RetryAfterSeconds = &retry
}

// clearPending takes back what markPending set, for a call's next step.
func (output *notionMCPQueryDataSourcesOutput) clearPending() {
	output.Status, output.NextAction, output.RetryAfterSeconds = "", "", nil
}

// mcpSQLQueryPendingAction says how far a SQL query's snapshot got: the
// data sources read whole of the query's, and the rows taken so far.
func mcpSQLQueryPendingAction(job storedDatabaseSQLQueryJob, sources []storedDatabaseSQLQuerySource) string {
	rows := 0
	for _, source := range sources {
		rows += source.CapturedRows
	}
	return fmt.Sprintf(
		"No results yet, which isn't an empty answer: Leafen is still taking this query's snapshot "+
			"(%d of its %d data sources read, %d rows so far). Call notion-query-data-sources again at once "+
			"with the same arguments: each call goes on with it for up to %d seconds, and the one that finishes it "+
			"answers with the results.",
		min(job.SourceOrdinal, len(sources)), len(sources), rows, int(mcpDataSourceSQLCallBudget.Seconds()),
	)
}
