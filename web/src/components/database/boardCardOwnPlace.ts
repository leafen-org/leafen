import { carriedCopy } from "@/lib/dragReorderLine";
import { sidePastOwnPlace } from "@/lib/dragReorderOwnPlace";

/** The card drawn right after `card` among its column's cards (in its own lane), a card being made passed over. */
export function cardDrawnAfter(card: HTMLElement): HTMLElement | null {
  const cards = [...card.parentElement?.parentElement?.querySelectorAll<HTMLElement>(":scope > * > [data-row-card]") ?? []];
  const at = cards.indexOf(card);
  return at < 0 ? null : cards[at + 1] ?? null;
}

/**
 * The cards a drag of `card` carries, as Notion's board does (seen
 * 2026-11-24 in Chrome at 1685, dpr 1: two chosen cards, one dragged into
 * another column, stood there together where it was dropped, in their
 * order, chosen, a faint copy of each going with the pointer): the chosen
 * cards of its board as drawn, each once, when it is chosen and its board
 * moves several in one step (`data-cards-carry`); else it alone.
 */
export function boardCardsCarried(card: HTMLElement): HTMLElement[] {
  const chosen = (element: HTMLElement) => Boolean(element.querySelector("[data-card-chosen]"));
  const board = card.closest<HTMLElement>("[data-cards-carry]");
  if (!board || !chosen(card)) return [card];
  const seen = new Set([card.dataset.rowId]);
  return [...board.querySelectorAll<HTMLElement>("[data-row-card][data-row-id]")].filter((element) => element === card
    || (chosen(element) && !seen.has(element.dataset.rowId) && Boolean(seen.add(element.dataset.rowId))));
}

/** See-through copies of the cards a drag of `card` carries, which go with the pointer (pointerDrag). */
export function carriedCardCopies(card: HTMLElement) {
  return boardCardsCarried(card).flatMap((one) => carriedCopy(one) ?? []);
}

/** The drag data of the cards a drag carries: their rows, and the cards drawn right after them, which take them after themselves (boardCardSide). */
export function carriedCardsData(cards: readonly HTMLElement[]) {
  const next = cards.map(cardDrawnAfter).filter((after): after is HTMLElement => Boolean(after) && !cards.includes(after!));
  return { carried: cards.map((card) => card.dataset.rowId ?? ""), next };
}

/** Whether a dragged card's drag (`dragged`, its drag data) carries the card of `rowId`. */
export function cardCarried(dragged: Record<string | symbol, unknown>, rowId: string | undefined) {
  return rowId === dragged.id || (Array.isArray(dragged.carried) && dragged.carried.includes(rowId));
}

/**
 * The side of `card` a dragged card lands on: by the nearer half (`after`),
 * except that a card drawn right after one the drag carries in its column
 * takes them after itself whichever half, as Notion's board column
 * (sidePastOwnPlace). `dragged` is the dragged card's drag data, its `next`
 * those cards' elements.
 */
export function boardCardSide(dragged: Record<string | symbol, unknown>, card: HTMLElement, after: boolean): "before" | "after" {
  return sidePastOwnPlace(after ? "after" : "before", Array.isArray(dragged.next) ? dragged.next.includes(card) : dragged.next === card);
}

/** The cards around a place (`index`) among a column's cards, those `moving` passed over: the block of them lands between. */
export function boardBlockNeighbours(rows: readonly { id: string }[], moving: readonly string[], index: number) {
  const carried = new Set(moving);
  return { beforeId: rows.slice(0, index).filter(({ id }) => !carried.has(id)).at(-1)?.id ?? null,
    afterId: rows.slice(index).find(({ id }) => !carried.has(id))?.id ?? null };
}
