package app

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/pocketbase/dbx"
)

// The check's rows and templates are each one Notion gave the run, and each
// template an earlier run made that Notion still gives and this run left as
// it was (notion_import_counted.go), against the Leafen row or template it
// is now. One a person moved to Leafen's trash or deleted there stays so, as
// a run again keeps a person's change, and the check lists it as taken away,
// whichever trash holds it — a row's, a template's or its database's
// (pageInTrashSQL) (2026-11-17: a template deleted in Leafen and a row moved
// to its trash while Notion still gave both were compared, and nothing
// differed).

// notionMigrationVerifyRowGroup is a database's rows, or its templates, as
// Notion gave them and as Leafen holds them.
type notionMigrationVerifyRowGroup struct {
	Database string `db:"parent_source_id"`
	Template bool   `db:"template"`
	Made     int    `db:"made"`
	Trashed  int    `db:"trashed"`
	Total    int    `db:"total"`
}

// notionMigrationVerifyHeldSQL counts, of the Leafen pages aliased page that
// made names (the row's page or the template's), those Leafen holds out of
// the trash as made, and those in its trash or gone as trashed.
func notionMigrationVerifyHeldSQL(made string) string {
	return `SUM(CASE WHEN [[page]].[[id]] IS NOT NULL AND NOT ` + pageInTrashSQL("[[page]]") + ` THEN 1 ELSE 0 END) AS [[made]],
		SUM(CASE WHEN ` + made + ` IS NOT NULL AND ([[page]].[[id]] IS NULL OR ` + pageInTrashSQL("[[page]]") + `) THEN 1 ELSE 0 END) AS [[trashed]],
		COUNT(*) AS [[total]]`
}

// rows compares each database's rows and templates as Notion gave them with
// those the run made: rows keep who they are when the plan merges or splits
// them, so each is found by its own Notion ID; a template by the template
// the migration's ledger names, or, before the run makes it, the row that
// stands for it.
func (v *notionMigrationVerifier) rows() error {
	groups := []notionMigrationVerifyRowGroup{}
	made := `COALESCE([[made_template]].[[native_id]], [[mapping]].[[native_id]])`
	params := notionImportCountedParams(dbx.Params{"jobId": v.jobID, "migrationId": v.migrationID})
	if err := v.app.DB().NewQuery(`
		SELECT [[source]].[[parent_source_id]], instr([[source]].[[text]], {:countedTemplate})>0 AS [[template]],
			` + notionMigrationVerifyHeldSQL(made) + `
		FROM {{leafen_notion_import_sources}} AS [[source]]
		LEFT JOIN {{leafen_notion_import_mappings}} AS [[mapping]] ON [[mapping]].[[job_id]]=[[source]].[[job_id]]
			AND [[mapping]].[[source_id]]=[[source]].[[source_id]] AND [[mapping]].[[kind]]='row'
		LEFT JOIN {{leafen_notion_migration_ledger}} AS [[made_template]] ON [[made_template]].[[migration_id]]={:migrationId}
			AND [[made_template]].[[source_id]]=[[source]].[[source_id]] AND [[made_template]].[[kind]]={:countedLedgerTemplate}
		LEFT JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]]=` + made + `
		WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[kind]]='row'
		GROUP BY [[source]].[[parent_source_id]], [[template]]
	`).Bind(params).All(&groups); err != nil {
		return err
	}
	// The templates it left as they were, by the database the ledger keeps
	// with what their bodies hold, or that their template is of.
	kept := []notionMigrationVerifyRowGroup{}
	if v.migrationID != "" {
		if err := v.app.DB().NewQuery(`
			SELECT COALESCE(NULLIF(CASE WHEN json_valid([[body]].[[state]]) THEN json_extract([[body]].[[state]], '$.database') END, ''),
				(SELECT [[source]].[[source_id]] FROM {{leafen_notion_import_mappings}} AS [[source]]
					WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[kind]]='data_source'
						AND [[source]].[[native_id]]=[[made_template]].[[data_source_id]] LIMIT 1), '') AS [[parent_source_id]],
				1 AS [[template]], ` + notionMigrationVerifyHeldSQL(`[[kept]].[[native_id]]`) + `
			FROM (SELECT [[source_id]], [[native_id]] FROM {{leafen_notion_migration_ledger}} WHERE ` + notionImportKeptTemplatesWhereSQL + `) AS [[kept]]
			LEFT JOIN {{leafen_notion_migration_ledger}} AS [[body]] ON [[body]].[[migration_id]]={:migrationId}
				AND [[body]].[[kind]]={:countedLedgerTemplateBody} AND [[body]].[[source_id]]=[[kept]].[[source_id]]
			LEFT JOIN {{leafen_database_templates}} AS [[made_template]] ON [[made_template]].[[id]]=[[kept]].[[native_id]]
			LEFT JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]]=[[kept]].[[native_id]]
			GROUP BY 1
		`).Bind(params).All(&kept); err != nil {
			return err
		}
	}
	for _, more := range kept {
		at := slices.IndexFunc(groups, func(group notionMigrationVerifyRowGroup) bool {
			return group.Database == more.Database && group.Template
		})
		if at < 0 {
			groups = append(groups, more)
			continue
		}
		groups[at].Made, groups[at].Trashed, groups[at].Total =
			groups[at].Made+more.Made, groups[at].Trashed+more.Trashed, groups[at].Total+more.Total
	}
	slices.SortFunc(groups, func(a, b notionMigrationVerifyRowGroup) int {
		if order := cmp.Compare(a.Database, b.Database); order != 0 || a.Template == b.Template {
			return order
		}
		if a.Template {
			return 1
		}
		return -1
	})
	for _, group := range groups {
		// A database the run didn't make — left out by the plan, or one Notion
		// didn't give, which the import reports — has nothing to compare.
		if _, made := v.sources[group.Database]; !made && !v.merged[notionMigrationVerifyDatabase(group.Database)] {
			continue
		}
		kind := notionMigrationVerifyKindRows
		if group.Template {
			kind, v.verify.Templates = notionMigrationVerifyKindTemplates, v.verify.Templates+group.Total
		} else {
			v.verify.Rows += group.Total
		}
		if group.Made == group.Total {
			continue
		}
		reason := notionMigrationVerifyNotMade
		if group.Made+group.Trashed == group.Total {
			reason = notionMigrationVerifyTrashed
		}
		database := notionMigrationVerifyDatabase(group.Database)
		v.differ(notionMigrationVerifyDifference{
			Kind: kind, Setting: notionMigrationVerifySettingCount, NotionID: database, ID: v.sources[group.Database], Title: v.titles[database],
			Notion: fmt.Sprint(group.Total), Leafen: fmt.Sprint(group.Made), Reason: reason,
		})
	}
	return nil
}
