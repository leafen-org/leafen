package app

import (
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// instanceProductExportTrackedTableNames are the tables whose changes an
// export in progress watches: every table the policy carries, as the
// schema holds it. A migration that adds such a table adds its triggers
// (trackInstanceProductExportSourceRevisions), and the schema baseline's
// check holds the triggers to exactly these tables
// (schema_baseline_standing.go).
func instanceProductExportTrackedTableNames(app core.App) []string {
	tables := []string{}
	for _, table := range instanceProductExportIncludedTableNames() {
		if app.HasTable(table) {
			tables = append(tables, table)
		}
	}
	return tables
}

func validateInstanceProductExportSourceRevisionSchema(app core.App, tables []string, exact bool) error {
	var columns int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM pragma_table_info('leafen_instance_product_export_source_revisions')
		WHERE [[name]] IN ('revision', 'job_id', 'table_name', 'row_id', 'changed')
	`).Row(&columns); err != nil {
		return err
	}
	var indexes int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM [[sqlite_master]]
		WHERE [[type]] = 'index' AND [[name]] = 'idx_leafen_instance_product_export_source_revisions_row'
	`).Row(&indexes); err != nil {
		return err
	}
	triggerNames := make([]string, 0, len(tables)*3)
	params := dbx.Params{}
	placeholders := make([]string, 0, len(tables)*3)
	for _, table := range tables {
		for _, event := range instanceProductExportRevisionEvents {
			key := fmt.Sprintf("trigger%d", len(triggerNames))
			triggerNames = append(triggerNames, instanceProductExportRevisionTrigger(table, event))
			params[key] = triggerNames[len(triggerNames)-1]
			placeholders = append(placeholders, "{:"+key+"}")
		}
	}
	var requiredSourceTriggers int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM [[sqlite_master]]
		WHERE [[type]] = 'trigger' AND [[name]] IN (` + joinSQL(placeholders) + `)
	`).Bind(params).Row(&requiredSourceTriggers); err != nil {
		return err
	}
	totalSourceTriggers := requiredSourceTriggers
	if exact {
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM [[sqlite_master]]
			WHERE [[type]] = 'trigger' AND [[name]] LIKE 'leafen_instance_product_export_revision_%'
		`).Row(&totalSourceTriggers); err != nil {
			return err
		}
	}
	var lifecycleTriggers int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM [[sqlite_master]] WHERE [[type]] = 'trigger'
			AND [[name]] IN (
				'leafen_instance_product_export_source_revision_job_terminal',
				'leafen_instance_product_export_source_revision_job_delete'
			)
	`).Row(&lifecycleTriggers); err != nil {
		return err
	}
	var invalid int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_instance_product_export_source_revisions}}
		WHERE [[revision]] < 1 OR [[job_id]] = '' OR [[table_name]] = ''
			OR [[row_id]] < 1 OR [[changed]] NOT IN (0, 1)
	`).Row(&invalid); err != nil {
		return err
	}
	if columns != 5 || indexes != 1 || requiredSourceTriggers != len(triggerNames) ||
		(exact && totalSourceTriggers != len(triggerNames)) || lifecycleTriggers != 2 || invalid != 0 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}

func createInstanceProductExportSourceRevision(app core.App, jobID, table string, rowID int64) (int64, error) {
	if jobID == "" {
		jobID = "fixture"
	}
	var revision int64
	err := app.DB().NewQuery(`
		INSERT INTO {{leafen_instance_product_export_source_revisions}}
			([[job_id]], [[table_name]], [[row_id]], [[changed]])
		VALUES ({:jobId}, {:table}, {:rowId}, 0)
		RETURNING [[revision]]
	`).Bind(dbx.Params{"jobId": jobID, "table": table, "rowId": rowID}).Row(&revision)
	if err != nil || revision < 1 {
		return 0, newInstanceProductExportFailure(errorCodeInstanceProductExportPolicyIncomplete, err)
	}
	return revision, nil
}

func loadInstanceProductExportSourceRevision(app core.App, revision int64) (bool, error) {
	var changed int
	err := app.DB().NewQuery(`
		SELECT [[changed]] FROM {{leafen_instance_product_export_source_revisions}}
		WHERE [[revision]] = {:revision}
	`).Bind(dbx.Params{"revision": revision}).Row(&changed)
	if err != nil {
		return false, fmt.Errorf("%s: source revision %d: %w", errorCodeInstanceProductExportSourceChanged, revision, err)
	}
	return changed == 1, nil
}

func releaseInstanceProductExportSourceRevision(app core.App, revision int64) error {
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_instance_product_export_source_revisions}}
		WHERE [[revision]] = {:revision}
	`).Bind(dbx.Params{"revision": revision}).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return newInstanceProductExportFailure(errorCodeInstanceProductExportSourceChanged, err)
	}
	return nil
}
