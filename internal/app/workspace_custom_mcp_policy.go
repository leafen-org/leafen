package app

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's "사용자 지정 MCP 서버 활성화하기" under 연결 → 관리 → 사용 권한
// 관리하기 (seen 2026-10-02 and 2026-10-13: on by default, letting members
// connect agents to custom MCP servers Notion hasn't reviewed), and
// the list of servers members may connect that its help describes (read
// 2026-10-12): a workspace's owner turns custom MCP servers off for its
// people, or keeps a list of the servers they may connect. A person's
// services are their own across their workspaces (mcp_services.go), so the
// narrowest policy of the workspaces they belong to — as its owner or a
// member, not as a guest — decides: a server is refused where any of them
// turned custom servers off or keeps a list that doesn't name it, when it is
// added, signed in to again, and each time it is used.

const (
	maxCustomMCPAllowlist            = 100
	errorCodeMCPServicePolicyBlocked = "LEAFEN_MCP_SERVICE_POLICY_BLOCKED"
)

var errMCPServicePolicyBlocked = errors.New(errorCodeMCPServicePolicyBlocked)

const mcpServicePolicyBlockedText = "A workspace you're in doesn't let its people connect agents to this MCP server. Its owner allows custom MCP servers in Settings → Connections."

// normalizeCustomMCPAllowlist reads the servers a workspace's list names:
// https addresses (the server's own test loopback too), each once.
func normalizeCustomMCPAllowlist(entries []string) ([]string, bool) {
	if len(entries) > maxCustomMCPAllowlist {
		return nil, false
	}
	normalized := make([]string, 0, len(entries))
	seen := map[string]bool{}
	for _, raw := range entries {
		entry, ok := normalizeCustomMCPAllowlistEntry(raw)
		if !ok {
			return nil, false
		}
		if !seen[entry] {
			seen[entry] = true
			normalized = append(normalized, entry)
		}
	}
	return normalized, true
}

func normalizeCustomMCPAllowlistEntry(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	scheme, host := strings.ToLower(parsed.Scheme), strings.ToLower(parsed.Host)
	if scheme != "https" && (scheme != "http" || host != mcpServiceAllowedLoopback()) {
		return "", false
	}
	return scheme + "://" + host + strings.TrimRight(parsed.EscapedPath(), "/"), true
}

// customMCPAllowlistNames is whether a list names the server: its address,
// or a place above it on the same host.
func customMCPAllowlistNames(allowlist []string, serviceURL string) bool {
	service := strings.ToLower(serviceURL)
	for _, entry := range allowlist {
		if service == entry || strings.HasPrefix(service, entry+"/") || strings.HasPrefix(service, entry+"?") {
			return true
		}
	}
	return false
}

// refuseMCPServiceByPolicy refuses a person's custom MCP server where a
// workspace they belong to doesn't let its people connect it.
func refuseMCPServiceByPolicy(app core.App, userID, serviceURL string) error {
	policies := []struct {
		Enabled   bool   `db:"custom_mcp_servers"`
		Allowlist string `db:"custom_mcp_allowlist"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[policy]].[[custom_mcp_servers]], [[policy]].[[custom_mcp_allowlist]]
		FROM {{leafen_workspace_connection_policies}} [[policy]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[policy]].[[workspace_id]]
		WHERE [[workspace]].[[deletion_job_id]] = ''
			AND ([[policy]].[[custom_mcp_servers]] = 0 OR [[policy]].[[custom_mcp_allowlist]] != '[]')
			AND ([[workspace]].[[workspace_owner]] = {:userId} OR EXISTS (
				SELECT 1 FROM {{leafen_workspace_memberships}} [[membership]]
				WHERE [[membership]].[[workspace_id]] = [[workspace]].[[id]] AND [[membership]].[[user_id]] = {:userId}
					AND [[membership]].[[role]] = 'member'))
	`).Bind(dbx.Params{"userId": userID}).All(&policies); err != nil {
		return err
	}
	for _, policy := range policies {
		if !policy.Enabled {
			return errMCPServicePolicyBlocked
		}
		allowlist := []string{}
		if json.Unmarshal([]byte(policy.Allowlist), &allowlist) != nil || len(allowlist) > 0 && !customMCPAllowlistNames(allowlist, serviceURL) {
			return errMCPServicePolicyBlocked
		}
	}
	return nil
}
