import { type RefObject, useEffect, useState } from "react";

// Notion's "/" menu draws its first 25 rows, typed or not, and 25 more each
// time its list is scrolled to within 200px of their end — by the wheel, or
// by the arrows, which scroll the lit row into view, so ↓ goes on past the
// 25th row and ↑ from none lights the last one drawn — the list staying
// where it is scrolled as they come in under it; a query starts again from
// 25 (seen 2026-11-17 in two tabs: "/e" 25, 50, 75 … 164 rows, the bare "/"
// … 181, "/ " … 177).
export const SLASH_PAGE_ROWS = 25;
const MORE_ROWS_WITHIN = 200;

/**
 * How many of a "/" menu's `total` rows it draws: a page of them, and a page
 * more each time its list (`listRef`) comes within 200px of the end of those
 * drawn, once `drawn` says they all are; the first page again for each query.
 */
export function useSlashMenuPages(listRef: RefObject<HTMLElement | null>, query: string, total: number, drawn: boolean): number {
  const [pages, setPages] = useState({ query, count: 1 });
  let count = pages.count;
  if (pages.query !== query) {
    count = 1;
    setPages({ query, count });
  }
  const shown = Math.min(total, count * SLASH_PAGE_ROWS);
  useEffect(() => {
    const list = listRef.current;
    if (!drawn || !list || shown >= total || typeof IntersectionObserver === "undefined") return;
    const rows = list.querySelectorAll('[role="option"]');
    const last = rows[rows.length - 1];
    if (!last) return;
    // The last row drawn wholly within 200px under the list's visible end
    // (Notion's came at 199–200px from that row's foot).
    const near = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting && entry.intersectionRatio > 0.99)) setPages({ query, count: count + 1 });
    }, { root: list, rootMargin: `0px 0px ${MORE_ROWS_WITHIN}px 0px`, threshold: 1 });
    near.observe(last);
    return () => near.disconnect();
  }, [count, drawn, listRef, query, shown, total]);
  return shown;
}
