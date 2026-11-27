package app

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's deletion ends by deleting its workspace row, whose foreign
// keys take every table the inventory says goes with it by a cascade. A
// table that grows with use went all at once inside that last write: the
// workspace's activity (a row for each page made) at 101,300 rows took
// 0.6–1.0 s of a 1.4–2.0 s last request, and a writer beside it waited as
// long (a query-plan follow-up, 2026-11-10). So after the residue lanes come
// the cascade lanes, one a table in the inventory's order, clearing the
// workspace's rows a window a step; the last step's cascade then finds them
// empty. In the inventory a table comes before any table it has a foreign key
// to, which the inventory check holds, so a window takes no other cascade
// lane's rows with it. The cascade still takes whatever a lane did not, so a
// deletion whose lane numbers a later inventory moved leaves at worst a
// table to its last step.

// workspaceDeletionCascadeRowsPerStep is the cascade lanes' window, the rows
// a step deletes from them.
const workspaceDeletionCascadeRowsPerStep = 256

type workspaceDeletionForeignKey struct {
	Table    string `db:"table_name"`
	Parent   string `db:"parent_name"`
	OnDelete string `db:"on_delete"`
}

var workspaceDeletionCascadeLanes = func() []workspaceDeletionResidueLane {
	lanes := []workspaceDeletionResidueLane{}
	for _, authority := range workspaceDeletionDirectAuthorities {
		if authority.Disposition == workspaceDeletionAuthorityCascade {
			lanes = append(lanes, workspaceDeletionResidueLane{
				Table: authority.Table, Key: authority.Key,
			})
		}
	}
	return lanes
}()

// workspaceDeletionLaneCount is the residue lanes and then the cascade lanes,
// as a deletion's residue lane counts them.
func workspaceDeletionLaneCount() int {
	return len(workspaceDeletionResidueLanes) + len(workspaceDeletionCascadeLanes)
}

// clearWorkspaceDeletionCascadeLanes deletes the workspace's rows from the
// cascade lanes from the given one on, a window in all, going on to the next
// lane while the window has room, and returns the lane to carry on from; so
// an empty table costs one read of its index, not a step of its own. It reads
// each table by the index led by its key that the inventory check holds, in
// that index's order, so no window sorts the workspace's rows.
func clearWorkspaceDeletionCascadeLanes(
	app core.App,
	workspaceID string,
	from int,
) (int, error) {
	room := int64(workspaceDeletionCascadeRowsPerStep)
	for index := from; index < len(workspaceDeletionCascadeLanes); index++ {
		lane := workspaceDeletionCascadeLanes[index]
		if !retiredAreaTableHeld(app, lane.Table) {
			continue
		}
		result, err := app.DB().NewQuery(fmt.Sprintf(`
			DELETE FROM {{%s}} WHERE [[rowid]] IN (
				SELECT [[rowid]] FROM {{%s}} WHERE [[%s]] = {:workspaceId} LIMIT {:limit}
			)
		`, lane.Table, lane.Table, lane.Key)).Bind(dbx.Params{
			"workspaceId": workspaceID, "limit": room,
		}).Execute()
		if err != nil {
			return index, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return index, err
		}
		if room -= rows; room <= 0 {
			return index, nil
		}
	}
	return len(workspaceDeletionCascadeLanes), nil
}

// validateWorkspaceDeletionCascadeLaneOrder refuses an inventory where a
// cascade lane's table comes after a table it has a foreign key to: that
// table's windows would take its rows with them, as many as there are.
func validateWorkspaceDeletionCascadeLaneOrder(references []workspaceDeletionForeignKey) error {
	position := make(map[string]int, len(workspaceDeletionCascadeLanes))
	for index, lane := range workspaceDeletionCascadeLanes {
		position[lane.Table] = index
	}
	for _, reference := range references {
		child, isLane := position[reference.Table]
		parent, parentIsLane := position[reference.Parent]
		if isLane && parentIsLane && child > parent {
			return fmt.Errorf("%w: %s is cleared after %s, which it has a foreign key to",
				errWorkspaceDeleteUnavailable, reference.Table, reference.Parent)
		}
	}
	return nil
}
