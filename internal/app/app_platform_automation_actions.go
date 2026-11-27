//go:build !leafen_noapps

package app

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// An app's action from a database automation or button (docs/app-platform.md
// 10): the same worker function and rules as from the app's screen. It goes
// to the app's durable queue, retried as its other calls are, and runs for
// the person whose change set it off (a button: who pressed it). An action
// that asks for approval, or any action where the administrator oversees
// the organization, doesn't run unattended.

const (
	appQueueAutomation       = "automation"
	appAutomationInputBytes  = 16 << 10
	appAutomationViaButton   = "button"
	appAutomationViaDatabase = "automation"
)

var errAppAutomationAction = errors.New("the app's action can't run from an automation")

func normalizeInteractiveAutomationAppAction(action *interactiveAutomationAction) error {
	action.AppInstallID, action.AppAction = strings.TrimSpace(action.AppInstallID), strings.TrimSpace(action.AppAction)
	if !boundedAutomationString(action.AppInstallID, interactiveAutomationMaxIdentifierRunes) {
		return automationRefused("appInstallId names the installed app, 1–%d characters", interactiveAutomationMaxIdentifierRunes)
	}
	if !appKeyPattern.MatchString(action.AppAction) {
		return automationRefused("appAction is the key of one of the app's actions, not %q", action.AppAction)
	}
	if len(action.AppInput) == 0 {
		action.AppInput = json.RawMessage(`{}`)
	}
	if len(action.AppInput) > appAutomationInputBytes || !appJSONObject(action.AppInput) {
		return automationRefused("appInput is an object of at most %d bytes", appAutomationInputBytes)
	}
	return nil
}

// appAutomationAction is the app's action an automation names, when it may
// run unattended for the person in the organization.
func appAutomationAction(app core.App, installID, key, personID, tenancy string) (storedAppInstall, appManifest, appManifestAction, error) {
	install, err := loadAppInstall(app, installID)
	if err != nil || install.Status != "active" || install.RemovedAt != 0 {
		return install, appManifest{}, appManifestAction{}, refusedUnlessStorage(errAppAutomationAction, err)
	}
	_, manifest, err := loadAppPackage(app, install.PackageHash)
	if err != nil {
		return install, manifest, appManifestAction{}, err
	}
	action, found := appManifestActionByKey(manifest, key)
	if !found || action.RequiresApproval || appOverseen(app, tenancy) {
		return install, manifest, action, errAppAutomationAction
	}
	if granted, err := appGranted(app, install.ID, tenancy, personID); err != nil || !granted {
		return install, manifest, action, refusedUnlessStorage(errAppAutomationAction, err)
	}
	return install, manifest, action, nil
}

// appAutomationRunAction is the queued call's action, if it may still run.
func appAutomationRunAction(app core.App, install storedAppInstall, key, personID, tenancy string) (appManifestAction, error) {
	_, _, action, err := appAutomationAction(app, install.ID, key, personID, tenancy)
	return action, err
}

// automationAppActionAllowed says the saving person may name the app's
// action in an automation of this workspace.
func automationAppActionAllowed(app core.App, authorization pageAuthorization, action interactiveAutomationAction) bool {
	_, _, _, err := appAutomationAction(app, action.AppInstallID, action.AppAction, authorization.Workspace.PrincipalID,
		appWorkspaceTenancyOf(app, authorization.Workspace.WorkspaceID))
	return err == nil
}

// usableAppAutomations are the actions an automation may run — those that
// don't ask for approval — with the fields of their input.
func usableAppAutomations(manifest appManifest) []usableAppAutomationWire {
	automations := []usableAppAutomationWire{}
	for _, action := range manifest.Actions {
		if action.RequiresApproval {
			continue
		}
		schema := struct {
			Properties map[string]struct {
				Title string `json:"title"`
				Type  any    `json:"type"`
			} `json:"properties"`
			Required []string `json:"required"`
		}{}
		_ = json.Unmarshal(action.Input, &schema)
		fields := []usableAppInputFieldWire{}
		for _, key := range appSchemaPropertyOrder(action.Input) {
			property := schema.Properties[key]
			kind, _ := property.Type.(string)
			if !appOneOf(kind, []string{"string", "number", "integer", "boolean"}) {
				kind = "other"
			}
			label := key
			if title := strings.TrimSpace(property.Title); title != "" {
				label = title
			}
			fields = append(fields, usableAppInputFieldWire{Key: key, Label: label, Labels: appTextOrEmpty(action.InputLabels[key]), Type: kind,
				Required: appOneOf(key, schema.Required)})
		}
		automations = append(automations, usableAppAutomationWire{Key: action.Key, Name: appTextOrEmpty(action.Name),
			ExternalEffect: action.ExternalEffect, Fields: fields})
	}
	return automations
}

// appWorkspaceTenancyOf names the organization a workspace belongs to.
func appWorkspaceTenancyOf(app core.App, workspaceID string) string {
	var organization string
	_ = app.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": workspaceID}).Row(&organization)
	if organization == "" {
		return appTenancyServer
	}
	return "org:" + organization
}

// queueAutomationAppActions puts the run's app actions in their apps'
// queues, once for the run.
func queueAutomationAppActions(app core.App, candidate databaseAutomationEvaluationCandidate, actions []interactiveAutomationAction, outcomeID string, now time.Time) error {
	tenancy := ""
	for _, action := range actions {
		if action.Type != interactiveAutomationActionRunAppAction {
			continue
		}
		if tenancy == "" {
			tenancy = appWorkspaceTenancyOf(app, candidate.WorkspaceID)
		}
		via, definition := appAutomationViaDatabase, candidate.DefinitionID
		if button, ok := strings.CutPrefix(definition, interactiveAutomationButtonDeliveryPrefix); ok {
			via, definition = appAutomationViaButton, button
		}
		body, _ := json.Marshal(map[string]any{"input": action.AppInput, "via": map[string]string{
			"kind": via, "automation": definition, "database": candidate.DatabaseID, "page": candidate.RowID}})
		if _, err := app.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_app_hook_events}}
			([[id]], [[install_id]], [[hook]], [[delivery]], [[content_type]], [[body]], [[status]], [[received_at]], [[kind]], [[topic]], [[person_id]])
			SELECT {:id}, [[id]], {:key}, {:delivery}, 'application/json', {:body}, 'waiting', {:now}, {:kind}, {:tenancy}, {:person}
			FROM {{leafen_app_installs}} WHERE [[id]] = {:install} AND [[removed_at]] = 0`).Bind(dbx.Params{
			"id": security.RandomString(20), "install": action.AppInstallID, "key": action.AppAction,
			"delivery": automationStableID("haa", outcomeID, action.ID), "body": body, "now": now.UnixMilli(),
			"kind": appQueueAutomation, "tenancy": tenancy, "person": candidate.ActorID,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
