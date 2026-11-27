import { useLayoutEffect, type RefObject } from "react";

/**
 * Marks the "토론" list's scroll on it, for its foot to follow as Notion's
 * does (PageDiscussions.module.css, seen 2026-11-25 in Chrome at 1440, dpr 2,
 * two tabs at different times): `data-overflowing` while it holds more than
 * its 384px show, and `data-rows-below` while rows are still under its fold.
 * Marked again on every redraw (rows come and go) and as it scrolls or its
 * box changes size.
 */
export function usePopoverListEdges(list: RefObject<HTMLElement | null>) {
  useLayoutEffect(() => {
    const element = list.current;
    if (!element) return undefined;
    const mark = () => {
      const more = element.scrollHeight > element.clientHeight + 1;
      element.toggleAttribute("data-overflowing", more);
      element.toggleAttribute("data-rows-below", more && element.scrollTop + element.clientHeight < element.scrollHeight - 1);
    };
    mark();
    const sizes = new ResizeObserver(mark);
    sizes.observe(element);
    element.addEventListener("scroll", mark, { passive: true });
    return () => {
      sizes.disconnect();
      element.removeEventListener("scroll", mark);
    };
  });
}
