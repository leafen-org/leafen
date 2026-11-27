"use client";

import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type RefObject } from "react";
import { useDragReorder } from "@/lib/useDragReorder";
import { landingBesideRows } from "@/lib/dragReorderReach";
import { sidePastOwnPlace } from "@/lib/dragReorderOwnPlace";
import { carriedCopy } from "@/lib/dragReorderLine";
import { carryChosen } from "@/lib/carriedChoice";
import { useViewingArchivedRows } from "./databaseArchivedRowsContext";
import { chosenLinesCarried, lineRowId, linesMoved } from "./viewGroupLines";

type Side = "before" | "after";
type Input = { clientX: number; clientY: number };
type Landing = { id: string; side: Side };

/** A row the timeline draws in a lane — its table's row, its bar, or both — under its parent, in its group; `id` names its line (viewGroupLines.ts). */
export type TimelineRowLane = { id: string; lane: number; parentId: string | null; group?: string };
/** Where a dragged row would land: before or after the row in that lane. */
export type TimelineDropLine = { lane: number; side: Side };
export type TimelineRowReorder = ReturnType<typeof useTimelineRowReorder>;

/** How far a bar's press goes before the bar goes with the pointer, and up or down before its row lands among the rows (TimelineView). */
const BAR_LIFT = 4;

/**
 * A timeline's rows moved among themselves, as Notion's (seen 2026-11-24 in
 * Chrome at 1685, dpr 1, a synthetic database's timeline, its table shown and
 * hidden, unsorted, sorted and grouped): by the ⠿ before a table row, which
 * carries a faint copy of each row it carries and of its bar with the
 * pointer, the rows and their bars staying put (seen 2026-11-24 in two tabs:
 * two chosen rows' copies and their bars' at 40%, at their own days, going
 * with the pointer both ways), or by a bar pressed and moved up or down,
 * which keeps moving along the days too, the bar itself going faint with the
 * pointer up and down, and a chosen row's the other chosen rows' bars too,
 * which land with it (seen 2026-11-24 in two tabs: two and three chosen
 * rows, their days kept) — along the days as far as its own, and a chosen
 * row with no bar as a faint copy of its table row (barFollow). Either way a 4px line across the days shows where
 * it lands (TimelineCanvas, under the table), a row is landed on by its nearer
 * half, never on itself, and among the rows of its own parent — the row
 * right after it takes it after itself, either half (sidePastOwnPlace,
 * seen 2026-11-24 by the ⠿ and by a bar); landed in
 * another group it takes that group's value, from the group of the lane it
 * left (a row under two tags', seen 2026-11-24). A chosen row's ⠿ carries the
 * other chosen rows of its parent along (chosenLinesCarried, seen 2026-11-24).
 * Let go, it moves through the one
 * way a view's rows move (moveShownRows), a step of ⌘Z that in a sorted view
 * asks whether the sort goes. Not while the view only reads, its structure is
 * locked or it shows its archived rows; the view's rows move by the order it
 * keeps, which a reader may not change.
 */
export function useTimelineRowReorder({ viewId, rootRef, rows, enabled, chosen, move }: {
  viewId: string;
  rootRef: RefObject<HTMLElement | null>;
  /** The rows with lanes, in their lanes' order. */
  rows: readonly TimelineRowLane[];
  enabled: boolean;
  /** The lines chosen (rows, where the timeline isn't grouped), which a chosen row's ⠿ carries along under its parent (chosenLinesCarried), as Notion's (seen 2026-11-24). */
  chosen?: () => ReadonlySet<string>;
  /** Puts the rows beside the target; `group` is the target's group, `groupOf` each row's own; `withDates`, the bar moved along the days too; `landed`, their lines there (linesMoved). */
  move(rowIds: readonly string[], target: Landing, group: string | undefined, groupOf: (rowId: string) => string | undefined, withDates?: boolean, landed?: readonly string[]): void;
}) {
  const moves = enabled && !useViewingArchivedRows();
  const latest = useRef({ rows, chosen, move });
  latest.current = { rows, chosen, move };
  const rowOf = (id: string) => latest.current.rows.find((row) => row.id === id);
  const takes = (sourceId: string, targetId: string) => lineRowId(sourceId) !== lineRowId(targetId) && rowOf(sourceId)?.parentId === rowOf(targetId)?.parentId;
  const next = (sourceId: string) => timelineRowAfter(latest.current.rows, sourceId);

  // A row's bar in its lane, as the canvas draws it.
  const barOf = (id: string) => [...rootRef.current?.querySelectorAll<HTMLElement>(`[data-timeline-lanes] > [data-timeline-row-id="${CSS.escape(lineRowId(id))}"]`) ?? []]
    .find((candidate) => candidate.style.gridRowStart === String((rowOf(id)?.lane ?? -1) + 1)) ?? null;
  const carriedBy = (sourceId: string) => chosenLinesCarried(latest.current.rows.map(({ id, parentId }) => ({ line: id, parentId })), latest.current.chosen?.() ?? new Set(), sourceId);
  // The faint copies of the bars of the rows a ⠿ carries, going with the pointer as the table's copies of the rows do (dragReorderLine).
  const copies = useRef<HTMLElement[] | null>(null);
  const copyBars = (id: string, input: Input, start: Input) => {
    copies.current ??= carriedBy(id).flatMap((line) => { const bar = barOf(line), copy = bar && carriedCopy(bar); return copy ? [copy] : []; });
    for (const copy of copies.current) copy.style.transform = `translate(${input.clientX - start.clientX}px, ${input.clientY - start.clientY}px)`;
  };
  const dropCopies = () => {
    for (const copy of copies.current ?? []) copy.remove();
    copies.current = null;
  };

  // The bars of the rows a bar carries, its own among them, moved as far as the pointer from where it was pressed, both ways,
  // not by whole days (the days they land on are marked over the bars, TimelineRangePill); a row it carries with no bar, a
  // faint copy of its table row going with the pointer instead, as Notion's (seen 2026-11-24 in Chrome: a bar moved alone
  // 80.6px and 96.7px along and 2px and 16px down went 80px and 96px along and 2px and 16px down at 40%, two chosen ones
  // 44px along and a lane up together; an undated chosen row's table row at 40% beside the chosen bars).
  const carried = useRef<HTMLElement[]>([]);
  const rowCopies = useRef<HTMLElement[] | null>(null);
  const tableRowOf = (id: string) => rootRef.current?.querySelector<HTMLElement>(
    `[data-timeline-table-row-id="${CSS.escape(lineRowId(id))}"][data-timeline-lane="${rowOf(id)?.lane ?? -1}"]`) ?? null;
  const carry = (ids: readonly string[], input: Input, start: Input) => {
    const bars = ids.flatMap((id) => { const bar = barOf(id); return bar ? [bar] : []; });
    if (carried.current.some((bar) => !bars.includes(bar))) settle();
    carried.current = bars;
    for (const bar of bars) {
      bar.dataset.rowCarried = "";
      bar.style.setProperty("--timeline-carried-x", `${input.clientX - start.clientX}px`);
      bar.style.setProperty("--timeline-carried-y", `${input.clientY - start.clientY}px`);
    }
    rowCopies.current ??= ids.flatMap((id) => { const row = barOf(id) ? null : tableRowOf(id), copy = row && carriedCopy(row); return copy ? [copy] : []; });
    for (const copy of rowCopies.current) copy.style.transform = `translate(${input.clientX - start.clientX}px, ${input.clientY - start.clientY}px)`;
  };
  const settle = () => {
    for (const bar of carried.current) {
      delete bar.dataset.rowCarried;
      bar.style.removeProperty("--timeline-carried-y");
      bar.style.removeProperty("--timeline-carried-x");
    }
    carried.current = [];
  };
  const dropRowCopies = () => {
    for (const copy of rowCopies.current ?? []) copy.remove();
    rowCopies.current = null;
  };
  const land = (sourceId: string, target: Landing, withDates = false, carried: readonly string[] = [sourceId]) => {
    // Gone from where it was carried in the frame the row is drawn in its new lane.
    settle();
    dropCopies();
    const to = rowOf(target.id), moved = linesMoved(carried, target.id);
    if (rowOf(sourceId) && to && takes(sourceId, target.id)) latest.current.move(moved.rowIds, { ...target, id: moved.targetId }, to.group, moved.groupOf, withDates, moved.landed);
  };

  const table = useDragReorder(`timeline-row:${viewId}`, (sourceId, targetId, side, carried) => land(sourceId, { id: targetId, side }, false, carried),
    { band: true, next, onMove: copyBars, carry: carriedBy });
  useEffect(() => { if (!table.dragging) dropCopies(); }, [table.dragging]);

  // A bar pressed to move: where it began, whether it carries its rows yet and has gone up or down, the rows it carries and the rows right after them, which take them after themselves.
  const bar = useRef<{ id: string; start: Input; carrying: boolean; lifted: boolean; carried: readonly string[]; next: ReadonlySet<string> } | null>(null);
  const [barLanding, setBarLanding] = useState<Landing | null>(null);
  const barShown = useRef<Landing | null>(null);
  const showBarLanding = (next: Landing | null) => {
    if (next?.id === barShown.current?.id && next?.side === barShown.current?.side) return;
    barShown.current = next;
    setBarLanding(next);
  };
  // Where the pointer lands a bar's row, by the lanes' rows as the canvas lays them out (its grid's tracks).
  const landingAt = (sourceId: string, carried: readonly string[], next: ReadonlySet<string>, input: Input): Landing | null => {
    const lanes = rootRef.current?.querySelector<HTMLElement>("[data-timeline-lanes]");
    if (!lanes) return null;
    const box = lanes.getBoundingClientRect(), tops: number[] = [];
    let top = box.top;
    const heights = getComputedStyle(lanes).gridTemplateRows.split(" ").map((track) => parseFloat(track) || 0);
    for (const height of heights) { tops.push(top); top += height; }
    const placed = latest.current.rows.flatMap(({ id, lane }) => lane < heights.length
      ? [{ id, box: new DOMRect(box.left, tops[lane], box.width, heights[lane]) }] : []);
    const landing = landingBesideRows(placed, box.left, input);
    return landing && takes(sourceId, landing.target) && !carried.includes(landing.target)
      ? { id: landing.target, side: sidePastOwnPlace(landing.side, next.has(landing.target)) } : null;
  };

  const over = table.over ?? barLanding;
  const overLane = over ? rowOf(over.id)?.lane : undefined;
  return {
    /** Whether its rows move now. */
    moves,
    /** The row being carried by its handle, if any. */
    dragging: table.dragging,
    /** Where the row being carried would land, for the canvas's line. */
    line: overLane === undefined ? null : { lane: overLane, side: over!.side } as TimelineDropLine,
    /** A table row's drop target, among its parent's rows. */
    row: (id: string) => table.row(id, (sourceId) => takes(sourceId, id)),
    /** A table row's ⠿; Alt+↑↓ moves its row past the row before or after it, as a table's handle does. */
    handle: (id: string) => ({
      ...table.handle(id, moves, {}),
      onKeyDown(event: ReactKeyboardEvent<HTMLElement>) {
        const shown = latest.current.rows, at = shown.findIndex((row) => row.id === id), parentId = shown[at]?.parentId;
        const siblings = shown.filter((row) => row.parentId === parentId), index = siblings.findIndex((row) => row.id === id);
        table.handle(id, moves, { above: siblings[index - 1]?.id, below: siblings[index + 1]?.id }).onKeyDown(event);
      },
    }),
    /** A bar pressed to move along the days goes with the pointer, up and down too, from its lane's line. */
    barBegin(id: string, start: Input, lane?: number) {
      bar.current = { id: latest.current.rows.find((row) => row.lane === lane)?.id ?? id, start, carrying: false, lifted: false, carried: [], next: new Set() };
    },
    /**
     * The pointer moved: whether the bar carries its rows. It goes faint with
     * the pointer once it goes anywhere, alone or with its chosen rows, as
     * Notion's (seen 2026-11-24 in Chrome: a bar alone moved along the days
     * and three chosen bars carried sideways went at 40% with the pointer);
     * where it lands among the rows only once it has gone up or down, and
     * only where the view's rows move (`moves`).
     */
    barFollow(input: Input) {
      const pressed = bar.current;
      if (!pressed) return false;
      const dy = input.clientY - pressed.start.clientY;
      const lifted = pressed.lifted || Math.abs(dy) >= BAR_LIFT;
      // The rows it carries and the rows right after them, asked once the bar first goes; carried, they wear no chosen look (carriedChoice).
      if (!pressed.carrying) {
        if (!lifted && Math.abs(input.clientX - pressed.start.clientX) <= BAR_LIFT) return false;
        const carried = carriedBy(pressed.id);
        pressed.carrying = true;
        pressed.carried = carried;
        pressed.next = new Set(carried.flatMap((id) => { const after = next(id); return after && !carried.includes(after) ? [after] : []; }));
        carryChosen(carried);
      }
      pressed.lifted = lifted;
      carry(pressed.carried, input, pressed.start);
      if (lifted && moves) showBarLanding(landingAt(pressed.id, pressed.carried, pressed.next, input));
      return true;
    },
    /** The lines the bar pressed carries beside its own, which go along the days as far as it: none until it carries them. */
    barCarriedAlong(): readonly string[] {
      const pressed = bar.current;
      return pressed?.carrying ? pressed.carried.filter((id) => id !== pressed.id) : [];
    },
    /** The bar let go (`drop`) or given up: its rows land where the line showed; `withDates`, its days moved too, in the same step of ⌘Z. */
    barEnd(drop: boolean, withDates = false) {
      const pressed = bar.current, landing = barShown.current;
      bar.current = null;
      showBarLanding(null);
      settle();
      dropRowCopies();
      if (pressed?.carrying) carryChosen([]);
      if (drop && pressed?.lifted && landing) land(pressed.id, landing, withDates, pressed.carried);
    },
  };
}

/**
 * The row right after `source` in its run among a timeline's lanes: the next
 * of its own parent's rows in its group, passing over its own rows under it;
 * none when a row of another group or of a parent above it comes first.
 */
function timelineRowAfter(rows: readonly TimelineRowLane[], source: string): string | undefined {
  const at = rows.findIndex((row) => row.id === source);
  const own = rows[at];
  if (!own) return undefined;
  const parentOf = new Map(rows.map((row) => [row.id, row.parentId]));
  const under = (id: string) => {
    for (let parent = parentOf.get(id); parent; parent = parentOf.get(parent)) if (parent === source) return true;
    return false;
  };
  const after = rows.slice(at + 1).find((row) => !under(row.id));
  return after && after.parentId === own.parentId && after.group === own.group ? after.id : undefined;
}

/**
 * The values of the rows a bar carried along the days beside its own
 * (useTimelineRowReorder's barCarriedAlong), by row: each with a bar and not
 * locked, moved as far as it (`shifted`), written with it in one step.
 */
export function carriedAlongValues<Item extends { row: { id: string; isLocked?: boolean } }>(items: readonly Item[], lines: readonly string[],
  shifted: (item: Item) => Readonly<Record<string, unknown>>): Readonly<Record<string, Readonly<Record<string, unknown>>>> | undefined {
  const ids = new Set(lines.map(lineRowId)), moved = items.filter((item) => ids.has(item.row.id) && !item.row.isLocked);
  return moved.length ? Object.fromEntries(moved.map((item) => [item.row.id, shifted(item)])) : undefined;
}
