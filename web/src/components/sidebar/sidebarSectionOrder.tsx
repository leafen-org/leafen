import { createContext, useCallback, useContext, useMemo, useRef, useState } from "react";
import { i18next } from "@/i18n";
import { useTranslation } from "react-i18next";
import { useProductToast } from "@/lib/productToast";
import type { SidebarSectionSort } from "@/workspaceMembershipWire.generated";
import type { Page } from "@/types";
import type { PageActionAccessReader } from "@/lib/pageActionAccess";
import { positionBetween } from "@/lib/ids";
import { confirmAction } from "../ConfirmActionDialog";
import { confirmMovePrivate } from "../movePrivateConfirm";
import { usePageNames } from "@/lib/pageNames";
import { usePageTreeRowContext } from "./pageTreeContext";
import { sectionTopDropPlan, type PageDropPlan } from "./pageTreeModel";
import { usePageSectionDrop } from "./usePageTreeRowDrag";

/**
 * The order of the section a top row sits in — the private or the shared
 * pages — and its turn to "수동" (by hand) as a page is put in it by hand.
 */
export type SectionOrder = {
  sort: SidebarSectionSort;
  /**
   * Once the section is in its order by hand, asked first where it was
   * sorted: the positions its top pages took to keep the order they showed
   * in (none where it was by hand already), for a drop to be placed among;
   * null when the reader said no.
   */
  toManual(): Promise<ReadonlyMap<string, number> | null>;
};

/**
 * The positions that keep a section's top pages in the order shown: those
 * already in that order keep theirs — the longest run of them — and the rest
 * go between them, so the fewest pages move.
 */
export function shownOrderPositions(shown: readonly Page[]): Map<string, number> {
  const count = shown.length;
  // The longest run, in the order shown, whose positions rise.
  const length = new Array<number>(count).fill(1), before = new Array<number>(count).fill(-1);
  let end = -1;
  for (let index = 0; index < count; index += 1) {
    for (let earlier = 0; earlier < index; earlier += 1) {
      if (shown[earlier].position < shown[index].position && length[earlier] + 1 > length[index]) {
        length[index] = length[earlier] + 1;
        before[index] = earlier;
      }
    }
    if (end < 0 || length[index] > length[end]) end = index;
  }
  const kept = new Set<number>();
  for (let index = end; index >= 0; index = before[index]) kept.add(index);
  const positions = new Map<string, number>();
  let last: number | undefined;
  for (let index = 0; index < count;) {
    if (kept.has(index)) {
      last = shown[index].position;
      positions.set(shown[index].id, last);
      index += 1;
      continue;
    }
    // A run of pages out of order goes between the kept pages around it, in
    // even steps: before the first kept one a step apart, after the last too.
    let next = index;
    while (next < count && !kept.has(next)) next += 1;
    const upper = next < count ? shown[next].position : undefined, run = next - index;
    for (let step = 1; step <= run; step += 1) {
      const position = last === undefined ? (upper ?? 0) - (run - step + 1)
        : upper === undefined ? last + step : last + ((upper - last) * step) / (run + 1);
      positions.set(shown[index + step - 1].id, position);
    }
    index = next;
  }
  return positions;
}

/**
 * The shown pages a turn to "수동" puts in the order shown: all but a locked
 * page and one whose access was read and refuses edits, which keep their
 * places. A page whose access isn't read yet moves too — the list shows it at
 * once and the server, judging last, puts back one it refuses — as
 * usePageEditRefused counts such a page (2026-11-16: a sidebar just loaded,
 * whose databases no pointer had come near, kept them in place, and the
 * section turned "새 데이터베이스·w1385 db·시작하기" over into
 * "시작하기·w1385 db·새 데이터베이스"; Notion keeps the order shown).
 */
export function movableShownPages(shown: readonly Page[], access: Pick<PageActionAccessReader, "knows" | "canEdit">) {
  return shown.filter((page) => !page.isLocked && !(access.knows(page.id) && !access.canEdit(page.id)));
}

/**
 * Puts a section's top pages in the order they show by their positions, as
 * Notion's turn to "수동" keeps the order a sorted section showed (seen
 * 2026-11-14: "…수동 정렬한 내용이 삭제됩니다"): only the pages that move are
 * saved, the list showing them at once, each keeping who edited it last and
 * when, as Notion's do (keepLastEdit). Which pages move: movableShownPages.
 * The positions all the pages then have.
 */
export function useKeepShownOrder() {
  const { store, pageAccess, onPageMetadataChange, onPageChange } = usePageTreeRowContext();
  return useCallback((shown: readonly Page[]) => {
    const movable = movableShownPages(shown, pageAccess);
    const positions = shownOrderPositions(movable);
    for (const page of movable) {
      const position = positions.get(page.id);
      if (position === undefined || position === page.position) continue;
      const current = store.get().pagesById[page.id] ?? page;
      void onPageMetadataChange(current, { parentId: current.parentId ?? "", position, keepLastEdit: true }, new AbortController().signal)
        .then((result) => { if (result !== false) onPageChange(typeof result === "object" ? result : { ...current, position }); })
        .catch(() => undefined);
    }
    return positions;
  }, [onPageChange, onPageMetadataChange, pageAccess, store]);
}

const SectionOrderContext = createContext<SectionOrder | null>(null);
export const SectionOrderProvider = SectionOrderContext.Provider;

/** The section order of the rows inside, or null outside the private and shared pages. */
export function useSectionOrder() {
  return useContext(SectionOrderContext);
}

/**
 * Notion's question before a page is put by hand — between its top rows or
 * on its heading — in a section sorted by last edit (seen 2026-11-14 for the
 * private and the shared pages): its order by hand is dropped for the order
 * it shows, which becomes its order by hand. "수동으로 전환" white on red.
 * Its words are Notion's, Korean and English (its language files, read
 * 2026-11-17: confirmDialog.manualSortForSidebarSection, manualSortForPrivate
 * and manualSortForShared — "…수동 정렬한 내용이 삭제됩니다" is the order by hand left before).
 */
export function confirmManualOrder(sectionName: string) {
  return confirmAction({
    message: i18next.t("sidebar:manualOrderConfirm"),
    detail: i18next.t("sidebar:manualOrderConfirmDetail", { section: sectionName }),
    confirmLabel: i18next.t("sidebar:manualOrderConfirmAction"),
    danger: "filled",
  });
}

/**
 * A section's order: "수동" once the reader agrees to put a page in it by
 * hand, its top pages keeping the order they showed in (`shown`, sorted).
 */
export function useSectionOrderValue(sort: SidebarSectionSort, sectionName: string, setManual: () => void,
  shown: () => readonly Page[]): SectionOrder {
  const keepShown = useKeepShownOrder();
  const latest = useRef({ sort, sectionName, setManual, shown });
  latest.current = { sort, sectionName, setManual, shown };
  const toManual = useCallback(async () => {
    if (latest.current.sort !== "edited") return new Map<string, number>();
    if (!await confirmManualOrder(latest.current.sectionName)) return null;
    const positions = keepShown(latest.current.shown());
    latest.current.setManual();
    return positions;
  }, [keepShown]);
  return { sort, toManual };
}

/**
 * Moves a page dropped on a heading — a teamspace's name, the private or
 * the shared pages' — to the top of that place, as a drop between rows
 * moves it: a teamspace's page made private asks first, as there.
 */
export function useMovePageToTop() {
  const pageNames = usePageNames();
  const { t } = useTranslation("pageTreeItem");
  const { notify } = useProductToast();
  const { store, onPageMetadataChange, onPageChange } = usePageTreeRowContext();
  return useCallback(async (pageId: string, plan: PageDropPlan | null, order?: SectionOrder | null) => {
    const dragged = store.get().pagesById[pageId];
    if (!plan || !dragged) {
      notify(t("cantMovePageHere"));
      return;
    }
    if (plan.teamspaceId === "" && dragged.teamspaceId && !await confirmMovePrivate(pageNames.title(dragged))) return;
    const kept = order ? await order.toManual() : new Map<string, number>();
    if (!kept) return;
    // Above the section's first page as its pages now stand.
    const others = [...kept].flatMap(([id, position]) => id === pageId ? [] : [position]);
    const placed = others.length ? { ...plan, position: positionBetween(undefined, Math.min(...others)) } : plan;
    try {
      const result = await onPageMetadataChange(dragged, placed, new AbortController().signal);
      if (result === false) return;
      onPageChange(typeof result === "object" ? result : { ...dragged, ...placed });
    } catch (error) {
      if (!(error instanceof DOMException && error.name === "AbortError")) notify(t("couldntMovePage"), "error");
    }
  }, [notify, onPageChange, onPageMetadataChange, store, t, pageNames]);
}

/**
 * The private or shared pages' section taking a page dropped on its heading
 * or its "모두 보기", as Notion's (seen 2026-11-14): the whole section from
 * its heading's top is tinted while it does, and the page goes to the
 * section's top (sectionTopDropPlan), asking first to put a sorted section
 * in its order by hand. Anywhere else in it the sidebar's drop map decides
 * (sidebarDropSpot): over a row taking nothing — the dragged page's own row,
 * a row under it, one whose access isn't read yet — nothing is lit and a
 * drop does nothing, as Notion's (seen 2026-11-27 in Chrome, two tabs at
 * two times: no tint, no line, no move and no question).
 */
export function useSectionHeadDrop(section: "private" | "shared", sectionName: string, sort: SidebarSectionSort,
  setManual: () => void, sharedPageIds: ReadonlySet<string>, shown: () => readonly Page[]) {
  const { store, pageAccess, canCreateRoot } = usePageTreeRowContext();
  const moveToTop = useMovePageToTop();
  const toManual = useSectionOrderValue(sort, sectionName, setManual, shown).toManual;
  const order = useMemo<SectionOrder>(() => ({ sort, toManual }), [sort, toManual]);
  const [element, setElement] = useState<HTMLElement | null>(null);
  const [over, setOver] = useState(false);
  const planFor = (pageId: string) => sectionTopDropPlan({
    section, pages: store.get().pages, draggedPageId: pageId, sharedPageIds, canEditPage: pageAccess.canEdit, canCreateRoot,
  });
  usePageSectionDrop(element, {
    accepts: (pageId) => planFor(pageId) !== null,
    drop: (pageId) => void moveToTop(pageId, planFor(pageId), order),
  }, setOver);
  return { ref: setElement, over, order };
}
