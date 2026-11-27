package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page that falls behind its changes reads what they changed, not its whole
// graph, when each of them changed only the page's rows, its databases'
// sources, views and properties and its blocks, and said which (2026-11-11:
// a page of 100,000 rows read its thousand windows again, 15–46 seconds, for
// one value another person typed, and again for a sort another person set).
// A change says so in its stored receipt (graphChangedParts); one that
// touched anything else, or a hook's or another writer's change that doesn't
// say, has the page read whole.

// maxGraphChangedRows is the most rows a page reads as its changes; more
// read the whole graph.
const maxGraphChangedRows = 500

// maxGraphChangedDatabaseItems is the most of its databases' containers,
// sources, views and properties a page reads with its changes; more read the
// whole graph.
const maxGraphChangedDatabaseItems = 1000

// graphChangedParts is what a change alone changed in its page's graph: the
// rows it names (OnlyRows), whether its databases' sources, views or
// properties changed (Databases) and whether its blocks did (PageBlocks,
// content_graph_changed_blocks.go), when it changed nothing else; a change
// that may have changed more has none of them.
type graphChangedParts struct {
	OnlyRows   []string `json:"onlyRows,omitempty"`
	Databases  bool     `json:"databases,omitzero"`
	PageBlocks bool     `json:"pageBlocks,omitzero"`
}

// graphChangeParts is what a change made by its operations alone changed in
// its page's graph, or nothing when it may have changed more: other items,
// the page's title, rows beside its operations (a hook's, a sprint's, a
// property's cells), dependency edges shown on rows, or a block holding a
// database. A hook's steps beside its operations count as changing more
// unless they say they keep the graph (StepsKeepGraph), or that this time
// they didn't change it (StepsChangeGraph); its check of the operations
// (CheckOperations) is not such a step. Text written with
// its blocks is theirs. A change of no
// operations says the rows its own steps changed when it names them
// (ChangedRows: a row put in the trash or brought back). `before` is the
// page's databases as they were before it, and `deltas` the blocks it
// changed.
func graphChangeParts(
	input graphMutationInput,
	hooks contentGraphMutationHooks,
	receipt graphMutationReceipt,
	sprintRowsMade []string,
	before *pageHistoryDatabaseCapture,
	deltas []pageHistoryGraphDelta,
) graphChangedParts {
	ownSteps := len(input.Operations) == 0 && hooks.ChangedRows != nil
	stepsKeep := hooks.StepsKeepGraph || hooks.StepsChangeGraph != nil && !hooks.StepsChangeGraph()
	if len(input.Operations) == 0 && !ownSteps || len(input.Operations) > maxGraphChangedRows ||
		input.PageTitle != nil || input.PageKind != nil || receipt.Page != nil ||
		len(receipt.PropertyConversions) > 0 || receipt.KeptSyncedCopies > 0 ||
		receipt.Counts.DependencyEdges > 0 || receipt.Counts.DependencyBindings > 0 || len(sprintRowsMade) > 0 ||
		hooks.BeforeApply != nil || hooks.BeforeApplyOperations != nil && !stepsKeep || hooks.ApplyAdditionalText != nil ||
		!ownSteps && (hooks.AfterApply != nil && !stepsKeep || len(hooks.HistoryRows) > 0) ||
		hooks.WorkspaceImportReplaceJobID != "" || hooks.WorkspaceImportReplaceRollback {
		return graphChangedParts{}
	}
	parts := graphChangedParts{}
	if ownSteps {
		parts.OnlyRows = hooks.ChangedRows()
	}
	for _, operation := range input.Operations {
		switch {
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil && operation.Row.ID != "":
			parts.OnlyRows = append(parts.OnlyRows, operation.Row.ID)
		case graphDatabaseChangeKeepsRows(operation, before):
			parts.Databases = true
		case graphBlockChangeKeepsDatabases(operation):
			parts.PageBlocks = true
		default:
			return graphChangedParts{}
		}
	}
	// Blocks and text written besides its blocks' operations aren't said.
	blocksOrText := len(input.TextUpdates) > 0 || receipt.TextUpdate != "" || receipt.MergedBlockContent || receipt.Counts.Blocks > 0
	if blocksOrText && !parts.PageBlocks || ownSteps && len(parts.OnlyRows) == 0 ||
		len(parts.OnlyRows) > maxGraphChangedRows ||
		parts.PageBlocks && graphBlocksHoldDatabases(before, input.Operations, deltas) {
		return graphChangedParts{}
	}
	return parts
}

// graphDatabaseChangeKeepsRows tells a change of a database's source, view or
// property that leaves every row and the sources the page shows as they
// were: a source of the page renamed or restyled, a view kept on its source
// or deleted, a property kept its type (a relation its target) or a new one
// of the page's own database that fills no cells. A type changed converts or
// links cells, a new relation shows its target's rows, a new Verification
// fills its Owner, and a property deleted takes its cells.
func graphDatabaseChangeKeepsRows(operation graphOperation, before *pageHistoryDatabaseCapture) bool {
	if before == nil {
		return false
	}
	switch {
	case operation.Kind == graphOperationKindUpsertDataSource && operation.DataSource != nil:
		_, shown := before.before[operation.DataSource.ID]
		return shown
	case operation.Kind == graphOperationKindDeleteView && operation.ViewDeletion != nil:
		return true
	case operation.Kind == graphOperationKindUpsertView && operation.View != nil:
		return graphViewKeptOnSource(before, *operation.View)
	case operation.Kind == graphOperationKindUpdateView && operation.ViewUpdate != nil:
		return graphViewKeptOnSource(before, operation.ViewUpdate.View)
	case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil:
		return graphPropertyKeepsCells(before, *operation.Property)
	}
	return false
}

// graphViewKeptOnSource tells a view of a source the page showed, on the
// source it was on when it was there.
func graphViewKeptOnSource(before *pageHistoryDatabaseCapture, view graphDatabaseView) bool {
	if _, shown := before.before[view.DataSourceID]; !shown {
		return false
	}
	for sourceID, state := range before.before {
		for _, kept := range state.Views {
			if kept.ID == view.ID {
				return sourceID == view.DataSourceID
			}
		}
	}
	return true
}

// graphPropertyKeepsCells tells a property of the page's own database that
// changes none of its cells and no source the page shows.
func graphPropertyKeepsCells(before *pageHistoryDatabaseCapture, property graphProperty) bool {
	state, shown := before.before[property.DataSourceID]
	if !shown || state.Source == nil {
		return false
	}
	for _, kept := range state.Properties {
		if kept.ID == property.ID {
			return kept.Type == property.Type &&
				(property.Type != graphValueTypeRelation || graphRelationPointsAt(kept.Config) == graphRelationPointsAt(property.Config))
		}
	}
	return property.Type != graphValueTypeRelation && property.Type != graphValueTypeVerification
}

// graphRelationPointsAt is the database and property a relation's settings
// point at, or "?" when they can't be read.
func graphRelationPointsAt(config json.RawMessage) string {
	target := struct {
		Database string `json:"relationDatabaseId"`
		Property string `json:"relatedPropertyId"`
	}{}
	if json.Unmarshal(config, &target) != nil {
		return "?"
	}
	return target.Database + "\x00" + target.Property
}

// loadContentGraphChangedRows reads what the page's changes after the
// position `since` changed for a reader that doesn't take its blocks with
// them (loadContentGraphChangedParts): a change of its blocks has the page
// read whole.
func loadContentGraphChangedRows(
	app core.App,
	owner, pageID string,
	linkedScope linkedDatabaseReadScope,
	since int,
) (graphReadResponse, bool, error) {
	return loadContentGraphChangedParts(app, owner, pageID, linkedScope, since, false)
}

// loadContentGraphChangedParts reads the page's rows its changes after the
// position `since` changed, as of the read's moment, and those of them it no
// longer has — those its reader no longer sees are added once the read is
// filtered for them (redactGraphReadForReader) — with its databases'
// containers, sources, views and properties whole when the changes changed
// those (ChangedDatabases), and its blocks whole when they changed those
// and the reader takes them (withBlocks, ChangedBlocks); false when one of
// those changes doesn't say what it changed, or that is too much, and the
// page is read whole.
func loadContentGraphChangedParts(
	app core.App,
	owner, pageID string,
	linkedScope linkedDatabaseReadScope,
	since int,
	withBlocks bool,
) (graphReadResponse, bool, error) {
	parts, count, said, err := graphPageChangesParts(app, owner, pageID, since)
	if err != nil || !said || parts.PageBlocks && !withBlocks {
		return graphReadResponse{}, false, err
	}
	response, read, err := readGraphChangedParts(app, owner, pageID, linkedScope, parts)
	// Changes made while it was read are the page's next.
	if err != nil || !read || response.SnapshotRevision != since+count {
		return graphReadResponse{}, false, err
	}
	response.ChangedSince = &since
	return response, true, nil
}

// graphPageChangesParts is what a page's changes after the position `since`
// changed, gathered — the rows they name, sorted, and whether they changed
// its databases and its blocks — and how many there were; false when one is
// missing between (trimmed away) or doesn't say what it changed, or that is
// too much. A page's own reader reads them (loadContentGraphChangedParts), and
// so do the pages showing its rows (content_graph_external_changed_rows.go),
// for which its blocks are nothing to read.
func graphPageChangesParts(app core.App, owner, pageID string, since int) (graphChangedParts, int, bool, error) {
	changes := []struct {
		Cursor int    `db:"cursor"`
		Counts string `db:"counts_json"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[cursor]], [[counts_json]] FROM {{leafen_graph_mutations}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[cursor]] > {:since}
		ORDER BY [[cursor]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "since": since, "limit": maxGraphChangedRows + 1,
	}).All(&changes); err != nil {
		return graphChangedParts{}, 0, false, err
	}
	rows := map[string]struct{}{}
	gathered := graphChangedParts{}
	for index, change := range changes {
		// A change missing between (one trimmed away) isn't known.
		if change.Cursor != since+index+1 {
			return graphChangedParts{}, 0, false, nil
		}
		stored := storedGraphMutationReceipt{}
		if err := json.Unmarshal([]byte(change.Counts), &stored); err != nil ||
			len(stored.OnlyRows) == 0 && !stored.Databases && !stored.PageBlocks {
			return graphChangedParts{}, 0, false, nil
		}
		gathered.Databases = gathered.Databases || stored.Databases
		gathered.PageBlocks = gathered.PageBlocks || stored.PageBlocks
		for _, id := range stored.OnlyRows {
			rows[id] = struct{}{}
		}
		if len(rows) > maxGraphChangedRows {
			return graphChangedParts{}, 0, false, nil
		}
	}
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	gathered.OnlyRows = ids
	return gathered, len(changes), true, nil
}

// readGraphChangedParts reads the page's rows of those named that it still
// has, as of the read's moment, and the others as gone, with its databases
// and its blocks whole when they changed; false when that doesn't fit one
// read.
func readGraphChangedParts(
	app core.App,
	owner, pageID string,
	linkedScope linkedDatabaseReadScope,
	parts graphChangedParts,
) (graphReadResponse, bool, error) {
	// A read of no row is still one of rows alone.
	ids := append([]string{}, parts.OnlyRows...)
	limit := max(1, len(ids))
	if parts.Databases {
		limit += maxGraphChangedDatabaseItems
	}
	if parts.PageBlocks {
		limit += maxGraphChangedBlocks
	}
	response, err := readContentGraphWindow(app, owner, pageID, linkedScope, "", limit, true, &graphChangedParts{
		OnlyRows: ids, Databases: parts.Databases, PageBlocks: parts.PageBlocks,
	})
	if err != nil || response.HasMore {
		return graphReadResponse{}, false, err
	}
	read := map[string]struct{}{}
	for _, item := range response.Items {
		if id, _ := item.Value["id"].(string); item.Kind == graphReadKindRow {
			read[id] = struct{}{}
		}
	}
	gone := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, has := read[id]; !has {
			gone = append(gone, id)
		}
	}
	response.GoneRows = gone
	response.ChangedDatabases = parts.Databases
	response.ChangedBlocks = parts.PageBlocks
	return response, true, nil
}

// redactGraphReadForReader filters a read for its reader
// (redactGraphItemsForReader). A read of what changes changed
// (ChangedSince) names gone, beside the rows the page no longer has, each
// row it read that the filtering took out: one closed to the reader, an
// unlinked item they hold no role in now. Judged before the filtering, such
// a row stayed on their open views as it last read (2026-11-25: an item
// shared to a group they had left kept its row, under its old name, in
// their open table after its next change of values). Every read of changes
// a reader's views take up goes through here.
func redactGraphReadForReader(app core.App, readerID string, response *graphReadResponse) error {
	// Taken first: the filtering reuses the items' list.
	read := []string{}
	if response.ChangedSince != nil {
		for _, item := range response.Items {
			if id, _ := item.Value["id"].(string); item.Kind == graphReadKindRow {
				read = append(read, id)
			}
		}
	}
	items, err := redactGraphItemsForReader(app, readerID, response.Items)
	if err != nil {
		return err
	}
	response.Items = items
	if len(read) == 0 {
		return nil
	}
	shown := make(map[string]struct{}, len(read))
	for _, item := range items {
		if id, _ := item.Value["id"].(string); item.Kind == graphReadKindRow {
			shown[id] = struct{}{}
		}
	}
	closed := false
	for _, id := range read {
		if _, kept := shown[id]; !kept {
			response.GoneRows = append(response.GoneRows, id)
			closed = true
		}
	}
	if closed {
		sort.Strings(response.GoneRows)
	}
	return nil
}

// onlyRowsSQL is the condition keeping a window that reads only some rows
// to those, their IDs bound in params.
func (window *graphWindowRead) onlyRowsSQL(params dbx.Params) string {
	if window.onlyRows == nil {
		return ""
	}
	if len(window.onlyRows) == 0 {
		return " AND 0"
	}
	names := make([]string, len(window.onlyRows))
	for index, id := range window.onlyRows {
		name := fmt.Sprintf("onlyRow%d", index)
		params[name] = id
		names[index] = "{:" + name + "}"
	}
	return " AND [[row]].[[id]] IN (" + strings.Join(names, ", ") + ")"
}
