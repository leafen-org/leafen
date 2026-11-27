package app

// Who may connect agents (installed apps' accounts) to a page, as Notion's
// "에이전트 사용 권한을 부여할 수 있는 사람" (seen 2026-10-11): people with
// full access (the default), or everyone with edit access
// (page_app_connections.go).

const (
	agentGrantScopeFullAccess = "full_access"
	agentGrantScopeAccess     = "access"
)
