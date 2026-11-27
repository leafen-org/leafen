package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errDatabaseAutomationInvalid          = errors.New(errorCodeDatabaseAutomationInvalid)
	errDatabaseAutomationNotFound         = errors.New(errorCodeDatabaseAutomationNotFound)
	errDatabaseAutomationUnsupported      = errors.New(errorCodeDatabaseAutomationUnsupported)
	errDatabaseAutomationConflict         = errors.New(errorCodeDatabaseAutomationConflict)
	errDatabaseAutomationMutationIDReused = errors.New(errorCodeDatabaseAutomationMutationIDReused)
	errDatabaseAutomationUnavailable      = errors.New(errorCodeDatabaseAutomationUnavailable)
)

type normalizedDatabaseAutomationDefinition struct {
	Name           string
	Scope          databaseAutomationScope
	TriggerType    string
	Trigger        databaseAutomationTrigger
	TriggerJSON    string
	ActionJSON     string
	ActionDocument interactiveAutomationDocument
	NextRunAt      int64
}

func normalizeDatabaseAutomationDefinition(
	app core.App,
	authorization pageAuthorization,
	databaseID string,
	input databaseAutomationDefinitionInput,
) (normalizedDatabaseAutomationDefinition, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Scope.Type = strings.TrimSpace(input.Scope.Type)
	// A view as leafen_catalog and notion-fetch write it, view://<ID>
	// (native_api_data_source_id_aliases.go).
	input.Scope.ViewID = nativeIDWithDashes(nativeViewID(strings.TrimSpace(input.Scope.ViewID)))
	if err := databaseAutomationNameAndScopeRefusal(input.Name, input.Scope); err != nil {
		return normalizedDatabaseAutomationDefinition{}, err
	}
	if err := validateDatabaseAutomationSource(app, authorization, databaseID, input.Scope); err != nil {
		return normalizedDatabaseAutomationDefinition{}, err
	}
	normalized, err := normalizeDatabaseAutomationRule(input.Trigger, input.ActionDocument, time.Now().UTC())
	if err != nil {
		return normalizedDatabaseAutomationDefinition{}, err
	}
	// People's database's own rules (people_automation_source.go).
	if database, people, err := peopleDatabaseOfPage(app, authorization.Page.Id); err != nil {
		return normalizedDatabaseAutomationDefinition{}, err
	} else if people {
		if err := validatePeopleAutomationRule(normalized, database.EmailPropertyID, database.DataSourceID); err != nil {
			return normalizedDatabaseAutomationDefinition{}, err
		}
	}
	for _, action := range normalized.ActionDocument.Actions {
		// An app's action the person may run there, unattended.
		if action.Type == interactiveAutomationActionRunAppAction &&
			!automationAppActionAllowed(app, authorization, action) {
			return normalizedDatabaseAutomationDefinition{}, databaseAutomationRefused("actionDocument runs an app's action that isn't one the workspace's apps let this person run unattended")
		}
	}
	if err := enforceOrganizationDLPContent(
		app, authorization.Workspace.Organization.OrganizationID,
		map[string]any{"name": input.Name, "trigger": normalized.Trigger, "actionDocument": normalized.ActionDocument},
	); err != nil {
		return normalizedDatabaseAutomationDefinition{}, err
	}
	normalized.Name, normalized.Scope = input.Name, input.Scope
	return normalized, nil
}

// normalizeDatabaseAutomationRule checks an automation's trigger and steps on
// their own, as making one does and as an archive's come back: the next run
// a schedule makes is the first after now.
func normalizeDatabaseAutomationRule(
	trigger databaseAutomationTrigger, actionDocument json.RawMessage, now time.Time,
) (normalizedDatabaseAutomationDefinition, error) {
	input := databaseAutomationDefinitionInput{Trigger: trigger, ActionDocument: actionDocument}
	input.Trigger.Type = strings.TrimSpace(input.Trigger.Type)
	input.Trigger.Mode = strings.TrimSpace(input.Trigger.Mode)
	if input.Trigger.Type == "" && input.Trigger.Mode != "" {
		input.Trigger.Type = databaseAutomationTriggerEvents
	}
	nextRunAt := int64(0)
	switch input.Trigger.Type {
	case databaseAutomationTriggerEvents:
		if err := databaseAutomationEventsRefusal(input.Trigger); err != nil {
			return normalizedDatabaseAutomationDefinition{}, err
		}
		seen := map[string]struct{}{}
		for index := range input.Trigger.Conditions {
			condition := &input.Trigger.Conditions[index]
			condition.Type = strings.TrimSpace(condition.Type)
			condition.PropertyID = strings.TrimSpace(condition.PropertyID)
			condition.Operator = strings.TrimSpace(condition.Operator)
			if string(bytes.TrimSpace(condition.Value)) == "null" {
				condition.Value = nil
			}
			key := condition.Type + "\x00" + condition.PropertyID
			if _, duplicate := seen[key]; duplicate {
				return normalizedDatabaseAutomationDefinition{}, databaseAutomationRefused(
					"trigger.conditions[%d] names the same type and property as one before it", index)
			}
			seen[key] = struct{}{}
			if err := databaseAutomationConditionRefusal(index, *condition); err != nil {
				return normalizedDatabaseAutomationDefinition{}, err
			}
		}
	case databaseAutomationTriggerSchedule:
		normalizeDatabaseAutomationScheduleTrigger(&input.Trigger)
		if err := validateDatabaseAutomationScheduleTrigger(input.Trigger); err != nil {
			return normalizedDatabaseAutomationDefinition{}, refusalAt(err, "trigger.")
		}
		next, exists, err := nextDatabaseAutomationScheduleRun(input.Trigger, now)
		if err != nil {
			return normalizedDatabaseAutomationDefinition{}, databaseAutomationRefused("trigger's schedule has no run to make")
		}
		if exists {
			nextRunAt = next.UnixMilli()
		}
	default:
		return normalizedDatabaseAutomationDefinition{}, errDatabaseAutomationUnsupported
	}
	normalizedAction, err := normalizeDatabaseAutomationDocument(input.ActionDocument)
	if errors.Is(err, errInteractiveAutomationUnsupported) {
		return normalizedDatabaseAutomationDefinition{}, errDatabaseAutomationUnsupported
	}
	if err != nil {
		return normalizedDatabaseAutomationDefinition{}, databaseAutomationActionRefusal(err)
	}
	for _, action := range normalizedAction.Document.Actions {
		switch action.Type {
		case interactiveAutomationActionDefineVariables, interactiveAutomationActionEditProperty,
			interactiveAutomationActionAddPage, interactiveAutomationActionEditPages, interactiveAutomationActionArchivePages, interactiveAutomationActionSetPageIcon,
			interactiveAutomationActionSendNotification, interactiveAutomationActionSendEmail,
			interactiveAutomationActionSendWebhook, interactiveAutomationActionSendSlack, interactiveAutomationActionRunAppAction:
		default:
			return normalizedDatabaseAutomationDefinition{}, errDatabaseAutomationUnsupported
		}
		// A schedule sets off no page: what it edits or archives is a database's, and it sets no page's icon.
		if input.Trigger.Type == databaseAutomationTriggerSchedule && interactiveAutomationWritesTriggerPage(action) {
			return normalizedDatabaseAutomationDefinition{}, databaseAutomationRefused(
				"actionDocument edits, archives or sets the icon of the triggering page, which a schedule has none of: edit or archive pages of a database instead")
		}
	}
	triggerJSON, err := json.Marshal(input.Trigger)
	if err != nil {
		return normalizedDatabaseAutomationDefinition{}, errDatabaseAutomationInvalid
	}
	return normalizedDatabaseAutomationDefinition{
		TriggerType: input.Trigger.Type, Trigger: input.Trigger,
		TriggerJSON: string(triggerJSON), ActionJSON: string(normalizedAction.CanonicalJSON),
		ActionDocument: normalizedAction.Document, NextRunAt: nextRunAt,
	}, nil
}

func validateDatabaseAutomationSource(
	app core.App,
	authorization pageAuthorization,
	databaseID string,
	scope databaseAutomationScope,
) error {
	var sources int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[owner]] = {:owner} AND [[source]].[[workspace_id]] = {:workspaceId}
			AND [[source]].[[page_id]] = {:pageId} AND [[source]].[[id]] = {:databaseId}
			AND ` + databaseSourceNotTrashedSQL("[[source]].[[id]]") + `
	`).Bind(dbx.Params{
		"owner": authorization.Workspace.OwnerID, "workspaceId": authorization.Workspace.WorkspaceID,
		"pageId": authorization.Page.Id, "databaseId": databaseID,
	}).Row(&sources); err != nil {
		return err
	}
	if sources != 1 {
		return databaseAutomationSourceRefusal()
	}
	if scope.Type != databaseAutomationScopeView {
		return nil
	}
	if !boundedAutomationString(scope.ViewID, databaseAutomationMaxIdentifierRunes) {
		return databaseAutomationRefused("scope.viewId names the view, 1–%d characters", databaseAutomationMaxIdentifierRunes)
	}
	var views int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_views}}
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
			AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:databaseId} AND [[id]] = {:viewId}
	`).Bind(dbx.Params{
		"owner": authorization.Workspace.OwnerID, "workspaceId": authorization.Workspace.WorkspaceID,
		"pageId": authorization.Page.Id, "databaseId": databaseID, "viewId": scope.ViewID,
	}).Row(&views); err != nil {
		return err
	}
	if views != 1 {
		return refusedFor(http.StatusNotFound, errorCodeDatabaseAutomationNotFound, "Check scope.viewId: that database has no view "+
			"with that ID. It takes a view's ID as notion-fetch or leafen_catalog gives it (view://…).",
			apiErrorField{Name: "scope.viewId", Problem: "no view of this database"})
	}
	return nil
}

// authorizeDatabaseAutomationSource authorizes an automation's source for
// the person: the body of the database's page — People's too, whose page
// its owner writes and its members read (people_database_access.go).
func authorizeDatabaseAutomationSource(
	app core.App, principalID, pageID string, requirement permissionRequirement,
) (pageAuthorization, error) {
	if requirement == permissionWorkspaceRead {
		return authorizePage(app, principalID, pageID, requirement)
	}
	return authorizePageBody(app, principalID, pageID, requirement)
}

// databaseAutomationSourceRefusal refuses a database a page's automation
// route names that isn't on that page, saying what the route takes: the
// IDs the tools give, which the native API reads as the data source's
// (native_api_data_source_id_aliases.go), and where to read both.
func databaseAutomationSourceRefusal() error {
	return refusedFor(http.StatusNotFound, errorCodeDatabaseAutomationNotFound, "Check databaseId: this page holds no database "+
		"with that ID out of the Trash. It takes the database as leafen_catalog or notion-fetch gives it (its address, or its "+
		"data source's collection://… ID), with pageId the page it is on (a full-page database's own address); "+
		"GET /workspaces/{workspaceId}/automation-targets/{dataSourceId} gives the two.",
		apiErrorField{Name: "databaseId", Problem: "no database on this page"})
}
