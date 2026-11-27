import { ListKeyboardDelegate } from "react-aria";
import type { Collection, Key, Node as CollectionNode } from "react-stately";
import type { RefObject } from "react";
import { shortcutModifierHeld } from "./keyboardShortcuts";

/**
 * Notion's menus move their highlight only with ↑↓ (seen 2026-10-05 in the
 * test page: the block type submenu, the sidebar page menu, the page "…"
 * menu with its search field, the slash menu and the mention menu). Home and
 * End move a text caret where there is one, PageUp and PageDown leave the
 * highlight where it is, and neither key closes the menu.
 */
const MENU_MOVE_KEYS = new Set(["ArrowDown", "ArrowUp"]);
const PLAIN_MENU_INERT_KEYS = new Set(["Home", "End", "PageUp", "PageDown", "Tab"]);

/**
 * Keys that do nothing in a Notion menu and leave it open: in a menu without
 * a search field, Home, End, PageUp, PageDown and Tab; in an action menu with
 * one, Tab (focus stays in the field, whose own Home and End move its caret).
 * Notion's Tab goes round a menu's own stops, and its field is the only one
 * while the foot names no person: a page's "⋯" whose last edit was a
 * connection's kept the field through Tab ×2, a letter after going into it;
 * once a person edited it, the foot's name — a link to their profile — took
 * the next Tab and the field the one after (seen 2026-11-26 in Chrome 1440,
 * one tab, no script before the keys; the person's foot in two tabs that day
 * on a page's "⋯" and a block's "⋮⋮"). This foot's person is such a link
 * too (EditedByLine), and with words typed the field's "입력 값 지우기" is a
 * stop (2026-11-26, two tabs): goRoundMenuStops (menuTabStops.ts) moves a
 * Tab round them first, and the field keeps it here while it is the only one.
 */
export function isInertMenuKey(event: { key: string; altKey: boolean; ctrlKey: boolean; metaKey: boolean }, searched = false) {
  if (event.altKey || event.ctrlKey || event.metaKey) return false;
  return searched ? event.key === "Tab" : PLAIN_MENU_INERT_KEYS.has(event.key);
}

/** A printable character typed without a modifier, Space aside (forwardMenuItemKey
 * takes it); in a menu without a search field it chooses no item, as in Notion's. */
export function isTypedLetter(event: { key: string; altKey: boolean; ctrlKey: boolean; metaKey: boolean; getModifierState?: (key: "AltGraph") => boolean }) {
  return event.key.length === 1 && event.key !== " " && !shortcutModifierHeld(event);
}

export function isTextField(element: EventTarget | Element | null | undefined): boolean {
  if (!element || typeof HTMLInputElement === "undefined") return false;
  return element instanceof HTMLTextAreaElement
    || (element instanceof HTMLInputElement && !["checkbox", "radio", "button", "submit", "range", "color"].includes(element.type));
}
export function isMenuMoveKey(key: string): boolean {
  return MENU_MOVE_KEYS.has(key);
}

/** The list movement every menu and picker shares: Aria's delegate, which
 * passes over items that can't be chosen, with no page movement (PageUp and
 * PageDown go on to the browser, which scrolls the list under the focus). */
export function nativeMenuKeyboardDelegate<T>(collection: Collection<CollectionNode<T>>, disabledKeys: Set<Key>,
  ref: RefObject<HTMLElement | null>) {
  const delegate = new ListKeyboardDelegate(collection, disabledKeys, ref,
    new Intl.Collator(undefined, { usage: "search", sensitivity: "base" }));
  delegate.getKeyPageAbove = () => null;
  delegate.getKeyPageBelow = () => null;
  return delegate;
}
