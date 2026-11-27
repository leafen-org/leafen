package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The database templates a migration's run made and the views' own default
// templates, as what is left of them at Leafen's defaults
// (notion_migration_formats_left.go): Notion's API gives neither a
// template's full width, small text and font nor a default set for one view
// only, which the reader reads (notion_migration_formats_templates.go).

// notionMigrationViewTemplateLeft is the left kind of a view whose own
// default template didn't come.
const notionMigrationViewTemplateLeft = "view_template"

type notionMigrationTemplateMade struct {
	NotionID string `db:"notion_id"`
	NativeID string `db:"native_id"`
	Title    string `db:"title"`
	SourceID string `db:"data_source_id"`
}

// notionMigrationTemplatesMade are the templates of the job's migration that
// are still its databases' templates, by the data source they belong to, and
// the template each Notion template came to. A run makes its templates last
// (notion_import_row_stamps.go), after its check against Notion and its
// report: until then a template the run read is still a row standing for one,
// one no earlier run made listed as such (pending, by its data source) — the
// report counts them the same way (notion_migration_report_counts.go). One
// in Leafen's trash is no database's template, read again or not.
type notionMigrationTemplatesMade struct {
	bySource map[string][]notionMigrationTemplateMade
	native   map[string]string
	// How many rows standing for templates each data source has, and which
	// Notion templates no earlier run made they stand for.
	pending    map[string]int
	pendingIDs map[string]bool
}

func loadNotionMigrationTemplatesMade(app core.App, jobID string) (notionMigrationTemplatesMade, error) {
	made := notionMigrationTemplatesMade{
		bySource: map[string][]notionMigrationTemplateMade{}, native: map[string]string{},
		pending: map[string]int{}, pendingIDs: map[string]bool{},
	}
	rows := []notionMigrationTemplateMade{}
	if err := app.DB().NewQuery(`
		SELECT [[ledger]].[[source_id]] AS [[notion_id]], [[ledger]].[[native_id]], [[template]].[[title]], [[template]].[[data_source_id]]
		FROM {{leafen_notion_migrations}} AS [[migration]]
		JOIN {{leafen_notion_migration_ledger}} AS [[ledger]] ON [[ledger]].[[migration_id]]=[[migration]].[[id]] AND [[ledger]].[[kind]]={:template}
		JOIN {{leafen_pages}} AS [[template]] ON [[template]].[[id]]=[[ledger]].[[native_id]] AND [[template]].[[database_template]]
			AND [[template]].[[in_trash]]=0
		WHERE [[migration]].[[import_job_id]]={:jobId} AND NOT EXISTS (
			SELECT 1 FROM {{leafen_database_toolbar_template_state}} AS [[state]]
			WHERE [[state]].[[template_id]]=[[ledger]].[[native_id]] AND [[state]].[[deleted_at]]!='')
		ORDER BY [[template]].[[created]], [[ledger]].[[source_id]]
	`).Bind(dbx.Params{"jobId": jobID, "template": notionMigrationLedgerTemplate}).All(&rows); err != nil {
		return made, err
	}
	for _, row := range rows {
		row.NotionID = canonicalNotionID(row.NotionID)
		made.bySource[row.SourceID] = append(made.bySource[row.SourceID], row)
		made.native[row.NotionID] = row.NativeID
	}
	// The rows standing for templates a run still makes; none once it ended.
	var status string
	if err := app.DB().NewQuery(`SELECT [[status]] FROM {{leafen_jobs}} WHERE [[id]]={:jobId}`).
		Bind(dbx.Params{"jobId": jobID}).Row(&status); err != nil || status == durableJobStatusCompleted {
		return made, ignoreNoRows(err)
	}
	pending := []struct {
		NotionID string `db:"notion_id"`
		NativeID string `db:"native_id"`
		Title    string `db:"title"`
		SourceID string `db:"data_source_id"`
		Update   string `db:"update"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[mapping]].[[source_id]] AS [[notion_id]], [[mapping]].[[native_id]], [[row]].[[title]], [[row]].[[data_source_id]],
			CASE WHEN json_valid([[source]].[[text]]) THEN COALESCE(json_extract([[source]].[[text]], {:update}), '') ELSE '' END AS [[update]]
		FROM {{leafen_notion_import_sources}} AS [[source]]
		JOIN {{leafen_notion_import_mappings}} AS [[mapping]] ON [[mapping]].[[job_id]]=[[source]].[[job_id]]
			AND [[mapping]].[[source_id]]=[[source]].[[source_id]] AND [[mapping]].[[kind]]='row'
		JOIN {{leafen_pages}} AS [[row]] ON [[row]].[[id]]=[[mapping]].[[native_id]] AND [[row]].[[in_trash]]=0
		WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[kind]]='row' AND instr([[source]].[[text]], {:template})>0
		ORDER BY [[source]].[[sequence]]
	`).Bind(dbx.Params{
		"jobId": jobID, "template": `"` + notionImportRowTemplateKey + `"`,
		"update": `$."` + notionImportRowTemplateKey + `".value.update`,
	}).All(&pending); err != nil {
		return made, err
	}
	for _, standing := range pending {
		row := notionMigrationTemplateMade{
			NotionID: standing.NotionID, NativeID: standing.NativeID, Title: standing.Title, SourceID: standing.SourceID,
		}
		row.NotionID = canonicalNotionID(row.NotionID)
		// A row standing for a template is no row, for a new template or
		// for one an earlier run made, which takes Notion's change from it
		// (2026-11-17: a run again that read a changed template counted 4
		// row pages of 3 rows).
		made.pending[row.SourceID]++
		// One an earlier run made is that template, listed above unless a
		// person moved it to the trash: then it is no more its database's
		// template than on a run that doesn't read it again (2026-11-17: a
		// template in the trash counted among the looks left on the run
		// that read Notion's change to it, and not on the next).
		if standing.Update != "" || made.native[row.NotionID] != "" {
			continue
		}
		made.bySource[row.SourceID] = append(made.bySource[row.SourceID], row)
		made.native[row.NotionID] = row.NativeID
		made.pendingIDs[row.NotionID] = true
	}
	return made, nil
}

// templateLeft says why a template's look is still at Leafen's defaults, or
// that it isn't: the reader hands in a template's look only when one is set,
// so one its database's reading left out has Notion's plain look, as Leafen's
// defaults.
func (matched notionMigrationFormatsMatched) templateLeft(notionID, databaseNotionID string) (string, bool) {
	switch reason := matched.items["page:"+canonicalNotionID(notionID)]; reason {
	case "matched":
		return "", true
	case "not_matched":
		return reason, false
	}
	if matched.items["database:"+canonicalNotionID(databaseNotionID)] != "" {
		return "", true
	}
	return "not_read", false
}

// viewTemplateLeft says why a view's own default template is still Leafen's
// default, or that it isn't: a view read with none of its own has none to
// match, and one with its own is matched once the view names the template
// the run made of it — or will, as the run makes that template last
// (finishNotionImportTemplate). A view no reading read could have one only
// where its database has templates.
func (matched notionMigrationFormatsMatched) viewTemplateLeft(
	notionID, viewDefault, dataSourceID string, templates notionMigrationTemplatesMade,
) (string, bool) {
	id := canonicalNotionID(notionID)
	reason, read := matched.items[notionMigrationViewTemplateLeft+":"+id]
	switch {
	case !read:
		return "not_read", len(templates.bySource[dataSourceID]) == 0
	case reason == "matched":
		return "", true
	}
	chosen := matched.viewTemplates[id]
	if templates.pendingIDs[chosen] && reason != notionMigrationViewUnsaved {
		return "", true
	}
	if native := templates.native[chosen]; native != "" && native == viewDefault {
		return "", true
	}
	return reason, false
}

// notionMigrationViewTakesTemplates reports whether a view makes its new
// pages from a template: not a form, a chart or a dashboard.
func notionMigrationViewTakesTemplates(viewType string) bool {
	switch viewType {
	case graphViewTypeForm, graphViewTypeChart, graphViewTypeDashboard:
		return false
	}
	return true
}
