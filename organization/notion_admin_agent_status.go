package organization

import "strings"

// notionAdminAgentStatusProjectionSQL is the single set-based owner for the
// externally visible agent run status. Every list, count, single-agent and
// usage projection reads the same current-period usage and effective limit.
func notionAdminAgentStatusProjectionSQL() string {
	limit := `CASE
		WHEN [[agent]].[[admin_credit_limit]] IS NOT NULL
			AND [[agent]].[[creator_credit_limit]] IS NOT NULL
			THEN min([[agent]].[[admin_credit_limit]], [[agent]].[[creator_credit_limit]])
		WHEN [[agent]].[[admin_credit_limit]] IS NOT NULL THEN [[agent]].[[admin_credit_limit]]
		WHEN [[agent]].[[creator_credit_limit]] IS NOT NULL THEN [[agent]].[[creator_credit_limit]]
		ELSE [[settings]].[[default_agent_credit_limit]] END`
	return `WITH [[agent_status_basis]] AS (
		SELECT [[agent]].*,
			(SELECT SUM([[usage]].[[credits_used]])
			 FROM {{leafen_notion_agent_usage_periods}} [[usage]]
			 WHERE [[usage]].[[agent_id]] = [[agent]].[[id]]
				AND [[usage]].[[metrics_period_start_ms]] <= {:statusNow}
				AND [[usage]].[[metrics_period_end_ms]] > {:statusNow}) AS [[current_credit_usage]],
			` + limit + ` AS [[enforced_credit_limit]],
			([[settings]].[[default_agent_credit_limit]] IS NOT NULL
			 AND [[agent]].[[admin_credit_limit]] IS NULL
			 AND [[agent]].[[creator_credit_limit]] IS NULL)
				AS [[workspace_credit_limit_owner]],
			[[settings]].[[creation_policy]] AS [[workspace_creation_policy]],
			[[settings]].[[disable_generation]] AS [[workspace_disable_generation]]
		FROM {{leafen_notion_agents}} [[agent]]
		JOIN {{leafen_notion_agent_settings}} [[settings]]
			ON [[settings]].[[workspace_id]] = [[agent]].[[workspace_id]]
	), [[projected_agent]] AS (
		SELECT [[agent_status_basis]].*, CASE
			WHEN [[deleted_at_ms]] IS NOT NULL THEN 'deleted'
			WHEN [[admin_status]] = 'disabled' THEN 'disabled_from_api'
			WHEN [[agent_type]] = 'custom'
				AND [[acknowledged_disable_generation]] < [[workspace_disable_generation]]
				THEN CASE WHEN [[workspace_creation_policy]] = 'disabled'
					THEN 'disabled_from_workspace_settings'
					ELSE 'disabled_from_workspace_settings_but_resumable' END
			WHEN [[current_credit_usage]] IS NOT NULL AND [[enforced_credit_limit]] IS NOT NULL
				AND [[current_credit_usage]] >= [[enforced_credit_limit]]
				THEN CASE WHEN [[workspace_credit_limit_owner]]
					THEN 'workspace_credit_limit' ELSE 'credit_limit' END
			ELSE 'active' END AS [[effective_status]]
		FROM [[agent_status_basis]]
	) `
}

func notionAdminAgentStatusResponseFor(agent storedNotionAdminAgent) notionAdminAgentStatusResponse {
	response := notionAdminAgentStatusResponse{AdminStatus: agent.AdminStatus, RunStatus: "paused"}
	if agent.EffectiveStatus == "active" {
		response.RunStatus = "active"
		return response
	}
	response.PausedReason = agent.EffectiveStatus
	return response
}

func notionAdminAgentStatusClause(statuses []string, params map[string]any) string {
	parts := make([]string, 0, len(statuses))
	for index, status := range statuses {
		key := "status" + string(rune('a'+index))
		params[key] = status
		parts = append(parts, "{:"+key+"}")
	}
	joined := strings.Join(parts, ",")
	return `([[agent]].[[effective_status]] IN (` + joined + `)
		OR ('disabled' IN (` + joined + `) AND [[agent]].[[effective_status]] = 'disabled_from_api'))`
}
