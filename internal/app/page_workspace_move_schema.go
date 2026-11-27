package app

import (
	"errors"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Migration 857 keeps a page's move to another workspace on the same server
// (page_workspace_move.go): the job, what it carries (the pages under its
// page, their databases, rows, properties, views, templates, forms and the
// files only they show), the one change the file guard takes from it, and
// the index an object's kept API identity is found by.
// A file keeps its workspace for good (the guard trigger
// leafen_file_authority_update refuses any other change), save a file the
// move carries, between its two workspaces, while the move goes or goes back.

const (
	pageWorkspaceMovesTable     = "leafen_page_workspace_moves"
	pageWorkspaceMoveItemsTable = "leafen_page_workspace_move_items"
)

func ensureNativePageWorkspaceMovesSchema(app core.App) error {
	for _, statement := range []string{
		`CREATE TABLE {{leafen_page_workspace_moves}} (
			[[job_id]] TEXT PRIMARY KEY NOT NULL,
			[[actor_id]] TEXT NOT NULL,
			[[request_hash]] TEXT NOT NULL,
			[[root_page_id]] TEXT NOT NULL,
			[[root_parent_id]] TEXT NOT NULL DEFAULT '',
			[[source_workspace_id]] TEXT NOT NULL,
			[[source_owner_id]] TEXT NOT NULL,
			[[target_workspace_id]] TEXT NOT NULL,
			[[target_owner_id]] TEXT NOT NULL,
			[[destination_parent_id]] TEXT NOT NULL DEFAULT '',
			[[destination_teamspace_id]] TEXT NOT NULL DEFAULT '',
			[[phase]] TEXT NOT NULL CHECK ([[phase]] IN ('moving','rolling_back','completed','failed')),
			[[lane]] INTEGER NOT NULL DEFAULT 0,
			[[cursor]] TEXT NOT NULL DEFAULT '',
			[[revision]] INTEGER NOT NULL DEFAULT 0,
			[[page_count]] INTEGER NOT NULL DEFAULT 0,
			[[moved_rows]] INTEGER NOT NULL DEFAULT 0,
			[[plan_json]] TEXT NOT NULL DEFAULT '{}' CHECK (json_valid([[plan_json]])),
			[[cut_json]] TEXT NOT NULL DEFAULT '{}' CHECK (json_valid([[cut_json]])),
			[[error_code]] TEXT NOT NULL DEFAULT '',
			[[created_at]] INTEGER NOT NULL,
			[[updated_at]] INTEGER NOT NULL,
			[[completed_at]] INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX [[idx_leafen_page_workspace_moves_source]]
			ON {{leafen_page_workspace_moves}} ([[source_workspace_id]], [[phase]])`,
		`CREATE INDEX [[idx_leafen_page_workspace_moves_target]]
			ON {{leafen_page_workspace_moves}} ([[target_workspace_id]], [[phase]])`,
		`CREATE TABLE {{leafen_page_workspace_move_items}} (
			[[job_id]] TEXT NOT NULL,
			[[kind]] TEXT NOT NULL,
			[[item_id]] TEXT NOT NULL,
			PRIMARY KEY ([[job_id]], [[kind]], [[item_id]])
		) WITHOUT ROWID`,
		`CREATE INDEX [[idx_leafen_page_workspace_move_items_item]]
			ON {{leafen_page_workspace_move_items}} ([[item_id]], [[kind]], [[job_id]])`,
		// An object a move carried keeps its API identity, found by what it names
		// (notion_object_identity_moved.go).
		`CREATE INDEX [[idx_leafen_notion_object_identities_moved]]
			ON {{leafen_notion_object_identities}} ([[internal_id]], [[object_kind]], [[created]])`,
		`DROP TRIGGER [[leafen_file_authority_update]]`,
		pageWorkspaceMoveFileAuthorityTrigger,
	} {
		if _, err := app.DB().NewQuery(statement).Execute(); err != nil {
			return err
		}
	}
	return validateNativePageWorkspaceMovesSchema(app)
}

// pageWorkspaceMoveFileAuthorityTrigger is the file guard with the move's
// one change: a file a move under way carries, from one of its workspaces to
// the other.
const pageWorkspaceMoveFileAuthorityTrigger = "CREATE TRIGGER `leafen_file_authority_update`\n" +
	"\tBEFORE UPDATE ON `leafen_files`\n" +
	"\tWHEN (NEW.`workspace` != OLD.`workspace` AND NOT EXISTS (\n" +
	"\t\t\tSELECT 1 FROM `leafen_page_workspace_move_items` `item`\n" +
	"\t\t\tJOIN `leafen_page_workspace_moves` `move` ON `move`.`job_id` = `item`.`job_id`\n" +
	"\t\t\tWHERE `item`.`item_id` = OLD.`id` AND `item`.`kind` = 'file'\n" +
	"\t\t\t\tAND `move`.`phase` IN ('moving','rolling_back')\n" +
	"\t\t\t\tAND ((`move`.`source_workspace_id` = OLD.`workspace` AND `move`.`target_workspace_id` = NEW.`workspace`)\n" +
	"\t\t\t\t\tOR (`move`.`target_workspace_id` = OLD.`workspace` AND `move`.`source_workspace_id` = NEW.`workspace`))\n" +
	"\t\t))\n" +
	"\t\tOR NEW.`authority_state` != OLD.`authority_state`\n" +
	"\t\tOR NEW.`created_by` != OLD.`created_by`\n" +
	"\t\tOR NEW.`created_by` = ''\n" +
	"\t\tOR (NEW.`authority_state` = 'workspace' AND (\n" +
	"\t\t\tNEW.`workspace` = '' OR NOT EXISTS (\n" +
	"\t\t\t\tSELECT 1 FROM `leafen_workspaces` `workspace`\n" +
	"\t\t\t\tWHERE `workspace`.`id` = NEW.`workspace`\n" +
	"\t\t\t)\n" +
	"\t\t))\n" +
	"\t\tOR (NEW.`authority_state` = 'unscoped_legacy' AND NEW.`workspace` != '')\n" +
	"\t\tOR NEW.`authority_state` NOT IN ('workspace','unscoped_legacy')\n" +
	"\tBEGIN\n" +
	"\t\tSELECT RAISE(ABORT, 'LEAFEN_FILE_AUTHORITY_INVALID');\n" +
	"\tEND"

func validateNativePageWorkspaceMovesSchema(app core.App) error {
	var columns, items, indexes int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_page_workspace_moves')
		WHERE [[notnull]] = 1`).Row(&columns); err != nil {
		return err
	}
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_page_workspace_move_items')
		WHERE [[name]] IN ('job_id', 'kind', 'item_id') AND [[notnull]] = 1`).Row(&items); err != nil {
		return err
	}
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM [[sqlite_master]]
		WHERE [[type]] = 'index' AND [[name]] IN ('idx_leafen_page_workspace_moves_source',
			'idx_leafen_page_workspace_moves_target', 'idx_leafen_page_workspace_move_items_item',
			'idx_leafen_notion_object_identities_moved')`).Row(&indexes); err != nil {
		return err
	}
	guard := ""
	if err := app.DB().NewQuery(`SELECT [[sql]] FROM [[sqlite_master]]
		WHERE [[type]] = 'trigger' AND [[name]] = 'leafen_file_authority_update'`).Row(&guard); err != nil {
		return err
	}
	if columns != 23 || items != 3 || indexes != 4 || !strings.Contains(guard, "leafen_page_workspace_move_items") {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
