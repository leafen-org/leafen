import { useEffect, useRef, useState, type RefObject } from "react";
import { dropTargetByPointer } from "@/lib/pointerDrag";
import { boardCardSide, cardCarried } from "./boardCardOwnPlace";

/** What a drag over a column shows: over one of its cards, a card's side it lands on away from them, its end, or a column dragged. */
export type BoardColumnOver = "card" | "end" | "group" | { card: string; side: "before" | "after" } | null;

/**
 * Where a card dragged over a board column, not over one of its cards,
 * lands, as Notion's board (seen 2026-11-24 in Chrome at 1685, dpr 1, two
 * tabs at different times, the line read while dragging; cards 40px tall,
 * 8px apart): in the room between two cards, beside the nearer card other
 * than one dragged, on the side of it the pointer is — the card right
 * after one dragged taking them after itself (boardCardSide) — so between
 * two other cards it lands between them; over the column's head or above
 * its first card, first; under its last card, over "+ 새 페이지" and below,
 * last ("end"). Leafen put every card dropped between two others last.
 */
export function boardColumnLanding(column: HTMLElement, y: number, dragged: Record<string | symbol, unknown>): BoardColumnOver {
  const boxes = [...column.querySelectorAll<HTMLElement>("[data-row-card][data-row-id]")].map((card) => ({ card, box: card.getBoundingClientRect() }));
  const last = boxes.at(-1);
  if (!last || y >= last.box.bottom) return "end";
  if (y < boxes[0]!.box.top) return { card: boxes[0]!.card.dataset.rowId!, side: "before" };
  let near: (typeof boxes)[number] | undefined, distance = Infinity;
  for (const candidate of boxes) {
    if (cardCarried(dragged, candidate.card.dataset.rowId)) continue;
    const away = Math.max(candidate.box.top - y, y - candidate.box.bottom, 0);
    if (away < distance) { near = candidate; distance = away; }
  }
  if (!near) return "end";
  return { card: near.card.dataset.rowId!, side: boardCardSide(dragged, near.card, y > near.box.top + near.box.height / 2) };
}

const sameOver = (left: BoardColumnOver, right: BoardColumnOver) => left === right
  || (typeof left === "object" && typeof right === "object" && left?.card === right?.card && left?.side === right?.side);

/**
 * A board column as a drop target: a card dropped on it, not on one of its
 * cards, lands where boardColumnLanding says — where the column takes it
 * (canTakeCard) — at its index among `rowIds`, the cards it shows, or last
 * (`count`, all its cards); a dragged column lands before this one.
 */
export function useBoardColumnDrop(columnRef: RefObject<HTMLElement | null>, columnKey: string, dragScope: string, now: {
  count: number; rowIds: readonly string[]; laneKey?: string;
  onDropAt(groupKey: string, index: number, laneKey?: string): void;
  onDropGroup(groupKey: string): void;
  canTakeCard?(groupKey: string, laneKey?: string): boolean;
}): BoardColumnOver {
  const [over, setOver] = useState<BoardColumnOver>(null);
  const latest = useRef(now);
  latest.current = now;
  useEffect(() => {
    const element = columnRef.current;
    if (!element) return;
    const cardKind = `board-card:${dragScope}`, groupKind = `board-group:${dragScope}`;
    const overOf = (data: Record<string | symbol, unknown>, onColumn: boolean, y: number): BoardColumnOver =>
      data.kind === groupKind ? "group" : onColumn ? boardColumnLanding(element, y, data) : "card";
    return dropTargetByPointer({
      element,
      canDrop: ({ source }) => (source.data.kind === cardKind && latest.current.canTakeCard?.(columnKey, latest.current.laneKey) !== false)
        || (source.data.kind === groupKind && source.data.id !== columnKey),
      onDragEnter: ({ source, location }) => setOver(overOf(source.data, location.current.dropTargets[0]?.element === element, location.current.input.clientY)),
      onDrag: ({ source, location }) => {
        const next = overOf(source.data, location.current.dropTargets[0]?.element === element, location.current.input.clientY);
        setOver((current) => sameOver(current, next) ? current : next);
      },
      onDragLeave: () => setOver(null),
      onDrop: ({ source, location }) => {
        setOver(null);
        if (location.current.dropTargets[0]?.element !== element) return;
        if (source.data.kind === groupKind) { latest.current.onDropGroup(columnKey); return; }
        const landing = boardColumnLanding(element, location.current.input.clientY, source.data);
        const at = typeof landing === "object" && landing ? latest.current.rowIds.indexOf(landing.card) : -1;
        latest.current.onDropAt(columnKey, at < 0 || typeof landing !== "object" || !landing ? latest.current.count : at + (landing.side === "after" ? 1 : 0),
          latest.current.laneKey);
      },
    });
  }, [columnKey, columnRef, dragScope]);
  return over;
}
