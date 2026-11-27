package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Password sign-in slows down per account (user decision 2026-10-22, on the
// measuring session's advice): five wrong passwords to one account within
// fifteen minutes, from any address, and its next password sign-in waits a
// minute, answered at once without hashing; each wrong one after a wait
// doubles it — 2, 4, 8 minutes — up to 15. It never locks: an attacker
// can't keep the owner out for longer than the cap by guessing, and the
// owner can still come in by a signed-in device's code, a passkey or SSO,
// which never wait. At the cap a guesser gets four passwords an hour. The
// right password starts the count over, and so does a quiet quarter hour.

const (
	passwordBackoffWindow    = 15 * time.Minute
	passwordBackoffThreshold = 5
	passwordBackoffFirst     = time.Minute
	passwordBackoffCap       = 15 * time.Minute
)

type storedPasswordBackoff struct {
	Failures    int   `db:"failures"`
	WindowStart int64 `db:"window_start"`
	Level       int   `db:"level"`
	WaitUntil   int64 `db:"wait_until"`
	LastFailure int64 `db:"last_failure"`
}

func loadPasswordBackoff(app core.App, userID string) (storedPasswordBackoff, error) {
	var stored storedPasswordBackoff
	err := app.DB().NewQuery(`
		SELECT [[failures]], [[window_start]], [[level]], [[wait_until]], [[last_failure]]
		FROM {{leafen_password_backoff}} WHERE [[user_id]] = {:userId}
	`).Bind(dbx.Params{"userId": userID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return storedPasswordBackoff{}, nil
	}
	return stored, err
}

// passwordSignInWait is how long an account's password sign-in waits now;
// zero when it doesn't. One read, no hash.
func passwordSignInWait(app core.App, userID string, now time.Time) (time.Duration, error) {
	stored, err := loadPasswordBackoff(app, userID)
	if err != nil {
		return 0, err
	}
	if wait := time.UnixMilli(stored.WaitUntil).Sub(now); wait > 0 {
		return wait, nil
	}
	return 0, nil
}

// passwordBackoffFor is the wait the backoff's level gives.
func passwordBackoffFor(level int) time.Duration {
	wait := passwordBackoffFirst
	for step := 1; step < level && wait < passwordBackoffCap; step++ {
		wait *= 2
	}
	return min(wait, passwordBackoffCap)
}

// notePasswordFailure counts a wrong password to an account and starts or
// lengthens its wait.
func notePasswordFailure(app core.App, userID string, now time.Time) error {
	return app.RunInTransaction(func(txApp core.App) error {
		stored, err := loadPasswordBackoff(txApp, userID)
		if err != nil {
			return err
		}
		at := now.UnixMilli()
		// A quiet quarter hour past any wait starts over.
		if stored.LastFailure > 0 && at-stored.LastFailure > passwordBackoffWindow.Milliseconds() && at >= stored.WaitUntil {
			stored = storedPasswordBackoff{}
		}
		if stored.Level > 0 {
			// Waited before: each wrong password after it doubles the wait.
			stored.Level++
			stored.WaitUntil = at + passwordBackoffFor(stored.Level).Milliseconds()
		} else {
			if stored.WindowStart == 0 || at-stored.WindowStart > passwordBackoffWindow.Milliseconds() {
				stored.Failures, stored.WindowStart = 0, at
			}
			stored.Failures++
			if stored.Failures >= passwordBackoffThreshold {
				stored.Level, stored.Failures = 1, 0
				stored.WaitUntil = at + passwordBackoffFor(1).Milliseconds()
			}
		}
		stored.LastFailure = at
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_password_backoff}}
				([[user_id]], [[failures]], [[window_start]], [[level]], [[wait_until]], [[last_failure]])
			VALUES ({:userId}, {:failures}, {:windowStart}, {:level}, {:waitUntil}, {:lastFailure})
			ON CONFLICT ([[user_id]]) DO UPDATE SET [[failures]] = excluded.[[failures]],
				[[window_start]] = excluded.[[window_start]], [[level]] = excluded.[[level]],
				[[wait_until]] = excluded.[[wait_until]], [[last_failure]] = excluded.[[last_failure]]
		`).Bind(dbx.Params{"userId": userID, "failures": stored.Failures, "windowStart": stored.WindowStart,
			"level": stored.Level, "waitUntil": stored.WaitUntil, "lastFailure": stored.LastFailure}).Execute()
		return err
	})
}

// clearPasswordBackoff starts an account's count over once it signs in.
func clearPasswordBackoff(app core.App, userID string) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_password_backoff}} WHERE [[user_id]] = {:userId}`).
		Bind(dbx.Params{"userId": userID}).Execute()
	return err
}
