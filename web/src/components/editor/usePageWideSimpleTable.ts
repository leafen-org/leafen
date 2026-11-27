import { type RefObject, useLayoutEffect } from "react";

/**
 * A page's top simple table whose columns don't fit the room it has scrolls
 * across the page, as Notion's (seen 2026-11-12 at 1440, a 12-column table):
 * its box runs from the page's edge to as far past the text's column as that
 * edge is before it, and its scroller from 12px before the page's edge (the
 * page clips it there) to 8px short of the box's end — 258–1417 by a page at
 * 270 whose text's column is 487.5–1207.5, and 258–1087 with the text moved
 * left for the discussions beside it (322.5–1042.5). Its first column stays
 * where it was. One that fits keeps its box, and so does one on a phone or a
 * row's page. A page too short to scroll keeps the box of one that does,
 * about the text's column moved half a scrollbar over: 265.5–1424.5 by its
 * text at 495–1215 (Notion's, seen 2026-11-13).
 */
export function usePageWideSimpleTable(ref: RefObject<HTMLElement | null>) {
  useLayoutEffect(() => {
    const scroller = ref.current;
    const wrap = scroller?.parentElement;
    const canvas = scroller?.firstElementChild;
    const block = scroller?.closest<HTMLElement>('[data-block-type="table"]');
    const page = scroller?.closest<HTMLElement>("[data-page-scroll]");
    if (!scroller || !wrap || !(canvas instanceof HTMLElement) || !block || !page || block.dataset.depth !== "0"
      || scroller.closest('[data-phone="true"], [data-row-page="true"], [data-column-id]')) return undefined;
    let reach = 0;
    const clear = () => {
      reach = 0;
      delete scroller.dataset.pageWide;
      scroller.style.removeProperty("--page-wide-reach");
      scroller.style.removeProperty("--page-wide-width");
    };
    const apply = () => {
      const frame = page.getBoundingClientRect();
      const start = frame.left + page.clientLeft;
      const end = start + page.clientWidth;
      const box = block.getBoundingClientRect();
      const own = wrap.getBoundingClientRect();
      const rtl = getComputedStyle(scroller).direction === "rtl";
      // From the page's edge to the text's column, on the side lines start,
      // less half the scrollbar a page that doesn't scroll leaves out.
      const unshown = page.offsetWidth - page.clientWidth - page.clientLeft * 2 > 0 ? 0 : scrollbarWidth() / 2;
      const gap = (rtl ? end - box.right : box.left - start) - unshown;
      // The room it has in the text: its box and 46px past it, less its inset.
      const inset = parseFloat(getComputedStyle(scroller).paddingInlineStart) - reach;
      if (gap <= 0 || canvas.offsetWidth <= own.width + 46 - inset) {
        clear();
        return;
      }
      reach = Math.round(((rtl ? end - own.right : own.left - start) - unshown + 12) * 10) / 10;
      scroller.dataset.pageWide = "true";
      scroller.style.setProperty("--page-wide-reach", `${reach}px`);
      scroller.style.setProperty("--page-wide-width", `${Math.round((box.width + 2 * gap + 4) * 10) / 10}px`);
    };
    apply();
    const observer = new ResizeObserver(apply);
    observer.observe(page);
    observer.observe(block);
    observer.observe(canvas);
    // The text moving aside for discussions moves it without resizing it.
    const doc = scroller.closest("[data-page-search-root]");
    const moved = new MutationObserver(apply);
    if (doc) moved.observe(doc, { attributes: true, attributeFilter: ["data-margin-discussions", "style"] });
    return () => {
      observer.disconnect();
      moved.disconnect();
      clear();
    };
  }, [ref]);
}

/** The width a page's scrollbar takes when it shows: 0 where they overlay. */
function scrollbarWidth() {
  const probe = document.createElement("div");
  probe.style.cssText = "position:fixed;visibility:hidden;inset-block-start:0;inset-inline-start:0;width:100px;height:100px;overflow:scroll";
  document.body.appendChild(probe);
  const width = probe.offsetWidth - probe.clientWidth;
  probe.remove();
  return width;
}
