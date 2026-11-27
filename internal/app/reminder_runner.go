package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	reminderRunnerStoreKey   = "leafenReminderRunner"
	reminderDirtyPageLimit   = 8
	reminderDeliveryLimit    = 32
	reminderTurnDelay        = time.Millisecond
	reminderFailureDelay     = time.Minute
	reminderMaintenanceDelay = time.Hour
	// A change wakes the runner, which then gathers the changes that follow
	// for this long before it makes the pages again: a page written several
	// times in a row is made once (2026-10-17: made after each write, it was
	// a third of a block write's statements). Mention notices and reminders
	// may come this much later.
	reminderGatherDelay = 2 * time.Second
)

type reminderTurn struct {
	HasMore bool
	NextDue int64
	// PacedUntil is when a page held back (projection_pace.go) may be made.
	PacedUntil time.Time
}

type storedDueReminder struct {
	ID               string `db:"id"`
	WorkspaceID      string `db:"workspace_id"`
	PageID           string `db:"page_id"`
	BlockID          string `db:"block_id"`
	ReminderKey      string `db:"reminder_key"`
	CreatorID        string `db:"creator_id"`
	RecipientIDsJSON string `db:"recipient_ids_json"`
	Preview          string `db:"preview"`
	DueAt            int64  `db:"due_at"`
}

// reminderRunner projects reminders of changed pages and delivers the ones
// that are due as personal notifications.
type reminderRunner struct {
	app  core.App
	wake chan struct{}

	runMu  sync.Mutex
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newReminderRunner(app core.App) *reminderRunner {
	return &reminderRunner{app: app, wake: make(chan struct{}, 1)}
}

func (runner *reminderRunner) Start() {
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

func (runner *reminderRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	cancel := runner.cancel
	done := runner.done
	runner.cancel = nil
	runner.done = nil
	runner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", errorCodeCommentNotificationDeliveryFailed, ctx.Err())
	}
}

func (runner *reminderRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *reminderRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := time.Now().UTC()
		turn, err := runner.runTurn(ctx, now)
		wait := reminderMaintenanceDelay
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// A turn that found the database busy left its reminders due and
			// comes back soon to give them (runner_turn_wait.go).
			wait = reminderFailureDelay
			if logRunnerTurnError(runner.app, errorCodeCommentNotificationDeliveryFailed, "reminder.continue", err) {
				wait = runnerTurnBusyDelay
			}
		} else if turn.HasMore {
			wait = reminderTurnDelay
		} else if turn.NextDue >= 0 {
			wait = min(wait, max(time.Duration(turn.NextDue-now.UnixMilli())*time.Millisecond, reminderTurnDelay))
		}
		if err == nil && !turn.PacedUntil.IsZero() {
			wait = min(wait, max(time.Until(turn.PacedUntil), reminderTurnDelay))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-runner.wake:
			timer.Stop()
			if !runner.gather(ctx) {
				return
			}
		case <-timer.C:
		}
		timer.Stop()
	}
}

// gather waits reminderGatherDelay after a wake, or until the runner stops;
// the wakes in between are for the turn that follows.
func (runner *reminderRunner) gather(ctx context.Context) bool {
	timer := time.NewTimer(reminderGatherDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
	}
	select {
	case <-runner.wake:
	default:
	}
	return true
}

func (runner *reminderRunner) runTurn(ctx context.Context, now time.Time) (reminderTurn, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	turn := reminderTurn{NextDue: -1}
	dirty := []storedReminderDirtyPage{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[page_id]], [[version]] FROM {{leafen_reminder_dirty_pages}}
		INDEXED BY [[idx_leafen_reminder_dirty_pages_order]]
		ORDER BY [[enqueued_at]], [[page_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"limit": reminderDirtyPageLimit + 1}).All(&dirty); err != nil {
		return turn, err
	}
	turn.HasMore = len(dirty) > reminderDirtyPageLimit
	if turn.HasMore {
		dirty = dirty[:reminderDirtyPageLimit]
	}
	turnErrors := []error{}
	made := 0
	for _, page := range dirty {
		// A long page changed again and again waits its turn (projection_pace.go).
		if ready, at := reminderProjectionPace.ready(page.PageID, time.Now()); !ready {
			if turn.PacedUntil.IsZero() || at.Before(turn.PacedUntil) {
				turn.PacedUntil = at
			}
			continue
		}
		// People's edits first (write_lane.go).
		yieldToPeople(ctx)
		began := time.Now()
		if err := projectPageReminders(ctx, runner.app, page, now); err != nil {
			turnErrors = append(turnErrors, err)
			// The pages after a busy one would each wait as long: the turn
			// stops there and its next one goes on from it.
			if isSQLiteBusyError(err) {
				return turn, errors.Join(turnErrors...)
			}
		}
		reminderProjectionPace.made(page.PageID, began, time.Now())
		made++
	}
	// Pages all held back are no backlog to run straight on with.
	if made == 0 {
		turn.HasMore = false
	}
	due := []storedDueReminder{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[id]], [[workspace_id]], [[page_id]], [[block_id]], [[reminder_key]], [[creator_id]],
			[[recipient_ids_json]], [[preview]], [[due_at]]
		FROM {{leafen_reminders}} INDEXED BY [[idx_leafen_reminders_due]]
		WHERE [[status]] = 'pending' AND [[due_at]] <= {:now}
		ORDER BY [[due_at]], [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"now": now.UnixMilli(), "limit": reminderDeliveryLimit + 1,
	}).All(&due); err != nil {
		return turn, errors.Join(append(turnErrors, err)...)
	}
	if len(due) > reminderDeliveryLimit {
		turn.HasMore = true
		due = due[:reminderDeliveryLimit]
	}
	for _, reminder := range due {
		if err := deliverReminder(ctx, runner.app, reminder, now); err != nil {
			turnErrors = append(turnErrors, err)
			if isSQLiteBusyError(err) {
				return turn, errors.Join(turnErrors...)
			}
		}
	}
	// Deadlines someone must not miss (deadline_notices.go) go the same way.
	if more, err := deliverDueDeadlineNotices(ctx, runner.app, now); err != nil {
		turnErrors = append(turnErrors, err)
	} else if more {
		turn.HasMore = true
	}
	var next int64 = -1
	if err := runner.app.DB().NewQuery(`
		SELECT COALESCE(MIN([[due_at]]), -1) FROM {{leafen_reminders}}
		INDEXED BY [[idx_leafen_reminders_due]] WHERE [[status]] = 'pending'
	`).WithContext(ctx).Row(&next); err != nil {
		turnErrors = append(turnErrors, err)
	}
	if deadline, err := nextDeadlineNoticeAt(ctx, runner.app); err != nil {
		turnErrors = append(turnErrors, err)
	} else if deadline >= 0 && (next < 0 || deadline < next) {
		next = deadline
	}
	turn.NextDue = next
	return turn, errors.Join(turnErrors...)
}

// deliverReminder gives each person a reminder is for a personal
// notification, when the page is still out of trash and they can still open
// it. The reminder is delivered once.
func deliverReminder(ctx context.Context, app core.App, reminder storedDueReminder, now time.Time) error {
	return app.RunInTransaction(func(txApp core.App) error {
		settle := func() error {
			_, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_reminders}} SET [[status]] = 'delivered', [[delivered_at]] = {:now},
					[[updated_at]] = {:now}
				WHERE [[id]] = {:id} AND [[status]] = 'pending'
			`).WithContext(ctx).Bind(dbx.Params{"id": reminder.ID, "now": now.UnixMilli()}).Execute()
			return err
		}
		page, err := txApp.FindRecordById(pagesCollection, reminder.PageID)
		if errors.Is(err, sql.ErrNoRows) {
			return settle()
		}
		if err != nil {
			return err
		}
		// A page in the trash, or under a page there, reminds no one
		// (loadPageTrashEffects); nor does a database item in the trash or
		// whose database is.
		if trashed, err := pageInTrashPath(txApp, page.Id); err != nil {
			return err
		} else if trashed {
			return settle()
		}
		recipients := []string{}
		if err := json.Unmarshal([]byte(reminder.RecipientIDsJSON), &recipients); err != nil {
			return err
		}
		title := strings.TrimSpace(page.GetString("title"))
		target := "/p/" + url.PathEscape(reminder.PageID) + pageBlockAnchor(txApp, reminder.PageID, reminder.BlockID)
		// A database date reminder names its item on the page holding the
		// database, and a database page's own reminder is on that item; both
		// open the item as its own page (databaseRowPageTarget).
		rowID := ""
		if strings.HasPrefix(reminder.ReminderKey, databaseDateReminderKeyPrefix) {
			rowID = reminder.BlockID
		} else if isDatabaseRowPageRecord(page) {
			rowID = page.Id
			if _, err := databaseRowPageContainer(txApp, page); isPermissionDenied(err) {
				return settle()
			} else if err != nil {
				return err
			}
		}
		row := rowID != ""
		if row {
			title = databaseRowPageTitle(txApp, rowID)
			target = databaseRowPageTarget(rowID)
		}
		// An untitled page keeps its empty name: each place that shows the
		// reminder names it in its reader's language ("새 페이지", "New page").
		previewSpans := reminderPreviewSpans(txApp, reminder.PageID, reminder.BlockID, rowID != "" && reminder.BlockID == rowID)
		projections := make([]productNotificationProjection, 0, len(recipients))
		for _, userID := range recipients {
			if !row {
				// A page an operation holds a while still reminds them (page_access_hold.go).
				if _, err := pageAccessDespiteHold(authorizePage(txApp, userID, reminder.PageID, permissionWorkspaceRead)); err != nil {
					if isPermissionDenied(err) {
						continue
					}
					return err
				}
			} else {
				// An item's reminder goes to whoever opens the item, through
				// its table's page or its own share, as its other notices do
				// (notification_item_subjects.go).
				authority, err := authorizeDatabaseRowPage(txApp, userID, rowID, permissionWorkspaceRead)
				if pageAccessHeld(err) != "" {
					authority, err = loadDatabaseRowPageAuthority(txApp, rowID)
				}
				if errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) {
					continue
				}
				if err != nil {
					return err
				}
				// A row in trash does not remind anyone until it is restored.
				if authority.TrashedAt != 0 {
					continue
				}
			}
			activityKey := "reminder:" + reminder.ID + ":" + strconv.FormatInt(reminder.DueAt, 10)
			digest := sha256.Sum256([]byte(strings.Join([]string{"reminder-notification", reminder.WorkspaceID, userID, activityKey}, "\x00")))
			projections = append(projections, productNotificationProjection{
				ID: hex.EncodeToString(digest[:16]), EventID: reminder.ID,
				WorkspaceID: reminder.WorkspaceID, UserID: userID, ActivityKey: activityKey,
				Kind: "reminder", PageID: reminder.PageID, BlockID: reminder.BlockID,
				ActorID: reminder.CreatorID, Title: title, Preview: reminder.Preview, PreviewSpans: previewSpans,
				Target:     target,
				OccurredAt: reminder.DueAt, Personal: true,
			})
		}
		if _, err := upsertProductNotifications(ctx, txApp, projections, now.UnixMilli()); err != nil {
			return err
		}
		return settle()
	})
}

func notifyReminderRunnerAfterCommit(app core.App) {
	notify := func() {
		runner, ok := app.Store().Get(reminderRunnerStoreKey).(*reminderRunner)
		if ok {
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

// reminderPreviewSpans is a reminder's line as it stands, so the inbox draws
// its date as the page does now, not as it was written, as Notion's (seen
// 2026-11-05). A database date has no line of its own, and a read that fails
// leaves the stored words.
func reminderPreviewSpans(app core.App, pageID, blockID string, databaseDate bool) string {
	if databaseDate || blockID == "" {
		return ""
	}
	return encodeNotificationLineSpans(blockLineSpans(app, pageID, blockID))
}

// blockLineSpans is a block's line as it stands, none when it can't be read.
func blockLineSpans(app core.App, pageID, blockID string) []graphTextSpan {
	doc, found, err := loadYgoDoc(app, pageID)
	if err != nil || !found {
		return nil
	}
	block := storedReminderBlock{ID: blockID}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[last_edited_by]],
			CASE WHEN instr([[content]], '"mention"') > 0 OR instr([[content]], '"link"') > 0
				THEN [[content]] ELSE '' END AS [[mention_content]]
		FROM {{leafen_blocks}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": blockID}).One(&block); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return graphTextSpansFromDelta(textDeltaAsItStands(blockMentionText(doc, block).ToDelta(), false))
}

// encodeNotificationLineSpans keeps a line to a preview's length, a mention
// whole or not at all, as a notification stores it.
func encodeNotificationLineSpans(spans []graphTextSpan) string {
	left := notificationEmailPreviewRunes
	kept := make([]graphTextSpan, 0, len(spans))
	for _, span := range spans {
		runes := []rune(span.Text)
		if len(runes) > left {
			if span.Mention != "" {
				break
			}
			span.Text = string(runes[:left])
			runes = runes[:left]
		}
		kept = append(kept, span)
		if left -= len(runes); left <= 0 {
			break
		}
	}
	if len(kept) == 0 {
		return ""
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return ""
	}
	return string(encoded)
}
