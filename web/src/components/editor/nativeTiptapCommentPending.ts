import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";

const PENDING = "leafen:comment-pending";
const key = new PluginKey<Pending>("nativeCommentPending");

/** How words a new comment is on look: being written, or its draft put away. */
export type CommentPendingState = "writing" | "kept";
type Request = { owner: string; ranges: readonly Range[]; state: CommentPendingState };
type Words = { from: number; to: number; state: CommentPendingState };
type Pending = { asked: ReadonlyMap<string, readonly Words[]>; set: DecorationSet };

/**
 * Marks the words of `ranges` (drawn text of any page's editor) as a new
 * comment's while `owner` asks; no ranges take its mark away.
 */
export function requestCommentPendingWords(owner: string, ranges: readonly Range[], state: CommentPendingState = "writing") {
  window.dispatchEvent(new CustomEvent<Request>(PENDING, { detail: { owner, ranges, state } }));
}

function wordsIn(view: EditorView, { ranges, state }: Request): Words[] {
  const words: Words[] = [];
  for (const range of ranges) {
    if (!view.dom.contains(range.startContainer) || !view.dom.contains(range.endContainer)) continue;
    try {
      const from = view.posAtDOM(range.startContainer, range.startOffset);
      const to = view.posAtDOM(range.endContainer, range.endOffset);
      if (to > from) words.push({ from, to, state });
    } catch { /* words no longer drawn */ }
  }
  return words;
}

/**
 * Words a new comment is being written on are wrapped as a discussion's
 * text is, and drawn by CommentAnchorMarks: Notion's span with its line 1px
 * under the words (seen 2026-11-12), darker while written, fainter while its
 * draft is put away. The page's text stays as it is.
 */
export const NativeTiptapCommentPending = Extension.create({
  name: "nativeCommentPending",
  addProseMirrorPlugins() {
    const draw = (doc: Parameters<typeof DecorationSet.create>[0], asked: Pending["asked"]) => asked.size
      ? DecorationSet.create(doc, [...asked.values()].flat().map(({ from, to, state }) =>
        Decoration.inline(from, to, { "data-comment-pending": state }, { inclusiveStart: false, inclusiveEnd: false })))
      : DecorationSet.empty;
    return [new Plugin<Pending>({
      key,
      state: {
        init: () => ({ asked: new Map(), set: DecorationSet.empty }),
        apply(tr, value) {
          const request = tr.getMeta(key) as { owner: string; words: readonly Words[] } | undefined;
          if (!request && (!value.asked.size || !tr.docChanged)) return value;
          const asked = new Map<string, readonly Words[]>();
          for (const [owner, words] of value.asked) {
            const mapped = words.map((word) => ({ ...word, from: tr.mapping.map(word.from, 1), to: tr.mapping.map(word.to, -1) }))
              .filter((word) => word.to > word.from);
            if (mapped.length) asked.set(owner, mapped);
          }
          if (request) {
            if (request.words.length) asked.set(request.owner, request.words); else asked.delete(request.owner);
          }
          return { asked, set: draw(tr.doc, asked) };
        },
      },
      props: { decorations: (state) => key.getState(state)?.set },
      view: (view) => {
        const listen = (event: Event) => {
          const request = (event as CustomEvent<Request>).detail;
          const words = wordsIn(view, request);
          if (!words.length && !key.getState(view.state)?.asked.has(request.owner)) return;
          view.dispatch(view.state.tr.setMeta(key, { owner: request.owner, words }).setMeta("addToHistory", false));
        };
        window.addEventListener(PENDING, listen);
        return { destroy: () => window.removeEventListener(PENDING, listen) };
      },
    })];
  },
});
