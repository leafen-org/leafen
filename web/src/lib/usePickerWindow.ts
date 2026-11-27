import { type CSSProperties, type RefObject, useLayoutEffect, useRef, useState } from "react";
import { defaultRangeExtractor, observeElementRect, type Range, type Rect, useVirtualizer } from "@tanstack/react-virtual";
import { placeInScrollBox } from "./scrollBoxPlace";

/** A line of a picker's list as it draws: its place, its key and, while the
 * list draws only what it shows, where it stands among the lines. */
export type PickerWindowLine = { index: number; key: string; top?: number };

// Up to this many lines a picker draws whole, as it always did.
const WHOLE_UP_TO = 100;

/**
 * A picker's lines, drawn only around what its list shows once they are
 * many (a relation's or a formula preview's 100,000 rows of a large
 * database drew as many options and held the tab for over 20 seconds,
 * 2026-11-11): TanStack Virtual places the lines it shows in the element
 * that scrolls them, measured as they draw, standing where they would stand
 * drawn whole. The highlighted line (`focused`, the picker's `active`)
 * always draws, so the arrow keys move onto a line that is there and it is
 * scrolled to (`useKeyWalkScroll`). Drawn whole, the lines are the list's own children;
 * windowed, they go in the box `linesRef` names, `height` high, each placed
 * by `pickerWindowLine`.
 */
export function usePickerWindow({ scrollRef, open, count, keyAt, sizeAt, gap = 0, focused }: {
  scrollRef: RefObject<HTMLElement | null>; open: boolean; count: number; keyAt(index: number): string;
  /** A line's height as it is expected to draw, before it is measured. */
  sizeAt(index: number): number;
  /** The space between two lines. */
  gap?: number;
  focused: number;
}) {
  const windowed = open && count > WHOLE_UP_TO;
  const linesRef = useRef<HTMLDivElement>(null);
  // Where the lines begin in what scrolls, below what stands above them — in
  // its own pixels while its popover grows into place (placeInScrollBox).
  const [margin, setMargin] = useState(0);
  useLayoutEffect(() => {
    const scroller = scrollRef.current, lines = linesRef.current;
    if (!windowed || !scroller || !lines) return;
    const next = placeInScrollBox(lines, scroller).top + scroller.scrollTop;
    setMargin((current) => Math.abs(current - next) < 0.5 ? current : next);
  });
  const pinned = windowed && focused >= 0 && focused < count ? focused : -1;
  const virtualizer = useVirtualizer<HTMLElement, HTMLElement>({
    count, enabled: windowed, getScrollElement: () => scrollRef.current, estimateSize: sizeAt, gap, overscan: 8,
    observeElementRect: (instance, notify) => observeElementRect(instance, (rect) => notify(withinWindow(rect))),
    scrollMargin: margin, getItemKey: keyAt,
    rangeExtractor: (range: Range) => {
      const indexes = defaultRangeExtractor(range);
      return pinned < 0 || indexes.includes(pinned) ? indexes : [...indexes, pinned].sort((left, right) => left - right);
    },
  });
  if (!windowed) {
    const lines: PickerWindowLine[] = Array.from({ length: count }, (_, index) => ({ index, key: keyAt(index) }));
    return { windowed, lines, linesRef, height: undefined, measure: undefined };
  }
  const lines: PickerWindowLine[] = virtualizer.getVirtualItems().map((item) => ({ index: item.index, key: String(item.key), top: item.start - margin }));
  return { windowed, lines, linesRef, height: virtualizer.getTotalSize(), measure: virtualizer.measureElement };
}

/**
 * What scrolls the lines, no taller than the window: a list can show no more
 * than that. A list measured before what holds it has a height of its own —
 * an automation's value window before its popover is placed — stood as tall
 * as all its lines and drew every one of them once (2026-11-22: 5,001 pages
 * drawn and taken down again, 640 ms to open).
 */
function withinWindow(rect: Rect): Rect {
  return typeof window === "undefined" ? rect : { width: rect.width, height: Math.min(rect.height, window.innerHeight) };
}

/** The box of a windowed list's lines, which they stand in; the browser's
 * scroll anchoring leaves it be, as the lines in it come and go. */
export const pickerWindowBox = (height: number | undefined): CSSProperties => ({ position: "relative", flex: "none", height, overflowAnchor: "none" });

/** A windowed line's own box: where it stands, measured as it draws. */
export function pickerWindowLine(line: PickerWindowLine, measure: ((element: HTMLElement | null) => void) | undefined) {
  return { "data-index": line.index, ref: measure, role: "presentation" as const,
    style: { position: "absolute", top: line.top, insetInline: 0 } as CSSProperties };
}
