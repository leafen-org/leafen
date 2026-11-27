package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mime"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// Notion's page update and workspace digest emails. Once a day a person
// hears of the pages they follow or created that someone else changed since
// the last digest; once a week, of the pages most read in their workspaces.
// Each person's first digest window starts when the server first sees them,
// so nobody receives their history. Digests use the administrator's SMTP,
// check again that each page can still be opened, and carry an unsubscribe
// link for that digest alone.

const (
	notificationDigestPageUpdates     = "page_updates"
	notificationDigestWorkspace       = "workspace"
	notificationDigestPageUpdateEvery = 24 * time.Hour
	notificationDigestWorkspaceEvery  = 7 * 24 * time.Hour
	notificationDigestTurnLimit       = 8
	notificationDigestBaselineLimit   = 200
	notificationDigestItemLimit       = 20
	notificationDigestPopularLimit    = 5
	notificationDigestRetryDelay      = time.Hour
	notificationDigestMaxAttempts     = 5
	notificationDigestIdleWait        = time.Hour
	notificationDigestRunnerStoreKey  = "leafenNotificationDigestRunner"
	notificationDigestAccountHookID   = "leafenNotificationDigestAccount"
	pocketBaseDateTimeLayout          = "2006-01-02 15:04:05.000Z"
)

type storedNotificationDigest struct {
	UserID     string `db:"user_id"`
	Kind       string `db:"kind"`
	LastSentAt int64  `db:"last_sent_at"`
	Attempts   int    `db:"attempts"`
}

type notificationDigestItem struct {
	PageID string `db:"id"`
	Title  string `db:"title"`
	Editor string `db:"last_edited_by"`
	Views  int    `db:"views"`
	// NameKind is its page's kind, which names it while untitled.
	NameKind pageNameKind `db:"-"`
}

func notificationDigestPeriod(kind string) time.Duration {
	if kind == notificationDigestWorkspace {
		return notificationDigestWorkspaceEvery
	}
	return notificationDigestPageUpdateEvery
}

// collectPageUpdateDigest lists the pages a person follows or created that
// someone else changed since the last digest. It reads those pages alone, the
// ones they made by their maker (pageCreatorIndex) and the ones they follow
// by their follows, so a person's digest reads what they made and follow
// rather than every page of the server.
func collectPageUpdateDigest(ctx context.Context, app core.App, userID string, since int64) ([]notificationDigestItem, error) {
	return readableNotificationDigestItems(app, userID, notificationDigestItemLimit*2, notificationDigestItemLimit,
		func(offset, size int) ([]notificationDigestItem, error) {
			items := []notificationDigestItem{}
			err := app.DB().NewQuery(`
				SELECT [[page]].[[id]], [[page]].[[title]], [[page]].[[last_edited_by]], 0 AS [[views]]
				FROM {{leafen_pages}} AS [[page]]
				WHERE [[page]].[[id]] IN (
						SELECT [[id]] FROM {{leafen_pages}} INDEXED BY [[` + pageCreatorIndex + `]]
						WHERE [[created_by]] = {:userId} AND [[data_source_id]] = ''
						UNION ALL
						SELECT [[page_id]] FROM {{leafen_page_notification_preferences}} WHERE [[user_id]] = {:userId}
					)
					AND [[page]].[[in_trash]] = 0 AND trim([[page]].[[archived_at]]) = '' AND [[page]].[[data_source_id]] = ''
					AND [[page]].[[notion_import_staging]] = 0
					AND [[page]].[[updated]] > {:since}
					AND [[page]].[[last_edited_by]] != '' AND [[page]].[[last_edited_by]] != {:userId}
				ORDER BY [[page]].[[updated]] DESC, [[page]].[[id]] LIMIT {:limit} OFFSET {:offset}
			`).WithContext(ctx).Bind(dbx.Params{
				"userId": userID, "since": time.UnixMilli(since).UTC().Format(pocketBaseDateTimeLayout),
				"limit": size, "offset": offset,
			}).All(&items)
			return items, err
		})
}

// collectWorkspaceDigest lists the pages most read by members over the
// last week in the person's workspaces, a week of days on their clock, as the
// views fall on their viewers' days (page_analytics_clock.go).
func collectWorkspaceDigest(ctx context.Context, app core.App, userID string, now time.Time) ([]notificationDigestItem, error) {
	return readableNotificationDigestItems(app, userID, notificationDigestPopularLimit*4, notificationDigestPopularLimit,
		func(offset, size int) ([]notificationDigestItem, error) {
			items := []notificationDigestItem{}
			err := app.DB().NewQuery(`
				SELECT [[page]].[[id]], [[page]].[[title]], [[page]].[[last_edited_by]],
					SUM([[daily]].[[unique_member_views]]) AS [[views]]
				FROM {{leafen_page_analytics_daily}} AS [[daily]]
				JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[daily]].[[page_id]]
				WHERE [[daily]].[[day]] >= {:since} AND [[daily]].[[workspace_id]] IN (
						SELECT [[id]] FROM {{leafen_workspaces}} WHERE [[workspace_owner]] = {:userId}
						UNION SELECT [[workspace_id]] FROM {{leafen_workspace_memberships}} WHERE [[user_id]] = {:userId}
					)
					AND [[page]].[[in_trash]] = 0 AND trim([[page]].[[archived_at]]) = '' AND [[page]].[[data_source_id]] = ''
				GROUP BY [[page]].[[id]]
				HAVING [[views]] > 0
				ORDER BY [[views]] DESC, [[page]].[[id]] LIMIT {:limit} OFFSET {:offset}
			`).WithContext(ctx).Bind(dbx.Params{
				"userId": userID, "since": pageAnalyticsDayOf(pageAnalyticsToday(app, userID, now).AddDate(0, 0, -7)),
				"limit": size, "offset": offset,
			}).All(&items)
			return items, err
		})
}

// readableNotificationDigestItems keeps, of the pages read gives a window at
// a time (from an offset, of a size), up to limit the person may read, in
// order: a fixed number read and then those closed to them left out, a
// digest could come out empty with pages they could read past them
// (fillWindows, 2026-11-23).
func readableNotificationDigestItems(
	app core.App, userID string, first, limit int, read func(offset, size int) ([]notificationDigestItem, error),
) ([]notificationDigestItem, error) {
	offset := 0
	readable, err := fillWindows(first, limit,
		func(size int, _ *notificationDigestItem) ([]notificationDigestItem, error) {
			items, err := read(offset, size)
			offset += len(items)
			return items, err
		},
		func(item *notificationDigestItem) (bool, error) {
			if _, err := authorizeCommentNotificationTarget(app, userID, item.PageID); err != nil {
				if isPermissionDenied(err) {
					return false, nil
				}
				return false, err
			}
			return true, nil
		})
	if err != nil {
		return nil, err
	}
	// Each title as the person reads it (page_name_mentions.go): a mention in
	// it closed to them in marks, which the email says "@찾을 수 없음"
	// (closedMentionsInTitle), as its notices' headlines do.
	titles := make(map[string]*string, len(readable))
	for index := range readable {
		titles[readable[index].PageID] = &readable[index].Title
	}
	pageTitlesAsRead(app, userID, titles, true)
	ids := make([]string, 0, len(readable))
	for _, item := range readable {
		ids = append(ids, item.PageID)
	}
	kinds, err := loadPageNameKinds(app, ids)
	if err != nil {
		return nil, err
	}
	for index := range readable {
		readable[index].NameKind = kinds[readable[index].PageID]
	}
	return readable, nil
}

// buildNotificationDigestEmail writes one plain-text digest.
func buildNotificationDigestEmail(
	settings storedAdministratorSettings,
	recipient, language, kind string,
	items []notificationDigestItem,
	editors map[string]string,
	origin, unsubscribe string,
) []byte {
	subject := "리펜 페이지 업데이트"
	if kind == notificationDigestWorkspace {
		subject = "리펜 워크스페이스 요약"
	}
	if language != accountLanguageKo {
		subject = "Page updates in Leafen"
		if kind == notificationDigestWorkspace {
			subject = "Your Leafen workspace digest"
		}
	}
	var body strings.Builder
	for _, item := range items {
		title := notificationEmailLine(closedMentionsInTitle(item.Title, language), notificationEmailTitleRunes)
		if title == "" {
			title = untitledNotificationPageTitleOf(language, item.NameKind)
		}
		editor := notificationEmailActor(language, editors, item.Editor)
		switch {
		case kind == notificationDigestWorkspace && language == accountLanguageKo:
			body.WriteString(fmt.Sprintf("%s — 멤버 %d명이 읽음\r\n", title, item.Views))
		case kind == notificationDigestWorkspace:
			body.WriteString(fmt.Sprintf("%s — read by %d members\r\n", title, item.Views))
		case language == accountLanguageKo:
			body.WriteString(title + " — " + editor + "님이 편집함\r\n")
		default:
			body.WriteString(title + " — edited by " + editor + "\r\n")
		}
		if origin != "" {
			body.WriteString("  " + origin + "/p/" + url.PathEscape(item.PageID) + "\r\n")
		}
	}
	body.WriteString("\r\n--\r\n")
	if language == accountLanguageKo {
		body.WriteString("설정 → 알림에서 이메일 알림을 바꿀 수 있습니다.\r\n")
		if unsubscribe != "" {
			body.WriteString("이 요약 받지 않기: " + unsubscribe + "\r\n")
		}
	} else {
		body.WriteString("Change email notifications in Settings → Notifications.\r\n")
		if unsubscribe != "" {
			body.WriteString("Stop this digest: " + unsubscribe + "\r\n")
		}
	}
	headers := []string{
		"From: " + (&mail.Address{Name: settings.SenderName, Address: settings.SenderAddress}).String(),
		"To: " + (&mail.Address{Address: recipient}).String(),
		"Subject: " + mime.QEncoding.Encode("UTF-8", subject),
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"Auto-Submitted: auto-generated",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	if unsubscribe != "" {
		headers = append(headers,
			"List-Unsubscribe: <"+unsubscribe+">", "List-Unsubscribe-Post: List-Unsubscribe=One-Click")
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body.String())
}

func rescheduleNotificationDigest(
	ctx context.Context, app core.App, digest storedNotificationDigest, sent bool, now time.Time,
) error {
	lastSent, next, attempts := now.UnixMilli(), now.Add(notificationDigestPeriod(digest.Kind)).UnixMilli(), 0
	if !sent && digest.Attempts+1 < notificationDigestMaxAttempts {
		lastSent, next, attempts = digest.LastSentAt, now.Add(notificationDigestRetryDelay).UnixMilli(), digest.Attempts+1
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notification_digests}}
		SET [[last_sent_at]] = {:lastSent}, [[next_due_at]] = {:next}, [[attempts]] = {:attempts}
		WHERE [[user_id]] = {:userId} AND [[kind]] = {:kind}
	`).WithContext(ctx).Bind(dbx.Params{
		"lastSent": lastSent, "next": next, "attempts": attempts, "userId": digest.UserID, "kind": digest.Kind,
	}).Execute()
	return err
}

// sendNotificationDigest sends one person's due digest, or moves its window
// on when they turned it off or nothing happened.
func sendNotificationDigest(
	ctx context.Context, app core.App, administrator storedAdministratorSettings,
	digest storedNotificationDigest, now time.Time,
) error {
	settings, err := loadNotificationSettings(app, digest.UserID)
	if err != nil {
		return err
	}
	user, userErr := app.FindRecordById("users", digest.UserID)
	wanted := (digest.Kind == notificationDigestPageUpdates && settings.EmailPageUpdates) ||
		(digest.Kind == notificationDigestWorkspace && settings.EmailDigest)
	if userErr != nil || strings.TrimSpace(user.Email()) == "" || !wanted {
		return rescheduleNotificationDigest(ctx, app, digest, true, now)
	}
	if active, err := accountMayAct(app, digest.UserID); err != nil {
		return err
	} else if !active {
		return rescheduleNotificationDigest(ctx, app, digest, true, now)
	}
	items := []notificationDigestItem{}
	if digest.Kind == notificationDigestWorkspace {
		items, err = collectWorkspaceDigest(ctx, app, digest.UserID, now)
	} else {
		items, err = collectPageUpdateDigest(ctx, app, digest.UserID, digest.LastSentAt)
	}
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return rescheduleNotificationDigest(ctx, app, digest, true, now)
	}
	editorItems := make([]storedNotificationEmailItem, 0, len(items))
	for _, item := range items {
		editorItems = append(editorItems, storedNotificationEmailItem{ActorID: item.Editor})
	}
	language, err := serverTextLanguage(app, digest.UserID, nil)
	if err != nil {
		return err
	}
	editors, err := loadNotificationEmailActors(app, digest.UserID, language, editorItems)
	if err != nil {
		return err
	}
	unsubscribe := ""
	if origin := othersLinkOrigin(); origin != "" {
		token, err := notificationEmailUnsubscribeToken(app, digest.UserID)
		if err != nil {
			return err
		}
		unsubscribe = origin + apiRoot + "/notification-email/unsubscribe?" +
			url.Values{"token": {token}, "kind": {digest.Kind}}.Encode()
	}
	password, err := decryptAdministratorSetting(app, administrator.SMTPPasswordCipher)
	if err != nil {
		return rescheduleNotificationDigest(ctx, app, digest, false, now)
	}
	message := buildNotificationDigestEmail(administrator, strings.TrimSpace(user.Email()), language, digest.Kind,
		items, editors, othersLinkOrigin(), unsubscribe)
	outcome, sendErr := sendConfiguredSMTP(administrator, password, strings.TrimSpace(user.Email()), message)
	if outcome == smtpDeliverySettledFailure {
		logNotificationEmailFailure(app, "digest-failed", sendErr)
	}
	// An uncertain delivery is not retried, so nobody gets a digest twice.
	return rescheduleNotificationDigest(ctx, app, digest, outcome != smtpDeliverySettledFailure, now)
}

type notificationDigestRunner struct {
	app      core.App
	wake     chan struct{}
	baseline notificationDigestBaseline

	runMu  sync.Mutex
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// newNotificationDigestRunner makes the runner and has each new account
// scheduled at its next turn.
func newNotificationDigestRunner(app core.App) *notificationDigestRunner {
	runner := &notificationDigestRunner{app: app, wake: make(chan struct{}, 1)}
	app.OnRecordAfterCreateSuccess("users").Bind(&hook.Handler[*core.RecordEvent]{
		Id: notificationDigestAccountHookID,
		Func: func(event *core.RecordEvent) error {
			runner.baseline.accountCreated(event.Record.Id)
			return event.Next()
		},
	})
	return runner
}

func (runner *notificationDigestRunner) Start() {
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

func (runner *notificationDigestRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("notification digest runner shutdown: %w", ctx.Err())
	}
}

func (runner *notificationDigestRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := time.Now().UTC()
		more, err := runner.runTurn(ctx, now)
		wait := notificationDigestIdleWait
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logNotificationEmailFailure(runner.app, "digest-turn", err)
		} else if more {
			wait = time.Second
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

// runTurn schedules new people and sends a bounded number of due digests,
// reporting whether more are due. Without SMTP nothing is scheduled.
func (runner *notificationDigestRunner) runTurn(ctx context.Context, now time.Time) (bool, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	administrator, err := loadAdministratorSettings(runner.app)
	if err != nil {
		return false, err
	}
	if !notificationEmailConfigured(administrator) {
		runner.baseline.reset()
		return false, nil
	}
	if err := runner.baseline.advance(ctx, runner.app, now); err != nil {
		return false, err
	}
	due := []storedNotificationDigest{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[user_id]], [[kind]], [[last_sent_at]], [[attempts]] FROM {{leafen_notification_digests}}
		INDEXED BY [[idx_leafen_notification_digests_due]]
		WHERE [[next_due_at]] <= {:now} ORDER BY [[next_due_at]], [[user_id]], [[kind]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": now.UnixMilli(), "limit": notificationDigestTurnLimit + 1}).All(&due); err != nil {
		return false, err
	}
	more := len(due) > notificationDigestTurnLimit
	if more {
		due = due[:notificationDigestTurnLimit]
	}
	turnErrors := []error{}
	for _, digest := range due {
		if err := ctx.Err(); err != nil {
			return more, errors.Join(append(turnErrors, err)...)
		}
		if err := sendNotificationDigest(ctx, runner.app, administrator, digest, now); err != nil &&
			!errors.Is(err, sql.ErrNoRows) {
			turnErrors = append(turnErrors, err)
		}
	}
	return more, errors.Join(turnErrors...)
}
