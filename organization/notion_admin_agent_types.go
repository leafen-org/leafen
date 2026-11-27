package organization

import "database/sql"

const (
	notionAdminAgentDefaultPageSize = 25
	notionAdminAgentMaximumPageSize = 100
	notionAdminAgentPermissionPage  = 250
	notionAdminAgentMaxBodyBytes    = 64 << 10
)

type notionAdminAgentAuthority struct {
	WorkspaceName      string         `db:"workspace_name"`
	OwnerID            string         `db:"owner_id"`
	AgentRevision      int64          `db:"agent_revision"`
	GroupRevision      int64          `db:"group_revision"`
	PermissionRevision int64          `db:"permission_revision"`
	CreationPolicy     sql.NullString `db:"creation_policy"`
	DefaultCreditLimit sql.NullInt64  `db:"default_agent_credit_limit"`
	DisableGeneration  int64          `db:"disable_generation"`
}

type storedNotionAdminAgent struct {
	ID                            string         `db:"id"`
	WorkspaceID                   string         `db:"workspace_id"`
	CreatedBy                     string         `db:"created_by"`
	Name                          string         `db:"name"`
	Description                   sql.NullString `db:"description"`
	AgentType                     string         `db:"agent_type"`
	AdminStatus                   string         `db:"admin_status"`
	CreatorCreditLimit            sql.NullInt64  `db:"creator_credit_limit"`
	AdminCreditLimit              sql.NullInt64  `db:"admin_credit_limit"`
	AcknowledgedDisableGeneration int64          `db:"acknowledged_disable_generation"`
	RunsCompleted                 int64          `db:"runs_completed"`
	CreatedTimeMS                 int64          `db:"created_time_ms"`
	LastEditedTimeMS              int64          `db:"last_edited_time_ms"`
	LastCreditUsageTimeMS         sql.NullInt64  `db:"last_credit_usage_time_ms"`
	DeletedAtMS                   sql.NullInt64  `db:"deleted_at_ms"`
	Revision                      int64          `db:"revision"`
	CurrentCreditUsage            sql.NullInt64  `db:"current_credit_usage"`
	EnforcedCreditLimit           sql.NullInt64  `db:"enforced_credit_limit"`
	WorkspaceCreditLimitOwner     bool           `db:"workspace_credit_limit_owner"`
	WorkspaceCreationPolicy       sql.NullString `db:"workspace_creation_policy"`
	WorkspaceDisableGeneration    int64          `db:"workspace_disable_generation"`
	EffectiveStatus               string         `db:"effective_status"`
}

type notionAdminAgentResponse struct {
	Alive bool `json:"alive"`
	// Whose credits it spends and who owns it, which Notion's agent object
	// always carries: Leafen keeps neither, as it holds no agents of its own,
	// so every agent spends the workspace's and has no owner.
	CreditSource        string  `json:"credit_source"`
	OwnerUserID         *string `json:"owner_user_id"`
	ID                  string  `json:"id"`
	SpaceID             string  `json:"space_id"`
	Status              string  `json:"status"`
	Type                string  `json:"type"`
	Name                string  `json:"name,omitempty"`
	Description         string  `json:"description,omitempty"`
	CreatedByID         string  `json:"created_by_id,omitempty"`
	CreatedByTable      string  `json:"created_by_table,omitempty"`
	ParentID            string  `json:"parent_id,omitempty"`
	ParentTable         string  `json:"parent_table,omitempty"`
	CreatedTime         int64   `json:"created_time,omitempty"`
	LastEditedTime      int64   `json:"last_edited_time,omitempty"`
	LastCreditUsageTime *int64  `json:"last_credit_usage_time,omitempty"`
}

type notionAdminAgentListResponse struct {
	Results    []notionAdminAgentResponse `json:"results"`
	HasMore    bool                       `json:"has_more"`
	NextCursor *string                    `json:"next_cursor,omitempty"`
}

type notionAdminAgentStatusResponse struct {
	AdminStatus  string `json:"admin_status"`
	RunStatus    string `json:"run_status"`
	PausedReason string `json:"paused_reason,omitempty"`
}

type notionAdminAgentCounts struct {
	Active   int `json:"active" db:"active"`
	Deleted  int `json:"deleted" db:"deleted"`
	Disabled int `json:"disabled" db:"disabled"`
}

type notionAdminAgentCreationPolicyResponse struct {
	AgentCounts              notionAdminAgentCounts `json:"agent_counts"`
	CurrentPolicy            string                 `json:"current_policy"`
	CurrentPolicyDescription string                 `json:"current_policy_description"`
	SpaceID                  string                 `json:"space_id"`
	SpaceName                string                 `json:"space_name"`
}

type notionAdminAgentWorkspaceCreditLimitResponse struct {
	SpaceID                 string `json:"space_id"`
	DefaultAgentCreditLimit *int64 `json:"default_agent_credit_limit"`
}

type notionAdminAgentCreditLimitResponse struct {
	CreditLimitByWorkspaceAdmin *int64 `json:"credit_limit_by_workspace_admin"`
	EnforcedCreditLimit         *int64 `json:"enforced_credit_limit"`
	CurrentCreditUsage          *int64 `json:"current_credit_usage"`
	DidClearPausedState         bool   `json:"did_clear_paused_state"`
	IsActive                    bool   `json:"is_active"`
}
