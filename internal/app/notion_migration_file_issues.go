package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A file the migration couldn't copy stays one of its standing problems until
// a run copies it (2026-11-07: a run again leaves a row Notion didn't change as
// it was and tries none of its files, and its report lost the two files never
// copied and the files value they left short). Each run's file tasks say what
// it tried; a file it didn't try keeps what the run before said, after that
// run's own tasks are let go too (notion_import_retention_policy.go).

const notionMigrationFileIssueKind = "file"

// forgetNotionMigrationFileIssue takes a file copied again ("Copy files
// again") out of its migration's standing problems, so its status and window
// count it no more (loadNotionMigrationFilesNotCopied).
func forgetNotionMigrationFileIssue(app core.App, migrationID, sourceID string) error {
	if migrationID == "" {
		return nil
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_migration_issues}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]={:file} AND [[issue_id]]={:sourceId}
	`).Bind(dbx.Params{"migrationId": migrationID, "file": notionMigrationFileIssueKind, "sourceId": sourceID}).Execute()
	return err
}

// settleNotionMigrationFiles brings the file outcomes of the migration's runs,
// in order up to this one, into its standing problems, and notes again in this
// run each files value a file still leaves short.
func settleNotionMigrationFiles(app core.App, job storedJob, migrationID string) error {
	runs := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_jobs}}
		WHERE [[kind]]={:kind} AND [[workspace_id]]={:workspaceId}
			AND json_extract([[payload_json]], '$.migrationId')={:migrationId}
			AND ([[id]]={:jobId} OR ([[status]]={:completed} AND [[created]] <= {:created}))
		ORDER BY [[created]], [[id]]
	`).Bind(dbx.Params{
		"kind": notionImportJobKind, "workspaceId": job.WorkspaceID, "completed": durableJobStatusCompleted,
		"jobId": job.ID, "migrationId": migrationID, "created": job.Created,
	}).Column(&runs); err != nil {
		return err
	}
	for _, run := range runs {
		if err := settleNotionMigrationRunFiles(app, run, job.WorkspaceID, migrationID); err != nil {
			return err
		}
	}
	return noteNotionMigrationValuesLeftShort(app, job.ID, migrationID)
}

// settleNotionMigrationRunFiles replaces the standing problems of the files a
// run tried with what came of them.
func settleNotionMigrationRunFiles(app core.App, jobID, workspaceID, migrationID string) error {
	return app.RunInTransaction(func(txApp core.App) error {
		params := dbx.Params{"migrationId": migrationID, "workspaceId": workspaceID, "jobId": jobID, "file": notionMigrationFileIssueKind}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_notion_migration_issues}}
			WHERE [[migration_id]]={:migrationId} AND [[kind]]={:file} AND [[issue_id]] IN (
				SELECT [[source_id]] FROM {{leafen_notion_import_file_tasks}} WHERE [[job_id]]={:jobId} AND [[status]] <> 'pending')
		`).Bind(params).Execute(); err != nil {
			return err
		}
		cursor := ""
		for {
			files, err := loadNotionImportFileOutcomeRows(txApp, jobID, cursor, notionMigrationReportIssues)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			for _, file := range files {
				if file.Status != "omitted" {
					continue
				}
				if _, err := txApp.DB().NewQuery(`
					INSERT INTO {{leafen_notion_migration_issues}}
						([[migration_id]], [[workspace_id]], [[issue_id]], [[kind]], [[item_source_id]], [[page_source_id]],
						 [[page_id]], [[page_title]], [[subject]], [[reason]])
					VALUES ({:migrationId}, {:workspaceId}, {:sourceId}, {:file}, {:sourceId}, '', {:pageId}, {:pageTitle}, {:name}, {:reason})
					ON CONFLICT ([[migration_id]], [[issue_id]]) DO UPDATE SET
						[[page_id]]=excluded.[[page_id]], [[page_title]]=excluded.[[page_title]],
						[[subject]]=excluded.[[subject]], [[reason]]=excluded.[[reason]]
				`).Bind(dbx.Params{
					"migrationId": migrationID, "workspaceId": workspaceID, "sourceId": file.SourceID,
					"file": notionMigrationFileIssueKind, "pageId": file.PageID, "pageTitle": file.PageTitle,
					"name": file.Name, "reason": file.ErrorCode,
				}).Execute(); err != nil {
					return err
				}
			}
			if len(files) < notionMigrationReportIssues {
				return nil
			}
			cursor = files[len(files)-1].SourceID
		}
	})
}

// noteNotionMigrationValuesLeftShort notes in a run the files values its rows
// still hold short of a file never copied, as the run that left it out did:
// a row the run kept as it was took no values again to note it.
func noteNotionMigrationValuesLeftShort(app core.App, jobID, migrationID string) error {
	files := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[issue_id]] FROM {{leafen_notion_migration_issues}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]={:file} AND instr([[issue_id]], {:marker}) > 0
	`).Bind(dbx.Params{
		"migrationId": migrationID, "file": notionMigrationFileIssueKind, "marker": notionImportPropertyFileMarker,
	}).Column(&files); err != nil {
		return err
	}
	for _, file := range files {
		rowID, propertyID, _, property := notionImportPropertyFileTarget(file)
		if !property {
			continue
		}
		var parent string
		err := app.DB().NewQuery(`
			SELECT [[parent_source_id]] FROM {{leafen_notion_import_sources}}
			WHERE [[job_id]]={:jobId} AND [[source_id]]={:rowId} AND [[kind]]='row'
		`).Bind(dbx.Params{"jobId": jobID, "rowId": rowID}).Row(&parent)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if err := recordNotionImportPropertyIssue(
			app, jobID, notionPropertySourceID(parent, propertyID), notionImportIssuePropertyValueOmitted, false,
		); err != nil {
			return err
		}
	}
	return nil
}

// loadNotionMigrationFileIssues reads the files a migration still couldn't
// copy, as a run's file outcomes are read.
func loadNotionMigrationFileIssues(app core.App, migrationID string, limit int) ([]storedNotionFileOutcome, error) {
	stored := []struct {
		SourceID  string `db:"issue_id"`
		Name      string `db:"subject"`
		ErrorCode string `db:"reason"`
		PageID    string `db:"page_id"`
		PageTitle string `db:"page_title"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[issue_id]], [[subject]], [[reason]], [[page_id]], [[page_title]] FROM {{leafen_notion_migration_issues}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]={:file}
		ORDER BY [[issue_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"migrationId": migrationID, "file": notionMigrationFileIssueKind, "limit": limit}).All(&stored); err != nil {
		return nil, err
	}
	files := make([]storedNotionFileOutcome, 0, len(stored))
	for _, file := range stored {
		files = append(files, storedNotionFileOutcome{
			storedNotionFileTask: storedNotionFileTask{SourceID: file.SourceID, Name: file.Name, Status: "omitted", ErrorCode: file.ErrorCode},
			PageID:               file.PageID, PageTitle: file.PageTitle,
		})
	}
	return files, nil
}
