import { useEffect, useRef, useSyncExternalStore } from "react";
import { lineRowId } from "@/components/database/viewGroupLines";

/**
 * The items a drag carries wear no chosen look while they go, as Notion's
 * database views (seen 2026-11-24 in Chrome at 1685, dpr 1, a synthetic
 * database: chosen rows of a table, a list and a timeline's table and chosen
 * cards of a gallery and a board dragged by one of them — the blue of their
 * choice went from the items left in place, from a timeline row's bar too,
 * and the see-through copies going with the pointer had none); let go, they
 * are chosen again where they land.
 *
 * A drag of chosen items says which it carries here as it starts and lets
 * them go as it ends (useDragReorder's `carry`, a board's cards); what draws
 * an item chosen asks whether its item is carried (CardChoiceHalo), and a
 * copy of an item going with the pointer is made without it
 * (withoutChosenLook).
 */
let carried: ReadonlySet<string> = new Set();
const listeners = new Set<() => void>();

/** The drag now carries these items (none, `[]`, as it ends); a grouped view's line stands for its row (viewGroupLines.ts). */
export function carryChosen(ids: readonly string[]) {
  if (!ids.length && !carried.size) return;
  carried = new Set(ids.map(lineRowId));
  for (const listener of listeners) listener();
}

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};

/**
 * A drag of a view's items given up with Escape puts them back and lets the
 * view's chosen items go, as Notion's views do (seen 2026-11-24 in Chrome at
 * 1685, dpr 1, a synthetic database: a table's, a list's and a board's
 * chosen rows and cards dragged, and a timeline's bars moved alone or chosen,
 * stretched, or carried by its table's ⠿ — after Escape the items were where
 * they had been, the copies gone and none chosen). A timeline's bar drag says
 * so outright (letGoChosen); a drag of rows or cards (dragReorderPointer,
 * pointerDrag) only while it carries a view's items, so a menu's rows put
 * back keep the menu and what it was opened on (letGoCarried). The views'
 * choices hear it (useChoiceLetGo).
 */
const lettingGo = new Set<() => void>();
export function letGoChosen() {
  carryChosen([]);
  for (const letGo of lettingGo) letGo();
}
export function letGoCarried() {
  if (carried.size) letGoChosen();
}

/** A view's choice let go when a drag of its items is given up (letGoChosen). */
export function useChoiceLetGo(letGo: () => void) {
  const latest = useRef(letGo);
  latest.current = letGo;
  useEffect(() => {
    const listener = () => latest.current();
    lettingGo.add(listener);
    return () => { lettingGo.delete(listener); };
  }, []);
}

/** Whether a drag carries the item now: its chosen look is not drawn. */
export function useCarried(id: string) {
  return useSyncExternalStore(subscribe, () => carried.has(id), () => false);
}

/**
 * What draws a view's item chosen — the halo a chosen card, list row or
 * timeline bar wears (CardChoiceHalo) and a chosen table row's mark
 * (TableGridRow) — taken out of a copy of the item (carriedCopy).
 */
export function withoutChosenLook(copy: HTMLElement) {
  for (const halo of copy.querySelectorAll("[data-card-chosen]")) halo.remove();
  for (const row of [copy, ...copy.querySelectorAll<HTMLElement>("[data-row-selected]")]) row.removeAttribute("data-row-selected");
}
