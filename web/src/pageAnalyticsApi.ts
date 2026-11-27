import { request } from "./apiTransport";
import type {
  PageAnalyticsPreferences,
  PageAnalyticsReadResult,
  PageAnalyticsRecordResult,
  PageAnalyticsRowViewsResult,
  PageAnalyticsTransportRequest,
  PageAnalyticsUpdatePolicyRequest,
  PageAnalyticsUpdatePolicyResult,
  PageAnalyticsUpdatePreferencesRequest,
  PageAnalyticsUpdatePreferencesResult,
  PageAnalyticsViewHistory,
} from "./pageAnalyticsWire.generated";
import type { Session } from "./types";

/** What on a page its analytics are of: a database row page or an inline database opened alone, or else the page. */
export type PageAnalyticsTarget = { rowId?: string; containerId?: string };

function analyticsTarget({ rowId, containerId }: PageAnalyticsTarget) {
  return rowId ? { rowId } : containerId ? { containerId } : {};
}

/**
 * The page analytics endpoints: recording page, database row page and
 * database opened alone views, reading a page's, row page's or database's
 * analytics and a feed's row view counts, and the member preferences and
 * workspace policy.
 */
export function createPageAnalyticsApi() {
  return {
    recordPageAnalytics: (
      session: Session | null,
      input: PageAnalyticsTransportRequest,
      options: { signal?: AbortSignal } = {},
    ) => request<PageAnalyticsRecordResult>(
      "/page-analytics",
      { method: "POST", body: JSON.stringify(input), signal: options.signal },
      input.action === "record" ? session : null,
    ),
    getPageAnalytics: (
      session: Session,
      pageId: string,
      rangeDays = 28,
      target: PageAnalyticsTarget = {},
    ) => request<PageAnalyticsReadResult>(
      "/page-analytics",
      { method: "POST", body: JSON.stringify({ action: "get", pageId, ...analyticsTarget(target), rangeDays }) },
      session,
    ),
    getRowPageViews: (
      session: Session,
      workspaceId: string,
      rowIds: string[],
      signal?: AbortSignal,
    ) => request<PageAnalyticsRowViewsResult>(
      "/page-analytics",
      { method: "POST", body: JSON.stringify({ action: "getRowViews", workspaceId, rowIds }), signal },
      session,
    ),
    getPageAnalyticsPreferences: (
      session: Session,
      workspaceId: string,
      pageId: string,
      target: PageAnalyticsTarget = {},
    ) => request<PageAnalyticsPreferences>(
      "/page-analytics",
      { method: "POST", body: JSON.stringify({ action: "getPreferences", workspaceId, pageId, ...analyticsTarget(target) }) },
      session,
    ),
    updatePageAnalyticsPreferences: (
      session: Session,
      input: Omit<PageAnalyticsUpdatePreferencesRequest, "action">,
    ) => request<PageAnalyticsUpdatePreferencesResult>(
      "/page-analytics",
      { method: "POST", body: JSON.stringify({ action: "updatePreferences", ...input }) },
      session,
    ),
    // A member's "내 조회 기록 표시" for a workspace, from the settings window.
    getPageAnalyticsViewHistory: (session: Session, workspaceId: string, signal?: AbortSignal) =>
      request<PageAnalyticsViewHistory>(
        "/page-analytics",
        { method: "POST", body: JSON.stringify({ action: "getViewHistory", workspaceId }), signal },
        session,
      ),
    updatePageAnalyticsViewHistory: (session: Session, workspaceId: string, identifiedHistoryEnabled: boolean) =>
      request<PageAnalyticsViewHistory>(
        "/page-analytics",
        { method: "POST", body: JSON.stringify({ action: "updateViewHistory", workspaceId, identifiedHistoryEnabled }) },
        session,
      ),
    updatePageAnalyticsPolicy: (
      session: Session,
      input: Omit<PageAnalyticsUpdatePolicyRequest, "action">,
    ) => request<PageAnalyticsUpdatePolicyResult>(
      "/page-analytics",
      { method: "POST", body: JSON.stringify({ action: "updatePolicy", ...input }) },
      session,
    ),
  };
}
