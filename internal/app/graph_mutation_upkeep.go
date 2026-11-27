package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// recordNativeMigrationGraphMutation records a page change Leafen's own
// upkeep makes — the job runner's pairing of sub-items and dependencies, a
// moved data source's table view — like a user mutation, so history, change
// feeds and cursors stay continuous.
func recordNativeMigrationGraphMutation(
	app core.App,
	owner, pageID, mutationID, actorID string,
	structuralUpdate []byte,
	textUpdate string,
	deltas []pageHistoryGraphDelta,
	counts graphMutationCounts,
	activityContext ...workspaceActivityGraphContext,
) error {
	digest := sha256.Sum256(append(append([]byte(mutationID+"\x00"), structuralUpdate...), textUpdate...))
	requestHash := hex.EncodeToString(digest[:])
	var cursor int
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[cursor]]),0)+1 FROM {{leafen_graph_mutations}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID}).Row(&cursor); err != nil {
		return err
	}
	countsJSON, err := json.Marshal(storedGraphMutationReceipt{
		graphMutationCounts: counts, TextUpdate: textUpdate,
	})
	if err != nil {
		return err
	}
	created := time.Now().UTC().UnixMilli()
	stateVector, err := graphChangeStateVector(structuralUpdate)
	if err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_graph_mutations}}
			([[owner]],[[page_id]],[[mutation_id]],[[request_hash]],[[cursor]],[[counts_json]],[[state_vector]],[[created]])
		VALUES ({:owner},{:pageId},{:mutationId},{:requestHash},{:cursor},{:countsJSON},{:stateVector},{:created})
	`).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "mutationId": mutationID,
		"requestHash": requestHash, "cursor": cursor, "countsJSON": string(countsJSON),
		"stateVector": stateVector, "created": created,
	}).Execute(); err != nil {
		return err
	}
	if err := insertStructuralGraphChange(
		app, owner, pageID, mutationID, requestHash, cursor, structuralUpdate, created,
	); err != nil {
		return err
	}
	// A migration's own change keeps the databases it left
	// (page_history_migration_databases.go), and is Leafen's, as its other
	// upkeep (contentGraphMutationHooks.LeafenOwn): its version read as the
	// workspace owner's edit (2026-11-17).
	activity := workspaceActivityGraphContext{Maintenance: true}
	if len(activityContext) > 0 {
		activity = activityContext[0]
	} else if activity.Databases, err = pageHistoryKnownDatabaseCapture(app, owner, pageID); err != nil {
		return err
	} else {
		actorID = leafenSystemAuthorID
	}
	databases, err := activity.Databases.finish(app)
	if err != nil {
		return err
	}
	rows, err := activity.Rows.finish(app)
	if err != nil {
		return err
	}
	return recordPageHistoryGraphMutation(app, pageID, actorID, structuralUpdate, deltas, databases, rows, activity)
}
