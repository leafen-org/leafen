import { type MutableRefObject, useEffect, useRef } from "react";
import { inlineSign, readsRightToLeft } from "@/lib/readingDirection";
import { timelineTableCover } from "./timelineShownEdge";

/** The edge a bar lies past, along the reading direction: the days' start or their end. */
export type BringSide = "start" | "end";

/** How far in from an edge a bar an arrow brings in comes: Notion's 200px at every zoom and width (seen 2026-11-11). */
export const BROUGHT_IN = 200;
/**
 * How long a bar being brought in is followed after the days were last drawn
 * again under it: Chrome's smooth scroll took 0.85 s over 3,500px (2026-11-17).
 */
const FOLLOWED_FOR = 1500;
/** Frames the scroll may stand still before it is started again (a smooth scroll moves every frame once begun). */
const STILL_FRAMES = 3;

/** Where a bar lies along the shown days: its start from their start edge, read as the screen reads. */
export function inlineSpan(view: DOMRect, viewWidth: number, rect: DOMRect, rtl: boolean) {
  return { start: rtl ? view.left + viewWidth - rect.right : rect.left - view.left, length: rect.width };
}

/**
 * The scroll that brings a bar in: past the end the end of its days comes
 * 200px before that edge, past the start their start 200px past the table,
 * however long it is — its days, not the bar drawn inside them
 * (.timelineBar's inline margins), as Notion's (seen 2026-11-17 at 1440: the
 * days 1040..1240 and the bar 1042..1239 by the end edge at 1440; 470..670
 * and 472..669 past the start at 270).
 */
function bringingScroll(scroller: HTMLElement, bar: HTMLElement, side: BringSide) {
  const span = inlineSpan(scroller.getBoundingClientRect(), scroller.clientWidth, bar.getBoundingClientRect(), readsRightToLeft(scroller));
  const style = getComputedStyle(bar);
  const shift = side === "end"
    ? span.start + span.length + (parseFloat(style.marginInlineEnd) || 0) - (scroller.clientWidth - BROUGHT_IN)
    : span.start - (parseFloat(style.marginInlineStart) || 0) - (timelineTableCover(scroller) + BROUGHT_IN);
  return scroller.scrollLeft + inlineSign(scroller) * shift;
}

/** The scroll as far as the days reach: a later one stops at their end. */
function reachable(scroller: HTMLElement, left: number) {
  const room = Math.max(0, scroller.scrollWidth - scroller.clientWidth), sign = inlineSign(scroller);
  return sign * Math.min(Math.max(0, sign * left), room);
}

/** A bar being brought in: the scroll it heads for, until when it is followed, and the frame that follows it. */
type Going = { rowId: string; side: BringSide; left: number; until: number; frame: number };

function letGo(going: MutableRefObject<Going | null>) {
  if (going.current) window.cancelAnimationFrame(going.current.frame);
  going.current = null;
}

/**
 * Brings a drawn bar in by a smooth scroll, its days' end 200px before the end
 * edge or their start 200px past the table, and follows it there.
 *
 * A bar near the end of the timeline's window is reached only as the window
 * is drawn again around its middle (TimelineCanvas's onNearEnd) and the days
 * are placed again under it, each placing cutting the smooth scroll short.
 * Followed each frame, the scroll heads on to where the bar stands now: started
 * again when the bar moved along the days or the scroll stood still short of it,
 * so the bar still ends 200px in, as Notion's (seen 2026-11-17 at 1440: a bar
 * three and a half months on, brought in by its arrow after "오늘"). The
 * viewer's own press, key, wheel or touch lets it go.
 */
export function useTimelineBringing(barElements: MutableRefObject<Map<string, HTMLElement>>) {
  const going = useRef<Going | null>(null);
  useEffect(() => {
    const stop = () => letGo(going);
    const events = ["pointerdown", "keydown", "wheel", "touchstart"] as const;
    for (const name of events) window.addEventListener(name, stop, { capture: true, passive: true });
    return () => {
      for (const name of events) window.removeEventListener(name, stop, { capture: true });
      stop();
    };
  }, []);
  return (scroller: HTMLElement, rowId: string, side: BringSide) => {
    letGo(going);
    const bar = barElements.current.get(rowId);
    if (!bar) return;
    const left = bringingScroll(scroller, bar, side);
    let still = 0, last = scroller.scrollLeft;
    const follow = () => {
      const now = going.current, shown = now ? barElements.current.get(now.rowId) : undefined;
      if (!now) return;
      if (!shown || performance.now() > now.until) { going.current = null; return; }
      const to = bringingScroll(scroller, shown, now.side), at = scroller.scrollLeft;
      // There to the device pixel a scroll stops on: a pixel short, it was cut there.
      if (Math.abs(at - reachable(scroller, to)) <= 0.5 / (window.devicePixelRatio || 1)) { going.current = null; return; }
      still = at === last ? still + 1 : 0;
      last = at;
      const moved = Math.abs(to - now.left) >= 1;
      if (moved || still >= STILL_FRAMES) {
        // Drawn again under it, it is followed a while longer; stood still, only until then.
        going.current = { ...now, left: to, until: moved ? performance.now() + FOLLOWED_FOR : now.until };
        still = 0;
        scroller.scrollTo({ left: to, behavior: "smooth" });
      }
      going.current!.frame = window.requestAnimationFrame(follow);
    };
    going.current = { rowId, side, left, until: performance.now() + FOLLOWED_FOR, frame: window.requestAnimationFrame(follow) };
    scroller.scrollTo({ left, behavior: "smooth" });
  };
}
