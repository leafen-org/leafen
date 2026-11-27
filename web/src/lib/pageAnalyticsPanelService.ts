import { api } from "@/api";
import type { PageAnalyticsPanelService } from "@/lib/pageAnalyticsView";
import type { Session } from "@/types";

export function createPageAnalyticsPanelService(
  session: Session,
): PageAnalyticsPanelService {
  return {
    getAnalytics: (pageId, rangeDays, target) => api.getPageAnalytics(session, pageId, rangeDays, target),
    getPreferences: (workspaceId, pageId, target) => api.getPageAnalyticsPreferences(
      session,
      workspaceId,
      pageId,
      target,
    ),
    updatePreferences: (input) => api.updatePageAnalyticsPreferences(
      session,
      input,
    ),
    updatePolicy: (input) => api.updatePageAnalyticsPolicy(session, input),
  };
}
