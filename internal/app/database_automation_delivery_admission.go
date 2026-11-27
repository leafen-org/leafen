package app

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A button's deliveries name it in place of an automation's definition, as
// "button:" and its source; they go out whatever becomes of the button after
// it ran.
const interactiveAutomationButtonDeliveryPrefix = "button:"

func interactiveAutomationButtonDelivery(definitionID string) bool {
	return strings.HasPrefix(definitionID, interactiveAutomationButtonDeliveryPrefix)
}

// admitInteractiveAutomationButtonDeliveries puts a button run's deliveries in
// the automations' outbox, in the run's transaction: once per actor's run.
func admitInteractiveAutomationButtonDeliveries(
	app core.App,
	actorID string,
	authorization pageAuthorization,
	source interactiveAutomationSource,
	actions []interactiveAutomationAction,
	executionID string,
	now time.Time,
) ([]string, error) {
	if source.Kind == interactiveAutomationSourceLegacyTemplate {
		return nil, nil
	}
	// A button's notifications say they come from it by its label, as Notion's (seen 2026-10-24).
	var document struct {
		Label string `json:"label"`
	}
	_ = json.Unmarshal(source.Document, &document)
	candidate := databaseAutomationEvaluationCandidate{
		EventID: executionID, OwnerID: authorization.Workspace.OwnerID, ActorID: actorID,
		WorkspaceID: authorization.Workspace.WorkspaceID, PageID: authorization.Page.Id,
		DatabaseID: source.TriggerDatabaseID, RowID: source.TriggerRowID,
		DefinitionID: interactiveAutomationButtonDeliveryPrefix + source.ID, DefinitionRevision: 1,
		DefinitionName: strings.TrimSpace(document.Label),
	}
	return admitDatabaseAutomationDeliveries(app, candidate, actions, automationStableID("hbo", actorID, executionID), now)
}

func databaseAutomationDeliveryChannel(action interactiveAutomationAction) (string, bool) {
	switch action.Type {
	case interactiveAutomationActionSendNotification:
		return databaseAutomationDeliveryNotification, true
	case interactiveAutomationActionSendEmail:
		return databaseAutomationDeliveryEmail, true
	case interactiveAutomationActionSendWebhook:
		return databaseAutomationDeliveryWebhook, true
	case interactiveAutomationActionSendSlack:
		return databaseAutomationDeliverySlack, true
	default:
		return "", false
	}
}

func admitDatabaseAutomationDeliveries(
	app core.App,
	candidate databaseAutomationEvaluationCandidate,
	actions []interactiveAutomationAction,
	outcomeID string,
	now time.Time,
) ([]string, error) {
	// An app's action goes to the app's own queue (app_platform_automation_actions.go).
	if err := queueAutomationAppActions(app, candidate, actions, outcomeID, now); err != nil {
		return nil, err
	}
	ids := []string{}
	for _, action := range actions {
		// An empty person property or undefined formula is a successful no-op.
		if action.Type == interactiveAutomationActionSendNotification && len(action.RecipientIDs) == 0 {
			continue
		}
		channel, delivery := databaseAutomationDeliveryChannel(action)
		if !delivery {
			continue
		}
		payload, err := databaseAutomationDeliveryPayload(app, candidate, action, outcomeID)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(payload)
		if err != nil || len(encoded) > databaseAutomationDeliveryPayloadBytes {
			return nil, failedBecause(errDatabaseAutomationUnavailable, err)
		}
		id := automationStableID("had", outcomeID, action.ID)
		result, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_automation_deliveries}}
				([[id]], [[owner]], [[workspace_id]], [[page_id]], [[database_id]], [[event_id]],
				 [[definition_id]], [[definition_revision]], [[outcome_id]], [[action_id]], [[channel]],
				 [[payload_json]], [[state]], [[attempts]], [[max_attempts]], [[next_attempt_at]],
				 [[lease_token]], [[lease_until]], [[provider_id]], [[last_error]],
				 [[delivered_at]], [[failed_at]], [[created]], [[updated]])
			VALUES
				({:id}, {:owner}, {:workspaceId}, {:pageId}, {:databaseId}, {:eventId},
				 {:definitionId}, {:definitionRevision}, {:outcomeId}, {:actionId}, {:channel},
				 {:payload}, 'pending', 0, {:maxAttempts}, {:nextAttemptAt}, '', 0, '', '', 0, 0,
				 {:created}, {:updated})
			ON CONFLICT ([[id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": id, "owner": candidate.OwnerID, "workspaceId": candidate.WorkspaceID,
			"pageId": candidate.PageID, "databaseId": candidate.DatabaseID, "eventId": candidate.EventID,
			"definitionId": candidate.DefinitionID, "definitionRevision": candidate.DefinitionRevision,
			"outcomeId": outcomeID, "actionId": action.ID, "channel": channel, "payload": string(encoded),
			"maxAttempts": databaseAutomationDeliveryMaxAttempts, "nextAttemptAt": now.UnixMilli(),
			"created": now.UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		if err != nil {
			return nil, err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if inserted == 0 {
			var matches int
			err = app.DB().NewQuery(`
				SELECT COUNT(*) FROM {{leafen_database_automation_deliveries}}
				WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
					AND [[page_id]] = {:pageId} AND [[database_id]] = {:databaseId}
					AND [[event_id]] = {:eventId} AND [[definition_id]] = {:definitionId}
					AND [[definition_revision]] = {:definitionRevision}
					AND [[outcome_id]] = {:outcomeId} AND [[action_id]] = {:actionId}
					AND [[channel]] = {:channel} AND [[payload_json]] = {:payload}
			`).Bind(dbx.Params{
				"id": id, "owner": candidate.OwnerID, "workspaceId": candidate.WorkspaceID,
				"pageId": candidate.PageID, "databaseId": candidate.DatabaseID, "eventId": candidate.EventID,
				"definitionId": candidate.DefinitionID, "definitionRevision": candidate.DefinitionRevision,
				"outcomeId": outcomeID, "actionId": action.ID, "channel": channel, "payload": string(encoded),
			}).Row(&matches)
			if err != nil || matches != 1 {
				return nil, failedBecause(errDatabaseAutomationUnavailable, err)
			}
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func databaseAutomationDeliveryPayload(
	app core.App,
	candidate databaseAutomationEvaluationCandidate,
	action interactiveAutomationAction,
	outcomeID string,
) (map[string]any, error) {
	switch action.Type {
	case interactiveAutomationActionSendNotification:
		payload := map[string]any{
			"recipientIds": action.RecipientIDs, "message": action.Message, "pageId": automationDeliveryPageID(candidate),
		}
		// One about a person's People row opens their profile.
		if candidate.RowID != "" {
			personID, err := peopleRowPersonOf(app, candidate.DatabaseID, candidate.RowID)
			if err != nil {
				return nil, err
			}
			if personID != "" {
				payload["personId"] = personID
			}
		}
		// Who sends it: the automation's or the button's name, kept as it ran;
		// an unnamed button's is whoever clicked it — "〈사람〉님의 자동화 기능",
		// as Notion's (seen 2026-10-25).
		if candidate.DefinitionName != "" {
			payload["sourceName"] = candidate.DefinitionName
		} else if interactiveAutomationButtonDelivery(candidate.DefinitionID) && candidate.ActorID != "" {
			payload["sourceActorId"] = candidate.ActorID
		}
		// Its words with their mentions and marks, which the inbox shows.
		if automationMessageSpansShaped(action.MessageSpans) {
			payload["messageSpans"] = action.MessageSpans
		}
		return payload, nil
	case interactiveAutomationActionSendEmail:
		return map[string]any{
			"recipientEmail": action.RecipientEmail, "subject": action.Subject, "message": action.Message,
		}, nil
	case interactiveAutomationActionSendWebhook:
		body, err := databaseAutomationWebhookBody(app, candidate, action, outcomeID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"url": action.URL, "headers": action.Headers, "body": body}, nil
	case interactiveAutomationActionSendSlack:
		return map[string]any{
			"connectionId": action.ConnectionID, "channelId": action.ChannelID, "message": action.Message,
		}, nil
	default:
		return nil, errDatabaseAutomationUnsupported
	}
}

// automationDeliveryPageID is the page a delivery is about: the row it ran
// for, or the page holding a page's button.
func automationDeliveryPageID(candidate databaseAutomationEvaluationCandidate) string {
	if candidate.RowID == "" && interactiveAutomationButtonDelivery(candidate.DefinitionID) {
		return candidate.PageID
	}
	return candidate.RowID
}
