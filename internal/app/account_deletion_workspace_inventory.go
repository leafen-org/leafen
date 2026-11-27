package app

// What deleting an account does to the rows a living workspace keeps by
// person (2026-11-17). The workspace deletion inventory names what goes with
// the workspace; this names what goes with the person while the workspace
// stays. The rule, as removing someone from an organization
// (clearOrganizationPlaces) has it: what was only theirs goes — their inbox
// and the mail and pushes waiting for them, their settings, favorites and
// profile, the places that named them — and what records their part in the
// workspace stays: its content, its receipts and history, and its analytics,
// which the organization keeps for the period it sets. Their analytics
// choices stay with the views they hide, or the views would show again.
// Teamspace memberships stay while the workspace lives (2026-11-07: they are
// decided with the teamspace owner rules), as the areas' did where a server
// still holds them (schema_area_retirement.go).
const (
	// The key a workspace's content is stored under, its first owner's ID:
	// the content stays with its workspace — deleted with the person's own
	// workspaces, kept under the successor's ownership otherwise — and the
	// key names no one to readers.
	accountDeletionContentKey = "workspace_content_key"
	// A teamspace's member line, kept while the workspace lives.
	accountDeletionMembershipKept = "membership_kept"
	// Rebuilt from the memberships the cleanup deletes; the rebuild takes the
	// person's rows with them (the runner is woken as it deletes).
	accountDeletionProjection = "projection"
	// Short-lived work, caches and links that expire by themselves; each
	// reads as its person, who can open nothing once their places are gone.
	accountDeletionExpiring = "expiring"
	// The column names a record or a stand-in, never an account here.
	accountDeletionNotPerson = "not_a_person"
)

// accountDeletionWorkspaceInventory names each table that keeps rows by
// person and by workspace; accountDeletionInventory names those without a
// workspace key. Both are checked against the schema together.
var accountDeletionWorkspaceInventory = []accountDeletionPersonTable{
	// A request still waiting goes with the person who asked (a lane keeps
	// to pending ones); a decided one is the page's record.
	{Table: "leafen_access_requests", Key: "principal_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_access_requests", Key: "requester_id", Disposition: accountDeletionCleared},
	{Table: "leafen_account_joins", Key: "user_id", Disposition: accountDeletionCleared},
	// The areas' tables, there only on a server that held areas
	// (schema_area_retirement.go), read while they are there.
	{Table: "leafen_area_joins", Key: "user_id", Disposition: accountDeletionMembershipKept},
	{Table: "leafen_area_members", Key: "principal_id", Disposition: accountDeletionMembershipKept},
	{Table: "leafen_area_personal", Key: "user_id", Disposition: accountDeletionMembershipKept},
	{Table: "leafen_block_move_jobs", Key: "owner_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_block_trash_tombstones", Key: "owner_id", Disposition: accountDeletionContentKey},
	{Table: "leafen_blocks", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_bootstrap_page_visibility", Key: "principal_id", Disposition: accountDeletionProjection},
	{Table: "leafen_bootstrap_visibility_projection_jobs", Key: "principal_id", Disposition: accountDeletionProjection},
	{Table: "leafen_bootstrap_workspace_visibility", Key: "principal_id", Disposition: accountDeletionProjection},
	{Table: "leafen_data_source_trash_in_place", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_data_sources", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_analytics_opt_outs", Key: "user_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_database_analytics_recent_viewers", Key: "user_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_database_automation_definitions", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_automation_deliveries", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_automation_events", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_automation_frontiers", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_automation_notifications", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_automation_outcomes", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_automation_schedule_occurrences", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_container_sources", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_container_views", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_containers", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_dependency_bindings", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_dependency_edges", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_favorites", Key: "principal_id", Disposition: accountDeletionCleared},
	{Table: "leafen_database_mutation_jobs", Key: "owner_id", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_notification_preferences", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_database_page_rules", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_properties", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_property_access_exceptions", Key: "principal_id", Disposition: accountDeletionCleared},
	{Table: "leafen_database_property_conversions", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_property_trash", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_query_jobs", Key: "principal_id", Disposition: accountDeletionExpiring},
	{Table: "leafen_database_relation_items", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_relation_pair_work", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_relation_property_targets", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_row_pages", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_row_trash", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_row_values", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_rows", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_sprints", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_sql_query_jobs", Key: "principal_id", Disposition: accountDeletionExpiring},
	{Table: "leafen_database_task_features", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_templates", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_toolbar_mutation_receipts", Key: "principal_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_database_unique_id_allocators", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_view_property_refs", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_database_views", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_deadline_acknowledgements", Key: "user_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_deleted_databases", Key: "owner", Disposition: accountDeletionContentKey},
	// The record a file property or block holds the file for.
	{Table: "leafen_file_references", Key: "owner_id", Disposition: accountDeletionNotPerson},
	{Table: "leafen_form_favorites", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_form_links", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_imported_people_mapping_jobs", Key: "owner_id", Disposition: accountDeletionContentKey},
	// An app's own account in a workspace, not a person's.
	{Table: "leafen_integration_accounts", Key: "user_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_interactive_automation_executions", Key: "owner_id", Disposition: accountDeletionContentKey},
	// Work they asked for runs out, and its record and result stay with it.
	{Table: "leafen_jobs", Key: "owner", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_mcp_enterprise_member_access", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_my_task_sources", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_my_task_views", Key: "owner_id", Disposition: accountDeletionCleared},
	// Waiting ones settle as skipped, the person no longer able to open
	// what they name, and go with their notification events.
	{Table: "leafen_notification_deliveries", Key: "user_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_notification_emails", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_notifications", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_notion_async_tasks", Key: "principal_id", Disposition: accountDeletionExpiring},
	// An administrator's AI credit policy line, as organization removal leaves it.
	{Table: "leafen_notion_credit_access_assignments", Key: "user_id", Disposition: accountDeletionKeptRecord},
	// The person's own Notion token for imports.
	{Table: "leafen_notion_import_connections", Key: "owner", Disposition: accountDeletionCleared},
	// A connection the workspace uses, as leafen_mcp_connections.
	{Table: "leafen_notion_internal_connection_owners", Key: "user_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_notion_migrations", Key: "owner", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_notion_oauth_grants", Key: "owner_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_notion_view_queries", Key: "principal_id", Disposition: accountDeletionExpiring},
	{Table: "leafen_page_analytics_batches", Key: "principal_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_page_analytics_member_preferences", Key: "user_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_page_analytics_page_opt_outs", Key: "user_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_page_analytics_recent_viewers", Key: "user_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_page_duplication_jobs", Key: "owner_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_page_favorites", Key: "principal_id", Disposition: accountDeletionCleared},
	{Table: "leafen_page_interactions", Key: "user_id", Disposition: accountDeletionExpiring},
	{Table: "leafen_page_link_visitors", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_page_lock_unlocks", Key: "principal_id", Disposition: accountDeletionCleared},
	{Table: "leafen_page_notification_preferences", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_page_permanent_delete_jobs", Key: "owner_id", Disposition: accountDeletionContentKey},
	{Table: "leafen_page_permissions", Key: "principal_id", Disposition: accountDeletionCleared},
	{Table: "leafen_page_reactions", Key: "user_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_page_share_access_pending", Key: "principal_id", Disposition: accountDeletionCleared},
	{Table: "leafen_page_share_access_receipts", Key: "principal_id", Disposition: accountDeletionCleared},
	{Table: "leafen_page_visits", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_pages", Key: "owner", Disposition: accountDeletionContentKey},
	// People keeps a row for someone who left, as deactivated.
	{Table: "leafen_people_guests", Key: "user_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_row_page_analytics_recent_viewers", Key: "user_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_search_pages", Key: "owner_id", Disposition: accountDeletionContentKey},
	{Table: "leafen_sidebar_layouts", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_slack_notification_deliveries", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_teamspace_join_requests", Key: "user_id", Disposition: accountDeletionMembershipKept},
	{Table: "leafen_teamspace_members", Key: "principal_id", Disposition: accountDeletionMembershipKept},
	{Table: "leafen_trash_empty_jobs", Key: "owner_id", Disposition: accountDeletionContentKey},
	{Table: "leafen_view_links", Key: "owner_id", Disposition: accountDeletionExpiring},
	{Table: "leafen_web_push_deliveries", Key: "user_id", Disposition: accountDeletionCleared},
	// It sends what its owner can read (authorizePage): nothing once their
	// places are gone; the connection it came with keeps its line.
	{Table: "leafen_webhook_subscriptions", Key: "owner_id", Disposition: accountDeletionKeptRecord},
	// It keeps the address it was queued with, so it goes.
	{Table: "leafen_wiki_verification_email_deliveries", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_workspace_analytics_member_daily", Key: "user_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_workspace_creation_receipts", Key: "owner_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_workspace_deletion_jobs", Key: "owner_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_workspace_export_artifacts", Key: "owner", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_workspace_export_page_counts", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_workspace_import_replace_leases", Key: "owner", Disposition: accountDeletionContentKey},
	{Table: "leafen_workspace_import_replace_snapshots", Key: "owner", Disposition: accountDeletionContentKey},
	// The names a copy into another workspace kept of the people its values
	// name (workspace_carried_people.go) stay, as a mention's written name does.
	{Table: "leafen_workspace_carried_people", Key: "person_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_workspace_membership_notifications", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_workspace_memberships", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_workspace_move_in_slots", Key: "owner_id", Disposition: accountDeletionExpiring},
	// The stand-ins of a moved workspace's people who have no account here.
	{Table: "leafen_workspace_moved_people", Key: "person_id", Disposition: accountDeletionNotPerson},
	{Table: "leafen_workspace_owner_transfer_notifications", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_workspace_people_rows", Key: "user_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_workspace_person_profiles", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_workspace_restricted_members", Key: "user_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_workspace_search_daily", Key: "user_id", Disposition: accountDeletionKeptRecord},
}
