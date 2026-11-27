package app

import (
	"context"
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A content change's receipt as its page keeps it, in the same room
// transaction as the change (content_graph_mutation_owner.go): the cursor it
// takes and the row a replay of its mutation ID answers from
// (storedGraphMutationReplay).

// nextGraphMutationCursor is the cursor a page's next change takes, one past
// the last its receipts hold.
func nextGraphMutationCursor(txApp core.App, ownerID string, pageID string) (int, error) {
	var lastCursor int
	if err := txApp.DB().NewQuery(`
		SELECT COALESCE(MAX([[cursor]]), 0)
		FROM {{leafen_graph_mutations}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"owner": ownerID, "pageId": pageID}).Row(&lastCursor); err != nil {
		return 0, err
	}
	return lastCursor + 1, nil
}

// storeGraphMutationReceipt keeps a change's receipt under its mutation ID
// and request: its counts, what it told the writer, the date command it can
// undo and what it alone changed in its page's graph
// (content_graph_changed_rows.go), with the page's state vector after it.
func storeGraphMutationReceipt(
	ctx context.Context,
	txApp core.App,
	ownerID string,
	pageID string,
	requestHash string,
	receipt graphMutationReceipt,
	dateUndo *databaseDateCommand,
	changed graphChangedParts,
	createdAt int64,
) error {
	countsJSON, err := json.Marshal(storedGraphMutationReceipt{
		graphMutationCounts:    receipt.Counts,
		TextUpdate:             receipt.TextUpdate,
		Page:                   receipt.Page,
		MergedBlockContent:     receipt.MergedBlockContent,
		DateUndo:               dateUndo,
		DependencyShifts:       receipt.DependencyShifts,
		DependencyShiftTasks:   receipt.DependencyShiftTasks,
		DependencyShiftsDenied: receipt.DependencyShiftsDenied,
		graphChangedParts:      changed,
	})
	if err != nil {
		return err
	}
	_, err = txApp.DB().NewQuery(`
		INSERT INTO {{leafen_graph_mutations}}
			([[owner]], [[page_id]], [[mutation_id]], [[request_hash]], [[cursor]], [[counts_json]], [[state_vector]], [[created]])
		VALUES
			({:owner}, {:pageId}, {:mutationId}, {:requestHash}, {:cursor}, {:countsJSON}, {:stateVector}, {:created})
	`).Bind(dbx.Params{
		"owner": ownerID, "pageId": pageID, "mutationId": receipt.MutationID,
		"requestHash": requestHash, "cursor": receipt.Cursor, "countsJSON": string(countsJSON),
		"stateVector": receipt.StateVector, "created": createdAt,
	}).WithContext(ctx).Execute()
	return err
}
