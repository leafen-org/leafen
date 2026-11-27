"use client";

import { useState, type PointerEvent as ReactPointerEvent } from "react";
import { activeDateLocale } from "@/lib/i18n";
import { dateInShape } from "./dateShapes";
import styles from "./TimelineResizeEdge.module.css";

/**
 * One end of a timeline bar, pressed to stretch the bar from it. Pointed at
 * or stretching, it shows a short upright grip on the bar's edge and that
 * edge's day in a small label outside the bar, as Notion's (seen 2026-11-24
 * in Chrome at 1685, dpr 1, a synthetic database at month, week and year
 * zoom: "11월 28일" 10px past the end, "11월 22일" 10px before the start,
 * month and day alone — "1월 3일" for a day next year; where the columns
 * are hours, its time too: "11월 24일 오후 1:00" before a start and "11월 24일
 * 오후 7:00" past an end whose last hour began at 6). While stretching, the
 * label names the day the edge would land on. Pressed, it chooses its
 * bar (TimelineView); let go without a drag it opens nothing (seen twice
 * 2026-11-24: an end and a start clicked, the bar chosen alone, no page open).
 */
export function TimelineResizeEdge({ side, at, hour, stretching, onPointerDown }: {
  side: "start" | "end";
  /** The column the edge stands at: its first or its last; where it would land while stretching. */
  at: Date;
  /** The columns' length in ms where they are hours (an end then names when its last one ends), else null. */
  hour: number | null;
  stretching: boolean;
  onPointerDown(event: ReactPointerEvent<HTMLElement>): void;
}) {
  const [pointed, setPointed] = useState(false);
  const shown = stretching || pointed;
  return <span className={styles.edge} data-side={side} data-shown={shown ? "true" : undefined} onPointerDown={onPointerDown} onClick={(event) => event.stopPropagation()}
    onPointerEnter={(event) => { if (event.pointerType === "mouse") setPointed(true); }} onPointerLeave={() => setPointed(false)}>
    {shown ? <><span className={styles.grip} aria-hidden="true" /><span className={styles.day} aria-hidden="true">{hour === null ? dateInShape(at, "shortDay", activeDateLocale())
      : dateInShape(side === "end" ? new Date(at.getTime() + hour) : at, "shortDayTime", activeDateLocale())}</span></> : null}
  </span>;
}
