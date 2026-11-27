package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func registerDatabasePropertyTrashRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.GET(apiRoot+databaseDeletedPropertyListPath, readDatabaseDeletedProperties)
	event.Router.GET(apiRoot+databaseUniqueIDStatePath, serveDatabaseUniqueIDState)
	event.Router.POST(apiRoot+databasePropertyTrashPath, func(event *core.RequestEvent) error {
		return ygo.serveDatabasePropertyTrashChange(event, true)
	}).Bind(apis.BodyLimit(databaseDataSourceMaxBodyBytes))
	event.Router.POST(apiRoot+databasePropertyRestorePath, func(event *core.RequestEvent) error {
		return ygo.serveDatabasePropertyTrashChange(event, false)
	}).Bind(apis.BodyLimit(databaseDataSourceMaxBodyBytes))
	event.Router.POST(apiRoot+databasePropertyDeletePermanentlyPath, ygo.serveDatabasePropertyDeletePermanently).
		Bind(apis.BodyLimit(databaseDataSourceMaxBodyBytes))
	event.Router.POST(apiRoot+databaseViewShownPath, ygo.serveDatabaseViewShown).
		Bind(apis.BodyLimit(databaseDataSourceMaxBodyBytes))
}

// Deleting and restoring a property are content graph mutations of its page,
// named by the request, with no graph operations of their own (the views on
// it keep naming it): the page revision moves, so query results and other
// open pages read the schema again, and the editor receives the same receipt
// as for its other graph writes.
// A two-way relation to another database deleted with relatedProperty
// takes its other side along or ends their sync
// (database_property_trash_pairs.go); ending it makes this one one-way in
// the same change, which the pair runner doesn't mirror, and restoring it
// pairs the two again while its other side allows.
func (p *pocketBaseYgoPersistence) changeDatabasePropertyTrash(
	ctx context.Context,
	principalID, pageID, propertyID, requestID, relatedProperty string,
	trash bool,
) (graphMutationReceipt, error) {
	action := "restore"
	if trash {
		action = "trash"
	}
	if !trash || (relatedProperty != databasePropertyRelatedDelete && relatedProperty != databasePropertyRelatedKeep) {
		relatedProperty = ""
	}
	target := databasePropertyTrashTarget{}
	partner, paired := databaseRelationPartner{}, false
	now := time.Now().UTC()
	return p.applyContentGraphMutationContext(ctx, principalID, pageID,
		graphMutationInput{MutationID: requestID},
		contentGraphMutationHooks{
			SkipRelationPairSync: relatedProperty == databasePropertyRelatedKeep,
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				authorization, err := authorizePageBody(txApp, principalID, pageID, permissionWorkspaceWrite)
				if err != nil {
					return authorization, err
				}
				// A locked database keeps its properties (page_lock_database_structure.go).
				if locked, err := pageDatabaseStructureLockedFor(txApp, authorization.Page, principalID); err != nil || locked {
					if err == nil {
						err = databaseLockedRefusal(txApp, principalID, pageID)
					}
					return pageAuthorization{}, err
				}
				// A restricted property moves only for whoever may edit it.
				if err := refuseRestrictedPropertyEdit(txApp, principalID, propertyID); err != nil {
					return pageAuthorization{}, err
				}
				return authorization, nil
			},
			ResolveRequestHash: func(_ core.App, _ pageAuthorization, _ *crdt.Doc) (string, error) {
				digest := sha256.Sum256([]byte("database-property-" + action + "\x00" + pageID + "\x00" + propertyID))
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				var err error
				target, err = loadDatabasePropertyTrashTarget(txApp, pageID, propertyID)
				if err != nil || !trash && (target.DeletedAt == 0 || target.Deleting != 0) {
					return contentGraphMutationPreparation{}, err
				}
				// One "이 속성만 삭제" made one-way pairs with its other side again
				// (database_property_trash_pairs.go).
				if !trash {
					operations, err := databaseRelationRepairOperation(txApp, target)
					return contentGraphMutationPreparation{Operations: operations}, err
				}
				// A wiki's Owner and Verification properties stay with the wiki.
				kept, err := wikiPropertyKept(txApp, target.ID)
				if err == nil && kept {
					err = errWikiPropertyKept
				}
				if err != nil || relatedProperty == "" || target.DeletedAt != 0 {
					return contentGraphMutationPreparation{}, err
				}
				var property databaseRelationPairTargetProperty
				var config map[string]any
				partner, property, config, paired, err = loadDatabaseRelationPartner(txApp, target)
				if err != nil || !paired || relatedProperty != databasePropertyRelatedKeep {
					return contentGraphMutationPreparation{}, err
				}
				operations, err := databaseRelationUnpairOperation(property, config)
				return contentGraphMutationPreparation{Operations: operations}, err
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				if !trash {
					return restoreDatabaseProperty(txApp, target)
				}
				if err := trashDatabaseProperty(txApp, target, principalID, now); err != nil || !paired {
					return err
				}
				return trashDatabaseRelationPartner(txApp, target, partner, relatedProperty, principalID, requestID, now)
			},
		},
	)
}

func bindDatabasePropertyTrashRequest(event *core.RequestEvent) (string, string, databasePropertyTrashRequest, error) {
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	propertyID := strings.TrimSpace(event.Request.PathValue("propertyId"))
	input := databasePropertyTrashRequest{}
	if pageID == "" || propertyID == "" || event.BindBody(&input) != nil {
		return "", "", input, errDatabasePropertyTrashRequestInvalid
	}
	// One without its request ID is told so (api_missing_input.go).
	if input.RequestID = strings.TrimSpace(input.RequestID); input.RequestID == "" {
		return "", "", input, databasePropertyRequestIDRefused()
	}
	if len(input.RequestID) > maxGraphIdentifierLength {
		return "", "", input, errDatabasePropertyTrashRequestInvalid
	}
	return pageID, propertyID, input, nil
}

var errDatabasePropertyTrashRequestInvalid = errors.New(errorCodeDatabaseDataSourceInvalid)

// databasePropertyRequestIDRefused is the refusal of a property's or a
// view's change sent without its request ID.
func databasePropertyRequestIDRefused() error {
	return refusedMissingInput(errorCodeDatabaseDataSourceInvalid, "requestId", "a new id of yours, 1 to 128 characters, so the same change sent again isn't made twice.")
}

func (p *pocketBaseYgoPersistence) serveDatabasePropertyTrashChange(event *core.RequestEvent, trash bool) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID, propertyID, input, err := bindDatabasePropertyTrashRequest(event)
	if err != nil {
		return apiErrorFrom(event, http.StatusBadRequest, errorCodeDatabaseDataSourceInvalid, "Check the property deletion details.", err)
	}
	receipt, err := p.changeDatabasePropertyTrash(event.Request.Context(), user.Id, pageID, propertyID,
		input.RequestID, input.RelatedProperty, trash)
	if err != nil {
		return writeDatabasePropertyTrashError(event, err)
	}
	return event.JSON(http.StatusOK, receipt)
}

// serveDatabasePropertyDeletePermanently deletes a property from Deleted
// properties for good. In the same change the views on the page that still
// name it (or the other side of its pair) let go of it
// (withDatabaseViewsOffProperties), so none is left naming a property that
// is gone; then the deletion lifecycle takes it.
func (p *pocketBaseYgoPersistence) serveDatabasePropertyDeletePermanently(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID, propertyID, input, err := bindDatabasePropertyTrashRequest(event)
	requestID := input.RequestID
	if err != nil {
		return apiErrorFrom(event, http.StatusBadRequest, errorCodeDatabaseDataSourceInvalid, "Check the property deletion details.", err)
	}
	response := databasePropertyTrashResponse{PropertyID: propertyID, Status: "deleting"}
	// A key's call asks its person only for a property this would delete
	// (user_key_acting_ask.go), by the checks the change makes.
	if _, err := authorizeDatabasePropertyDeletion(event.App, user.Id, pageID, propertyID); err != nil {
		return writeDatabasePropertyTrashError(event, err)
	}
	if _, err := databasePropertyDeletionTarget(event.App, pageID, propertyID); err != nil {
		return writeDatabasePropertyTrashError(event, err)
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	_, err = p.applyContentGraphMutationContext(event.Request.Context(), user.Id, pageID,
		graphMutationInput{MutationID: requestID},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				return authorizeDatabasePropertyDeletion(txApp, user.Id, pageID, propertyID)
			},
			ResolveRequestHash: func(_ core.App, _ pageAuthorization, _ *crdt.Doc) (string, error) {
				digest := sha256.Sum256([]byte("database-property-delete-permanently\x00" + pageID + "\x00" + propertyID))
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				target, err := databasePropertyDeletionTarget(txApp, pageID, propertyID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				if target.Deleting != 0 {
					response.Duplicate = true
					return contentGraphMutationPreparation{Deferred: true}, nil
				}
				operations, err := withDatabaseViewsOffProperties(txApp, pageID, []string{propertyID}, nil)
				return contentGraphMutationPreparation{Operations: operations}, err
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				return deleteDatabasePropertyPermanently(txApp, pageID, propertyID, user.Id)
			},
		},
	)
	if err != nil {
		return writeDatabasePropertyTrashError(event, err)
	}
	// It goes on behind, followed at GET /jobs/{id} by its job.
	if job, err := loadDatabaseMutationDeletionByRequest(event.App, user.Id, databasePropertyDeletionRequestID(propertyID)); err == nil {
		response.JobID = job.ID
	}
	return event.JSON(http.StatusAccepted, response)
}

// authorizeDatabasePropertyDeletion is whether the person may delete a
// property of the page's database for good: they edit the page, the property
// isn't one restricted from them, and the database isn't locked.
func authorizeDatabasePropertyDeletion(app core.App, principalID, pageID, propertyID string) (pageAuthorization, error) {
	authorization, err := authorizePageBody(app, principalID, pageID, permissionWorkspaceWrite)
	if err != nil {
		return authorization, err
	}
	if err := refuseRestrictedPropertyEdit(app, principalID, propertyID); err != nil {
		return pageAuthorization{}, err
	}
	if locked, err := pageDatabaseStructureLockedFor(app, authorization.Page, principalID); err != nil || locked {
		if err == nil {
			err = databaseLockedRefusal(app, principalID, pageID)
		}
		return pageAuthorization{}, err
	}
	return authorization, nil
}

// databasePropertyDeletionTarget is the property to delete for good, which
// is in Deleted properties: one that isn't is to be moved there first.
func databasePropertyDeletionTarget(app core.App, pageID, propertyID string) (databasePropertyTrashTarget, error) {
	target, err := loadDatabasePropertyTrashTarget(app, pageID, propertyID)
	if err == nil && target.DeletedAt == 0 {
		err = errDatabasePropertyNotDeleted
	}
	return target, err
}

// errDatabasePropertyNotDeleted is a property to delete for good that isn't
// in Deleted properties, as a page to delete for good isn't in the trash;
// it is still a conflict to callers that only ask that.
var errDatabasePropertyNotDeleted = fmt.Errorf("%w: not in Deleted properties", errDatabaseMutationConflict)

// serveDatabaseViewShown keeps what a view shows by once it is shown: the
// default a board, calendar or timeline naming none draws, or the fallback
// for a property in Deleted properties, as Notion's writes them once shown
// (databaseViewShownConfig). The server decides from what it holds, whatever
// the page that showed it had read; "changed" says whether the view changed.
func (p *pocketBaseYgoPersistence) serveDatabaseViewShown(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	viewID := strings.TrimSpace(event.Request.PathValue("viewId"))
	input := databasePropertyTrashRequest{}
	if err := event.BindBody(&input); err != nil || pageID == "" || viewID == "" {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseDataSourceInvalid, "Check the view details.")
	}
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" {
		return apiErrorFrom(event, http.StatusBadRequest, errorCodeDatabaseDataSourceInvalid, "Check the view details.", databasePropertyRequestIDRefused())
	}
	if len(requestID) > maxGraphIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseDataSourceInvalid, "Check the view details.")
	}
	response := databaseViewShownResponse{}
	_, err := p.applyContentGraphMutationContext(event.Request.Context(), user.Id, pageID,
		graphMutationInput{MutationID: requestID},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				authorization, err := authorizePageBody(txApp, user.Id, pageID, permissionWorkspaceWrite)
				if err != nil {
					return authorization, err
				}
				// A locked database keeps its views as they are (page_lock_database_structure.go).
				if locked, err := pageDatabaseStructureLockedFor(txApp, authorization.Page, user.Id); err != nil || locked {
					if err == nil {
						err = databaseLockedRefusal(txApp, user.Id, pageID)
					}
					return pageAuthorization{}, err
				}
				return authorization, nil
			},
			ResolveRequestHash: func(_ core.App, _ pageAuthorization, _ *crdt.Doc) (string, error) {
				digest := sha256.Sum256([]byte("database-view-shown\x00" + pageID + "\x00" + viewID))
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				operations, err := databaseViewShownChange(txApp, pageID, viewID, user.Id)
				response.Changed = len(operations) > 0
				return contentGraphMutationPreparation{Operations: operations, Deferred: err == nil && len(operations) == 0}, err
			},
		},
	)
	if err != nil {
		return writeDatabasePropertyTrashError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func readDatabaseDeletedProperties(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	dataSourceID := strings.TrimSpace(event.Request.PathValue("dataSourceId"))
	if _, err := authorizePageBody(event.App, user.Id, pageID, permissionWorkspaceRead); err != nil {
		return writeDatabasePropertyTrashError(event, err)
	}
	items, next, err := listDatabaseDeletedProperties(event.App, pageID, dataSourceID, event.Request.URL.Query().Get("cursor"))
	if errors.Is(err, errDatabaseDeletedPropertyCursor) {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseDataSourceInvalid, "Give the nextCursor of the last list, or none to start.")
	}
	if err != nil {
		return writeDatabasePropertyTrashError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, databaseDeletedPropertyList{Items: items, NextCursor: next})
}

func writeDatabasePropertyTrashError(event *core.RequestEvent, err error) error {
	requestError := &graphRequestError{}
	switch {
	case isPermissionDenied(err) || errors.Is(err, errDatabaseMutationNotFound):
		return apiError(event, http.StatusNotFound, errorCodePropertyNotFound, "Property not found.")
	case errors.Is(err, errDatabasePropertyInUse):
		return apiError(event, http.StatusConflict, errorCodeDatabasePropertyInUse, "A title can't be deleted.")
	case errors.Is(err, errDatabasePropertyNotDeleted):
		return apiError(event, http.StatusConflict, errorCodeDatabaseDataSourceConflict, "Move the property to Deleted properties first.")
	case errors.Is(err, errDatabaseMutationConflict), errors.Is(err, errDatabaseMutationRequestIDReused):
		return apiError(event, http.StatusConflict, errorCodeDatabaseDataSourceConflict, "The property changed. Try again.")
	case errors.As(err, &requestError):
		return apiError(event, requestError.status, requestError.code, requestError.message)
	default:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeDatabaseDataSourceConflict,
			"Couldn't change the property deletion state.", "database_property_trash", "change",
			errorCodeDatabaseDataSourceConflict, err,
		)
	}
}
