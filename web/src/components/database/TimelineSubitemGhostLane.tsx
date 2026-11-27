"use client";

import { useState, type MouseEvent as ReactMouseEvent } from "react";
import { readsRightToLeft } from "@/lib/readingDirection";
import styles from "./databaseSheet";

// How far a point lies into a box along the reading direction: from its left,
// or from its right where the screen reads from the right (the days run from
// there, as Notion's in Hebrew, seen 2026-11-11).
export function inlineInto(box: Element, clientX: number) {
  const rect = box.getBoundingClientRect();
  return readsRightToLeft(box) ? rect.right - clientX : clientX - rect.left;
}

/** Pointed at, the bar a sub-item made here would be; pressed, it is made there (Notion's, seen 2026-11-05). */
export function TimelineSubitemGhostLane({ lane, dayWidth, columns, onCreate }: { lane: number; dayWidth: number; columns: number; onCreate(first: number): void }) {
  const [at, setAt] = useState<number | null>(null);
  const columnOf = (event: ReactMouseEvent<HTMLElement>) => Math.max(0, Math.floor(inlineInto(event.currentTarget, event.clientX) / dayWidth));
  return <div className={styles.timelineSubitemLane} data-timeline-subitem-lane={lane} style={{ gridColumn: "1 / -1", gridRow: lane + 1 }}
    onMouseMove={(event) => setAt(columnOf(event))} onMouseLeave={() => setAt(null)} onClick={(event) => onCreate(columnOf(event))}>
    {at === null ? null : <span className={styles.timelineSubitemGhost} aria-hidden="true" style={{ insetInlineStart: at * dayWidth, width: columns * dayWidth }} />}
  </div>;
}
