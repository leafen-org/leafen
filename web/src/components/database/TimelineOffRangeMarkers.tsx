import { type MutableRefObject, type RefObject, useLayoutEffect, useState } from "react";
import { holdMeasure, holdStyleWrite } from "./heldStyleWrites";
import { ArrowLeft, ArrowRight } from "@/icons/leafen";
import { readsRightToLeft } from "@/lib/readingDirection";
import type { TimelineRenderedRow } from "./TimelineRows";
import type { TimelineAwayLane } from "./timelineLaneProjection";
import { timelineTableCover } from "./timelineShownEdge";
import { useChooseCard } from "./cardSelection";
import { BROUGHT_IN, inlineSpan, useTimelineBringing, type BringSide } from "./useTimelineBringing";
import styles from "./databaseSheet";

/** The edge a bar lies past, along the reading direction: the days' start or their end. */
type Side = BringSide;
/** A row its arrow stands for: a bar drawn, or one dated past the canvas's window (`away`). */
type MarkedRow = Pick<TimelineRenderedRow, "row" | "lane" | "start" | "end">;
/** A bar wholly past that edge, or one the edge cuts, its other part shown. */
type Mark = { item: MarkedRow; side: Side; whole: boolean; away?: TimelineAwayLane };

/** Where a pulled-out title starts past the shown days' start (.timelineBarLabel), and its gap after a bar's end. */
const TITLE_PULLED_IN = 27;
const TITLE_AFTER_END = 5;
const BAR_PADDING = 6;
/**
 * A bar narrower than this keeps its title at its own start and slides off
 * with it, never pulled out beside the arrow (Notion's, seen 2026-11-17 at
 * the month, quarter and year zooms: bars 33, 37, 45 and 47px kept it, 52,
 * 57, 69 and 77px pulled it out — so a one-day bar wholly past the start
 * shows the arrow alone).
 */
const TITLE_PULLED_FROM = 50;

/**
 * A row whose bar lies past an edge of the shown days keeps a gray arrow at
 * that edge of its lane, 8px in, as Notion's timeline (seen 2026-11-06 and
 * 2026-11-11: 16px, round 4), a row dated past the canvas's window too
 * (seen 2026-11-13: a year off, every line its arrow). The days shown start past the table laid
 * over them, so its arrows stand 8px past the table's edge. A bar wholly
 * past keeps its arrow in a 220px box at the edge, pressed as the arrow and
 * showing its dates beside it with the pointer anywhere in it (Notion's,
 * seen 2026-11-17; DatabaseTimelineOffRange.module.css); a bar the edge
 * cuts keeps the arrow over its shown part, and at the start the title of
 * one 50px or wider comes out beside it (.timelineBarLabel), a narrower
 * one's staying at its own start. Where the bar's shown part is
 * too short for its title, the title runs on past the bar's end, 5px after
 * it once that end comes nearer the edge than the pulled-out title, so it
 * slides under the table with the bar (Notion's, seen 2026-11-11: "행 1"
 * at the bar's end + 5 while the bar ends before the table's edge + 23).
 * Pressing the arrow brings the bar 200px in from that edge, one near the
 * window's end followed as the window is drawn again (useTimelineBringing),
 * one past the window by drawing the window again around it (onBringAway), and chooses it
 * alone, the focus left on the page, so Escape or a press on the empty room
 * lets it go (Notion's, seen 2026-11-17 in two tabs: a bar brought from either
 * side wore the chosen halo, the focus on BODY). Its edges are
 * the days' start and end, so where they run from the right a later bar's
 * arrow stands at the left, pointing there.
 */
export function TimelineOffRangeMarkers({ scrollRef, barElements, items, away, onBringAway, layoutKey, dateOf, titleOf }: {
  scrollRef: RefObject<HTMLDivElement | null>;
  barElements: MutableRefObject<Map<string, HTMLElement>>;
  items: readonly TimelineRenderedRow[];
  /** The rows dated past the canvas's window, each wholly past its side. */
  away: readonly TimelineAwayLane[];
  /** Brings in a row dated past the window, its end (or start) this far from the shown middle. */
  onBringAway?(item: TimelineAwayLane, fromMiddle: number): void;
  /** Changes when a bar moves or the table opens, so the marks are measured again. */
  layoutKey: string;
  /** The dates it names beside the arrow of a bar wholly past. */
  dateOf(item: MarkedRow): string;
  titleOf(item: MarkedRow): string;
}) {
  const [marks, setMarks] = useState<Mark[]>([]);
  const choose = useChooseCard();
  const bring = useTimelineBringing(barElements);
  useLayoutEffect(() => {
    const scroller = scrollRef.current;
    if (!scroller) return undefined;
    let frame = 0;
    const measure = () => {
      frame = 0;
      const view = scroller.getBoundingClientRect(), rtl = readsRightToLeft(scroller), cover = timelineTableCover(scroller);
      const shownEnd = scroller.clientWidth, pinned = cover + TITLE_PULLED_IN;
      const next: Mark[] = [];
      const titlesOut: [HTMLElement, number | null][] = [];
      for (const item of items) {
        const bar = barElements.current.get(item.row.id);
        if (!bar) continue;
        const span = inlineSpan(view, scroller.clientWidth, bar.getBoundingClientRect(), rtl), spanEnd = span.start + span.length;
        const titleWidth = bar.querySelector<HTMLElement>(`.${styles.timelineBarLabel!.split(" ")[0]}`)?.scrollWidth ?? 0;
        // Pulled out past the start but too long for the bar's shown part: past its end, at most where it is pulled to;
        // a bar under 50px keeps it at its own start (BAR_PADDING in), where the sticky title would have pinned it.
        const pulled = span.start + BAR_PADDING < pinned, kept = span.length < TITLE_PULLED_FROM;
        const out = pulled && !kept && spanEnd - BAR_PADDING < pinned + titleWidth ? Math.min(pinned, spanEnd + TITLE_AFTER_END) : null;
        titlesOut.push([bar, out !== null ? out - span.start : pulled && kept ? BAR_PADDING : null]);
        // A short bar's title runs on past its end (.timelineBar[data-narrow]); while that shows, it is not past.
        const reach = out === null ? span.start + Math.max(span.length, bar.scrollWidth) : out + titleWidth;
        if (span.start >= shownEnd) next.push({ item, side: "end", whole: true });
        else if (reach <= cover) next.push({ item, side: "start", whole: true });
        else {
          if (span.start < cover) next.push({ item, side: "start", whole: false });
          if (spanEnd > shownEnd) next.push({ item, side: "end", whole: false });
        }
      }
      holdStyleWrite(() => {
        for (const [bar, at] of titlesOut) {
          if (at === null) {
            delete bar.dataset.titleOut;
            bar.style.removeProperty("--timeline-title-out");
          } else {
            bar.dataset.titleOut = "true";
            bar.style.setProperty("--timeline-title-out", `${at}px`);
          }
        }
      });
      setMarks((current) => current.length === next.length && current.every((mark, index) =>
        mark.item.row.id === next[index]!.item.row.id && mark.side === next[index]!.side && mark.whole === next[index]!.whole
          && mark.item.lane === next[index]!.item.lane) ? current : next);
    };
    const schedule = () => { if (!frame) frame = window.requestAnimationFrame(measure); };
    // First with the view's other first measures, its writes after theirs (heldStyleWrites).
    const dropFirst = holdMeasure(measure, measure);
    scroller.addEventListener("scroll", schedule, { passive: true });
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    observer?.observe(scroller);
    // The table's width moves the days' shown start without a scroll.
    const table = scroller.parentElement?.querySelector("[data-timeline-table-pane]");
    if (table) observer?.observe(table);
    return () => {
      dropFirst();
      scroller.removeEventListener("scroll", schedule);
      observer?.disconnect();
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, [barElements, items, layoutKey, scrollRef]);

  function bringIn({ item, side, away: far }: Mark) {
    const scroller = scrollRef.current;
    if (!scroller) return;
    choose?.(item.row.id);
    // Its end comes 200px before the end edge, its start 200px past the table, from the middle.
    if (far) {
      onBringAway?.(far, side === "end" ? scroller.clientWidth / 2 - BROUGHT_IN : timelineTableCover(scroller) + BROUGHT_IN - scroller.clientWidth / 2);
      return;
    }
    bring(scroller, item.row.id, side);
  }

  // Its measured marks for the bars drawn now (a bar gone past the window is measured again a moment later), then the rows past it.
  const drawn = new Set(items.map((item) => item.row.id));
  return <>{[...marks.filter((mark) => drawn.has(mark.item.row.id)), ...away.map((far): Mark => ({ item: far, side: far.side, whole: true, away: far }))].map((mark) => {
    const date = mark.whole ? dateOf(mark.item) : "";
    // Drawn pointing right and left; turned over with the screen (styles.css).
    const Arrow = mark.side === "end" ? ArrowRight : ArrowLeft;
    return (
      <div key={`${mark.away ? "far" : "off"}:${mark.item.row.id}:${mark.side}`} className={styles.timelineOffRange} data-side={mark.side}
        data-cut={mark.whole ? undefined : "true"} style={{ gridRow: mark.item.lane + 1, gridColumn: "1 / -1" }}>
        <button type="button" className={styles.timelineOffRangeButton} data-timeline-off-range={mark.item.row.id}
          aria-label={date ? `${titleOf(mark.item)} · ${date}` : titleOf(mark.item)}
          onClick={(event) => { event.currentTarget.blur(); bringIn(mark); }}>
          {date ? <span className={styles.timelineOffRangeDate}>{date}</span> : null}
          <span className={styles.timelineOffRangeArrow} aria-hidden="true"><Arrow size="micro" /></span>
        </button>
      </div>
    );
  })}</>;
}
