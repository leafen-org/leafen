package app

import (
	"context"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notificationFanoutRetention             = 7 * 24 * time.Hour
	notificationFanoutRetentionPruneBatch   = 20
	notificationFanoutRetentionFailureDelay = time.Second

	errorCodeNotificationFanoutRetentionFailed = "LEAFEN_NOTIFICATION_FANOUT_RETENTION_FAILED"
)

func pruneCompletedNotificationFanoutEvents(
	ctx context.Context,
	app core.App,
	now time.Time,
) (int64, error) {
	params := dbx.Params{
		"cutoff": now.Add(-notificationFanoutRetention).UnixMilli(),
		"limit":  notificationFanoutRetentionPruneBatch,
	}
	var pending int
	if err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_notification_events}}
			INDEXED BY [[idx_leafen_notification_events_completed_retention]]
			WHERE [[status]]='complete' AND [[completed_at]] <= {:cutoff} LIMIT 1
		)
	`).WithContext(ctx).Bind(params).Row(&pending); err != nil || pending == 0 {
		return 0, err
	}
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notification_events}}
		WHERE [[id]] IN (
			SELECT [[id]] FROM {{leafen_notification_events}}
			INDEXED BY [[idx_leafen_notification_events_completed_retention]]
			WHERE [[status]]='complete' AND [[completed_at]] <= {:cutoff}
			ORDER BY [[completed_at]], [[id]] LIMIT {:limit}
		)
	`).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func logNotificationFanoutRetentionFailure(app core.App, err error) {
	logRunnerTurnError(app, errorCodeNotificationFanoutRetentionFailed, "notification.fanout_retention", err)
}
