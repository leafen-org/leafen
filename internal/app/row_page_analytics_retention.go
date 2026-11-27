package app

// The retention sweep visits the row page analytics tables after the page
// tables, then workspace analytics' and the databases' own view tables, each
// by its own time column and keys, before starting over.

const (
	pageAnalyticsRetentionPhaseRowDaily  = "row_daily"
	pageAnalyticsRetentionPhaseRowUnique = "row_unique"
	pageAnalyticsRetentionPhaseRowRecent = "row_recent"
	// Workspace analytics' member view counts, then its search counts,
	// follow the row tables.
	pageAnalyticsRetentionPhaseMemberDaily = "member_daily"
	pageAnalyticsRetentionPhaseSearchDaily = "search_daily"
	// An inline database's own views, kept beside the page holding it
	// (schema_baseline.json), close the sweep.
	pageAnalyticsRetentionPhaseDatabaseDaily  = "database_daily"
	pageAnalyticsRetentionPhaseDatabaseUnique = "database_unique"
	pageAnalyticsRetentionPhaseDatabaseRecent = "database_recent"
)

func rowPageAnalyticsRetentionSpec(phase, historyCutoff, dailyCutoff string) (pageAnalyticsRetentionPhaseSpec, bool) {
	switch phase {
	case pageAnalyticsRetentionPhaseRowDaily:
		return pageAnalyticsRetentionPhaseSpec{
			Table: "leafen_row_page_analytics_daily", DeleteFrom: `DELETE FROM {{leafen_row_page_analytics_daily}}`,
			Index:       "idx_leafen_row_page_analytics_daily_retention_global",
			DeleteIndex: "sqlite_autoindex_leafen_row_page_analytics_daily_1",
			TimeColumn:  "retention_at", Keys: []string{"workspace_id", "row_id", "day"},
			Cutoff: dailyCutoff, Shift: maxPageAnalyticsReadDays, Page: "row_id",
		}, true
	case pageAnalyticsRetentionPhaseRowUnique:
		return pageAnalyticsRetentionPhaseSpec{
			Table: "leafen_row_page_analytics_unique_viewers", DeleteFrom: `DELETE FROM {{leafen_row_page_analytics_unique_viewers}}`,
			Index:       "idx_leafen_row_page_analytics_unique_retention_global",
			DeleteIndex: "sqlite_autoindex_leafen_row_page_analytics_unique_viewers_1",
			TimeColumn:  "observed_at", Keys: []string{"workspace_id", "row_id", "day", "viewer_key"},
			Cutoff: historyCutoff, Page: "row_id", Person: "viewer_key",
		}, true
	case pageAnalyticsRetentionPhaseRowRecent:
		return pageAnalyticsRetentionPhaseSpec{
			Table: "leafen_row_page_analytics_recent_viewers", DeleteFrom: `DELETE FROM {{leafen_row_page_analytics_recent_viewers}}`,
			Index:       "idx_leafen_row_page_analytics_recent_retention_global",
			DeleteIndex: "sqlite_autoindex_leafen_row_page_analytics_recent_viewers_1",
			TimeColumn:  "last_viewed_at", Keys: []string{"workspace_id", "row_id", "user_id"}, Cutoff: historyCutoff,
			Page: "row_id", Person: "user_id",
		}, true
	case pageAnalyticsRetentionPhaseMemberDaily:
		return pageAnalyticsRetentionPhaseSpec{
			Table: workspaceAnalyticsMemberDailyTable, DeleteFrom: `DELETE FROM {{leafen_workspace_analytics_member_daily}}`,
			Index:       "idx_leafen_workspace_analytics_member_daily_retention_global",
			DeleteIndex: "sqlite_autoindex_leafen_workspace_analytics_member_daily_1",
			TimeColumn:  "retention_at", Keys: []string{"workspace_id", "user_id", "day"},
			Cutoff: dailyCutoff, Shift: maxPageAnalyticsReadDays, Person: "user_id",
		}, true
	case pageAnalyticsRetentionPhaseSearchDaily:
		return pageAnalyticsRetentionPhaseSpec{
			Table: workspaceSearchDailyTable, DeleteFrom: `DELETE FROM {{leafen_workspace_search_daily}}`,
			Index:       "idx_leafen_workspace_search_daily_retention_global",
			DeleteIndex: "sqlite_autoindex_leafen_workspace_search_daily_1",
			TimeColumn:  "retention_at", Keys: []string{"workspace_id", "day", "query", "user_id"},
			Cutoff: dailyCutoff, Shift: maxPageAnalyticsReadDays, Person: "user_id",
		}, true
	case pageAnalyticsRetentionPhaseDatabaseDaily:
		return pageAnalyticsRetentionPhaseSpec{
			Table: "leafen_database_analytics_daily", DeleteFrom: `DELETE FROM {{leafen_database_analytics_daily}}`,
			Index:       "idx_leafen_database_analytics_daily_retention_global",
			DeleteIndex: "sqlite_autoindex_leafen_database_analytics_daily_1",
			TimeColumn:  "retention_at", Keys: []string{"workspace_id", "container_id", "day"},
			Cutoff: dailyCutoff, Shift: maxPageAnalyticsReadDays, Page: "page_id",
		}, true
	case pageAnalyticsRetentionPhaseDatabaseUnique:
		return pageAnalyticsRetentionPhaseSpec{
			Table: "leafen_database_analytics_unique_viewers", DeleteFrom: `DELETE FROM {{leafen_database_analytics_unique_viewers}}`,
			Index:       "idx_leafen_database_analytics_unique_retention_global",
			DeleteIndex: "sqlite_autoindex_leafen_database_analytics_unique_viewers_1",
			TimeColumn:  "observed_at", Keys: []string{"workspace_id", "container_id", "day", "viewer_key"},
			Cutoff: historyCutoff, Page: "page_id", Person: "viewer_key",
		}, true
	case pageAnalyticsRetentionPhaseDatabaseRecent:
		return pageAnalyticsRetentionPhaseSpec{
			Table: "leafen_database_analytics_recent_viewers", DeleteFrom: `DELETE FROM {{leafen_database_analytics_recent_viewers}}`,
			Index:       "idx_leafen_database_analytics_recent_retention_global",
			DeleteIndex: "sqlite_autoindex_leafen_database_analytics_recent_viewers_1",
			TimeColumn:  "last_viewed_at", Keys: []string{"workspace_id", "container_id", "user_id"}, Cutoff: historyCutoff,
			Page: "page_id", Person: "user_id",
		}, true
	default:
		return pageAnalyticsRetentionPhaseSpec{}, false
	}
}

func nextRowPageAnalyticsRetentionPhase(current string) (string, bool) {
	switch current {
	case pageAnalyticsRetentionPhaseRowDaily:
		return pageAnalyticsRetentionPhaseRowUnique, false
	case pageAnalyticsRetentionPhaseRowUnique:
		return pageAnalyticsRetentionPhaseRowRecent, false
	case pageAnalyticsRetentionPhaseRowRecent:
		return pageAnalyticsRetentionPhaseMemberDaily, false
	case pageAnalyticsRetentionPhaseMemberDaily:
		return pageAnalyticsRetentionPhaseSearchDaily, false
	case pageAnalyticsRetentionPhaseSearchDaily:
		return pageAnalyticsRetentionPhaseDatabaseDaily, false
	case pageAnalyticsRetentionPhaseDatabaseDaily:
		return pageAnalyticsRetentionPhaseDatabaseUnique, false
	case pageAnalyticsRetentionPhaseDatabaseUnique:
		return pageAnalyticsRetentionPhaseDatabaseRecent, false
	case pageAnalyticsRetentionPhaseDatabaseRecent:
		return pageAnalyticsRetentionPhaseBatches, true
	default:
		return "", true
	}
}
