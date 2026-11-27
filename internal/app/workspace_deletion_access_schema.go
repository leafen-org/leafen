package app

import (
	"strings"
)

type workspaceDeletionAccessIndex struct {
	Name    string
	Table   string
	Columns string
}

var workspaceDeletionOrderedAccessIndexes = []workspaceDeletionAccessIndex{
	{Name: "idx_leafen_workspace_deletion_target", Table: "leafen_workspace_deletion_jobs", Columns: "actor_id,workspace_id,created_at DESC,id DESC"},
	{Name: "idx_leafen_workspace_deletion_jobs_preflight", Table: "leafen_jobs", Columns: "workspace_id,status"},
	{Name: "idx_leafen_workspace_deletion_file_receipts_preflight", Table: "leafen_file_delete_receipts", Columns: "workspace_id,status"},
	{Name: "idx_leafen_workspace_deletion_pages_window", Table: "leafen_pages", Columns: "workspace,id"},
	{Name: "idx_leafen_workspace_deletion_export_artifacts_window", Table: "leafen_workspace_export_artifacts", Columns: "workspace_id,job_id"},
	{Name: "idx_leafen_workspace_deletion_oauth_codes_grant", Table: "leafen_notion_oauth_authorization_codes", Columns: "grant_id"},
	{Name: "idx_leafen_workspace_deletion_oauth_refresh_grant", Table: "leafen_notion_oauth_refresh_tokens", Columns: "grant_id"},
	{Name: "idx_leafen_workspace_deletion_oauth_resources_grant", Table: "leafen_notion_oauth_grant_resources", Columns: "grant_id"},
	{Name: "idx_leafen_workspace_deletion_workspace_export_scope_pages", Table: "leafen_workspace_export_scope_pages", Columns: "job_id"},
	{Name: "idx_leafen_workspace_deletion_workspace_export_scope_blocks", Table: "leafen_workspace_export_scope_blocks", Columns: "job_id"},
}

func workspaceDeletionScopeIndexName(table string) string {
	return "idx_leafen_workspace_deletion_" + strings.TrimPrefix(table, "leafen_")
}

func workspaceDeletionChildAccessIndex(table string) (string, bool) {
	for _, index := range workspaceDeletionOrderedAccessIndexes {
		if index.Table == table && (index.Columns == "grant_id" || index.Columns == "job_id") {
			return index.Name, true
		}
	}
	return "", false
}
