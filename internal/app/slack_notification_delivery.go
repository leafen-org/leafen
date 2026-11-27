package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	slackNotificationDelay          = 10 * time.Second
	slackNotificationMaxAttempts    = 5
	slackNotificationRetention      = 7 * 24 * time.Hour
	slackNotificationTurnLimit      = 32
	slackNotificationCleanupLimit   = 100
	slackNotificationRunnerStoreKey = "leafenSlackNotificationRunner"
	slackNotificationMaintenance    = time.Hour
	slackNotificationFailureDelay   = time.Minute
	slackAPIBaseURL                 = "https://slack.com/api"
)

type storedSlackNotificationDelivery struct {
	NotificationID string `db:"notification_id"`
	WorkspaceID    string `db:"workspace_id"`
	UserID         string `db:"user_id"`
	Attempts       int    `db:"attempts"`
}

type slackNotificationRunner struct {
	app     core.App
	client  *http.Client
	apiBase string
	getenv  func(string) string
	wake    chan struct{}

	runMu  sync.Mutex
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newSlackNotificationRunner(app core.App) *slackNotificationRunner {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = outboundPublicProxy
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return outboundPublicDialContext(ctx, network, address, "", net.DefaultResolver.LookupNetIP, dialer.DialContext)
	}
	return &slackNotificationRunner{
		app: app, apiBase: slackAPIBaseURL, getenv: os.Getenv, wake: make(chan struct{}, 1),
		client: &http.Client{
			Transport: transport, Timeout: databaseAutomationDeliveryRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (runner *slackNotificationRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel, runner.done = cancel, make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *slackNotificationRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("slack notification runner shutdown: %w", ctx.Err())
	}
}

func (runner *slackNotificationRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func notifySlackNotificationRunnerAfterCommit(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(slackNotificationRunnerStoreKey).(*slackNotificationRunner); ok {
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

func (runner *slackNotificationRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := time.Now().UTC()
		hasMore, nextDue, err := runner.runTurn(ctx, now)
		wait := slackNotificationMaintenance
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// The database busy, its notices wait for the next turn, soon (runner_turn_wait.go).
			wait = slackNotificationFailureDelay
			if logRunnerTurnWait(runner.app, "notification.slack.send", err) {
				wait = runnerTurnBusyDelay
			} else {
				logNotificationEmailFailure(runner.app, "slack-notification-turn", err)
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

// runTurn sends the due messages and forgets settled ones after a week. It
// reports whether more are due and when the next one is.
func (runner *slackNotificationRunner) runTurn(ctx context.Context, now time.Time) (bool, int64, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	if _, err := runner.app.DB().NewQuery(`
		DELETE FROM {{leafen_slack_notification_deliveries}} WHERE [[notification_id]] IN (
			SELECT [[notification_id]] FROM {{leafen_slack_notification_deliveries}}
			WHERE [[settled_at]] > 0 AND [[settled_at]] < {:before}
			ORDER BY [[settled_at]], [[notification_id]] LIMIT {:limit}
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"before": now.Add(-slackNotificationRetention).UnixMilli(), "limit": slackNotificationCleanupLimit,
	}).Execute(); err != nil {
		return false, -1, err
	}
	due := []storedSlackNotificationDelivery{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[notification_id]], [[workspace_id]], [[user_id]], [[attempts]]
		FROM {{leafen_slack_notification_deliveries}} INDEXED BY [[idx_leafen_slack_notification_deliveries_due]]
		WHERE [[status]] = 'pending' AND [[next_attempt_at]] <= {:now}
		ORDER BY [[next_attempt_at]], [[notification_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": now.UnixMilli(), "limit": slackNotificationTurnLimit + 1}).All(&due); err != nil {
		return false, -1, err
	}
	hasMore := len(due) > slackNotificationTurnLimit
	if hasMore {
		due = due[:slackNotificationTurnLimit]
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
		SELECT MIN([[next_attempt_at]]) FROM {{leafen_slack_notification_deliveries}} WHERE [[status]] = 'pending'
	`).WithContext(ctx).Row(&nextDue); err != nil {
		turnErrors = append(turnErrors, err)
	}
	next := int64(-1)
	if nextDue.Valid {
		next = nextDue.Int64
	}
	return hasMore, next, errors.Join(turnErrors...)
}

// deliver sends one notification to the person in Slack when it is still
// unread, they can still open its page and Slack notifications are still on.
func (runner *slackNotificationRunner) deliver(ctx context.Context, delivery storedSlackNotificationDelivery, now time.Time) error {
	app := runner.app
	connectionID, token := slackNotificationConnection(runner.getenv)
	enabled := 0
	if err := app.DB().NewQuery(`SELECT [[enabled]] FROM {{leafen_slack_notification_settings}} WHERE [[user_id]] = {:userId}`).
		WithContext(ctx).Bind(dbx.Params{"userId": delivery.UserID}).Row(&enabled); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if connectionID == "" || enabled != 1 {
		return runner.settle(ctx, delivery, "skipped", "", now)
	}
	// Nothing goes to an account being deleted, deleted or suspended, as
	// with its emails (sendNotificationEmailBatch).
	if active, err := accountMayAct(app, delivery.UserID); err != nil {
		return err
	} else if !active {
		return runner.settle(ctx, delivery, "skipped", "recipient_inactive", now)
	}
	items, err := loadNotificationEmailItems(ctx, app, []storedNotificationEmail{{
		NotificationID: delivery.NotificationID, UserID: delivery.UserID,
	}})
	if err != nil {
		return err
	}
	item, found := items[delivery.NotificationID]
	if !found || item.ReadAt > 0 || item.ArchivedAt > 0 {
		return runner.settle(ctx, delivery, "skipped", "", now)
	}
	if err := authorizeNotificationSubject(app, delivery.UserID, item.PageID, item.Target); err != nil {
		if isPermissionDenied(err) {
			return runner.settle(ctx, delivery, "skipped", "", now)
		}
		return err
	}
	slackUserID, outcome, code := runner.slackUser(ctx, delivery.UserID, connectionID, token)
	if outcome == databaseAutomationDeliveryResultSucceeded {
		text, err := slackNotificationText(app, delivery.UserID, item)
		if err != nil {
			return err
		}
		outcome, code = runner.post(ctx, token, slackUserID, text, delivery.NotificationID)
	}
	switch {
	case outcome == databaseAutomationDeliveryResultSucceeded:
		return runner.settle(ctx, delivery, "sent", "", now)
	case outcome == databaseAutomationDeliveryResultRetryable && delivery.Attempts+1 < slackNotificationMaxAttempts:
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_slack_notification_deliveries}} SET [[attempts]] = [[attempts]] + 1,
				[[next_attempt_at]] = {:next}, [[error_code]] = {:code}, [[updated_at]] = {:now}
			WHERE [[notification_id]] = {:id} AND [[status]] = 'pending'
		`).WithContext(ctx).Bind(dbx.Params{
			"id": delivery.NotificationID, "now": now.UnixMilli(), "code": code,
			"next": now.Add(time.Duration(delivery.Attempts+1) * time.Minute).UnixMilli(),
		}).Execute()
		return err
	default:
		return runner.settle(ctx, delivery, "failed", code, now)
	}
}

// slackUser is the person's Slack user in the bot's workspace, found once by
// their Leafen email and kept until the operator names another connection.
func (runner *slackNotificationRunner) slackUser(ctx context.Context, userID, connectionID, token string) (string, string, string) {
	stored := struct {
		ConnectionID string `db:"connection_id"`
		SlackUserID  string `db:"slack_user_id"`
	}{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[connection_id]], [[slack_user_id]] FROM {{leafen_slack_notification_settings}} WHERE [[user_id]] = {:userId}
	`).WithContext(ctx).Bind(dbx.Params{"userId": userID}).One(&stored); err != nil {
		return "", databaseAutomationDeliveryResultRetryable, "slack_settings_unreadable"
	}
	if stored.ConnectionID == connectionID && stored.SlackUserID != "" {
		return stored.SlackUserID, databaseAutomationDeliveryResultSucceeded, ""
	}
	user, err := runner.app.FindRecordById("users", userID)
	email := ""
	if err == nil {
		email = strings.TrimSpace(user.GetString("email"))
	}
	if email == "" {
		return "", databaseAutomationDeliveryResultFailed, "slack_user_email_missing"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		runner.apiBase+"/users.lookupByEmail?email="+url.QueryEscape(email), nil)
	if err != nil {
		return "", databaseAutomationDeliveryResultFailed, "slack_request_invalid"
	}
	request.Header.Set("Authorization", "Bearer "+token)
	var decoded struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	outcome, code := runner.call(request, &decoded)
	if outcome != databaseAutomationDeliveryResultSucceeded {
		return "", outcome, code
	}
	if !decoded.OK || decoded.User.ID == "" || len([]rune(decoded.User.ID)) > slackNotificationUserIDRunes {
		if slackRetryableError(decoded.Error) {
			return "", databaseAutomationDeliveryResultRetryable, "slack_provider_retryable"
		}
		return "", databaseAutomationDeliveryResultFailed, "slack_user_not_found"
	}
	if _, err := runner.app.DB().NewQuery(`
		UPDATE {{leafen_slack_notification_settings}} SET [[connection_id]] = {:connectionId}, [[slack_user_id]] = {:slackUserId}
		WHERE [[user_id]] = {:userId}
	`).WithContext(ctx).Bind(dbx.Params{
		"userId": userID, "connectionId": connectionID, "slackUserId": decoded.User.ID,
	}).Execute(); err != nil {
		return "", databaseAutomationDeliveryResultRetryable, "slack_settings_unwritable"
	}
	return decoded.User.ID, databaseAutomationDeliveryResultSucceeded, ""
}

// post sends the direct message; the same notification always carries the
// same client message ID, so a retried send does not repeat it.
func (runner *slackNotificationRunner) post(ctx context.Context, token, slackUserID, text, notificationID string) (string, string) {
	body, _ := json.Marshal(map[string]any{
		"channel": slackUserID, "text": text, "unfurl_links": false, "unfurl_media": false,
		"client_msg_id": databaseAutomationSlackClientMessageID("notification:" + notificationID),
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, runner.apiBase+"/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return databaseAutomationDeliveryResultFailed, "slack_request_invalid"
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	var decoded struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	outcome, code := runner.call(request, &decoded)
	if outcome != databaseAutomationDeliveryResultSucceeded {
		return outcome, code
	}
	if decoded.OK {
		return databaseAutomationDeliveryResultSucceeded, ""
	}
	if slackRetryableError(decoded.Error) {
		return databaseAutomationDeliveryResultRetryable, "slack_provider_retryable"
	}
	return databaseAutomationDeliveryResultFailed, "slack_provider_rejected"
}

func (runner *slackNotificationRunner) call(request *http.Request, decoded any) (string, string) {
	response, err := runner.client.Do(request)
	if err != nil {
		return databaseAutomationDeliveryResultRetryable, "slack_provider_retryable"
	}
	defer response.Body.Close()
	limited, ok := databaseAutomationReadResponse(response.Body)
	if !ok {
		return databaseAutomationDeliveryResultFailed, "slack_response_oversized"
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return databaseAutomationDeliveryResultRetryable, "slack_provider_retryable"
	}
	if json.Unmarshal(limited, decoded) != nil {
		return databaseAutomationDeliveryResultRetryable, "slack_provider_retryable"
	}
	return databaseAutomationDeliveryResultSucceeded, ""
}

func slackRetryableError(code string) bool {
	return code == "ratelimited" || code == "internal_error" || code == "fatal_error" || code == "service_unavailable"
}

// slackNotificationText words the message as the notification's email is
// headed, in the person's language, with its preview and a link when the
// server has a public address.
func slackNotificationText(app core.App, userID string, item storedNotificationEmailItem) (string, error) {
	language, err := serverTextLanguage(app, userID, nil)
	if err != nil {
		return "", err
	}
	actors, err := loadNotificationEmailActors(app, userID, language, []storedNotificationEmailItem{item})
	if err != nil {
		return "", err
	}
	lines := []string{notificationEmailHeadline(language, item, notificationEmailItemActor(language, actors, item))}
	if preview := notificationEmailLine(notificationLineAsItStands(item.Preview, item.PreviewSpans, language, accountLocation(app, userID), time.Now()), notificationEmailPreviewRunes); preview != "" {
		lines = append(lines, preview)
	}
	if origin := othersLinkOrigin(); origin != "" && strings.HasPrefix(item.Target, "/") {
		lines = append(lines, origin+item.Target)
	}
	return strings.Join(lines, "\n"), nil
}

func (runner *slackNotificationRunner) settle(
	ctx context.Context, delivery storedSlackNotificationDelivery, status, code string, now time.Time,
) error {
	_, err := runner.app.DB().NewQuery(`
		UPDATE {{leafen_slack_notification_deliveries}}
		SET [[status]] = {:status}, [[attempts]] = [[attempts]] + 1, [[error_code]] = {:code},
			[[settled_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[notification_id]] = {:id} AND [[status]] = 'pending'
	`).WithContext(ctx).Bind(dbx.Params{
		"id": delivery.NotificationID, "status": status, "code": code, "now": now.UnixMilli(),
	}).Execute()
	return err
}
