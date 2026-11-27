"use client";

import { useLayoutEffect, useRef, type RefObject } from "react";
import { readsRightToLeft } from "@/lib/readingDirection";
import { holdMeasure } from "./heldStyleWrites";
import { useOwnPageView } from "./useOwnPageViewFill";
import styles from "./OwnPageViewBar.module.css";

/**
 * A view's sideways scrollbar on a database's own page — a table's, a
 * board's, a timeline's days' — kept at the window's foot while the view runs
 * on under it, as Notion's page bar is: its one scroller for both ways puts
 * the bar at the window's foot whatever is shown (seen 2026-11-15 in Chrome at
 * 1440: a timeline's at 716 by its thumb at today; a table of 30 rows by 9
 * properties at 716 before scrolling, and People with its view settings
 * docked at 716 under one row). The view's own bar gives way to it (its box
 * marked data-own-page-bar); it stands where the view's box does, as wide,
 * and moves with it both ways. Its room follows the view, which a short view
 * counts as its own (useOwnPageViewFill). Elsewhere — a page's text, a
 * dashboard's card, a phone — it is not drawn and the view keeps its bar.
 */
export function OwnPageViewBar({ scrollRef, onMove }: { scrollRef: RefObject<HTMLElement | null>; onMove?(): void }) {
  const ownPage = useOwnPageView();
  const barRef = useRef<HTMLDivElement>(null);
  const fillRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const view = scrollRef.current, bar = barRef.current, fill = fillRef.current;
    if (!ownPage || !view || !bar || !fill || typeof ResizeObserver === "undefined") return;
    view.dataset.ownPageBar = "true";
    bar.dataset.shown = "true";
    // As wide as the view's box and where it stands in the box they share
    // (the first one around them drawn as a box: a board's own stands in
    // one laid out as its contents), from its start in the reading direction.
    const place = () => {
      if (getComputedStyle(bar).display === "none") return;
      let parent = bar.parentElement;
      while (parent && getComputedStyle(parent).display === "contents") parent = parent.parentElement;
      if (!parent) return;
      const room = parent.getBoundingClientRect(), box = view.getBoundingClientRect(), frame = getComputedStyle(parent);
      const start = readsRightToLeft(view)
        ? room.right - parseFloat(frame.borderRightWidth) - parseFloat(frame.paddingRight) - box.right
        : box.left - room.left - parseFloat(frame.borderLeftWidth) - parseFloat(frame.paddingLeft);
      bar.style.marginInlineStart = `${start}px`;
      bar.style.width = `${view.clientWidth}px`;
      fill.style.width = `${view.scrollWidth}px`;
      bar.scrollLeft = view.scrollLeft;
    };
    // The bar's own scroll that following the view caused comes a frame later: given back, its place, a
    // moment old or rounded to the bar's pixels, took the view back there — stopping a smooth scroll on its
    // first frame (a timeline's arrow bringing its bar in) and undoing a timeline drawn again around its
    // middle, which then ran to its end and was drawn again a second time past it (2026-11-17).
    let followed: number | null = null;
    const fromView = () => {
      if (bar.scrollLeft === view.scrollLeft) return;
      bar.scrollLeft = view.scrollLeft;
      followed = bar.scrollLeft;
    };
    const fromBar = () => {
      const at = bar.scrollLeft, echo = followed !== null && Math.abs(at - followed) < 1;
      followed = null;
      if (!echo && Math.abs(view.scrollLeft - at) >= 1) view.scrollLeft = at;
    };
    const observer = new ResizeObserver(place);
    observer.observe(view);
    if (view.firstElementChild) observer.observe(view.firstElementChild);
    view.addEventListener("scroll", fromView, { passive: true });
    bar.addEventListener("scroll", fromBar, { passive: true });
    // First placed with the view's other first measures (heldStyleWrites): placed here at once, its style
    // read worked out the whole new timeline's styles alone inside its view tab's press (~30 ms at CPU ×2, 2026-11-15).
    const dropFirst = holdMeasure(barRef, place);
    return () => {
      dropFirst();
      observer.disconnect();
      view.removeEventListener("scroll", fromView);
      bar.removeEventListener("scroll", fromBar);
      delete view.dataset.ownPageBar;
      delete bar.dataset.shown;
    };
  }, [ownPage, scrollRef]);
  if (!ownPage) return null;
  return (
    <div ref={barRef} className={styles.bar} data-pinned-view-bar="" aria-hidden="true" onPointerDown={onMove} onWheel={onMove}>
      <div ref={fillRef} className={styles.fill} />
    </div>
  );
}
