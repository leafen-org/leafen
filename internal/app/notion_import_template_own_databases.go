package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// A template a run again takes Notion's body for anew
// (updateNotionImportTemplate) gets its own databases anew with it, copied
// from the row that stands for it. The ones the move's last copy into it
// made go for good, not to the person's trash: each run again of a template
// Notion changed had left one more of them among the deleted databases
// (2026-11-24, the gate's synthetic source: three runs again, three
// "템플릿 속 표"). One somebody changed in Leafen since that copy ended — its
// block, data source, properties, views, rows or their values, a row or a
// property moved to the trash among them — goes to the trash as a database
// deleted on screen does, and comes back from there, as a block a person
// changed stays when Notion deleted it (notion_migration_removed.go); so
// does one the move didn't make, one another database relates to, and one
// an active hold keeps.

// replaceNotionImportTemplateDatabases takes a template's own and linked
// databases away, with their views, before its body is the row's anew.
func replaceNotionImportTemplateDatabases(app core.App, job storedJob, payload notionImportJobPayload, templateID string) error {
	made, err := notionImportTemplateDatabasesAsMade(app, templateID)
	if err != nil {
		return err
	}
	if err := deleteNotionImportPageLinkedDatabases(app, job, payload, templateID); err != nil || len(made) == 0 {
		return err
	}
	if held, err := legalHoldKeepsTrashedDatabase(app, job.WorkspaceID, templateID); err != nil || held {
		return err
	}
	for _, blockID := range made {
		stored := storedDeletedDatabase{}
		err := app.DB().NewQuery(`
			SELECT [[block_id]],[[owner]],[[workspace_id]],[[page_id]],[[data_source_ids]],[[block_content]],[[title]],
				[[deleted_at]],[[expires_at]]
			FROM {{leafen_deleted_databases}} WHERE [[block_id]]={:blockId} AND [[page_id]]={:templateId}
		`).Bind(dbx.Params{"blockId": blockID, "templateId": templateID}).One(&stored)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if err := purgeDeletedDatabase(app, stored); err != nil && !errors.Is(err, errDeletedDatabaseRelated) {
			return err
		}
	}
	return nil
}

// notionImportTemplateDatabasesAsMade lists the blocks of a template's own
// databases that the move's last copy into it made — the copy that took
// the row an import made the template of (deferRowPageRemovalToDatabaseCopies),
// whose copies name them — and that nobody changed since it ended.
func notionImportTemplateDatabasesAsMade(app core.App, templateID string) ([]string, error) {
	copied := struct {
		Payload string `db:"payload_json"`
		Updated int64  `db:"updated"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[payload_json]], [[updated]] FROM {{leafen_jobs}}
		WHERE [[page_id]]={:templateId} AND [[kind]]={:kind} AND [[status]]={:completed}
			AND json_valid([[payload_json]]) AND json_extract([[payload_json]], '$.importTemplateRow')=1
		ORDER BY [[updated]] DESC LIMIT 1
	`).Bind(dbx.Params{
		"templateId": templateID, "kind": rowPageDatabaseCopyJobKind, "completed": durableJobStatusCompleted,
	}).One(&copied)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	payload := rowPageDatabaseCopyPayload{}
	if json.Unmarshal([]byte(copied.Payload), &payload) != nil {
		return nil, nil
	}
	made := []string{}
	for _, blockID := range payload.Copies {
		content := ""
		err := app.DB().NewQuery(`
			SELECT [[content]] FROM {{leafen_blocks}}
			WHERE [[id]]={:blockId} AND [[page_id]]={:templateId} AND [[type]]={:type} AND [[updated]]<={:since}
		`).Bind(dbx.Params{
			"blockId": blockID, "templateId": templateID, "type": graphBlockTypeInlineDatabase, "since": copied.Updated,
		}).Row(&content)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		database, err := parseDatabaseContainerBlockContent(json.RawMessage(content))
		if err != nil || database.LinkedDatabaseSource || database.SourceContainerID != "" || database.DataSourceID == "" {
			continue
		}
		changed, err := notionImportTemplateDatabaseChangedSince(app, database.dataSourceIDs(), copied.Updated)
		if err != nil {
			return nil, err
		}
		if !changed {
			made = append(made, blockID)
		}
	}
	return made, nil
}

// notionImportTemplateDatabaseChangedSince says whether anything of a
// database's data sources changed after since (milliseconds): the data
// source, its properties and views, its rows, their values, pages and the
// blocks of their bodies, or a row or property moved to the trash.
func notionImportTemplateDatabaseChangedSince(app core.App, dataSourceIDs []string, since int64) (bool, error) {
	members, err := json.Marshal(dataSourceIDs)
	if err != nil {
		return false, err
	}
	// A page keeps its times as text (types.DateTime).
	sinceText, err := types.ParseDateTime(time.UnixMilli(since).UTC())
	if err != nil {
		return false, err
	}
	changed := 0
	err = app.DB().NewQuery(`
		WITH [[member]]([[id]]) AS (SELECT [[value]] FROM json_each({:members})),
			[[row]]([[id]]) AS (SELECT [[id]] FROM {{leafen_database_rows}} WHERE [[data_source_id]] IN (SELECT [[id]] FROM [[member]]))
		SELECT EXISTS(SELECT 1 FROM {{leafen_data_sources}} WHERE [[id]] IN (SELECT [[id]] FROM [[member]]) AND [[updated]]>{:since})
			OR EXISTS(SELECT 1 FROM {{leafen_database_properties}} WHERE [[data_source_id]] IN (SELECT [[id]] FROM [[member]])
				AND MAX([[created]], [[updated]])>{:since})
			OR EXISTS(SELECT 1 FROM {{leafen_database_property_trash}} AS [[trash]]
				JOIN {{leafen_database_properties}} AS [[property]] ON [[property]].[[id]]=[[trash]].[[property_id]]
				WHERE [[property]].[[data_source_id]] IN (SELECT [[id]] FROM [[member]]) AND [[trash]].[[deleted_at]]>{:since})
			OR EXISTS(SELECT 1 FROM {{leafen_database_views}} WHERE [[data_source_id]] IN (SELECT [[id]] FROM [[member]])
				AND MAX([[created]], [[updated]])>{:since})
			OR EXISTS(SELECT 1 FROM {{leafen_database_rows}} WHERE [[id]] IN (SELECT [[id]] FROM [[row]])
				AND MAX([[created]], [[updated]])>{:since})
			OR EXISTS(SELECT 1 FROM {{leafen_database_row_values}} WHERE [[row_id]] IN (SELECT [[id]] FROM [[row]]) AND [[updated]]>{:since})
			OR EXISTS(SELECT 1 FROM {{leafen_database_row_trash}} WHERE [[row_id]] IN (SELECT [[id]] FROM [[row]]) AND [[trashed_at]]>{:since})
			OR EXISTS(SELECT 1 FROM {{leafen_pages}} WHERE [[id]] IN (SELECT [[id]] FROM [[row]]) AND [[updated]]>{:sinceText})
			OR EXISTS(SELECT 1 FROM {{leafen_blocks}} WHERE [[page_id]] IN (SELECT [[id]] FROM [[row]])
				AND MAX([[created]], [[updated]])>{:since})
	`).Bind(dbx.Params{
		"members": string(members), "since": since, "sinceText": sinceText.String(),
	}).Row(&changed)
	return changed != 0, err
}
