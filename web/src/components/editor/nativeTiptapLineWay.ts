import { useCallback, useLayoutEffect, useSyncExternalStore } from "react";
import type { Editor } from "@tiptap/core";
import type { Node as PMNode, ResolvedPos } from "@tiptap/pm/model";
import { Selection } from "@tiptap/pm/state";
import { appReadsFromRight, blockTextDirection, TEXT_DIRECTION_BLOCK_TYPES, wordsDirection, wordsNameDirection } from "@/lib/textDirection";

/*
 * The way a text line reads that names none itself — no direction set, no
 * letter in its words (empty, or only digits and marks). Where the app reads
 * from the right it reads as the nearest line above it that names one, and
 * from the right at the page's top, shown so at once — its caret, hint, "/"
 * and its menu on that side — as Notion's in Arabic (seen 2026-11-19: Enter
 * after an Arabic or a Hebrew line, a list item's first child under an Arabic
 * one, a line emptied of its words and "123", each after an English line or
 * under an Arabic one, took the line above's way, set as `dir`; the line
 * above, not the one Enter split; a line after a divider the line's above
 * it; the page's first line the app's, the title above it not counted; a
 * letter typed gave it its words' way; seen too by the translation strike
 * s248). Where the app reads from the left the line finds its way as
 * dir="auto" does, left to right, as Notion's in Korean with "always show
 * text direction controls" on or off (seen 2026-11-19). The words above are
 * the editor's own, a line's text living in it before the page's graph. An
 * empty line still steps left to right under ←/→ (lineArrowStep).
 */

type Way = "ltr" | "rtl";

// What each place of a document found above it, kept with that document: a
// run of empty lines asks once along it, not once from each of them.
const foundAbove = new WeakMap<PMNode, Map<number, Way | undefined>>();

// Where to go on from when a line found lies in a closed toggle, which shows
// none of its children: before its children, on to its own line.
function closedFrom($line: ResolvedPos): number | null {
  for (let depth = 1; depth < $line.depth; depth++) {
    if ($line.node(depth).type.name !== "nativeChildren") continue;
    if ($line.node(depth - 1).attrs.props?.collapsed === true) return $line.before(depth);
  }
  return null;
}

/** The way the nearest line shown above a place reads that names one, past lines with no letter, code, captions, cells and blocks with no text; none at the page's top. */
function lineAbove(doc: PMNode, before: number): Way | undefined {
  let known = foundAbove.get(doc);
  if (!known) foundAbove.set(doc, known = new Map());
  const passed: number[] = [];
  let found: Way | undefined;
  for (let at = before, steps = doc.content.size; steps > 0; steps--) {
    if (known.has(at)) { found = known.get(at); break; }
    passed.push(at);
    const $line = Selection.findFrom(doc.resolve(at), -1, true)?.$from;
    if (!$line) break;
    const closed = closedFrom($line);
    at = closed ?? $line.before();
    if (closed !== null) continue;
    const owner = $line.node($line.depth - 1);
    if ($line.parent.type.name !== "nativeText" || owner.type.name !== "nativeBlock" || !TEXT_DIRECTION_BLOCK_TYPES.has(owner.attrs.kind)) continue;
    const set = blockTextDirection(owner.attrs.props);
    const words = $line.parent.textContent;
    found = set !== "auto" ? set : wordsNameDirection(words) ? wordsDirection(words) : undefined;
    if (found) break;
  }
  for (const at of passed) known.set(at, found);
  return found;
}

const NOTHING = () => () => {};

/**
 * The way a line with no letter of its own is shown — the line whose text
 * starts at `before` in the editor's document — where the app reads from
 * the right; undefined while its words name a way or the app reads from the
 * left. It follows the lines above as they change.
 */
export function useWordlessLineDirection(editor: Editor, before: () => number | undefined, wordless: boolean): Way | undefined {
  const active = wordless && appReadsFromRight();
  const subscribe = useCallback((listener: () => void) => {
    editor.on("update", listener);
    return () => { editor.off("update", listener); };
  }, [editor]);
  return useSyncExternalStore(active ? subscribe : NOTHING, () => {
    const at = active ? before() : undefined;
    if (at === undefined || at < 0 || at > editor.state.doc.content.size) return undefined;
    return lineAbove(editor.state.doc, at) ?? "rtl";
  });
}

// The way each line of an editor is shown, told by the line's own view —
// which draws again with each letter typed in it — to the view of the
// line's children, which indent from that side (blockChildrenDirection) and
// would otherwise draw again only when their own blocks change.
type Told = { way: Way | undefined; listeners: Set<() => void> };
const toldWays = new WeakMap<Editor, Map<string, Told>>();

function toldFor(editor: Editor, line: string) {
  let lines = toldWays.get(editor);
  if (!lines) toldWays.set(editor, lines = new Map());
  let told = lines.get(line);
  if (!told) lines.set(line, told = { way: undefined, listeners: new Set() });
  return told;
}

/** Tells the line's children the way the line is shown (useShownLineWay). */
export function useTellLineWay(editor: Editor, line: string, way: Way | undefined) {
  useLayoutEffect(() => {
    if (!line) return;
    const told = toldFor(editor, line);
    if (told.way === way) return;
    told.way = way;
    for (const listener of [...told.listeners]) listener();
  }, [editor, line, way]);
}

/** The way a line is shown as its own view last told it; undefined before it has. */
export function useShownLineWay(editor: Editor, line: string): Way | undefined {
  const subscribe = useCallback((listener: () => void) => {
    if (!line) return () => {};
    const told = toldFor(editor, line);
    told.listeners.add(listener);
    return () => { told.listeners.delete(listener); };
  }, [editor, line]);
  return useSyncExternalStore(subscribe, () => line ? toldWays.get(editor)?.get(line)?.way : undefined);
}
