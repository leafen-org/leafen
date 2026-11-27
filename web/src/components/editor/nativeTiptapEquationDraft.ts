import { Extension } from "@tiptap/core";
import { Plugin, PluginKey, type Transaction } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";
import { EDITOR_MARKDOWN_BOUNDS } from "./editorClipboardPolicy";
import { spansToHtml } from "./richtext";

type Draft = { at: number; to: number; tex: string };
type Asked = { at?: number; to?: number; tex: string } | null;
const key = new PluginKey<Draft | null>("nativeEquationDraft");

/**
 * Draws the equation being made, `tex` as typed so far, at `at` when given
 * — in place of the words up to `to`, which it hides while it stands —, or
 * nothing with null; a step of no history, nothing of it the page's until
 * it is done.
 */
export function showEquationDraft(view: EditorView, asked: Asked) {
  if (key.getState(view.state) === undefined || (!asked && !key.getState(view.state))) return;
  view.dispatch(view.state.tr.setMeta(key, asked).setMeta("addToHistory", false));
}

/** The transaction that makes the equation takes its drawing away. */
export function withoutEquationDraft(tr: Transaction) {
  return tr.setMeta(key, null);
}

function chip(tex: string) {
  const element = document.createElement("span");
  element.contentEditable = "false";
  element.setAttribute("data-equation-draft", "true");
  element.innerHTML = spansToHtml([tex ? { text: tex, equation: tex } : { text: "", emptyPiece: "equation" }], EDITOR_MARKDOWN_BOUNDS.richtext);
  element.querySelector("[data-equation]")?.setAttribute("data-equation-editing", "true");
  return element;
}

/**
 * An equation made where the caret is ("/인라인 수학 공식", ⌘⇧E) stands in
 * the line while its TeX is typed, as Notion's (seen 2026-11-11): the gray
 * "√x 새로운 수학 공식" piece, chosen, then the equation as typed; it is
 * the page's only when done, an Undo taking it away whole, and closing it
 * empty leaves the line as it was. Made of chosen words (√x, ⌘⇧E), it is
 * drawn of them in their place at once, the words hidden until it is done
 * or closed (seen 2026-09-30).
 */
export const NativeTiptapEquationDraft = Extension.create({
  name: "nativeEquationDraft",
  addProseMirrorPlugins() {
    return [new Plugin<Draft | null>({
      key,
      state: {
        init: () => null,
        apply(tr, draft) {
          const asked = tr.getMeta(key) as Asked | undefined;
          const mapped = draft && tr.docChanged
            ? { ...draft, at: tr.mapping.map(draft.at, -1), to: Math.max(tr.mapping.map(draft.at, -1), tr.mapping.map(draft.to, 1)) } : draft;
          if (asked === undefined) return mapped;
          if (!asked) return null;
          return asked.at !== undefined ? { at: asked.at, to: asked.to ?? asked.at, tex: asked.tex } : mapped && { ...mapped, tex: asked.tex };
        },
      },
      props: {
        decorations(state) {
          const draft = key.getState(state);
          return draft ? DecorationSet.create(state.doc, [Decoration.widget(draft.at, () => chip(draft.tex),
            { side: -1, ignoreSelection: true, key: `equation-draft:${draft.tex}` }),
          ...(draft.to > draft.at ? [Decoration.inline(draft.at, draft.to, { style: "display: none", "data-equation-draft-hidden": "true" })] : [])]) : null;
        },
      },
    })];
  },
});
