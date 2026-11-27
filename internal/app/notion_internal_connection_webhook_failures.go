package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A connection subscription's failed events, as Notion's "실패한 이벤트 보기"
// tells them (seen 2026-11-21, a subscription made to an address that never
// answers and deleted at once): how many failed and when the latest did,
// read from the subscription's deliveries — its verification token's sends
// among them, a resend too.

// eventWebhookWithheld is a delivery settled without a send because its
// page is no longer the subscription's to see (eventWebhookRunner.deliver):
// nothing went to the address, so it isn't a failed send.
const eventWebhookWithheld = "not_visible"

// internalConnectionWebhookFailures counts a subscription's failed events:
// each event whose latest send its address didn't take — still retried or
// given up — once, however often it was tried (Notion's count stayed at one
// while its first token send waited, and a resend the address didn't take
// made it two); an event its address took at last no longer counts. The time
// is the latest such send's, 0 with none.
func internalConnectionWebhookFailures(app core.App, subscriptionID string) (int, int64) {
	rows := []storedEventWebhookDelivery{}
	if err := app.DB().NewQuery(`SELECT [[state]], [[attempts]], [[next_attempt_at]], [[settled_at]]
		FROM {{leafen_webhook_deliveries}}
		WHERE [[subscription_id]] = {:id} AND [[state]] IN ({:retrying}, {:failed}) AND [[error_code]] != {:withheld}`).
		Bind(dbx.Params{
			"id": subscriptionID, "retrying": eventWebhookDeliveryRetrying, "failed": eventWebhookDeliveryFailed,
			"withheld": eventWebhookWithheld,
		}).All(&rows); err != nil {
		app.Logger().Warn("leafen webhook failures unread", "subscription", subscriptionID, "error", err)
		return 0, 0
	}
	latest := int64(0)
	for _, row := range rows {
		latest = max(latest, eventWebhookLastSentAt(row))
	}
	return len(rows), latest
}

// eventWebhookLastSentAt is when a delivery was last sent: the time it was
// settled, or, while it waits to be tried again, its next try less the wait
// its runner set after that send (eventWebhookRunner.deliver).
func eventWebhookLastSentAt(delivery storedEventWebhookDelivery) int64 {
	if delivery.SettledAt > 0 || delivery.Attempts == 0 {
		return delivery.SettledAt
	}
	wait := eventWebhookRetryWaits[min(delivery.Attempts-1, len(eventWebhookRetryWaits)-1)]
	return delivery.NextAttemptAt - wait.Milliseconds()
}

// recordNotionWebhookVerificationResent keeps a token sent again at once
// (resendInternalConnectionWebhookToken) among the subscription's
// deliveries, settled as it went with the status its address gave, so its
// failed events count it as Notion's do. Its body isn't kept: nothing sends
// it from there again.
func recordNotionWebhookVerificationResent(app core.App, subscription storedEventWebhookSubscription, status int, now time.Time) error {
	return app.RunInTransaction(func(txApp core.App) error {
		eventID, at := eventWebhookID(), now.UnixMilli()
		result, err := txApp.DB().Insert("leafen_webhook_events", dbx.Params{
			"id": eventID, "workspace_id": subscription.WorkspaceID, "type": notionWebhookVerify,
			"entity_id": subscription.ID, "entity_type": "webhook", "page_id": "", "authors_json": "[]", "data_json": "{}",
			"occurred_at": at, "dispatch_after": at, "dispatched": 1,
		}).Execute()
		if err != nil {
			return err
		}
		sequence, err := result.LastInsertId()
		if err != nil {
			return err
		}
		state, code := eventWebhookDeliverySucceeded, ""
		if status < 200 || status >= 300 {
			state, code = eventWebhookDeliveryFailed, "receiver_rejected"
		}
		_, err = txApp.DB().Insert("leafen_webhook_deliveries", dbx.Params{
			"id": eventWebhookID(), "subscription_id": subscription.ID, "event_id": eventID, "event_type": notionWebhookVerify,
			"entity_id": subscription.ID, "sequence": sequence, "body_json": "{}", "state": state, "attempts": 1,
			"next_attempt_at": at, "status_code": status, "error_code": code, "created": at, "settled_at": at,
		}).Execute()
		return err
	})
}
