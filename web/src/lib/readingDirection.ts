// Which way the screen reads where an element sits. The app's language sets
// <html dir> (i18n/index.ts: Arabic, Hebrew, Persian, Urdu ... read from the
// right), and the whole frame follows it, a page's too: its handles, menus,
// head buttons, icon, database tables and panels, as Notion's in Hebrew and
// Arabic (seen 2026-11-09). Text reads by its own words and turns nothing
// around it: a block's text, a title, a comment (dir="auto",
// lib/textDirection.ts), a block set to a direction and code. Code that
// places, drags or steps sideways asks here rather than assuming the left.

import { wordsNameDirection } from "./textDirection";

export type ReadingDirection = "ltr" | "rtl";

// Text whose direction comes from its words or its block's own setting.
const OWN_WORDS = '[dir="auto"], [data-native-text-view]';

/** The direction of the frame an element sits in, past any text around it
 * that reads by its own words: a menu opened from an English line in a
 * Hebrew app opens as the app reads, as Notion's "/" does (seen 2026-11-09). */
export function readingDirection(element?: Element | null): ReadingDirection {
  let at = element ?? (typeof document === "undefined" ? null : document.documentElement);
  for (let text = at?.closest(OWN_WORDS); text; text = at?.closest(OWN_WORDS)) at = text.parentElement;
  if (!at || typeof getComputedStyle === "undefined") return "ltr";
  return getComputedStyle(at).direction === "rtl" ? "rtl" : "ltr";
}

export function readsRightToLeft(element?: Element | null): boolean {
  return readingDirection(element) === "rtl";
}

/** A step toward the reading direction's end as a step along the screen's x:
 * 1 left to right, -1 right to left (CSS's `--inline-sign`). */
export function inlineSign(element?: Element | null): 1 | -1 {
  return readsRightToLeft(element) ? -1 : 1;
}

/** Where a box starts in the reading direction, `inward` past that along it:
 * its left plus `inward`, or its right less `inward` where the screen reads
 * from the right. A popover placed at a point (placeAtPoint) has its top
 * start there, so one measured from its opener's start mirrors with it. */
export function inlineStartX(box: { left: number; right: number }, inward = 0, element?: Element | null): number {
  return readsRightToLeft(element) ? box.right - inward : box.left + inward;
}

/** Where a box ends in the reading direction, `outward` past that: its right
 * plus `outward`, or its left less `outward` read from the right. */
export function inlineEndX(box: { left: number; right: number }, outward = 0, element?: Element | null): number {
  return readsRightToLeft(element) ? box.left - outward : box.right + outward;
}

/** The left/right arrow key that moves toward the reading direction's end
 * (into a submenu, to the next item along a row). */
export function inlineEndKey(element?: Element | null): "ArrowRight" | "ArrowLeft" {
  return readsRightToLeft(element) ? "ArrowLeft" : "ArrowRight";
}

export function inlineStartKey(element?: Element | null): "ArrowRight" | "ArrowLeft" {
  return readsRightToLeft(element) ? "ArrowRight" : "ArrowLeft";
}

/**
 * Which way ← or → steps at the edges of a line of text — a page's title,
 * a block's line: 1 toward its end, -1 toward its start. A line steps the
 * way it reads on screen (`text`'s own direction: its words', or its block's
 * set one), so in an Arabic or Hebrew line → goes back over its start — to
 * the line above's end, or from a page's first line to its title — and ←
 * on, into the line at its start and out of a title's end to the first
 * line; a line with no letter steps left to right however it is shown
 * (editor/nativeTiptapLineWay.ts), as Notion's (seen 2026-11-19,
 * Korean and Arabic, each twice in two tabs: Home then ← one letter into an
 * Arabic first line, → to the title's end; → at a Hebrew line's start to the
 * Arabic line's end above; an Arabic title's end ← to the first line, → a
 * letter back; an empty line shown from the right ← to the title, → to the
 * next line's start).
 */
export function lineArrowStep(key: "ArrowLeft" | "ArrowRight", text: Element | null, words: string): 1 | -1 {
  const fromRight = text !== null && wordsNameDirection(words) && getComputedStyle(text).direction === "rtl";
  return (key === "ArrowRight") !== fromRight ? 1 : -1;
}

/** How far a box is scrolled from its reading direction's start: a box read
 * from the right counts its scrollLeft from 0 down. */
export function inlineScrolled(element: Element): number {
  return Math.abs(element.scrollLeft);
}

/** Scrolls a box to its reading direction's end. */
export function scrollToInlineEnd(element: Element) {
  element.scrollLeft = inlineSign(element) * (element.scrollWidth - element.clientWidth);
}

/** An arrow key as it reads along a row: "ArrowLeft" and "ArrowRight" trade
 * places where the screen reads from the right, so the one that steps toward
 * a row's end (the next column, the next day) comes back as "ArrowRight". */
export function arrowAlongRow<K extends string>(key: K, element?: Element | null): K {
  if (!readsRightToLeft(element) || (key !== "ArrowLeft" && key !== "ArrowRight")) return key;
  return (key === "ArrowLeft" ? "ArrowRight" : "ArrowLeft") as K;
}
