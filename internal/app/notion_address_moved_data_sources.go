package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A data source's mention written by the ID Notion gives it
// (<mention-data-source url="collection://ID">) names what a Notion move into
// the workspace made from that data source, as a page's mention names what a
// move made from its page (notion_address_mentions_here.go). The move's ID
// map keeps a database's first data source under the database's ID, and each
// other one, of a database of several, under its key, which ends in its own
// Notion ID (notionImportDataSourceKey); the staged database keeps its first
// data source's own Notion ID beside what it shows
// (recordNotionImportDatabaseDataSource), so a mention of it finds the
// database's entry. Both go with the move's run when its records are retired,
// as the map does; the migration's ledger keeps each data source by its own
// Notion ID past them (notion_address_moved_ledger.go).

// notionImportDatabaseDataSourceKey is where a staged database's text, a
// presentation (notionImportPresentation), keeps its first data source's
// Notion ID, which no reader of the presentation reads.
const notionImportDatabaseDataSourceKey = "dataSourceId"

// recordNotionImportDatabaseDataSource keeps a staged database's first data
// source's Notion ID in the staged database's text, as its presentation or
// on its own.
func recordNotionImportDatabaseDataSource(app core.App, jobID, databaseID, dataSourceID string) error {
	id, isUUID := normalizeNotionUUIDRequest(dataSourceID)
	if !isUUID {
		return nil
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_sources}}
		SET [[text]] = {:prefix} || json_set(
			CASE WHEN [[text]] = '' THEN '{}' ELSE substr([[text]], length({:prefix}) + 1) END, {:path}, {:dataSourceId})
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:databaseId} AND [[kind]] = 'database'
			AND ([[text]] = '' OR substr([[text]], 1, length({:prefix})) = {:prefix}
				AND json_valid(substr([[text]], length({:prefix}) + 1)))
	`).Bind(dbx.Params{
		"prefix": notionPresentationSourcePrefix, "path": "$." + notionImportDatabaseDataSourceKey,
		"dataSourceId": id, "jobId": jobID, "databaseId": databaseID,
	}).Execute()
	return err
}

// notionMovedDataSource is the data source a Notion move into the workspace
// made from the Notion data source an ID names, the latest move's first, or
// "" for none: a database's first by the database's entry, which its staged
// database names it in, and another by its key, while the job's records are
// kept; either by its own ID in the migration's ledger after.
func notionMovedDataSource(app core.App, workspaceID, id string) (string, error) {
	nativeID := ""
	err := app.DB().NewQuery(`
		SELECT [[made]].[[native_id]] FROM (
			SELECT [[mapping]].[[native_id]], [[mapping]].[[created]] AS [[made_at]]
			FROM {{leafen_jobs}} AS [[job]]
			JOIN {{leafen_notion_import_mappings}} AS [[mapping]] ON [[mapping]].[[job_id]] = [[job]].[[id]]
				AND [[mapping]].[[kind]] = 'data_source'
			WHERE [[job]].[[workspace_id]] = {:workspaceId}
				AND (substr([[mapping]].[[source_id]], -length({:keyed})) = {:keyed}
					OR [[mapping]].[[source_id]] IN (
						SELECT [[staged]].[[source_id]] FROM {{leafen_notion_import_sources}} AS [[staged]]
						WHERE [[staged]].[[job_id]] = [[job]].[[id]] AND [[staged]].[[kind]] = 'database'
							AND instr([[staged]].[[text]], {:marker}) > 0))
			UNION ALL
			SELECT [[ledger]].[[native_id]], MAX([[migration]].[[run_started_at]], [[migration]].[[created_at]])
			FROM {{leafen_notion_migrations}} AS [[migration]]
			JOIN {{leafen_notion_migration_ledger}} AS [[ledger]] ON [[ledger]].[[migration_id]] = [[migration]].[[id]]
				AND [[ledger]].[[source_id]] = {:id} AND [[ledger]].[[kind]] = {:byId}
			WHERE [[migration]].[[workspace_id]] = {:workspaceId}
` + notionMovedKeptByIDSQL + `
		) AS [[made]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[made]].[[native_id]]
			AND [[source]].[[workspace_id]] = {:workspaceId}
		ORDER BY [[made]].[[made_at]] DESC
		LIMIT 1
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "keyed": notionImportDataSourceKeyMarker + id, "id": id,
		"marker": `"` + notionImportDatabaseDataSourceKey + `":"` + id + `"`, "byId": notionMigrationLedgerDataSourceByID,
	}).Row(&nativeID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return nativeID, err
}

// dataSourceMention points a data source's mention here, in place, and tells
// whether it changed: one naming the workspace's data source by its public
// API ID, or by its own, stays; one naming by a UUID a data source a Notion
// move made names that one by its own ID, which the mention's writer rule
// reads as its database's (principalDataSourceMention, dataSourceMention).
func (here *notionAddressLinksHere) dataSourceMention(span *graphTextSpan, written string) (bool, error) {
	id, isUUID := normalizeNotionUUIDRequest(strings.TrimSpace(written))
	if !isUUID {
		return false, nil
	}
	moved, cached := here.sources[id]
	if !cached {
		_, err := resolveNotionDataSourceInternalID(here.app, here.workspaceID, id)
		switch {
		case err == nil:
		case !errors.Is(err, errInvalidNotionObjectIdentity):
			return false, err
		default:
			if moved, err = notionMovedDataSource(here.app, here.workspaceID, id); err != nil {
				return false, err
			}
		}
		here.sources[id] = moved
	}
	if moved == "" {
		return false, nil
	}
	span.DatabaseID = notionMarkdownDataSourceAddress + moved
	return true, nil
}

// notionMovedDataSourcePath is the link a write keeps for a path in the app
// naming by an ID nothing here answers to — as a link to a data source's
// address reads (notionMarkdownAppAddress: [a](collection://ID) links
// /p/<ID>, as Notion's does) — the data source a Notion move made from the
// Notion data source of that ID, at the address a link to it written by its
// public API ID reads at; else a view's (notionMovedViewPath).
func notionMovedDataSourcePath(app core.App, workspaceID, id, link string) (string, error) {
	moved, err := notionMovedDataSource(app, workspaceID, id)
	if err != nil {
		return link, err
	}
	if moved == "" {
		return notionMovedViewPath(app, workspaceID, id, link)
	}
	public, err := ensureNotionObjectIdentity(app, notionObjectIdentityDataSource, workspaceID, moved)
	if err != nil {
		return link, err
	}
	return "/p/" + strings.ReplaceAll(public, "-", ""), nil
}
