package organization

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedNotionAdminAgentUsagePeriod struct {
	AgentID              string `db:"agent_id"`
	MetricsPeriodStartMS int64  `db:"metrics_period_start_ms"`
	MetricsPeriodEndMS   int64  `db:"metrics_period_end_ms"`
	CreditsUsed          int64  `db:"credits_used"`
}

type notionAdminAgentUsagePeriod struct {
	CreditsUsed          int64 `json:"credits_used"`
	MetricsPeriodStartMS int64 `json:"metrics_period_start_ms"`
	MetricsPeriodEndMS   int64 `json:"metrics_period_end_ms"`
}

type notionAdminAgentUsageResponse struct {
	CreditPerBillingPeriodList []notionAdminAgentUsagePeriod `json:"credit_per_billing_period_list"`
	HasMore                    bool                          `json:"has_more"`
	ID                         string                        `json:"id"`
	RunsCompleted              int64                         `json:"runs_completed"`
	Status                     string                        `json:"status"`
	TotalCreditsUsed           int64                         `json:"total_credits_used"`
	Type                       string                        `json:"type"`
	CreatedByID                string                        `json:"created_by_id,omitempty"`
	CreditLimit                *int64                        `json:"credit_limit,omitempty"`
	LastCreditUsageTime        *int64                        `json:"last_credit_usage_time,omitempty"`
	Name                       string                        `json:"name,omitempty"`
	NextCursor                 *string                       `json:"next_cursor,omitempty"`
}

type notionAdminAgentsUsageResponse struct {
	Results    []notionAdminAgentUsageResponse `json:"results"`
	HasMore    bool                            `json:"has_more"`
	NextCursor *string                         `json:"next_cursor,omitempty"`
}

type notionAdminAgentUsageTotal struct {
	Credits int64
}

func serveNotionAdminAgentCreditUsage(
	event *core.RequestEvent, secret string, identity leafen.NotionAdminIdentity, workspaceID, agentID string,
) error {
	if event.Request.ContentLength > 0 {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "This request takes no body."))
	}
	pageSize, rawCursor, err := notionAdminAgentSimpleQuery(event.Request, "start_cursor", false)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	filterHash := notionAdminAgentCanonicalQueryHash(event.Request.URL.Query(), "start_cursor")
	cursor, err := decodeNotionAdminAgentCursor(
		rawCursor, secret, identity, workspaceID, agentID, "agent_usage", filterHash,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	response, err := readNotionAdminAgentUsage(
		event.Request.Context(), event.App, secret, identity, workspaceID, agentID,
		cursor, filterHash, pageSize,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func serveNotionAdminAgentsCreditUsage(
	event *core.RequestEvent, secret string, identity leafen.NotionAdminIdentity, workspaceID string,
) error {
	if event.Request.ContentLength > 0 {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "This request takes no body."))
	}
	pageSize, rawCursor, includeDeleted, from, to, err := notionAdminAgentsUsageQuery(event.Request)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	filterHash := notionAdminAgentCanonicalQueryHash(event.Request.URL.Query(), "cursor", "page_size")
	cursor, err := decodeNotionAdminAgentCursor(
		rawCursor, secret, identity, workspaceID, "", "agents_usage", filterHash,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	response, err := listNotionAdminAgentsUsage(
		event.Request.Context(), event.App, secret, identity, workspaceID,
		cursor, filterHash, pageSize, includeDeleted, from, to,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func notionAdminAgentsUsageQuery(request *http.Request) (int, string, bool, *int64, *int64, error) {
	query := request.URL.Query()
	for key, values := range query {
		if !governanceOneOf(key, "cursor", "page_size", "include_deleted", "credit_usage_time_from", "credit_usage_time_to") || len(values) != 1 {
			return 0, "", false, nil, nil, errInvalidNotionAdminAgentCursor
		}
	}
	pageSize := notionAdminAgentMaximumPageSize
	if raw := query.Get("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > notionAdminAgentMaximumPageSize {
			return 0, "", false, nil, nil, errInvalidNotionAdminAgentCursor
		}
		pageSize = parsed
	}
	includeDeleted := false
	if raw := query.Get("include_deleted"); raw != "" {
		if raw != "true" && raw != "false" {
			return 0, "", false, nil, nil, errInvalidNotionAdminAgentCursor
		}
		includeDeleted = raw == "true"
	}
	var from, to *int64
	for key, target := range map[string]**int64{
		"credit_usage_time_from": &from, "credit_usage_time_to": &to,
	} {
		if raw := query.Get(key); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				return 0, "", false, nil, nil, errInvalidNotionAdminAgentCursor
			}
			*target = &parsed
		}
	}
	return pageSize, query.Get("cursor"), includeDeleted, from, to, nil
}

func readNotionAdminAgentUsage(
	ctx context.Context, app core.App, secret string, identity leafen.NotionAdminIdentity,
	workspaceID, agentID string, cursor notionAdminAgentCursor, filterHash string, pageSize int,
) (notionAdminAgentUsageResponse, error) {
	response := notionAdminAgentUsageResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		authority, err := loadNotionAdminAgentAuthority(ctx, txApp, identity.OrganizationID, workspaceID)
		if err != nil {
			return err
		}
		if !notionAdminAgentCursorMatchesAuthority(cursor, authority) {
			return errInvalidNotionAdminAgentCursor
		}
		agent, err := loadStoredNotionAdminAgent(ctx, txApp, workspaceID, agentID)
		if err != nil {
			return err
		}
		after := int64(math.MaxInt64)
		if cursor.Version != 0 {
			after, err = strconv.ParseInt(cursor.AfterID, 10, 64)
			if err != nil || after < 0 {
				return errInvalidNotionAdminAgentCursor
			}
		}
		periods, hasMore, totals, err := loadNotionAdminAgentUsagePeriods(
			ctx, txApp, []string{agentID}, after, nil, nil, pageSize,
		)
		if err != nil {
			return err
		}
		response, err = projectNotionAdminAgentUsage(authority, agent, periods[agentID], totals[agentID])
		if err != nil {
			return err
		}
		response.HasMore = hasMore[agentID]
		if response.HasMore {
			last := periods[agentID][len(periods[agentID])-1].MetricsPeriodStartMS
			next, err := encodeNotionAdminAgentCursor(
				secret, identity, workspaceID, agentID, "agent_usage", authority,
				filterHash, strconv.FormatInt(last, 10),
			)
			if err != nil {
				return err
			}
			response.NextCursor = &next
		}
		return nil
	})
	return response, err
}

func listNotionAdminAgentsUsage(
	ctx context.Context, app core.App, secret string, identity leafen.NotionAdminIdentity,
	workspaceID string, cursor notionAdminAgentCursor, filterHash string, pageSize int,
	includeDeleted bool, from, to *int64,
) (notionAdminAgentsUsageResponse, error) {
	response := notionAdminAgentsUsageResponse{Results: []notionAdminAgentUsageResponse{}}
	authority := notionAdminAgentAuthority{}
	lastID := ""
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		authority, err = loadNotionAdminAgentAuthority(ctx, txApp, identity.OrganizationID, workspaceID)
		if err != nil {
			return err
		}
		if !notionAdminAgentCursorMatchesAuthority(cursor, authority) {
			return errInvalidNotionAdminAgentCursor
		}
		rows := []storedNotionAdminAgent{}
		deletedClause := "AND [[deleted_at_ms]] IS NULL"
		if includeDeleted {
			deletedClause = ""
		}
		usageWindowClause := ""
		params := dbx.Params{
			"workspaceId": workspaceID, "after": cursor.AfterID, "limit": pageSize + 1,
			"statusNow": time.Now().UTC().UnixMilli(),
		}
		if from != nil {
			usageWindowClause += " AND [[last_credit_usage_time_ms]] >= {:from}"
			params["from"] = *from
		}
		if to != nil {
			usageWindowClause += " AND [[last_credit_usage_time_ms]] <= {:to}"
			params["to"] = *to
		}
		if err := txApp.DB().NewQuery(notionAdminAgentStatusProjectionSQL() + `SELECT * FROM [[projected_agent]]
			WHERE [[workspace_id]] = {:workspaceId} AND [[id]] > {:after} ` +
			deletedClause + usageWindowClause + `
			ORDER BY [[id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(params).All(&rows); err != nil {
			return err
		}
		response.HasMore = len(rows) > pageSize
		if response.HasMore {
			rows = rows[:pageSize]
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		periods, nestedMore, totals, err := loadNotionAdminAgentUsagePeriods(
			ctx, txApp, ids, math.MaxInt64, nil, nil, notionAdminAgentMaximumPageSize,
		)
		if err != nil {
			return err
		}
		for _, row := range rows {
			projected, err := projectNotionAdminAgentUsage(authority, row, periods[row.ID], totals[row.ID])
			if err != nil {
				return err
			}
			projected.HasMore = nestedMore[row.ID]
			if projected.HasMore {
				last := periods[row.ID][len(periods[row.ID])-1].MetricsPeriodStartMS
				next, err := encodeNotionAdminAgentCursor(
					secret, identity, workspaceID, row.ID, "agent_usage", authority,
					notionAdminAgentEmptyFilterHash, strconv.FormatInt(last, 10),
				)
				if err != nil {
					return err
				}
				projected.NextCursor = &next
			}
			response.Results = append(response.Results, projected)
			lastID = row.ID
		}
		return nil
	})
	if err != nil {
		return notionAdminAgentsUsageResponse{}, err
	}
	if response.HasMore {
		next, err := encodeNotionAdminAgentCursor(
			secret, identity, workspaceID, "", "agents_usage", authority, filterHash, lastID,
		)
		if err != nil {
			return notionAdminAgentsUsageResponse{}, err
		}
		response.NextCursor = &next
	}
	return response, nil
}

func loadNotionAdminAgentUsagePeriods(
	ctx context.Context, app core.App, agentIDs []string, after int64, from, to *int64, limit int,
) (map[string][]storedNotionAdminAgentUsagePeriod, map[string]bool, map[string]notionAdminAgentUsageTotal, error) {
	periods := map[string][]storedNotionAdminAgentUsagePeriod{}
	hasMore := map[string]bool{}
	totals := map[string]notionAdminAgentUsageTotal{}
	if len(agentIDs) == 0 {
		return periods, hasMore, totals, nil
	}
	params := dbx.Params{"after": after, "limit": limit + 1}
	in := notionAdminAgentInClause("[[agent_id]]", "usageAgent", agentIDs, params)
	clauses := []string{in, "[[metrics_period_start_ms]] < {:after}"}
	if from != nil {
		clauses = append(clauses, "[[metrics_period_end_ms]] > {:from}")
		params["from"] = *from
	}
	if to != nil {
		clauses = append(clauses, "[[metrics_period_start_ms]] < {:to}")
		params["to"] = *to
	}
	rows := []storedNotionAdminAgentUsagePeriod{}
	if err := app.DB().NewQuery(`WITH [[ranked]] AS (
		SELECT *, ROW_NUMBER() OVER (
			PARTITION BY [[agent_id]] ORDER BY [[metrics_period_start_ms]] DESC
		) AS [[page_rank]] FROM {{leafen_notion_agent_usage_periods}} WHERE ` +
		strings.Join(clauses, " AND ") + `)
		SELECT [[agent_id]], [[metrics_period_start_ms]], [[metrics_period_end_ms]],
			[[credits_used]] FROM [[ranked]]
		WHERE [[page_rank]] <= {:limit} ORDER BY [[agent_id]], [[metrics_period_start_ms]] DESC`).
		WithContext(ctx).Bind(params).All(&rows); err != nil {
		return nil, nil, nil, err
	}
	for _, row := range rows {
		if len(periods[row.AgentID]) == limit {
			hasMore[row.AgentID] = true
			continue
		}
		periods[row.AgentID] = append(periods[row.AgentID], row)
	}
	type totalRow struct {
		AgentID string `db:"agent_id"`
		Credits int64  `db:"credits"`
	}
	totalRows := []totalRow{}
	if err := app.DB().NewQuery(`SELECT [[agent_id]], SUM([[credits_used]]) AS [[credits]]
		FROM {{leafen_notion_agent_usage_periods}}
		WHERE ` + in + ` GROUP BY [[agent_id]]`).WithContext(ctx).Bind(params).All(&totalRows); err != nil {
		return nil, nil, nil, err
	}
	for _, row := range totalRows {
		totals[row.AgentID] = notionAdminAgentUsageTotal{Credits: row.Credits}
	}
	return periods, hasMore, totals, nil
}

func projectNotionAdminAgentUsage(
	authority notionAdminAgentAuthority, agent storedNotionAdminAgent,
	periods []storedNotionAdminAgentUsagePeriod, total notionAdminAgentUsageTotal,
) (notionAdminAgentUsageResponse, error) {
	publicID, err := notionCompatibleAgentID(agent.ID)
	if err != nil {
		return notionAdminAgentUsageResponse{}, err
	}
	creatorID, err := leafen.NotionCompatibleUserID(agent.CreatedBy)
	if err != nil {
		return notionAdminAgentUsageResponse{}, err
	}
	projectedPeriods := make([]notionAdminAgentUsagePeriod, 0, len(periods))
	for _, period := range periods {
		projectedPeriods = append(projectedPeriods, notionAdminAgentUsagePeriod{
			CreditsUsed: period.CreditsUsed, MetricsPeriodStartMS: period.MetricsPeriodStartMS,
			MetricsPeriodEndMS: period.MetricsPeriodEndMS,
		})
	}
	return notionAdminAgentUsageResponse{
		CreditPerBillingPeriodList: projectedPeriods, ID: publicID, RunsCompleted: agent.RunsCompleted,
		Status: agent.EffectiveStatus, TotalCreditsUsed: total.Credits, Type: agent.AgentType,
		CreatedByID: creatorID, CreditLimit: nullableInt64Pointer(agent.EnforcedCreditLimit),
		LastCreditUsageTime: nullableInt64Pointer(agent.LastCreditUsageTimeMS), Name: agent.Name,
	}, nil
}
