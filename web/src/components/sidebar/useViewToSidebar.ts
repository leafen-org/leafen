import type { GraphView } from "@/contentGraph";
import { useProductPages } from "@/components/product/productPagesContext";
import { i18next } from "@/i18n";
import { useOptionalProductToast } from "@/lib/productToast";
import { workspaceMembershipBounds } from "@/workspaceMembershipWire.generated";
import { SIDEBAR_HOME_TAB, addSidebarView, canAddSidebarView, newSidebarTabId } from "./sidebarLayoutModel";
import { useSidebarLayout } from "./useSidebarLayout";

/**
 * A view tab's "사이드바에 보기 추가", as Notion's (seen 2026-11-03): the
 * view's rows become a section after the sidebar's shown sections, headed
 * "〈보기〉 · 〈데이터베이스〉" (sidebarViewHeading). None where nobody is signed
 * in, the page holding the database is unknown, or the sidebar's layout isn't
 * read yet; a tab already holding its ten views says so.
 */
export function useViewToSidebar(holderPageId: string | undefined) {
  const pages = useProductPages();
  const session = pages?.session ?? null, workspaceId = pages?.workspaceId ?? null;
  const sections = useSidebarLayout(session, workspaceId);
  const toast = useOptionalProductToast();
  if (!session || !workspaceId || !holderPageId || !sections.loaded) return null;
  return (view: GraphView) => {
    if (!canAddSidebarView(sections.layout, SIDEBAR_HOME_TAB)) {
      toast?.notify(i18next.t("sidebar:viewsFull", { limit: workspaceMembershipBounds.sidebarViewsPerTab }), "error");
      return;
    }
    sections.change(addSidebarView(sections.layout, SIDEBAR_HOME_TAB, {
      key: newSidebarTabId(), pageId: holderPageId, dataSourceId: view.dataSourceId, viewId: view.id,
    }));
  };
}

/**
 * Why a view can't become a sidebar section, as Notion dims its row and says
 * in a tip (seen 2026-11-15 at 1440, the same view switched between layouts):
 * a board, or a table, list, gallery or timeline grouped, because grouped
 * views aren't taken there; a calendar or a map, because its kind isn't. An
 * ungrouped table, list, gallery, timeline or feed may be added.
 */
export type SidebarViewRefusal = "grouped" | "calendar" | "map";

export function sidebarViewRefusal(view: Pick<GraphView, "type" | "config">): SidebarViewRefusal | null {
  if (view.type === "calendar") return "calendar";
  if (view.type === "map") return "map";
  return view.type === "board" || typeof view.config.groupBy === "string" ? "grouped" : null;
}

/**
 * Whether a view's "사이드바에 보기 추가" is dimmed, and the tip saying why
 * where there is one: a form's, a chart's or a dashboard's row faded (seen
 * 2026-11-06), a grouped view's, a calendar's or a map's with its reason
 * (sidebarViewRefusal) — a view tab's menu and a dashboard widget's alike
 * (seen 2026-11-15 at 1440, in two tabs: a widget's chart, board and
 * calendar dimmed in Edit mode and out of it, its table and list not).
 */
export function sidebarViewDimmed(view: Pick<GraphView, "type" | "config">, labels: Parameters<typeof sidebarRefusalLabel>[1]) {
  const refusal = sidebarViewRefusal(view);
  return { disabled: view.type === "form" || view.type === "chart" || view.type === "dashboard" || refusal !== null,
    reason: sidebarRefusalLabel(refusal, labels) };
}

/** The tip for a refusal, in the reader's language. */
export function sidebarRefusalLabel(refusal: SidebarViewRefusal | null, labels: {
  sidebarGroupedUnsupported: string; sidebarCalendarUnsupported: string; sidebarMapUnsupported: string;
}): string | undefined {
  if (refusal === "grouped") return labels.sidebarGroupedUnsupported;
  if (refusal === "calendar") return labels.sidebarCalendarUnsupported;
  return refusal === "map" ? labels.sidebarMapUnsupported : undefined;
}
