import { Extension, getTextBetween, getTextSerializersFromSchema } from "@tiptap/core";
import { Fragment, Slice, type Node, type Schema } from "@tiptap/pm/model";
import { Plugin, type EditorState } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { LEAFEN_WORDS_MIME, writeTextWithMime } from "@/lib/clipboard";
import { copiedBlockContent, copiedWords } from "@/lib/clipboardServer";
import { EDITOR_CLIPBOARD_BOUNDS } from "./editorClipboardPolicy";
import type { TextSpan } from "@/lib/types";
import { fragmentToNativeRichText, nativeRichTextToFragment } from "./nativeTiptapInline";
import { nativeAtomShownText, shownWordsSerializer } from "./nativeTiptapSchema";

// Words of a line, as a copy gives them around suggested edits (copiedWords);
// a line with no suggestion stays the very nodes it was.
function copiedInline(content: Fragment, schema: Schema, asBlocks: boolean, code: boolean): Fragment {
  try {
    const spans = fragmentToNativeRichText(content);
    if (!spans.some((span) => span.suggestion !== undefined || span.suggestionId !== undefined || span.suggestionBefore !== undefined)) return content;
    return nativeRichTextToFragment(copiedWords(spans, asBlocks), schema, code);
  } catch {
    return content;
  }
}

function copiedFragment(fragment: Fragment, schema: Schema, asBlocks: boolean, code = false): Fragment {
  if (fragment.firstChild?.isInline) return copiedInline(fragment, schema, asBlocks, code);
  const nodes: Node[] = [];
  fragment.forEach((node) => {
    if (node.isLeaf) { nodes.push(node); return; }
    const content = copiedFragment(node.content, schema, asBlocks, node.type.spec.code === true);
    // A block copied is no part of a suggestion: a suggested line, or a suggested break's (copiedBlockContent).
    const props = node.type.name === "nativeBlock" && node.attrs.props ? copiedBlockContent(node.attrs.props as object) : null;
    nodes.push(props && props !== node.attrs.props ? node.type.create({ ...node.attrs, props }, content, node.marks) : node.copy(content));
  });
  return Fragment.fromArray(nodes);
}

// Whether a copy's words run over more than one line of text.
function overLines(fragment: Fragment) {
  let lines = 0;
  fragment.descendants((node) => {
    if (node.isTextblock) lines += 1;
    return lines < 2 && !node.isTextblock;
  });
  return lines > 1;
}

/** A line words copied over lines run over: its block, and the words of it chosen when not all (null). */
export type CopiedLine = { id: string; words: TextSpan[] | null };

// The lines a choice of words runs over, each by its block: one whose words
// it runs through, a block holding where it starts left out; a table whole.
function chosenLines(state: EditorState) {
  const { from, to } = state.selection;
  const lines: (CopiedLine & { table?: true })[] = [];
  state.doc.nodesBetween(from, to, (node, pos) => {
    if (node.type.name !== "nativeBlock") return !node.isTextblock;
    const id = String(node.attrs.sourceId || node.attrs.id);
    if (node.attrs.kind === "table") {
      lines.push({ id, words: null, table: true });
      return false;
    }
    const body = node.firstChild, start = pos + 1;
    if (!body || start + body.nodeSize <= from || start >= to) return true;
    const size = body.content.size, cutFrom = Math.max(0, from - start - 1), cutTo = Math.min(size, to - start - 1);
    lines.push({ id, words: body.isTextblock && (cutFrom > 0 || cutTo < size) ? fragmentToNativeRichText(body.content.cut(cutFrom, cutTo)) : null });
    return true;
  });
  return lines;
}

/** What words copied over lines give other apps: the lines as text and as HTML. */
export type CopiedLines = { markdown: string; html: string };

/**
 * Words copied or cut in the page's text — a range of them, not all of a
 * line's ⌘A chose (wholeLineCopied) — come with no suggestion on them and
 * suggested formatting as it was before, as Notion's copy pastes them (seen
 * 2026-11-11): within a line, words suggested in or away as words; over
 * lines, as the lines were before their suggestions (copiedWords), which
 * are no part of one. Text dragged within the page moves with its
 * suggestions.
 */
export const NativeTiptapCopiedWords = Extension.create<{ linesCopy(lines: readonly CopiedLine[]): CopiedLines | null }>({
  name: "nativeCopiedWords",
  addOptions: () => ({ linesCopy: () => null }),
  addProseMirrorPlugins() {
    // Other apps take words over lines as a copy of the lines they run over
    // gives them (copiedLines); within a line or a table, its mentions as
    // their chips show them (nativeAtomShownText).
    const over = (state: EditorState) => {
      const lines = overLines(state.selection.content().content) ? chosenLines(state) : [];
      return lines.length && !(lines.length === 1 && lines[0].table) ? this.options.linesCopy(lines) : null;
    };
    const text = (state: EditorState) => {
      const lines = over(state);
      if (lines) return lines.markdown;
      const textSerializers = { ...getTextSerializersFromSchema(state.schema), nativeAtom: ({ node }: { node: Node }) => nativeAtomShownText(node.attrs) };
      return [...state.selection.ranges].sort((a, b) => a.$from.pos - b.$from.pos)
        .map(({ $from, $to }) => getTextBetween(state.doc, { from: $from.pos, to: $to.pos }, { textSerializers })).join("\n\n");
    };
    let copying = false;
    // Words over lines give other apps the lines' HTML, as Notion's does, and
    // a page here the editor's own HTML of them (LEAFEN_WORDS_MIME), which
    // pastes them back as they were (nativeTiptapClipboard).
    const copy = (view: EditorView, event: Event) => {
      const data = (event as ClipboardEvent).clipboardData, lines = data ? over(view.state) : null;
      if (data && lines) {
        copying = true;
        let own = "";
        try { own = view.serializeForClipboard(view.state.selection.content()).dom.innerHTML; } finally { copying = false; }
        if (writeTextWithMime(data, lines.markdown, LEAFEN_WORDS_MIME, own, lines.html, EDITOR_CLIPBOARD_BOUNDS)) {
          event.preventDefault();
          if (event.type === "cut" && view.editable) view.dispatch(view.state.tr.deleteSelection().scrollIntoView().setMeta("uiEvent", "cut"));
          return true;
        }
      }
      copying = true;
      queueMicrotask(() => { copying = false; });
      return false;
    };
    return [new Plugin({ props: {
      handleDOMEvents: { copy, cut: copy },
      clipboardTextSerializer: (_slice, view) => text(view.state),
      clipboardSerializer: shownWordsSerializer(this.editor.schema),
      transformCopied: (slice, view) => {
        if (!copying) return slice;
        copying = false;
        return new Slice(copiedFragment(slice.content, view.state.schema, overLines(slice.content)), slice.openStart, slice.openEnd);
      },
    } })];
  },
});
