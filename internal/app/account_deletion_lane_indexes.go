package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The account cleanup reads a person's rows by their key while it holds the
// server's one write: each lane a window at a time, the last step at once,
// and a deletion's request counts every lane in its own write, twice — as it
// rehearses and as it goes (countAdministratorAccountCleanupItems). So each
// of those tables keeps an index serving the read: led by its key, or by its
// workspace and then the key for a lane read a workspace at a time
// (administratorAccountCleanupLaneRows), neither partial, the columns
// compared as written (schemaIndexReadPaths). Ten lanes had none and read
// their whole table at each turn (2026-11-17, ten tables of 200,000 rows on
// a NAS-class server: a deletion's request held the write 4.8 s, 0.37 s with
// the indexes, and its cleanup 16.4 s, 13.8 s with them). Migration 816
// gives the ten theirs; validateAccountDeletionLaneIndexes, part of the
// inventory check, keeps a later lane from joining without one: a
// development build stops at its start, a release build refuses the
// deletion until an update adds it.

// validateAccountDeletionLaneIndexes checks that an index serves each read
// of the cleanup's lanes and last step. The inventory check has found every
// one of their tables in the schema by then.
func validateAccountDeletionLaneIndexes(app core.App) error {
	lanes := append(append([]administratorAccountCleanupTable{}, administratorAccountCleanupTables...), administratorAccountLastStepTables...)
	tables := make([]string, 0, len(lanes))
	for _, lane := range lanes {
		tables = append(tables, lane.Table)
	}
	paths, err := readSchemaIndexReadPaths(app, tables)
	if err != nil {
		return err
	}
	for _, lane := range lanes {
		read := []string{lane.Key}
		if lane.Workspace != "" {
			read = []string{lane.Workspace, lane.Key}
		}
		if !paths.ledBy(lane.Table, read...) {
			return fmt.Errorf("%w: %s is read by %s with no index led by it", errAccountDeletionInventory, lane.Table, strings.Join(read, ", "))
		}
	}
	return nil
}
