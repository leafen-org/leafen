import { useLayoutEffect, useState, type CSSProperties, type RefObject } from "react";
import { shownBlockElement } from "@/lib/shownBlockCopy";
import { commentBlockElement, commentWordsHolder, inCommentWordsHolder } from "./commentBlockElement";
import type { CommentTextPlace } from "./commentQuotePlaces";
import { pendingCommentBox } from "./pendingCommentMark";

// A block's card: its width — a new comment's field alone is narrower — as
// Notion's 480 and 470 (seen 2026-10-08, 2026-11-06 and 2026-11-09).
export const ANCHORED_WIDTH = 480;
export const ANCHORED_COMPOSER_WIDTH = 470;
// How far it keeps from the window's sides, and how much room it leaves
// under itself to stand below what it was opened from, as Notion's (seen
// 2026-11-09 at 1054×715: 12px off the right side; under the words while
// 8px stays below the card, over them with 7).
const SIDE_EDGE = 12;
const END_EDGE = 8;
// A new comment's field stands 4px under one block's chosen words and 7px
// under the last line of words chosen across blocks, as Notion's 18px-high
// words (seen 2026-11-09); Leafen draws the same words 20px high, 1px lower.
const UNDER_WORDS = 3;
const UNDER_ACROSS = 6;
// Where the comment count Notion puts by a block's first line ends, past the
// block's right edge (seen 2026-11-09: "💬 1" to 977 by a block to 943).
const COUNT_MARK_END = 34;

/**
 * The pressed threads' words in the block they were pressed in — its text or
 * its caption — as drawn, every line they run on; not those in the blocks a
 * thread runs on into, nor in the blocks inside it.
 */
function pressedWordsBox(blockId: string, threadIds: readonly string[]): DOMRect | null {
  const block = commentWordsHolder(blockId);
  if (!block || !threadIds.length) return null;
  const marks = block.querySelectorAll(threadIds.map((id) => `[data-rt-editable] [data-comment-id~="${CSS.escape(id)}"]`).join(","));
  let left = Infinity, top = Infinity, right = -Infinity, bottom = -Infinity;
  for (const mark of marks) {
    if (!inCommentWordsHolder(mark, block)) continue;
    for (const box of mark.getClientRects()) {
      left = Math.min(left, box.left); top = Math.min(top, box.top);
      right = Math.max(right, box.right); bottom = Math.max(bottom, box.bottom);
    }
  }
  return right >= left ? new DOMRect(left, top, right - left, bottom - top) : null;
}

/**
 * What a new comment's field stands under, as Notion's (seen 2026-11-09, 1054
 * wide): one block's chosen words, centred on them; words chosen across
 * blocks, centred on the box Notion takes round the whole choice — from its
 * leftmost line, or the left of a block chosen whole between the first and
 * the last, to the end of the count Notion puts by the first block at once —
 * so the field stands right of the words' middle, the same however they
 * were chosen.
 */
function newCommentPlace(places: readonly CommentTextPlace[]): { centre: number; under: number } | null {
  const words = pendingCommentBox(places);
  if (!words) return null;
  if (places.length < 2) return { centre: words.left + words.width / 2, under: words.bottom + UNDER_WORDS };
  let left = words.left;
  for (const place of places.slice(1, -1)) {
    const whole = shownBlockElement(place.blockId)?.getBoundingClientRect();
    if (whole) left = Math.min(left, whole.left);
  }
  const first = shownBlockElement(places[0].blockId)?.getBoundingClientRect();
  const right = first ? first.right + COUNT_MARK_END : words.right;
  return { centre: (left + right) / 2, under: words.bottom + UNDER_ACROSS };
}

/**
 * Where a block's card stands, as Notion's (seen 2026-11-09, 1054×715):
 * opened from text a discussion was left on, centred on that discussion's
 * words in the block pressed — all their lines, its caption's words in a
 * caption, the pressed block's words of one running across blocks — 4px
 * off their underline; opened from the block's side mark, its reactions
 * or an address, centred on the block, just under it; for a new
 * comment, under the selected text (newCommentPlace). It is kept inside the window,
 * centred even where that puts it over the sidebar (Notion's x 152 under a
 * line's first word); it stands below while the card as it is leaves 8px
 * under it, else above while it fits there, else — a picture taller than
 * the window — over the block, 8px off the window's bottom (Notion's at
 * 560–707 over a 608px picture). It follows the words or the block
 * wherever they move; a row's card follows the row or card it was opened
 * from.
 */
export function useAnchoredPlace(blockId: string | null | undefined, quoting: boolean, element: HTMLElement | null | undefined,
  quote: readonly CommentTextPlace[] | null, pressed: readonly string[] | undefined,
  card: RefObject<HTMLElement | null>, cardWidth: number): CSSProperties | null {
  const [place, setPlace] = useState<CSSProperties | null>(null);
  const pressedKey = blockId && !element && pressed?.length ? pressed.join(" ") : "";
  useLayoutEffect(() => {
    if (!blockId && !element) return;
    const row = () => {
      if (element) return element.isConnected ? element : null;
      const block = shownBlockElement(blockId!);
      return (block?.firstElementChild as HTMLElement | null) ?? block ?? commentBlockElement(blockId!);
    };
    const threadIds = pressedKey ? pressedKey.split(" ") : [];
    // The selected text, as it was when the card opened.
    let quoted: { x: number; y: number } | null = null;
    const first = row();
    const text = quoting && blockId && quote ? newCommentPlace(quote) : null;
    if (first && text) {
      const own = first.getBoundingClientRect();
      quoted = { x: text.centre - own.left, y: text.under - own.top };
    }
    // The block may move without the window telling — the sidebar folding,
    // text above it growing — so the card follows it frame by frame while open.
    let frame = 0;
    let last = "";
    // A block's card overlaps the block 2px, as Notion's: 3px off its line,
    // which lies 5px inside it; a row's keeps 2px off the row.
    const lineGap = element ? 2 : 3;
    function measure() {
      frame = window.requestAnimationFrame(measure);
      const line = row();
      if (!line) return;
      const rect = line.getBoundingClientRect();
      const words = threadIds.length ? pressedWordsBox(blockId!, threadIds) : null;
      // The card's whole height, however much of it the window lets show.
      const height = card.current?.scrollHeight ?? 0;
      const key = [rect.left, rect.top, rect.width, rect.height, words?.left, words?.top, words?.right, words?.bottom,
        height, window.innerWidth, window.innerHeight].join();
      if (key === last) return;
      last = key;
      // Kept off a panel docked at the page's right.
      const area = line.closest("main")?.getBoundingClientRect();
      const start = SIDE_EDGE;
      const end = Math.min(window.innerWidth, area?.right ?? window.innerWidth) - SIDE_EDGE;
      const width = Math.min(cardWidth, Math.max(240, end - start));
      const anchor = words ? { centre: words.left + words.width / 2, under: words.bottom + 4, over: words.top - 4 }
        : quoted ? { centre: rect.left + quoted.x, under: rect.top + quoted.y, over: rect.top - 2 }
        : { centre: rect.left + rect.width / 2, under: rect.bottom + lineGap, over: rect.top - lineGap };
      const left = Math.max(start, Math.min(anchor.centre - width / 2, end - width));
      const below = window.innerHeight - END_EDGE - anchor.under;
      const above = anchor.over - END_EDGE;
      const room = window.innerHeight - 2 * END_EDGE;
      setPlace(height <= below ? { left, width, top: anchor.under, maxHeight: below }
        : height <= above ? { left, width, bottom: window.innerHeight - anchor.over, maxHeight: above }
        : { left, width, top: Math.max(END_EDGE, Math.min(anchor.under, END_EDGE + room - height)), maxHeight: room });
    }
    measure();
    return () => window.cancelAnimationFrame(frame);
  }, [blockId, card, cardWidth, element, pressedKey, quoting]);
  return place;
}
