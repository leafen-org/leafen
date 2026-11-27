package app

// An MCP token's key, kept sealed with the server's settings key beside its
// hash (MCP token decision changed 2026-11-13, 1), so its person sees it
// again on its page as Notion's eye shows it (mcp_token_settings.go). The
// hash still checks every call. A revoked connection lets its sealed key go
// at once, whichever of its paths revoked it. A manual key made before keys
// were kept and never refreshed is the one the server's MCP key file makes
// again from its connection's id, so migration 773 seals it and it shows at
// once; one refreshed since is random, can't be made again and shows once
// refreshed.

const mcpTokenSealedColumn = "token_sealed"
