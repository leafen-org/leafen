package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A deleted database — its block gone from its page, the trash keeping it
// with its block's content and its sources (deleted_databases.go) — goes by
// the name it went by on its page (databaseNameFrom), its sources' names as
// they are now, one it showed from another database among them
// (database_source_titles.go); one whose sources are all gone by the name it
// left with.

// deletedDatabaseNames reads the names of the deleted databases listed by
// their blocks' IDs, as the screen names them.
func deletedDatabaseNames(app core.App, blockIDs []string) (map[string]databaseContainerName, error) {
	return deletedDatabaseNamesBy(app, blockIDs, databaseNamingShown)
}

// deletedDatabaseAPINames reads them as their API names them, by their own
// sources alone.
func deletedDatabaseAPINames(app core.App, blockIDs []string) (map[string]databaseContainerName, error) {
	return deletedDatabaseNamesBy(app, blockIDs, databaseNamingAPI)
}

// deletedDatabaseNamesBy reads them where naming names them.
func deletedDatabaseNamesBy(app core.App, blockIDs []string, naming databaseNaming) (map[string]databaseContainerName, error) {
	names := map[string]databaseContainerName{}
	encoded, err := json.Marshal(blockIDs)
	if len(blockIDs) == 0 || err != nil {
		return names, err
	}
	stored := []struct {
		BlockID string `db:"block_id"`
		PageID  string `db:"page_id"`
		Content string `db:"block_content"`
		Title   string `db:"title"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[block_id]], [[page_id]], [[block_content]], [[title]] FROM {{leafen_deleted_databases}}
		WHERE [[block_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&stored); err != nil {
		return nil, err
	}
	for _, deleted := range stored {
		own, live := databaseOwnSources([]byte(deleted.Content))
		sources := make([]databaseNameSource, 0, len(own))
		// A linked database's own name stands (deleted.Title); one of its own
		// lists those it showed from others beside its own.
		linked := []string{}
		if len(own) > 0 {
			linked = databaseLinkedMemberIDs([]byte(deleted.Content))
		}
		for _, id := range append(slices.Clone(own), linked...) {
			source := struct {
				Title string `db:"title"`
				Spans string `db:"title_spans"`
			}{}
			err := app.DB().NewQuery(`
				SELECT [[title]], COALESCE([[title_spans]], '') AS [[title_spans]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}
			`).Bind(dbx.Params{"id": id}).One(&source)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			isLinked := slices.Contains(linked, id) && !slices.Contains(own, id)
			sources = append(sources, databaseNameSource{
				databaseNameRead: databaseNameRead{Title: source.Title, Spans: decodeStoredTextSpans(source.Spans), Linked: isLinked},
				Gone:             !isLinked && !slices.Contains(live, id),
			})
		}
		name := databaseNameFrom(false, "", databaseContainerKindSource, []byte(deleted.Content), sources, naming)
		name.PageID = deleted.PageID
		if !name.Sourced {
			name.Title, name.Spans = deleted.Title, nil
		}
		names[deleted.BlockID] = name
	}
	return names, nil
}
