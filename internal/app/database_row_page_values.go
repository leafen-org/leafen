package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A row page's property values are written through the database's page, as
// the database's own edits are, with the row page's authority: someone a
// row page is shared with for editing writes its values without access to
// the rest of the database. Values the database computes are not written,
// and relations only by people who edit the database's page itself, since a
// relation reaches rows the row page's share does not.

const databaseRowPageValuesLimit = 100

var errDatabaseRowPageValuesInvalid = errors.New("database row page values invalid")

var databaseRowPageComputedValueTypes = map[string]bool{
	"formula": true, "rollup": true, "created_time": true, "created_by": true,
	"last_edited_time": true, "last_edited_by": true, "unique_id": true, "button": true,
	"last_visited_time": true, graphValueTypeRelation: true,
}

// databaseRowPageMutationAuthorization authorizes a change to a row through
// the database's page: by that page's role, or else by the row page's own
// share, carrying the page's record for the change's bookkeeping.
func databaseRowPageMutationAuthorization(
	app core.App, principalID, rowID string, requirement permissionRequirement,
) (databaseRowPageAuthority, pageAuthorization, error) {
	authority, err := authorizeDatabaseRowPage(app, principalID, rowID, requirement)
	if err != nil {
		return databaseRowPageAuthority{}, pageAuthorization{}, err
	}
	page, err := authorizePageBody(app, principalID, authority.ContainerPageID, requirement)
	if isPermissionDenied(err) {
		page, err = authorizeDatabaseRowPageGrant(app, principalID, authority, pageAccessRoleForWorkspaceRequirement(requirement))
		if err == nil {
			page.Page, err = app.FindRecordById(pagesCollection, authority.ContainerPageID)
		}
	}
	if isPermissionDenied(err) {
		// Read but not changed: the row is there, only not theirs to change.
		if requirement != permissionWorkspaceRead {
			return databaseRowPageAuthority{}, pageAuthorization{}, errDatabaseRowPageReadOnly
		}
		return databaseRowPageAuthority{}, pageAuthorization{}, errDatabaseRowPageNotFound
	}
	if err != nil {
		return databaseRowPageAuthority{}, pageAuthorization{}, err
	}
	return authority, page, nil
}

// databaseRowRelationsWritable is whether the person writes a row's
// relations: they edit the database's page itself, so the rows a relation
// reaches are theirs to reach.
func databaseRowRelationsWritable(app core.App, principalID, containerPageID string) bool {
	_, err := authorizePageBody(app, principalID, containerPageID, permissionWorkspaceWrite)
	return err == nil
}

// planDatabaseRowPageValues checks the values a request writes against the
// row's properties; relations only when relations says so.
func planDatabaseRowPageValues(
	app core.App, authority databaseRowPageAuthority, values map[string]graphRowValue, relations bool,
) (map[string]graphRowValue, error) {
	if len(values) == 0 || len(values) > databaseRowPageValuesLimit {
		return nil, refuse(errDatabaseRowPageValuesInvalid, "values holds 1–%d properties, by property name or ID", databaseRowPageValuesLimit)
	}
	properties, err := loadDatabaseRowPageProperties(app, authority, authority.ContainerPageID, authority.DataSourceID)
	if err != nil {
		return nil, err
	}
	// By its property's ID or name (database_row_values_named.go).
	values, err = databaseRowValuesByID(newDatabaseRowPropertyKeys(app, authority.DataSourceID, properties), "values", values)
	if err != nil {
		return nil, err
	}
	types, names := make(map[string]string, len(properties)), make(map[string]string, len(properties))
	for _, property := range properties {
		types[property.ID], names[property.ID] = property.Type, property.Name
	}
	planned := make(map[string]graphRowValue, len(values))
	for propertyID, value := range values {
		propertyType := types[propertyID]
		value = databaseRowValueTyped(value, propertyType)
		switch {
		case value.Type != propertyType:
			return nil, refuse(errDatabaseRowPageValuesInvalid, "values.%s (column %q).type is %q, the property's type",
				propertyID, names[propertyID], propertyType)
		case databaseRowPageComputedValueTypes[propertyType] && !(relations && propertyType == graphValueTypeRelation):
			return nil, refuse(errDatabaseRowPageValuesInvalid, "values.%s (column %q) is a %s property, which isn't written here",
				propertyID, names[propertyID], propertyType)
		}
		planned[propertyID] = value
	}
	return planned, nil
}

func writeDatabaseRowPageValues(ygo *pocketBaseYgoPersistence) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		user, ok := authenticatedUser(event)
		if !ok {
			return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		input := databaseRowPageValuesInput{}
		rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
		if err := event.BindBody(&input); err != nil {
			return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid,
				refusalMessage("Check the database row values.", databaseRowPageValuesBodyReason(err)))
		}
		// The server makes one left out (native_api_body_mutation_id.go).
		mutationID := strings.TrimSpace(input.MutationID)
		if rowID == "" || mutationID == "" || len(mutationID) > 128 {
			return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, refusalMessage("Check the database row values.",
				"mutationId is a new id of yours, 1–128 bytes, so the same change sent again isn't made twice"))
		}
		authority, _, err := databaseRowPageMutationAuthorization(event.App, user.Id, rowID, permissionWorkspaceWrite)
		if err != nil {
			return databaseRowPageValuesError(event, err)
		}
		receipt, err := ygo.applyContentGraphMutationContext(
			event.Request.Context(), user.Id, authority.ContainerPageID,
			graphMutationInput{MutationID: "database-row-values:" + rowID + ":" + mutationID},
			contentGraphMutationHooks{
				// A locked database page keeps its values.
				EnforcePageLock:  true,
				DateCommandRowID: rowID,
				// Its options checked; an agent's values name options their columns have (database_option_values.go).
				CheckOperations: databaseOptionChangesChecked(event.Get(userKeyAuthorityKey) != nil),
				Authorize: func(txApp core.App) (pageAuthorization, error) {
					current, page, err := databaseRowPageMutationAuthorization(txApp, user.Id, rowID, permissionWorkspaceWrite)
					if err == nil && current.ContainerPageID != authority.ContainerPageID {
						err = errDatabaseRowPageConflict
					}
					return page, err
				},
				Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
					current, _, err := databaseRowPageMutationAuthorization(txApp, user.Id, rowID, permissionWorkspaceWrite)
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					if input.RestoreDateCommand != "" {
						if len(input.Values) > 0 || len(input.Options) > 0 {
							return contentGraphMutationPreparation{}, refuse(errDatabaseRowPageValuesInvalid, "restoreDateCommand goes without values or options")
						}
						return contentGraphMutationPreparation{Operations: []graphOperation{{
							Kind:        graphOperationKindRestoreDateCommand,
							DateRestore: &graphDateCommandRestore{MutationID: input.RestoreDateCommand, Forward: input.Redo},
						}}}, nil
					}
					// A value sent with the one its writer read is written only over
					// that; both, and new options, by property name or ID.
					if err := databaseRowPageReadByID(txApp, current, &input); err != nil {
						return contentGraphMutationPreparation{}, err
					}
					if err := databaseRowValuesAsRead(txApp, current, input.Expected); err != nil {
						return contentGraphMutationPreparation{}, err
					}
					values, err := planDatabaseRowPageValues(txApp, current, input.Values,
						databaseRowRelationsWritable(txApp, user.Id, current.ContainerPageID))
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					// The options a value adds come first, so the value names
					// them; a database its own lock holds keeps the options it
					// has, from a row page's editor too (databaseLockedFor),
					// who hears whether they can unlock it (lock_refusal.go).
					operations, err := planDatabaseRowPageNewOptions(txApp, current, input.Options, values)
					if locked := false; err == nil && len(operations) > 0 {
						if locked, err = databaseRowPageDatabaseLockedFor(txApp, user.Id, current); err == nil && locked {
							err = databaseLockedRefusal(txApp, user.Id, current.ContainerPageID)
						}
					}
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					written := graphMutationInput{Operations: append(operations, graphOperation{
						Kind: graphOperationKindUpsertRow,
						Row: &graphDatabaseRow{
							ID: current.RowID, DataSourceID: current.DataSourceID,
							SubitemParentID: optionalDatabaseRowParent(current.SubitemParentID),
							Position:        current.Position, Values: values,
						},
					})}
					// A value written as its writer read it — a title from its
					// rename box or heading, a mention they can't open in marks —
					// keeps the mention as stored, as a graph write does
					// (content_graph_writer_mentions.go).
					restoreGraphWriteAsStored(txApp, &written)
					return contentGraphMutationPreparation{Operations: written.Operations}, nil
				},
			},
		)
		if err != nil {
			return databaseRowPageValuesError(event, err)
		}
		current, err := authorizeDatabaseRowPage(event.App, user.Id, rowID, permissionWorkspaceRead)
		if err != nil {
			return databaseRowPageValuesError(event, err)
		}
		return event.JSON(http.StatusOK, databaseRowPageActionResponse{
			RowID: current.RowID, ContainerPageID: current.ContainerPageID, AuthorityRevision: current.AuthorityRevision,
			DateCommand: receipt.DateCommand, Duplicate: receipt.Duplicate,
			DependencyShifts: receipt.DependencyShifts, DependencyShiftTasks: receipt.DependencyShiftTasks,
			DependencyShiftsDenied: receipt.DependencyShiftsDenied,
		})
	}
}

// databaseRowPageValuesBodyReason is why a body of a row's values doesn't
// read — the field and what it takes — and, for a value, how one is
// written: never its mutationId, which it may well hold (2026-11-26: a
// select given as its option's name, {"values":{"Category":"Category 47"}},
// was told "mutationId is a new id of yours", and the agent gave up).
func databaseRowPageValuesBodyReason(err error) string {
	reason := bodyRefusalReason("row's values", err)
	if strings.HasPrefix(reason, "values.") || strings.HasPrefix(reason, "expected.") {
		reason += `; each is an object, {"value": …} with a select's option ID, as row.values of GET /database-rows/{rowId}/page holds it`
	}
	return reason
}

func databaseRowPageValuesError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errDatabaseRowPageReadOnly):
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You can view this database row but not change it.")
	case errors.Is(err, errDatabaseRowPageNotFound):
		return apiError(event, http.StatusNotFound, errorCodeDatabaseRowPageNotFound, "Database row not found.")
	case errors.Is(err, errDatabaseRowPageInTrash):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageInTrash, "Restore the database row before editing it.")
	case errors.Is(err, errDatabaseRowPageValuesInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, refusalMessage("Check the database row values.", refusalReason(err)))
	case errors.Is(err, errDatabaseRowPageConflict):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageConflict, "The database row changed. Try again.")
	case errors.Is(err, errDatabaseRowPageValueChanged):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageValueChanged, "The value changed since it was read.")
	}
	graphError := &graphRequestError{}
	if errors.As(err, &graphError) && graphError.status == http.StatusNotFound {
		return apiError(event, http.StatusNotFound, errorCodeDatabaseRowPageNotFound, "Database row not found.")
	}
	if errors.As(err, &graphError) && graphError.status == http.StatusBadRequest {
		// The row's values as this request names them, not as the content
		// change it became does.
		reason := graphError.reason
		if at := strings.Index(reason, ".row."); strings.HasPrefix(reason, "operations[") && at > 0 {
			reason = reason[at+len(".row."):]
		}
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, refusalMessage("Check the database row values.", reason))
	}
	if isLockRefusal(err) {
		return answerLockRefusal(event, err)
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeDatabaseRowPageFailed,
		"Couldn't save the database row.", "database_row_page_values", "write", errorCodeDatabaseRowPageFailed, err)
}
