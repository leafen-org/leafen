import { useLayoutEffect, useRef, type ReactNode } from "react";
import styles from "./FilterMenuList.module.css";

/**
 * The rows of a filter menu with a search over them — the search window's
 * filters and the trash's — as Notion's (seen 2026-11-16 at 1440×723, its
 * search window's "검색 범위:" in two tabs and "생성자"): the search stays
 * where it is and only the rows scroll, 4px in, with the gap the menu keeps
 * between them; while there are more than it shows, its last 34px fade out
 * (241.2 of a 289.2 menu under its 48px search). The menu's frame stays
 * still, so a long list never takes the search out of reach.
 */
export function FilterMenuList({ children }: { children: ReactNode }) {
  const list = useRef<HTMLDivElement>(null);
  // Whether it holds more than it shows, again on every redraw (a search
  // narrows the rows) and when its box changes size (the window's height).
  useLayoutEffect(() => {
    const element = list.current;
    if (!element) return;
    const mark = () => element.toggleAttribute("data-overflowing", element.scrollHeight > element.clientHeight + 1);
    mark();
    const sizes = new ResizeObserver(mark);
    sizes.observe(element);
    return () => sizes.disconnect();
  });
  return <div ref={list} className={styles.list} data-filter-menu-list="">{children}</div>;
}
