package app

// Workspace search analytics counts, for each day, how often each member
// searched a query and how many of those searches opened a result, kept for
// the read window and swept by the page analytics retention runner, whose
// phases now include this table.

const workspaceSearchDailyTable = "leafen_workspace_search_daily"
