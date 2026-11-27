package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
)

// An agent starts a migration naming each root by its id and kind, as
// leafen_notion_migration_sources lists them; a title it gives is kept, and
// one it leaves out is Notion's: the scan names the root as Notion titles it
// when it reads it (titleRoot), so the import screen's history and the
// tools' answers name what moves as they do for a root the screen picked. A
// root Notion leaves untitled keeps no title, as the screen's does.

type leafenMCPNotionMigrationRoot struct {
	ID    string `json:"id" jsonschema:"The Notion page's or database's ID or address, as leafen_notion_migration_sources lists it"`
	Kind  string `json:"kind" jsonschema:"page or database"`
	Title string `json:"title,omitempty" jsonschema:"Not needed: the scan reads the title from Notion"`
}

func notionMigrationRootsNamed(roots []leafenMCPNotionMigrationRoot) []notionImportRoot {
	named := make([]notionImportRoot, 0, len(roots))
	for _, root := range roots {
		named = append(named, notionImportRoot{ID: root.ID, Kind: root.Kind, Title: root.Title})
	}
	return named
}

// titleRoot gives the migration's root with this id the title the scan read
// for it, when it has none yet. Only a root is scanned with no parent.
func (store notionMigrationScanStore) titleRoot(id, parentID, title string) error {
	title = strings.TrimSpace(title)
	if parentID != "" || title == "" {
		return nil
	}
	var rootsJSON string
	if err := store.app.DB().NewQuery(`SELECT [[roots_json]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": store.migrationID}).Row(&rootsJSON); err != nil {
		return err
	}
	roots := []notionImportRoot{}
	if err := json.Unmarshal([]byte(rootsJSON), &roots); err != nil {
		return err
	}
	titled := false
	for index, root := range roots {
		if strings.TrimSpace(root.Title) == "" && notionMigrationSourceKey(root.ID) == notionMigrationSourceKey(id) {
			roots[index].Title, titled = title, true
		}
	}
	if !titled {
		return nil
	}
	encoded, err := json.Marshal(roots)
	if err != nil {
		return err
	}
	_, err = store.app.DB().NewQuery(`UPDATE {{leafen_notion_migrations}} SET [[roots_json]]={:roots} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"roots": string(encoded), "id": store.migrationID}).Execute()
	return err
}

// withRootTitles is the migration as its scan step leaves it: with the
// titles that step read for its roots, for the step's answer.
func (store notionMigrationScanStore) withRootTitles(stored storedNotionMigration) storedNotionMigration {
	var rootsJSON string
	if err := store.app.DB().NewQuery(`SELECT [[roots_json]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": store.migrationID}).Row(&rootsJSON); err == nil {
		stored.RootsJSON = rootsJSON
	}
	return stored
}
