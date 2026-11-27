import type { Editor } from "@tiptap/core";
import { Plugin, PluginKey, type EditorState, type Transaction } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import styles from "./MentionQuery.module.css";
import { imeComposing } from "@/lib/fieldKeyBoundary";

type MentionQueryRange = { from: number; to: number; placeholder?: string } | null;
type MentionQueryPlace = "page" | "cell" | "comment";
const key = new PluginKey<MentionQueryRange>("mentionQuery");

/**
 * What the "@" menu's chosen row would add after what is typed, drawn faint
 * after it in the pill (MentionMenu decides it): kept with the words it was
 * read for, so it never shows after other words a keystroke has made.
 */
type MentionGhost = { query: string; rest: string };
let ghost: MentionGhost | null = null;
const ghostListeners = new Set<() => void>();

/** Shows the faint rest after the open "@" and its query, or none. */
export function showMentionGhost(next: MentionGhost | null) {
  const value = next?.rest ? next : null;
  if (ghost?.query === value?.query && ghost?.rest === value?.rest) return;
  ghost = value;
  ghostListeners.forEach((listener) => listener());
}

/**
 * "@" (or a page's "/") and what is typed after it, while its menu is open,
 * in a pale pill as Notion's (seen 2026-10-28): its outline is its own tone,
 * 5.5px — 2.5px in a comment box. A "/" alone says in it what typing does
 * (Notion's, seen 2026-11-11), an "@" the rest of its menu's chosen row
 * (showMentionGhost), hidden while a syllable is composed, as Notion's (seen
 * 2026-11-13). The range follows the typing; the menu's owner shows or clears
 * it (showMentionQuery).
 */
function mentionQueryPlugin(place: MentionQueryPlace) {
  return new Plugin<MentionQueryRange>({
    key,
    state: {
      init: () => null,
      apply(tr, value) {
        const next = tr.getMeta(key) as MentionQueryRange | undefined;
        if (next !== undefined) return next;
        return value && { ...value, from: tr.mapping.map(value.from, -1), to: tr.mapping.map(value.to, 1) };
      },
    },
    view(view) {
      // A new rest redraws the pill it belongs to, after any syllable being composed.
      let waiting = false;
      const redraw = () => {
        if (!key.getState(view.state) || waiting) return;
        if (imeComposing(view)) {
          waiting = true;
          view.dom.addEventListener("compositionend", () => requestAnimationFrame(() => { waiting = false; redraw(); }), { once: true });
          return;
        }
        view.dispatch(view.state.tr.setMeta("addToHistory", false));
      };
      ghostListeners.add(redraw);
      return { destroy: () => { ghostListeners.delete(redraw); } };
    },
    props: {
      decorations(state) {
        const range = key.getState(state);
        if (!range || range.to <= range.from) return null;
        const typed = state.doc.textBetween(range.from, range.to);
        // Only after an "@" that begins its line: none after a word, a space
        // or a line break (Notion's, seen 2026-11-13: "@jun" + "e lee", "@" +
        // "오늘"; "낱말 @jun", " @jun", ⇧↵ "@jun" and "ab @" none).
        const lineStart = state.doc.resolve(range.from).parentOffset === 0;
        const rest = !range.placeholder && lineStart && ghost && typed === `@${ghost.query}` ? ghost.rest : "";
        return DecorationSet.create(state.doc, [Decoration.inline(range.from, range.to,
          { class: styles.pill, "data-mention-query": place, ...range.placeholder || rest ? { "data-query-placeholder": range.placeholder || rest } : {} }),
        ...suggestedAroundQuery(state, range.from, range.to)]);
      },
      handleDOMEvents: {
        compositionstart: (view) => { view.dom.setAttribute("data-composing", ""); return false; },
        compositionend: (view) => { view.dom.removeAttribute("data-composing"); return false; },
      },
    },
  });
}

/**
 * The pill stands alone in the suggestion its words were typed in, as
 * Notion's (seen 2026-11-18 in Chrome at 1440, dark, a page and a database
 * page's side peek: the "/" and its "검색어를 입력하세요" in the chip's grey with
 * the words' own color, no blue, tint or line under them): the suggestion's
 * mark leaves the span that holds the pill (MentionQuery.module.css), and the
 * rest of its words there keep their marks, drawn on them here.
 */
function suggestedAroundQuery(state: EditorState, from: number, to: number) {
  const $from = state.doc.resolve(from), start = $from.start();
  const suggestion = (value: unknown) => {
    try { const kind = (JSON.parse(String(value)) as { suggestion?: unknown }).suggestion; return typeof kind === "string" ? kind : null; } catch { return null; }
  };
  const mark = state.doc.nodeAt(from)?.marks.find((each) => each.type.name === "nativeAnnotation" && suggestion(each.attrs.stringValue));
  if (!mark) return [];
  // The line's text under that same mark comes in runs, each one span.
  const runs: [number, number][] = [];
  let at = start;
  $from.parent.forEach((child) => {
    const next = at + child.nodeSize, last = runs[runs.length - 1];
    if (child.marks.some((each) => each.eq(mark))) {
      if (last && last[1] === at) last[1] = next; else runs.push([at, next]);
    }
    at = next;
  });
  const run = runs.find(([runFrom, runTo]) => runFrom <= from && to <= runTo);
  if (!run) return [];
  const kind = suggestion(mark.attrs.stringValue);
  return [[run[0], from], [to, run[1]]].filter(([pieceFrom, pieceTo]) => pieceTo > pieceFrom)
    .map(([pieceFrom, pieceTo]) => Decoration.inline(pieceFrom, pieceTo, { "data-suggested-near-query": String(kind) }));
}

/** Gives an editor the pill; the returned function takes it away. */
export function registerMentionQuery(editor: Editor, place: MentionQueryPlace = "page") {
  editor.registerPlugin(mentionQueryPlugin(place));
  return () => { if (!editor.isDestroyed) editor.unregisterPlugin(key); };
}

/** What a menu's pick puts in takes the pill away with it, as Notion's (seen 2026-11-03). */
export function withoutMentionQuery(tr: Transaction) {
  return tr.setMeta(key, null);
}

/** Shows the pill over the "@" and its query, or none. */
export function showMentionQuery(editor: Editor, range: MentionQueryRange) {
  if (editor.isDestroyed || !key.get(editor.state)) return;
  const current = key.getState(editor.state);
  if (current?.from === range?.from && current?.to === range?.to && current?.placeholder === range?.placeholder) return;
  editor.view.dispatch(editor.state.tr.setMeta(key, range).setMeta("addToHistory", false));
}
