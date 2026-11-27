package organization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	leafen "leafen.org/leafen/internal/app"
)

const (
	notionAdminAgentCursorVersion   = 1
	notionAdminAgentCursorPrefix    = "hnaag1"
	notionAdminAgentCursorDomain    = "leafen:notion-admin-agent-governance-cursor:v1"
	notionAdminAgentCursorMaxLength = 4096
	notionAdminAgentEmptyFilterHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

var errInvalidNotionAdminAgentCursor = leafen.NotionAdminInvalid(errors.New("invalid Notion Admin agent-governance cursor"), "start_cursor isn't one this list gave.")

type notionAdminAgentCursor struct {
	Version            int    `json:"v"`
	OrganizationID     string `json:"o"`
	TokenID            string `json:"t"`
	WorkspaceID        string `json:"w"`
	AgentID            string `json:"g,omitempty"`
	Kind               string `json:"k"`
	AgentRevision      int64  `json:"r"`
	GroupRevision      int64  `json:"q"`
	PermissionRevision int64  `json:"p"`
	FilterHash         string `json:"f"`
	AfterID            string `json:"a"`
}

type notionAdminAgentListFilter struct {
	Name                string
	CreatedByIDs        []string
	CreatedTimeFrom     *int64
	CreatedTimeTo       *int64
	LastEditedTimeFrom  *int64
	LastEditedTimeTo    *int64
	CreditUsageTimeFrom *int64
	CreditUsageTimeTo   *int64
	Statuses            []string
	IntegrationTypes    []string
	MCPServerNames      []string
	Models              []string
	SharedWithUserIDs   []string
	SharedWithGroupIDs  []string
}

func notionAdminAgentListQuery(request *http.Request) (int, string, notionAdminAgentListFilter, error) {
	query := request.URL.Query()
	allowed := map[string]bool{
		"name": true, "created_by_ids": true, "created_time_from": true, "created_time_to": true,
		"last_edited_time_from": true, "last_edited_time_to": true,
		"credit_usage_time_from": true, "credit_usage_time_to": true, "status": true,
		"integration_types": true, "mcp_server_names": true, "models": true,
		"shared_with_user_ids": true, "shared_with_group_ids": true, "cursor": true, "page_size": true,
	}
	scalar := map[string]bool{
		"name": true, "created_time_from": true, "created_time_to": true,
		"last_edited_time_from": true, "last_edited_time_to": true,
		"credit_usage_time_from": true, "credit_usage_time_to": true, "cursor": true, "page_size": true,
	}
	for key, values := range query {
		if !allowed[key] || len(values) == 0 || scalar[key] && len(values) != 1 {
			return 0, "", notionAdminAgentListFilter{}, errInvalidNotionAdminAgentCursor
		}
	}
	pageSize := notionAdminAgentDefaultPageSize
	if raw := query.Get("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > notionAdminAgentMaximumPageSize {
			return 0, "", notionAdminAgentListFilter{}, errInvalidNotionAdminAgentCursor
		}
		pageSize = parsed
	}
	filter := notionAdminAgentListFilter{
		Name: query.Get("name"), CreatedByIDs: query["created_by_ids"], Statuses: query["status"],
		IntegrationTypes: query["integration_types"], MCPServerNames: query["mcp_server_names"],
		Models: query["models"], SharedWithUserIDs: query["shared_with_user_ids"],
		SharedWithGroupIDs: query["shared_with_group_ids"],
	}
	for key, target := range map[string]**int64{
		"created_time_from": &filter.CreatedTimeFrom, "created_time_to": &filter.CreatedTimeTo,
		"last_edited_time_from": &filter.LastEditedTimeFrom, "last_edited_time_to": &filter.LastEditedTimeTo,
		"credit_usage_time_from": &filter.CreditUsageTimeFrom, "credit_usage_time_to": &filter.CreditUsageTimeTo,
	} {
		if raw := query.Get(key); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				return 0, "", notionAdminAgentListFilter{}, errInvalidNotionAdminAgentCursor
			}
			*target = &parsed
		}
	}
	if !validNotionAdminAgentFilter(filter) {
		return 0, "", notionAdminAgentListFilter{}, errInvalidNotionAdminAgentCursor
	}
	return pageSize, query.Get("cursor"), filter, nil
}

func validNotionAdminAgentFilter(filter notionAdminAgentListFilter) bool {
	for _, status := range filter.Statuses {
		if !governanceOneOf(status, "active", "deleted", "disabled", "run_limit", "credit_limit",
			"runaway_credit_usage", "workspace_credit_limit", "failure_limit",
			"mark_session_failed_autopause", "disabled_from_workspace_settings",
			"disabled_from_workspace_settings_but_resumable", "disabled_from_api",
			"disabled_from_agent_settings", "disabled_due_to_lack_of_full_access_members",
			"disabled_due_to_lack_of_editors", "disabled_by_notion", "internal_error",
			"needs_review", "tool_unavailable") {
			return false
		}
	}
	return true
}

func notionAdminAgentFilterHash(filter notionAdminAgentListFilter) (string, error) {
	canonical := filter
	for _, values := range []*[]string{
		&canonical.CreatedByIDs, &canonical.Statuses, &canonical.IntegrationTypes,
		&canonical.MCPServerNames, &canonical.Models, &canonical.SharedWithUserIDs,
		&canonical.SharedWithGroupIDs,
	} {
		sort.Strings(*values)
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func notionAdminAgentSimpleQuery(request *http.Request, cursorKey string, withPageSize bool) (int, string, error) {
	query := request.URL.Query()
	for key, values := range query {
		if key != cursorKey && (!withPageSize || key != "page_size") || len(values) != 1 {
			return 0, "", errInvalidNotionAdminAgentCursor
		}
	}
	pageSize := notionAdminAgentMaximumPageSize
	if withPageSize {
		pageSize = notionAdminAgentDefaultPageSize
		if raw := query.Get("page_size"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > notionAdminAgentMaximumPageSize {
				return 0, "", errInvalidNotionAdminAgentCursor
			}
			pageSize = parsed
		}
	}
	return pageSize, query.Get(cursorKey), nil
}

func notionAdminAgentCanonicalQueryHash(query url.Values, excluded ...string) string {
	clone := url.Values{}
	for key, values := range query {
		clone[key] = append([]string(nil), values...)
		sort.Strings(clone[key])
	}
	for _, key := range excluded {
		delete(clone, key)
	}
	digest := sha256.Sum256([]byte(clone.Encode()))
	return hex.EncodeToString(digest[:])
}

func encodeNotionAdminAgentCursor(
	secret string, identity leafen.NotionAdminIdentity, workspaceID, agentID, kind string,
	authority notionAdminAgentAuthority, filterHash, afterID string,
) (string, error) {
	return leafen.EncodeNotionAdminLegalHoldScopeCursor(
		secret, notionAdminAgentCursorPrefix, notionAdminAgentCursorDomain,
		notionAdminAgentCursorMaxLength, notionAdminAgentCursor{
			Version: notionAdminAgentCursorVersion, OrganizationID: identity.OrganizationID,
			TokenID: identity.TokenID, WorkspaceID: workspaceID, AgentID: agentID, Kind: kind,
			AgentRevision: authority.AgentRevision, GroupRevision: authority.GroupRevision,
			PermissionRevision: authority.PermissionRevision, FilterHash: filterHash, AfterID: afterID,
		},
	)
}

func decodeNotionAdminAgentCursor(
	raw, secret string, identity leafen.NotionAdminIdentity, workspaceID, agentID, kind, filterHash string,
) (notionAdminAgentCursor, error) {
	if raw == "" {
		return notionAdminAgentCursor{}, nil
	}
	cursor := notionAdminAgentCursor{}
	if err := leafen.DecodeNotionAdminLegalHoldScopeCursor(
		raw, secret, notionAdminAgentCursorPrefix, notionAdminAgentCursorDomain,
		notionAdminAgentCursorMaxLength, &cursor,
	); err != nil || cursor.Version != notionAdminAgentCursorVersion ||
		cursor.OrganizationID != identity.OrganizationID || cursor.TokenID != identity.TokenID ||
		cursor.WorkspaceID != workspaceID || cursor.AgentID != agentID || cursor.Kind != kind ||
		cursor.FilterHash != filterHash || cursor.AgentRevision < 1 || cursor.GroupRevision < 1 ||
		cursor.PermissionRevision < 0 || cursor.AfterID == "" {
		return notionAdminAgentCursor{}, errInvalidNotionAdminAgentCursor
	}
	return cursor, nil
}
