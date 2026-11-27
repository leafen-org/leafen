package app

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Many rows of one database page changed in one change — an app's table
// helper writing a hundred rows at a time spends one write of its budget —
// each as a row's values are written (database_row_page_values.go), for
// people and apps who edit that page. Relations are theirs to write.

func writeDatabaseRowValuesBatch(ygo *pocketBaseYgoPersistence) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		user, ok := authenticatedUser(event)
		if !ok {
			return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
		input := databaseRowValuesBatchInput{}
		if err := event.BindBody(&input); err != nil {
			return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid,
				refusalMessage("Check the database rows' values.", bodyRefusalReason("rows' values", err)))
		}
		// One without its change's ID or its rows is told so (api_missing_input.go).
		if strings.TrimSpace(input.MutationID) == "" {
			return refuseMissingInput(event, errorCodeDatabaseRowPageInvalid, "mutationId",
				"a new id of yours, 1–128 bytes, so the same change sent again isn't made twice.")
		}
		if len(input.Rows) == 0 {
			return refuseMissingInput(event, errorCodeDatabaseRowPageInvalid, "rows",
				fmt.Sprintf("the rows to change, 1–%d, each its rowId and values.", databaseRowValuesBatchRows))
		}
		reason := ""
		if len(input.MutationID) > 128 {
			reason = "mutationId is a new id of yours, 1–128 bytes, so the same change sent again isn't made twice"
		} else if len(input.Rows) == 0 || len(input.Rows) > databaseRowValuesBatchRows {
			reason = fmt.Sprintf("rows lists 1–%d rows, %d here", databaseRowValuesBatchRows, len(input.Rows))
		}
		seen := map[string]int{}
		for index, row := range input.Rows {
			input.Rows[index].RowID = strings.TrimSpace(row.RowID)
			if first, twice := seen[input.Rows[index].RowID]; reason == "" && twice {
				reason = fmt.Sprintf("rows[%d] names the row rows[%d] names: one entry per row", index, first)
			} else if reason == "" && input.Rows[index].RowID == "" {
				reason = fmt.Sprintf("rows[%d] has no rowId", index)
			}
			seen[input.Rows[index].RowID] = index
		}
		if reason != "" || pageID == "" {
			return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, refusalMessage("Check the database rows' values.", reason))
		}
		receipt, err := ygo.applyContentGraphMutationContext(
			event.Request.Context(), user.Id, pageID,
			graphMutationInput{MutationID: "database-row-values:" + pageID + ":" + strings.TrimSpace(input.MutationID)},
			contentGraphMutationHooks{
				// A locked database page keeps its values.
				EnforcePageLock: true,
				// An agent's values name options their columns have (database_option_values.go).
				CheckOperations: databaseOptionChangesChecked(event.Get(userKeyAuthorityKey) != nil),
				// Someone who can't edit the database's page doesn't learn of it.
				Authorize: func(txApp core.App) (pageAuthorization, error) {
					page, err := authorizePageBody(txApp, user.Id, pageID, permissionWorkspaceWrite)
					if isPermissionDenied(err) {
						err = errDatabaseRowPageNotFound
					}
					return page, err
				},
				Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
					operations := make([]graphOperation, 0, len(input.Rows))
					for index, row := range input.Rows {
						current, _, err := databaseRowPageMutationAuthorization(txApp, user.Id, row.RowID, permissionWorkspaceWrite)
						if err == nil && current.ContainerPageID != pageID {
							err = errDatabaseRowPageNotFound
						}
						if err != nil {
							return contentGraphMutationPreparation{}, databaseRowValuesBatchRowRefusal(err, index, row.RowID)
						}
						values, err := planDatabaseRowPageValues(txApp, current, row.Values, true)
						if err != nil {
							return contentGraphMutationPreparation{}, databaseRowValuesBatchRowRefusal(err, index, row.RowID)
						}
						operations = append(operations, graphOperation{
							Kind: graphOperationKindUpsertRow,
							Row: &graphDatabaseRow{
								ID: current.RowID, DataSourceID: current.DataSourceID,
								SubitemParentID: optionalDatabaseRowParent(current.SubitemParentID),
								Position:        current.Position, Values: values,
							},
						})
					}
					return contentGraphMutationPreparation{Operations: operations}, nil
				},
			},
		)
		if err != nil {
			return databaseRowPageValuesError(event, err)
		}
		return event.JSON(http.StatusOK, databaseRowValuesBatchResponse{PageID: pageID, Rows: len(input.Rows), Duplicate: receipt.Duplicate})
	}
}

// databaseRowValuesBatchRowRefusal says which of the rows a refusal is
// about — its place in rows and its ID — before why, the change made for
// none of them (2026-11-26: an agent writing 200 rows, told only why, split
// them to find which). A row that isn't one of this database's is said so,
// as the route's not found; a failure is as it was.
func databaseRowValuesBatchRowRefusal(err error, index int, rowID string) error {
	switch {
	case errors.Is(err, errDatabaseRowPageNotFound):
		return refusedFor(http.StatusNotFound, errorCodeDatabaseRowPageNotFound, fmt.Sprintf(
			"Check rows[%d]: %s isn't a row of this database that you can open. Nothing was changed; take it out or fix its rowId and send them again.",
			index, strconvQuoteJS(rowID)))
	case errors.Is(err, errDatabaseRowPageReadOnly):
		return refusedFor(http.StatusForbidden, errorCodePermissionDenied, fmt.Sprintf(
			"Check rows[%d]: you can view %s but not change it. Nothing was changed; take it out and send the others again.",
			index, strconvQuoteJS(rowID)))
	case errors.Is(err, errDatabaseRowPageValuesInvalid):
		return refuse(errDatabaseRowPageValuesInvalid, "rows[%d] (%s): %s — nothing was changed; fix that row and send them again",
			index, rowID, strings.TrimSuffix(refusalReason(err), "."))
	}
	return err
}
