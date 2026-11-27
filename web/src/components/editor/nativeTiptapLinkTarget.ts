import { Extension } from "@tiptap/core";
import { Plugin, PluginKey, type Transaction } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";

/** The words a link window is open on, from and to in the page's text. */
export type LinkWindowTarget = { from: number; to: number } | null;
type Marked = { target: LinkWindowTarget; set: DecorationSet };
const key = new PluginKey<Marked>("nativeLinkTarget");

/** Marks, in `tr`, the words a link window is open on; null takes the mark away. */
export function withLinkWindowTarget(tr: Transaction, target: LinkWindowTarget) {
  return tr.setMeta(key, target).setMeta("addToHistory", false);
}

/** Marks the words a link window is open on, or with null takes the mark away. */
export function markLinkWindowTarget(view: EditorView, target: LinkWindowTarget) {
  if (view.isDestroyed) return;
  const current = key.getState(view.state)?.target ?? null;
  if (current === target || (current && target && current.from === target.from && current.to === target.to)) return;
  view.dispatch(withLinkWindowTarget(view.state.tr, target));
}

/**
 * While a link window is open on words — ⌘K's, the selection toolbar's link
 * button's, a link's "편집" — they are drawn on the gray ground in a span 2.5px
 * taller above and below, a link among them as plain words
 * (EditorInlineDecorations), so the window shows what it links, as Notion's
 * (measured 2026-11-14 and 2026-11-15 in Chrome at 1440: graBacSecTra, light
 * #2a1c0012 and dark #ffffeb1a, padding 2.5px 0, the link's own fade and
 * underline gone while its "편집" window is open). The words follow the text's
 * changes while it stays open.
 */
export const NativeTiptapLinkTarget = Extension.create({
  name: "nativeLinkTarget",
  addProseMirrorPlugins() {
    const draw = (doc: Parameters<typeof DecorationSet.create>[0], target: LinkWindowTarget) => target
      ? DecorationSet.create(doc, [Decoration.inline(target.from, target.to, { "data-link-target": "" })])
      : DecorationSet.empty;
    return [new Plugin<Marked>({
      key,
      state: {
        init: () => ({ target: null, set: DecorationSet.empty }),
        apply(tr, value) {
          const asked = tr.getMeta(key) as LinkWindowTarget | undefined;
          if (asked !== undefined) return { target: asked, set: draw(tr.doc, asked) };
          if (!value.target || !tr.docChanged) return value;
          const from = tr.mapping.map(value.target.from, 1), to = tr.mapping.map(value.target.to, -1);
          const target = to > from ? { from, to } : null;
          return { target, set: draw(tr.doc, target) };
        },
      },
      props: { decorations: (state) => key.getState(state)?.set },
    })];
  },
});
