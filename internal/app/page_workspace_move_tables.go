package app

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// What a page's move to another workspace does to each table that keeps
// rows by workspace, read against the schema at start as the deletion
// inventories are (deletion_inventories_start.go): a table with a workspace
// column the list doesn't name stops a development build's start, and the
// move refuses until an update names it, so no table's rows stay behind in
// the old workspace unsaid.
//
// A row belongs to the moved pages by its key: a page under the moved one
// (its databases' pages, their rows' pages and the pages under those
// included), one of their data sources, rows, properties, views or
// templates, a form of theirs, or a file only they show. Such a row moves
// (its workspace, and its storage owner where the two workspaces' owners
// differ, changes; its ID stays), is cut (deleted once the move is done: the
// old workspace's sharing, links and access, which the user's decision of
// 2026-11-23 cuts), or is shown again (search: the moved pages are indexed
// again in their new workspace). A favorite of a moved page stays its
// person's where that person is a member of the new workspace and goes
// otherwise. Everything else stays where it is, and says why.

const (
	pageWorkspaceMoveRuleMove     = "move"
	pageWorkspaceMoveRuleCut      = "cut"
	pageWorkspaceMoveRuleReindex  = "reindex"
	pageWorkspaceMoveRuleFavorite = "favorite"
	pageWorkspaceMoveRuleKept     = "kept"
)

// The kinds of what the move carries (leafen_page_workspace_move_items).
const (
	pageWorkspaceMoveItemPage     = "page"
	pageWorkspaceMoveItemSource   = "source"
	pageWorkspaceMoveItemRow      = "row"
	pageWorkspaceMoveItemProperty = "property"
	pageWorkspaceMoveItemView     = "view"
	pageWorkspaceMoveItemTemplate = "template"
	pageWorkspaceMoveItemForm     = "form"
	pageWorkspaceMoveItemFile     = "file"
	// pageWorkspaceMoveItemAny is a key naming any of them.
	pageWorkspaceMoveItemAny = ""
)

type pageWorkspaceMoveTable struct {
	Table string
	// Workspace is the column naming the row's workspace; "" for a table
	// keyed by its page and storage owner alone.
	Workspace string
	// Owners are the columns naming the storage owner (a workspace's owner
	// field), rewritten when the two workspaces' owners differ.
	Owners []string
	// Organization is the column naming the workspace's organization, if any.
	Organization string
	// Key names what the row belongs to, of the item kind Kind.
	Key  string
	Kind string
	Rule string
	// Principal is a favorite's person (pageWorkspaceMoveRuleFavorite), and
	// Favorite the condition that makes a row one the plan counts ("" for a
	// person's visit, which it doesn't).
	Principal string
	Favorite  string
	Why       string
}

func movedTable(table, key, kind string, owners ...string) pageWorkspaceMoveTable {
	return pageWorkspaceMoveTable{Table: table, Workspace: "workspace_id", Owners: owners, Key: key, Kind: kind, Rule: pageWorkspaceMoveRuleMove}
}

func cutTable(table, key, kind, why string) pageWorkspaceMoveTable {
	return pageWorkspaceMoveTable{Table: table, Workspace: "workspace_id", Key: key, Kind: kind, Rule: pageWorkspaceMoveRuleCut, Why: why}
}

func favoriteTable(table, key, kind, principal, favorite string) pageWorkspaceMoveTable {
	return pageWorkspaceMoveTable{Table: table, Workspace: "workspace_id", Key: key, Kind: kind, Principal: principal, Favorite: favorite, Rule: pageWorkspaceMoveRuleFavorite}
}

func keptTable(table, why string) pageWorkspaceMoveTable {
	return pageWorkspaceMoveTable{Table: table, Workspace: "workspace_id", Rule: pageWorkspaceMoveRuleKept, Why: why}
}

const (
	keptWorkspaceOwn    = "the workspace's own: its people, settings, connections, policies and counters"
	keptReceipt         = "a receipt or log of something done in the old workspace"
	keptOtherJob        = "another operation's job; one under way on the moved pages refuses the move"
	keptInbox           = "a person's inbox in the old workspace; an item opens the page wherever it is, for whoever may"
	keptNotionMigration = "a Notion migration's record into the old workspace"
	keptExpiring        = "a short-lived answer that expires and is asked again"
)

var pageWorkspaceMoveTables = []pageWorkspaceMoveTable{
	cutTable("leafen_access_requests", "page_id", pageWorkspaceMoveItemPage, "requests to the old workspace's people for access"),
	keptTable("leafen_account_joins", keptWorkspaceOwn),
	keptTable("leafen_app_workspaces", keptWorkspaceOwn),
	keptTable("leafen_block_move_jobs", keptOtherJob),
	movedTable("leafen_block_page_references", "source_page_id", pageWorkspaceMoveItemPage),
	keptTable("leafen_block_revision_holds", keptWorkspaceOwn),
	movedTable("leafen_block_trash_tombstones", "page_id", pageWorkspaceMoveItemPage, "owner_id"),
	movedTable("leafen_blocks", "page_id", pageWorkspaceMoveItemPage, "owner"),
	keptTable("leafen_bootstrap_visibility_projection_jobs", "a person's start projection, made again from the pages as they are"),
	keptTable("leafen_bootstrap_workspace_visibility", keptWorkspaceOwn),
	movedTable("leafen_child_page_block_backfill", "parent_page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_child_page_block_places", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_comment_deletion_jobs", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_comment_receipts", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_comment_tombstones", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_comments", "page_id", pageWorkspaceMoveItemPage),
	keptTable("leafen_custom_emoji_tombstones", keptWorkspaceOwn),
	keptTable("leafen_custom_emojis", "the old workspace's emoji; the moved pages name them as they did (the plan counts them)"),
	movedTable("leafen_data_source_trash_in_place", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_data_sources", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_analytics_daily", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_database_analytics_opt_outs", "page_id", pageWorkspaceMoveItemPage),
	cutTable("leafen_database_analytics_recent_viewers", "page_id", pageWorkspaceMoveItemPage, "who of the old workspace looked at it lately"),
	movedTable("leafen_database_analytics_unique_viewers", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_database_automation_definitions", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_automation_deliveries", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_automation_events", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_automation_frontiers", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_automation_mutations", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_database_automation_notifications", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_automation_outcomes", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_automation_schedule_occurrences", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_container_sources", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_container_views", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_containers", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_dependency_bindings", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_dependency_edges", "page_id", pageWorkspaceMoveItemPage, "owner"),
	favoriteTable("leafen_database_favorites", "page_id", pageWorkspaceMoveItemPage, "principal_id", "[[favorite]] = 1"),
	movedTable("leafen_database_mutation_jobs", "page_id", pageWorkspaceMoveItemPage, "owner_id"),
	movedTable("leafen_database_notification_preferences", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_database_page_layouts", "data_source_id", pageWorkspaceMoveItemSource),
	movedTable("leafen_database_page_rules", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_page_signposts", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_database_properties", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_property_access", "data_source_id", pageWorkspaceMoveItemSource),
	cutTable("leafen_database_property_access_exceptions", "data_source_id", pageWorkspaceMoveItemSource, "the old workspace's people's and groups' own access to a property"),
	movedTable("leafen_database_property_conversions", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_property_trash", "property_id", pageWorkspaceMoveItemProperty, "owner"),
	movedTable("leafen_database_query_jobs", "page_id", pageWorkspaceMoveItemPage, "content_owner_id"),
	movedTable("leafen_database_relation_items", "page_id", pageWorkspaceMoveItemPage, "owner"),
	keptTable("leafen_database_relation_pair_work", "a queue of the workspace's relation pairings, by workspace and owner"),
	movedTable("leafen_database_relation_property_targets", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_row_notification_fanouts", "container_page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_database_row_pages", "container_page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_row_trash", "row_id", pageWorkspaceMoveItemRow, "owner"),
	movedTable("leafen_database_row_values", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_rows", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_sprints", "page_id", pageWorkspaceMoveItemPage, "owner"),
	keptTable("leafen_database_sql_query_jobs", keptExpiring),
	movedTable("leafen_database_task_features", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_templates", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_toolbar_mutation_receipts", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_database_toolbar_template_state", "template_id", pageWorkspaceMoveItemTemplate),
	movedTable("leafen_database_unique_id_allocators", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_view_property_refs", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_database_views", "page_id", pageWorkspaceMoveItemPage, "owner"),
	movedTable("leafen_deadline_acknowledgements", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_deadline_notices", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_deleted_databases", "page_id", pageWorkspaceMoveItemPage, "owner"),
	keptTable("leafen_file_delete_receipts", keptReceipt),
	keptTable("leafen_file_maintenance_outcomes", keptWorkspaceOwn),
	keptTable("leafen_file_maintenance_runs", keptWorkspaceOwn),
	keptTable("leafen_file_manual_cleanup_jobs", keptWorkspaceOwn),
	movedTable("leafen_file_reference_cleanups", "file_id", pageWorkspaceMoveItemFile),
	movedTable("leafen_file_references", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_file_text_extracts", "file_id", pageWorkspaceMoveItemFile),
	movedTable("leafen_file_uploads", "file_id", pageWorkspaceMoveItemFile),
	keptTable("leafen_file_usage_report_jobs", keptWorkspaceOwn),
	keptTable("leafen_file_usage_report_workspace_results", keptWorkspaceOwn),
	{Table: "leafen_files", Workspace: "workspace", Key: "id", Kind: pageWorkspaceMoveItemFile, Rule: pageWorkspaceMoveRuleMove},
	favoriteTable("leafen_form_favorites", "form_link_id", pageWorkspaceMoveItemForm, "owner_id", "TRUE"),
	cutTable("leafen_form_links", "page_id", pageWorkspaceMoveItemPage, "a form's link, which shares it; it is made again when the form is shared"),
	keptTable("leafen_form_option_frontiers", keptWorkspaceOwn),
	keptTable("leafen_imported_people_mapping_jobs", keptOtherJob),
	keptTable("leafen_integration_accounts", keptWorkspaceOwn),
	movedTable("leafen_interactive_automation_executions", "page_id", pageWorkspaceMoveItemPage),
	keptTable("leafen_jobs", "the job queue; each job reads its page by its ID, and one under way on the moved pages refuses the move"),
	keptTable("leafen_mcp_connection_workspaces", keptWorkspaceOwn),
	keptTable("leafen_mcp_enterprise_member_access", keptWorkspaceOwn),
	keptTable("leafen_mcp_inbox_shown", keptInbox),
	keptTable("leafen_mcp_oauth_authorization_workspaces", keptWorkspaceOwn),
	keptTable("leafen_my_task_sources", "a person's 'My tasks' in the old workspace; a moved database drops out of it there"),
	keptTable("leafen_my_task_views", "a person's 'My tasks' in the old workspace"),
	keptTable("leafen_notification_archive_receipts", keptReceipt),
	keptTable("leafen_notification_deliveries", keptInbox),
	keptTable("leafen_notification_emails", keptInbox),
	keptTable("leafen_notification_events", keptInbox),
	keptTable("leafen_notification_read_settlements", keptReceipt),
	keptTable("leafen_notifications", keptInbox),
	keptTable("leafen_notion_agent_revisions", keptWorkspaceOwn),
	keptTable("leafen_notion_agent_settings", keptWorkspaceOwn),
	keptTable("leafen_notion_agents", keptWorkspaceOwn),
	keptTable("leafen_notion_async_tasks", keptExpiring),
	keptTable("leafen_notion_credit_access_assignments", keptWorkspaceOwn),
	keptTable("leafen_notion_credit_access_policies", keptWorkspaceOwn),
	keptTable("leafen_notion_credit_access_revisions", keptWorkspaceOwn),
	keptTable("leafen_notion_import_connections", keptWorkspaceOwn),
	movedTable("leafen_notion_import_kept_mappings", "native_id", pageWorkspaceMoveItemAny),
	movedTable("leafen_notion_imported_authors", "page_id", pageWorkspaceMoveItemPage),
	keptTable("leafen_notion_imported_people", "the old workspace's list of people a Notion import named; a moved value reads its name by the rule a copy's does"),
	keptTable("leafen_notion_internal_connection_owners", keptWorkspaceOwn),
	keptTable("leafen_notion_internal_connections", keptWorkspaceOwn),
	keptTable("leafen_notion_migration_formats", keptNotionMigration),
	keptTable("leafen_notion_migration_issues", keptNotionMigration),
	keptTable("leafen_notion_migration_ledger", keptNotionMigration),
	keptTable("leafen_notion_migration_scan_items", keptNotionMigration),
	keptTable("leafen_notion_migrations", keptNotionMigration),
	keptTable("leafen_notion_oauth_grants", keptWorkspaceOwn),
	keptTable("leafen_notion_object_identities", "the API identities made from the old workspace's ID, which a moved object keeps answering with (notion_object_identity_moved.go)"),
	keptTable("leafen_notion_personal_access_token_revisions", keptWorkspaceOwn),
	keptTable("leafen_notion_personal_access_tokens", keptWorkspaceOwn),
	cutTable("leafen_notion_view_queries", "view_id", pageWorkspaceMoveItemView, keptExpiring),
	keptTable("leafen_organization_audit_events", keptReceipt),
	keptTable("leafen_organization_guest_mfa_workspaces", keptWorkspaceOwn),
	keptTable("leafen_organization_mcp_approved_clients", keptWorkspaceOwn),
	keptTable("leafen_organization_mcp_workspace_policies", keptWorkspaceOwn),
	keptTable("leafen_organization_storage_reservations", keptWorkspaceOwn),
	keptTable("leafen_page_analytics_batches", keptReceipt),
	movedTable("leafen_page_analytics_daily", "page_id", pageWorkspaceMoveItemPage),
	keptTable("leafen_page_analytics_member_preferences", keptWorkspaceOwn),
	movedTable("leafen_page_analytics_page_opt_outs", "page_id", pageWorkspaceMoveItemPage),
	cutTable("leafen_page_analytics_recent_viewers", "page_id", pageWorkspaceMoveItemPage, "who of the old workspace looked at it lately"),
	movedTable("leafen_page_analytics_unique_viewers", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_page_deletion_holds", "page_id", pageWorkspaceMoveItemPage),
	keptTable("leafen_page_duplication_jobs", keptOtherJob),
	favoriteTable("leafen_page_favorites", "page_id", pageWorkspaceMoveItemPage, "principal_id", "[[favorite]] = 1"),
	keptTable("leafen_page_guest_links", "a guest invite link of the old workspace; the moved pages leave its list (the plan counts the links)"),
	movedTable("leafen_page_history_block_spans", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_page_history_checkpoints", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_page_history_events", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_page_history_restore_receipts", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_page_history_snapshot_jobs", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_page_history_text_spans", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_page_history_versions", "page_id", pageWorkspaceMoveItemPage),
	keptTable("leafen_page_interaction_revisions", keptWorkspaceOwn),
	{Table: "leafen_page_interactions", Workspace: "workspace_id", Organization: "organization_id", Key: "page_id", Kind: pageWorkspaceMoveItemPage, Rule: pageWorkspaceMoveRuleMove},
	cutTable("leafen_page_invite_emails", "page_id", pageWorkspaceMoveItemPage, "an invite to a share the move ends"),
	keptTable("leafen_page_link_visitors", keptReceipt),
	keptTable("leafen_page_list_changes", "the sidebar's change feed, which the pages' own triggers write for both workspaces"),
	movedTable("leafen_page_lock_unlocks", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_page_notification_preferences", "page_id", pageWorkspaceMoveItemPage),
	keptTable("leafen_page_permanent_delete_jobs", keptOtherJob),
	cutTable("leafen_page_permissions", "page_id", pageWorkspaceMoveItemPage, "the old workspace's shares, which the move ends"),
	movedTable("leafen_page_public_identities", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_page_reactions", "page_id", pageWorkspaceMoveItemPage),
	cutTable("leafen_page_share_access_pending", "page_id", pageWorkspaceMoveItemPage, "a share's first opening, of a share the move ends"),
	cutTable("leafen_page_share_access_receipts", "page_id", pageWorkspaceMoveItemPage, "a share's first opening, of a share the move ends"),
	cutTable("leafen_page_share_unlinks", "page_id", pageWorkspaceMoveItemPage, "a page's own sharing apart from its parent's in the old workspace; it takes its new parent's"),
	cutTable("leafen_page_teamspace_roles", "page_id", pageWorkspaceMoveItemPage, "the roles an old workspace's teamspace gave on it"),
	favoriteTable("leafen_page_visits", "page_id", pageWorkspaceMoveItemPage, "owner_id", ""),
	{Table: "leafen_pages", Workspace: "workspace", Owners: []string{"owner"}, Key: "id", Kind: pageWorkspaceMoveItemPage, Rule: pageWorkspaceMoveRuleMove},
	keptTable("leafen_people_database_moves", keptWorkspaceOwn),
	keptTable("leafen_people_guest_checks", keptWorkspaceOwn),
	keptTable("leafen_people_guests", keptWorkspaceOwn),
	keptTable("leafen_people_value_invitees", keptWorkspaceOwn),
	cutTable("leafen_public_mutation_effects", "page_id", pageWorkspaceMoveItemPage, "publishing of the old workspace's public links, which end"),
	cutTable("leafen_public_mutation_operations", "page_id", pageWorkspaceMoveItemPage, "publishing of the old workspace's public links, which end"),
	cutTable("leafen_public_mutation_states", "page_id", pageWorkspaceMoveItemPage, "publishing of the old workspace's public links, which end"),
	cutTable("leafen_public_share_links", "root_page_id", pageWorkspaceMoveItemPage, "a public link, which the move ends"),
	cutTable("leafen_public_sites", "root_page_id", pageWorkspaceMoveItemPage, "a published site, which the move ends"),
	keptTable("leafen_read_log_places", keptReceipt),
	keptTable("leafen_read_log_readers", keptWorkspaceOwn),
	movedTable("leafen_reminders", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_row_page_analytics_daily", "row_id", pageWorkspaceMoveItemRow),
	cutTable("leafen_row_page_analytics_recent_viewers", "row_id", pageWorkspaceMoveItemRow, "who of the old workspace looked at it lately"),
	movedTable("leafen_row_page_analytics_unique_viewers", "row_id", pageWorkspaceMoveItemRow),
	movedTable("leafen_row_page_content_cleanup", "page_id", pageWorkspaceMoveItemPage),
	{Table: "leafen_search_chunks", Workspace: "workspace_id", Key: "page_id", Kind: pageWorkspaceMoveItemPage, Rule: pageWorkspaceMoveRuleReindex},
	{Table: "leafen_search_dirty", Workspace: "workspace_id", Key: "page_id", Kind: pageWorkspaceMoveItemPage, Rule: pageWorkspaceMoveRuleReindex},
	{Table: "leafen_search_pages", Workspace: "workspace_id", Key: "page_id", Kind: pageWorkspaceMoveItemPage, Rule: pageWorkspaceMoveRuleReindex},
	{Table: "leafen_search_pages_fts", Workspace: "workspace_id", Key: "page_id", Kind: pageWorkspaceMoveItemPage, Rule: pageWorkspaceMoveRuleReindex},
	keptTable("leafen_search_workspace_state", keptWorkspaceOwn),
	keptTable("leafen_server_admin_workspace_events", keptReceipt),
	keptTable("leafen_sidebar_layouts", "a person's sidebar in the old workspace, which leaves out pages no longer there"),
	keptTable("leafen_slack_notification_deliveries", keptInbox),
	keptTable("leafen_teamspace_invite_links", keptWorkspaceOwn),
	keptTable("leafen_teamspace_join_requests", keptWorkspaceOwn),
	keptTable("leafen_teamspace_members", keptWorkspaceOwn),
	keptTable("leafen_teamspace_mutation_receipts", keptReceipt),
	keptTable("leafen_teamspace_publication_jobs", keptWorkspaceOwn),
	keptTable("leafen_teamspace_settings", keptWorkspaceOwn),
	keptTable("leafen_teamspaces", keptWorkspaceOwn),
	keptTable("leafen_trash_empty_jobs", keptOtherJob),
	movedTable("leafen_unavailable_database_blocks", "page_id", pageWorkspaceMoveItemPage),
	cutTable("leafen_view_links", "page_id", pageWorkspaceMoveItemPage, "a view's embed link, which shares it outside"),
	keptTable("leafen_web_push_deliveries", keptInbox),
	keptTable("leafen_webhook_events", "what the old workspace's subscriptions were told"),
	keptTable("leafen_webhook_subscriptions", keptWorkspaceOwn),
	keptTable("leafen_wiki_mutations", keptOtherJob),
	movedTable("leafen_wiki_verification_email_deliveries", "page_id", pageWorkspaceMoveItemPage),
	movedTable("leafen_wiki_verification_queue", "page_id", pageWorkspaceMoveItemPage),
	{Table: "leafen_workspace_activity", Workspace: "workspace_id", Organization: "organization_id", Key: "page_id", Kind: pageWorkspaceMoveItemPage, Rule: pageWorkspaceMoveRuleMove},
	keptTable("leafen_workspace_allowed_email_domains", keptWorkspaceOwn),
	movedTable("leafen_workspace_carried_people", "page_id", pageWorkspaceMoveItemPage),
	keptTable("leafen_workspace_analytics_member_daily", keptWorkspaceOwn),
	keptTable("leafen_workspace_archive_import_targets", keptWorkspaceOwn),
	keptTable("leafen_workspace_connection_policies", keptWorkspaceOwn),
	keptTable("leafen_workspace_content_revisions", "the workspace's counters, which the pages' own triggers carry from one to the other"),
	keptTable("leafen_workspace_creation_receipts", keptReceipt),
	keptTable("leafen_workspace_deletion_jobs", keptReceipt),
	keptTable("leafen_workspace_export_artifacts", keptReceipt),
	{Table: "leafen_workspace_export_page_counts", Workspace: "workspace_id", Owners: []string{"owner"}, Key: "page_id", Kind: pageWorkspaceMoveItemPage, Rule: pageWorkspaceMoveRuleMove},
	keptTable("leafen_workspace_import_replace_leases", keptOtherJob),
	movedTable("leafen_workspace_import_replace_snapshots", "page_id", pageWorkspaceMoveItemPage, "owner"),
	keptTable("leafen_workspace_invitations", keptWorkspaceOwn),
	keptTable("leafen_workspace_invite_links", keptWorkspaceOwn),
	keptTable("leafen_workspace_membership_notifications", keptWorkspaceOwn),
	keptTable("leafen_workspace_memberships", keptWorkspaceOwn),
	keptTable("leafen_workspace_move_in_slots", keptWorkspaceOwn),
	keptTable("leafen_workspace_move_out_codes", keptWorkspaceOwn),
	keptTable("leafen_workspace_moved_people", keptWorkspaceOwn),
	keptTable("leafen_workspace_moves_out", keptWorkspaceOwn),
	keptTable("leafen_workspace_name_invitations", keptWorkspaceOwn),
	keptTable("leafen_workspace_owner_transfer_notifications", keptWorkspaceOwn),
	keptTable("leafen_workspace_people_databases", "the workspace's People database, which a move doesn't take"),
	keptTable("leafen_workspace_people_rows", "the workspace's People database, which a move doesn't take"),
	keptTable("leafen_workspace_permission_group_revisions", keptWorkspaceOwn),
	keptTable("leafen_workspace_permission_groups", keptWorkspaceOwn),
	keptTable("leafen_workspace_permission_state", keptWorkspaceOwn),
	keptTable("leafen_workspace_person_profiles", keptWorkspaceOwn),
	keptTable("leafen_workspace_restricted_members", keptWorkspaceOwn),
	keptTable("leafen_workspace_search_daily", keptWorkspaceOwn),
	keptTable("leafen_workspace_settings_receipts", keptReceipt),
	keptTable("leafen_workspace_settings_state", keptWorkspaceOwn),
	keptTable("leafen_workspace_sidebar_presets", keptWorkspaceOwn),
}

// pageWorkspaceMoveOwnerTables keep a page's changes by its page and its
// storage owner alone, with no workspace column: their owner is rewritten
// when the two workspaces' owners differ, as the page's is.
var pageWorkspaceMoveOwnerTables = []pageWorkspaceMoveTable{
	{Table: "leafen_graph_changes", Owners: []string{"owner"}, Key: "page_id", Kind: pageWorkspaceMoveItemPage, Rule: pageWorkspaceMoveRuleMove},
	{Table: "leafen_graph_mutations", Owners: []string{"owner"}, Key: "page_id", Kind: pageWorkspaceMoveItemPage, Rule: pageWorkspaceMoveRuleMove},
	{Table: "leafen_mutation_receipts", Owners: []string{"owner"}, Key: "page", Kind: pageWorkspaceMoveItemPage, Rule: pageWorkspaceMoveRuleMove},
}

// pageWorkspaceMoveLanes are the tables a move goes through a window at a
// time, in order: what it carries, then the owner tables, then what is shown
// again — the search index first, whose rows its chunks and pages name; what
// it cuts and favorites are done in its last step.
var pageWorkspaceMoveLanes = func() []pageWorkspaceMoveTable {
	lanes, reindexed := []pageWorkspaceMoveTable{}, []pageWorkspaceMoveTable{}
	for _, table := range pageWorkspaceMoveTables {
		switch {
		case table.Rule == pageWorkspaceMoveRuleMove:
			lanes = append(lanes, table)
		case table.Rule == pageWorkspaceMoveRuleReindex && table.Table == "leafen_search_pages_fts":
			reindexed = append([]pageWorkspaceMoveTable{table}, reindexed...)
		case table.Rule == pageWorkspaceMoveRuleReindex:
			reindexed = append(reindexed, table)
		}
	}
	lanes = append(lanes, pageWorkspaceMoveOwnerTables...)
	return append(lanes, reindexed...)
}()

// validatePageWorkspaceMoveInventory checks the list against the schema:
// every table with a workspace column is named once, with a rule, and every
// table and column it names is there.
func validatePageWorkspaceMoveInventory(app core.App) error {
	actual := []struct {
		Table  string `db:"table_name"`
		Column string `db:"column_name"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[schema]].[[name]] AS [[table_name]], [[field]].[[name]] AS [[column_name]]
		FROM [[sqlite_master]] [[schema]]
		JOIN pragma_table_info([[schema]].[[name]]) [[field]]
		WHERE [[schema]].[[type]] = 'table'
	`).All(&actual); err != nil {
		return err
	}
	columns := map[string]map[string]bool{}
	for _, column := range actual {
		if columns[column.Table] == nil {
			columns[column.Table] = map[string]bool{}
		}
		columns[column.Table][column.Column] = true
	}
	named := map[string]bool{}
	for _, table := range append(append([]pageWorkspaceMoveTable{}, pageWorkspaceMoveTables...), pageWorkspaceMoveOwnerTables...) {
		if named[table.Table] {
			return fmt.Errorf("%w: %s is named twice", errPageWorkspaceMoveUnavailable, table.Table)
		}
		named[table.Table] = true
		held := columns[table.Table]
		if held == nil {
			return fmt.Errorf("%w: %s isn't in the schema", errPageWorkspaceMoveUnavailable, table.Table)
		}
		wanted := append([]string{}, table.Owners...)
		if table.Organization != "" {
			wanted = append(wanted, table.Organization)
		}
		if table.Workspace != "" {
			wanted = append(wanted, table.Workspace)
		}
		if table.Rule != pageWorkspaceMoveRuleKept {
			wanted = append(wanted, table.Key)
		}
		if table.Principal != "" {
			wanted = append(wanted, table.Principal)
		}
		for _, column := range wanted {
			if !held[column] {
				return fmt.Errorf("%w: %s has no column %s", errPageWorkspaceMoveUnavailable, table.Table, column)
			}
		}
		if (table.Rule == pageWorkspaceMoveRuleKept || table.Rule == pageWorkspaceMoveRuleCut) != (table.Why != "") {
			return fmt.Errorf("%w: %s says no reason", errPageWorkspaceMoveUnavailable, table.Table)
		}
	}
	for table, held := range columns {
		if (held["workspace"] || held["workspace_id"]) && !named[table] &&
			table != pageWorkspaceMovesTable && table != pageWorkspaceMoveItemsTable {
			return fmt.Errorf("%w: %s says nothing of a page's move to another workspace", errPageWorkspaceMoveUnavailable, table)
		}
	}
	return nil
}
