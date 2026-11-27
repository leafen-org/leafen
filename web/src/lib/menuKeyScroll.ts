import { type Key, type RefObject, useLayoutEffect, useRef } from "react";
import { keysLeadNow } from "./menuInputLead";
import { placeInScrollBox } from "./scrollBoxPlace";
import { scrollIntoViewNearest } from "./scrollIntoViewNearest";

// How much of its list a lit row keeps showing past it as ↑↓ walk a menu.
const KEY_WALK_ROOM = 64;

/**
 * Scrolls the list a menu's ↑↓ walk so the row they lit shows, as Notion's
 * menus scroll theirs: the list stays where it is while any part of the row
 * stands in its middle — the list less 64px at its top and its foot — and a
 * row wholly within those 64px at an end, or past it, is brought to 64px
 * from that end, so about two rows show past it. Seen 2026-11-17 at 1440 in
 * Chrome, the same in "/" (247px list, 32px rows), "@" (289px, 28px and
 * 45px rows), "[[" (289px), a block's "⋮⋮ › 전환" (506px) and "옮기기"
 * (374px): "@"'s 28px row stayed with its top 36.4px under the list's top
 * and was brought down at 35.4px; 45px rows 42.2px under it stayed; a row
 * 3.2px over the foot was brought up to 64px over it. A list too short for
 * that room keeps what room the row leaves it. A list that scrolls outside
 * the menu (`menu`) only keeps the row in view. Measured in the list's own
 * pixels (placeInScrollBox), so a menu still growing into place as it opens
 * scrolls as it will stand grown.
 */
export function scrollKeyWalkedRow(row: HTMLElement | null | undefined, menu: HTMLElement | null) {
  if (!row) return;
  let box = row.parentElement;
  while (box && !(box.scrollHeight > box.clientHeight && /(auto|scroll)/u.test(getComputedStyle(box).overflowY))) {
    box = box.parentElement;
  }
  if (!box) return;
  if (!menu || !(box === menu || menu.contains(box))) { scrollIntoViewNearest(row); return; }
  const target = placeInScrollBox(row, box);
  const top = box.clientTop, bottom = top + box.clientHeight;
  const room = Math.max(0, Math.min(KEY_WALK_ROOM, (box.clientHeight - target.height) / 2));
  if (target.bottom < top + room) box.scrollTop -= top + room - target.top;
  else if (target.top > bottom - room) box.scrollTop += target.bottom - (bottom - room);
}

/**
 * The scrolling of a menu's rows as its keys walk them (`scrollKeyWalkedRow`),
 * for the menus built on Aria's collections: given `ariaScrollsNothing`,
 * Aria leaves the rows where they are, and the row the keys light
 * (`focusedKey`, found by its `data-key` in `menuRef`) is scrolled here once
 * it is drawn. A row the pointer lit, or one lit as the menu opened under it,
 * stays where it is.
 */
export function useKeyWalkScroll(menuRef: RefObject<HTMLElement | null>, focusedKey: Key | null | undefined) {
  const shown = useRef<Key | null | undefined>(focusedKey);
  useLayoutEffect(() => {
    if (focusedKey === shown.current) return;
    const menu = menuRef.current;
    const row = focusedKey == null ? null : menu?.querySelector<HTMLElement>(`[data-key="${CSS.escape(String(focusedKey))}"]`);
    // A row not drawn yet is scrolled to once it is.
    if (focusedKey != null && !row) return;
    shown.current = focusedKey;
    if (row && keysLeadNow()) scrollKeyWalkedRow(row, menu);
  });
}

/**
 * Spread into an Aria collection's options (`useListBox`, `useMenu`, a `Menu`)
 * so it scrolls nothing itself: its own scrolling (`useSelectableCollection`'s
 * `scrollRef`, the browser's nearest edge) is left to `useKeyWalkScroll`.
 */
export const ariaScrollsNothing: Record<never, never> = { scrollRef: { current: null } } as Record<never, never>;
