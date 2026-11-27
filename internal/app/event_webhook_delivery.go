package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Each subscription's events go out one at a time, in the order they
// happened: the next waits until the one before it succeeds or fails for
// good. A delivery is signed with the subscription's secret over its time
// and body, and retried with growing waits when the receiver is busy or
// down. An event goes only to subscriptions whose person — and key, if one
// made it — may still read its page when it is sent.

const (
	eventWebhookRunnerStoreKey       = "leafen.eventWebhookRunner"
	eventWebhookAllowedLoopbackEnv   = "LEAFEN_WEBHOOK_ALLOWED_LOOPBACK"
	eventWebhookTurnBatch            = 100
	eventWebhookMaintenance          = 30 * time.Second
	eventWebhookFailureDelay         = 5 * time.Second
	eventWebhookEventRetention       = 7 * 24 * time.Hour
	eventWebhookDeliveryRetention    = 30 * 24 * time.Hour
	eventWebhookDeliveryStatePending = "pending"
	eventWebhookDeliveryRetrying     = "retrying"
	eventWebhookDeliverySucceeded    = "succeeded"
	eventWebhookDeliveryFailed       = "failed"
)

var eventWebhookRetryWaits = []time.Duration{
	10 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour,
}

type eventWebhookRunner struct {
	app    core.App
	client *http.Client
	wake   chan struct{}

	runMu  sync.Mutex
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// eventWebhookAllowedLoopback is the one loopback receiver, host:port, a
// development or test server may deliver to; real receivers are public.
func eventWebhookAllowedLoopback() string {
	return os.Getenv(eventWebhookAllowedLoopbackEnv)
}

func newEventWebhookRunner(app core.App) *eventWebhookRunner {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = outboundPublicProxy
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return outboundPublicDialContext(ctx, network, address, eventWebhookAllowedLoopback(),
			net.DefaultResolver.LookupNetIP, dialer.DialContext)
	}
	return &eventWebhookRunner{
		app: app, wake: make(chan struct{}, 1),
		client: &http.Client{
			Transport: transport, Timeout: databaseAutomationDeliveryRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (runner *eventWebhookRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel, runner.done = cancel, make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *eventWebhookRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	cancel, done := runner.cancel, runner.done
	runner.cancel, runner.done = nil, nil
	runner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("event webhook runner shutdown: %w", ctx.Err())
	}
}

func (runner *eventWebhookRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func notifyEventWebhookRunnerAfterCommit(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(eventWebhookRunnerStoreKey).(*eventWebhookRunner); ok {
			runner.notify()
		}
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				notify()
			}
			return nil
		})
		return
	}
	notify()
}

func (runner *eventWebhookRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := time.Now().UTC()
		nextDue, err := runner.runTurn(ctx, now)
		wait := eventWebhookMaintenance
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			runner.app.Logger().Warn("leafen webhook turn failed", "error", err)
			wait = eventWebhookFailureDelay
		} else if nextDue >= 0 {
			wait = min(wait, max(time.Duration(nextDue-now.UnixMilli())*time.Millisecond, time.Millisecond))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-runner.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

type storedEventWebhookEvent struct {
	Sequence    int64  `db:"sequence"`
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
	Type        string `db:"type"`
	EntityID    string `db:"entity_id"`
	EntityType  string `db:"entity_type"`
	PageID      string `db:"page_id"`
	AuthorsJSON string `db:"authors_json"`
	DataJSON    string `db:"data_json"`
	OccurredAt  int64  `db:"occurred_at"`
}

type storedEventWebhookSubscription struct {
	ID             string `db:"id"`
	WorkspaceID    string `db:"workspace_id"`
	OwnerID        string `db:"owner_id"`
	ConnectionID   string `db:"connection_id"`
	RequestID      string `db:"request_id"`
	URL            string `db:"url"`
	SecretCipher   string `db:"secret_cipher"`
	EventTypesJSON string `db:"event_types_json"`
	Description    string `db:"description"`
	APIVersion     string `db:"api_version"`
	Active         bool   `db:"active"`
	Created        int64  `db:"created"`
	Updated        int64  `db:"updated"`
	// Kind is "leafen", or "notion" for a Notion-compatible connection's.
	Kind string `db:"kind"`
	// VerifyState is "pending" while a subscription waits for its
	// verification token to be given back (notion_internal_connection_webhooks.go).
	VerifyState string `db:"verify_state"`
}

type storedEventWebhookDelivery struct {
	ID             string `db:"id"`
	SubscriptionID string `db:"subscription_id"`
	EventID        string `db:"event_id"`
	EventType      string `db:"event_type"`
	EntityID       string `db:"entity_id"`
	Sequence       int64  `db:"sequence"`
	BodyJSON       string `db:"body_json"`
	State          string `db:"state"`
	Attempts       int    `db:"attempts"`
	NextAttemptAt  int64  `db:"next_attempt_at"`
	StatusCode     int    `db:"status_code"`
	ErrorCode      string `db:"error_code"`
	Created        int64  `db:"created"`
	SettledAt      int64  `db:"settled_at"`
}

const eventWebhookSubscriptionColumns = `[[id]], [[workspace_id]], [[owner_id]], [[connection_id]], [[request_id]], [[url]],
	[[secret_cipher]], [[event_types_json]], [[description]], [[api_version]], [[active]], [[created]], [[updated]], [[kind]], [[verify_state]]`

// runTurn hands due events to their subscriptions, sends what is due, and
// tells when the next thing is due (-1 for nothing).
func (runner *eventWebhookRunner) runTurn(ctx context.Context, now time.Time) (int64, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	if err := syncNotionWebhookSubscriptions(runner.app, now); err != nil {
		return -1, err
	}
	if err := dispatchEventWebhookEvents(runner.app, now); err != nil {
		return -1, err
	}
	if err := runner.deliverDue(ctx, now); err != nil {
		return -1, err
	}
	if _, err := runner.app.DB().NewQuery(`DELETE FROM {{leafen_webhook_events}}
		WHERE [[dispatched]] = 1 AND [[occurred_at]] < {:before}`).
		Bind(dbx.Params{"before": now.Add(-eventWebhookEventRetention).UnixMilli()}).Execute(); err != nil {
		return -1, err
	}
	if _, err := runner.app.DB().NewQuery(`DELETE FROM {{leafen_webhook_deliveries}}
		WHERE [[settled_at]] > 0 AND [[settled_at]] < {:before}`).
		Bind(dbx.Params{"before": now.Add(-eventWebhookDeliveryRetention).UnixMilli()}).Execute(); err != nil {
		return -1, err
	}
	var next sql.NullInt64
	err := runner.app.DB().NewQuery(`SELECT MIN([[due]]) FROM (
		SELECT MIN([[dispatch_after]]) AS [[due]] FROM {{leafen_webhook_events}} WHERE [[dispatched]] = 0
		UNION ALL
		SELECT MIN([[delivery]].[[next_attempt_at]]) FROM {{leafen_webhook_deliveries}} [[delivery]]
			JOIN {{leafen_webhook_subscriptions}} [[subscription]] ON [[subscription]].[[id]] = [[delivery]].[[subscription_id]]
			WHERE [[delivery]].[[settled_at]] = 0 AND [[subscription]].[[active]] = 1)`).Row(&next)
	if err != nil || !next.Valid {
		return -1, err
	}
	return next.Int64, nil
}

func dispatchEventWebhookEvents(app core.App, now time.Time) error {
	return app.RunInTransaction(func(txApp core.App) error {
		events := []storedEventWebhookEvent{}
		if err := txApp.DB().NewQuery(`SELECT [[sequence]], [[id]], [[workspace_id]], [[type]], [[entity_id]],
				[[entity_type]], [[page_id]], [[authors_json]], [[data_json]], [[occurred_at]]
			FROM {{leafen_webhook_events}} WHERE [[dispatched]] = 0 AND [[dispatch_after]] <= {:now}
			ORDER BY [[sequence]] LIMIT {:limit}`).
			Bind(dbx.Params{"now": now.UnixMilli(), "limit": eventWebhookTurnBatch}).All(&events); err != nil {
			return err
		}
		// An event still gathering its page's quick edits holds back the
		// workspace's events after it, so they go out in order.
		holds := []struct {
			WorkspaceID string `db:"workspace_id"`
			Sequence    int64  `db:"sequence"`
		}{}
		// An upload's expiry, told when it comes, holds back nothing.
		if err := txApp.DB().NewQuery(`SELECT [[workspace_id]], MIN([[sequence]]) AS [[sequence]] FROM {{leafen_webhook_events}}
			WHERE [[dispatched]] = 0 AND [[dispatch_after]] > {:now} AND [[type]] != {:expiry} GROUP BY [[workspace_id]]`).
			Bind(dbx.Params{"now": now.UnixMilli(), "expiry": eventWebhookFileUploadExpired}).All(&holds); err != nil {
			return err
		}
		held := map[string]int64{}
		for _, hold := range holds {
			held[hold.WorkspaceID] = hold.Sequence
		}
		subscriptions := map[string][]storedEventWebhookSubscription{}
		for _, event := range events {
			if first, holding := held[event.WorkspaceID]; holding && event.Sequence > first {
				continue
			}
			listed, loaded := subscriptions[event.WorkspaceID]
			if !loaded {
				if err := txApp.DB().NewQuery(`SELECT ` + eventWebhookSubscriptionColumns + `
					FROM {{leafen_webhook_subscriptions}} WHERE [[workspace_id]] = {:workspace} AND [[active]] = 1`).
					Bind(dbx.Params{"workspace": event.WorkspaceID}).All(&listed); err != nil {
					return err
				}
				subscriptions[event.WorkspaceID] = listed
			}
			for _, subscription := range listed {
				// One waiting for its verification token hears nothing yet.
				if subscription.VerifyState != "" {
					continue
				}
				types := []string{}
				if json.Unmarshal([]byte(subscription.EventTypesJSON), &types) != nil || !containsString(types, event.Type) {
					continue
				}
				var body []byte
				if subscription.Kind == notionWebhookKind {
					grant, visible := notionWebhookGrantMaySee(txApp, subscription, event.PageID)
					if !visible {
						continue
					}
					built, send, err := notionWebhookBody(txApp, subscription, grant, event)
					if err != nil || !send {
						continue
					}
					body = built
				} else {
					if !eventWebhookSubscriptionMaySee(txApp, subscription, event.PageID) {
						continue
					}
					built, err := eventWebhookBody(subscription, event)
					if err != nil {
						return err
					}
					body = built
				}
				if err := insertEventWebhookDelivery(txApp, subscription.ID, event, body, now); err != nil {
					return err
				}
			}
			if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_webhook_events}} SET [[dispatched]] = 1 WHERE [[sequence]] = {:sequence}`).
				Bind(dbx.Params{"sequence": event.Sequence}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
}

func insertEventWebhookDelivery(app core.App, subscriptionID string, event storedEventWebhookEvent, body []byte, now time.Time) error {
	_, err := app.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_webhook_deliveries}}
		([[id]], [[subscription_id]], [[event_id]], [[event_type]], [[entity_id]], [[sequence]], [[body_json]],
			[[state]], [[next_attempt_at]], [[created]])
		VALUES ({:id}, {:subscription}, {:event}, {:type}, {:entity}, {:sequence}, {:body}, {:state}, {:now}, {:now})`).
		Bind(dbx.Params{
			"id": eventWebhookID(), "subscription": subscriptionID, "event": event.ID, "type": event.Type,
			"entity": event.EntityID, "sequence": event.Sequence, "body": string(body),
			"state": eventWebhookDeliveryStatePending, "now": now.UnixMilli(),
		}).Execute()
	return err
}

func eventWebhookBody(subscription storedEventWebhookSubscription, event storedEventWebhookEvent) ([]byte, error) {
	payload := eventWebhookPayload{
		ID: event.ID, Timestamp: time.UnixMilli(event.OccurredAt).UTC().Format(time.RFC3339Nano),
		WorkspaceID: event.WorkspaceID, SubscriptionID: subscription.ID, Type: event.Type,
		Authors: []eventWebhookAuthor{}, Entity: eventWebhookEntity{ID: event.EntityID, Type: event.EntityType},
		Data: map[string]any{}, APIVersion: subscription.APIVersion,
	}
	if json.Unmarshal([]byte(event.AuthorsJSON), &payload.Authors) != nil || json.Unmarshal([]byte(event.DataJSON), &payload.Data) != nil {
		return nil, errors.New(errorCodeEventWebhookFailed)
	}
	return json.Marshal(payload)
}

// eventWebhookSubscriptionMaySee reports whether a subscription may learn
// of a change to a page: its person can read the page (while an operation holds it too, page_access_hold.go)
// and, when a key made it, the key is still good, covers the workspace and its organization
// still allows it.
func eventWebhookSubscriptionMaySee(app core.App, subscription storedEventWebhookSubscription, pageID string) bool {
	if pageID != "" {
		if _, err := pageAccessDespiteHold(authorizePage(app, subscription.OwnerID, pageID, permissionWorkspaceRead)); err != nil {
			return false
		}
	}
	if subscription.ConnectionID == "" {
		return true
	}
	key, found, err := findUserKeyByID(app, subscription.ConnectionID)
	if err != nil || !found || key.OwnerID != subscription.OwnerID || !containsString(key.WorkspaceIDs, subscription.WorkspaceID) {
		return false
	}
	denial, err := userKeyPolicyDenial(app, key, userKeyScopeRead, []string{subscription.WorkspaceID})
	return err == nil && denial == ""
}

// deliverDue sends, for each subscription with something waiting, its
// earliest unsettled delivery when that is due.
func (runner *eventWebhookRunner) deliverDue(ctx context.Context, now time.Time) error {
	heads := []storedEventWebhookDelivery{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[delivery]].* FROM {{leafen_webhook_deliveries}} [[delivery]]
		JOIN {{leafen_webhook_subscriptions}} [[subscription]] ON [[subscription]].[[id]] = [[delivery]].[[subscription_id]]
		WHERE [[delivery]].[[settled_at]] = 0 AND [[subscription]].[[active]] = 1
			AND [[delivery]].[[sequence]] = (SELECT MIN([[sequence]]) FROM {{leafen_webhook_deliveries}}
				WHERE [[subscription_id]] = [[delivery]].[[subscription_id]] AND [[settled_at]] = 0)
			AND [[delivery]].[[next_attempt_at]] <= {:now}
		LIMIT {:limit}`).Bind(dbx.Params{"now": now.UnixMilli(), "limit": eventWebhookTurnBatch}).All(&heads); err != nil {
		return err
	}
	for _, delivery := range heads {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		subscription := storedEventWebhookSubscription{}
		if err := runner.app.DB().NewQuery(`SELECT ` + eventWebhookSubscriptionColumns + `
			FROM {{leafen_webhook_subscriptions}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": delivery.SubscriptionID}).One(&subscription); err != nil {
			continue
		}
		if err := runner.deliver(ctx, subscription, delivery, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func (runner *eventWebhookRunner) deliver(
	ctx context.Context, subscription storedEventWebhookSubscription, delivery storedEventWebhookDelivery, now time.Time,
) error {
	settle := func(state string, status int, code string, next int64) error {
		settledAt := int64(0)
		if state == eventWebhookDeliverySucceeded || state == eventWebhookDeliveryFailed {
			settledAt = now.UnixMilli()
		}
		_, err := runner.app.DB().NewQuery(`UPDATE {{leafen_webhook_deliveries}}
			SET [[state]] = {:state}, [[attempts]] = [[attempts]] + 1, [[status_code]] = {:status},
				[[error_code]] = {:code}, [[next_attempt_at]] = {:next}, [[settled_at]] = {:settled}
			WHERE [[id]] = {:id}`).Bind(dbx.Params{
			"state": state, "status": status, "code": code, "next": next, "settled": settledAt, "id": delivery.ID,
		}).Execute()
		return err
	}
	if delivery.EventType != eventWebhookTest && delivery.EventType != notionWebhookVerify {
		var pageID string
		_ = runner.app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_webhook_events}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": delivery.EventID}).Row(&pageID)
		visible := false
		if subscription.Kind == notionWebhookKind {
			_, visible = notionWebhookGrantMaySee(runner.app, subscription, pageID)
		} else {
			visible = eventWebhookSubscriptionMaySee(runner.app, subscription, pageID)
		}
		if !visible {
			return settle(eventWebhookDeliveryFailed, 0, eventWebhookWithheld, now.UnixMilli())
		}
	}
	secret, err := decryptAdministratorSetting(runner.app, subscription.SecretCipher)
	if err != nil {
		return settle(eventWebhookDeliveryFailed, 0, "secret_unavailable", now.UnixMilli())
	}
	payload := map[string]any{}
	if json.Unmarshal([]byte(delivery.BodyJSON), &payload) != nil {
		return settle(eventWebhookDeliveryFailed, 0, "payload_invalid", now.UnixMilli())
	}
	attempt := delivery.Attempts + 1
	if _, numbered := payload["attempt_number"]; numbered {
		payload["attempt_number"] = attempt
	}
	body, _ := json.Marshal(payload)
	target, err := normalizeEventWebhookURL(subscription.URL)
	if err != nil {
		return settle(eventWebhookDeliveryFailed, 0, "target_invalid", now.UnixMilli())
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return settle(eventWebhookDeliveryFailed, 0, "target_invalid", now.UnixMilli())
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Leafen-Webhooks/1")
	if subscription.Kind == notionWebhookKind {
		request.Header.Set("User-Agent", "notion-api")
		request.Header.Set(notionWebhookSignature, notionWebhookSignatureValue(secret, body))
	} else {
		request.Header.Set(eventWebhookIDHeader, delivery.EventID)
		request.Header.Set(eventWebhookSignatureHeader, "t="+timestamp+",v1="+eventWebhookSignature(secret, timestamp, body))
	}
	response, err := runner.client.Do(request)
	retry := func(code string, status int) error {
		if attempt >= eventWebhookMaxAttempts || !eventWebhookDeliveryRetries(delivery.EventType) {
			return settle(eventWebhookDeliveryFailed, status, code, now.UnixMilli())
		}
		wait := eventWebhookRetryWaits[min(attempt-1, len(eventWebhookRetryWaits)-1)]
		return settle(eventWebhookDeliveryRetrying, status, code, now.Add(wait).UnixMilli())
	}
	if errors.Is(err, errOutboundUnsafeDestination) {
		return settle(eventWebhookDeliveryFailed, 0, "target_invalid", now.UnixMilli())
	}
	if err != nil {
		return retry("receiver_unreachable", 0)
	}
	defer response.Body.Close()
	databaseAutomationResponseWithinLimit(response.Body)
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		return settle(eventWebhookDeliverySucceeded, response.StatusCode, "", now.UnixMilli())
	case response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500:
		return retry("receiver_busy", response.StatusCode)
	default:
		return settle(eventWebhookDeliveryFailed, response.StatusCode, "receiver_rejected", now.UnixMilli())
	}
}

// eventWebhookSignature is the hex HMAC-SHA256, under the subscription's
// secret, of the delivery's time and body joined by a dot.
func eventWebhookSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
