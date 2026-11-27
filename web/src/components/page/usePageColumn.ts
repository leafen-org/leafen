import { useLayoutEffect, type RefObject } from "react";
import { scrollbarWidth } from "../comments/marginDiscussionLayout";
import { holdStyleWrite } from "../database/heldStyleWrites";
import { useBlockHoverReach } from "../editor/useBlockHoverReach";
import { watchPageSize } from "./pageSizeWatch";
import styles from "./pageColumn.module.css";

let standingScrollbar: number | null = null;

/** How wide a scrollbar stands in this browser: none where scrollbars lie over the page. */
function standingScrollbarWidth() {
  if (standingScrollbar !== null) return standingScrollbar;
  const probe = document.createElement("div");
  probe.style.cssText = "position:absolute;top:-9999px;inset-inline-start:0;width:100px;height:100px;overflow:scroll;visibility:hidden";
  document.body.appendChild(probe);
  standingScrollbar = Math.max(0, probe.offsetWidth - probe.clientWidth);
  probe.remove();
  return standingScrollbar;
}

/**
 * A page document's column of blocks, a page's, a database page's or a
 * preview's: the pointer beside a block is on it (useBlockHoverReach), and
 * where the document keeps a page's column (`column`: a page, or a database
 * page opened as a full page) it is marked (pageColumn.module.css) and told how
 * wide its page is (--page-width), how much narrower a scrollbar would
 * leave it if none stands (--page-scrollbar-room) and the room it gives a
 * side peek beside it (--page-peek-room), as the page's width or its
 * scrollbar changes: its blocks keep to the page as if a scrollbar stood
 * whether one does or not, as Notion's (seen 2026-11-12), while its title
 * takes what the page has (pageColumn.module.css). A preview's blocks keep
 * its column (Notion's peeks, seen 2026-11-12: a limit wider than the page).
 */
export function usePageColumn(ref: RefObject<HTMLElement | null>, column = true) {
  useBlockHoverReach(ref);
  useLayoutEffect(() => {
    const element = ref.current;
    const page = element?.parentElement;
    if (!element || !page || !column) return;
    // Measured, then written once the page's other measures are done too (pageSizeWatch).
    function measure(write: (change: () => void) => void = holdStyleWrite) {
      if (!element || !page) return;
      const width = page.clientWidth;
      const room = Math.max(0, standingScrollbarWidth() - scrollbarWidth(page));
      // The room the page gives a side peek beside it (AppShell), so what
      // keeps the page's own width behind it can (PageView.module.css).
      const main = page.closest<HTMLElement>("[data-app-main]");
      const peekRoom = main ? parseFloat(getComputedStyle(main).marginInlineEnd) || 0 : 0;
      write(() => {
        element.style.setProperty("--page-width", `${width}px`);
        element.style.setProperty("--page-scrollbar-room", `${room}px`);
        element.style.setProperty("--page-peek-room", `${peekRoom}px`);
      });
    }
    measure((change) => change());
    element.classList.add(styles.column);
    const stop = watchPageSize(page, measure);
    return () => {
      stop();
      element.classList.remove(styles.column);
    };
  }, [ref, column]);
}
