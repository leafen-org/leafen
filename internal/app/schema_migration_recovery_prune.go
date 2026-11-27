package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// The recovery points the starts keep (schema_migration_recovery.go) go once
// a later one has served: each is the data as it was before an update, and
// only the newest way back is kept, so they don't pile up update after
// update (each about the database's size, and a hard-linked one holds the
// attachments deleted since). On data at schema N, a server that has served
// for a while keeps
//
//   - the way back: the newest point that brought the data to N;
//   - every point made from N or later: an update from here that failed
//     left the data as that point holds it, and is tried again (each time
//     the data changed between tries, kept again beside it as <pair>-2 …);
//
// and lets the others go — points of updates the data has gone past, and
// attempts superseded by the update that brought it to N. With no point
// that brought the data to N (it never took an update here, or came from a
// backup, which leaves the points out) nothing goes. The zips of the
// earlier form go by the same rule. A point goes whole: renamed away into
// Leafen's own work folder first, its receipt next, so a stop half-way
// leaves either the point or nothing the start would read.
//
// The points stay out of the backup screen: they are this data folder's own
// way back, which the rule keeps and lets go by itself; the start line says
// where the way back is and the self-hosting docs how to take it.

// migrationRecoveryLetGoAfter is how long a server serves before the points
// it no longer needs go, so a release that fails at once leaves them.
const migrationRecoveryLetGoAfter = 5 * time.Minute

// A pair's point kept again is named with -2, -3 … (ensureMigrationRecovery).
var migrationRecoveryNamePattern = regexp.MustCompile(`^leafen_pre_migration_v(\d+)_to_v(\d+)(-[0-9]+)?(\.zip|\.json)?$`)

type migrationRecoveryPoint struct {
	from, to int
	names    []string
	created  time.Time
}

// letGoOfPassedMigrationRecoveryPoints lets the points the data no longer
// needs go, a while after the server began to serve.
func letGoOfPassedMigrationRecoveryPoints(app core.App) {
	serverBackgroundOf(app).run("migration recovery points", func(ctx context.Context) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(migrationRecoveryLetGoAfter):
		}
		current := 0
		if err := app.DB().NewQuery(`SELECT COALESCE(MAX([[version]]), 0) FROM {{leafen_schema_migrations}}`).Row(&current); err != nil || current == 0 {
			return
		}
		// Not while another process on the data brings it up to date.
		file, err := openBackupLockFile(filepath.Join(app.DataDir(), core.LocalBackupsDirName, migrationTurnLockName))
		if err != nil {
			return
		}
		defer file.Close()
		unlock, taken, err := tryExclusiveBackupFileLock(file)
		if err != nil || !taken {
			return
		}
		defer unlock()
		removed, err := letGoOfMigrationRecoveryPoints(app.DataDir(), current)
		if err != nil {
			app.Logger().Warn("migration recovery points couldn't all be let go", "error", err)
		}
		if len(removed) > 0 {
			app.Logger().Info("migration recovery points an earlier update kept are let go", "schema", current, "removed", removed)
			fmt.Printf("leafen: the data kept before earlier updates went, as this one serves on schema %d: %s\n", current, strings.Join(removed, ", "))
		}
	})
}

// letGoOfMigrationRecoveryPoints applies the rule to the points of a data
// folder at schema current, and names the points it removed.
func letGoOfMigrationRecoveryPoints(dataDir string, current int) ([]string, error) {
	root := filepath.Join(dataDir, core.LocalBackupsDirName, "migrations")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	points := map[string]*migrationRecoveryPoint{}
	for _, entry := range entries {
		match := migrationRecoveryNamePattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		base := migrationRecoveryName(atoiMigrationVersion(match[1]), atoiMigrationVersion(match[2])) + match[3]
		point := points[base]
		if point == nil {
			point = &migrationRecoveryPoint{from: atoiMigrationVersion(match[1]), to: atoiMigrationVersion(match[2])}
			points[base] = point
		}
		point.names = append(point.names, entry.Name())
		if info, err := entry.Info(); err == nil && info.ModTime().After(point.created) {
			point.created = info.ModTime()
		}
		if match[4] == ".json" {
			receipt := migrationRecoveryReceipt{}
			raw, err := os.ReadFile(filepath.Join(root, entry.Name()))
			if err == nil && json.Unmarshal(raw, &receipt) == nil {
				if created, err := time.Parse(time.RFC3339Nano, receipt.CreatedAt); err == nil {
					point.created = created
				}
			}
		}
	}
	var wayBack *migrationRecoveryPoint
	for _, point := range points {
		if point.to == current && (wayBack == nil || point.created.After(wayBack.created)) {
			wayBack = point
		}
	}
	if wayBack == nil {
		return nil, nil
	}
	bases := make([]string, 0, len(points))
	for base := range points {
		bases = append(bases, base)
	}
	sort.Strings(bases)
	removed := []string{}
	var errs []error
	for _, base := range bases {
		point := points[base]
		if point == wayBack || point.from >= current {
			continue
		}
		if err := removeMigrationRecoveryPoint(dataDir, root, point); err != nil {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, base)
	}
	return removed, errors.Join(errs...)
}

// removeMigrationRecoveryPoint removes a point whole: its folder or zip
// renamed away into Leafen's own work folder, then its receipt, then what
// was renamed away (a stop before that leaves it to the work folder's
// clearing a day on).
func removeMigrationRecoveryPoint(dataDir string, root string, point *migrationRecoveryPoint) error {
	work := leafenTempDir(dataDir)
	if err := os.MkdirAll(work, 0o700); err != nil {
		return err
	}
	away := []string{}
	receipt := ""
	for _, name := range point.names {
		if filepath.Ext(name) == ".json" {
			receipt = name
			continue
		}
		target := filepath.Join(work, "migration-recovery-gone-"+security.RandomString(12))
		if err := os.Rename(filepath.Join(root, name), target); err != nil {
			return err
		}
		away = append(away, target)
	}
	if receipt != "" {
		if err := os.Remove(filepath.Join(root, receipt)); err != nil {
			return err
		}
	}
	if err := syncDirectory(root); err != nil {
		return err
	}
	var errs []error
	for _, target := range away {
		errs = append(errs, os.RemoveAll(target))
	}
	return errors.Join(errs...)
}

func atoiMigrationVersion(text string) int {
	value, _ := strconv.Atoi(text)
	return value
}
