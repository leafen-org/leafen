package app

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A Notion move makes a database's template of a Notion template it read as
// a row, its body copied from the row's (notion_import_templates.go), and a
// page made from the template copies the template's body, block IDs and all
// ("<template>:<block>" in the template, "<page>:<template>:<block>" in the
// page). Migrations 843 and 844 took away the names a move made before
// 2026-11-21 gave what Notion never named — a linked database's "Untitled",
// a database's "New database" or "New data source" — from what the move's
// records still name, and a template's copy is named by none: the templates
// kept them, and so did each page made from one since (seen 2026-11-22: a
// template's four linked databases read "Untitled" in fetch, and a page an
// agent made from it read so too, while the rows Notion had made from it,
// moved, read unnamed). Migration 851 takes those names away from the
// templates a migration made and the pages made from them (a page made from
// a template holds a block whose ID names it), where nobody changed them
// since: written no later than a minute after the copy was made (a body's
// databases are copied, and named, by a job after it), or, a template's, no
// later than its move's last turn. A database the move's scan read as named
// that very name in Notion keeps it, as 844 kept one so. The move makes neither name today (the link phase names a linked
// database it reads as "Untitled" none, and the reading of a database never
// named takes no made-up name), so a template made now copies none.

// movedTemplateCopySettles is how long after a body's copy — a template's,
// a page's made from one — it goes on being written (its databases are
// copied, and named, by a job after it): a name changed later was changed by
// somebody.
const movedTemplateCopySettles = int64(60_000)

// movedTemplate is a template a Notion migration made, with its migration
// and when its move's last turn was.
type movedTemplate struct {
	ID          string `db:"id"`
	MigrationID string `db:"migration_id"`
	LastTurn    int64  `db:"last_turn"`
}

// clearMovedTemplateNames is migration 851's content.
func clearMovedTemplateNames(app core.App) error {
	templates := []movedTemplate{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[ledger]].[[native_id]] AS [[id]], [[ledger]].[[migration_id]], [[job]].[[updated]] AS [[last_turn]]
		FROM {{leafen_notion_migration_ledger}} AS [[ledger]]
		JOIN {{leafen_notion_migrations}} AS [[migration]] ON [[migration]].[[id]] = [[ledger]].[[migration_id]]
		JOIN {{leafen_jobs}} AS [[job]] ON [[job]].[[id]] = [[migration]].[[import_job_id]]
		WHERE [[ledger]].[[kind]] = {:template} AND [[ledger]].[[native_id]] <> ''
		ORDER BY [[ledger]].[[native_id]]
	`).Bind(dbx.Params{"template": notionMigrationLedgerTemplate}).All(&templates); err != nil {
		return err
	}
	views, sources := 0, 0
	for _, template := range templates {
		cleared, err := clearMovedTemplateLinkedViewNames(app, template)
		if err != nil {
			return fmt.Errorf("template %s's linked databases: %w", template.ID, err)
		}
		views += cleared
		if cleared, err = clearMovedTemplateSourceNames(app, template); err != nil {
			return fmt.Errorf("template %s's databases: %w", template.ID, err)
		}
		sources += cleared
	}
	if views+sources > 0 {
		app.Logger().Info("templates a Notion move made, and pages made from them, left unnamed", "linkedDatabases", views, "sources", sources)
	}
	return nil
}

// clearMovedTemplateLinkedViewNames takes "Untitled" away from the linked
// databases of a template's body and of the pages made from it.
func clearMovedTemplateLinkedViewNames(app core.App, template movedTemplate) (int, error) {
	views := []movedUntitledLinkedView{}
	if err := app.DB().NewQuery(`
		SELECT [[block]].[[id]], [[block]].[[owner]], [[block]].[[workspace_id]], [[block]].[[page_id]],
			[[block]].[[parent_id]], [[block]].[[position]], [[block]].[[content]], [[block]].[[last_edited_by]], [[block]].[[updated]]
		FROM {{leafen_blocks}} AS [[block]]
		WHERE [[block]].[[type]] = 'inline_database' AND instr([[block]].[[id]], {:prefix}) > 0 AND json_valid([[block]].[[content]])
			AND json_extract([[block]].[[content]], '$.linkedDatabaseSource') = 1
			AND json_extract([[block]].[[content]], '$.linkedTitle') = {:untitled}
			AND json_type([[block]].[[content]], '$.linkedTitleSpans') IS NULL
			AND ([[block]].[[updated]] <= [[block]].[[created]] + {:settle}
				OR ([[block]].[[page_id]] = {:template} AND [[block]].[[updated]] <= {:lastTurn}))
		ORDER BY [[block]].[[page_id]], [[block]].[[id]]
	`).Bind(dbx.Params{
		"prefix": template.ID + ":", "template": template.ID, "lastTurn": template.LastTurn, "settle": movedTemplateCopySettles,
		"untitled": notionUnnamedLinkedDatabaseTitle,
	}).All(&views); err != nil {
		return 0, err
	}
	for start := 0; start < len(views); {
		end := start + 1
		for end < len(views) && views[end].PageID == views[start].PageID {
			end++
		}
		if err := clearMovedUntitledLinkedViewNamesOnPage(app, views[start:end]); err != nil {
			return 0, fmt.Errorf("page %s: %w", views[start].PageID, err)
		}
		start = end
	}
	return len(views), nil
}

// clearMovedTemplateSourceNames takes Notion's made-up name away from the
// databases of a template's body and of the pages made from it, unless the
// move's scan read a database named so.
func clearMovedTemplateSourceNames(app core.App, template movedTemplate) (int, error) {
	sources := []movedDefaultNamedSource{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[source]].[[id]], [[source]].[[owner]], [[source]].[[workspace_id]], [[source]].[[page_id]],
			[[source]].[[title]], [[source]].[[updated]], '' AS [[source_id]], {:migrationId} AS [[migration_id]]
		FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[title]] IN ({:database}, {:other}) AND [[source]].[[title_spans]] = ''
			AND ([[source]].[[page_id]] = {:template} OR [[source]].[[page_id]] IN (
				SELECT [[made]].[[page_id]] FROM {{leafen_blocks}} AS [[made]] WHERE instr([[made]].[[id]], {:prefix}) > 0))
			AND ([[source]].[[updated]] <= [[source]].[[created]] + {:settle}
				OR ([[source]].[[page_id]] = {:template} AND [[source]].[[updated]] <= {:lastTurn}))
			AND NOT EXISTS (SELECT 1 FROM {{leafen_notion_migration_scan_items}} AS [[scanned]]
				WHERE [[scanned]].[[migration_id]] = {:migrationId} AND [[scanned]].[[kind]] = {:scannedKind}
					AND json_valid([[scanned]].[[value]]) AND json_extract([[scanned]].[[value]], '$.title') = [[source]].[[title]])
		ORDER BY [[source]].[[page_id]], [[source]].[[id]]
	`).Bind(dbx.Params{
		"migrationId": template.MigrationID, "prefix": template.ID + ":", "settle": movedTemplateCopySettles,
		"database": notionMovedDefaultSourceNames[0], "other": notionMovedDefaultSourceNames[1],
		"template": template.ID, "lastTurn": template.LastTurn, "scannedKind": notionMigrationItemDatabase,
	}).All(&sources); err != nil {
		return 0, err
	}
	for start := 0; start < len(sources); {
		end := start + 1
		for end < len(sources) && sources[end].PageID == sources[start].PageID {
			end++
		}
		if err := clearMovedDefaultSourceNamesOnPage(app, sources[start:end]); err != nil {
			return 0, fmt.Errorf("page %s: %w", sources[start].PageID, err)
		}
		start = end
	}
	return len(sources), nil
}

// validateMovedTemplateNames is its check: the tables it reads and writes
// are there.
func validateMovedTemplateNames(app core.App) error {
	for _, table := range []string{
		"leafen_blocks", "leafen_data_sources", "leafen_database_containers", "leafen_notion_migration_ledger",
		"leafen_notion_migrations", "leafen_jobs", "leafen_notion_migration_scan_items", "leafen_ygo_structural_documents",
	} {
		if !app.HasTable(table) {
			return fmt.Errorf("templates moved unnamed: the table %s is missing", table)
		}
	}
	return nil
}
