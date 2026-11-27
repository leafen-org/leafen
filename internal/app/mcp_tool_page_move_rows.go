package app

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A database's item notion-move-pages moves into another data source moves
// as the screen's 옮기기 moves a row into another database: the row move
// operation (POST /database-rows/{rowId}/page/move), run as the connection's
// person (mcp_tool_native_call.go), its sub-items with it (the user,
// 2026-11-26: an agent asked to move a row into another database was
// refused — "not already a database's item" — and had to look the row move
// up). Its values for properties the other database has stay. As the screen
// asks only when a move would leave values behind, such a move is refused,
// naming them, and nothing moves — unless the call says drop_values, which
// Leafen's endpoint takes; it then moves without them, the answer's warning
// naming them.

// mcpRowMove is a database's item a move takes into another data source.
type mcpRowMove struct {
	index int
	rowID string
	// requestID names the move, one per call (mcpPageMoveMutationID).
	requestID string
	title     string
	revision  int
	// dropped are the properties whose values the move would leave behind.
	dropped []databaseRowPageMoveDroppedProperty
}

// admitMCPRowMove reads what moving the row into the data source would
// leave behind, refusing one that leaves values when the call doesn't say
// to drop them.
func admitMCPRowMove(
	ctx context.Context, app core.App, authority mcpAuthority, move *mcpRowMove, dataSourceID string, dropValues bool,
) error {
	row, err := authorizeDatabaseRowPage(app, authority.PrincipalID, move.rowID, permissionWorkspaceWrite)
	if err != nil {
		return mcpRowAuthorityRefusal(app, authority.PrincipalID, move.rowID, mcpPageConversionError(err))
	}
	move.revision = row.AuthorityRevision
	preview, err := mcpRowMoveCall(ctx, move, dataSourceID, true, nil)
	if err != nil {
		return err
	}
	move.dropped = preview.DroppedProperties
	if len(move.dropped) == 0 || dropValues {
		return nil
	}
	return mcpInvalid("%s isn't moved, nor is anything else: its values for %s would be left behind, the other database having no such properties. "+
		"Give that database those properties first, or call again with drop_values true to move it without them", cmp.Or(move.title, move.rowID), mcpRowMoveDroppedNames(move.dropped))
}

// moveMCPRow moves the admitted row into the data source.
func moveMCPRow(ctx context.Context, authority mcpAuthority, move mcpRowMove, dataSourceID string) (notionMCPMovedPage, error) {
	accepted := make([]string, 0, len(move.dropped))
	for _, property := range move.dropped {
		accepted = append(accepted, property.PropertyID)
	}
	moved, err := mcpRowMoveCall(ctx, &move, dataSourceID, false, accepted)
	if err != nil {
		return notionMCPMovedPage{}, err
	}
	page := notionMCPMovedPage{
		ID: move.rowID, Title: move.title, URL: mcpPageURL(authority, move.rowID),
		Parent: notionMCPMovedPageParent{Type: "data_source_id", DataSourceID: dataSourceID}, Duplicate: moved.Duplicate,
	}
	if len(move.dropped) > 0 {
		page.Warning = fmt.Sprintf("%s moved without its values for %s, which the other database has no properties for.",
			cmp.Or(move.title, move.rowID), mcpRowMoveDroppedNames(move.dropped))
	}
	return page, nil
}

// mcpRowMoveCall runs the row move operation, as a preview or for good.
func mcpRowMoveCall(
	ctx context.Context, move *mcpRowMove, dataSourceID string, preview bool, accepted []string,
) (databaseRowPageActionResponse, error) {
	result, err := mcpNativeCall(ctx, "POST", "/database-rows/"+move.rowID+"/page/move", "", databaseRowPageActionInput{
		RequestID: move.requestID, ExpectedAuthorityRevision: move.revision, TargetDataSourceID: dataSourceID, Preview: preview, AcceptDroppedPropertyIDs: accepted,
	})
	if err != nil {
		return databaseRowPageActionResponse{}, err
	}
	if refusal := mcpNativeRefusalNamed(result, mcpMovePagesFieldNames); refusal != nil {
		return databaseRowPageActionResponse{}, refusal
	}
	answer := databaseRowPageActionResponse{}
	encoded, err := json.Marshal(result.Body)
	if err == nil {
		err = json.Unmarshal(encoded, &answer)
	}
	if err != nil {
		return databaseRowPageActionResponse{}, failedBecause(errMCPToolFailed, err)
	}
	return answer, nil
}

// mcpRowMoveDroppedNames names the properties a move leaves behind.
func mcpRowMoveDroppedNames(dropped []databaseRowPageMoveDroppedProperty) string {
	names := make([]string, 0, len(dropped))
	for _, property := range dropped {
		names = append(names, fmt.Sprintf("%q", cmp.Or(property.Name, property.PropertyID)))
	}
	return strings.Join(names, ", ")
}
