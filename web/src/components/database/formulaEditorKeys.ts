import { fieldEnter, fieldEscape, imeComposing, type EnterMeaning } from "@/lib/fieldKeyBoundary";
import { primaryModifier } from "@/lib/keyboardShortcuts";

/**
 * What a key does in the formula editor, as Notion's (measured 2026-11-27 in
 * Chrome at 1440, two tabs some ten minutes apart, the screen looked at after
 * each key and, where the editor stayed, a letter typed; no script before it):
 *  - Enter writes the suggestion chosen in the list, wherever the caret is —
 *    after dateBetween(마감일, now(), "days") the list's first is "+", which
 *    goes in, and the formula then wants a value after it at [33,34] — and
 *    does nothing where the list has none ("결과 없음": in a text value, after
 *    a name nothing is called);
 *  - ⌘Enter (Ctrl+Enter off a Mac) saves the formula and closes the editor,
 *    as Escape and a click outside do;
 *  - Shift+Enter, and on a Mac Ctrl+Enter, breaks the line (each seen once,
 *    in one tab); ⌥Enter does nothing (seen once);
 *  - the Enter that ends a Korean syllable being composed ends it and writes
 *    the suggestion chosen for it: "마감" then Enter is the 마감일 chip (seen
 *    once, macOS's Korean input method and real keys) — the key comes after
 *    the composition's end (fieldKeyBoundary.ts);
 *  - the arrows choose in the list wherever the caret is, and Tab writes the
 *    chosen suggestion only while a name is being typed; elsewhere it writes
 *    a tab (seen 2026-10-08).
 * `suggesting` is whether the list shows a suggestion; `typing`, whether a
 * name is being typed at the caret. "close" saves and closes, "tab" writes a
 * tab, "list" hands the key to the list (FormulaEditorHelper); with none the
 * key is left to the field (formulaKeyEnter).
 */
export function formulaEditorKey(event: KeyboardEvent, { menuOpen, suggesting, typing }: {
  menuOpen: boolean; suggesting: boolean; typing: boolean;
}): "close" | "tab" | "list" | null {
  if (menuOpen || imeComposing(event)) return null;
  if (fieldEscape(event) || primaryModifier(event) && fieldEnter(event)) return "close";
  if (event.shiftKey || event.altKey || event.ctrlKey || event.metaKey) return null;
  if (event.key === "Tab") return suggesting && typing ? "list" : "tab";
  return suggesting && (fieldEnter(event) || event.key === "ArrowDown" || event.key === "ArrowUp") ? "list" : null;
}

/** The formula field's own Enter: a line break with Shift, or a Mac's Ctrl; any other Enter the list didn't take does nothing. */
export const formulaKeyEnter = (event: KeyboardEvent): EnterMeaning =>
  event.shiftKey || event.ctrlKey && !primaryModifier(event) ? "newline" : "confirm";
