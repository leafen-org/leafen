package app

import (
	"database/sql"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// activityRetentionDefaultDays is how long an organization keeps its updates
// feed and its page analytics and page views unless it chooses (the audit
// decision's 2026-10-21 22:42 addition 2); 0 days keeps them.
const activityRetentionDefaultDays = 365

const activityRetentionDay = int64(24 * time.Hour / time.Millisecond)

type organizationActivityRetention struct {
	FeedDays      int `db:"feed_days"`
	AnalyticsDays int `db:"analytics_days"`
}

// loadOrganizationActivityRetention is an organization's periods, a year of
// each where it chose none.
func loadOrganizationActivityRetention(app core.App, organizationID string) (organizationActivityRetention, error) {
	policy := organizationActivityRetention{FeedDays: activityRetentionDefaultDays, AnalyticsDays: activityRetentionDefaultDays}
	err := app.DB().NewQuery(`SELECT [[feed_days]], [[analytics_days]] FROM {{leafen_organization_activity_retention_policies}}
		WHERE [[organization_id]] = {:organization}`).Bind(dbx.Params{"organization": organizationID}).One(&policy)
	if errors.Is(err, sql.ErrNoRows) {
		return policy, nil
	}
	return policy, err
}

// workspaceActivityRetention is the periods of a workspace's organization.
func workspaceActivityRetention(app core.App, workspaceID string) (organizationActivityRetention, error) {
	organizationID := ""
	if err := app.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = {:workspace}`).
		Bind(dbx.Params{"workspace": workspaceID}).Row(&organizationID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return organizationActivityRetention{}, err
	}
	return loadOrganizationActivityRetention(app, organizationID)
}

// activityRetentionCutoff is the earliest time kept for a period: none (0)
// where it keeps everything.
func activityRetentionCutoff(days int, now time.Time) int64 {
	if days <= 0 {
		return 0
	}
	return now.UTC().UnixMilli() - int64(days)*activityRetentionDay
}

// organizationRetentionDaysSQL and workspaceRetentionDaysSQL are an
// organization's, or a workspace's organization's, period (feed_days or
// analytics_days) in a query.
func organizationRetentionDaysSQL(column, organization string) string {
	return `COALESCE((SELECT [[` + column + `]] FROM {{leafen_organization_activity_retention_policies}}
		WHERE [[organization_id]] = ` + organization + `), ` + strconv.Itoa(activityRetentionDefaultDays) + `)`
}

func workspaceRetentionDaysSQL(column, workspace string) string {
	return organizationRetentionDaysSQL(column, `(SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = `+workspace+`)`)
}

// shortestWorkspaceRetentionDays is the shortest period any workspace's
// organization keeps, what a sweep over every workspace looks back to: none
// (0) when every organization keeps all of it.
func shortestWorkspaceRetentionDays(app core.App, column string) (int, error) {
	shortest := sql.NullInt64{}
	if err := app.DB().NewQuery(`SELECT MIN([[days]]) FROM (SELECT ` + workspaceRetentionDaysSQL(column, "[[shortest_workspace]].[[id]]") + ` AS [[days]]
		FROM {{leafen_workspaces}} [[shortest_workspace]]) WHERE [[days]] > 0`).Row(&shortest); err != nil {
		return 0, err
	}
	return int(shortest.Int64), nil
}

// activityRetentionFallbackDue is when a sweep that left rows kept by a
// longer period looks again: a day on.
func activityRetentionFallbackDue(due, now time.Time) time.Time {
	if due.IsZero() || due.After(now) {
		return due
	}
	return now.Add(24 * time.Hour)
}

// mutateOrganizationActivityRetention sets one of an organization's periods
// from its settings: any number of days, or 0 to keep.
func mutateOrganizationActivityRetention(app core.App, organizationID, actorID string, input organizationSettingsMutationInput, now int64) error {
	days, err := strconv.Atoi(input.Value)
	if err != nil || days < 0 || days > math.MaxInt32 {
		return errOrganizationSettingsInvalid
	}
	column := "feed_days"
	if input.Action == organizationSettingsActionAnalyticsRetention {
		column = "analytics_days"
	}
	_, err = app.DB().NewQuery(`INSERT INTO {{leafen_organization_activity_retention_policies}}
		([[organization_id]], [[feed_days]], [[analytics_days]], [[updated_by]], [[updated_at]])
		VALUES ({:organization}, {:default}, {:default}, {:actor}, {:now})
		ON CONFLICT ([[organization_id]]) DO NOTHING`).
		Bind(dbx.Params{"organization": organizationID, "default": activityRetentionDefaultDays, "actor": actorID, "now": now}).Execute()
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_organization_activity_retention_policies}}
		SET [[` + column + `]] = {:days}, [[updated_by]] = {:actor}, [[updated_at]] = {:now} WHERE [[organization_id]] = {:organization}`).
		Bind(dbx.Params{"organization": organizationID, "days": days, "actor": actorID, "now": now}).Execute()
	if err != nil || column != "analytics_days" {
		// The feed's hourly trim takes up a changed period.
		return err
	}
	// The analytics sweep and the page views' take it up now.
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_page_analytics_retention_state}} SET [[next_due_at]] = ''
		WHERE [[id]] = 'global' AND [[status]] = {:idle}`).Bind(dbx.Params{"idle": pageAnalyticsRetentionStatusIdle}).Execute(); err != nil {
		return err
	}
	notifyPageAnalyticsRetentionAfterCommit(app)
	notifyPageInteractionRetentionAfterCommit(app)
	return nil
}
