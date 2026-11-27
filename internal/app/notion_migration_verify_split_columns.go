package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
)

// The check's columns of a split: each property of a database a split sent
// rows of to another one has its column there, made ahead
// (notion_migration_split_columns.go) — what a row there shows of it,
// computed or not (2026-11-24: a formula's, a rollup's and the created and
// edited columns weren't made there, the rows lost those values, and the
// check said nothing differed). One a person took away in Leafen since is
// told apart, and one an import issue of the property explains says it.

type notionMigrationVerifySplitColumn struct {
	Database string `db:"database"`
	Target   string `db:"target"`
	SourceID string `db:"source_id"`
	Title    string `db:"title"`
	Text     string `db:"text"`
	NativeID string `db:"native_id"`
	Held     bool   `db:"held"`
	Issues   string `db:"issues"`
}

func (v *notionMigrationVerifier) splitColumns() error {
	planJSON := ""
	if err := v.app.DB().NewQuery(`SELECT [[plan_json]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": v.migrationID}).Row(&planJSON); err != nil || strings.TrimSpace(planJSON) == "" {
		return nil
	}
	plan := notionMigrationPlan{}
	if json.Unmarshal([]byte(planJSON), &plan) != nil {
		return nil
	}
	run := &notionMigrationRunPlan{columns: notionMigrationPlanColumns(plan)}
	columns := []notionMigrationVerifySplitColumn{}
	// Each database whose rows are in another data source than its own, by
	// target, with each of its properties and the column it has there.
	if err := v.app.DB().NewQuery(`
		WITH [[split]] AS (
			SELECT DISTINCT [[source]].[[parent_source_id]] AS [[database]], [[row]].[[data_source_id]] AS [[target]]
			FROM {{leafen_notion_import_sources}} AS [[source]]
			JOIN {{leafen_notion_import_mappings}} AS [[row_map]] ON [[row_map]].[[job_id]]=[[source]].[[job_id]]
				AND [[row_map]].[[kind]]='row' AND [[row_map]].[[source_id]]=[[source]].[[source_id]]
			JOIN {{leafen_database_rows}} AS [[row]] ON [[row]].[[id]]=[[row_map]].[[native_id]]
			JOIN {{leafen_notion_import_mappings}} AS [[own]] ON [[own]].[[job_id]]=[[source]].[[job_id]]
				AND [[own]].[[kind]]='data_source' AND [[own]].[[source_id]]=[[source]].[[parent_source_id]]
			WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[kind]]='row' AND [[row]].[[data_source_id]]!=[[own]].[[native_id]]
		)
		SELECT [[split]].[[database]], [[split]].[[target]], [[property]].[[source_id]], [[property]].[[title]], [[property]].[[text]],
			COALESCE([[column]].[[native_id]], '') AS [[native_id]],
			([[made]].[[id]] IS NOT NULL AND [[trash]].[[property_id]] IS NULL) AS [[held]],
			COALESCE((SELECT group_concat([[issue]].[[text]], ',') FROM {{leafen_notion_import_sources}} AS [[issue]]
				WHERE [[issue]].[[job_id]]={:jobId} AND [[issue]].[[kind]]='issue'
					AND [[issue]].[[source_id]] > [[property]].[[source_id]] || '#' AND [[issue]].[[source_id]] < [[property]].[[source_id]] || '$'), '') AS [[issues]]
		FROM [[split]]
		JOIN {{leafen_notion_import_sources}} AS [[property]] ON [[property]].[[job_id]]={:jobId}
			AND [[property]].[[kind]]='property' AND [[property]].[[parent_source_id]]=[[split]].[[database]]
		LEFT JOIN {{leafen_notion_import_mappings}} AS [[column]] ON [[column]].[[job_id]]={:jobId} AND [[column]].[[kind]]='property'
			AND [[column]].[[source_id]]=[[property]].[[source_id]] || ':split:' || [[split]].[[target]]
		LEFT JOIN {{leafen_database_properties}} AS [[made]] ON [[made]].[[id]]=[[column]].[[native_id]]
		LEFT JOIN {{leafen_database_property_trash}} AS [[trash]] ON [[trash]].[[property_id]]=[[column]].[[native_id]]
		ORDER BY [[split]].[[database]], [[split]].[[target]], [[property]].[[sequence]]
	`).Bind(dbx.Params{"jobId": v.jobID}).All(&columns); err != nil {
		return err
	}
	targets := map[string]string{}
	for key, nativeID := range v.sources {
		if !v.merged[notionMigrationVerifyDatabase(key)] {
			targets[nativeID] = v.titles[notionMigrationSourceKey(key)]
		}
	}
	for _, column := range columns {
		if column.Held || !run.splitAhead(column.SourceID) {
			continue
		}
		staged := storedNotionSource{SourceID: column.SourceID, Title: column.Title, Text: column.Text}
		planned, err := run.mergeColumn(column.Database, staged)
		if err != nil {
			continue
		}
		// One its own database lacks too is why its import issue says.
		reason := cmpOr(column.Issues, notionMigrationVerifyNotMade)
		if column.NativeID != "" {
			reason = notionMigrationVerifyTrashed
		}
		v.differ(notionMigrationVerifyDifference{
			Kind: notionMigrationVerifyKindColumn, Setting: notionMigrationVerifySettingMissing, NotionID: notionMigrationVerifyDatabase(column.Database), ID: column.Target,
			Title:  cmpOr(targets[column.Target], v.titles[notionMigrationVerifyDatabase(column.Database)]) + " · " + planned.Name,
			Notion: notionMigrationTypeName(v.language, planned.Source), Reason: reason,
		})
	}
	return nil
}
