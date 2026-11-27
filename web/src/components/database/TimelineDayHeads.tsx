import { memo, useLayoutEffect, useRef, useState, type MutableRefObject, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { tipRequest } from "@/lib/tipPlacement";
import type { TimelineDayLabel, TimelineMonthSpan, TimelineNowLabel } from "./timelineViewModel";
import styles from "./databaseSheet";
import tickStyles from "./TimelineTicks.module.css";
import { TimelineTickLines } from "./TimelineTickLines";
import tipStyles from "../IconTipButton.module.css";
import { useTipPress } from "@/lib/tipTiming";

type TimelineDayHeadsProps = {
  days: readonly Date[];
  monthSpans: readonly TimelineMonthSpan[];
  gridTemplate: string;
  /** A day's label, its number or its weekday and number, or an hour's; made again with each drawing of the view. */
  dayLabel(day: Date): TimelineDayLabel;
  /** What a labelled day says pointed at, if anything: its weekday's name. */
  dayTip?(day: Date): string;
  dayLabelOnTick?: boolean;
  /** How many columns apart lines run down the days from their ticks, if any (timelineLineColumns). */
  lineColumns?: number;
  todayIndex: number;
  /** A column's width: how far apart the ticks stand. */
  dayWidth: number;
  /** Where now stands, when it is in the shown days: its dot on the whole pixel it falls in, its time (`nowLabel`) centred on it. */
  nowOffset?: number;
  /** Under the hour zooms, now's time over its line (TimelineNowTime). */
  nowLabel?: TimelineNowLabel;
  /** Whether the days are whole days, whose weekends and today are marked. */
  wholeDays: boolean;
  /** Drawn under the dates, on its own (TimelineRangePill). */
  layer?: ReactNode;
};

type DayTipShown = { index: number; label: string; anchor: PopoverRect };

/**
 * The timeline's months and days, which stay under the page's top as it
 * scrolls past them (useStickyViewHeads). Drawn again only when the days
 * shown, today or where they stand change: their labels follow from the
 * days, which a new zoom, step or reading language makes afresh, so the
 * label ways made again with each drawing of the view are not compared. A
 * row made or moved drew all its days again (a year of them, two date labels
 * each: some 13 ms a drawing, four drawings a press, 2026-11-13).
 *
 * A labelled day pointed at names its weekday in the dark tip, 6px over its
 * line and centred on its label, as Notion's (seen 2026-11-15 at 1440:
 * "목요일" over 19 under 월, "화요일" under 주 and 2주, "월요일" over a
 * Monday under 분기 and 년; none over hours nor under 5년). Its tip draws
 * apart from the days (DayTipLayer), so pointing at them draws them not again.
 * New days under a pointer that stays (another zoom chosen from the keyboard,
 * its menu over them meanwhile) close it, and it comes again only as the
 * pointer goes into another day, as Notion's (seen 2026-11-15 at 1440, two
 * tabs: no tip after 월→주 and 주→월, none for a move within the day under
 * it; Leafen kept the tip it had, saying the weekday of the day that stood
 * there before, or drew the new day's at once).
 */
export const TimelineDayHeads = memo(function TimelineDayHeads(props: TimelineDayHeadsProps) {
  const showTip = useRef<((tip: DayTipShown | null) => void) | null>(null);
  // Where the pointer last stood over the days, and where it stood when they changed under it or a
  // menu came over them: the browser's own pointing after that, at that same point, is no going into a day.
  const pointer = useRef<{ x: number; y: number } | null>(null), stillAt = useRef<{ x: number; y: number } | null>(null);
  const shownDays = useRef(props.days);
  useLayoutEffect(() => {
    if (shownDays.current === props.days) return;
    shownDays.current = props.days;
    if (pointer.current) stillAt.current = pointer.current;
    showTip.current?.(null);
  }, [props.days]);
  // A mouse's alone: a finger's press is no pointing.
  const pointAt = (event: ReactPointerEvent<HTMLElement>) => {
    pointer.current = { x: event.clientX, y: event.clientY };
    if (stillAt.current?.x === event.clientX && stillAt.current.y === event.clientY) return;
    stillAt.current = null;
    const cell = event.pointerType === "mouse" ? (event.target as Element).closest<HTMLElement>("[data-day-index]") : null;
    const day = cell ? props.days[Number(cell.dataset.dayIndex)] : undefined;
    if (!cell || !day || !props.dayTip) return showTip.current?.(null);
    const line = cell.getBoundingClientRect();
    const words = (cell.dataset.onTick ? cell.firstElementChild ?? cell : cell).getBoundingClientRect();
    showTip.current?.({ index: Number(cell.dataset.dayIndex), label: props.dayTip(day), anchor: { top: line.top, bottom: line.bottom, left: words.left, right: words.right } });
  };
  return (
    <>
      <div className={styles.timelineMonths} style={{ gridTemplateColumns: props.gridTemplate }} data-sticky-head="">
        {props.monthSpans.map((span) => (
          <div key={span.key} className={styles.timelineMonth} style={{ gridColumn: `${span.start} / span ${span.span}` }}>
            {span.label}
          </div>
        ))}
      </div>
      <div className={styles.timelineDays} style={{ gridTemplateColumns: props.gridTemplate }} data-sticky-head=""
        onPointerOver={pointAt} onPointerMove={(event) => { pointer.current = { x: event.clientX, y: event.clientY }; }}
        onPointerLeave={(event) => { pointer.current = null; stillAt.current = { x: event.clientX, y: event.clientY }; showTip.current?.(null); }}>
        {props.layer}
        {props.nowOffset !== undefined && <div className={styles.timelineNowDot} style={{ insetInlineStart: Math.floor(props.nowOffset) }} />}
        {props.nowOffset !== undefined && props.nowLabel && <TimelineNowTime at={props.nowOffset} label={props.nowLabel} />}
        {props.days.map((day, index) => {
          const head = props.dayLabel(day), label = tickLabel(props, day, index, typeof head === "string" ? head : head.period + head.time);
          // A week's days name their weekday before the number; today's
          // circle holds the number alone, as Notion's (seen 2026-11-03).
          const weekday = /^(\D+)\s(\d+)$/u.exec(label);
          return (
            <div
              key={day.getTime()}
              className={styles.timelineDay}
              data-timeline-day-label={label ? "true" : undefined}
              data-day-index={label ? index : undefined}
              data-on-tick={label && props.dayLabelOnTick ? "true" : undefined}
              data-weekend={day.getDay() === 0 || day.getDay() === 6 ? "true" : undefined}
              data-today={index === props.todayIndex && label && props.wholeDays ? "true" : undefined}
            >
              {typeof head !== "string" && label ? <span className={tickStyles.hour}><span>{head.period}</span>{head.time}</span>
                : weekday ? <><span data-timeline-weekday>{weekday[1]}</span><span>{weekday[2]}</span></> : <span>{label}</span>}
            </div>
          );
        })}
      </div>
      {props.lineColumns ? <TimelineTickLines count={props.days.length} every={props.lineColumns} gridTemplate={props.gridTemplate} /> : null}
      <DayTipLayer show={showTip} />
    </>
  );
}, (previous, next) => previous.days === next.days && sameSpans(previous.monthSpans, next.monthSpans)
  && previous.gridTemplate === next.gridTemplate && previous.dayLabelOnTick === next.dayLabelOnTick && previous.lineColumns === next.lineColumns
  && previous.todayIndex === next.todayIndex && previous.dayWidth === next.dayWidth && previous.nowOffset === next.nowOffset && previous.wholeDays === next.wholeDays
  && previous.nowLabel?.text === next.nowLabel?.text && previous.nowLabel?.apart === next.nowLabel?.apart
  && Boolean(previous.dayTip) === Boolean(next.dayTip) && previous.layer === next.layer);

// The pointed day's tip, drawn on its own as the pointer moves from day to day.
function DayTipLayer({ show }: { show: MutableRefObject<((tip: DayTipShown | null) => void) | null> }) {
  const [tip, setTip] = useState<DayTipShown | null>(null);
  // A press takes it, as Notion's every tip goes as the mouse goes down (lib/tipTiming.ts).
  useTipPress(tip !== null, () => setTip(null));
  show.current = (next) => setTip((current) => (current?.index ?? null) === (next?.index ?? null) && current?.label === next?.label ? current : next);
  const ref = useRef<HTMLSpanElement>(null);
  const place = usePopoverPlacement(ref, tip ? () => tipRequest(tip.anchor, "top", 6) : null, [tip], undefined, false);
  return tip && typeof document !== "undefined" ? createPortal(
    <span ref={ref} className={tipStyles.tip} style={popoverPlacementStyle(place)} role="tooltip">{tip.label}</span>, document.body) : null;
}

/**
 * Now's time under the hour zooms, 12px/500 in now's red on the dates' line,
 * centred on its line in a box as wide as their named columns and 8px more,
 * 3px after its words, as Notion's (seen 2026-11-24 in Chrome at 1440, dpr 2,
 * two tabs: "오후 9:06" in 60.08px under 일, 80.92px under 시간, centred on
 * 4720.5 by its line at 4720). The times of the ticks near it give way
 * (nowClears). Drawn again as each minute starts (useTimelineNow).
 */
function TimelineNowTime({ at, label }: { at: number; label: TimelineNowLabel }) {
  const width = label.apart + 8;
  return <div className={tickStyles.now} style={{ insetInlineStart: at - width / 2, width }}><span>{label.text}</span></div>;
}

/**
 * A day's label. Where labels stand on the ticks (quarter, year, five years)
 * today has one too, its date in the circle centred on its start, and a tick
 * label its box would overlap goes, as Notion's (seen 2026-11-15 at 1440:
 * "15" in a 22px circle in a box 7px either side of it, 16 gone 12px away
 * under 분기 and 5px under 년, 11 and 12 gone 14 and 16px away under 5년;
 * 9 30px away, 10 45px away kept). Under the hour zooms the labels near now
 * give way to its time (nowClears).
 */
function tickLabel(props: TimelineDayHeadsProps, day: Date, index: number, label: string) {
  if (label && props.nowLabel && props.nowOffset !== undefined && nowClears(index * props.dayWidth, props.nowOffset, props.nowLabel)) return "";
  if (!props.dayLabelOnTick || !props.wholeDays || props.todayIndex < 0 || props.todayIndex >= props.days.length) return label;
  if (index === props.todayIndex) return String(day.getDate());
  const today = String(props.days[props.todayIndex].getDate());
  // About 6.7px a digit at 12px: half of today's box and of this label.
  const apart = Math.abs(index - props.todayIndex) * props.dayWidth;
  return label && apart < today.length * 3.4 + 7 + label.length * 3.4 ? "" : label;
}

/**
 * Whether a named tick stands too near now's time to keep its own: nearer its
 * line (the whole pixel it falls in) than half a named column and 26px, the
 * same whatever the time says, as Notion's (seen 2026-11-24 in Chrome at
 * 1440, dpr 2, a tab under 시간 and one under 일, the times over each minute
 * from 21:05 to 22:01: under 일 a time 51.25px before the line gone and one
 * 52.25px before it kept, one 52.91px after it kept and 48.38px after it gone;
 * under 시간 58.86px after gone, 63.73px after kept, 68.5px before kept —
 * neither "오후 9:59" nor the wider "오후 10:00" moved those bounds).
 */
function nowClears(tick: number, nowOffset: number, label: TimelineNowLabel) {
  return Math.abs(tick - Math.floor(nowOffset)) < label.apart / 2 + 26;
}

// The months are worked out again with each drawing of the view, the same while its days are.
function sameSpans(previous: readonly TimelineMonthSpan[], next: readonly TimelineMonthSpan[]) {
  return previous === next || (previous.length === next.length && previous.every((span, index) => {
    const other = next[index];
    return span.key === other.key && span.start === other.start && span.span === other.span && span.label === other.label;
  }));
}
