package app

import (
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A migration's run after one of its runs finished — a run again, which
// reads Notion anew and brings in only what changed there since — makes
// nothing missing: what the runs before moved stays in Leafen as it was,
// whole as of the last run that finished, and only what Notion changed since
// (new, changed or removed) changes as the run goes (2026-11-23: an agent
// read a moved database during its run again and was told its rows "may be
// missing", "0 of 7189 made", though the move had finished and been told
// hours before). A first move — no run of the migration has finished, a
// stopped first run gone on with included — makes what it makes for the
// first time, and so does a run again for a database new in Notion since.
// This is that judgement, for every place that says how a running import
// stands to what it makes: a read's aside and refusal (notion_import_filling.go),
// the run's progress on the import screen and in the agent's migration
// status (notionImportProgressOf, notionImportProgressText).

// notionImportMovedBefore is what the earlier runs of a running import's
// migration moved, for a run again; zero for a first move.
type notionImportMovedBefore struct {
	// When the last run that finished ended — or began, when a run before
	// runs kept their end didn't keep it (Ended false); 0 for a first move.
	AsOf  int64
	Ended bool
	// What was made before this is whole as of AsOf: when that run ended,
	// or this run's start when its end isn't known.
	Before int64
}

// notionImportMovedBeforeOf is what the runs before a running import moved:
// a migration's run when one of its runs finished, by when the last finished
// one began as the migration and the run's payload keep it
// (notionMigrationRunAgainOf), and when it ended as its report keeps it.
func notionImportMovedBeforeOf(app core.App, job storedJob, payload notionImportJobPayload) notionImportMovedBefore {
	if payload.MigrationID == "" {
		return notionImportMovedBefore{}
	}
	var stored struct {
		Began int64 `db:"began"`
		Ended int64 `db:"ended"`
	}
	if err := app.DB().NewQuery(`
		SELECT [[last_done_run_started_at]] AS [[began]],
			COALESCE(json_extract([[run_report_json]], '$.endedAt'), 0) AS [[ended]]
		FROM {{leafen_notion_migrations}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": payload.MigrationID}).One(&stored); err != nil {
		return notionImportMovedBefore{}
	}
	began := stored.Began
	if began <= 0 {
		// A migration done before it kept when its last done run began: the
		// run's payload has it from the migration's run state.
		began = max(payload.MigrationStampedBefore, 0)
	}
	if began <= 0 {
		return notionImportMovedBefore{}
	}
	if stored.Ended > began {
		return notionImportMovedBefore{AsOf: stored.Ended, Ended: true, Before: stored.Ended}
	}
	return notionImportMovedBefore{AsOf: began, Before: job.Created}
}

// again says the import is a migration's run again.
func (moved notionImportMovedBefore) again() bool {
	return moved.AsOf > 0
}

// whole says a data source made at created came whole in the runs before.
func (moved notionImportMovedBefore) whole(created int64) bool {
	return moved.again() && created > 0 && created < moved.Before
}

// asOfText says, in words for an agent, the run its data is whole as of.
func (moved notionImportMovedBefore) asOfText() string {
	at := time.UnixMilli(moved.AsOf).UTC().Format(time.RFC3339)
	if moved.Ended {
		return "the move that finished at " + at
	}
	return "the last move that finished, begun at " + at
}

// notionImportFillingAgainNotice says of a data source the runs before
// moved whole that its rows are all there as of then, and that the run going
// on brings in only what Notion changed since: no "may be missing", and none
// of a first move's made counts.
func notionImportFillingAgainNotice(app core.App, dataSourceID string, job notionImportFillingJob) string {
	return fmt.Sprintf(
		"%s came over whole from Notion in %s: its rows are all here as Notion had them then. "+
			"This migration is running again now (%s) to import what changed in Notion since, so only rows new, changed or removed there will change; "+
			"the rest stay as they are; %s.",
		mcpDataSourceURL(app, dataSourceID), job.Moved.asOfText(), job.Stage, job.Follow,
	)
}
