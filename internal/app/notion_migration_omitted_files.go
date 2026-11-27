package app

import (
	"strconv"

	"github.com/pocketbase/pocketbase/core"
)

// notionMigrationWireWithTop adds the page a migration's runs made for its
// first root and, once a run is done, the files it says couldn't be copied
// (loadNotionMigrationFilesNotCopied), as its window and the agent's status
// read them.
func notionMigrationWireWithTop(app core.App, stored storedNotionMigration) (notionMigration, error) {
	migration, err := notionMigrationWire(stored)
	if err != nil {
		return migration, err
	}
	if migration.ImportTopPageID, err = notionMigrationRunTopPage(app, stored, stored.ImportJobID); err != nil {
		return migration, err
	}
	if stored.RunState == notionMigrationRunDone && stored.ImportJobID != "" {
		migration.FilesNotCopied, migration.FilesNotCopiedEarlier, err = loadNotionMigrationFilesNotCopied(app, stored.ImportJobID, stored.ID)
	}
	return migration, err
}

// notionMigrationFilesOmittedNext is what a finished migration's status tells
// an agent of the files that couldn't be copied, as the migration's window
// says them (NotionMigrationOmittedFiles.tsx): this run's, which Notion no
// longer gave, and besides those earlier runs couldn't copy, so the person
// hears the report's number. Told only that it was done, a person who then
// cleaned up Notion lost them (2026-11-14).
func notionMigrationFilesOmittedNext(migration notionMigration) string {
	if migration.RunState != notionMigrationRunDone {
		return ""
	}
	next := ""
	if omitted := migration.FilesNotCopied; omitted > 0 {
		next += " " + strconv.Itoa(omitted) + " file(s) this run couldn't bring because Notion no longer gives them (migration.filesNotCopied): " +
			"they are only in Notion, so deleting the Notion originals loses them; once restored in Notion, \"Copy files again\" in the migration's window brings them."
	}
	if earlier := migration.FilesNotCopiedEarlier; earlier > 0 {
		lead := " "
		if migration.FilesNotCopied > 0 {
			lead = " Besides, "
		}
		next += lead + strconv.Itoa(earlier) + " file(s) earlier runs couldn't copy are still not copied: this run didn't try them (their pages Notion no longer gives, " +
			"or rows it left as they were; migration.filesNotCopiedEarlier). The migration's window says them in one line with this run's; file_groups and the report page count both."
	}
	return next
}
