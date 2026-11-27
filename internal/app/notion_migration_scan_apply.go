package app

import "github.com/pocketbase/pocketbase/core"

// applyNotionMigrationScanChange takes an item off the queue and writes what
// reading it found, with the counts, in one transaction.
func applyNotionMigrationScanChange(
	store notionMigrationScanStore, ordinal int64, change notionMigrationScanChange, counts *notionMigrationCounts,
) error {
	// What it read shows in Leafen's import window (notion_migration_live.go):
	// a page or database by its name, a database's rows by the database's.
	for _, page := range change.pages {
		noteNotionMigrationLive(store.migrationID, "scanning", "page", page.Title)
	}
	for _, database := range change.databases {
		noteNotionMigrationLive(store.migrationID, "scanning", "database", database.Title)
	}
	if change.updated != nil && change.rows > 0 {
		noteNotionMigrationLive(store.migrationID, "scanning", "row", change.updated.Title)
	}
	return store.app.RunInTransaction(func(txApp core.App) error {
		tx := store
		tx.app = txApp
		if err := tx.dropQueued(ordinal); err != nil {
			return err
		}
		for _, id := range change.seen {
			if err := tx.markSeen(id); err != nil {
				return err
			}
		}
		for _, page := range change.pages {
			if err := tx.addPage(page); err != nil {
				return err
			}
		}
		for pageID, blocks := range change.blocks {
			if err := tx.countBlocks(pageID, blocks); err != nil {
				return err
			}
		}
		for _, database := range change.databases {
			if err := tx.addDatabase(database); err != nil {
				return err
			}
		}
		if change.updated != nil {
			if err := tx.putDatabase(*change.updated); err != nil {
				return err
			}
		}
		for _, issue := range change.issues {
			if err := tx.addIssue(issue); err != nil {
				return err
			}
		}
		for _, person := range change.people {
			if err := tx.addPerson(person); err != nil {
				return err
			}
		}
		if err := tx.pushFront(change.queue...); err != nil {
			return err
		}
		if err := tx.keepSearchMade(change.made); err != nil {
			return err
		}
		return saveNotionMigrationCounts(txApp, store.migrationID, *counts)
	})
}
