package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// trimWorkspaceActivity lets go of each workspace's groups past its
// organization's period, a bounded batch at a time; an organization keeping
// its feed keeps all of it, and a legal hold what it covers.
func trimWorkspaceActivity(app core.App, now time.Time) error {
	workspaces := []struct {
		ID   string `db:"id"`
		Days int    `db:"days"`
		Held bool   `db:"held"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[trim_workspace]].[[id]], ` + organizationRetentionDaysSQL("feed_days", "[[trim_workspace]].[[organization]]") + ` AS [[days]],
		` + legalHoldKeepsSQL(legalHoldRecordSQL{Organization: "[[trim_workspace]].[[organization]]", Workspace: "[[trim_workspace]].[[id]]"}) + ` AS [[held]]
		FROM {{leafen_workspaces}} [[trim_workspace]]`).All(&workspaces); err != nil {
		return err
	}
	held := legalHoldKeepsSQL(legalHoldRecordSQL{Organization: "[[trimmed]].[[organization_id]]", Workspace: "[[trimmed]].[[workspace_id]]",
		Pages: []string{"[[trimmed]].[[page_id]]"}, People: []string{"[[trimmed]].[[actor_id]]"}})
	for _, workspace := range workspaces {
		if workspace.Days <= 0 || workspace.Held {
			continue
		}
		for {
			result, err := app.DB().NewQuery(`
				DELETE FROM {{leafen_workspace_activity}} WHERE rowid IN (
					SELECT [[trimmed]].rowid FROM {{leafen_workspace_activity}} [[trimmed]] INDEXED BY [[idx_leafen_workspace_activity_feed]]
					WHERE [[trimmed]].[[workspace_id]] = {:workspace} AND [[trimmed]].[[last_at]] < {:cutoff} AND NOT ` + held + ` LIMIT 2000)
			`).Bind(dbx.Params{"workspace": workspace.ID, "cutoff": activityRetentionCutoff(workspace.Days, now)}).Execute()
			if err != nil {
				return err
			}
			deleted, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if deleted < 2000 {
				break
			}
		}
	}
	return nil
}
