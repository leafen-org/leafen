package app

// Notion's MCP tools Leafen doesn't have (mcp_tool_unsupported.go) — folders,
// skills, agents, sessions, calendar, meeting notes, AI search and the like,
// 34 on 2026-11-16 — answer only that Leafen doesn't have them. An agent choosing among
// its tools read them for nothing and tried them (2026-11-16: 28% of the
// list), so a token lists them only when its page turns on "Notion에만 있는
// 도구 이름" (decided 2026-11-16 by the command session): a connection that
// needs every Notion name, such as an agent written against Notion's list.
// Called by name, each still answers why and what to use instead.

// mcpNotionOnlyToolHidden says whether the connection's list leaves the tool
// out: one of Notion's Leafen doesn't have, its page not having turned them on.
func mcpNotionOnlyToolHidden(authority mcpAuthority, name string) bool {
	return !authority.NotionOnlyTools && notionMCPToolUnsupported(name)
}
