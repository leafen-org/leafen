import { Extension, type Editor } from "@tiptap/core";
import { Selection, TextSelection } from "@tiptap/pm/state";
import { pageTitleJoin } from "@/lib/inlineDatabaseRequests";
import { editorBlockChildren } from "./editorBlockIndex";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { projectedBlocks } from "./nativeTiptapBinding";
import { nativeTiptapSelectionContext } from "./nativeTiptapSelectionContext";
import { imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * Backspace at the start of the first line of a synced copy shown on
 * another page, as Notion's (seen 2026-11-16 three times, in two tabs, a
 * letter typed after the key and no script before it): the caret goes back
 * over the copy's edge to the end of the text before the copy on that page —
 * the line before it, or the page's title where the copy opens the page —
 * and the letter before it there goes, the copy's own lines staying as they
 * were ("beforeZfirst" over the copy became "beforeZfirs", a page "w1343
 * sync C" opened by the copy "w1343 sync", the next letter going in there).
 * A line of another kind first keeps its own Backspace (a list item turning
 * into text, …), which Notion's copies were not seen with.
 */
export function backspaceBeforeSyncedCopy(editor: Editor, adapter: NativeEditorGraphAdapter, rootId: string | null): boolean {
  const copy = adapter.moveContext?.syncedCopy;
  const { selection } = editor.state;
  if (!copy || !rootId || !editor.isEditable || imeComposing(editor.view) || !(selection instanceof TextSelection) || !selection.empty
    || selection.$from.parentOffset !== 0) return false;
  const target = nativeTiptapSelectionContext(editor.state);
  const line = target?.field === "text" ? target.block : null;
  if (!line || line.container.attrs.kind !== "paragraph" || editorBlockChildren(adapter.graph, rootId)[0]?.id !== line.nativeId) return false;
  const page = copy.page();
  if (page.readOnly) return true;
  // The copy's page draws the copy, and this editor inside it.
  const outer = (editor.view.dom.parentElement?.closest(".ProseMirror") as (HTMLElement & { editor?: Editor }) | null)?.editor;
  const shown = outer && projectedBlocks(outer.state.doc).find((block) => block.nativeId === copy.blockId);
  if (!outer || !shown) return true;
  const back = Selection.findFrom(outer.state.doc.resolve(shown.contentFrom - 2), -1, true);
  if (!back) {
    const title = pageTitleJoin(page.pageId);
    if (title) {
      const kept = Array.from(title.title).slice(0, -1).join("");
      title.set(kept, kept.length);
    }
    return true;
  }
  // A letter of two code units, or a mention, goes whole.
  const before = back.$from.nodeBefore;
  const size = before?.isText ? Array.from(before.text ?? "").pop()?.length ?? 0 : before?.nodeSize ?? 0;
  const tr = outer.state.tr;
  if (size) tr.delete(back.from - size, back.from);
  tr.setSelection(TextSelection.create(tr.doc, back.from - size));
  editor.view.dom.blur();
  outer.view.dispatch(tr.scrollIntoView());
  outer.view.focus();
  return true;
}

/** Its key, ahead of the structure and merge keys (1050, 1000), which keep the line where it is. */
export const NativeTiptapSyncedCopyBackspace = Extension.create<{ adapter: (() => NativeEditorGraphAdapter) | null; rootId: string | null }>({
  name: "nativeSyncedCopyBackspace", priority: 1075,
  addOptions() {
    return { adapter: null, rootId: null };
  },
  addKeyboardShortcuts() {
    return {
      Backspace: ({ editor }) => {
        const { adapter, rootId } = this.options;
        return adapter ? backspaceBeforeSyncedCopy(editor, adapter(), rootId) : false;
      },
    };
  },
});
