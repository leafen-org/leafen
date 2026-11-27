package app

const (
	mcpConnectionRetireIndex       = "idx_leafen_mcp_connections_retire_after"
	mcpConnectionOwnerActiveIndex  = "idx_leafen_mcp_connections_owner_active"
	mcpConnectionClientActiveIndex = "idx_leafen_mcp_connections_oauth_client_active"
	mcpOAuthClientLastUsedIndex    = "idx_leafen_mcp_oauth_clients_last_used"
	mcpAuthorizationClientIndex    = "idx_leafen_mcp_oauth_authorizations_client_expiry"
	mcpConnectionCleanupDueIndex   = "idx_leafen_mcp_connections_cleanup_due"
	mcpOAuthClientCleanupDueIndex  = "idx_leafen_mcp_oauth_clients_cleanup_due"
)
