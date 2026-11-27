package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A deadline someone must not miss (deadline_values.go) gives a notice for
// each of its days. Each notice goes to the item's people and to whoever
// marked the deadline, as Leafen's own notice in the inbox and by email whenever the server sends email. It comes
// again to those who haven't acknowledged it until a closer notice takes
// over or the deadline passes, and a notice that fell due while the server
// was down goes as soon as it is back — only the closest of a deadline's.

const (
	deadlineActivityPrefix = "deadline:"
	deadlineDeliveryLimit  = 32
)

type storedDeadlineNotice struct {
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
	RowID       string `db:"row_id"`
	PropertyID  string `db:"property_id"`
	DeadlineKey string `db:"deadline_key"`
	Days        int    `db:"days"`
	TimeZone    string `db:"time_zone"`
	DeadlineAt  int64  `db:"deadline_at"`
	DueAt       int64  `db:"due_at"`
	NextAt      int64  `db:"next_at"`
	Status      string `db:"status"`
	SetterID    string `db:"setter_id"`
	Rounds      int    `db:"rounds"`
}

const storedDeadlineNoticeColumns = `[[id]], [[workspace_id]], [[page_id]], [[row_id]], [[property_id]], [[deadline_key]], [[days]],
	[[time_zone]], [[deadline_at]], [[due_at]], [[next_at]], [[status]], [[setter_id]], [[rounds]]`

// A deadline's key names its item, its property, its date and its clock;
// its notices are one per day before it.
func deadlineKey(rowID, propertyID, text, zone string) string {
	return rowID + ":" + propertyID + "|" + text + "|" + zone
}

func deadlineKeyText(key string) string {
	parts := strings.Split(key, "|")
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

// reconcileDatabaseDeadlines keeps the notices of the deadlines a mutation
// wrote: a deadline that stays keeps its notices and who acknowledged them,
// a day added later than its notice's time is past, and a new deadline's
// notices that are already due go at once, the closest of them.
func reconcileDatabaseDeadlines(
	ctx context.Context, app core.App,
	workspaceID, pageID, actorID string,
	operations []graphOperation,
	now time.Time,
) error {
	inserted := false
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		rowID := operation.Row.ID
		for propertyID, value := range operation.Row.Values {
			if value.Type != graphValueTypeDate {
				continue
			}
			date := map[string]any{}
			_ = json.Unmarshal(value.Value, &date)
			existing := []storedDeadlineNotice{}
			if err := app.DB().NewQuery(`SELECT ` + storedDeadlineNoticeColumns + ` FROM {{leafen_deadline_notices}}
				WHERE [[page_id]] = {:pageId} AND [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}`).
				WithContext(ctx).Bind(dbx.Params{"pageId": pageID, "rowId": rowID, "propertyId": propertyID}).All(&existing); err != nil {
				return err
			}
			marked, _ := date["deadline"].(string)
			_, days, valid := canonicalDeadlineDays(marked)
			text := deadlineText(date)
			zone, _ := date["timeZone"].(string)
			if zone == "" {
				// A date without a clock of its own, as one moved through
				// MCP, keeps the one its deadline was set on.
				for _, notice := range existing {
					if notice.TimeZone != "" {
						zone = notice.TimeZone
						break
					}
				}
				if zone == "" {
					zone = loadAccountTimeZone(app, actorID)
				}
			}
			location := deadlineLocation(zone)
			deadline, day, known := deadlineMoment(text, location)
			key := deadlineKey(rowID, propertyID, text, zone)
			wanted := map[int]bool{}
			if valid && known && text != "" {
				for _, before := range days {
					wanted[before] = true
				}
			}
			kept := map[int]bool{}
			for _, notice := range existing {
				if notice.DeadlineKey == key && wanted[notice.Days] {
					kept[notice.Days] = true
					continue
				}
				if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_deadline_notices}} WHERE [[id]] = {:id}`).
					WithContext(ctx).Bind(dbx.Params{"id": notice.ID}).Execute(); err != nil {
					return err
				}
				// What it said no longer holds: its notifications go to the archive.
				if _, err := app.DB().NewQuery(`
					UPDATE {{leafen_notifications}} SET [[archived_at]] = {:now}, [[updated_at]] = {:now}
					WHERE [[page_id]] = {:pageId} AND [[archived_at]] = 0 AND substr([[activity_key]], 1, length({:prefix})) = {:prefix}
				`).WithContext(ctx).Bind(dbx.Params{"now": now.UnixMilli(), "pageId": pageID, "prefix": deadlineActivityPrefix + notice.ID + ":"}).
					Execute(); err != nil {
					return err
				}
			}
			fresh := len(kept) == 0
			for before := range wanted {
				if kept[before] {
					continue
				}
				due := deadlineNoticeDue(deadline, day, before)
				status := "pending"
				if !deadline.After(now) || !fresh && !due.After(now) {
					status = "past"
				}
				digest := sha256.Sum256([]byte(strings.Join([]string{
					"deadline-notice", pageID, key, strconv.Itoa(before), strconv.FormatInt(now.UnixNano(), 10),
				}, "\x00")))
				if _, err := app.DB().NewQuery(`
					INSERT INTO {{leafen_deadline_notices}} (
						[[id]], [[workspace_id]], [[page_id]], [[row_id]], [[property_id]], [[deadline_key]], [[days]],
						[[time_zone]], [[deadline_at]], [[due_at]], [[next_at]], [[status]], [[setter_id]],
						[[created_at]], [[updated_at]]
					) VALUES (
						{:id}, {:workspaceId}, {:pageId}, {:rowId}, {:propertyId}, {:key}, {:days},
						{:zone}, {:deadlineAt}, {:dueAt}, {:dueAt}, {:status}, {:setterId}, {:now}, {:now}
					)
				`).WithContext(ctx).Bind(dbx.Params{
					"id": hex.EncodeToString(digest[:16]), "workspaceId": workspaceID, "pageId": pageID, "rowId": rowID,
					"propertyId": propertyID, "key": key, "days": before, "zone": zone,
					"deadlineAt": max(deadline.UnixMilli(), 0), "dueAt": max(due.UnixMilli(), 0), "status": status,
					"setterId": actorID, "now": now.UnixMilli(),
				}).Execute(); err != nil {
					return err
				}
				inserted = inserted || status == "pending"
			}
		}
	}
	if inserted {
		notifyReminderRunnerAfterCommit(app)
	}
	return nil
}

// deliverDueDeadlineNotices sends the notices that are due, saying whether
// more wait.
func deliverDueDeadlineNotices(ctx context.Context, app core.App, now time.Time) (bool, error) {
	due := []storedDeadlineNotice{}
	if err := app.DB().NewQuery(`
		SELECT ` + storedDeadlineNoticeColumns + ` FROM {{leafen_deadline_notices}}
		INDEXED BY [[idx_leafen_deadline_notices_next]]
		WHERE [[status]] IN ('pending', 'active') AND [[next_at]] <= {:now}
		ORDER BY [[next_at]], [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": now.UnixMilli(), "limit": deadlineDeliveryLimit + 1}).All(&due); err != nil {
		return false, err
	}
	more := len(due) > deadlineDeliveryLimit
	if more {
		due = due[:deadlineDeliveryLimit]
	}
	failures := []error{}
	for _, notice := range due {
		if err := deliverDeadlineNotice(ctx, app, notice, now); err != nil {
			failures = append(failures, err)
		}
	}
	return more, errors.Join(failures...)
}

func nextDeadlineNoticeAt(ctx context.Context, app core.App) (int64, error) {
	var next int64 = -1
	err := app.DB().NewQuery(`
		SELECT COALESCE(MIN([[next_at]]), -1) FROM {{leafen_deadline_notices}}
		INDEXED BY [[idx_leafen_deadline_notices_next]] WHERE [[status]] IN ('pending', 'active')
	`).WithContext(ctx).Row(&next)
	return next, err
}

// deadlineRecipients is who a deadline's notice is for: the people the item
// names and whoever marked the deadline.
func deadlineRecipients(app core.App, notice storedDeadlineNotice) ([]string, error) {
	values := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[value]] FROM {{leafen_database_row_values}}
		WHERE [[row_id]] = {:rowId} AND [[type]] = 'person' AND json_valid([[value]])
	`).Bind(dbx.Params{"rowId": notice.RowID}).Column(&values); err != nil {
		return nil, err
	}
	seen := map[string]bool{notice.SetterID: notice.SetterID != ""}
	for _, value := range values {
		for _, id := range graphRelationRowIDs(json.RawMessage(value)) {
			if id != "" {
				seen[id] = true
			}
		}
	}
	recipients := make([]string, 0, len(seen))
	for id, wanted := range seen {
		if wanted {
			recipients = append(recipients, id)
		}
	}
	sort.Strings(recipients)
	return recipients, nil
}

// deadlineRecipientCanSee is whether someone can still open the item a
// notice is about, out of trash.
func deadlineRecipientCanSee(app core.App, userID string, notice storedDeadlineNotice) (bool, error) {
	err := authorizeNotificationSubject(app, userID, notice.PageID, databaseRowPageTarget(notice.RowID))
	if isPermissionDenied(err) {
		return false, nil
	}
	return err == nil, err
}

func deliverDeadlineNotice(ctx context.Context, app core.App, notice storedDeadlineNotice, now time.Time) error {
	return app.RunInTransaction(func(txApp core.App) error {
		settle := func(status string, next int64, rounds int, sentAt int64) error {
			_, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_deadline_notices}} SET [[status]] = {:status}, [[next_at]] = {:next}, [[rounds]] = {:rounds},
					[[last_sent_at]] = MAX([[last_sent_at]], {:sentAt}), [[updated_at]] = MAX([[created_at]], {:now})
				WHERE [[id]] = {:id} AND [[status]] IN ('pending', 'active')
			`).WithContext(ctx).Bind(dbx.Params{
				"id": notice.ID, "status": status, "next": max(next, 0), "rounds": rounds, "sentAt": sentAt, "now": now.UnixMilli(),
			}).Execute()
			return err
		}
		// A closer notice of the same deadline that is due goes instead.
		var closer int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_deadline_notices}}
			WHERE [[page_id]] = {:pageId} AND [[deadline_key]] = {:key} AND [[days]] < {:days}
				AND [[due_at]] <= {:now} AND [[status]] IN ('pending', 'active')
		`).WithContext(ctx).Bind(dbx.Params{"pageId": notice.PageID, "key": notice.DeadlineKey, "days": notice.Days, "now": now.UnixMilli()}).
			Row(&closer); err != nil {
			return err
		}
		if closer > 0 {
			return settle("done", notice.NextAt, notice.Rounds, 0)
		}
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_deadline_notices}} SET [[status]] = 'done', [[updated_at]] = MAX([[created_at]], {:now})
			WHERE [[page_id]] = {:pageId} AND [[deadline_key]] = {:key} AND [[days]] > {:days} AND [[status]] IN ('pending', 'active')
		`).WithContext(ctx).Bind(dbx.Params{"pageId": notice.PageID, "key": notice.DeadlineKey, "days": notice.Days, "now": now.UnixMilli()}).
			Execute(); err != nil {
			return err
		}
		page, err := txApp.FindRecordById(pagesCollection, notice.PageID)
		if errors.Is(err, sql.ErrNoRows) {
			return settle("done", notice.NextAt, notice.Rounds, 0)
		}
		if err != nil {
			return err
		}
		// A page in the trash, or under a page there, tells no one of its
		// deadline (loadPageTrashEffects), as it reminds no one.
		if trashed, err := pageInTrashPath(txApp, page.Id); err != nil {
			return err
		} else if trashed {
			return settle("done", notice.NextAt, notice.Rounds, 0)
		}
		recipients, err := deadlineRecipients(txApp, notice)
		if err != nil {
			return err
		}
		acknowledged := []string{}
		if err := txApp.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_deadline_acknowledgements}} WHERE [[notice_id]] = {:id}`).
			WithContext(ctx).Bind(dbx.Params{"id": notice.ID}).Column(&acknowledged); err != nil {
			return err
		}
		sort.Strings(acknowledged)
		// An untitled item stays so, named in each reader's language where shown.
		title := databaseRowPageTitle(txApp, notice.RowID)
		location := deadlineLocation(notice.TimeZone)
		deadline := time.UnixMilli(notice.DeadlineAt)
		round := notice.Rounds + 1
		activityKey := deadlineActivityPrefix + notice.ID + ":" + strconv.Itoa(round)
		projections := []productNotificationProjection{}
		for _, userID := range recipients {
			if sortedContains(acknowledged, userID) {
				continue
			}
			visible, err := deadlineRecipientCanSee(txApp, userID, notice)
			if err != nil {
				return err
			}
			if !visible {
				continue
			}
			language, err := serverTextLanguage(txApp, userID, nil)
			if err != nil {
				return err
			}
			digest := sha256.Sum256([]byte(strings.Join([]string{"deadline-notification", notice.WorkspaceID, userID, activityKey}, "\x00")))
			projections = append(projections, productNotificationProjection{
				ID: hex.EncodeToString(digest[:16]), EventID: notice.ID,
				WorkspaceID: notice.WorkspaceID, UserID: userID, ActivityKey: activityKey,
				Kind: "system", PageID: notice.PageID, BlockID: notice.RowID, Title: title,
				Preview:    deadlinePreview(language, deadlineKeyText(notice.DeadlineKey), deadline, location, now),
				Target:     databaseRowPageTarget(notice.RowID),
				OccurredAt: now.UnixMilli(), Personal: true,
			})
		}
		if len(projections) == 0 {
			return settle("done", notice.NextAt, notice.Rounds, 0)
		}
		// The round before goes to the archive; the inbox keeps the latest.
		receiving := make([]string, len(projections))
		for index, projection := range projections {
			receiving[index] = projection.UserID
		}
		encoded, _ := json.Marshal(receiving)
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notifications}} SET [[archived_at]] = {:now}, [[updated_at]] = {:now}
			WHERE [[workspace_id]] = {:workspaceId} AND [[archived_at]] = 0
				AND substr([[activity_key]], 1, length({:prefix})) = {:prefix}
				AND [[user_id]] IN (SELECT [[value]] FROM json_each({:users}))
		`).WithContext(ctx).Bind(dbx.Params{
			"now": now.UnixMilli(), "workspaceId": notice.WorkspaceID, "prefix": deadlineActivityPrefix + notice.ID + ":",
			"users": string(encoded),
		}).Execute(); err != nil {
			return err
		}
		if _, err := upsertProductNotifications(ctx, txApp, projections, now.UnixMilli()); err != nil {
			return err
		}
		next, again := deadlineNextRound(deadline, location, notice.Days, now)
		if !again {
			return settle("done", notice.NextAt, round, now.UnixMilli())
		}
		return settle("active", next.UnixMilli(), round, now.UnixMilli())
	})
}

// deadlinePreview says how far off a deadline is, in the reader's language.
func deadlinePreview(language, text string, deadline time.Time, location *time.Location, now time.Time) string {
	moment := deadline
	if !dateMentionHasTime(text) {
		moment = deadline.AddDate(0, 0, -1)
	}
	local := moment.In(location)
	today := now.In(location)
	left := int(time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC).
		Sub(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)).Hours() / 24)
	if language == accountLanguageKo {
		date := local.Format("2006년 1월 2일")
		if dateMentionHasTime(text) {
			date += " " + map[bool]string{true: "오전", false: "오후"}[local.Hour() < 12] + " " + strconv.Itoa((local.Hour()+11)%12+1) + local.Format(":04")
		}
		switch {
		case !now.Before(deadline):
			return "기한이 지남 · " + date
		case left <= 0:
			return "오늘이 기한 · " + date
		default:
			return "기한까지 " + strconv.Itoa(left) + "일 · " + date
		}
	}
	date := local.Format("January 2, 2006")
	if dateMentionHasTime(text) {
		date += " " + local.Format("3:04 PM")
	}
	switch {
	case !now.Before(deadline):
		return "The deadline has passed · " + date
	case left <= 0:
		return "The deadline is today · " + date
	case left == 1:
		return "1 day to the deadline · " + date
	default:
		return strconv.Itoa(left) + " days to the deadline · " + date
	}
}

// deadlineNoticeOfActivity is the notice a deadline notification is about.
func deadlineNoticeOfActivity(activityKey string) string {
	if !strings.HasPrefix(activityKey, deadlineActivityPrefix) {
		return ""
	}
	id, _, _ := strings.Cut(strings.TrimPrefix(activityKey, deadlineActivityPrefix), ":")
	return id
}
