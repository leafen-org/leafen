import { useSyncExternalStore } from "react";
import { afterNextPaint } from "@/lib/afterPress";

/**
 * A page as the product reads the one drawn at the address: its id, its name
 * as its window names it, whether it is in the trash, and the page it sits
 * under as its own records say (a database's page: the page holding its
 * database). The product's own reading is useDrawnPage.ts.
 */
export type DrawnPage = { id: string; title: string; inTrash: boolean; parentId: string | null };

// The database pages drawn whole — opened full at their own address or over
// their database's page (`?p=<page>&pm=f`, as search, the trash, backlinks
// and the library open one) — each told by its page while it shows
// (useFullRowWindowTitle.ts). What a page tells is taken in once the frame it
// was drawn in is painted, as its body's is (lib/openPageBody.ts): the
// product drawn again in that frame would hold the page's own first frame.
const told = new Map<string, DrawnPage>();
const listeners = new Set<() => void>();
const changes: (() => void)[] = [];
const later = (change: () => void) => {
  changes.push(change);
  if (changes.length > 1) return;
  afterNextPaint(() => {
    for (const made of changes.splice(0)) made();
    for (const listener of [...listeners]) listener();
  });
};
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};

/** Tells the product a database's page is drawn whole until the returned call. */
export function tellRowDrawnWhole(page: DrawnPage): () => void {
  later(() => told.set(page.id, page));
  return () => later(() => {
    if (told.get(page.id) === page) told.delete(page.id);
  });
}

/** The database page `id` drawn whole, as it told itself; null when none is. */
export function useRowDrawnWhole(id: string | null): DrawnPage | null {
  return useSyncExternalStore(subscribe, () => (id ? told.get(id) ?? null : null), () => null);
}
