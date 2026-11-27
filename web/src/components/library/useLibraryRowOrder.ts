import { useTranslation } from "react-i18next";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { movedFavoriteOrder, orderFavorites } from "@/components/sidebar/sidebarLayoutModel";
import { confirmManualOrder, movableShownPages, shownOrderPositions } from "@/components/sidebar/sidebarSectionOrder";
import type { SidebarLayout } from "@/workspaceMembershipWire.generated";
import { positionBetween } from "@/lib/ids";
import { usePageActionAccessReader } from "@/lib/pageActionAccessContext";
import type { LibraryViewId } from "@/lib/router";
import { useDragReorder } from "@/lib/useDragReorder";
import type { Page } from "@/types";
import type { LibraryPageActions } from "./LibrarySelectionBar";

/**
 * The favorites' and the private pages' rows put in order by hand, as
 * Notion's library (seen 2026-11-14 and 2026-11-16 at 1440): each top row's
 * handle in the gutter drags it, a blue line where it lands, and "즐겨찾기
 * 상단으로 이동" or "개인 페이지 상단으로 이동" puts the chosen rows first.
 * The favorites' order is the sidebar's (its favoriteOrder), so both follow
 * a move; the private pages' is their own order by hand, the sidebar's
 * "수동". A view sorted (the private pages by last edit, at first) asks
 * first, as Notion's: "수동 정렬로 변경하시겠어요?" — yes takes the order
 * shown as the order by hand, drops the sort and then moves the row; no
 * leaves both. Rows opened under others don't move here.
 */
export function useLibraryRowOrder({ view, sidebar, favoritePageIds, privatePages, shownTops, sorted, actions, onManual }: {
  view: LibraryViewId;
  sidebar: { layout: SidebarLayout; change(layout: SidebarLayout): void };
  favoritePageIds: readonly string[];
  /** The private top pages, in their order by hand. */
  privatePages: readonly Page[];
  /** The top rows as shown, in their order. */
  shownTops: readonly Page[];
  sorted: boolean;
  actions: LibraryPageActions;
  /** Drops the view's sort once its order is by hand. */
  onManual(): void;
}) {
  const { t } = useTranslation(["library", "sidebar"]);
  const access = usePageActionAccessReader();
  const favoriteKeys = orderFavorites([...new Set([...(sidebar.layout.favoriteOrder ?? []), ...favoritePageIds])], (key) => key,
    sidebar.layout.favoriteOrder);

  // A page put at a position; one placed only to keep the order shown keeps
  // who edited it last and when (keepLastEdit), as Notion's.
  function place(page: Page, position: number, keepLastEdit = false) {
    if (page.position === position) return;
    void Promise.resolve(actions.changePageMetadata(page, { parentId: page.parentId ?? "", position, ...(keepLastEdit ? { keepLastEdit } : {}) },
      new AbortController().signal))
      .then((result) => { if (result !== false) actions.onPageChange(typeof result === "object" ? result : { ...page, position }); })
      .catch(() => undefined);
  }

  // The private pages in the order they would show by hand: the order
  // shown, once a sorted view turns to "수동" — the pages about to move
  // (`moving`) are placed once, where they go, not first where they show.
  async function privateOrder(ask: () => Promise<boolean>, moving: ReadonlySet<string>): Promise<Array<{ page: Page; position: number }> | null> {
    if (!sorted) return privatePages.map((page) => ({ page, position: page.position }));
    if (!(await ask())) return null;
    // The sidebar's turn to "수동" moves the same pages (movableShownPages).
    const movable = movableShownPages(shownTops, access);
    const positions = shownOrderPositions(movable);
    for (const page of movable) if (!moving.has(page.id)) place(page, positions.get(page.id) ?? page.position, true);
    onManual();
    const shownIds = new Set(movable.map((page) => page.id));
    return [...movable.map((page) => ({ page, position: positions.get(page.id) ?? page.position })),
      ...privatePages.filter((page) => !shownIds.has(page.id)).map((page) => ({ page, position: page.position }))]
      .sort((left, right) => left.position - right.position);
  }

  const reorder = useDragReorder("library-row", (movedId, targetId, side) => {
    if (view === "favorites") {
      sidebar.change({ ...sidebar.layout, favoriteOrder: movedFavoriteOrder(favoriteKeys, movedId, targetId, side) });
      return;
    }
    const ask = () => confirmAction({ message: t("manualSort.question"), confirmLabel: t("manualSort.yes"), cancelLabel: t("manualSort.no") });
    void privateOrder(ask, new Set([movedId])).then((order) => {
      if (!order) return;
      const moved = order.find(({ page }) => page.id === movedId)?.page;
      const rest = order.filter(({ page }) => page.id !== movedId);
      const at = rest.findIndex(({ page }) => page.id === targetId);
      if (!moved || at < 0 || moved.isLocked) return;
      const index = side === "after" ? at + 1 : at;
      place(moved, positionBetween(rest[index - 1]?.position, rest[index]?.position));
    });
  });

  // The chosen rows first, in the order they show. A sorted view asks
  // first, as Notion's "개인 페이지 상단으로 이동" does (seen 2026-11-16 at
  // 1440): the sidebar's question before a sorted section turns to "수동"
  // (confirmManualOrder), a red "수동으로 전환" and "취소".
  function moveToTop(chosen: readonly Page[]) {
    const ids = new Set(chosen.map((page) => page.id));
    if (view === "favorites") {
      sidebar.change({ ...sidebar.layout, favoriteOrder: [...chosen.map((page) => page.id), ...favoriteKeys.filter((key) => !ids.has(key))] });
      return;
    }
    void privateOrder(() => confirmManualOrder(t("sidebar:privatePages")), ids).then((order) => {
      if (!order) return;
      let next = order.find(({ page }) => !ids.has(page.id))?.position;
      for (const page of [...chosen].reverse()) {
        if (page.isLocked) continue;
        next = positionBetween(undefined, next);
        place(page, next);
      }
    });
  }

  const orderable = view === "favorites" || view === "private";
  const ids = orderable ? shownTops.map((page) => page.id) : [];
  return {
    /** The private pages' shown order put by hand as their sort is taken off, the section turning to "수동" (onManual). */
    keepShownOrder: () => { if (view === "private" && sorted) void privateOrder(() => Promise.resolve(true), new Set()); },
    /** The rows' handles and drop lines, where the view is put in order by hand. */
    rows: orderable ? { reorder, ids, label: t("rowHandle") } : undefined,
    /** The bar's "… 상단으로 이동", where the view has one. */
    top: orderable ? { label: t(view === "favorites" ? "selection.moveFavoritesTop" : "selection.movePrivateTop"), move: moveToTop } : undefined,
  };
}

/** The favorites in the reader's order, the sidebar's (its favoriteOrder). */
export function libraryFavoriteOrder(favoritePageIds: readonly string[], order: readonly string[] | undefined) {
  return orderFavorites(favoritePageIds, (id) => id, order);
}

export type LibraryRowOrder = NonNullable<ReturnType<typeof useLibraryRowOrder>["rows"]>;
