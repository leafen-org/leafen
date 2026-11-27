"use client";

import type { Page } from "@/lib/types";
import { TimelineCreateLane } from "./TimelineCreateLane";

/** An undated row's lane beside the table: `line` names which of its groups' lines it is (viewGroupLines.ts). */
export type TimelineUndatedLane = { row: Page; lane: number; line?: string };

/**
 * An undated row's line across the days beside the table: pointed at, it shows
 * the bar the row would take from the column under the pointer — as long as a
 * new row's, its title in it, a crosshair over it, its days marked over the
 * bars — and pressed, the row takes those dates there, as Notion's (seen
 * 2026-11-24 in Chrome at 1685, dpr 1, a synthetic database showing its table,
 * in two tabs at month, two-week and day zoom: "바" pointed at on 12/2 at month
 * zoom shows a 197px box "바" over 12/2–12/6 and 2 … 6 marked; clicked it is
 * 12/2–12/6, and "라" pressed on 12/2 and dragged on to 12/9 is 12/2–12/6 too,
 * the box held where pressed while dragged; two weeks, three days; day zoom,
 * three hours; neither chosen nor opened; one ⌘Z takes the dates back).
 */
export function TimelineUndatedLanes({ lanes, newBar, rowTitle, onDate, onPoint }: {
  lanes: readonly TimelineUndatedLane[];
  newBar: { dayWidth: number; rangeDays: number; columns: number; cover: number; label: string };
  rowTitle(row: Page): string;
  onDate(row: Page, first: number, columns: number): void;
  onPoint(first: number | null, last: number): void;
}) {
  return <>{lanes.map((lane) => <TimelineCreateLane key={`undated:${lane.line ?? lane.row.id}`} {...newBar} lane={lane.lane} labelled={false} atPress
    title={rowTitle(lane.row)} onCreate={(first, columns) => onDate(lane.row, first, columns)} onPoint={onPoint} />)}</>;
}
