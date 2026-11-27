package app

import (
	"sort"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A database automation's run records its page's change with what it changed
// there, as a person's change of the same operations says it
// (graphChangeParts): the rows it wrote and whether it wrote the page's
// blocks. The page's open reader then reads those rows (and its blocks)
// again, and a page linking its database those rows alone. Before, the run's
// change said nothing: on a page holding a database of 5,000 rows, each value
// an automation set had the open table and every page linking it read their
// 51 windows again, some 7.5 seconds on a NAS-class server (2026-11-25). A
// chunk of a run gathers what each automation it ran changed; one that
// doesn't say has the chunk's change say nothing, and its readers read whole.

// databaseAutomationGraphChange is what a chunk of a run changed in its
// page's graph so far.
type databaseAutomationGraphChange struct {
	parts  graphChangedParts
	unsaid bool
}

// add takes in what one automation's change of the page changed; a change
// that doesn't say leaves the chunk's saying nothing.
func (change *databaseAutomationGraphChange) add(parts graphChangedParts) {
	if len(parts.OnlyRows) == 0 && !parts.Databases && !parts.PageBlocks {
		change.unsaid = true
		return
	}
	change.parts.OnlyRows = append(change.parts.OnlyRows, parts.OnlyRows...)
	change.parts.Databases = change.parts.Databases || parts.Databases
	change.parts.PageBlocks = change.parts.PageBlocks || parts.PageBlocks
}

// receipt is the receipt the chunk's change is kept with: what it changed,
// each row once, or nothing when one of its changes doesn't say or it names
// more rows than a reader reads as changes.
func (change *databaseAutomationGraphChange) receipt() storedGraphMutationReceipt {
	if change.unsaid {
		return storedGraphMutationReceipt{}
	}
	seen := make(map[string]struct{}, len(change.parts.OnlyRows))
	rows := make([]string, 0, len(change.parts.OnlyRows))
	for _, id := range change.parts.OnlyRows {
		if _, dup := seen[id]; !dup {
			seen[id] = struct{}{}
			rows = append(rows, id)
		}
	}
	if len(rows) > maxGraphChangedRows {
		return storedGraphMutationReceipt{}
	}
	sort.Strings(rows)
	parts := change.parts
	parts.OnlyRows = rows
	return storedGraphMutationReceipt{graphChangedParts: parts}
}

// interactiveAutomationGraphChangeBefore is the page as a run's change finds
// it, for graphChangeParts to judge the change by: the blocks holding its
// databases, read when the change writes blocks.
func interactiveAutomationGraphChangeBefore(
	app core.App, owner, pageID string, operations []graphOperation,
) (*pageHistoryDatabaseCapture, error) {
	before := &pageHistoryDatabaseCapture{owner: owner, pageID: pageID}
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock, graphOperationKindMoveBlock, graphOperationKindDeleteBlock:
			blocks, err := pageDatabaseContainerBlocks(app, owner, pageID)
			before.containerBlocks = blocks
			return before, err
		}
	}
	return before, nil
}

// applyPlan applies an automation's plan to the run's page (doc) and takes
// in what its change of the page changed. A plan refused part way may have
// written some of it, which the chunk can't say.
func (state *databaseAutomationEvaluationState) applyPlan(
	app core.App, doc *crdt.Doc, authorization pageAuthorization,
	plan interactiveAutomationPlan, scanner organizationDLPContentScanner,
) (bool, error) {
	changed, parts, err := applyInteractiveAutomationPlanSaying(app, doc, authorization, plan, scanner)
	switch {
	case state.graphChange == nil:
	case err != nil:
		state.graphChange.unsaid = true
	case changed:
		state.graphChange.add(parts)
	}
	return changed, err
}

// recordDatabaseAutomationGraphChange records a chunk of a run's change of
// its page with what it changed (databaseAutomationGraphChange.receipt).
func recordDatabaseAutomationGraphChange(
	app core.App, ownerID, pageID, mutationID string, update []byte, change *databaseAutomationGraphChange,
) error {
	return recordServerGraphChangeReceipt(app, ownerID, pageID, mutationID, update, change.receipt())
}
