/**
 * Dashboards a view tab's "+" made in this tab and not yet shown: such a new
 * dashboard opens in Edit mode, ready for its first widget, while one opened
 * again with no widget shows its empty view, as Notion's (seen 2026-11-07).
 * One "/" makes opens with its empty view (useAuthenticatedEditorActions).
 */
const made = new Set<string>();

export function markDashboardMade(viewId: string) {
  made.add(viewId);
}

/** Whether the dashboard was just made here; asking forgets it. */
export function takeDashboardMade(viewId: string) {
  return made.delete(viewId);
}
