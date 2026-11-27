package app

import "github.com/pocketbase/pocketbase/core"

// A page's analytics — its views by day, who viewed it, who made and edited
// it, a row page's views on a feed post — are its workspace's own: a guest,
// who sees only the pages shared with them, reads none of them whatever
// their access to the page, as Notion's updates panel shows a guest who
// edits the page or has full access no analytics tab (seen 2026-11-28, two
// tabs at different times). The workspace's owner and members who edit the
// page read them (readPageAnalytics, readRowPageAnalyticsViews); a guest's
// own view-history choice stays theirs (page_analytics_preferences.go).

// pageAnalyticsReader is whether the person is the workspace's owner or one
// of its members, who may read its pages' analytics.
func pageAnalyticsReader(app core.App, principalID, workspaceID string) (bool, error) {
	_, err := authorizeWorkspace(app, principalID, workspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return false, nil
	}
	return err == nil, err
}
