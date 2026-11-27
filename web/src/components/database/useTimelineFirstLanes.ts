import { type RefObject, startTransition, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { afterNextPaint } from "@/lib/afterPress";
import { holdMeasure } from "./heldStyleWrites";

// The lowest a lane without a height of its own stands (--timeline-lane), for the most lanes a window can show.
const LEAST_LANE = 36;

/**
 * A timeline's bars and table rows drawn a part at a time as it opens, as a
 * table draws its lines (useWindowedRows' `progressive`): before its first
 * frame those of the lanes a window can show from its top, the rest once that
 * frame has painted, as work a press goes before. A 46-row timeline beside its
 * table made every bar's values and every row before its view tab's press
 * first painted (2026-11-15: 46 bars of seven values, ~200 ms at CPU ×2).
 * Opened with its top already scrolled past the window's top, it draws them
 * all before it paints, measured with its other first measures (heldStyleWrites).
 */
export function useTimelineFirstLanes<Bar extends { lane: number }, Row extends { lane: number }>(
  rootRef: RefObject<HTMLElement | null>, laneCount: number, heights: readonly number[] | undefined, bars: readonly Bar[], rows: readonly Row[],
) {
  const [upTo, setUpTo] = useState<number | null>(() => {
    const room = typeof window === "undefined" ? 0 : window.innerHeight;
    let lanes = 0;
    for (let reach = 0; lanes < laneCount && reach < room; lanes += 1) reach += heights?.[lanes] ?? LEAST_LANE;
    return lanes + 1 < laneCount ? lanes + 1 : null;
  });
  const measureOwner = useRef({});
  useLayoutEffect(() => {
    if (upTo === null) return undefined;
    return holdMeasure(measureOwner.current, () => {
      if ((rootRef.current?.getBoundingClientRect().top ?? 0) < -LEAST_LANE) setUpTo(null);
    });
  }, [rootRef, upTo]);
  useEffect(() => upTo === null ? undefined : afterNextPaint(() => startTransition(() => setUpTo(null))), [upTo]);
  const drawnBars = useMemo(() => upTo === null ? bars : bars.filter((bar) => bar.lane < upTo), [bars, upTo]);
  const drawnRows = useMemo(() => upTo === null ? rows : rows.filter((row) => row.lane < upTo), [rows, upTo]);
  return { bars: drawnBars, rows: drawnRows };
}
