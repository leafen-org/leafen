package app

import (
	"context"
	"log/slog"

	"github.com/pocketbase/pocketbase/core"
)

// The data database keeps no planner statistics, so SQLite picks the same
// plan for a query on every server — a new one, one that has run for months,
// the desktop app and a copy made to measure — and the query's own shape
// decides it (CROSS JOIN for the order, + for a column read after the row is
// found). PocketBase writes statistics with PRAGMA optimize, which analyzes
// each table its write connection has used since it opened: nightly, and
// after it saves a collection, which some migrations do (measured 2026-11-18:
// a start that brought data up one schema version wrote statistics for 98
// tables, leafen_pages among them; a start with nothing to migrate wrote
// none). Those statistics come from a sample (PRAGMA optimize limits its
// analysis), told SQLite a workspace holds some hundred of its 20,000 pages,
// and changed the plans the query shapes were measured with. So the nightly
// job keeps only its checkpoints, and statistics written meanwhile — by a
// migration, an older build's night, or a copy of data from elsewhere — are
// dropped once the data is up to date, as the app opens it. Dropping the
// tables changes the schema, so every open connection reads it again without
// them and its kept statements compile again.

// pocketBaseNightlyOptimizeJob is PocketBase's own nightly job
// (core.BaseApp.registerBaseHooks), replaced under the same name.
const pocketBaseNightlyOptimizeJob = "__pbDBOptimize__"

var sqlitePlannerStatisticsTables = []string{"sqlite_stat1", "sqlite_stat4"}

// registerSQLitePlannerStatistics is bound before the product schema, so it
// drops the statistics after the migrations have run.
func registerSQLitePlannerStatistics(pb core.App) {
	_ = pb.Cron().Add(pocketBaseNightlyOptimizeJob, "0 0 * * *", func() {
		// PocketBase's checkpoints of both databases' logs, without its
		// optimize, stopped with the server (server_background.go).
		serverBackgroundOf(pb).run("nightly log checkpoints", func(context.Context) {
			if _, err := pb.NonconcurrentDB().NewQuery("PRAGMA wal_checkpoint(TRUNCATE)").Execute(); err != nil {
				pb.Logger().Warn("Failed to run periodic PRAGMA wal_checkpoint for the main DB", slog.String("error", err.Error()))
			}
			if _, err := pb.AuxNonconcurrentDB().NewQuery("PRAGMA wal_checkpoint(TRUNCATE)").Execute(); err != nil {
				pb.Logger().Warn("Failed to run periodic PRAGMA wal_checkpoint for the auxiliary DB", slog.String("error", err.Error()))
			}
		})
	})
	pb.OnBootstrap().BindFunc(func(event *core.BootstrapEvent) error {
		if err := event.Next(); err != nil {
			return err
		}
		dropSQLitePlannerStatistics(event.App)
		return nil
	})
}

// dropSQLitePlannerStatistics drops the data database's statistics tables
// where they exist. A table it can't drop leaves the start going, as before.
func dropSQLitePlannerStatistics(app core.App) {
	for _, table := range sqlitePlannerStatisticsTables {
		if _, err := app.NonconcurrentDB().NewQuery("DROP TABLE IF EXISTS " + table).Execute(); err != nil {
			app.Logger().Warn("Couldn't drop the SQLite planner statistics", slog.String("table", table), slog.String("error", err.Error()))
		}
	}
}
