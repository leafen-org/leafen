package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A migration's report says what still isn't fully moved, not only what its
// last run met: running the same plan again keeps the views it made first as
// they were, so what those lost stays lost (2026-11-06: a first run's report
// said 62 views, the next run's only its own 12). Each run, as it writes the
// report, settles the migration's standing problems here: those of what it
// read again go, its own come in. A job's own staged problems are let go after
// days (notion_import_retention_policy.go); these stay with the migration.

const notionMigrationIssuesTable = "leafen_notion_migration_issues"

// settleNotionMigrationIssues makes the migration's standing problems those
// still true after this run: a view's stays until a run makes that view again
// (a run again keeps the views it made), any other's goes once this run read
// its block, row, database or page again, and this run's own come in.
func settleNotionMigrationIssues(app core.App, job storedJob, migrationID string) error {
	settled, err := notionMigrationIssuesSettled(app, migrationID)
	if err != nil {
		return err
	}
	if !settled {
		// A migration that ran before kept no standing problems: its earlier
		// runs, while their problems are still staged, settle first in turn.
		earlier := []string{}
		if err := app.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_jobs}}
			WHERE [[kind]]={:kind} AND [[workspace_id]]={:workspaceId} AND [[status]]={:completed} AND [[id]]<>{:jobId}
				AND json_extract([[payload_json]], '$.migrationId')={:migrationId} AND [[created]] <= {:created}
			ORDER BY [[created]], [[id]]
		`).Bind(dbx.Params{
			"kind": notionImportJobKind, "workspaceId": job.WorkspaceID, "completed": durableJobStatusCompleted,
			"jobId": job.ID, "migrationId": migrationID, "created": job.Created,
		}).Column(&earlier); err != nil {
			return err
		}
		for _, jobID := range earlier {
			if err := settleNotionMigrationRunIssues(app, jobID, job.WorkspaceID, migrationID); err != nil {
				return err
			}
		}
	}
	// Files go by what each run tried, and a row this run kept short of one
	// is noted again before this run's problems come in.
	if err := settleNotionMigrationFiles(app, job, migrationID); err != nil {
		return err
	}
	if err := settleNotionMigrationRunIssues(app, job.ID, job.WorkspaceID, migrationID); err != nil {
		return err
	}
	// A property the plan kept as Notion showed it says so (notion_import_not_whole.go).
	return settleNotionMigrationPlanAnswers(app, migrationID)
}

func settleNotionMigrationRunIssues(app core.App, jobID, workspaceID, migrationID string) error {
	return app.RunInTransaction(func(txApp core.App) error {
		params := dbx.Params{
			"migrationId": migrationID, "jobId": jobID, "workspaceId": workspaceID,
			"propertyParent": notionImportPropertyIssueParentKind, "viewParent": notionImportViewIssueParentKind,
			"file": notionMigrationFileIssueKind,
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_notion_migration_issues}}
			WHERE [[migration_id]]={:migrationId} AND (
				[[issue_id]] IN (SELECT [[source_id]] FROM {{leafen_notion_import_sources}}
					WHERE [[job_id]]={:jobId} AND [[kind]]='issue')
				OR ([[kind]] NOT IN ({:viewParent}, {:file}) AND (
					[[item_source_id]] IN (SELECT [[source_id]] FROM {{leafen_notion_import_sources}} WHERE [[job_id]]={:jobId})
					OR [[page_source_id]] IN (SELECT [[source_id]] FROM {{leafen_notion_import_sources}}
						WHERE [[job_id]]={:jobId} AND [[kind]] IN ('page', 'row')))))
		`).Bind(params).Execute(); err != nil {
			return err
		}
		// A problem under blocks, a property or a view goes up to its page
		// however deep; each step is taken once (UNION), so a chain that comes
		// back on itself ends.
		_, err := txApp.DB().NewQuery(`
			WITH RECURSIVE [[issue]] AS (
				SELECT [[source_id]] AS [[issue_id]], [[parent_kind]] AS [[kind]], [[parent_source_id]] AS [[item_source_id]],
					[[parent_source_id]] AS [[ancestor_id]], [[parent_kind]] AS [[ancestor_kind]],
					[[title]] AS [[subject]], [[text]] AS [[reason]]
				FROM {{leafen_notion_import_sources}}
				WHERE [[job_id]]={:jobId} AND [[kind]]='issue'
				UNION
				SELECT [[issue]].[[issue_id]], [[issue]].[[kind]], [[issue]].[[item_source_id]],
					[[parent]].[[parent_source_id]], [[parent]].[[parent_kind]],
					[[issue]].[[subject]], [[issue]].[[reason]]
				FROM [[issue]]
				JOIN {{leafen_notion_import_sources}} AS [[parent]]
					ON [[parent]].[[job_id]]={:jobId} AND [[parent]].[[source_id]]=[[issue]].[[ancestor_id]]
				WHERE [[issue]].[[ancestor_kind]] IN ('block', {:propertyParent}, {:viewParent})
			)
			INSERT INTO {{leafen_notion_migration_issues}}
				([[migration_id]], [[workspace_id]], [[issue_id]], [[kind]], [[item_source_id]], [[page_source_id]],
				 [[page_id]], [[page_title]], [[subject]], [[reason]])
			SELECT {:migrationId}, {:workspaceId}, [[issue]].[[issue_id]], [[issue]].[[kind]], [[issue]].[[item_source_id]],
				[[issue]].[[ancestor_id]], COALESCE([[mapping]].[[native_id]], ''), COALESCE([[page]].[[title]], ''),
				COALESCE([[issue]].[[subject]], ''), COALESCE([[issue]].[[reason]], '')
			FROM [[issue]]
			LEFT JOIN {{leafen_notion_import_mappings}} AS [[mapping]]
				ON [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[source_id]]=[[issue]].[[ancestor_id]]
				AND [[mapping]].[[kind]] IN ('page', 'row')
			LEFT JOIN {{leafen_notion_import_sources}} AS [[page]]
				ON [[page]].[[job_id]]={:jobId} AND [[page]].[[source_id]]=[[issue]].[[ancestor_id]]
			WHERE [[issue]].[[ancestor_kind]] IN ('page', 'row')
			ON CONFLICT ([[migration_id]], [[issue_id]]) DO UPDATE SET
				[[kind]]=excluded.[[kind]], [[item_source_id]]=excluded.[[item_source_id]],
				[[page_source_id]]=excluded.[[page_source_id]], [[page_id]]=excluded.[[page_id]],
				[[page_title]]=excluded.[[page_title]], [[subject]]=excluded.[[subject]], [[reason]]=excluded.[[reason]]
		`).Bind(params).Execute()
		return err
	})
}

// notionMigrationIssuesSettled says whether a migration has kept its
// standing problems yet; one that hasn't run since reads its last job's.
func notionMigrationIssuesSettled(app core.App, migrationID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM (SELECT 1 FROM {{leafen_notion_migration_issues}} WHERE [[migration_id]]={:migrationId} LIMIT 1)
	`).Bind(dbx.Params{"migrationId": migrationID}).Row(&count)
	return count > 0, err
}

// repointNotionMigrationIssuesToTemplate moves the migration's standing
// problems on a row that became a template — its blocks', views' and files' —
// onto the template, which the row's content is now, and says whether there
// were any. The report is written before rows become templates (its step
// comes before the rows' stamps, notion_import_row_stamps.go), and the row
// page goes: a report naming it would mention a page that is no more
// (2026-11-23: a move's report said a template's linked database on an empty
// mention). The run writes its report again once it ends when there were.
func repointNotionMigrationIssuesToTemplate(app core.App, migrationID, rowID, templateID string) (bool, error) {
	if migrationID == "" || rowID == "" || templateID == "" {
		return false, nil
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_migration_issues}} SET [[page_id]]={:templateId}
		WHERE [[migration_id]]={:migrationId} AND [[page_id]]={:rowId}
	`).Bind(dbx.Params{"migrationId": migrationID, "rowId": rowID, "templateId": templateID}).Execute()
	if err != nil {
		return false, err
	}
	moved, err := result.RowsAffected()
	return moved > 0, err
}
