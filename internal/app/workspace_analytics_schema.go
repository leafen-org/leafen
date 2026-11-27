package app

// Workspace analytics counts each member's page views per day beside the
// page counts, kept for the read window and swept by the page analytics
// retention runner, whose phases now include this table.

const workspaceAnalyticsMemberDailyTable = "leafen_workspace_analytics_member_daily"
