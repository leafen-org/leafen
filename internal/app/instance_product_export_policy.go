package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	instanceProductExportIncluded = "included"
	instanceProductExportExcluded = "excluded"
)

var errInstanceProductExportPolicyIncomplete = errors.New("LEAFEN_INSTANCE_PRODUCT_EXPORT_POLICY_INCOMPLETE")

// dataDatabaseToolTables are the tables a tool other than Leafen makes in the
// data database: litestream, once continuous replication starts
// (replica_session.go), keeps its place in the database's log in these and
// leaves them there when it stops. They are the tool's, not the product's:
// machine-only, left out of the export, and no part of the product's schema
// (productSchemaDigest). A backup or a replica copies the database as it is,
// and litestream takes up its rows again, or makes them afresh, wherever it
// next starts. Before 2026-11-22 a server with replication on had tables the
// export policy had no class for: its whole-server export failed, and a
// development build wouldn't start again.
var dataDatabaseToolTables = []string{"_litestream_seq", "_litestream_lock"}

type instanceProductExportTablePolicy struct {
	Table          string
	Disposition    string
	Reason         string
	Columns        []string
	OmittedColumns map[string]struct{}
	// ReviewedColumns are columns whose names hold a sensitive word but whose
	// values were read and hold no secret (a setting about secrets, say).
	ReviewedColumns map[string]struct{}
}

type instanceProductExportCoverageEntry struct {
	Table  string `json:"table"`
	Reason string `json:"reason"`
}

type instanceProductExportCoverage struct {
	Included []instanceProductExportCoverageEntry `json:"included"`
	Excluded []instanceProductExportCoverageEntry `json:"excluded"`
}

func instanceProductExportPolicyDefinitions() map[string]instanceProductExportTablePolicy {
	definitions := map[string]instanceProductExportTablePolicy{}
	add := func(disposition, reason string, tables ...string) {
		for _, table := range tables {
			definitions[table] = instanceProductExportTablePolicy{
				Table: table, Disposition: disposition, Reason: reason,
			}
		}
	}
	add(instanceProductExportIncluded, "authoritative product content",
		"leafen_blocks", "leafen_block_trash_tombstones", "leafen_bootstrap_page_visibility", "leafen_bootstrap_workspace_visibility",
		"leafen_database_automation_definitions", "leafen_database_automation_frontiers",
		"leafen_database_automation_notifications",
		"leafen_database_automation_schedule_occurrences",
		"leafen_data_sources", "leafen_database_containers", "leafen_database_container_sources",
		"leafen_database_page_signposts",
		"leafen_database_container_trash", "leafen_data_source_trash_in_place",
		"leafen_database_container_views", "leafen_database_dependency_bindings", "leafen_database_dependency_edges",
		"leafen_database_properties", "leafen_database_relation_items", "leafen_database_rows",
		"leafen_database_property_conversion_cells", "leafen_database_property_conversions",
		"leafen_database_row_trash", "leafen_database_row_trash_spans", "leafen_database_row_kept_values",
		"leafen_database_property_trash",
		"leafen_database_row_page_favorites", "leafen_database_row_pages",
		"leafen_database_row_values", "leafen_database_task_features", "leafen_database_templates",
		"leafen_database_toolbar_template_state", "leafen_database_unique_id_allocators", "leafen_database_views",
		"leafen_file_references", "leafen_form_links", "leafen_form_option_frontiers",
		"leafen_comments", "leafen_comment_tombstones", "leafen_comment_reactions", "leafen_page_reactions", "leafen_comment_thread_states", "leafen_custom_emojis", "leafen_custom_emoji_tombstones", "leafen_people_value_invitees", "leafen_graph_changes", "leafen_graph_mutations", "leafen_mutation_receipts",
		"leafen_connection_authorship",
		"leafen_notion_import_mappings", "leafen_notion_import_reports", "leafen_notion_import_sources",
		"leafen_notion_credit_access_policies", "leafen_notion_credit_access_assignments",
		"leafen_notion_agents", "leafen_notion_agent_permissions",
		"leafen_notion_agent_settings", "leafen_notion_agent_usage_periods",
		"leafen_organization_dlp_policies", "leafen_organization_user_key_policies", "leafen_organization_domains", "leafen_organization_legal_hold_mutations",
		"leafen_organization_legal_hold_scopes", "leafen_organization_legal_holds",
		"leafen_mcp_enterprise_member_access",
		"leafen_organization_mcp_approved_clients", "leafen_organization_mcp_workspace_policies",
		"leafen_organization_admin_roles", "leafen_organization_group_members", "leafen_organization_groups",
		"leafen_organization_memberships", "leafen_organization_policy_state", "leafen_organization_managed_user_policies",
		"leafen_organization_sharing_security_policies", "leafen_organization_guest_mfa_workspaces", "leafen_organization_sign_in_policies", "leafen_workspace_restricted_members", "leafen_workspace_person_profiles",
		"leafen_organization_audit_retention_policies", "leafen_organization_activity_retention_policies", "leafen_organization_account_history_policies", "leafen_organization_view_display_policies", "leafen_organization_storage_reconciliation",
		"leafen_organization_storage_reservations", "leafen_organization_storage_usage", "leafen_organizations",
		"leafen_page_analytics_daily", "leafen_page_analytics_member_preferences",
		"leafen_notification_settings", "leafen_page_notification_preferences", "leafen_reminders", "leafen_deadline_notices", "leafen_deadline_acknowledgements", "leafen_database_sprints",
		"leafen_database_row_notification_preferences", "leafen_database_notification_preferences",
		"leafen_page_analytics_page_opt_outs", "leafen_page_analytics_recent_viewers",
		"leafen_page_analytics_unique_viewers", "leafen_page_interactions", "leafen_page_interaction_revisions",
		"leafen_row_page_analytics_daily", "leafen_row_page_analytics_recent_viewers", "leafen_row_page_analytics_unique_viewers",
		"leafen_database_analytics_daily", "leafen_database_analytics_unique_viewers", "leafen_database_analytics_recent_viewers",
		"leafen_database_analytics_opt_outs",
		"leafen_workspace_analytics_member_daily", "leafen_workspace_allowed_email_domains", "leafen_workspace_sidebar_presets",
		"leafen_workspace_search_daily",
		"leafen_page_favorites", "leafen_database_favorites", "leafen_sidebar_layouts", "leafen_my_task_sources", "leafen_form_favorites", "leafen_my_task_views", "leafen_workspace_people_databases", "leafen_workspace_people_rows", "leafen_people_database_moves", "leafen_page_visits", "leafen_page_lock_unlocks", "leafen_page_lock_records", "leafen_page_history_checkpoints", "leafen_page_history_events", "leafen_page_history_versions",
		"leafen_page_history_bookmarks",
		"leafen_page_permissions", "leafen_page_share_unlinks", "leafen_page_teamspace_roles", "leafen_database_row_page_permissions", "leafen_public_mutation_states",
		// The tables areas were kept in are carried where a server holds
		// them still: migration 835 takes them away where it holds none
		// (schema_area_retirement.go).
		"leafen_area_sources", "leafen_areas", "leafen_area_members", "leafen_area_closure", "leafen_area_joins", "leafen_page_areas", "leafen_teamspace_areas", "leafen_area_personal",
		"leafen_server_admin_workspace_events", "leafen_read_watches", "leafen_read_log_readers", "leafen_database_page_rules", "leafen_database_page_layouts", "leafen_database_property_access", "leafen_database_property_access_exceptions", "leafen_page_link_visitors", "leafen_deleted_databases",
		"leafen_pages", "leafen_page_deletion_holds", "leafen_public_share_graph_memberships",
		// An earlier wiki's list of its pages is carried while a database
		// holds it, until migration 832 takes it away: the schema baseline's
		// check counts its export triggers on a database before 832.
		"leafen_wiki_pages", "leafen_wiki_page_owners",
		"leafen_public_share_links", "leafen_public_sites", "leafen_scim_group_members", "leafen_scim_groups",
		"leafen_scim_list_access_entries", "leafen_scim_list_access_state", "leafen_scim_list_access_tree",
		"leafen_scim_settings", "leafen_workspace_memberships", "leafen_workspace_permission_group_members",
		"leafen_workspace_permission_groups", "leafen_workspace_permission_state",
		"leafen_teamspace_join_requests", "leafen_teamspace_members", "leafen_teamspace_settings", "leafen_teamspaces",
		"leafen_workspaces", "leafen_ygo_documents", "leafen_ygo_document_states", "leafen_ygo_structural_documents",
		"leafen_ygo_structural_document_states",
		"leafen_ygo_structural_updates", "leafen_ygo_updates",
	)
	add(instanceProductExportIncluded, "file metadata only; raw file bytes are excluded", "leafen_files")
	add(instanceProductExportIncluded, "a workspace's move to another server, which keeps it read-only there", "leafen_workspace_moves_out")
	add(instanceProductExportExcluded, "a workspace's move under way: the code the old server asks the new one by, and what the new one imports; it ends between those two",
		"leafen_workspace_move_in_slots", "leafen_workspace_move_out_codes")
	add(instanceProductExportIncluded, "the names of a moved workspace's people with no account here yet, which its history shows", "leafen_workspace_moved_people")
	add(instanceProductExportIncluded, "the names of the people a copy from another workspace names in its values, kept by the copy's top page", "leafen_workspace_carried_people")
	add(instanceProductExportIncluded, "the names of a Notion migration's people matched to no member, and what they made", "leafen_notion_imported_people", "leafen_notion_imported_authors")
	filePolicy := definitions["leafen_files"]
	filePolicy.OmittedColumns = map[string]struct{}{"blob": {}}
	definitions["leafen_files"] = filePolicy
	for table, columns := range map[string][]string{
		"leafen_form_links":               {"token_digest"},
		"leafen_custom_emojis":            {"workspace_import_job_id"},
		"leafen_organization_legal_holds": {"notion_admin_token_id"},
		"leafen_public_share_links":       {"token_digest", "token_cipher"},
	} {
		policy := definitions[table]
		policy.OmittedColumns = map[string]struct{}{}
		for _, column := range columns {
			policy.OmittedColumns[column] = struct{}{}
		}
		definitions[table] = policy
	}
	// Whether a view link may open without its secret: a yes or no, no secret.
	sharingPolicy := definitions["leafen_organization_sharing_security_policies"]
	sharingPolicy.ReviewedColumns = map[string]struct{}{"view_link_without_secret": {}}
	definitions["leafen_organization_sharing_security_policies"] = sharingPolicy

	add(instanceProductExportExcluded, "continuous replication's own bookkeeping, which litestream makes and keeps in the database",
		dataDatabaseToolTables...)
	add(instanceProductExportExcluded, "PocketBase internal or authentication authority",
		"_authOrigins", "_collections", "_externalAuths", "_mfas", "_migrations", "_otps", "_params",
		"_superusers", "users", "leafen_account_emails", "leafen_unproven_account_emails", "leafen_signup_invitations", "leafen_account_email_challenges", "leafen_passkeys", "leafen_passkey_registrations", "leafen_account_password_state", "leafen_account_preferences", "leafen_account_display_preferences",
		"leafen_account_profile_state", "leafen_account_profile_receipts", "leafen_account_photos", "leafen_switcher_keys",
		"leafen_administrators", "leafen_administrator_account_states", "leafen_account_joins", "leafen_organization_user_suspensions", "leafen_auth_audit_events", "leafen_auth_messages",
		"leafen_auth_mfa_challenges", "leafen_auth_mfa_factors", "leafen_auth_mfa_recovery_codes",
		"leafen_auth_provider_exchanges", "leafen_auth_provider_identities", "leafen_auth_provider_redirects", "leafen_auth_provider_states",
		"leafen_auth_sessions", "leafen_saml_assertions", "leafen_saml_completions", "leafen_saml_domains",
		"leafen_notion_oauth_authorization_codes", "leafen_notion_oauth_grant_resources",
		"leafen_notion_oauth_grants", "leafen_notion_oauth_refresh_tokens",
		"leafen_notion_admin_tokens", "leafen_notion_personal_access_tokens", "leafen_notion_internal_connections", "leafen_notion_internal_connection_owners",
		"leafen_saml_providers", "leafen_saml_requests", "leafen_scim_tokens", "leafen_server_settings",
		"leafen_user_state", "leafen_account_unknown_passwords", "leafen_password_backoff",
		"leafen_agent_sign_in_requests", "leafen_agent_sign_in_off", "leafen_person_steps", "leafen_person_screen_languages",
	)
	add(instanceProductExportExcluded, "this server's own number; another server has its own",
		"leafen_server_identity",
	)
	add(instanceProductExportExcluded, "this server's own way to be reached from outside; another server sets up its own",
		"leafen_server_tunnel", "leafen_server_administrator_addresses",
	)
	add(instanceProductExportExcluded, "what this server's request log keeps; another server keeps its own log",
		"leafen_request_log_settings",
	)
	add(instanceProductExportExcluded, "the proxy this server's network goes out by; another server is on its own network",
		"leafen_network_proxy_settings",
	)
	add(instanceProductExportExcluded, "a person's logins to external services; they connect them again on another server",
		"leafen_mcp_services", "leafen_mcp_service_approvals", "leafen_mcp_connection_services", "leafen_mcp_service_tool_choices",
	)
	add(instanceProductExportExcluded, "installed app code and its server-scoped approvals; a server installs its apps itself",
		"leafen_app_packages", "leafen_app_package_files", "leafen_app_installs", "leafen_app_grants", "leafen_app_audit_events",
		"leafen_app_connections", "leafen_app_delegations", "leafen_app_workspaces", "leafen_app_data",
		"leafen_app_secrets", "leafen_app_relay_log", "leafen_app_platform_settings", "leafen_app_oauth_states", "leafen_app_oauth_tokens",
		"leafen_app_hook_secrets", "leafen_app_hook_events", "leafen_app_runs", "leafen_app_run_versions", "leafen_action_approvals", "leafen_app_schedule_states",
		"leafen_app_guest_settings", "leafen_app_guest_links", "leafen_mcp_connection_apps",
		"leafen_app_catalogs", "leafen_app_catalog_packages", "leafen_app_usage", "leafen_app_data_reads",
		"leafen_app_person_storage", "leafen_app_requests", "leafen_app_allowlist", "leafen_app_consents",
		"leafen_app_setting_values", "leafen_app_credential_policies", "leafen_app_person_credentials", "leafen_app_person_data",
		"leafen_app_approval_allowances", "leafen_app_oversight", "leafen_app_schedule_optouts", "leafen_app_hook_org_secrets",
		"leafen_app_guest_reports", "leafen_app_guest_blocks", "leafen_app_strict_tenancies", "leafen_app_passwords",
		"leafen_app_endpoint_limits", "leafen_app_endpoint_usage", "leafen_app_endpoint_files", "leafen_app_endpoint_public",
	)
	add(instanceProductExportExcluded, "operational continuation, delivery, cache, or rebuildable projection",
		"leafen_file_text_extracts", "leafen_product_afterwork_left",
		"leafen_administrator_account_audit_events", "leafen_administrator_account_cleanup_assignments",
		"leafen_administrator_account_directory_authority",
		"leafen_administrator_settings_audit_events", "leafen_server_settings_audit_events", "leafen_database_query_dependency_values",
		"leafen_release_manifest_state", "leafen_scheduled_backups", "leafen_scheduled_backup_file_hashes", "leafen_data_epoch",
		"leafen_database_mutation_jobs", "leafen_database_relation_property_targets", "leafen_database_view_property_refs",
		"leafen_comment_deletion_frontier", "leafen_comment_deletion_jobs", "leafen_comment_quote_blocks",
		"leafen_comment_page_state", "leafen_comment_receipts", "leafen_notification_events", "leafen_notification_deliveries", "leafen_database_row_notification_fanouts", "leafen_notifications",
		"leafen_notification_read_settlements", "leafen_notification_archive_receipts", "leafen_notification_emails",
		"leafen_page_invite_emails", "leafen_workspace_invitations", "leafen_workspace_name_invitations", "leafen_workspace_invite_links", "leafen_teamspace_invite_links", "leafen_page_guest_links", "leafen_access_requests",
		"leafen_page_share_access_receipts", "leafen_page_share_access_pending",
		"leafen_web_push_subscriptions", "leafen_web_push_deliveries", "leafen_notification_digests",
		"leafen_slack_notification_settings", "leafen_slack_notification_deliveries",
		"leafen_webhook_subscriptions", "leafen_webhook_events", "leafen_webhook_deliveries", "leafen_app_registrations",
		"leafen_integration_accounts", "leafen_view_links",
		"leafen_reminder_dirty_pages", "leafen_page_person_mentions", "leafen_page_person_mention_baselines",
		"leafen_database_row_reminder_dirty", "leafen_database_row_person_mentions", "leafen_database_row_person_mention_baselines",
		"leafen_database_automation_deliveries", "leafen_database_automation_events",
		"leafen_database_automation_mutations", "leafen_database_automation_outcomes",
		"leafen_database_toolbar_mutation_receipts",
		"leafen_database_query_jobs", "leafen_database_query_results", "leafen_notion_view_queries", "leafen_notion_async_tasks",
		"leafen_database_sql_query_jobs", "leafen_database_sql_query_sources",
		"leafen_database_sql_query_rows", "leafen_database_sql_query_results",
		"leafen_enterprise_retention_sweeps", "leafen_enterprise_retention_work", "leafen_file_delete_receipts",
		"leafen_file_maintenance_outcomes", "leafen_file_maintenance_runs",
		"leafen_file_manual_cleanup_jobs", "leafen_file_manual_cleanup_outcomes",
		"leafen_file_reference_cleanups",
		"leafen_file_upload_parts", "leafen_file_uploads", "leafen_file_usage_report_jobs",
		"leafen_file_usage_report_workspace_results", "leafen_form_submission_admission_logical_ids", "leafen_form_file_uploads",
		"leafen_form_submission_admission_windows", "leafen_health_write_probes", "leafen_job_chunks",
		"leafen_job_effects", "leafen_jobs", "leafen_mcp_connection_workspaces", "leafen_mcp_connections", "leafen_mcp_inbox_shown",
		"leafen_mcp_connection_codes", "leafen_mcp_device_connections", "leafen_account_device_codes", "leafen_auth_address_handoffs", "leafen_auth_app_sign_ins", "leafen_mcp_admin_authority_revisions", "leafen_mcp_connection_owner_revisions",
		"leafen_imported_people_mapping_items", "leafen_imported_people_mapping_jobs",
		"leafen_mcp_consent_authority_revision", "leafen_mcp_consent_principal_revisions",
		"leafen_bootstrap_authority_revision", "leafen_access_revision", "leafen_bootstrap_visibility_projection_jobs",
		"leafen_page_list_changes", "leafen_page_list_change_sequence",
		"leafen_people_guest_checks", "leafen_people_guests",
		"leafen_mcp_oauth_authorization_workspaces", "leafen_mcp_oauth_authorizations", "leafen_mcp_oauth_clients", "leafen_mcp_oauth_retired_refresh_tokens",
		"leafen_notion_admin_token_requests",
		"leafen_notion_object_identities", "leafen_page_public_identities", "leafen_row_page_content_cleanup",
		"leafen_child_page_block_places", "leafen_child_page_block_backfill",
		"leafen_public_mutation_effects", "leafen_public_mutation_operations", "leafen_public_share_graph_jobs",
		"leafen_notion_personal_access_token_revisions",
		"leafen_notion_credit_access_revisions",
		"leafen_notion_agent_revisions",
		"leafen_notion_import_connections", "leafen_notion_migrations", "leafen_notion_migration_ledger", "leafen_notion_import_kept_mappings", "leafen_notion_migration_issues", "leafen_notion_migration_scan_items", "leafen_notion_migration_formats", "leafen_notion_import_file_tasks", "leafen_notion_import_retention_runs",
		"leafen_bundle_import_sources", "leafen_bundle_import_ids", "leafen_bundle_import_versions",
		"leafen_organization_audit_events", "leafen_read_log_places", "leafen_workspace_activity",
		"leafen_page_history_packing", "leafen_graph_journal_trim",
		"leafen_organization_audit_export_artifacts", "leafen_organization_audit_exports",
		"leafen_organization_discovery_exports", "leafen_organization_discovery_export_chunks",
		"leafen_organization_list_projection",
		"leafen_organization_storage_policy_receipts",
		"leafen_organization_settings_receipts",
		"leafen_organization_sharing_security_receipts",
		"leafen_block_trash_tombstone_blocks",
		"leafen_block_page_references", "leafen_block_text_page_references", "leafen_page_analytics_batches", "leafen_page_analytics_retention_state", "leafen_page_file_reference_backfill",
		"leafen_database_relation_pair_work",
		"leafen_database_row_page_icon_reference_backfill",
		"leafen_page_duplication_entity_states",
		"leafen_page_duplication_file_state",
		"leafen_page_duplication_files",
		"leafen_page_duplication_identity_map",
		"leafen_page_duplication_custom_emojis",
		"leafen_page_duplication_items",
		"leafen_page_duplication_jobs",
		"leafen_page_workspace_moves", "leafen_page_workspace_move_items",
		"leafen_page_history_restore_receipts",
		"leafen_page_history_block_spans", "leafen_page_history_text_spans",
		"leafen_page_history_snapshot_jobs", "leafen_page_history_snapshot_blocks",
		"leafen_block_copy_database_states", "leafen_block_copy_files", "leafen_block_copy_custom_emojis",
		"leafen_unavailable_database_blocks",
		"leafen_block_move_database_items", "leafen_block_move_database_scopes",
		"leafen_block_move_items", "leafen_block_move_jobs",
		"leafen_page_permanent_delete_items", "leafen_page_permanent_delete_jobs",
		"leafen_trash_empty_items", "leafen_trash_empty_jobs", "leafen_page_trash_retention_waits",
		"leafen_scim_operation_members", "leafen_scim_operations", "leafen_search_dirty", "leafen_search_pages", "leafen_search_chunks", "leafen_search_cell_person_mentions", "leafen_search_pages_fts", "leafen_search_pages_fts_config", "leafen_search_pages_fts_content",
		"leafen_search_pages_fts_data", "leafen_search_pages_fts_docsize", "leafen_search_pages_fts_idx",
		"leafen_search_workspace_state", "leafen_schema_migrations", "leafen_schema_migration_checks",
		"leafen_workspace_content_revisions", "leafen_block_revision_holds",
		"leafen_teamspace_mutation_receipts", "leafen_teamspace_publication_jobs",
		"leafen_workspace_membership_notifications",
		"leafen_workspace_owner_transfer_notifications",
		"leafen_workspace_permission_group_revisions",
		"leafen_workspace_settings_receipts", "leafen_workspace_settings_state", "leafen_workspace_connection_policies", "leafen_managed_user_notices",
		"leafen_workspace_deletion_jobs",
		"leafen_workspace_archive_import_pages", "leafen_workspace_archive_import_targets",
		"leafen_workspace_archive_import_task_settings",
		"leafen_workspace_creation_receipts", "leafen_database_row_body_mutations",
		"leafen_workspace_export_artifacts", "leafen_workspace_export_chunks",
		"leafen_workspace_export_files", "leafen_workspace_export_page_counts",
		"leafen_workspace_export_scope_pages", "leafen_workspace_export_scope_blocks", "leafen_ygo_structural_quarantine",
		"leafen_ygo_update_quarantine", "leafen_ygo_marks", "leafen_instance_product_export_artifacts",
		"leafen_administrator_backup_artifacts", "leafen_instance_report_authority",
		"leafen_instance_administration_reports",
		"leafen_instance_product_export_source_revisions", "leafen_workspace_import_replace_leases",
		"leafen_workspace_import_replace_snapshots", "leafen_workspace_import_sources", "leafen_workspace_import_batch_items",
		"leafen_wiki_mutations", "leafen_wiki_mutation_items", "leafen_wiki_verification_queue",
		"leafen_wiki_verification_email_deliveries",
	)
	add(instanceProductExportExcluded, "execution history is not a portable product-data authority",
		"leafen_interactive_automation_executions",
	)
	return definitions
}

func instanceProductExportIncludedTableNames() []string {
	definitions := instanceProductExportPolicyDefinitions()
	tables := make([]string, 0, len(definitions))
	for table, policy := range definitions {
		if policy.Disposition == instanceProductExportIncluded {
			tables = append(tables, table)
		}
	}
	sort.Strings(tables)
	return tables
}

func loadInstanceProductExportPolicy(app core.App) ([]instanceProductExportTablePolicy, instanceProductExportCoverage, error) {
	var installedVersion int
	if err := app.DB().NewQuery(`SELECT COALESCE(MAX([[version]]), 0) FROM {{leafen_schema_migrations}}`).Row(&installedVersion); err != nil {
		return nil, instanceProductExportCoverage{}, err
	}
	if installedVersion != instanceProductExportPolicySchemaVersion {
		return nil, instanceProductExportCoverage{}, errInstanceProductExportPolicyIncomplete
	}

	tables, err := schemaTableNames(app)
	if err != nil {
		return nil, instanceProductExportCoverage{}, err
	}
	definitions := instanceProductExportPolicyDefinitions()
	// Every table without a class at once, not the first alone, so one fix
	// covers them all; the job's own failure keeps only its code, so the log
	// names them.
	if unclassified := instanceProductExportUnclassified(definitions, tables); len(unclassified) > 0 {
		logUnclassifiedExportTables(app, "leafen instance product export: tables the export policy has no class for", unclassified)
		return nil, instanceProductExportCoverage{}, fmt.Errorf("%w: unclassified tables %s",
			errInstanceProductExportPolicyIncomplete, strings.Join(unclassified, ", "))
	}
	policies := make([]instanceProductExportTablePolicy, 0, len(tables))
	coverage := instanceProductExportCoverage{}
	for _, table := range tables {
		policy := definitions[table]
		entry := instanceProductExportCoverageEntry{Table: table, Reason: policy.Reason}
		if policy.Disposition == instanceProductExportExcluded {
			coverage.Excluded = append(coverage.Excluded, entry)
			policies = append(policies, policy)
			continue
		}
		columns, err := instanceProductExportColumns(app, policy)
		if err != nil {
			return nil, instanceProductExportCoverage{}, err
		}
		policy.Columns = columns
		coverage.Included = append(coverage.Included, entry)
		policies = append(policies, policy)
	}
	sort.Slice(policies, func(left, right int) bool { return policies[left].Table < policies[right].Table })
	return policies, coverage, nil
}

// instanceProductExportUnclassified are the tables, in the order given, that
// the definitions have no class for.
func instanceProductExportUnclassified(definitions map[string]instanceProductExportTablePolicy, tables []string) []string {
	unclassified := []string{}
	for _, table := range tables {
		if _, exists := definitions[table]; !exists {
			unclassified = append(unclassified, table)
		}
	}
	return unclassified
}

func instanceProductExportColumns(app core.App, policy instanceProductExportTablePolicy) ([]string, error) {
	type columnRecord struct {
		Name string `db:"name"`
	}
	rows := []columnRecord{}
	query := `SELECT [[name]] FROM pragma_table_info(` + sqlStringLiteral(policy.Table) + `) ORDER BY [[cid]]`
	if err := app.DB().NewQuery(query).All(&rows); err != nil {
		return nil, err
	}
	columns := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, omitted := policy.OmittedColumns[row.Name]; omitted {
			continue
		}
		normalized := strings.ToLower(strings.ReplaceAll(row.Name, "-", "_"))
		_, reviewed := policy.ReviewedColumns[row.Name]
		for _, fragment := range []string{"password", "secret", "cipher", "credential", "private_key", "token"} {
			if !reviewed && strings.Contains(normalized, fragment) {
				return nil, fmt.Errorf("%w: sensitive column %s.%s", errInstanceProductExportPolicyIncomplete, policy.Table, row.Name)
			}
		}
		columns = append(columns, row.Name)
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("%w: no exportable columns for %s", errInstanceProductExportPolicyIncomplete, policy.Table)
	}
	return columns, nil
}
