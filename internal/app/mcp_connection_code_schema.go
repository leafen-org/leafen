package app

// One-time connection codes: a person issues one for an agent that can't sign
// in with OAuth, and the agent trades it once, within ten minutes, for a
// scoped MCP key. The row stays as the record of who used it and which
// connection it made.

const mcpConnectionCodesTable = "leafen_mcp_connection_codes"
