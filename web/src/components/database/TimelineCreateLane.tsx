"use client";

import { useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
import { Plus } from "../icons";
import { readsRightToLeft } from "@/lib/readingDirection";
import styles from "./databaseSheet";

/**
 * "+ 새로 만들기" under the bars, as Notion's (seen 2026-11-15 in Chrome at
 * 768 and 1440, at every zoom): the whole line is its button; pointed at, its
 * words give way to the bar a new row would be, from the column under the
 * mouse, and pressed, the row is made there, its bar running `columns`
 * columns on (timelineNewBarColumns) — over that many more days, or that many
 * quarter hours where the columns are hours (`cover`: 12 of them at day zoom,
 * 4 at hours, as Notion's, seen 2026-11-24 in Chrome at 1685, 153.25 and
 * 288.66 wide; at the day zoom from the quarter hour under the pointer, 15:52
 * making 15:45–18:45 and 14:40 14:30–17:30). Pressed by the keyboard, it is made where its words stand, as a
 * press on them makes it. With the table shown the table's "+ 새로 만들기" is
 * the only one, the lane under the days still showing and making the new bar
 * where it is pointed at (Notion's, seen 2026-11-15 in Chrome at 1440):
 * `labelled` false draws no words of its own, which would lie under the
 * table, out of sight but in Tab's way. An undated row's line uses it too,
 * its title in the bar shown and placed where pressed (TimelineUndatedLanes).
 */
export function TimelineCreateLane({ dayWidth, rangeDays, columns, cover = columns + 1, label, labelled = true, lane, title, atPress = false, onCreate, onPoint }: {
  dayWidth: number;
  rangeDays: number;
  columns: number;
  /** The columns the bar covers. */
  cover?: number;
  label: string;
  labelled?: boolean;
  /** A group's lane among the bars' lanes (TimelineGroupLanes), where its words stand as the group's head does. */
  lane?: number;
  /** Written in the bar shown. */
  title?: string;
  /** Placed at the column pressed, the bar shown held there while the pointer goes on pressed, and the press its own, not a box choosing bars. */
  atPress?: boolean;
  onCreate(first: number, columns: number): void;
  /** Where the bar a new row would be stands under the pointer, or none (its days marked over the bars), and how many columns past it it runs. */
  onPoint?(first: number | null, last: number): void;
}) {
  const [at, setShownAt] = useState<number | null>(null);
  const pressed = useRef<number | null>(null);
  const setAt = (next: number | null) => { setShownAt(next); onPoint?.(next, cover - 1); };
  const columnAt = (lane: Element, clientX: number) => laneColumnAt(lane, clientX, dayWidth, rangeDays, cover - 1);
  const create = (event: ReactMouseEvent<HTMLElement>) => {
    const first = pressed.current ?? pressedColumn(event, dayWidth, rangeDays, cover - 1);
    pressed.current = null;
    setAt(null);
    onCreate(first, columns);
  };
  return (
    <div className={styles.timelineCreateLane} data-pointed={at === null ? undefined : "true"} data-own-press={atPress ? "" : undefined}
      data-timeline-lane={lane} style={lane === undefined ? undefined : { gridRow: lane + 1 }}
      onPointerDown={atPress ? (event) => { if (event.button === 0) setAt(pressed.current = columnAt(event.currentTarget, event.clientX)); } : undefined}
      onPointerMove={(event) => { if (event.pointerType === "mouse" && pressed.current === null) setAt(columnAt(event.currentTarget, event.clientX)); }}
      onPointerLeave={() => { pressed.current = null; setAt(null); }} onClick={create}>
      {labelled && <button type="button" className={styles.timelineCreateRow}><Plus size="small" aria-hidden="true" />{label}</button>}
      {at === null ? null : <span className={styles.timelineCreateGhost} aria-hidden="true"
        style={{ insetInlineStart: at * dayWidth + 2, width: cover * dayWidth - 3 }}>{title}</span>}
    </div>
  );
}

/** The column under a point of a line across the days, from their start in the reading direction, its bar kept in the days. */
export function laneColumnAt(lane: Element, clientX: number, dayWidth: number, rangeDays: number, columns: number) {
  const rect = lane.getBoundingClientRect();
  const into = readsRightToLeft(lane) ? rect.right - clientX : clientX - rect.left;
  return Math.max(0, Math.min(rangeDays - 1 - columns, Math.floor(into / dayWidth)));
}

/** The column a press on such a line made: under the pointer, or by the keyboard where its words stand. */
export function pressedColumn(event: ReactMouseEvent<HTMLElement>, dayWidth: number, rangeDays: number, columns: number) {
  const lane = event.currentTarget;
  const words = lane.querySelector("button")?.getBoundingClientRect();
  const x = event.detail === 0 && words ? (words.left + words.right) / 2 : event.clientX;
  return laneColumnAt(lane, x, dayWidth, rangeDays, columns);
}
