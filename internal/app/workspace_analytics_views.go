package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// recordWorkspaceAnalyticsMemberViews counts, for the day, the page,
// database page and database opened alone views of a member whose views are
// identified, on the day of the member's clock now is on
// (page_analytics_clock.go); views a member keeps out of analytics, on a
// page, a row or a database, are not counted.
func recordWorkspaceAnalyticsMemberViews(
	app core.App,
	workspaceID string,
	principalID string,
	observations []pageAnalyticsObservation,
	identifiedPages map[string]struct{},
	identifiedRows map[string]struct{},
	identifiedDatabases map[string]struct{},
	now time.Time,
) error {
	views := 0
	for _, observation := range observations {
		_, identified := identifiedPages[observation.PageID]
		if observation.RowID != "" {
			_, identified = identifiedRows[observation.RowID]
		}
		if observation.ContainerID != "" {
			_, identified = identifiedDatabases[observation.ContainerID]
		}
		if identified {
			views++
		}
	}
	if views == 0 {
		return nil
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_analytics_member_daily}}
			([[workspace_id]], [[user_id]], [[day]], [[views]], [[last_viewed_at]], [[retention_at]])
		VALUES ({:workspaceId}, {:userId}, {:day}, {:views}, {:viewedAt}, {:retentionAt})
		ON CONFLICT ([[workspace_id]], [[user_id]], [[day]]) DO UPDATE
		SET [[views]] = [[views]] + excluded.[[views]], [[last_viewed_at]] = excluded.[[last_viewed_at]]
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "userId": principalID, "day": pageAnalyticsDayOf(now),
		"views": views, "viewedAt": pageAnalyticsTimestamp(now),
		"retentionAt": now.UTC().AddDate(0, 0, maxPageAnalyticsReadDays).Format(pageAnalyticsTimeLayout),
	}).Execute()
	return err
}
