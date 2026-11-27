import { type CSSProperties, type Key, type RefObject, startTransition, useEffect, useLayoutEffect, useState } from "react";
import { flushSync } from "react-dom";
import { defaultRangeExtractor, type Range, useVirtualizer, useWindowVirtualizer } from "@tanstack/react-virtual";
import { afterNextPaint } from "./afterPress";

// A list drawn a part at a time (`progressive`) no longer than this draws
// whole at once.
const PROGRESSIVE_LEAST = 20;
// The least a line stands high, for finding the lines the window shows.
const LEAST_LINE = 30;

/** How many of a list's `count` lines stand above the window's foot, at
 * most; none while all of it is above the window's top. */
function linesInView(list: HTMLElement | null, count: number) {
  const box = list?.getBoundingClientRect();
  if (box && box.bottom < 0) return 0;
  let lines = 0;
  for (let at = box?.top ?? 0; lines < count && at < window.innerHeight; lines += 1) at += LEAST_LINE;
  return lines;
}

/** The element that scrolls `element` up and down, or null for the window.
 * A box that scrolls only sideways (a wide table's) does not count. */
function scrollingAncestor(element: HTMLElement): HTMLElement | null {
  for (let node = element.parentElement; node; node = node.parentElement) {
    if (/(auto|scroll)/u.test(window.getComputedStyle(node).overflowY) && node.scrollHeight > node.clientHeight + 1) return node;
  }
  return null;
}

/** A gap's style: its height, and no part in the browser's scroll anchoring,
 * which would otherwise follow a gap as the rows above it draw and jump the
 * page. */
export function windowGapStyle(size: number): CSSProperties {
  return { height: size, overflowAnchor: "none" };
}

// The lists that keep their items' lines (useWindowedRows `lines`), marked so.
const HELD_LINES = "data-windowed-lines";
const heldLines = new WeakMap<Element, () => readonly string[]>();

/** Every item's line a windowed list in `root` holds, drawn or not; undefined where none keeps them. */
export function windowedLines(root: Element | null): readonly string[] | undefined {
  const list = root?.querySelector(`[${HELD_LINES}]`);
  return list ? heldLines.get(list)?.() : undefined;
}

export type WindowedRowEntry = { kind: "row"; index: number; key: Key } | { kind: "gap"; size: number; key: string };

export type WindowedRows = {
  /** The rows to draw in order, with gaps standing in for the rows between. */
  entries: readonly WindowedRowEntry[];
  /** Space standing in for the rows after the last one drawn. */
  after: number;
  /** Measures a drawn row as it draws, when the list is windowed; the row
   * carries its index as `data-index`. */
  measure?: (element: HTMLElement | null) => void;
  /** Brings a row into the drawn range and into view. */
  reveal(index: number): void;
};

/**
 * A long list of rows drawn only around what its page shows, as TanStack
 * Virtual places them in the element (or window) that scrolls the page: rows
 * are measured as they draw and gaps stand in for the rest, so the page
 * scrolls as if every row were there. `pinned` rows (one being dragged or
 * edited) stay drawn. A list of `threshold` rows or fewer draws whole —
 * `progressive`, the lines its first frame shows before that frame and the
 * rest after it, that many lines a frame as work that gives way to a press,
 * a gap standing in for them meanwhile: a page opening with three tables of
 * 50 rows made all their cells in one go before its first frame
 * (2026-11-13: 18k components, some 350–430 ms). `lines`, the lines of the
 * items it holds, each in order, drawn or not, are kept on the list for what
 * acts on them all (windowedLines: ⌘A choosing a view's every item).
 */
export function useWindowedRows({ listRef, count, threshold, estimateSize, getItemKey, pinned, pinFor, progressive = 0, lines }: {
  listRef: RefObject<HTMLElement | null>;
  count: number;
  threshold: number;
  estimateSize: (index: number) => number;
  getItemKey: (index: number) => Key;
  pinned: readonly number[];
  /** Lines kept drawn for where the window starts, as a group's heads that stay at the page's top. */
  pinFor?: (start: number) => readonly number[];
  /** Lines drawn a frame after the first, or none to draw whole at once. */
  progressive?: number;
  /** The lines of the items it holds, in order, drawn or not. */
  lines?: () => readonly string[];
}): WindowedRows {
  useLayoutEffect(() => {
    const list = listRef.current;
    if (!list || !lines) return;
    heldLines.set(list, lines);
    list.setAttribute(HELD_LINES, "");
  });
  // undefined until the list has drawn and its scroller is known.
  const [scroller, setScroller] = useState<HTMLElement | null | undefined>(undefined);
  const [margin, setMargin] = useState(0);
  const large = count > threshold;
  const windowed = large && scroller !== undefined;
  // How many lines a list drawn whole has drawn: -1 until it has lines and
  // its first frame's are found, null once all are.
  const [drawn, setDrawn] = useState<number | null>(progressive > 0 && (count === 0 || count > PROGRESSIVE_LEAST) ? -1 : null);
  const partial = progressive > 0 && !large && drawn !== null;
  // The lines down to the window's foot, found where the list stands before
  // they are painted; a short list draws whole.
  useLayoutEffect(() => {
    if (!partial || drawn !== -1 || count === 0) return;
    const lines = linesInView(listRef.current, count);
    setDrawn(count <= PROGRESSIVE_LEAST || lines >= count ? null : lines);
  }, [count, drawn, listRef, partial]);
  // A scroll that brings lines not yet drawn into view — the page's place
  // given back as it opens, a quick scroll — draws them before it paints.
  useEffect(() => {
    if (!partial || drawn === null || drawn < 0) return undefined;
    const reach = () => {
      const lines = linesInView(listRef.current, count);
      if (lines > drawn) flushSync(() => setDrawn((current) => current === null || current >= lines ? current : lines >= count ? null : lines));
    };
    document.addEventListener("scroll", reach, { capture: true, passive: true });
    return () => document.removeEventListener("scroll", reach, { capture: true });
  }, [count, drawn, listRef, partial]);
  // The rest once each frame has painted, a press first.
  useEffect(() => {
    if (!partial || drawn === null || drawn < 0) return undefined;
    return afterNextPaint(() => startTransition(() => {
      setDrawn((current) => current === null || current + progressive >= count ? null : current + progressive);
    }));
  }, [count, drawn, partial, progressive]);
  // Found once the list is long, when what scrolls the page overflows.
  useLayoutEffect(() => {
    if (large && listRef.current) setScroller(scrollingAncestor(listRef.current));
  }, [large, listRef]);
  // How far down the scrolled content the list begins; what is above it may
  // grow or shrink at any time.
  useLayoutEffect(() => {
    const list = listRef.current;
    if (!windowed || !list) return;
    const measure = () => {
      const top = list.getBoundingClientRect().top;
      const next = scroller ? top - scroller.getBoundingClientRect().top + scroller.scrollTop : top + window.scrollY;
      setMargin((current) => Math.abs(current - next) < 0.5 ? current : next);
    };
    measure();
    const observer = new ResizeObserver(measure);
    for (const child of Array.from((scroller ?? document.body).children)) observer.observe(child);
    window.addEventListener("resize", measure);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [listRef, scroller, windowed]);

  const rangeExtractor = (range: Range) => {
    const indexes = new Set(defaultRangeExtractor(range));
    for (const index of [...pinned, ...pinFor?.(range.startIndex) ?? []]) if (index >= 0 && index < range.count) indexes.add(index);
    return [...indexes].sort((left, right) => left - right);
  };
  const shared = { count, estimateSize, getItemKey, overscan: 8, scrollMargin: margin, rangeExtractor };
  const inElement = useVirtualizer<HTMLElement, HTMLElement>({
    ...shared, enabled: windowed && scroller !== null, getScrollElement: () => scroller ?? null,
  });
  const inWindow = useWindowVirtualizer<HTMLElement>({ ...shared, enabled: windowed && scroller === null });
  const virtualizer = scroller ? inElement : inWindow;

  if (!large) {
    const shown = partial ? Math.max(0, Math.min(count, drawn ?? count)) : count;
    let after = 0;
    for (let index = shown; index < count; index += 1) after += estimateSize(index);
    return {
      entries: Array.from({ length: shown }, (_, index) => ({ kind: "row", index, key: getItemKey(index) })),
      after,
      reveal() { if (partial) setDrawn(null); },
    };
  }
  // A long list draws nothing until its scroller is found, before paint.
  if (!windowed) return { entries: [], after: count * estimateSize(0), reveal() {} };
  const entries: WindowedRowEntry[] = [];
  let cursor = margin;
  for (const item of virtualizer.getVirtualItems()) {
    if (item.start > cursor + 0.5) entries.push({ kind: "gap", size: item.start - cursor, key: `gap:${item.index}` });
    entries.push({ kind: "row", index: item.index, key: item.key });
    cursor = Math.max(cursor, item.end);
  }
  return {
    entries,
    after: Math.max(0, margin + virtualizer.getTotalSize() - cursor),
    measure: virtualizer.measureElement,
    reveal: (index) => virtualizer.scrollToIndex(index, { align: "auto" }),
  };
}
