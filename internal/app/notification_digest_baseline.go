package app

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// notificationDigestBaseline is how far scheduling new people has come: for
// each digest, the last account its walk over the accounts in id order
// scheduled and whether the walk reached the end, and the accounts created
// since. A runner keeps one across its turns, so after the walk from its
// start a turn reads only the new accounts (2026-11-08: each turn read every
// account).
type notificationDigestBaseline struct {
	mu      sync.Mutex
	after   map[string]string
	walked  map[string]bool
	created []string
}

// accountCreated notes an account for the next turn to schedule.
func (baseline *notificationDigestBaseline) accountCreated(userID string) {
	baseline.mu.Lock()
	defer baseline.mu.Unlock()
	baseline.created = append(baseline.created, userID)
}

// reset starts the walks again, as at a start, and forgets the accounts
// created since: the walks find them.
func (baseline *notificationDigestBaseline) reset() {
	baseline.mu.Lock()
	defer baseline.mu.Unlock()
	baseline.after, baseline.walked, baseline.created = nil, nil, nil
}

// baselineNotificationDigests starts the digest windows of people the
// server has not scheduled yet, the first notificationDigestBaselineLimit
// of each digest by account id.
func baselineNotificationDigests(ctx context.Context, app core.App, now time.Time) error {
	return (&notificationDigestBaseline{}).advance(ctx, app, now)
}

// advance starts the windows of up to notificationDigestBaselineLimit
// accounts created since the last turn, then goes on with each digest's walk
// over the accounts until it schedules fewer than the limit at once.
func (baseline *notificationDigestBaseline) advance(ctx context.Context, app core.App, now time.Time) error {
	baseline.mu.Lock()
	created := slices.Clone(baseline.created[:min(len(baseline.created), notificationDigestBaselineLimit)])
	baseline.mu.Unlock()
	encoded, err := json.Marshal(created)
	if err != nil {
		return err
	}
	for _, kind := range []string{notificationDigestPageUpdates, notificationDigestWorkspace} {
		params := dbx.Params{
			"kind": kind, "now": now.UnixMilli(), "due": now.Add(notificationDigestPeriod(kind)).UnixMilli(),
			"limit": notificationDigestBaselineLimit, "created": string(encoded),
		}
		unscheduled := `NOT EXISTS (
				SELECT 1 FROM {{leafen_notification_digests}} AS [[digest]]
				WHERE [[digest]].[[user_id]] = [[user]].[[id]] AND [[digest]].[[kind]] = {:kind}
			)`
		if len(created) > 0 {
			if _, err := app.DB().NewQuery(`
				INSERT INTO {{leafen_notification_digests}} ([[user_id]], [[kind]], [[last_sent_at]], [[next_due_at]])
				SELECT [[user]].[[id]], {:kind}, {:now}, {:due} FROM {{users}} AS [[user]]
				WHERE [[user]].[[id]] IN (SELECT [[value]] FROM json_each({:created})) AND ` + unscheduled + `
			`).WithContext(ctx).Bind(params).Execute(); err != nil {
				return err
			}
		}
		baseline.mu.Lock()
		after, walked := baseline.after[kind], baseline.walked[kind]
		baseline.mu.Unlock()
		if walked {
			continue
		}
		params["after"] = after
		scheduled := []string{}
		if err := app.DB().NewQuery(`
			INSERT INTO {{leafen_notification_digests}} ([[user_id]], [[kind]], [[last_sent_at]], [[next_due_at]])
			SELECT [[user]].[[id]], {:kind}, {:now}, {:due} FROM {{users}} AS [[user]]
			WHERE [[user]].[[id]] > {:after} AND ` + unscheduled + `
			ORDER BY [[user]].[[id]] LIMIT {:limit}
			RETURNING [[user_id]]
		`).WithContext(ctx).Bind(params).Column(&scheduled); err != nil {
			return err
		}
		baseline.mu.Lock()
		if baseline.after == nil {
			baseline.after, baseline.walked = map[string]string{}, map[string]bool{}
		}
		if len(scheduled) < notificationDigestBaselineLimit {
			baseline.walked[kind] = true
		} else {
			baseline.after[kind] = slices.Max(scheduled)
		}
		baseline.mu.Unlock()
	}
	baseline.mu.Lock()
	baseline.created = baseline.created[min(len(baseline.created), len(created)):]
	baseline.mu.Unlock()
	return nil
}
