package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Those following a database row page for its updates hear of a change by a
// fan-out: the change admits one record for each row it changed, in its own
// commit, and the notification runner tells the row's followers a slice at a
// time from where the last slice ended, after people's edits (write_lane.go).
// Every follower hears, however many there are, and the change doesn't wait
// on them. A slice is a hundred followers: a commit costs more to open and
// close than to tell twenty (2026-10-15).

const (
	databaseRowFanoutTurnLimit     = 4
	databaseRowFanoutFollowerLimit = 100
	databaseRowFanoutMaxRetryDelay = 15 * time.Minute
)

// databaseRowFollowerFanout is one changed row's news for its followers:
// the properties that changed, named to each follower who can see them.
type databaseRowFollowerFanout struct {
	ID              string `db:"id"`
	WorkspaceID     string `db:"workspace_id"`
	ContainerPageID string `db:"container_page_id"`
	RowID           string `db:"row_id"`
	DataSourceID    string `db:"data_source_id"`
	ActorID         string `db:"actor_id"`
	MutationID      string `db:"mutation_id"`
	ActivityKey     string `db:"activity_key"`
	PropertyIDsJSON string `db:"property_ids_json"`
	Cursor          string `db:"cursor"`
	Attempts        int    `db:"attempts"`
	// ActorConnection names the integration the change was written through
	// (noticeConnection), which its followers' notices name.
	ActorConnection string `db:"actor_connection"`
}

// admitDatabaseRowFollowerFanouts records a change's fan-outs in its commit.
func admitDatabaseRowFollowerFanouts(app core.App, plan databaseRowNotificationPlan, now int64) error {
	for _, fanout := range plan.Fanouts {
		digest := sha256.Sum256([]byte(strings.Join([]string{
			"database-row-fanout", plan.WorkspaceID, fanout.RowID, plan.MutationID,
		}, "\x00")))
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_row_notification_fanouts}}
				([[id]], [[workspace_id]], [[container_page_id]], [[row_id]], [[data_source_id]], [[actor_id]],
				 [[mutation_id]], [[activity_key]], [[property_ids_json]], [[created_at]], [[updated_at]], [[actor_connection]])
			VALUES ({:id}, {:workspaceId}, {:pageId}, {:rowId}, {:sourceId}, {:actorId},
				{:mutationId}, {:activityKey}, {:properties}, {:now}, {:now}, {:connection})
			ON CONFLICT ([[id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": hex.EncodeToString(digest[:16]), "workspaceId": plan.WorkspaceID, "pageId": plan.ContainerPageID,
			"rowId": fanout.RowID, "sourceId": fanout.DataSourceID, "actorId": plan.ActorID, "mutationId": plan.MutationID,
			"activityKey": fanout.ActivityKey, "properties": fanout.PropertyIDsJSON, "now": now,
			"connection": plan.Connection,
		}).Execute(); err != nil {
			return err
		}
	}
	if len(plan.Fanouts) > 0 {
		notifyCommentNotificationMaterializationAfterCommit(app)
	}
	return nil
}

// runDatabaseRowFollowerFanouts tells one slice of followers for each of the
// fan-outs due, and reports whether more are due now and when the next is.
func runDatabaseRowFollowerFanouts(ctx context.Context, app core.App, now time.Time) (more bool, nextDue int64, err error) {
	nowMillis := now.UTC().UnixMilli()
	due := []databaseRowFollowerFanout{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[workspace_id]], [[container_page_id]], [[row_id]], [[data_source_id]], [[actor_id]],
			[[mutation_id]], [[activity_key]], [[property_ids_json]], [[cursor]], [[attempts]], [[actor_connection]]
		FROM {{leafen_database_row_notification_fanouts}}
		INDEXED BY [[idx_leafen_database_row_notification_fanouts_due]]
		WHERE [[next_attempt_at]] <= {:now}
		ORDER BY [[next_attempt_at]], [[created_at]], [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": nowMillis, "limit": databaseRowFanoutTurnLimit + 1}).All(&due); err != nil {
		return false, -1, err
	}
	more = len(due) > databaseRowFanoutTurnLimit
	if more {
		due = due[:databaseRowFanoutTurnLimit]
	}
	failures := []error{}
	for _, fanout := range due {
		if err := ctx.Err(); err != nil {
			return more, -1, err
		}
		left := false
		yieldToPeople(ctx)
		err := app.RunInTransaction(func(txApp core.App) error {
			var tellErr error
			left, tellErr = tellDatabaseRowFollowers(ctx, txApp, fanout, nowMillis)
			return tellErr
		})
		more = more || left
		if err == nil {
			continue
		}
		failures = append(failures, err)
		delay := min(time.Second<<min(fanout.Attempts, 10), databaseRowFanoutMaxRetryDelay)
		if _, retryErr := app.DB().NewQuery(`
			UPDATE {{leafen_database_row_notification_fanouts}}
			SET [[attempts]] = [[attempts]] + 1, [[next_attempt_at]] = {:next},
				[[error_code]] = {:code}, [[updated_at]] = {:now}
			WHERE [[id]] = {:id}
		`).WithContext(ctx).Bind(dbx.Params{
			"id": fanout.ID, "next": nowMillis + delay.Milliseconds(), "now": nowMillis,
			"code": errorCodeCommentNotificationMaterializationFailed,
		}).Execute(); retryErr != nil {
			failures = append(failures, retryErr)
		}
	}
	var next sql.NullInt64
	if err := app.DB().NewQuery(`
		SELECT MIN([[next_attempt_at]]) FROM {{leafen_database_row_notification_fanouts}}
	`).WithContext(ctx).Row(&next); err != nil {
		failures = append(failures, err)
	}
	nextDue = -1
	if next.Valid {
		nextDue = next.Int64
	}
	return more, nextDue, errors.Join(failures...)
}

// tellDatabaseRowFollowers tells the next slice of a row's followers of its
// change, naming to each only the changed properties they can see in the row
// as it is now; the fan-out ends with its last follower. It reports whether
// followers remain.
func tellDatabaseRowFollowers(ctx context.Context, app core.App, fanout databaseRowFollowerFanout, now int64) (bool, error) {
	followers := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[user_id]] FROM {{leafen_page_notification_preferences}}
		INDEXED BY [[idx_leafen_page_notification_preferences_level]]
		WHERE [[page_id]] = {:rowId} AND [[level]] = {:level} AND [[user_id]] > {:cursor}
		ORDER BY [[user_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"rowId": fanout.RowID, "level": databaseRowNotificationAllUpdates, "cursor": fanout.Cursor,
		"limit": databaseRowFanoutFollowerLimit + 1,
	}).Column(&followers); err != nil {
		return false, err
	}
	more := len(followers) > databaseRowFanoutFollowerLimit
	if more {
		followers = followers[:databaseRowFanoutFollowerLimit]
	}
	if len(followers) > 0 {
		propertyIDs := []string{}
		if err := json.Unmarshal([]byte(fanout.PropertyIDsJSON), &propertyIDs); err != nil {
			return false, err
		}
		properties, err := loadDatabaseRowNotificationProperties(app, map[string]struct{}{fanout.DataSourceID: {}})
		if err != nil {
			return false, err
		}
		stored, _, err := loadDatabaseRowNotificationValues(app, []string{fanout.RowID})
		if err != nil {
			return false, err
		}
		visibility := newDatabaseRowNotificationVisibility(app, []string{fanout.DataSourceID})
		row := &graphDatabaseRow{ID: fanout.RowID}
		plan := databaseRowNotificationPlan{
			ActorID: fanout.ActorID, WorkspaceID: fanout.WorkspaceID,
			ContainerPageID: fanout.ContainerPageID, MutationID: fanout.MutationID,
			Connection: fanout.ActorConnection,
		}
		for _, userID := range followers {
			if userID == fanout.ActorID {
				continue
			}
			names := []string{}
			for _, propertyID := range propertyIDs {
				property, known := properties[propertyID]
				if !known {
					continue
				}
				if shown, err := visibility.sees(userID, property, row, stored[fanout.RowID]); err != nil {
					return false, err
				} else if shown {
					names = append(names, property.Name)
				}
			}
			if len(names) == 0 {
				continue
			}
			plan.Items = append(plan.Items, plannedDatabaseRowNotification{
				RowID: fanout.RowID, UserID: userID, Kind: "page_edit",
				Preview: strings.Join(names, ", "), ActivityKey: fanout.ActivityKey,
			})
		}
		if err := deliverDatabaseRowNotifications(ctx, app, plan, now); err != nil {
			return false, err
		}
	}
	if !more {
		_, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_row_notification_fanouts}} WHERE [[id]] = {:id}`).
			WithContext(ctx).Bind(dbx.Params{"id": fanout.ID}).Execute()
		return false, err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_row_notification_fanouts}}
		SET [[cursor]] = {:cursor}, [[attempts]] = 0, [[error_code]] = '', [[updated_at]] = {:now}
		WHERE [[id]] = {:id}
	`).WithContext(ctx).Bind(dbx.Params{"id": fanout.ID, "cursor": followers[len(followers)-1], "now": now}).Execute()
	return true, err
}
