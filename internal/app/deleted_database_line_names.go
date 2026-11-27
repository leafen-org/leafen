package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The trash's databases are named as a database is everywhere
// (databaseContainerNames, deletedDatabaseNames), as their pages' paths name
// them (seen 2026-11-24 in Notion's paths of the rows of a database whose
// block alone went to the trash: by its own name, else its sources'): a
// database's line by its name — an inline one of several sources by the
// name it keeps, else by its sources' names (Title empty, SourceTitles, which
// the trash lists in its reader's language) — and a source kept in its
// database by its own, placed through its database so named (DatabaseTitle,
// SourceTitles).

// deletedDatabaseCandidate is a line the trash's window may list, with its
// database: a deleted one's block, or the source database's — a source kept
// in it placed through it — and how many sources that database owns.
type deletedDatabaseCandidate struct {
	item        deletedDatabase
	container   bool
	containerID string
	ownSources  int
}

// nameDeletedDatabaseLines names the lines kept by their databases.
func nameDeletedDatabaseLines(app core.App, kept []deletedDatabaseCandidate) ([]deletedDatabase, error) {
	containers, deleted := []string{}, []string{}
	for _, entry := range kept {
		if entry.container {
			containers = append(containers, entry.containerID)
		} else {
			deleted = append(deleted, entry.containerID)
		}
	}
	live, err := databaseContainerNames(app, containers)
	if err != nil {
		return nil, err
	}
	gone, err := deletedDatabaseNames(app, deleted)
	if err != nil {
		return nil, err
	}
	items := make([]deletedDatabase, 0, len(kept))
	for _, entry := range kept {
		item := entry.item
		names := gone
		if entry.container {
			names = live
		}
		if name, found := names[entry.containerID]; found {
			title := strings.TrimSpace(name.Title)
			sources := databaseSourceTitlesOf(name.Sources)
			if len(sources) < 2 {
				sources = nil
			}
			item.SourceTitles = sources
			if item.InPlace {
				item.DatabaseTitle = title
			} else {
				item.Title = title
			}
		}
		items = append(items, item)
	}
	return items, nil
}
