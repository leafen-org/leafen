import { useEffect, useLayoutEffect, useMemo, useRef, useState, type MutableRefObject, type PointerEvent as ReactPointerEvent } from "react";
import { activeDateLocale } from "@/lib/i18n";
import { useChosenCards } from "./cardSelection";
import type { TimelineCanvasPreview } from "./TimelineCanvas";
import styles from "./TimelineRangePill.module.css";

type Range = { first: number; last: number };
type Channel = { point(range: Range | null): void; choose(ranges: readonly Range[]): void };
type Bar = { row: { id: string }; first: number; last: number };

/**
 * The days a bar pointed at, moved or chosen covers, marked on the dates over
 * the bars, as Notion's (seen 2026-11-15 at 1440, dark): under a zoom naming
 * every column (hours to month) a 22px pill rounded 11px from 4px before its
 * first column to 4px past its last, level with today's circle, its first and
 * last dates in the text colour, the rest kept grey ("13 14 15" for 13–15,
 * "9 … 12" for 9–12, the bar a new row would be under the pointer too); under
 * quarter, year and five years, whose columns go unnamed, its dates in words
 * in such a pill from 2px into its first column ("11월 17일", "11월 13일 –
 * 15일"), under today's circle. The chosen bars and the one pointed at are
 * marked together in one such pill, from the first of their days to the last.
 * Drawn on its own as the pointer moves, so the dates under it are not drawn
 * again. Over hours none is drawn (TimelineCanvas), as Notion marks nothing
 * there (seen 2026-11-24 in Chrome at 1685, two tabs: bars pointed at and the
 * bar a new row would be, at the day and hours zooms).
 */
export function useTimelineRangePill({ items, preview, dayWidth, days, onTick }: {
  items: readonly Bar[];
  preview?: TimelineCanvasPreview | null;
  dayWidth: number;
  days: readonly Date[];
  onTick: boolean;
}) {
  const channel = useRef<Channel | null>(null);
  const bars = useRef(items); bars.current = items;
  const chosen = useChosenCards();
  // A chosen bar moved carries the chosen bars along the days as far (useTimelineRowReorder), their days marked where they
  // go, as Notion's (seen 2026-11-24 in Chrome: three chosen bars moved two days marked 20 … 28, their new days together).
  const moved = preview?.mode === "move" ? items.find((item) => item.row.id === preview.rowId && chosen?.has(item.row.id)) : undefined;
  const along = moved ? preview!.first - moved.first : 0;
  // A bar moved or stretched shows where it would land — a chosen one among the chosen days — whatever the pointer passes over meanwhile (`dragged`).
  const pointed = !preview ? "" : moved ? "chosen" : `${preview.first}:${preview.last}`;
  useEffect(() => { const [first, last] = pointed.split(":").map(Number); channel.current?.point(pointed.includes(":") ? { first, last } : null); }, [pointed]);
  const dragged = useRef(preview); dragged.current = preview;
  // Marked together, from the first chosen day to the last, the days between them too, as Notion's (seen 2026-11-24 in
  // Chrome: 21–23 and 24–26 chosen marked 21 … 26, 18–20 and 23–25 marked 18 … 25 in one pill, and carried a day 19 … 26).
  // A chosen bar being stretched counts the days it would take (Notion's, seen 2026-11-24: 24–28 stretched by its start to 22 marked 22 … 28 at once).
  // The chosen bars it carries count the days they would take too (timelineStretchAlong).
  const ranges = items.filter((item) => chosen?.has(item.row.id)).map((item) => preview && preview.mode !== "move" && preview.rowId === item.row.id ? preview : preview?.along?.get(item.row.id) ?? item);
  const chosenRanges = ranges.length ? [{ first: Math.min(...ranges.map(({ first }) => first)) + along, last: Math.max(...ranges.map(({ last }) => last)) + along }] : [];
  const chosenKey = chosenRanges.map(({ first, last }) => `${first}:${last}`).join(",");
  const chosenNow = useRef(chosenRanges); chosenNow.current = chosenRanges;
  useEffect(() => { channel.current?.choose(chosenNow.current); }, [chosenKey]);
  const layer = useMemo(() => <RangeLayer channel={channel} dayWidth={dayWidth} days={days} onTick={onTick} />, [dayWidth, days, onTick]);
  return useMemo(() => ({
    layer,
    /** The pointer over the bars: the bar under it, or none. */
    over(event: ReactPointerEvent<HTMLElement>) {
      if (event.pointerType !== "mouse" || dragged.current) return;
      const id = (event.target as Element).closest<HTMLElement>("[data-timeline-row-id]")?.dataset.timelineRowId;
      const bar = id ? bars.current.find((item) => item.row.id === id) : undefined;
      channel.current?.point(bar ? { first: bar.first, last: bar.last } : null);
    },
    /** The bar a new row would be, under the pointer, or none. */
    ghost(first: number | null, columns: number) { if (!dragged.current) channel.current?.point(first === null ? null : { first, last: first + columns }); },
    leave() { if (!dragged.current) channel.current?.point(null); },
  }), [layer]);
}

function RangeLayer({ channel, dayWidth, days, onTick }: { channel: MutableRefObject<Channel | null>; dayWidth: number; days: readonly Date[]; onTick: boolean }) {
  const [pointed, setPointed] = useState<Range | null>(null);
  const [chosen, setChosen] = useState<readonly Range[]>([]);
  channel.current = {
    point: (range) => setPointed((current) => same(current, range) ? current : range),
    choose: setChosen,
  };
  // One pill: the bar pointed at joins the chosen days, as Notion's (seen 2026-11-24 in Chrome: 18–20 and 23–25 chosen, the
  // pointer over 24–26 marked 18 … 26 and over a chosen one 18 … 25).
  const all = pointed ? [pointed, ...chosen] : chosen;
  const shown = all.length ? [{ first: Math.min(...all.map(({ first }) => first)), last: Math.max(...all.map(({ last }) => last)) }] : [];
  const shownKey = shown.map(({ first, last }) => `${first}:${last}`).join(",");
  const ref = useRef<HTMLSpanElement>(null);
  // The first and last dates in the text colour, marked on the dates themselves.
  const edges = useMemo(() => onTick ? [] : shownKey.split(",").filter(Boolean).flatMap((range) => [...new Set(range.split(":"))]), [onTick, shownKey]);
  useLayoutEffect(() => {
    const row = ref.current?.parentElement;
    if (!row) return;
    const marked = edges
      .map((index) => row.querySelector<HTMLElement>(`[data-day-index="${index}"]`)).filter((cell): cell is HTMLElement => !!cell);
    marked.forEach((cell) => cell.setAttribute("data-range-edge", "true"));
    return () => marked.forEach((cell) => cell.removeAttribute("data-range-edge"));
  }, [edges]);
  return <span ref={ref} className={styles.layer} data-on-tick={onTick ? "true" : undefined} aria-hidden="true">
    {shown.map(({ first, last }) => onTick
      ? <span key={`${first}:${last}`} className={styles.words} style={{ insetInlineStart: first * dayWidth + 2 }}>{rangeWords(days[first], days[last])}</span>
      : <span key={`${first}:${last}`} className={styles.pill} style={{ insetInlineStart: first * dayWidth - 4, width: (last - first + 1) * dayWidth + 8 }} />)}
  </span>;
}

function same(a: Range | null, b: Range | null) {
  return a === b || (!!a && !!b && a.first === b.first && a.last === b.last);
}

// "11월 17일", or "11월 13일 – 15일" (Notion's, seen 2026-11-15): the end's month and year said only where they differ.
function rangeWords(start: Date | undefined, end: Date | undefined) {
  if (!start) return "";
  const locale = activeDateLocale(), years = !!end && end.getFullYear() !== start.getFullYear();
  const whole = new Intl.DateTimeFormat(locale, { ...(years ? { year: "numeric" } : {}), month: "long", day: "numeric" });
  if (!end || end.toDateString() === start.toDateString()) return whole.format(start);
  const tail = years || end.getMonth() !== start.getMonth() ? whole : new Intl.DateTimeFormat(locale, { day: "numeric" });
  return `${whole.format(start)} – ${tail.format(end)}`;
}
