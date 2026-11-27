package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

type workspaceDeletionDirectAuthority struct {
	Table       string
	Key         string
	Disposition string
}

const (
	workspaceDeletionAuthorityCascade    = "workspace_cascade"
	workspaceDeletionAuthorityPage       = "page_owner"
	workspaceDeletionAuthorityFile       = "file_owner"
	workspaceDeletionAuthorityResidue    = "bounded_residue"
	workspaceDeletionAuthorityTrigger    = "root_delete_trigger"
	workspaceDeletionAuthorityArtifact   = "export_artifact_owner"
	workspaceDeletionAuthorityAudit      = "preserved_audit"
	workspaceDeletionAuthorityReceipt    = "preserved_deletion_receipt"
	workspaceDeletionAuthorityProjection = "delegated_page_projection"
)

var workspaceDeletionDirectAuthorities = []workspaceDeletionDirectAuthority{
	{Table: "leafen_block_move_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_block_page_references", Key: "workspace_id", Disposition: workspaceDeletionAuthorityProjection},
	{Table: "leafen_blocks", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_block_revision_holds", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_block_trash_tombstones", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_bootstrap_page_visibility", Key: "workspace_id", Disposition: workspaceDeletionAuthorityProjection},
	{Table: "leafen_bootstrap_visibility_projection_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityProjection},
	{Table: "leafen_bootstrap_workspace_visibility", Key: "workspace_id", Disposition: workspaceDeletionAuthorityProjection},
	{Table: "leafen_comment_deletion_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_comment_receipts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_comments", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_comment_tombstones", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_custom_emoji_tombstones", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_unavailable_database_blocks", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_custom_emojis", Key: "workspace_id", Disposition: workspaceDeletionAuthorityFile},
	{Table: "leafen_people_value_invitees", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_data_sources", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	// Its source's record in the trash outlives the page lanes; the
	// page-owned-leftovers lane clears it (workspace_deletion_page_leftovers.go).
	{Table: "leafen_data_source_trash_in_place", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_containers", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_container_sources", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_container_views", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_dependency_bindings", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_dependency_edges", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_automation_definitions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_automation_deliveries", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_automation_events", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_automation_frontiers", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_automation_mutations", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_automation_notifications", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_automation_outcomes", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_automation_schedule_occurrences", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_properties", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_query_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_sql_query_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_database_relation_items", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_relation_property_targets", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_database_view_property_refs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_database_mutation_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_database_row_trash", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_property_trash", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_row_pages", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_row_values", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_rows", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_task_features", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_templates", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_toolbar_mutation_receipts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_database_toolbar_template_state", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_unique_id_allocators", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_views", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_file_delete_receipts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_file_manual_cleanup_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_file_maintenance_outcomes", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_file_maintenance_runs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_file_reference_cleanups", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_file_references", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_file_uploads", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_file_usage_report_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_file_usage_report_workspace_results", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_files", Key: "workspace", Disposition: workspaceDeletionAuthorityFile},
	{Table: "leafen_file_text_extracts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityFile},
	{Table: "leafen_form_links", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_form_option_frontiers", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_imported_people_mapping_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_interactive_automation_executions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_mcp_connection_workspaces", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_mcp_enterprise_member_access", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_mcp_oauth_authorization_workspaces", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_notification_deliveries", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_notification_events", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_row_notification_fanouts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_notifications", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_mcp_inbox_shown", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_wiki_verification_queue", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_wiki_verification_email_deliveries", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_wiki_mutations", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_notification_archive_receipts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_notification_read_settlements", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_notification_emails", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_invite_emails", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_share_access_receipts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_share_access_pending", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_invitations", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_name_invitations", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_account_joins", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_invite_links", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_teamspace_invite_links", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_guest_links", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_organization_guest_mfa_workspaces", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_restricted_members", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_person_profiles", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_people_databases", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_people_rows", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_people_database_moves", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_web_push_deliveries", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_slack_notification_deliveries", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_agent_revisions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_agent_settings", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_agents", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_credit_access_assignments", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_credit_access_policies", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_credit_access_revisions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_import_connections", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_notion_migration_ledger", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_migration_issues", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_migration_scan_items", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_migration_formats", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_migrations", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_object_identities", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_row_page_content_cleanup", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_notion_view_queries", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_async_tasks", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_database_property_conversions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_database_page_rules", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_area_sources", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_areas", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_area_members", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_area_closure", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_area_joins", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_areas", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_teamspace_areas", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_area_personal", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_server_admin_workspace_events", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_read_log_readers", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_read_log_places", Key: "workspace_id", Disposition: workspaceDeletionAuthorityAudit},
	{Table: "leafen_page_link_visitors", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_deleted_databases", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_oauth_grants", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_notion_personal_access_token_revisions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_personal_access_tokens", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_internal_connections", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_notion_internal_connection_owners", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_organization_audit_events", Key: "workspace_id", Disposition: workspaceDeletionAuthorityAudit},
	{Table: "leafen_organization_mcp_approved_clients", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_organization_mcp_workspace_policies", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_organization_storage_reservations", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_database_relation_pair_work", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_analytics_batches", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_analytics_daily", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_analytics_member_preferences", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_analytics_page_opt_outs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_analytics_recent_viewers", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_analytics_unique_viewers", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_row_page_analytics_daily", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_row_page_analytics_recent_viewers", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_row_page_analytics_unique_viewers", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_database_analytics_daily", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_database_analytics_unique_viewers", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_database_analytics_recent_viewers", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_database_analytics_opt_outs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_analytics_member_daily", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_activity", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_allowed_email_domains", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_sidebar_presets", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_search_daily", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	// A workspace's move to another server (workspace_move_fence.go) goes with it.
	{Table: "leafen_workspace_moves_out", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_move_in_slots", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_move_out_codes", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_moved_people", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	// The names a copy kept go with its top page (workspace_carried_people.go).
	{Table: "leafen_workspace_carried_people", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_notion_imported_people", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_notion_imported_authors", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_child_page_block_places", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_child_page_block_backfill", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_page_signposts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_page_interaction_revisions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_interactions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_duplication_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_page_favorites", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_page_reactions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_page_share_unlinks", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_page_teamspace_roles", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_page_layouts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_property_access_exceptions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_database_property_access", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_database_favorites", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_sidebar_layouts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_my_task_sources", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_form_favorites", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_my_task_views", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_visits", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_list_changes", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_page_public_identities", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_page_lock_unlocks", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_page_notification_preferences", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_notification_preferences", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_reminders", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_deadline_notices", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_deadline_acknowledgements", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_database_sprints", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_page_history_checkpoints", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_page_history_events", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_page_history_restore_receipts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_page_history_block_spans", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_page_history_text_spans", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_page_history_snapshot_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_page_history_versions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_page_permanent_delete_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_page_permissions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_pages", Key: "workspace", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_public_mutation_effects", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_public_mutation_operations", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_public_mutation_states", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_public_sites", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_public_share_links", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_search_dirty", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_search_pages", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_search_chunks", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_search_pages_fts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityProjection},
	{Table: "leafen_search_workspace_state", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_teamspace_join_requests", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_teamspace_members", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_teamspace_publication_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_teamspace_mutation_receipts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_teamspace_settings", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_teamspaces", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_trash_empty_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_page_deletion_holds", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_workspace_archive_import_targets", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_workspace_content_revisions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityTrigger},
	{Table: "leafen_workspace_creation_receipts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_workspace_deletion_jobs", Key: "workspace_id", Disposition: workspaceDeletionAuthorityReceipt},
	{Table: "leafen_workspace_export_artifacts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityArtifact},
	{Table: "leafen_workspace_export_page_counts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityPage},
	{Table: "leafen_workspace_import_replace_leases", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_workspace_import_replace_snapshots", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_workspace_membership_notifications", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_memberships", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_workspace_owner_transfer_notifications", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_permission_group_revisions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_permission_groups", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_permission_state", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_workspace_settings_receipts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_settings_state", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_workspace_connection_policies", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_access_requests", Key: "workspace_id", Disposition: workspaceDeletionAuthorityResidue},
	{Table: "leafen_integration_accounts", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_view_links", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_webhook_events", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_webhook_subscriptions", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	{Table: "leafen_app_workspaces", Key: "workspace_id", Disposition: workspaceDeletionAuthorityTrigger},
}

var workspaceDeletionResidueLanes = []workspaceDeletionResidueLane{
	{Table: "leafen_database_toolbar_mutation_receipts", Key: "workspace_id"},
	{Table: "leafen_database_sql_query_jobs", Key: "workspace_id"},
	{Table: "leafen_file_uploads", Key: "workspace_id"},
	{Table: "leafen_file_delete_receipts", Key: "workspace_id"},
	{Table: "leafen_file_reference_cleanups", Key: "workspace_id"},
	{Table: "leafen_file_usage_report_workspace_results", Key: "workspace_id"},
	{Table: "leafen_file_usage_report_jobs", Key: "workspace_id"},
	{Table: "leafen_mcp_connection_workspaces", Key: "workspace_id"},
	{Table: "leafen_mcp_oauth_authorization_workspaces", Key: "workspace_id"},
	{Table: "leafen_notion_import_connections", Key: "workspace_id"},
	{Table: "leafen_notion_oauth_authorization_codes", Key: "grant_id", ParentTable: "leafen_notion_oauth_grants"},
	{Table: "leafen_notion_oauth_refresh_tokens", Key: "grant_id", ParentTable: "leafen_notion_oauth_grants"},
	{Table: "leafen_notion_oauth_grant_resources", Key: "grant_id", ParentTable: "leafen_notion_oauth_grants"},
	{Table: "leafen_notion_oauth_grants", Key: "workspace_id"},
	{Table: "leafen_notion_internal_connections", Key: "workspace_id"},
	{Table: "leafen_notion_internal_connection_owners", Key: "workspace_id"},
	{SharedOwner: "page-permanent-delete-receipts"},
	{Table: "leafen_workspace_export_scope_pages", Key: "job_id", ParentTable: "leafen_jobs"},
	{Table: "leafen_workspace_export_scope_blocks", Key: "job_id", ParentTable: "leafen_jobs"},
	{Table: "leafen_jobs", Key: "workspace_id"},
	{Table: "leafen_search_workspace_state", Key: "workspace_id"},
	{Table: "leafen_trash_empty_jobs", Key: "workspace_id"},
	{Table: "leafen_page_deletion_holds", Key: "workspace_id"},
	{Table: "leafen_workspace_archive_import_targets", Key: "workspace_id"},
	{Table: "leafen_workspace_creation_receipts", Key: "workspace_id"},
	{Table: "leafen_workspace_import_replace_snapshots", Key: "workspace_id"},
	{Table: "leafen_workspace_import_replace_leases", Key: "workspace_id"},
	{Table: "leafen_workspace_memberships", Key: "workspace_id"},
	{Table: "leafen_workspace_permission_state", Key: "workspace_id"},
	// Appended so a deletion already under way keeps its lane numbers.
	{Table: "leafen_row_page_content_cleanup", Key: "workspace_id"},
	{Table: "leafen_access_requests", Key: "workspace_id"},
	// Database row pages leave with their database, not through the page
	// lanes; the search owner clears what they left in its projection.
	{SharedOwner: "workspace-search-projection"},
	// The page lanes clear a page's own history; a database's row pages keep
	// theirs when their database goes, so it is cleared by workspace (a
	// snapshot job's blocks and a version's bookmarks go with them).
	{Table: "leafen_page_history_snapshot_jobs", Key: "workspace_id"},
	{Table: "leafen_page_history_restore_receipts", Key: "workspace_id"},
	{Table: "leafen_page_history_text_spans", Key: "workspace_id"},
	{Table: "leafen_page_history_block_spans", Key: "workspace_id"},
	{Table: "leafen_page_history_checkpoints", Key: "workspace_id"},
	{Table: "leafen_page_history_events", Key: "workspace_id"},
	{Table: "leafen_page_history_versions", Key: "workspace_id"},
	// Tables that point at their workspace with no foreign key to cascade.
	{Table: "leafen_notification_archive_receipts", Key: "workspace_id"},
	{Table: "leafen_notification_read_settlements", Key: "workspace_id"},
	{Table: "leafen_notion_async_tasks", Key: "workspace_id"},
	{Table: "leafen_workspace_moved_people", Key: "workspace_id"},
	// Rows the workspace's pages owned that outlive the page lanes (workspace_deletion_page_leftovers.go).
	{SharedOwner: "page-owned-leftovers"},
}

func validateWorkspaceDeletionDirectAuthorityInventory(app core.App) error {
	actual := []struct {
		Table string `db:"table_name"`
		Key   string `db:"key_name"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[schema]].[[name]] AS [[table_name]], [[field]].[[name]] AS [[key_name]]
		FROM [[sqlite_master]] [[schema]]
		JOIN pragma_table_info([[schema]].[[name]]) [[field]]
		WHERE [[schema]].[[type]] = 'table'
			AND [[field]].[[name]] IN ('workspace', 'workspace_id')
		ORDER BY [[schema]].[[name]], [[field]].[[cid]]
	`).All(&actual); err != nil {
		return err
	}
	expected := make(map[string]string, len(workspaceDeletionDirectAuthorities))
	// The per-person page list went with migration 613.
	kept := bootstrapPageVisibilityKept(app)
	for _, authority := range workspaceDeletionDirectAuthorities {
		if !kept && authority.Table == bootstrapPageVisibilityTable || !retiredAreaTableHeld(app, authority.Table) {
			continue
		}
		key := authority.Table + ":" + authority.Key
		if _, duplicate := expected[key]; duplicate {
			return errWorkspaceDeleteUnavailable
		}
		expected[key] = authority.Disposition
	}
	for _, authority := range actual {
		if _, ok := expected[authority.Table+":"+authority.Key]; !ok {
			return fmt.Errorf("%w: %s.%s says nothing of deleting a workspace", errWorkspaceDeleteUnavailable, authority.Table, authority.Key)
		}
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("%w: the inventory lists a table the schema hasn't", errWorkspaceDeleteUnavailable)
	}
	directLanes := map[string]struct{}{}
	childLanes := map[string]struct{}{}
	sharedLanes := map[string]struct{}{}
	projectionAuthorities := map[string]struct{}{}
	for _, lane := range workspaceDeletionResidueLanes {
		if lane.SharedOwner != "" {
			if lane.Table != "" || lane.Key != "" || lane.ParentTable != "" {
				return errWorkspaceDeleteUnavailable
			}
			if _, duplicate := sharedLanes[lane.SharedOwner]; duplicate {
				return errWorkspaceDeleteUnavailable
			}
			sharedLanes[lane.SharedOwner] = struct{}{}
			continue
		}
		key := lane.Table + ":" + lane.Key
		owners := directLanes
		if lane.ParentTable != "" {
			key += ":" + lane.ParentTable
			owners = childLanes
		}
		if _, duplicate := owners[key]; duplicate {
			return errWorkspaceDeleteUnavailable
		}
		owners[key] = struct{}{}
	}
	for key, disposition := range expected {
		_, hasLane := directLanes[key]
		if (disposition == workspaceDeletionAuthorityResidue) != hasLane {
			return errWorkspaceDeleteUnavailable
		}
		if disposition == workspaceDeletionAuthorityProjection {
			projectionAuthorities[key] = struct{}{}
		}
	}
	projections := []string{
		"leafen_block_page_references:workspace_id",
		"leafen_bootstrap_visibility_projection_jobs:workspace_id",
		"leafen_bootstrap_workspace_visibility:workspace_id",
		"leafen_search_pages_fts:workspace_id",
	}
	if kept {
		projections = append(projections, "leafen_bootstrap_page_visibility:workspace_id")
	}
	if len(projectionAuthorities) != len(projections) {
		return errWorkspaceDeleteUnavailable
	}
	for _, key := range projections {
		if _, ok := projectionAuthorities[key]; !ok {
			return errWorkspaceDeleteUnavailable
		}
	}
	for _, key := range []string{
		"leafen_notion_oauth_authorization_codes:grant_id:leafen_notion_oauth_grants",
		"leafen_notion_oauth_refresh_tokens:grant_id:leafen_notion_oauth_grants",
		"leafen_notion_oauth_grant_resources:grant_id:leafen_notion_oauth_grants",
		"leafen_workspace_export_scope_pages:job_id:leafen_jobs",
		"leafen_workspace_export_scope_blocks:job_id:leafen_jobs",
	} {
		if _, ok := childLanes[key]; !ok {
			return errWorkspaceDeleteUnavailable
		}
	}
	if len(childLanes) != 5 {
		return errWorkspaceDeleteUnavailable
	}
	if len(sharedLanes) != 3 {
		return errWorkspaceDeleteUnavailable
	}
	for _, owner := range []string{"page-permanent-delete-receipts", "workspace-search-projection", "page-owned-leftovers"} {
		if _, ok := sharedLanes[owner]; !ok {
			return errWorkspaceDeleteUnavailable
		}
	}
	if err := validateWorkspaceDeletionCascades(app, expected); err != nil {
		return err
	}
	// And each table it reads by its workspace has an index for that read.
	if err := validateWorkspaceDeletionAccessPaths(app); err != nil {
		return fmt.Errorf("%w: %v", errWorkspaceDeleteUnavailable, err)
	}
	return nil
}

// validateWorkspaceDeletionCascades checks that each table said to go with
// its workspace by a cascade has one, read from the schema: a foreign key
// that deletes its rows with the workspace or with a table that goes with
// it. A row left behind would stop the deletion at its last step. It also
// checks the order the cascade lanes clear those tables in (see
// workspace_deletion_cascade_lanes.go).
func validateWorkspaceDeletionCascades(app core.App, expected map[string]string) error {
	references := []workspaceDeletionForeignKey{}
	if err := app.DB().NewQuery(`
		SELECT [[schema]].[[name]] AS [[table_name]], [[reference]].[[table]] AS [[parent_name]],
			upper([[reference]].[[on_delete]]) AS [[on_delete]]
		FROM [[sqlite_master]] [[schema]]
		JOIN pragma_foreign_key_list([[schema]].[[name]]) [[reference]]
		WHERE [[schema]].[[type]] = 'table'
	`).All(&references); err != nil {
		return err
	}
	removed := map[string]bool{workspacesCollection: true}
	for key, disposition := range expected {
		switch disposition {
		case workspaceDeletionAuthorityAudit, workspaceDeletionAuthorityReceipt, workspaceDeletionAuthorityProjection:
		default:
			removed[strings.SplitN(key, ":", 2)[0]] = true
		}
	}
	cascading := map[string]bool{}
	for _, reference := range references {
		if reference.OnDelete == "CASCADE" && removed[reference.Parent] {
			cascading[reference.Table] = true
		}
	}
	for key, disposition := range expected {
		if table := strings.SplitN(key, ":", 2)[0]; disposition == workspaceDeletionAuthorityCascade && !cascading[table] {
			return fmt.Errorf("%w: %s goes with a workspace by no cascade", errWorkspaceDeleteUnavailable, key)
		}
	}
	return validateWorkspaceDeletionCascadeLaneOrder(references)
}

func validateWorkspaceDeletionTerminalResidue(app core.App, workspaceID string) error {
	queries := make([]string, 0, len(workspaceDeletionDirectAuthorities)-2)
	for _, authority := range workspaceDeletionDirectAuthorities {
		if authority.Disposition == workspaceDeletionAuthorityAudit ||
			authority.Disposition == workspaceDeletionAuthorityReceipt ||
			authority.Disposition == workspaceDeletionAuthorityProjection || !retiredAreaTableHeld(app, authority.Table) {
			continue
		}
		queries = append(queries, fmt.Sprintf(
			"SELECT COUNT(*) AS [[count]] FROM {{%s}} WHERE [[%s]] = {:workspaceId}",
			authority.Table, authority.Key,
		))
	}
	var residue int
	query := "SELECT COALESCE(SUM([[count]]), 0) FROM (" + strings.Join(queries, " UNION ALL ") + ")"
	if err := app.DB().NewQuery(query).Bind(map[string]any{"workspaceId": workspaceID}).Row(&residue); err != nil {
		return err
	}
	if residue != 0 {
		return errWorkspaceDeleteUnavailable
	}
	return nil
}
