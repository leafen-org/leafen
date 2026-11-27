package app

import (
	"context"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A migration's scan goes on on the server once asked (2026-11-22: one
// leafen_notion_migration_scan call read four steps of up to 25 seconds each
// and held 1m01s, and a Synology reverse proxy's 60 seconds cut it off with
// a 504; that scan of 34 pages, 16 databases and 4,356 rows took four and a
// half minutes). A call — the agent's, or the import screen's — sets it going
// when it isn't, and answers as soon as it ends or after longJobCallMost with
// how far it is; the reading goes on between calls, each step keeping what
// it read, so a call cut off on its way loses nothing. A scan that reads
// nothing for a while — Notion down, the connection refused for a moment —
// stops going on until the next call.

// notionMigrationScanIdleSteps is how many steps in a row that read nothing
// a scan going on by itself takes before it waits for the next call.
const notionMigrationScanIdleSteps = 30

var notionMigrationScansGoing sync.Map

// scanNotionMigrationOn sets a migration's scan going and answers it as it
// is once it ends or after longJobCallMost, whichever comes first.
func scanNotionMigrationOn(
	ctx context.Context, app core.App, ownerID, workspaceID, id string,
	clientFor func(connectionID, token string) notionProviderClient,
) (notionMigration, error) {
	read := func() (notionMigration, error) {
		stored, err := loadNotionMigration(app, ownerID, workspaceID, id)
		if err != nil {
			return notionMigration{}, err
		}
		return notionMigrationWire(stored)
	}
	migration, err := read()
	if err != nil || migration.Status != notionMigrationStatusScanning {
		return migration, err
	}
	goOnNotionMigrationScan(app, ownerID, workspaceID, migration.ID, clientFor)
	return waitWhileJobGoes(ctx, int(longJobCallMost/time.Second), read, func(migration notionMigration) bool {
		return migration.Status == notionMigrationStatusScanning
	})
}

// goOnNotionMigrationScan reads a migration's structure step after step on
// the server, once at a time, until it is scanned, stopped, or reads nothing
// for notionMigrationScanIdleSteps steps.
func goOnNotionMigrationScan(
	app core.App, ownerID, workspaceID, id string, clientFor func(connectionID, token string) notionProviderClient,
) {
	if _, going := notionMigrationScansGoing.LoadOrStore(id, true); going {
		return
	}
	go func() {
		defer notionMigrationScansGoing.Delete(id)
		requests, idle := -1, 0
		for idle < notionMigrationScanIdleSteps {
			started := time.Now()
			migration, err := stepNotionMigration(context.Background(), app, ownerID, workspaceID, id, clientFor)
			if err != nil {
				app.Logger().Warn("notion migration scan: a step failed", "migration", id, "error", err)
				return
			}
			if migration.Status != notionMigrationStatusScanning {
				return
			}
			if migration.Counts.Requests == requests {
				idle++
				// A step that read nothing at once waits a little before the next.
				time.Sleep(max(0, 2*time.Second-time.Since(started)))
			} else {
				requests, idle = migration.Counts.Requests, 0
			}
		}
	}()
}
