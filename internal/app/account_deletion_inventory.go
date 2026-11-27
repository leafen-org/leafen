package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Deleting an account keeps its users record, given an email and a password
// no one knows (cleanupAdministratorAccountData), so no foreign key to users
// takes a person's rows with it: each table that keeps rows by person says
// what deleting the account does to them, a living workspace's tables too
// (account_deletion_workspace_inventory.go). A table without a class stops a
// development build's start and refuses a deletion in a release build
// (deletion_inventories_start.go), as the workspace deletion inventory and
// the export classes do: none leaves that table's rows behind unnoticed
// (2026-11-17: the agent sign-in requests kept a deleted person's id, and
// mail and pushes queued for a deleted person waited in their workspaces).

// accountDeletionPersonKeys are the columns that say whose row it is. Who
// made or changed a row (created_by, actor_id, author_id …) is not among
// them: that stays with the row as the person's part in it.
var accountDeletionPersonKeys = []string{"user_id", "owner_id", "principal_id", "person_id", "user", "owner", "requester_id"}

const (
	// A lane of the cleanup (administratorAccountCleanupTables) deletes them.
	accountDeletionCleared = "account_cleanup"
	// The cleanup's last step deletes them (administratorAccountLastStepTables).
	accountDeletionLastStep = "account_last_step"
	// The account's own state and counters, kept with its users record:
	// a revision never goes back, and the account stays known as deleted.
	accountDeletionAccountRecord = "account_record"
	// Rows of a parent record (a page, a comment, an app's install …) that
	// go when it goes; checked against the schema's cascades (a workspace
	// is no such parent: it outlives the person).
	accountDeletionWithParent = "with_parent"
	// What others keep of the person: audit lines, logs, receipts and page
	// history, artifacts made for the server or an organization, and an
	// app or connection others still use.
	accountDeletionKeptRecord = "kept_record"
	// Passes to a successor, or goes with the account's own workspaces
	// (the cleanup's workspace and organization steps).
	accountDeletionTransferred = "transferred"
)

type accountDeletionPersonTable struct {
	Table       string
	Key         string
	Disposition string
}

// accountDeletionInventory names each table that keeps rows by person and
// has no workspace key; accountDeletionWorkspaceInventory those with one.
var accountDeletionInventory = []accountDeletionPersonTable{
	{Table: "leafen_account_device_codes", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_account_display_preferences", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_account_email_challenges", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_account_emails", Key: "user_id", Disposition: accountDeletionLastStep},
	{Table: "leafen_account_password_state", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_account_photos", Key: "user_id", Disposition: accountDeletionLastStep},
	{Table: "leafen_account_preferences", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_account_profile_receipts", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_account_profile_state", Key: "user_id", Disposition: accountDeletionAccountRecord},
	{Table: "leafen_account_unknown_passwords", Key: "owner_id", Disposition: accountDeletionAccountRecord},
	{Table: "leafen_action_approvals", Key: "person_id", Disposition: accountDeletionCleared},
	{Table: "leafen_administrator_account_states", Key: "user_id", Disposition: accountDeletionAccountRecord},
	{Table: "leafen_administrator_backup_artifacts", Key: "owner", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_administrators", Key: "user_id", Disposition: accountDeletionLastStep},
	{Table: "leafen_agent_sign_in_off", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_agent_sign_in_requests", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_app_approval_allowances", Key: "person_id", Disposition: accountDeletionCleared},
	{Table: "leafen_app_connections", Key: "user_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_app_consents", Key: "person_id", Disposition: accountDeletionCleared},
	{Table: "leafen_app_data_reads", Key: "person_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_app_delegations", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_app_endpoint_files", Key: "user_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_app_hook_events", Key: "person_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_app_oauth_states", Key: "person_id", Disposition: accountDeletionCleared},
	{Table: "leafen_app_oauth_tokens", Key: "person_id", Disposition: accountDeletionCleared},
	{Table: "leafen_app_passwords", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_app_person_credentials", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_app_person_data", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_app_person_storage", Key: "person_id", Disposition: accountDeletionCleared},
	// Others' connections made with it end when it goes (app_registrations.go).
	{Table: "leafen_app_registrations", Key: "owner_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_app_relay_log", Key: "person_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_app_requests", Key: "person_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_app_schedule_optouts", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_auth_address_handoffs", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_auth_app_sign_ins", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_auth_audit_events", Key: "owner_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_auth_messages", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_auth_mfa_challenges", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_auth_mfa_factors", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_auth_mfa_recovery_codes", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_auth_provider_exchanges", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_auth_provider_identities", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_auth_sessions", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_comment_reactions", Key: "user_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_comment_thread_states", Key: "user_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_graph_changes", Key: "owner", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_graph_mutations", Key: "owner", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_instance_administration_reports", Key: "owner", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_instance_product_export_artifacts", Key: "owner", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_managed_user_notices", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_mcp_connection_codes", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_mcp_connection_owner_revisions", Key: "owner_id", Disposition: accountDeletionAccountRecord},
	// A key acts only while its account may (accountMayAct); organization
	// connection lists keep its line.
	{Table: "leafen_mcp_connections", Key: "owner_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_mcp_consent_principal_revisions", Key: "principal_id", Disposition: accountDeletionAccountRecord},
	{Table: "leafen_mcp_device_connections", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_mcp_oauth_authorizations", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_mcp_service_approvals", Key: "person_id", Disposition: accountDeletionCleared},
	{Table: "leafen_mcp_services", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_mutation_receipts", Key: "owner", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_notification_digests", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_notification_settings", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_notion_agent_permissions", Key: "principal_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_notion_oauth_authorization_codes", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_organization_admin_roles", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_organization_discovery_exports", Key: "owner_id", Disposition: accountDeletionKeptRecord},
	{Table: "leafen_organization_list_projection", Key: "user_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_organization_memberships", Key: "user", Disposition: accountDeletionCleared},
	{Table: "leafen_organization_user_suspensions", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_organizations", Key: "owner", Disposition: accountDeletionTransferred},
	{Table: "leafen_page_person_mentions", Key: "user_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_passkey_registrations", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_passkeys", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_password_backoff", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_person_steps", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_person_screen_languages", Key: "owner_id", Disposition: accountDeletionCleared},
	{Table: "leafen_saml_assertions", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_saml_completions", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_search_cell_person_mentions", Key: "user_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_slack_notification_settings", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_switcher_keys", Key: "user_id", Disposition: accountDeletionLastStep},
	{Table: "leafen_unproven_account_emails", Key: "user_id", Disposition: accountDeletionCleared},
	// The page metadata cursor of the workspaces it owned counts on.
	{Table: "leafen_user_state", Key: "owner", Disposition: accountDeletionAccountRecord},
	{Table: "leafen_web_push_subscriptions", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_wiki_page_owners", Key: "user_id", Disposition: accountDeletionWithParent},
	{Table: "leafen_workspace_permission_group_members", Key: "user_id", Disposition: accountDeletionCleared},
	{Table: "leafen_workspaces", Key: "owner", Disposition: accountDeletionTransferred},
}

type administratorAccountCleanupTable struct {
	Table string
	Key   string
	// Workspace names the workspace column of a table read by workspace and
	// then person, as its index leads: the lane reads it a workspace at a
	// time rather than the whole table.
	Workspace string
	// Where keeps the lane to the rows it takes; the others are records.
	Where string
}

// administratorAccountCleanupTables are the cleanup's lanes, one table each,
// in the order a cleanup takes them; its job keeps the index of the lane it
// is on, so a new one goes at the end.
var administratorAccountCleanupTables = []administratorAccountCleanupTable{
	{Table: "leafen_workspace_memberships", Key: "user_id"},
	{Table: "leafen_organization_memberships", Key: "user"},
	{Table: "leafen_account_preferences", Key: "owner_id"},
	{Table: "leafen_account_display_preferences", Key: "owner_id"},
	{Table: "leafen_sidebar_layouts", Key: "owner_id"},
	{Table: "leafen_my_task_sources", Key: "owner_id"},
	{Table: "leafen_form_favorites", Key: "owner_id"},
	{Table: "leafen_my_task_views", Key: "owner_id"},
	{Table: "leafen_page_visits", Key: "owner_id"},
	{Table: "leafen_account_profile_receipts", Key: "user_id"},
	{Table: "leafen_account_password_state", Key: "owner_id"},
	{Table: "leafen_auth_sessions", Key: "owner_id"},
	{Table: "leafen_auth_provider_exchanges", Key: "user_id"},
	{Table: "leafen_auth_mfa_recovery_codes", Key: "owner_id"},
	{Table: "leafen_auth_mfa_challenges", Key: "owner_id"},
	{Table: "leafen_auth_mfa_factors", Key: "owner_id"},
	{Table: "leafen_auth_messages", Key: "user_id"},
	{Table: "leafen_saml_assertions", Key: "user_id"},
	{Table: "leafen_saml_completions", Key: "user_id"},
	{Table: "leafen_organization_user_suspensions", Key: "user_id"},
	{Table: "leafen_managed_user_notices", Key: "user_id"},
	{Table: "leafen_mcp_connection_codes", Key: "owner_id"},
	// A deleted person keeps no page grant or favorite: share windows would
	// show the internal address left in their place.
	{Table: "leafen_page_permissions", Key: "principal_id"},
	{Table: "leafen_page_favorites", Key: "principal_id"},
	{Table: "leafen_mcp_device_connections", Key: "owner_id"},
	// The person's sign-in ways, settings, devices and what apps and outside
	// services keep for them (2026-11-17, accountDeletionInventory).
	{Table: "leafen_agent_sign_in_requests", Key: "owner_id"},
	{Table: "leafen_agent_sign_in_off", Key: "user_id"},
	{Table: "leafen_person_steps", Key: "owner_id"},
	{Table: "leafen_account_device_codes", Key: "owner_id"},
	{Table: "leafen_account_email_challenges", Key: "user_id"},
	{Table: "leafen_unproven_account_emails", Key: "user_id"},
	{Table: "leafen_auth_address_handoffs", Key: "owner_id"},
	{Table: "leafen_auth_app_sign_ins", Key: "owner_id"},
	{Table: "leafen_auth_provider_identities", Key: "user_id"},
	{Table: "leafen_passkey_registrations", Key: "user_id"},
	{Table: "leafen_passkeys", Key: "user_id"},
	{Table: "leafen_password_backoff", Key: "user_id"},
	{Table: "leafen_notification_settings", Key: "user_id"},
	{Table: "leafen_notification_digests", Key: "user_id"},
	{Table: "leafen_slack_notification_settings", Key: "user_id"},
	{Table: "leafen_web_push_subscriptions", Key: "user_id"},
	{Table: "leafen_organization_admin_roles", Key: "user_id"},
	{Table: "leafen_mcp_oauth_authorizations", Key: "owner_id"},
	{Table: "leafen_notion_oauth_authorization_codes", Key: "owner_id"},
	{Table: "leafen_mcp_service_approvals", Key: "person_id"},
	{Table: "leafen_mcp_services", Key: "user_id"},
	{Table: "leafen_action_approvals", Key: "person_id"},
	{Table: "leafen_app_approval_allowances", Key: "person_id"},
	{Table: "leafen_app_consents", Key: "person_id"},
	{Table: "leafen_app_delegations", Key: "user_id"},
	{Table: "leafen_app_oauth_states", Key: "person_id"},
	{Table: "leafen_app_oauth_tokens", Key: "person_id"},
	{Table: "leafen_app_passwords", Key: "user_id"},
	{Table: "leafen_app_person_credentials", Key: "user_id"},
	{Table: "leafen_app_person_data", Key: "user_id"},
	{Table: "leafen_app_person_storage", Key: "person_id"},
	{Table: "leafen_app_schedule_optouts", Key: "user_id"},
	// What a living workspace kept only for them: the mail, pushes and Slack
	// messages waiting for them and their inbox, their settings, favorites,
	// profile and unlocks, their places in its pages, properties and groups,
	// the requests they made and their Notion import token (2026-11-17,
	// account_deletion_workspace_inventory.go).
	{Table: "leafen_notification_emails", Key: "user_id"},
	{Table: "leafen_web_push_deliveries", Key: "user_id"},
	{Table: "leafen_slack_notification_deliveries", Key: "user_id"},
	{Table: "leafen_wiki_verification_email_deliveries", Key: "user_id"},
	{Table: "leafen_workspace_membership_notifications", Key: "user_id"},
	{Table: "leafen_workspace_owner_transfer_notifications", Key: "user_id"},
	{Table: "leafen_notifications", Key: "user_id", Workspace: "workspace_id"},
	{Table: "leafen_page_notification_preferences", Key: "user_id"},
	{Table: "leafen_database_notification_preferences", Key: "user_id"},
	{Table: "leafen_workspace_person_profiles", Key: "user_id", Workspace: "workspace_id"},
	{Table: "leafen_database_favorites", Key: "principal_id"},
	{Table: "leafen_page_lock_unlocks", Key: "principal_id"},
	{Table: "leafen_page_link_visitors", Key: "user_id"},
	{Table: "leafen_page_share_access_pending", Key: "principal_id"},
	{Table: "leafen_page_share_access_receipts", Key: "principal_id"},
	{Table: "leafen_database_property_access_exceptions", Key: "principal_id"},
	{Table: "leafen_workspace_permission_group_members", Key: "user_id"},
	{Table: "leafen_mcp_enterprise_member_access", Key: "user_id"},
	{Table: "leafen_access_requests", Key: "requester_id", Where: "[[status]] = 'pending'"},
	{Table: "leafen_account_joins", Key: "user_id"},
	{Table: "leafen_notion_import_connections", Key: "owner"},
	// The language they last read Leafen in on a screen (person_screen_language.go).
	{Table: "leafen_person_screen_languages", Key: "owner_id"},
}

// administratorAccountLastStepTables are the rows the cleanup's last step
// deletes as it marks the account deleted.
var administratorAccountLastStepTables = []administratorAccountCleanupTable{
	{Table: "leafen_account_emails", Key: "user_id"},
	{Table: "leafen_account_photos", Key: "user_id"},
	{Table: "leafen_switcher_keys", Key: "user_id"},
	{Table: "leafen_administrators", Key: "user_id"},
}

var errAccountDeletionInventory = errors.New("account deletion inventory doesn't fit this schema")

type accountDeletionSchemaKey struct {
	Table       string `db:"table_name"`
	Key         string `db:"key_name"`
	ByWorkspace bool   `db:"by_workspace"`
}

// validateAccountDeletionInventory checks both inventories against the
// schema both ways: each table that keeps rows by person is named once with
// its class, and nothing else is; each lane and last-step table is there
// with its key and an index serving its read (account_deletion_lane_indexes.go);
// a cleared table has its lane; a table said to go with its parent has a
// cascade to one, and one said to expire a time it expires at.
func validateAccountDeletionInventory(app core.App) error {
	actual := []accountDeletionSchemaKey{}
	if err := app.DB().NewQuery(`
		SELECT [[schema]].[[name]] AS [[table_name]], [[field]].[[name]] AS [[key_name]],
			EXISTS (SELECT 1 FROM pragma_table_info([[schema]].[[name]]) [[place]]
				WHERE [[place]].[[name]] IN ('workspace', 'workspace_id')) AS [[by_workspace]]
		FROM [[sqlite_master]] [[schema]]
		JOIN pragma_table_info([[schema]].[[name]]) [[field]]
		WHERE [[schema]].[[type]] = 'table'
			AND [[field]].[[name]] IN ('` + strings.Join(accountDeletionPersonKeys, "', '") + `')
		ORDER BY [[schema]].[[name]], [[field]].[[cid]]
	`).All(&actual); err != nil {
		return err
	}
	inSchema := make(map[string]bool, len(actual))
	for _, key := range actual {
		inSchema[key.Table+"."+key.Key] = key.ByWorkspace
	}
	expected := make(map[string]string, len(accountDeletionInventory)+len(accountDeletionWorkspaceInventory))
	for inventory, byWorkspace := range map[*[]accountDeletionPersonTable]bool{
		&accountDeletionInventory: false, &accountDeletionWorkspaceInventory: true,
	} {
		for _, table := range *inventory {
			// The per-person page list went with migration 613.
			if table.Table == bootstrapPageVisibilityTable && !bootstrapPageVisibilityKept(app) || !retiredAreaTableHeld(app, table.Table) {
				continue
			}
			key := table.Table + "." + table.Key
			if _, duplicate := expected[key]; duplicate {
				return fmt.Errorf("%w: %s is named twice", errAccountDeletionInventory, key)
			}
			if kept, found := inSchema[key]; !found || kept != byWorkspace {
				return fmt.Errorf("%w: the inventory names %s, which the schema hasn't or keeps otherwise", errAccountDeletionInventory, key)
			}
			expected[key] = table.Disposition
		}
	}
	for _, key := range actual {
		if _, named := expected[key.Table+"."+key.Key]; !named {
			return fmt.Errorf("%w: %s.%s says nothing of deleting an account", errAccountDeletionInventory, key.Table, key.Key)
		}
	}
	columns, err := accountDeletionTableColumns(app)
	if err != nil {
		return err
	}
	for disposition, tables := range map[string][]administratorAccountCleanupTable{
		accountDeletionCleared: administratorAccountCleanupTables, accountDeletionLastStep: administratorAccountLastStepTables,
	} {
		listed := map[string]bool{}
		for _, table := range tables {
			key := table.Table + "." + table.Key
			if _, found := inSchema[key]; !found || listed[key] {
				return fmt.Errorf("%w: %s is a cleanup table the schema hasn't, or named twice", errAccountDeletionInventory, key)
			}
			listed[key] = true
			if expected[key] != disposition {
				return fmt.Errorf("%w: %s is deleted by the cleanup but classed %q", errAccountDeletionInventory, key, expected[key])
			}
			if table.Workspace != "" && (table.Where != "" || !columns[table.Table+"."+table.Workspace]) {
				return fmt.Errorf("%w: %s is read by a workspace column it hasn't", errAccountDeletionInventory, key)
			}
		}
		for key, class := range expected {
			if class == disposition && !listed[key] {
				return fmt.Errorf("%w: %s is classed %q but the cleanup doesn't delete it", errAccountDeletionInventory, key, class)
			}
		}
	}
	for key, class := range expected {
		table := strings.SplitN(key, ".", 2)[0]
		if class == accountDeletionExpiring && !columns[table+".expires_at"] && !columns[table+".expires"] {
			return fmt.Errorf("%w: %s is said to expire but keeps no time it does", errAccountDeletionInventory, key)
		}
	}
	if err := validateAccountDeletionParents(app, expected); err != nil {
		return err
	}
	return validateAccountDeletionLaneIndexes(app)
}

// accountDeletionTableColumns says which tables have the columns a lane
// reads by workspace and the times a row expires at, as "table.column".
func accountDeletionTableColumns(app core.App) (map[string]bool, error) {
	rows := []accountDeletionSchemaKey{}
	if err := app.DB().NewQuery(`
		SELECT [[schema]].[[name]] AS [[table_name]], [[field]].[[name]] AS [[key_name]], 0 AS [[by_workspace]]
		FROM [[sqlite_master]] [[schema]]
		JOIN pragma_table_info([[schema]].[[name]]) [[field]]
		WHERE [[schema]].[[type]] = 'table'
			AND [[field]].[[name]] IN ('workspace', 'workspace_id', 'expires_at', 'expires')
	`).All(&rows); err != nil {
		return nil, err
	}
	columns := make(map[string]bool, len(rows))
	for _, row := range rows {
		columns[row.Table+"."+row.Key] = true
	}
	return columns, nil
}

// validateAccountDeletionParents checks that each table said to go with its
// parent has a foreign key that deletes its rows with a record other than
// the account's own, which stays, or a workspace, which outlives it.
func validateAccountDeletionParents(app core.App, expected map[string]string) error {
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
	withParent := map[string]bool{}
	for _, reference := range references {
		if reference.OnDelete == "CASCADE" && reference.Parent != "users" && reference.Parent != workspacesCollection {
			withParent[reference.Table] = true
		}
	}
	for key, disposition := range expected {
		if table := strings.SplitN(key, ".", 2)[0]; disposition == accountDeletionWithParent && !withParent[table] {
			return fmt.Errorf("%w: %s goes with its parent by no cascade", errAccountDeletionInventory, key)
		}
	}
	return nil
}
