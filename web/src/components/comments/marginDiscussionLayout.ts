/**
 * Where Notion puts a page's discussions beside its blocks (seen 2026-09-30,
 * 1280–1700px windows): a 308px column 32px right of the text with 50px to
 * the window's edge, the text moving left to make room, once at least 40px
 * stays left of the text; each card with its name line level with its
 * block's first line, or 10px below the card above it.
 */
const MARGIN_CARD_WIDTH = 308;
const MARGIN_CARD_GAP = 32;
const MARGIN_EDGE = 50;
const MARGIN_CARD_SPACING = 10;
/**
 * How far above its block's top a card starts, closed or open, as Notion's
 * (seen 2026-11-12 at 1440: 212.5 by a text block at 220, both pages' blocks
 * standing alike).
 */
export const MARGIN_ANCHOR_RISE = 7.5;
const MARGIN_MIN_LEFT = 40;

/** The room right of the text the column takes. */
export const MARGIN_COLUMN_ROOM = MARGIN_CARD_GAP + MARGIN_CARD_WIDTH + MARGIN_EDGE;
/** How far the text's column reaches outside its blocks, on each side. */
const MARGIN_COLUMN_INSET = 6;

/** The width the scrollbar of the page's scroll box takes from it. */
/**
 * The stretches of the page that wide content fills across the cards'
 * column, from `origin` down, as Notion's cards make way for them (seen
 * 2026-11-12 at 1440): an overflowing simple table's whole block, a wide
 * board's groups (its scroller less the 15px its bar takes under them) and a
 * wide table's head (its rows may lie under a card); a timeline's whole box
 * (not seen in Notion).
 */
export function marginObstacles(page: HTMLElement, origin: number, columnStart: number): MarginObstacle[] {
  const obstacles: MarginObstacle[] = [];
  for (const wide of page.querySelectorAll<HTMLElement>('[data-page-wide="true"]')) {
    const box = wide.getBoundingClientRect();
    if (box.right <= columnStart) continue;
    const simpleTable = wide.closest<HTMLElement>('[data-block-type="table"]');
    const head = wide.matches("[data-table-scroll]") ? wide.querySelector<HTMLElement>("[data-table-head]") : null;
    const area = simpleTable?.getBoundingClientRect() ?? head?.getBoundingClientRect() ?? box;
    const bottom = wide.matches("[data-board-scroll]") ? box.bottom - 15 : area.bottom;
    obstacles.push({ top: Math.round(area.top - origin), bottom: Math.round(bottom - origin) });
  }
  return obstacles.sort((left, right) => left.top - right.top);
}

export function scrollbarWidth(from: HTMLElement) {
  for (let node: HTMLElement | null = from; node; node = node.parentElement) {
    const style = getComputedStyle(node);
    if (/auto|scroll/.test(style.overflowY)) {
      return Math.max(0, node.offsetWidth - node.clientWidth - parseFloat(style.borderLeftWidth) - parseFloat(style.borderRightWidth));
    }
  }
  return 0;
}

/** Whether a page this wide has room for its discussions beside its text (its blocks' width). */
export function marginDiscussionsFit(pageWidth: number, textWidth: number) {
  return pageWidth - (textWidth + 2 * MARGIN_COLUMN_INSET) - MARGIN_COLUMN_ROOM >= MARGIN_MIN_LEFT;
}

export type MarginCard = { id: string; anchorTop: number; height: number };
/** A stretch of the page a wide table or board fills across the cards' column. */
export type MarginObstacle = { top: number; bottom: number };

/**
 * Each card's top: its anchor's, or just below the card before it. The open
 * card (`openId`) stands at its own anchor, those before it going up as far
 * as it needs, as Notion's (seen 2026-11-12: a kept new comment's card rose
 * above the discussion opened under it). A closed card that would come within
 * the spacing of an obstacle stands that far under it instead, and so do the
 * cards above stacked on to it that already stand lower than their own
 * blocks (Notion's, seen 2026-11-12 at 1440: a card beside an overflowing
 * table under it, the card above it staying at its block; a card held under
 * a wide table's head and the one stacked on to it into a wide board's top
 * both under the board; the open card at its block whatever lies there).
 */
export function stackMarginCards(cards: readonly MarginCard[], openId?: string | null, obstacles: readonly MarginObstacle[] = []) {
  const tops = new Map<string, number>();
  const sorted = [...cards].sort((left, right) => left.anchorTop - right.anchorTop);
  const open = sorted.findIndex((card) => card.id === openId);
  const floors = sorted.map(() => Number.NEGATIVE_INFINITY);
  const stack = () => {
    let free = Number.NEGATIVE_INFINITY;
    return sorted.map((card, index) => {
      const top = index === open ? card.anchorTop : Math.max(card.anchorTop, free, floors[index]);
      free = top + card.height + MARGIN_CARD_SPACING;
      return top;
    });
  };
  const blocking = (top: number, height: number) => obstacles.find((obstacle) =>
    top < obstacle.bottom + MARGIN_CARD_SPACING && top + height + MARGIN_CARD_SPACING > obstacle.top);
  let placed = stack();
  for (let round = 0; round < sorted.length * (obstacles.length + 1); round++) {
    const index = sorted.findIndex((card, at) => at !== open && blocking(placed[at], card.height));
    if (index < 0) break;
    const hit = blocking(placed[index], sorted[index].height)!;
    // The cards stacked on to it that stand lower than their own blocks go with it; one still at its block stays.
    let first = index;
    while (first > 0 && first - 1 !== open && placed[first] > sorted[first].anchorTop
      && placed[first - 1] > sorted[first - 1].anchorTop
      && placed[first] === placed[first - 1] + sorted[first - 1].height + MARGIN_CARD_SPACING) first--;
    floors[first] = Math.max(floors[first], hit.bottom + MARGIN_CARD_SPACING);
    placed = stack();
  }
  sorted.forEach((card, index) => tops.set(card.id, placed[index]));
  let limit = open > 0 ? sorted[open].anchorTop - MARGIN_CARD_SPACING : Number.NEGATIVE_INFINITY;
  for (const card of sorted.slice(0, Math.max(open, 0)).reverse()) {
    const top = Math.min(tops.get(card.id) ?? card.anchorTop, limit - card.height);
    tops.set(card.id, top);
    limit = top - MARGIN_CARD_SPACING;
  }
  return tops;
}
