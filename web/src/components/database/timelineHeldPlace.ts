import { useMemo, useRef } from "react";
import { inlineScrolled, inlineSign } from "@/lib/readingDirection";

// Scrolls a timeline so this far along it stands at its start, within its days.
export function scrollInlineTo(scroller: HTMLElement, distance: number) {
  scroller.scrollLeft = inlineSign(scroller) * Math.min(Math.max(0, distance), Math.max(0, scroller.scrollWidth - scroller.clientWidth));
}

type OpenedCenter = { offset: number; showTable: boolean };

/**
 * Where a timeline put its days, kept until its viewer moves them (a press,
 * wheel, key or touch on them, or its pinned bar).
 *
 * Opened where its viewer left it (the offset of that time), that time stays
 * in its middle as its box settles — marked boxed or wide by its page after
 * it opened, its page's scrollbar coming — until the viewer moves it or opens
 * or shuts its table: it opens where it was left, measured as it was left,
 * the same each time.
 *
 * Until its viewer first moves it, its days found back at their start are put
 * back where it put them: a page opened anew (a new tab, a reload) has its
 * editor put a block's content in its place after the timeline in it placed
 * itself, and a box moved loses its scroll offset. Its days came back at their
 * start, half a year before now, which it took for its viewer's scrolling: it
 * was drawn again around that time and kept it as where its viewer left it,
 * opening there since (2026-11-15: 2026년 5월 17일–25일 on a phone, 5월 31일 in
 * the middle at 1440, opened on 11월 15일).
 */
export function useTimelineHeldPlace() {
  const opened = useRef<OpenedCenter | null>(null);
  const held = useRef<number | null>(null);
  const moved = useRef(false);
  return useMemo(() => {
    // Scrolls it, the place held following while it is held.
    const put = (scroller: HTMLElement, distance: number) => {
      scrollInlineTo(scroller, distance);
      if (held.current !== null) held.current = inlineScrolled(scroller);
    };
    return {
      put,
      /** Put where it opens, or where a press took it: the time it opened where its viewer left it, if so. */
      placed(scroller: HTMLElement, center: OpenedCenter | null) {
        opened.current = center;
        if (!moved.current) held.current = inlineScrolled(scroller);
      },
      /** The time it opened where its viewer left it, put back in its middle; whether it was. */
      keepOpened(scroller: HTMLElement, showTable: boolean) {
        if (opened.current && opened.current.showTable !== showTable) opened.current = null;
        if (!opened.current) return false;
        put(scroller, opened.current.offset - scroller.clientWidth / 2);
        return true;
      },
      /** Its days found at their start, not by its viewer: put back where they were; whether they were. */
      restore(scroller: HTMLElement) {
        const at = held.current;
        if (at === null || at < 1 || inlineScrolled(scroller) >= 1) return false;
        scrollInlineTo(scroller, at);
        return true;
      },
      /** Its viewer moved it: it stays where they take it from now on. */
      release() {
        moved.current = true;
        opened.current = null;
        held.current = null;
      },
    };
  }, []);
}
