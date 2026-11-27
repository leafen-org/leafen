import { api } from "@/api";
import type { PageAnalyticsPanelService } from "@/lib/pageAnalyticsView";
import type { Session } from "@/types";

/**
 * Whether one may open a page's updates ("업데이트와 애널리틱스") at all, the
 * one rule every way in follows — the bar's edited time (a page's, a row's
 * page's, a database's opened alone), the "⋯" row (a phone's sheet's, a row
 * page's) and a peek's ask to open the page whole with them beside it — given
 * whether they may edit the page (their access, not its lock). Its owner, a
 * member or a guest allowed to edit opens them; one who may only comment on
 * it or read it, guest or member alike, has no way in: the edited time is
 * only shown (no pointer, no hover, no focus stop), pressing it opens
 * nothing, and the "⋯" has no such row, nor finds one by its words, as
 * Notion's (seen 2026-11-28 in Chrome at 1440, dpr 1, two tabs at different
 * times each, nothing in the page before the press: a read guest's and a
 * comment guest's page, and a read member's in a Business workspace; the
 * owner's edited time took the pointer and a hover and opened them).
 */
export function opensPageUpdates(canEdit: boolean): boolean {
  return canEdit;
}

/** The same for a page read with its access role: a database row's page, a database opened alone in one. */
export function roleOpensPageUpdates(role: "view" | "comment" | "edit" | "full_access" | undefined): boolean {
  return opensPageUpdates(role === "edit" || role === "full_access");
}

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
