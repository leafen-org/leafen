package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerDatabaseAutomationRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+databaseAutomationListPath, listDatabaseAutomations)
	event.Router.POST(apiRoot+databaseAutomationCreatePath, createDatabaseAutomation).
		Bind(apis.BodyLimit(interactiveAutomationMaxBodyBytes))
	event.Router.PATCH(apiRoot+databaseAutomationUpdatePath, updateDatabaseAutomation).
		Bind(apis.BodyLimit(interactiveAutomationMaxBodyBytes))
	event.Router.DELETE(apiRoot+databaseAutomationDeletePath, deleteDatabaseAutomation).
		Bind(apis.BodyLimit(interactiveAutomationMaxBodyBytes))
	event.Router.POST(apiRoot+databaseAutomationEnabledPath, setDatabaseAutomationEnabled).
		Bind(apis.BodyLimit(interactiveAutomationMaxBodyBytes))
	event.Router.POST(apiRoot+databaseAutomationResumePath, resumeDatabaseAutomation).
		Bind(apis.BodyLimit(interactiveAutomationMaxBodyBytes))
}

func databaseAutomationRouteIdentity(event *core.RequestEvent) (string, string, string) {
	return strings.TrimSpace(event.Request.PathValue("pageId")),
		strings.TrimSpace(event.Request.PathValue("databaseId")),
		strings.TrimSpace(event.Request.PathValue("automationId"))
}

// databaseAutomationActor is the person signed in or through their key; ok false when
// it answered 401 instead, err then that answer's write.
func databaseAutomationActor(event *core.RequestEvent) (*core.Record, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return nil, false, apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	return user, true, nil
}

func writeDatabaseAutomationError(event *core.RequestEvent, err error) error {
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "database-automation")
	}
	if storageErr, ok := persistentWriteStorageAPIError(
		event, err, persistentWriteDatabaseAutomation, "transaction",
	); ok {
		return storageErr
	}
	switch {
	case isPermissionDenied(err), errors.Is(err, errDatabaseAutomationNotFound):
		return apiError(event, http.StatusNotFound, errorCodeDatabaseAutomationNotFound, "Automation not found.")
	case errors.Is(err, errDatabaseAutomationUnsupported):
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseAutomationUnsupported, "This automation isn't supported yet.")
	case errors.Is(err, errDatabaseAutomationInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseAutomationInvalid, refusalMessage("Check the automation details.", refusalReason(err)))
	case errors.Is(err, errDatabaseAutomationMutationIDReused):
		return apiError(event, http.StatusConflict, errorCodeDatabaseAutomationMutationIDReused, "This ID was used for a different automation change.")
	case errors.Is(err, errDatabaseAutomationConflict):
		return apiError(event, http.StatusConflict, errorCodeDatabaseAutomationConflict, databaseAutomationConflictMessage(err))
	default:
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeDatabaseAutomationUnavailable, "Couldn't process the automation.", err)
	}
}

func listDatabaseAutomations(event *core.RequestEvent) error {
	user, ok, err := databaseAutomationActor(event)
	if !ok {
		return err
	}
	pageID, databaseID, _ := databaseAutomationRouteIdentity(event)
	response, err := listDatabaseAutomationDefinitions(event.App, user.Id, pageID, databaseID, event.Request.URL.Query().Get("after"))
	if err != nil {
		return writeDatabaseAutomationError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func createDatabaseAutomation(event *core.RequestEvent) error {
	user, ok, err := databaseAutomationActor(event)
	if !ok {
		return err
	}
	input := databaseAutomationDefinitionInput{}
	if err := event.BindBody(&input); err != nil {
		return writeDatabaseAutomationError(event, bodyRefusal(errDatabaseAutomationInvalid, "database automation", err))
	}
	pageID, databaseID, _ := databaseAutomationRouteIdentity(event)
	response, err := createDatabaseAutomationDefinition(event.App, user.Id, pageID, databaseID, input)
	if err != nil {
		return writeDatabaseAutomationError(event, err)
	}
	status := http.StatusCreated
	if response.Duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func updateDatabaseAutomation(event *core.RequestEvent) error {
	user, ok, err := databaseAutomationActor(event)
	if !ok {
		return err
	}
	input := databaseAutomationDefinitionInput{}
	sent := databaseAutomationSentFields{}
	if err := rereadableRequestBody(event); err != nil {
		return writeDatabaseAutomationError(event, bodyRefusal(errDatabaseAutomationInvalid, "database automation", err))
	}
	if err := event.BindBody(&input); err != nil {
		return writeDatabaseAutomationError(event, bodyRefusal(errDatabaseAutomationInvalid, "database automation", err))
	}
	if err := event.BindBody(&sent); err != nil {
		return writeDatabaseAutomationError(event, bodyRefusal(errDatabaseAutomationInvalid, "database automation", err))
	}
	pageID, databaseID, automationID := databaseAutomationRouteIdentity(event)
	response, err := updateDatabaseAutomationDefinition(event.App, user.Id, pageID, databaseID, automationID, input, sent)
	if err != nil {
		return writeDatabaseAutomationError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func mutateDatabaseAutomationState(event *core.RequestEvent, operation string) error {
	user, ok, err := databaseAutomationActor(event)
	if !ok {
		return err
	}
	input := databaseAutomationMutationInput{}
	if err := event.BindBody(&input); err != nil {
		return writeDatabaseAutomationError(event, bodyRefusal(errDatabaseAutomationInvalid, "database automation change", err))
	}
	pageID, databaseID, automationID := databaseAutomationRouteIdentity(event)
	response, err := setDatabaseAutomationDefinitionState(event.App, user.Id, pageID, databaseID, automationID, operation, input)
	if err != nil {
		return writeDatabaseAutomationError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func deleteDatabaseAutomation(event *core.RequestEvent) error {
	return mutateDatabaseAutomationState(event, "delete")
}

func setDatabaseAutomationEnabled(event *core.RequestEvent) error {
	return mutateDatabaseAutomationState(event, "enabled")
}

func resumeDatabaseAutomation(event *core.RequestEvent) error {
	return mutateDatabaseAutomationState(event, "resume")
}
