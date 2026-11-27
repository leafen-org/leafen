import { useEffect, useRef } from "react";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";
import { opensPageUpdates } from "@/lib/pageAnalyticsPanelService";
import { forgetSidePanel, keepSidePanel, keptSidePanel, type KeptSidePanel } from "./pageSidePanelKept";

// A side panel's width beside the page (--page-side-panel-width) and the
// page's least width beside it: Notion kept its sidebar at 1100px and folded
// it at 1000px (seen 2026-10-01).
const SIDE_PANEL_WIDTH = 385;
const PAGE_LEAST_WIDTH = 400;

/** Which panel is open beside the page: all its discussions, an app, or its updates. */
export type PageSidePanelKind = "discussions" | "app" | "updates";

/**
 * Whether a panel docks beside the page, which then gives up its right side
 * and whose bar's "»" closes it. A phone has no room beside the page: every
 * panel opens over it as its own window or sheet, closed from its own head.
 * A touch screen of any width opens the page's updates as Notion's phone and
 * iPad do (seen 2026-11-13 in iPhone Safari at 402 and iPad Safari at 834 and
 * 1210): the "페이지 업데이트" sheet with its "완료" (PageAnalyticsPanel).
 */
export function pageSidePanelDocks(kind: PageSidePanelKind | null, screen: { phone: boolean; touch: boolean }) {
  if (!kind) return false;
  return kind === "updates" ? !screen.touch : !screen.phone;
}

/**
 * What any panel beside the page — all discussions, updates, an app — does
 * while open: docking folds the sidebar when the page would be left too
 * narrow, and ⌘⇧\ closes it, as Notion's "패널 닫기" tip names (seen
 * 2026-10-28). Answers whether the open panel docks (pageSidePanelDocks).
 * Given the page (`pageId`), the page keeps its docked discussions or updates
 * open as they open and close there (pageSidePanelKept), opened again with it
 * by panelOpeningBeside.
 */
export function usePageSidePanel(kind: PageSidePanelKind | null, close: () => void, pageId?: string) {
  const { sidebarWidth, collapseSidebar, phone, touch } = useSidebarLayout(true);
  const docked = pageSidePanelDocks(kind, { phone, touch });
  const latest = useRef({ sidebarWidth, collapseSidebar, close });
  latest.current = { sidebarWidth, collapseSidebar, close };
  const shown = useRef({ pageId, kind });
  useEffect(() => {
    const before = shown.current;
    shown.current = { pageId, kind };
    // A page's first draw still holds the panel of the page before it: only a change on the same page counts.
    if (!pageId || before.pageId !== pageId || before.kind === kind) return;
    if (docked && (kind === "discussions" || kind === "updates")) keepSidePanel(pageId, kind);
    else forgetSidePanel(pageId);
  }, [docked, kind, pageId]);
  useEffect(() => {
    if (docked && window.innerWidth - latest.current.sidebarWidth - SIDE_PANEL_WIDTH < PAGE_LEAST_WIDTH) latest.current.collapseSidebar();
  }, [docked]);
  const open = kind !== null;
  useEffect(() => {
    if (!open) return undefined;
    function onKeyDown(event: KeyboardEvent) {
      if (!matchesKeyboardShortcut("closeSidePanel", event)) return;
      event.preventDefault();
      latest.current.close();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [open]);
  return docked;
}

/*
 * A panel asked for in a peek — beside the page or over its middle, a page's
 * or a database page's alike — opens the page whole with that panel docked
 * beside it, as Notion's does: "업데이트와 애널리틱스" (seen 2026-11-17 at 1416
 * in Chrome: a wiki's new page in the center peek, a database row in the side
 * peek and a page in the center peek each opened as the page itself with its
 * updates beside it, not a window over the peek) and the "토론" list's "모두
 * 보기" (seen 2026-11-25 in Chrome at 1440, dpr 2, two tabs at different
 * times: a database row's side peek and center peek each opened as the row's
 * own page with "모든 토론" docked beside it). The peek asks it here and opens
 * the page whole its own way (its ⤢); the page opening then takes the ask as
 * it shows (PageView, useRowPageDockedPanel).
 */
let panelAsked: { pageId: string; kind: KeptSidePanel; at: number } | null = null;
// Long enough for a page read from the server, short enough that a later
// visit to the page doesn't open the panel.
const PANEL_ASKED_FOR_MS = 4000;

/** Opens the page whole (`openWhole`, the peek's ⤢) with the panel docked beside it. */
export function openWholeWith(pageId: string, kind: KeptSidePanel, openWhole: () => void) {
  panelAsked = { pageId, kind, at: Date.now() };
  openWhole();
}

/** Opens the page whole with its updates docked beside it (openWholeWith). */
export function openWholeWithUpdates(pageId: string, openWhole: () => void) {
  openWholeWith(pageId, "updates", openWhole);
}

/** The panel the page opening now was asked to show: for a moment, however often it is drawn then. */
function panelAskedWhole(pageId: string) {
  return panelAsked && panelAsked.pageId === pageId && Date.now() - panelAsked.at < PANEL_ASKED_FOR_MS ? panelAsked.kind : null;
}

/**
 * Whether the page's updates, asked open beside it, show: only to one who may
 * open them (opensPageUpdates) — kept open there, or asked from a peek, before
 * their access fell to reading or commenting, they stay shut.
 */
export function updatesBesideShown(asked: boolean, canEdit: boolean) {
  return asked && opensPageUpdates(canEdit);
}

/** The panel the page opens with beside it: the one asked from a peek, else the one kept open there (pageSidePanelKept). */
export function panelOpeningBeside(pageId: string): KeptSidePanel | null {
  return panelAskedWhole(pageId) ?? keptSidePanel(pageId);
}
