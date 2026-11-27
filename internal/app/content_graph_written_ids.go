package app

import (
	"database/sql"

	"github.com/pocketbase/pocketbase/core"
)

// A content change chooses the IDs of what it writes — a block's, a data
// source's, a view's, a property's, a row's, a dependency link's — and an ID
// already taken by content elsewhere is refused before anything is written,
// telling the caller to choose another: writing under an ID changes only
// what the same owner's same page holds under it (a dependency link, also
// only on the same data source), and never takes over another person's or
// another page's content. The graph's validation asks this one rule of every
// ID a change writes (loadGraphProjectionWithAllReferenceCaches), and the
// dependency records' writers ask it again of the row they would update
// (content_graph_database_dependency_projection.go), since nothing else
// holds a link's ID to its page.

const graphIDTakenMessage = "This ID is already used by content elsewhere. Choose a new ID."

func graphIDTakenError() error {
	return graphInputError(errorCodeGraphReferenceInvalid, graphIDTakenMessage)
}

// graphWrittenIDs are the IDs a change writes under, by what they name.
type graphWrittenIDs struct {
	blocks, sources, views, properties, rows map[string]struct{}
	// edges are the dependency links written, each with its data source.
	edges map[string]string
}

func graphWrittenIDsOf(operations []graphOperation) graphWrittenIDs {
	written := graphWrittenIDs{
		blocks: map[string]struct{}{}, sources: map[string]struct{}{}, views: map[string]struct{}{},
		properties: map[string]struct{}{}, rows: map[string]struct{}{}, edges: map[string]string{},
	}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil:
			written.blocks[operation.Block.ID] = struct{}{}
		case operation.Kind == graphOperationKindUpsertDataSource && operation.DataSource != nil:
			written.sources[operation.DataSource.ID] = struct{}{}
		case operation.Kind == graphOperationKindUpsertView && operation.View != nil:
			written.views[operation.View.ID] = struct{}{}
		case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil:
			written.properties[operation.Property.ID] = struct{}{}
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil:
			written.rows[operation.Row.ID] = struct{}{}
		case operation.Kind == graphOperationKindUpsertDependencyEdge && operation.DependencyEdge != nil:
			written.edges[operation.DependencyEdge.ID] = operation.DependencyEdge.DataSourceID
		}
	}
	return written
}

// refuseTakenGraphDependencyEdgeIDs refuses a dependency link written under
// an ID another owner's, another page's or another data source's link holds.
func refuseTakenGraphDependencyEdgeIDs(app core.App, owner, pageID string, written graphWrittenIDs) error {
	if len(written.edges) == 0 {
		return nil
	}
	ids := make(map[string]struct{}, len(written.edges))
	for id := range written.edges {
		ids[id] = struct{}{}
	}
	stored, err := loadGraphReferences(app, "leafen_database_dependency_edges", ids)
	if err != nil {
		return err
	}
	for id, link := range stored {
		if link.Owner != owner || link.PageID != pageID || link.DataSourceID != written.edges[id] {
			return graphIDTakenError()
		}
	}
	return nil
}

// refuseUnwrittenGraphRecord refuses an upsert that wrote nothing: its ID's
// record is another owner's, page's or source's, which its update leaves.
func refuseUnwrittenGraphRecord(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	written, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if written == 0 {
		return graphIDTakenError()
	}
	return nil
}
