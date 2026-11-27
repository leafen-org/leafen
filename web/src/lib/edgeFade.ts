import { type RefObject, useLayoutEffect } from "react";

/** How far a list fades at an edge it scrolls on past, as Notion's menus (34px). */
const EDGE_FADE = 34;

/**
 * The fade of a list that scrolls (styles.css `[data-edge-fade]`), at each
 * edge it runs on past: 34px, or what is left to scroll there when that is
 * less, as Notion's "@" and "/" menus fade (seen 2026-11-13: at the top none
 * until scrolled, 34px once 120px down; at the bottom 34px, 0.85px with 0.85
 * left), and its move list (seen 2026-11-10). Written on the list itself as it
 * scrolls, resizes or what it shows (`content`) changes, so nothing holding it
 * draws again. Its fade may grow faster than what is left (`rate`): the
 * views list's grows 1.7px a pixel scrolled up to its 34px (Notion's, seen
 * 2026-11-15 at 1440 in two tabs: 8.5px at 5 scrolled, 17 at 10, 25.5 at
 * 15, 34 from 20; 20.4px with 12 left, 10.2 with 6).
 */
export function useEdgeFade(listRef: RefObject<HTMLElement | null>, content: unknown, rate = 1) {
  useLayoutEffect(() => {
    const list = listRef.current;
    if (!list) return undefined;
    const measure = () => {
      const below = list.scrollHeight - list.clientHeight - list.scrollTop;
      list.style.setProperty("--edge-fade-start", `${Math.min(EDGE_FADE, Math.max(0, list.scrollTop) * rate)}px`);
      list.style.setProperty("--edge-fade-end", `${Math.min(EDGE_FADE, Math.max(0, below) * rate)}px`);
    };
    measure();
    list.addEventListener("scroll", measure, { passive: true });
    const observer = new ResizeObserver(measure);
    observer.observe(list);
    return () => {
      list.removeEventListener("scroll", measure);
      observer.disconnect();
    };
  }, [listRef, content, rate]);
}

/**
 * The same fade, measured only where the page is laid out already: as the
 * list scrolls, and as it or a row in it is laid out anew — a row grown,
 * come or gone — by a ResizeObserver, whose answers come after the page is
 * laid out and before it is drawn. For a list drawn again with what holds it,
 * whose rows say nothing of what changed: keyed on them, each drawing read
 * the list's height in its own task (2026-11-13: a menu beside the page menu,
 * drawn again as the page's text was made small, laid the page out there,
 * 123 ms). Its fade may grow faster than what is left (`rate`), as useEdgeFade's.
 */
export function useEdgeFadeAsSized(listRef: RefObject<HTMLElement | null>, rate = 1) {
  useLayoutEffect(() => {
    const list = listRef.current;
    return list ? watchEdgeFadeAsSized(list, rate) : undefined;
  }, [listRef, rate]);
}

/**
 * useEdgeFadeAsSized's watch on a list it is given, for a list held by a
 * callback ref (the sidebar's, SidebarPageSections): the fade written as it
 * scrolls and as it or a row is laid out anew, until the returned stop.
 */
export function watchEdgeFadeAsSized(list: HTMLElement, rate = 1): () => void {
  const measure = () => writeEdgeFade(list, rate);
  // Its first answer, for the list and each row, comes once each is laid out.
  const sized = new ResizeObserver(measure);
  sized.observe(list);
  for (const row of list.children) sized.observe(row);
  // A row come or gone: measured again once laid out (observing anew answers then).
  const rows = new MutationObserver((records) => {
    for (const record of records) {
      for (const node of record.addedNodes) if (node instanceof Element) sized.observe(node);
      for (const node of record.removedNodes) if (node instanceof Element) sized.unobserve(node);
    }
    sized.unobserve(list);
    sized.observe(list);
  });
  rows.observe(list, { childList: true });
  list.addEventListener("scroll", measure, { passive: true });
  return () => {
    list.removeEventListener("scroll", measure);
    rows.disconnect();
    sized.disconnect();
  };
}

/** The fade written on a list for where it is scrolled now (useEdgeFadeAsSized). */
function writeEdgeFade(list: HTMLElement, rate: number) {
  const below = list.scrollHeight - list.clientHeight - list.scrollTop;
  list.style.setProperty("--edge-fade-start", `${Math.min(EDGE_FADE, Math.max(0, list.scrollTop) * rate)}px`);
  list.style.setProperty("--edge-fade-end", `${Math.min(EDGE_FADE, Math.max(0, below) * rate)}px`);
}
