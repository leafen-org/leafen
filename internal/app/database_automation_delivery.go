package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var databaseAutomationDeliveryErrorCode = regexp.MustCompile(`^[a-z0-9_]{1,80}$`)

type databaseAutomationDeliveryPassResult struct {
	Processed     int
	Notifications int
	External      int
	Retried       int
	Failed        int
	Stale         int
	HasMore       bool
}

func processDatabaseAutomationDeliveryPass(
	ctx context.Context,
	app core.App,
	now time.Time,
	provider databaseAutomationDeliveryProvider,
) (databaseAutomationDeliveryPassResult, error) {
	claimed, hasMore, err := claimDatabaseAutomationDeliveries(app, now)
	result := databaseAutomationDeliveryPassResult{Processed: len(claimed), HasMore: hasMore}
	if err != nil || len(claimed) == 0 {
		return result, err
	}
	active := make([]storedDatabaseAutomationDelivery, 0, len(claimed))
	for _, delivery := range claimed {
		if delivery.State == databaseAutomationDeliveryStale {
			result.Stale++
			continue
		}
		active = append(active, delivery)
	}
	settlements := make([]databaseAutomationDeliverySettlement, 0, len(active))
	notificationResults, notificationCount := deliverDatabaseAutomationNotifications(app, now, active)
	result.Notifications = notificationCount
	for _, delivery := range active {
		if outcome, exists := notificationResults[delivery.ID]; exists {
			settlements = append(settlements, databaseAutomationDeliverySettlement{
				Delivery: delivery, Result: outcome,
				RetryAt:  databaseAutomationDeliveryRetryAt(now, delivery.Attempts),
				Terminal: outcome.State == databaseAutomationDeliveryResultFailed || delivery.Attempts >= delivery.MaxAttempts,
			})
		}
	}
	external := make([]storedDatabaseAutomationDelivery, 0, len(active))
	webhooks := map[string]bool{}
	for _, delivery := range active {
		if delivery.Channel == databaseAutomationDeliveryNotification {
			continue
		}
		// A workspace that allows no webhooks fails them, as Notion's policy does.
		if delivery.Channel == databaseAutomationDeliveryWebhook {
			allowed, known := webhooks[delivery.WorkspaceID]
			if !known {
				policies, err := loadWorkspaceConnectionPolicies(app, delivery.WorkspaceID)
				if err != nil {
					return databaseAutomationDeliveryPassResult{}, err
				}
				allowed = policies.AutomationWebhooks
				webhooks[delivery.WorkspaceID] = allowed
			}
			if !allowed {
				settlements = append(settlements, databaseAutomationDeliverySettlement{
					Delivery: delivery, Terminal: true, RetryAt: databaseAutomationDeliveryRetryAt(now, delivery.Attempts),
					Result: databaseAutomationDeliveryProviderResult{
						State: databaseAutomationDeliveryResultFailed, ErrorCode: "webhook_disabled_by_workspace",
					},
				})
				continue
			}
		}
		external = append(external, delivery)
	}
	externalResults := deliverDatabaseAutomationExternal(ctx, external, provider)
	for _, delivery := range external {
		outcome := normalizeDatabaseAutomationDeliveryProviderResult(externalResults[delivery.ID])
		terminal := outcome.State == databaseAutomationDeliveryResultFailed || delivery.Attempts >= delivery.MaxAttempts
		settlements = append(settlements, databaseAutomationDeliverySettlement{
			Delivery: delivery, Result: outcome,
			RetryAt: databaseAutomationDeliveryRetryAt(now, delivery.Attempts), Terminal: terminal,
		})
		if outcome.State == databaseAutomationDeliveryResultSucceeded {
			result.External++
		}
	}
	for _, settlement := range settlements {
		if settlement.Result.State == databaseAutomationDeliveryResultRetryable && !settlement.Terminal {
			result.Retried++
		} else if settlement.Result.State != databaseAutomationDeliveryResultSucceeded {
			result.Failed++
		}
	}
	if err := settleDatabaseAutomationDeliveries(app, now, settlements); err != nil {
		return databaseAutomationDeliveryPassResult{}, err
	}
	return result, nil
}

func deliverDatabaseAutomationExternal(
	ctx context.Context,
	deliveries []storedDatabaseAutomationDelivery,
	provider databaseAutomationDeliveryProvider,
) map[string]databaseAutomationDeliveryProviderResult {
	results := map[string]databaseAutomationDeliveryProviderResult{}
	var mu sync.Mutex
	var wait sync.WaitGroup
	for _, delivery := range deliveries {
		delivery := delivery
		wait.Add(1)
		go func() {
			defer wait.Done()
			outcome := databaseAutomationDeliveryProviderResult{
				State: databaseAutomationDeliveryResultFailed, ErrorCode: "provider_idempotency_required",
			}
			if provider != nil && provider.SupportsIdempotency(delivery.Channel) {
				requestCtx, cancel := context.WithTimeout(ctx, databaseAutomationDeliveryRequestTimeout)
				outcome = provider.Deliver(requestCtx, databaseAutomationDeliveryRequest{
					ID: delivery.ID, Channel: delivery.Channel, PayloadJSON: delivery.PayloadJSON,
					Attempt: delivery.Attempts,
				})
				cancel()
			}
			mu.Lock()
			results[delivery.ID] = outcome
			mu.Unlock()
		}()
	}
	wait.Wait()
	return results
}

func deliverDatabaseAutomationNotifications(
	app core.App,
	now time.Time,
	deliveries []storedDatabaseAutomationDelivery,
) (map[string]databaseAutomationDeliveryProviderResult, int) {
	results := map[string]databaseAutomationDeliveryProviderResult{}
	notificationCount := 0
	err := app.RunInTransaction(func(txApp core.App) error {
		for _, delivery := range deliveries {
			if delivery.Channel != databaseAutomationDeliveryNotification {
				continue
			}
			// A delivery claimed before this transaction may be gone by now:
			// a workspace's deletion clears its deliveries in one lane and its
			// notifications in a later one, and a notification written
			// between them would outlive both, keeping the workspace's rows
			// where its last step refuses them. So a delivery writes only
			// while it still stands, read in the transaction that writes.
			var stands int
			if err := txApp.DB().NewQuery(`
				SELECT COUNT(*) FROM {{leafen_database_automation_deliveries}} WHERE [[id]] = {:id}
			`).Bind(dbx.Params{"id": delivery.ID}).Row(&stands); err != nil {
				return err
			}
			if stands == 0 {
				continue
			}
			var payload struct {
				RecipientIDs []string                           `json:"recipientIds"`
				Message      string                             `json:"message"`
				MessageSpans []interactiveAutomationMessageSpan `json:"messageSpans"`
				PageID       string                             `json:"pageId"`
				PersonID     string                             `json:"personId"`
				SourceName   string                             `json:"sourceName"`
				SourceActor  string                             `json:"sourceActorId"`
			}
			if json.Unmarshal([]byte(delivery.PayloadJSON), &payload) != nil ||
				len(payload.RecipientIDs) == 0 {
				results[delivery.ID] = databaseAutomationDeliveryProviderResult{
					State: databaseAutomationDeliveryResultFailed, ErrorCode: "notification_payload_invalid",
				}
				continue
			}
			valid, err := databaseAutomationNotificationRecipients(txApp, delivery.WorkspaceID, payload.RecipientIDs)
			if err != nil {
				return err
			}
			if len(valid) != len(payload.RecipientIDs) {
				results[delivery.ID] = databaseAutomationDeliveryProviderResult{
					State: databaseAutomationDeliveryResultFailed, ErrorCode: "notification_recipient_invalid",
				}
				continue
			}
			for _, recipientID := range payload.RecipientIDs {
				id := automationStableID("han", delivery.ID, recipientID)
				inserted, err := txApp.DB().NewQuery(`
					INSERT INTO {{leafen_database_automation_notifications}}
						([[id]], [[owner]], [[workspace_id]], [[page_id]], [[delivery_id]],
						 [[recipient_id]], [[message]], [[created]])
					VALUES ({:id}, {:owner}, {:workspaceId}, {:pageId}, {:deliveryId},
						{:recipientId}, {:message}, {:created})
					ON CONFLICT ([[delivery_id]], [[recipient_id]]) DO NOTHING
				`).Bind(dbx.Params{
					"id": id, "owner": delivery.OwnerID, "workspaceId": delivery.WorkspaceID,
					"pageId": delivery.PageID, "deliveryId": delivery.ID,
					"recipientId": recipientID, "message": payload.Message, "created": now.UnixMilli(),
				}).Execute()
				if err != nil {
					return err
				}
				rows, _ := inserted.RowsAffected()
				notificationCount += int(rows)
			}
			if err := projectDatabaseAutomationNotification(txApp, delivery, payload.RecipientIDs, payload.Message, payload.MessageSpans, payload.PageID,
				payload.PersonID, payload.SourceName, payload.SourceActor, now); err != nil {
				return err
			}
			results[delivery.ID] = databaseAutomationDeliveryProviderResult{
				State: databaseAutomationDeliveryResultSucceeded, ProviderID: delivery.ID,
			}
		}
		return nil
	})
	if err != nil {
		for _, delivery := range deliveries {
			if delivery.Channel == databaseAutomationDeliveryNotification {
				results[delivery.ID] = databaseAutomationDeliveryProviderResult{
					State: databaseAutomationDeliveryResultRetryable, ErrorCode: "notification_store_retryable",
				}
			}
		}
		return results, 0
	}
	return results, notificationCount
}

// databaseAutomationNotificationRecipients is who of these an automation may
// notify: the workspace's owner — whoever holds it now, after a transfer, not
// the account its content is kept under — and its members.
func databaseAutomationNotificationRecipients(
	app core.App, workspaceID string, recipientIDs []string,
) (map[string]struct{}, error) {
	// The ids go as one JSON list: a People property names any number.
	encoded, err := json.Marshal(recipientIDs)
	if err != nil {
		return nil, err
	}
	rows := []string{}
	err = app.DB().NewQuery(`
		SELECT [[workspace_owner]] AS [[user_id]] FROM {{leafen_workspaces}} WHERE [[id]]={:workspaceId}
			AND [[workspace_owner]] IN (SELECT [[value]] FROM json_each({:ids}))
		UNION
		SELECT [[user_id]] FROM {{leafen_workspace_memberships}}
		WHERE [[workspace_id]]={:workspaceId} AND [[role]]='member'
			AND [[user_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).Column(&rows)
	if err != nil {
		return nil, err
	}
	valid := map[string]struct{}{}
	for _, id := range rows {
		valid[id] = struct{}{}
	}
	return valid, nil
}

func normalizeDatabaseAutomationDeliveryProviderResult(
	result databaseAutomationDeliveryProviderResult,
) databaseAutomationDeliveryProviderResult {
	if result.State != databaseAutomationDeliveryResultSucceeded &&
		result.State != databaseAutomationDeliveryResultRetryable &&
		result.State != databaseAutomationDeliveryResultFailed {
		return databaseAutomationDeliveryProviderResult{
			State: databaseAutomationDeliveryResultFailed, ErrorCode: "provider_result_invalid",
		}
	}
	result.ProviderID = boundedDatabaseAutomationProviderID(result.ProviderID)
	result.ErrorCode = databaseAutomationDeliveryErrorCode.FindString(strings.ToLower(result.ErrorCode))
	if result.State != databaseAutomationDeliveryResultSucceeded && result.ErrorCode == "" {
		result.ErrorCode = "provider_failure"
	}
	return result
}

func sortedDatabaseAutomationDeliveryIDs(values map[string]databaseAutomationDeliveryProviderResult) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// projectDatabaseAutomationNotification puts an automation's or a button's
// notice in each recipient's inbox, as Notion's "send notification" does: who
// sent it — the automation's or the button's name, its title (seen
// 2026-10-24: "〈name〉에서 메시지를 보냈습니다.") — its message, and a link to
// the page it ran for, or the member one of People's ran for (personID).
const automationNotificationActivityPrefix = "automation-notification:"

func projectDatabaseAutomationNotification(
	app core.App,
	delivery storedDatabaseAutomationDelivery,
	recipientIDs []string,
	message string,
	spans []interactiveAutomationMessageSpan,
	pageID string,
	personID string,
	sourceName string,
	sourceActorID string,
	now time.Time,
) error {
	if !tableKnown(app, "leafen_notifications") {
		return nil
	}
	if pageID == "" {
		pageID = delivery.PageID
	}
	// One admitted before it named its sender names the automation as it is
	// now; one left unnamed names its maker as the sender, which the inbox
	// reads as "〈사람〉님의 자동화 기능", as Notion's (seen 2026-10-24) — an
	// unnamed button's, whoever clicked it (seen 2026-10-25).
	title, maker := sourceName, sourceActorID
	if title == "" && !interactiveAutomationButtonDelivery(delivery.DefinitionID) {
		if err := app.DB().NewQuery(`SELECT [[name]], [[created_by]] FROM {{leafen_database_automation_definitions}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": delivery.DefinitionID}).Row(&title, &maker); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if title != "" {
			maker = ""
		}
	}
	// One about People or a person on it opens People or their profile.
	target, people, err := peopleNoticeTargetOf(app, pageID, personID)
	if err != nil {
		return err
	}
	if !people {
		if target, err = pageNotificationTarget(app, pageID); err != nil {
			return err
		}
	}
	previewSpans := ""
	if automationMessageSpansShaped(spans) {
		if encoded, err := json.Marshal(spans); err == nil {
			previewSpans = string(encoded)
		}
	}
	projections := make([]productNotificationProjection, 0, len(recipientIDs))
	for _, recipientID := range recipientIDs {
		projections = append(projections, productNotificationProjection{
			ID:          automationDigest("automation-notification", delivery.ID, recipientID)[:32],
			EventID:     automationDigest("automation-notification-event", delivery.ID)[:32],
			WorkspaceID: delivery.WorkspaceID, UserID: recipientID,
			ActivityKey: automationNotificationActivityPrefix + delivery.ID,
			Kind:        "system", PageID: pageID, ActorID: maker, Title: title, Preview: message, PreviewSpans: previewSpans, Target: target,
			OccurredAt: now.UnixMilli(), Personal: true,
		})
	}
	_, err = upsertProductNotifications(context.Background(), app, projections, now.UnixMilli())
	return err
}
