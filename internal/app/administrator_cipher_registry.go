package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type administratorCipherPurpose string

const (
	cipherServerSettings  administratorCipherPurpose = "server-settings"
	cipherAuthMessage     administratorCipherPurpose = "auth-message"
	cipherMFAFactor       administratorCipherPurpose = "mfa-factor"
	cipherProviderState   administratorCipherPurpose = "provider-state"
	cipherSAMLProvider    administratorCipherPurpose = "saml-provider"
	cipherPublicShare     administratorCipherPurpose = "public-share"
	cipherWebhook         administratorCipherPurpose = "webhook"
	cipherScheduledBackup administratorCipherPurpose = "scheduled-backup"
	cipherEmailChallenge  administratorCipherPurpose = "email-challenge"
	cipherAppSecret       administratorCipherPurpose = "app-secret"
	cipherAppOAuthState   administratorCipherPurpose = "app-oauth-state"
	cipherAppOAuthToken   administratorCipherPurpose = "app-oauth-token"
	cipherAppHook         administratorCipherPurpose = "app-hook"
	cipherAppCredential   administratorCipherPurpose = "app-credential"
	cipherPasskeyTicket   administratorCipherPurpose = "passkey-ticket"
	cipherMCPService      administratorCipherPurpose = "mcp-service"
	cipherServerTunnel    administratorCipherPurpose = "server-tunnel"
	cipherNetworkProxy    administratorCipherPurpose = "network-proxy"
	cipherMCPToken        administratorCipherPurpose = "mcp-token"
)

type administratorCipherColumn struct {
	table  string
	column string
}

// Every sealing call names its purpose. Its persisted destinations belong here,
// so adding a purpose without its recovery guard cannot silently create a key.
// Even expired/disabled rows protect the old key until their owner clears them.
var administratorCipherStorage = map[administratorCipherPurpose][]administratorCipherColumn{
	cipherServerSettings: {
		{"leafen_server_settings", "smtp_password_cipher"},
		{"leafen_server_settings", "google_secret_cipher"},
		{"leafen_server_settings", "oidc_secret_cipher"},
		{"leafen_server_settings", "place_search_config_cipher"},
	},
	cipherAuthMessage:   {{"leafen_auth_messages", "token_cipher"}},
	cipherMFAFactor:     {{"leafen_auth_mfa_factors", "secret_cipher"}},
	cipherProviderState: {{"leafen_auth_provider_states", "verifier_cipher"}},
	cipherSAMLProvider:  {{"leafen_saml_providers", "certificate_cipher"}},
	cipherPublicShare:   {{"leafen_public_share_links", "token_cipher"}},
	cipherWebhook:       {{"leafen_webhook_subscriptions", "secret_cipher"}},
	cipherScheduledBackup: {
		{"leafen_scheduled_backups", "s3_secret_cipher"},
		{"leafen_scheduled_backups", "webdav_password_cipher"},
		{"leafen_scheduled_backups", "password_cipher"},
		{"leafen_scheduled_backups", "backup_master_cipher"},
	},
	cipherEmailChallenge: {{"leafen_account_email_challenges", "code_cipher"}},
	cipherAppSecret:      {{"leafen_app_secrets", "sealed"}},
	cipherAppOAuthState:  {{"leafen_app_oauth_states", "verifier"}},
	cipherAppOAuthToken: {
		{"leafen_app_oauth_tokens", "access"},
		{"leafen_app_oauth_tokens", "refresh"},
	},
	cipherAppHook: {
		{"leafen_app_hook_secrets", "sealed"},
		{"leafen_app_hook_org_secrets", "sealed"},
	},
	cipherAppCredential: {{"leafen_app_person_credentials", "sealed"}},
	cipherMCPService:    mcpServiceCipherColumns,
	cipherServerTunnel:  {{"leafen_server_tunnel", "token_cipher"}},
	cipherNetworkProxy:  {{"leafen_network_proxy_settings", "password_cipher"}},
	// An MCP token's key, for its person to see again (mcp_token_sealed_schema.go).
	cipherMCPToken: {{mcpConnectionsTable, mcpTokenSealedColumn}},
	// Login tickets and app-return receipts live only on the client. Losing the
	// key rejects them and the person starts sign-in again; no DB cipher is lost.
	cipherPasskeyTicket: nil,
}

func requireNoAdministratorSecretCiphertext(app core.App) error {
	registered := map[administratorCipherColumn]bool{}
	tables := map[string]bool{}
	for _, columns := range administratorCipherStorage {
		for _, column := range columns {
			registered[column] = true
			tables[column.table] = true
		}
	}
	params := dbx.Params{}
	placeholders := make([]string, 0, len(tables))
	for table := range tables {
		key := fmt.Sprintf("table%d", len(params))
		params[key] = table
		placeholders = append(placeholders, "{:"+key+"}")
	}
	// Some callers run while an older schema is still migrating. Read only
	// existing columns, including a new column on a previously existing table.
	present := []struct {
		Table  string `db:"table_name"`
		Column string `db:"column_name"`
	}{}
	if err := app.DB().NewQuery(`SELECT m.name AS table_name, p.name AS column_name
		FROM sqlite_master AS m JOIN pragma_table_info(m.name) AS p
		WHERE m.type='table' AND m.name IN (` + strings.Join(placeholders, ",") + `)`).Bind(params).All(&present); err != nil {
		return err
	}
	clauses := []string{}
	for _, column := range present {
		if registered[administratorCipherColumn{column.Table, column.Column}] {
			// Separate EXISTS clauses keep each partial presence index usable.
			clauses = append(clauses, "EXISTS(SELECT 1 FROM {{"+column.Table+"}} WHERE [["+column.Column+"]] <> '')")
		}
	}
	if len(clauses) == 0 {
		return nil
	}
	sort.Strings(clauses)
	var found int
	if err := app.DB().NewQuery("SELECT " + strings.Join(clauses, " OR ")).Row(&found); err != nil {
		return err
	}
	if found != 0 {
		return errors.New(errorCodeAdministratorSecretFailed)
	}
	return nil
}
