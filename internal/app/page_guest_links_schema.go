package app

import (
	"errors"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Migration 853 keeps guest invite links (page_guest_links.go) and lets an
// account say it came in by one: the kinds leafen_account_joins takes are
// SQLite's own check, so that table is made again with 'page_link' among
// them, every row it held copied over in the same transaction, and its
// index made again (the start keeps the data as it was before, in
// backups/migrations, schema_migration_recovery.go).

const nativePageGuestLinksChecksum = "5ddb1f877448e3585753acc6e3e61018c1904983dea76cd41d945243d49fb1f3"

func ensureNativePageGuestLinksSchema(app core.App) error {
	for _, statement := range []string{
		"CREATE TABLE `leafen_page_guest_links` (" +
			"`id` TEXT PRIMARY KEY NOT NULL, " +
			"`token_hash` TEXT NOT NULL UNIQUE, " +
			"`workspace_id` TEXT NOT NULL REFERENCES `leafen_workspaces` (`id`) ON DELETE CASCADE, " +
			"`created_by` TEXT NOT NULL, " +
			"`role` TEXT NOT NULL CHECK (`role` IN ('edit', 'comment', 'view')), " +
			"`page_ids` TEXT NOT NULL CHECK (json_valid(`page_ids`) AND json_type(`page_ids`) = 'array'), " +
			"`single_use` INTEGER NOT NULL DEFAULT 1 CHECK (`single_use` IN (0, 1)), " +
			"`uses` INTEGER NOT NULL DEFAULT 0 CHECK (`uses` >= 0), " +
			"`last_used_by` TEXT NOT NULL DEFAULT '', " +
			"`last_used_at` INTEGER NOT NULL DEFAULT 0, " +
			"`revoked_at` INTEGER NOT NULL DEFAULT 0, " +
			"`expires_at` INTEGER NOT NULL CHECK (`expires_at` > 0), " +
			"`created_at` INTEGER NOT NULL CHECK (`created_at` > 0))",
		"CREATE INDEX `idx_leafen_workspace_deletion_page_guest_links` ON `leafen_page_guest_links` (`workspace_id`)",
		"CREATE TABLE `leafen_account_joins_next` (" +
			"`user_id` TEXT PRIMARY KEY NOT NULL REFERENCES `users` (`id`) ON DELETE CASCADE, " +
			"`kind` TEXT NOT NULL CHECK (`kind` IN ('workspace_email', 'page_email', 'workspace_name', " +
			"'workspace_link', 'teamspace_link', 'allowed_domain', 'page_link')), " +
			"`workspace_id` TEXT NOT NULL REFERENCES `leafen_workspaces` (`id`) ON DELETE CASCADE, " +
			"`inviter_id` TEXT NOT NULL DEFAULT '', " +
			"`created_at` INTEGER NOT NULL CHECK (`created_at` > 0))",
		"INSERT INTO `leafen_account_joins_next` (`user_id`, `kind`, `workspace_id`, `inviter_id`, `created_at`) " +
			"SELECT `user_id`, `kind`, `workspace_id`, `inviter_id`, `created_at` FROM `leafen_account_joins`",
		"DROP TABLE `leafen_account_joins`",
		"ALTER TABLE `leafen_account_joins_next` RENAME TO `leafen_account_joins`",
		"CREATE INDEX `idx_leafen_account_joins_workspace` ON `leafen_account_joins` (`workspace_id`)",
	} {
		if _, err := app.DB().NewQuery(statement).Execute(); err != nil {
			return err
		}
	}
	return validateNativePageGuestLinksSchema(app)
}

func validateNativePageGuestLinksSchema(app core.App) error {
	var links, joins string
	var indexes int
	if err := app.DB().NewQuery("SELECT COALESCE((SELECT `sql` FROM `sqlite_master` WHERE `type` = 'table' AND `name` = 'leafen_page_guest_links'), '')").
		Row(&links); err != nil {
		return err
	}
	if err := app.DB().NewQuery("SELECT COALESCE((SELECT `sql` FROM `sqlite_master` WHERE `type` = 'table' AND `name` = 'leafen_account_joins'), '')").
		Row(&joins); err != nil {
		return err
	}
	if err := app.DB().NewQuery("SELECT COUNT(*) FROM `sqlite_master` WHERE `type` = 'index' AND `name` IN " +
		"('idx_leafen_workspace_deletion_page_guest_links', 'idx_leafen_account_joins_workspace')").Row(&indexes); err != nil {
		return err
	}
	if !strings.Contains(links, "`token_hash`") || !strings.Contains(joins, "'page_link'") || indexes != 2 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
