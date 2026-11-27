import type { ComponentType } from "react";
import { DotsHorizontal, type LeafenIconProps, TurnIntoIcon } from "@/icons/leafen";
import type { PhoneTool } from "./PhoneSelectionTools";
import styles from "./NativePhoneToolbar.module.css";

// A phone's editing strip, as the Notion Android app's (measured 2026-11-21 on
// the A90 at 411, the app's own tool names read from its screen, twice for
// each kind of line; phones follow the app — the phone basis decision): every
// tool its own cell, an icon alone, no list's chevron and no cell of several.
// The app's AI and dictation come first where it has them; Leafen has neither:
// no built-in AI, dictation left to the keyboard's own.

/** A text line's tools, scrolled along: +, Aa, picture, turn into, undo, comment, "@", delete, then the nesting, ⋯. */
const LINE = ["add", "textStyle", "image", "turn", "undo", "comment", "mention", "delete", "indent", "outdent", "moveUp", "moveDown", "more"];
/** A list item's: its nesting first, out before in, and its comment before undo (the app's, both times). */
const LIST = ["outdent", "indent", "moveUp", "moveDown", "add", "textStyle", "image", "turn", "comment", "undo", "mention", "delete", "more"];
/** A page title's: the picture and "@" alone. */
const TITLE = ["image", "mention"];

/**
 * The lines a list item's order is for: the app's bulleted lines were seen;
 * numbered, to-do and toggle lines are its other lists (inferred).
 */
const LIST_LINES = new Set(["bulleted_list_item", "numbered_list_item", "to_do", "toggle"]);

/** Marks drawn for a tool the wider screens write as a word. */
const MARKS: Record<string, ComponentType<LeafenIconProps>> = { turn: TurnIntoIcon, more: DotsHorizontal };

/**
 * A phone's caret tools from the wider screens' (`tools`, by key), in the
 * app's order for the field the caret is in, and its "Aa" (`textStyle`),
 * which turns the strip to the line's marks.
 */
export function phoneStripTools(tools: readonly PhoneTool[], at: { title: boolean; blockType?: string; textStyle(): void }) {
  const byKey = new Map(tools.map(tool => [tool.key, tool]));
  byKey.set("textStyle", { key: "textStyle", label: <span className={styles.textStyle} aria-hidden="true">Aa</span>, run: at.textStyle });
  const order = at.title ? TITLE : at.blockType && LIST_LINES.has(at.blockType) ? LIST : LINE;
  return order.flatMap(key => {
    const tool = byKey.get(key);
    if (!tool) return [];
    const icon = MARKS[key] ?? tool.icon;
    return [{ ...tool, icon, label: icon ? undefined : tool.label, arrow: false, group: undefined }];
  });
}

/**
 * The app's "+" list leads "기본 블록" with text, the four headings, the
 * bulleted, numbered, to-do and toggle lists, page, callout and quote (seen
 * 2026-11-21 on the A90, its cards read off its screen twice); the rest of a
 * group keeps Leafen's order after them.
 */
const ADD_FIRST: readonly string[] = ["paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "bulleted_list_item",
  "numbered_list_item", "to_do", "toggle", "child_page", "callout", "quote"];

/** A group's blocks in the order the app's "+" list draws them. */
export function stripAddOrder<T extends { type?: string }>(items: readonly T[]): T[] {
  const rank = (item: T) => { const at = ADD_FIRST.indexOf(item.type ?? ""); return at < 0 ? ADD_FIRST.length : at; };
  return items.map((item, index) => ({ item, index })).sort((a, b) => rank(a.item) - rank(b.item) || a.index - b.index).map(({ item }) => item);
}
