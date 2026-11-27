package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A new personal notification becomes a desktop push, as in Notion, when it
// is still unread ten seconds later: each of the person's subscribed browsers
// gets it, one the push service no longer knows is forgotten, and a push that
// cannot be sent is retried a few times.

const (
	webPushDelay              = 10 * time.Second
	webPushMaxAttempts        = 5
	webPushRetention          = 7 * 24 * time.Hour
	webPushTurnLimit          = 32
	webPushCleanupLimit       = 100
	webPushRequestTimeout     = 10 * time.Second
	webPushTTLSeconds         = 86400
	webPushMaxSubscriberFails = 5
	webPushRunnerStoreKey     = "leafenWebPushRunner"
	webPushMaintenanceWait    = time.Hour
	webPushFailureDelay       = time.Minute
)

type storedWebPushDelivery struct {
	NotificationID string `db:"notification_id"`
	WorkspaceID    string `db:"workspace_id"`
	UserID         string `db:"user_id"`
	Attempts       int    `db:"attempts"`
}

type storedWebPushSubscription struct {
	ID       string `db:"id"`
	Endpoint string `db:"endpoint"`
	P256DH   string `db:"p256dh"`
	Auth     string `db:"auth"`
	Failures int    `db:"failures"`
}

type webPushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

// enqueueWebPushDeliveries gives each new personal notification the push it
// becomes, for people who turned push on in some browser.
func enqueueWebPushDeliveries(app core.App, projections []productNotificationProjection, now int64) error {
	subscribed := map[string]bool{}
	queued := false
	for _, projection := range projections {
		if !projection.Personal {
			continue
		}
		has, known := subscribed[projection.UserID]
		if !known {
			if err := app.DB().NewQuery(`
				SELECT EXISTS (SELECT 1 FROM {{leafen_web_push_subscriptions}} WHERE [[user_id]] = {:userId})
			`).Bind(dbx.Params{"userId": projection.UserID}).Row(&has); err != nil {
				return err
			}
			subscribed[projection.UserID] = has
		}
		if !has {
			continue
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_web_push_deliveries}} (
				[[notification_id]], [[workspace_id]], [[user_id]], [[status]], [[attempts]],
				[[next_attempt_at]], [[created_at]], [[updated_at]]
			) VALUES ({:id}, {:workspaceId}, {:userId}, 'pending', 0, {:due}, {:now}, {:now})
			ON CONFLICT ([[notification_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": projection.ID, "workspaceId": projection.WorkspaceID, "userId": projection.UserID,
			"due": now + webPushDelay.Milliseconds(), "now": now,
		}).Execute(); err != nil {
			return err
		}
		queued = true
	}
	if queued {
		notifyWebPushRunnerAfterCommit(app)
	}
	return nil
}

type webPushRunner struct {
	app    core.App
	client *http.Client
	// allowEndpoint admits the push services a browser's subscription may
	// name.
	allowEndpoint func(string) bool
	wake          chan struct{}

	runMu  sync.Mutex
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newWebPushRunner(app core.App) *webPushRunner {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = outboundPublicProxy
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return outboundPublicDialContext(ctx, network, address, "", net.DefaultResolver.LookupNetIP, dialer.DialContext)
	}
	return &webPushRunner{
		app: app, wake: make(chan struct{}, 1), allowEndpoint: webPushEndpointAllowed,
		client: &http.Client{
			Transport: transport, Timeout: webPushRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (runner *webPushRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel = cancel
	runner.done = make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *webPushRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("web push runner shutdown: %w", ctx.Err())
	}
}

func (runner *webPushRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *webPushRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := time.Now().UTC()
		hasMore, nextDue, err := runner.runTurn(ctx, now)
		wait := webPushMaintenanceWait
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// The database busy, its pushes wait for the next turn, soon (runner_turn_wait.go).
			wait = webPushFailureDelay
			if logRunnerTurnWait(runner.app, "notification.web-push.send", err) {
				wait = runnerTurnBusyDelay
			} else {
				logNotificationEmailFailure(runner.app, "web-push-turn", err)
			}
		} else if hasMore {
			wait = time.Millisecond
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

// runTurn sends the due pushes and forgets settled ones after a week. It
// reports whether more are due and when the next one is.
func (runner *webPushRunner) runTurn(ctx context.Context, now time.Time) (bool, int64, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	table := "{{leafen_web_push_deliveries}}"
	if _, err := runner.app.DB().NewQuery(`
		DELETE FROM ` + table + ` WHERE [[notification_id]] IN (
			SELECT [[notification_id]] FROM ` + table + `
			WHERE [[settled_at]] > 0 AND [[settled_at]] < {:before}
			ORDER BY [[settled_at]], [[notification_id]] LIMIT {:limit}
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"before": now.Add(-webPushRetention).UnixMilli(), "limit": webPushCleanupLimit,
	}).Execute(); err != nil {
		return false, -1, err
	}
	due := []storedWebPushDelivery{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[notification_id]], [[workspace_id]], [[user_id]], [[attempts]]
		FROM ` + table + ` INDEXED BY [[idx_leafen_web_push_deliveries_due]]
		WHERE [[status]] = 'pending' AND [[next_attempt_at]] <= {:now}
		ORDER BY [[next_attempt_at]], [[notification_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": now.UnixMilli(), "limit": webPushTurnLimit + 1}).All(&due); err != nil {
		return false, -1, err
	}
	hasMore := len(due) > webPushTurnLimit
	if hasMore {
		due = due[:webPushTurnLimit]
	}
	turnErrors := []error{}
	for _, delivery := range due {
		if err := ctx.Err(); err != nil {
			return hasMore, -1, errors.Join(append(turnErrors, err)...)
		}
		if err := runner.deliver(ctx, delivery, now); err != nil {
			turnErrors = append(turnErrors, err)
		}
	}
	var nextDue sql.NullInt64
	if err := runner.app.DB().NewQuery(`
		SELECT MIN([[next_attempt_at]]) FROM ` + table + ` WHERE [[status]] = 'pending'
	`).WithContext(ctx).Row(&nextDue); err != nil {
		turnErrors = append(turnErrors, err)
	}
	next := int64(-1)
	if nextDue.Valid {
		next = nextDue.Int64
	}
	return hasMore, next, errors.Join(turnErrors...)
}

// deliver sends one notification to the person's browsers when it is still
// unread and they can still open its page.
func (runner *webPushRunner) deliver(ctx context.Context, delivery storedWebPushDelivery, now time.Time) error {
	app := runner.app
	// Nothing goes to an account being deleted, deleted or suspended, as
	// with its emails (sendNotificationEmailBatch).
	if active, err := accountMayAct(app, delivery.UserID); err != nil {
		return err
	} else if !active {
		return settleWebPushDelivery(ctx, app, delivery, "skipped", now)
	}
	items, err := loadNotificationEmailItems(ctx, app, []storedNotificationEmail{{
		NotificationID: delivery.NotificationID, UserID: delivery.UserID,
	}})
	if err != nil {
		return err
	}
	item, found := items[delivery.NotificationID]
	if !found || item.ReadAt > 0 || item.ArchivedAt > 0 {
		return settleWebPushDelivery(ctx, app, delivery, "skipped", now)
	}
	if err := authorizeNotificationSubject(app, delivery.UserID, item.PageID, item.Target); err != nil {
		if isPermissionDenied(err) {
			return settleWebPushDelivery(ctx, app, delivery, "skipped", now)
		}
		return err
	}
	// Every device the person has, a window at a time.
	readSubscriptions := func(after string) ([]storedWebPushSubscription, error) {
		subscriptions := []storedWebPushSubscription{}
		err := app.DB().NewQuery(`
			SELECT [[id]], [[endpoint]], [[p256dh]], [[auth]], [[failures]] FROM {{leafen_web_push_subscriptions}}
			WHERE [[user_id]] = {:userId} AND [[id]] > {:after} ORDER BY [[id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"userId": delivery.UserID, "after": after, "limit": webPushSubscriptionWindow,
		}).All(&subscriptions)
		return subscriptions, err
	}
	subscriptions, err := readSubscriptions("")
	if err != nil {
		return err
	}
	if len(subscriptions) == 0 {
		return settleWebPushDelivery(ctx, app, delivery, "skipped", now)
	}
	payload, err := webPushNotificationPayload(app, delivery.UserID, item)
	if err != nil {
		return err
	}
	key, err := webPushKey(app)
	if err != nil {
		return err
	}
	sent, retry := false, false
	for index := 0; index < len(subscriptions); index++ {
		subscription := subscriptions[index]
		if index == len(subscriptions)-1 && len(subscriptions)%webPushSubscriptionWindow == 0 {
			more, err := readSubscriptions(subscription.ID)
			if err != nil {
				return err
			}
			subscriptions = append(subscriptions, more...)
		}
		status, sendErr := runner.send(ctx, key, subscription, payload, now)
		switch {
		case sendErr != nil || status == http.StatusTooManyRequests || status >= 500:
			retry = true
		case status >= 200 && status < 300:
			sent = true
			err = updateWebPushSubscriptionFailures(ctx, app, subscription.ID, 0)
		case status == http.StatusNotFound || status == http.StatusGone || subscription.Failures+1 >= webPushMaxSubscriberFails:
			_, err = app.DB().NewQuery(`DELETE FROM {{leafen_web_push_subscriptions}} WHERE [[id]] = {:id}`).
				WithContext(ctx).Bind(dbx.Params{"id": subscription.ID}).Execute()
		default:
			err = updateWebPushSubscriptionFailures(ctx, app, subscription.ID, subscription.Failures+1)
		}
		if err != nil {
			return err
		}
	}
	switch {
	case sent:
		return settleWebPushDelivery(ctx, app, delivery, "sent", now)
	case retry && delivery.Attempts+1 < webPushMaxAttempts:
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_web_push_deliveries}} SET [[attempts]] = [[attempts]] + 1,
				[[next_attempt_at]] = {:next}, [[updated_at]] = {:now}
			WHERE [[notification_id]] = {:id} AND [[status]] = 'pending'
		`).WithContext(ctx).Bind(dbx.Params{
			"id": delivery.NotificationID, "now": now.UnixMilli(),
			"next": now.Add(time.Duration(delivery.Attempts+1) * time.Minute).UnixMilli(),
		}).Execute()
		return err
	default:
		return settleWebPushDelivery(ctx, app, delivery, "failed", now)
	}
}

// webPushNotificationPayload words a push as its email would be headed, in
// the person's language, with the notification's preview and link.
func webPushNotificationPayload(app core.App, userID string, item storedNotificationEmailItem) ([]byte, error) {
	language, err := serverTextLanguage(app, userID, nil)
	if err != nil {
		return nil, err
	}
	actors, err := loadNotificationEmailActors(app, userID, language, []storedNotificationEmailItem{item})
	if err != nil {
		return nil, err
	}
	return json.Marshal(webPushPayload{
		Title: notificationEmailHeadline(language, item, notificationEmailItemActor(language, actors, item)),
		Body:  inviteMessageQuoted(item.Kind, notificationEmailLine(notificationLineAsItStands(item.Preview, item.PreviewSpans, language, accountLocation(app, userID), time.Now()), notificationEmailPreviewRunes)),
		URL:   item.Target, Tag: item.ID,
	})
}

func (runner *webPushRunner) send(
	ctx context.Context, key *ecdsa.PrivateKey, subscription storedWebPushSubscription, payload []byte, now time.Time,
) (int, error) {
	if !runner.allowEndpoint(subscription.Endpoint) {
		return http.StatusGone, nil
	}
	endpoint, err := url.Parse(subscription.Endpoint)
	if err != nil {
		return http.StatusGone, nil
	}
	body, err := encryptWebPushPayload(subscription.P256DH, subscription.Auth, payload)
	if err != nil {
		return http.StatusGone, nil
	}
	subject := "mailto:notifications@leafen.invalid"
	if origin := configuredPublicOrigin(); origin != "" {
		subject = origin
	}
	authorization, err := webPushAuthorization(key, endpoint.Scheme+"://"+endpoint.Host, subject, now)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, subscription.Endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Encoding", "aes128gcm")
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("TTL", strconv.Itoa(webPushTTLSeconds))
	request.Header.Set("Urgency", "normal")
	response, err := runner.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
	return response.StatusCode, nil
}

func updateWebPushSubscriptionFailures(ctx context.Context, app core.App, id string, failures int) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_web_push_subscriptions}} SET [[failures]] = {:failures} WHERE [[id]] = {:id}
	`).WithContext(ctx).Bind(dbx.Params{"id": id, "failures": failures}).Execute()
	return err
}

func settleWebPushDelivery(
	ctx context.Context, app core.App, delivery storedWebPushDelivery, status string, now time.Time,
) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_web_push_deliveries}}
		SET [[status]] = {:status}, [[attempts]] = [[attempts]] + 1, [[settled_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[notification_id]] = {:id} AND [[status]] = 'pending'
	`).WithContext(ctx).Bind(dbx.Params{"id": delivery.NotificationID, "status": status, "now": now.UnixMilli()}).Execute()
	return err
}

func notifyWebPushRunnerAfterCommit(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(webPushRunnerStoreKey).(*webPushRunner); ok {
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
